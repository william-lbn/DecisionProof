package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AuthMode 表示调用 GitLab API 时使用的令牌头。
type AuthMode string

const (
	AuthPrivateToken AuthMode = "PRIVATE-TOKEN"
	AuthJobToken     AuthMode = "JOB-TOKEN"
	AuthBearer       AuthMode = "BEARER"
)

// Client 是 GitLab REST API 的最小生产适配器。
// 读操作可在 429/5xx 时安全重试；写操作不自动重试，必须由上层 Tool Gateway 通过 Read-back 对账。
type Client struct {
	BaseURL         string
	Token           string
	AuthMode        AuthMode
	HTTP            *http.Client
	MaxReadAttempts int
}

func NewClient(baseURL, token string, mode AuthMode) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("GitLab BaseURL 不能为空")
	}
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, fmt.Errorf("GitLab BaseURL 无效: %w", err)
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("GitLab BaseURL 必须是无用户信息的 HTTP(S) 绝对地址")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("GitLab API Token 不能为空")
	}
	if mode == "" {
		mode = AuthPrivateToken
	}
	switch mode {
	case AuthPrivateToken, AuthJobToken, AuthBearer:
	default:
		return nil, fmt.Errorf("不支持的 GitLab AuthMode %q", mode)
	}
	return &Client{BaseURL: baseURL, Token: token, AuthMode: mode, HTTP: &http.Client{Timeout: 30 * time.Second}, MaxReadAttempts: 4}, nil
}

// Project 是平台接入所需的 GitLab 项目最小视图。
type Project struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	DefaultBranch     string `json:"default_branch"`
	WebURL            string `json:"web_url"`
	Visibility        string `json:"visibility"`
}
type Issue struct {
	ID          int64     `json:"id"`
	IID         int64     `json:"iid"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       string    `json:"state"`
	WebURL      string    `json:"web_url"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (c *Client) FindIssues(ctx context.Context, projectID int64, search string) ([]Issue, error) {
	q := url.Values{"scope": {"all"}, "state": {"all"}, "search": {search}, "per_page": {"100"}}
	var out []Issue
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/issues", projectID), q, &out)
	return out, err
}

// CreateIssueRequest 是工程就绪包同步到 GitLab Issue 的最小请求。
type CreateIssueRequest struct {
	ProjectID   int64
	Title       string
	Description string
	Labels      []string
}

// CommitAction 映射 GitLab Repository Commits API 的批量文件动作。
type CommitAction struct {
	Action       string `json:"action"`
	FilePath     string `json:"file_path"`
	Content      string `json:"content,omitempty"`
	Encoding     string `json:"encoding,omitempty"`
	PreviousPath string `json:"previous_path,omitempty"`
}

// CreateCommitRequest 用于把工程就绪包以 Docs-as-Code 方式提交到特性分支。
type CreateCommitRequest struct {
	ProjectID     int64
	Branch        string
	StartBranch   string
	CommitMessage string
	AuthorName    string
	AuthorEmail   string
	Actions       []CommitAction
}

// Commit 是 GitLab Commit API 的最小响应。
type Commit struct {
	ID        string    `json:"id"`
	ShortID   string    `json:"short_id"`
	Title     string    `json:"title"`
	WebURL    string    `json:"web_url"`
	CreatedAt time.Time `json:"created_at"`
}
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
	Protected bool `json:"protected"`
}

// RepositoryFile 是 GitLab Repository Files API 的可对账视图。
type RepositoryFile struct {
	FilePath     string `json:"file_path"`
	CommitID     string `json:"commit_id"`
	LastCommitID string `json:"last_commit_id"`
	BlobID       string `json:"blob_id"`
	Encoding     string `json:"encoding"`
	Content      string `json:"content"`
}

func (c *Client) GetFile(ctx context.Context, projectID int64, filePath, ref string) (RepositoryFile, error) {
	var out RepositoryFile
	err := c.get(ctx,
		fmt.Sprintf("/api/v4/projects/%d/repository/files/%s", projectID, url.PathEscape(filePath)),
		url.Values{"ref": {ref}},
		&out,
	)
	return out, err
}

