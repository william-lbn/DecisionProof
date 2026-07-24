package gitlab

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/william-lbn/DecisionProof/internal/delivery"
)

type fakeDeliveryAPI struct {
	issue                 *Issue
	branch                *Branch
	files                 map[string]RepositoryFile
	mr                    *MergeRequest
	issueCreates          int
	branchCreates         int
	commitCreates         int
	mrCreates             int
	commitThenError       bool
	commitWithoutStateErr bool
}

func (f *fakeDeliveryAPI) FindIssues(context.Context, int64, string) ([]Issue, error) {
	if f.issue == nil {
		return nil, nil
	}
	return []Issue{*f.issue}, nil
}
func (f *fakeDeliveryAPI) GetIssue(context.Context, int64, int64) (Issue, error) {
	if f.issue == nil {
		return Issue{}, &HTTPError{Status: 404}
	}
	return *f.issue, nil
}
func (f *fakeDeliveryAPI) CreateIssue(_ context.Context, in CreateIssueRequest) (Issue, error) {
	f.issueCreates++
	issue := Issue{IID: 11, Title: in.Title, Description: in.Description, State: "opened"}
	f.issue = &issue
	return issue, nil
}
func (f *fakeDeliveryAPI) GetBranch(context.Context, int64, string) (Branch, error) {
	if f.branch == nil {
		return Branch{}, &HTTPError{Status: 404}
	}
	return *f.branch, nil
}
func (f *fakeDeliveryAPI) CreateBranch(_ context.Context, _ int64, name, _ string) (Branch, error) {
	f.branchCreates++
	branch := Branch{Name: name}
	branch.Commit.ID = "base-sha"
	f.branch = &branch
	return branch, nil
}
func (f *fakeDeliveryAPI) GetFile(_ context.Context, _ int64, filePath, _ string) (RepositoryFile, error) {
	file, ok := f.files[filePath]
	if !ok {
		return RepositoryFile{}, &HTTPError{Status: 404}
	}
	return file, nil
}
func (f *fakeDeliveryAPI) CreateCommit(_ context.Context, in CreateCommitRequest) (Commit, error) {
	f.commitCreates++
	if f.commitWithoutStateErr {
		return Commit{}, errors.New("connection reset")
	}
	if f.files == nil {
		f.files = map[string]RepositoryFile{}
	}
	for _, action := range in.Actions {
		f.files[action.FilePath] = RepositoryFile{
			FilePath: action.FilePath, Encoding: "base64",
			Content: base64.StdEncoding.EncodeToString([]byte(action.Content)), LastCommitID: "commit-sha",
		}
	}
	commit := Commit{ID: "commit-sha"}
	if f.commitThenError {
		return commit, errors.New("timeout after commit")
	}
	return commit, nil
}
func (f *fakeDeliveryAPI) FindOpenMergeRequest(context.Context, int64, string, string) ([]MergeRequest, error) {
	if f.mr == nil {
		return nil, nil
	}
	return []MergeRequest{*f.mr}, nil
}
func (f *fakeDeliveryAPI) GetMergeRequest(context.Context, int64, int64) (MergeRequest, error) {
	if f.mr == nil {
		return MergeRequest{}, &HTTPError{Status: 404}
	}
	return *f.mr, nil
}
func (f *fakeDeliveryAPI) CreateMergeRequest(_ context.Context, in CreateMRRequest) (MergeRequest, error) {
	f.mrCreates++
	mr := MergeRequest{
		IID: 22, State: "opened", Draft: in.Draft, SourceBranch: in.SourceBranch,
		TargetBranch: in.TargetBranch, SHA: "commit-sha", WebURL: "https://gitlab.example/mr/22",
	}
	f.mr = &mr
	return mr, nil
}

func deliveryFixture() DeliveryRequest {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	return DeliveryRequest{
		ProjectID: 7, DefaultBranch: "main", IdempotencyKey: "delivery:erp-1:" + digest,
		Bundle: delivery.EngineeringBundle{
			PackageID: "erp-1", PackageDigest: digest,
			IssueTitle:       "[DecisionProof] 工程就绪包 erp-1",
			IssueDescription: "Package Digest: `" + digest + "`",
			SuggestedLabels:  []string{"decisionproof"},
			SuggestedBranch:  "decisionproof/erp-1",
			Files:            map[string]string{".decisionproof/packages/erp-1/README.md": "approved\n"},
		},
	}
}

func TestDeliveryIsIdempotentAndReadBackBound(t *testing.T) {
	api := &fakeDeliveryAPI{}
	executor := DeliveryExecutor{API: api}
	first, err := executor.Execute(context.Background(), deliveryFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := executor.Execute(context.Background(), deliveryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.CommitSHA != "commit-sha" || first.MergeRequestIID != 22 {
		t.Fatalf("交付结果不稳定: first=%+v second=%+v", first, second)
	}
	if api.issueCreates != 1 || api.branchCreates != 1 || api.commitCreates != 1 || api.mrCreates != 1 {
		t.Fatalf("重复执行产生了副作用: issue=%d branch=%d commit=%d mr=%d",
			api.issueCreates, api.branchCreates, api.commitCreates, api.mrCreates)
	}
}

func TestCommitThenTimeoutIsReconciledByFileReadBack(t *testing.T) {
	api := &fakeDeliveryAPI{commitThenError: true}
	result, err := (DeliveryExecutor{API: api}).Execute(context.Background(), deliveryFixture())
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitSHA != "commit-sha" {
		t.Fatalf("没有使用回读 Commit SHA: %+v", result)
	}
}

func TestCommitUnknownIsNotRetriedBlindly(t *testing.T) {
	api := &fakeDeliveryAPI{commitWithoutStateErr: true}
	_, err := (DeliveryExecutor{API: api}).Execute(context.Background(), deliveryFixture())
	if !errors.Is(err, ErrDeliveryUnknown) {
		t.Fatalf("无法对账的写入必须为 UNKNOWN，实际 %v", err)
	}
	if api.commitCreates != 1 {
		t.Fatalf("UNKNOWN 不得盲目重试，写入次数 %d", api.commitCreates)
	}
}
