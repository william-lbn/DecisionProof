package gitlab

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/delivery"
)

var (
	// ErrDeliveryConflict 表示外部状态存在但不等于本次批准基线，必须人工处理。
	ErrDeliveryConflict = errors.New("GitLab 外部状态与工程就绪包冲突")
	// ErrDeliveryUnknown 表示写请求结果无法由 Read-back 证明，禁止盲目重试。
	ErrDeliveryUnknown = errors.New("GitLab 写入结果 UNKNOWN")
)

// DeliveryAPI 只暴露 GitLab 交付所需能力，便于隔离 SDK 并验证故障语义。
type DeliveryAPI interface {
	FindIssues(context.Context, int64, string) ([]Issue, error)
	GetIssue(context.Context, int64, int64) (Issue, error)
	CreateIssue(context.Context, CreateIssueRequest) (Issue, error)
	GetBranch(context.Context, int64, string) (Branch, error)
	CreateBranch(context.Context, int64, string, string) (Branch, error)
	GetFile(context.Context, int64, string, string) (RepositoryFile, error)
	CreateCommit(context.Context, CreateCommitRequest) (Commit, error)
	FindOpenMergeRequest(context.Context, int64, string, string) ([]MergeRequest, error)
	GetMergeRequest(context.Context, int64, int64) (MergeRequest, error)
	CreateMergeRequest(context.Context, CreateMRRequest) (MergeRequest, error)
}

type DeliveryRequest struct {
	ProjectID      int64
	DefaultBranch  string
	IdempotencyKey string
	Bundle         delivery.EngineeringBundle
}

// DeliveryResult 绑定真实 GitLab ID/SHA 与精确工程包 Digest。
type DeliveryResult struct {
	ProjectID       int64  `json:"project_id"`
	PackageID       string `json:"package_id"`
	PackageDigest   string `json:"package_digest"`
	IdempotencyKey  string `json:"idempotency_key"`
	IssueIID        int64  `json:"issue_iid"`
	Branch          string `json:"branch"`
	CommitSHA       string `json:"commit_sha"`
	MergeRequestIID int64  `json:"merge_request_iid"`
	MergeRequestURL string `json:"merge_request_url"`
}

type DeliveryExecutor struct {
	API DeliveryAPI
}

// Execute 按 Issue → Branch → Commit → Draft MR 执行。
// 每一步都先查、写后再查；写错误只有在 Read-back 证明目标状态后才能视为成功。
func (e DeliveryExecutor) Execute(ctx context.Context, in DeliveryRequest) (DeliveryResult, error) {
	if e.API == nil {
		return DeliveryResult{}, errors.New("GitLab Delivery API 不能为空")
	}
	if err := validateDeliveryRequest(in); err != nil {
		return DeliveryResult{}, err
	}
	marker := "<!-- decisionproof:idempotency:" + in.IdempotencyKey + " -->"
	result := DeliveryResult{
		ProjectID: in.ProjectID, PackageID: in.Bundle.PackageID,
		PackageDigest: in.Bundle.PackageDigest, IdempotencyKey: in.IdempotencyKey,
		Branch: in.Bundle.SuggestedBranch,
	}

	issue, err := e.ensureIssue(ctx, in, marker)
	if err != nil {
		return result, err
	}
	result.IssueIID = issue.IID

	branch, err := e.ensureBranch(ctx, in)
	if err != nil {
		return result, err
	}

	commitSHA, err := e.ensureCommit(ctx, in, branch)
	if err != nil {
		return result, err
	}
	result.CommitSHA = commitSHA

	mr, err := e.ensureDraftMR(ctx, in, marker, commitSHA)
	if err != nil {
		return result, err
	}
	result.MergeRequestIID = mr.IID
	result.MergeRequestURL = mr.WebURL
	return result, nil
}

func validateDeliveryRequest(in DeliveryRequest) error {
	if in.ProjectID <= 0 || strings.TrimSpace(in.DefaultBranch) == "" {
		return errors.New("GitLab ProjectID 和默认分支不能为空")
	}
	if strings.TrimSpace(in.Bundle.PackageID) == "" || !strings.HasPrefix(in.Bundle.PackageDigest, "sha256:") {
		return errors.New("工程就绪包 ID 或 Digest 无效")
	}
	if strings.TrimSpace(in.Bundle.SuggestedBranch) == "" || len(in.Bundle.Files) == 0 {
		return errors.New("工程交付分支和文件不能为空")
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" || len(key) > 200 || strings.ContainsAny(key, " \t\r\n<>") {
		return errors.New("幂等键为空或格式无效")
	}
	return nil
}