type MergeRequest struct {
	ID           int64     `json:"id"`
	IID          int64     `json:"iid"`
	Title        string    `json:"title"`
	State        string    `json:"state"`
	Draft        bool      `json:"draft"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	SHA          string    `json:"sha"`
	MergeStatus  string    `json:"merge_status"`
	WebURL       string    `json:"web_url"`
	UpdatedAt    time.Time `json:"updated_at"`
}
type Pipeline struct {
	ID        int64     `json:"id"`
	IID       int64     `json:"iid"`
	ProjectID int64     `json:"project_id"`
	SHA       string    `json:"sha"`
	Ref       string    `json:"ref"`
	Status    string    `json:"status"`
	Source    string    `json:"source"`
	WebURL    string    `json:"web_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Job struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
	Commit struct {
		ID string `json:"id"`
	} `json:"commit"`
}

func (c *Client) GetProject(ctx context.Context, projectID int64) (Project, error) {
	var out Project
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d", projectID), nil, &out)
	return out, err
}
func (c *Client) GetIssue(ctx context.Context, projectID, iid int64) (Issue, error) {
	var out Issue
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid), nil, &out)
	return out, err
}
func (c *Client) CreateIssue(ctx context.Context, in CreateIssueRequest) (Issue, error) {
	payload := map[string]any{"title": in.Title, "description": in.Description}
	if len(in.Labels) > 0 {
		payload["labels"] = strings.Join(in.Labels, ",")
	}
	var out Issue
	err := c.write(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/issues", in.ProjectID), payload, &out)
	return out, err
}

func (c *Client) UpdateIssue(ctx context.Context, projectID, iid int64, title, description string, labels []string) (Issue, error) {
	payload := map[string]any{}
	if title != "" {
		payload["title"] = title
	}
	if description != "" {
		payload["description"] = description
	}
	if labels != nil {
		payload["labels"] = strings.Join(labels, ",")
	}
	var out Issue
	err := c.write(ctx, http.MethodPut, fmt.Sprintf("/api/v4/projects/%d/issues/%d", projectID, iid), payload, &out)
	return out, err
}

func (c *Client) CreateCommit(ctx context.Context, in CreateCommitRequest) (Commit, error) {
	if len(in.Actions) == 0 {
		return Commit{}, errors.New("GitLab Commit Actions 不能为空")
	}
	payload := map[string]any{
		"branch": in.Branch, "commit_message": in.CommitMessage, "actions": in.Actions,
	}
	if in.StartBranch != "" {
		payload["start_branch"] = in.StartBranch
	}
	if in.AuthorName != "" {
		payload["author_name"] = in.AuthorName
	}
	if in.AuthorEmail != "" {
		payload["author_email"] = in.AuthorEmail
	}
	var out Commit
	err := c.write(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/repository/commits", in.ProjectID), payload, &out)
	return out, err
}
func (c *Client) GetBranch(ctx context.Context, projectID int64, name string) (Branch, error) {
	var out Branch
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/repository/branches/%s", projectID, url.PathEscape(name)), nil, &out)
	return out, err
}
func (c *Client) CreateBranch(ctx context.Context, projectID int64, name, ref string) (Branch, error) {
	var out Branch
	err := c.write(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/repository/branches", projectID), map[string]string{"branch": name, "ref": ref}, &out)
	return out, err
}

type CreateMRRequest struct {
	ProjectID                                      int64
	SourceBranch, TargetBranch, Title, Description string
	Draft                                          bool
	RemoveSourceBranch                             bool
}

func (c *Client) CreateMergeRequest(ctx context.Context, in CreateMRRequest) (MergeRequest, error) {
	title := in.Title
	if in.Draft && !strings.HasPrefix(strings.ToLower(title), "draft:") {
		title = "Draft: " + title
	}
	payload := map[string]any{"source_branch": in.SourceBranch, "target_branch": in.TargetBranch, "title": title, "description": in.Description, "remove_source_branch": in.RemoveSourceBranch}
	var out MergeRequest
	err := c.write(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/merge_requests", in.ProjectID), payload, &out)
	return out, err
}
func (c *Client) GetMergeRequest(ctx context.Context, projectID, iid int64) (MergeRequest, error) {
	var out MergeRequest
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d", projectID, iid), nil, &out)
	return out, err
}
func (c *Client) FindOpenMergeRequest(ctx context.Context, projectID int64, source, target string) ([]MergeRequest, error) {
	q := url.Values{"state": {"opened"}, "source_branch": {source}, "target_branch": {target}}
	var out []MergeRequest
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/merge_requests", projectID), q, &out)
	return out, err
}

type MergeMRRequest struct {
	ProjectID, IID     int64
	SHA                string
	Squash             bool
	MergeCommitMessage string
}

func (c *Client) MergeMergeRequest(ctx context.Context, in MergeMRRequest) (MergeRequest, error) {
	payload := map[string]any{"sha": in.SHA, "squash": in.Squash}
	if in.MergeCommitMessage != "" {
		payload["merge_commit_message"] = in.MergeCommitMessage
	}
	var out MergeRequest
	err := c.write(ctx, http.MethodPut, fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/merge", in.ProjectID, in.IID), payload, &out)
	return out, err
}
func (c *Client) GetPipeline(ctx context.Context, projectID, pipelineID int64) (Pipeline, error) {
	var out Pipeline
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/pipelines/%d", projectID, pipelineID), nil, &out)
	return out, err
}
func (c *Client) ListPipelines(ctx context.Context, projectID int64, sha, ref string) ([]Pipeline, error) {
	q := url.Values{}
	if sha != "" {
		q.Set("sha", sha)
	}
	if ref != "" {
		q.Set("ref", ref)
	}
	q.Set("order_by", "id")
	q.Set("sort", "desc")
	var out []Pipeline
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/pipelines", projectID), q, &out)
	return out, err
}
func (c *Client) ListJobs(ctx context.Context, projectID, pipelineID int64) ([]Job, error) {
	var out []Job
	err := c.get(ctx, fmt.Sprintf("/api/v4/projects/%d/pipelines/%d/jobs", projectID, pipelineID), url.Values{"per_page": {"100"}}, &out)
	return out, err
}
func (c *Client) AddMRNote(ctx context.Context, projectID, iid int64, body string) error {
	var out map[string]any
	return c.write(ctx, http.MethodPost, fmt.Sprintf("/api/v4/projects/%d/merge_requests/%d/notes", projectID, iid), map[string]string{"body": body}, &out)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	attempts := c.MaxReadAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var last error
	for i := 0; i < attempts; i++ {
		err, retryAfter := c.do(ctx, http.MethodGet, path, q, nil, out)
		if err == nil {
			return nil
		}
		last = err
		var he *HTTPError
		if !errors.As(err, &he) || !(he.Status == http.StatusTooManyRequests || he.Status >= 500) {
			return err
		}
		delay := retryAfter
		if delay <= 0 {
			delay = time.Duration(100*(1<<i)) * time.Millisecond
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return last
}
func (c *Client) write(ctx context.Context, method, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.doOnly(ctx, method, path, nil, body, out)
}
func (c *Client) doOnly(ctx context.Context, method, path string, q url.Values, body []byte, out any) error {
	err, _ := c.do(ctx, method, path, q, body, out)
	return err
}
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, out any) (error, time.Duration) {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err, 0
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	switch c.AuthMode {
	case AuthJobToken:
		req.Header.Set("JOB-TOKEN", c.Token)
	case AuthBearer:
		req.Header.Set("Authorization", "Bearer "+c.Token)
	default:
		req.Header.Set("PRIVATE-TOKEN", c.Token)
	}
	baseClient := c.HTTP
	if baseClient == nil {
		baseClient = &http.Client{Timeout: 30 * time.Second}
	}
	// 禁止自动跟随重定向，避免 PRIVATE-TOKEN/JOB-TOKEN 被 3xx 转发到非预期主机。
	client := *baseClient
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GitLab API 请求失败: %w", err), 0
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err, 0
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{Status: resp.StatusCode, Method: method, Path: path, Body: safeBody(data, c.Token)}, parseRetryAfter(resp.Header.Get("Retry-After"))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析 GitLab API 响应失败: %w", err), 0
		}
	}
	return nil, 0
}

type HTTPError struct {
	Status             int
	Method, Path, Body string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GitLab API %s %s 返回 HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

var providerSecretPattern = regexp.MustCompile(`(?i)((?:private[-_ ]?token|job[-_ ]?token|authorization|access[_-]?token|api[_-]?key)["']?\s*[:=]\s*["']?)[^"',\s}]+`)

func safeBody(v []byte, exactToken string) string {
	s := strings.TrimSpace(string(v))
	if exactToken != "" {
		s = strings.ReplaceAll(s, exactToken, "[REDACTED]")
	}
	s = providerSecretPattern.ReplaceAllString(s, "${1}[REDACTED]")
	if len(s) > 2048 {
		s = s[:2048] + "…"
	}
	return s
}
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}