func (e DeliveryExecutor) ensureIssue(ctx context.Context, in DeliveryRequest, marker string) (Issue, error) {
	issues, err := e.API.FindIssues(ctx, in.ProjectID, in.IdempotencyKey)
	if err != nil {
		return Issue{}, fmt.Errorf("查询 GitLab Issue 幂等状态失败: %w", err)
	}
	for _, issue := range issues {
		if strings.Contains(issue.Description, marker) {
			return e.verifyIssue(ctx, in, issue.IID, marker)
		}
	}
	created, writeErr := e.API.CreateIssue(ctx, CreateIssueRequest{
		ProjectID: in.ProjectID, Title: in.Bundle.IssueTitle,
		Description: marker + "\n\n" + in.Bundle.IssueDescription,
		Labels:      in.Bundle.SuggestedLabels,
	})
	if writeErr == nil {
		return e.verifyIssue(ctx, in, created.IID, marker)
	}
	issues, readErr := e.API.FindIssues(ctx, in.ProjectID, in.IdempotencyKey)
	if readErr == nil {
		for _, issue := range issues {
			if strings.Contains(issue.Description, marker) {
				return e.verifyIssue(ctx, in, issue.IID, marker)
			}
		}
	}
	return Issue{}, fmt.Errorf("%w: Issue 写入后无法对账", ErrDeliveryUnknown)
}

func (e DeliveryExecutor) verifyIssue(ctx context.Context, in DeliveryRequest, iid int64, marker string) (Issue, error) {
	issue, err := e.API.GetIssue(ctx, in.ProjectID, iid)
	if err != nil {
		return Issue{}, fmt.Errorf("回读 GitLab Issue 失败: %w", err)
	}
	if issue.Title != in.Bundle.IssueTitle || !strings.Contains(issue.Description, marker) ||
		!strings.Contains(issue.Description, in.Bundle.PackageDigest) {
		return Issue{}, fmt.Errorf("%w: Issue IID %d 内容不匹配", ErrDeliveryConflict, iid)
	}
	return issue, nil
}

func (e DeliveryExecutor) ensureBranch(ctx context.Context, in DeliveryRequest) (Branch, error) {
	branch, err := e.API.GetBranch(ctx, in.ProjectID, in.Bundle.SuggestedBranch)
	if err == nil {
		return branch, nil
	}
	if !isNotFound(err) {
		return Branch{}, fmt.Errorf("查询 GitLab Branch 失败: %w", err)
	}
	created, writeErr := e.API.CreateBranch(ctx, in.ProjectID, in.Bundle.SuggestedBranch, in.DefaultBranch)
	if writeErr == nil {
		branch, err = e.API.GetBranch(ctx, in.ProjectID, created.Name)
		if err == nil {
			return branch, nil
		}
	}
	branch, readErr := e.API.GetBranch(ctx, in.ProjectID, in.Bundle.SuggestedBranch)
	if readErr == nil {
		return branch, nil
	}
	return Branch{}, fmt.Errorf("%w: Branch 写入后无法对账", ErrDeliveryUnknown)
}

func (e DeliveryExecutor) ensureCommit(ctx context.Context, in DeliveryRequest, branch Branch) (string, error) {
	exact, commitSHA, exists, err := e.filesMatch(ctx, in)
	if err != nil {
		return "", fmt.Errorf("回读 GitLab 文件失败: %w", err)
	}
	if exists {
		if !exact {
			return "", fmt.Errorf("%w: 分支上批准文件已存在但内容不同", ErrDeliveryConflict)
		}
		return commitSHA, nil
	}
	paths := make([]string, 0, len(in.Bundle.Files))
	for filePath := range in.Bundle.Files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	actions := make([]CommitAction, 0, len(paths))
	for _, filePath := range paths {
		actions = append(actions, CommitAction{Action: "create", FilePath: filePath, Content: in.Bundle.Files[filePath]})
	}
	created, writeErr := e.API.CreateCommit(ctx, CreateCommitRequest{
		ProjectID: in.ProjectID, Branch: in.Bundle.SuggestedBranch,
		CommitMessage: "docs(decisionproof): add package " + in.Bundle.PackageID,
		Actions:       actions,
	})
	exact, readSHA, exists, readErr := e.filesMatch(ctx, in)
	if readErr == nil && exists && exact {
		if readSHA != "" {
			return readSHA, nil
		}
		return created.ID, nil
	}
	if writeErr != nil {
		return "", fmt.Errorf("%w: Commit 写入后无法对账", ErrDeliveryUnknown)
	}
	return "", fmt.Errorf("%w: Commit 回读内容不匹配", ErrDeliveryConflict)
}

func (e DeliveryExecutor) filesMatch(ctx context.Context, in DeliveryRequest) (exact bool, commitSHA string, exists bool, err error) {
	paths := make([]string, 0, len(in.Bundle.Files))
	for filePath := range in.Bundle.Files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	matched := 0
	missing := 0
	for _, filePath := range paths {
		expected := in.Bundle.Files[filePath]
		file, fileErr := e.API.GetFile(ctx, in.ProjectID, filePath, in.Bundle.SuggestedBranch)
		if fileErr != nil {
			if isNotFound(fileErr) {
				missing++
				continue
			}
			return false, "", exists, fileErr
		}
		exists = true
		content, decodeErr := decodeGitLabFile(file)
		if decodeErr != nil {
			return false, "", true, decodeErr
		}
		if content != expected {
			return false, "", true, nil
		}
		if commitSHA == "" {
			commitSHA = file.LastCommitID
			if commitSHA == "" {
				commitSHA = file.CommitID
			}
		}
		matched++
	}
	return missing == 0 && matched == len(in.Bundle.Files), commitSHA, exists, nil
}

func decodeGitLabFile(file RepositoryFile) (string, error) {
	if file.Encoding == "" || file.Encoding == "text" {
		return file.Content, nil
	}
	if file.Encoding != "base64" {
		return "", fmt.Errorf("不支持的 GitLab 文件编码 %q", file.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(file.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("解码 GitLab 文件失败: %w", err)
	}
	return string(content), nil
}

func (e DeliveryExecutor) ensureDraftMR(ctx context.Context, in DeliveryRequest, marker, commitSHA string) (MergeRequest, error) {
	mrs, err := e.API.FindOpenMergeRequest(ctx, in.ProjectID, in.Bundle.SuggestedBranch, in.DefaultBranch)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("查询 GitLab MR 幂等状态失败: %w", err)
	}
	if len(mrs) > 1 {
		return MergeRequest{}, fmt.Errorf("%w: 同一分支存在多个打开的 MR", ErrDeliveryConflict)
	}
	if len(mrs) == 1 {
		return e.verifyDraftMR(ctx, in, mrs[0].IID, commitSHA)
	}
	created, writeErr := e.API.CreateMergeRequest(ctx, CreateMRRequest{
		ProjectID: in.ProjectID, SourceBranch: in.Bundle.SuggestedBranch,
		TargetBranch: in.DefaultBranch, Title: in.Bundle.IssueTitle, Draft: true,
		Description: marker + "\n\nPackage Digest: `" + in.Bundle.PackageDigest + "`",
	})
	if writeErr == nil {
		return e.verifyDraftMR(ctx, in, created.IID, commitSHA)
	}
	mrs, readErr := e.API.FindOpenMergeRequest(ctx, in.ProjectID, in.Bundle.SuggestedBranch, in.DefaultBranch)
	if readErr == nil && len(mrs) == 1 {
		return e.verifyDraftMR(ctx, in, mrs[0].IID, commitSHA)
	}
	return MergeRequest{}, fmt.Errorf("%w: Draft MR 写入后无法对账", ErrDeliveryUnknown)
}

func (e DeliveryExecutor) verifyDraftMR(ctx context.Context, in DeliveryRequest, iid int64, commitSHA string) (MergeRequest, error) {
	mr, err := e.API.GetMergeRequest(ctx, in.ProjectID, iid)
	if err != nil {
		return MergeRequest{}, fmt.Errorf("回读 GitLab MR 失败: %w", err)
	}
	if mr.SourceBranch != in.Bundle.SuggestedBranch || mr.TargetBranch != in.DefaultBranch ||
		mr.State != "opened" || !mr.Draft || (mr.SHA != "" && commitSHA != "" && mr.SHA != commitSHA) {
		return MergeRequest{}, fmt.Errorf("%w: MR IID %d 状态或 Head SHA 不匹配", ErrDeliveryConflict, iid)
	}
	return mr, nil
}

func isNotFound(err error) bool {
	var apiErr *HTTPError
	return errors.As(err, &apiErr) && apiErr.Status == 404
}
