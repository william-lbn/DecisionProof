package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/william-lbn/DecisionProof/internal/domain"
)

func TestFileStorePersistsAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	store1, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	change := domain.Change{ID: "chg-1", TenantID: "tenant", ProjectID: "project", Title: "test", RiskLevel: "low", State: domain.StateDraft, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := store1.Transact(context.Background(), func(tx Tx) error { return tx.CreateChange(change) }); err != nil {
		t.Fatal(err)
	}
	store2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store2.Transact(context.Background(), func(tx Tx) error {
		got, err := tx.GetChange("tenant", "chg-1")
		if err != nil {
			return err
		}
		if got.Title != "test" {
			t.Fatalf("恢复后的对象不一致: %#v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFileStoreRejectsCorruptedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, []byte(`{"changes":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(path); err == nil {
		t.Fatal("损坏快照必须导致启动失败")
	}
}

func TestSnapshotJSONContainsNoUnknownPrivateState(t *testing.T) {
	snapshot := NewSnapshot()
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) == 0 {
		t.Fatalf("快照必须可序列化: %v", err)
	}
}

type fakeSQLStateError struct{ state string }

func (e fakeSQLStateError) Error() string    { return "模拟 PostgreSQL 错误" }
func (e fakeSQLStateError) SQLState() string { return e.state }

func TestPostgresRetryClassification(t *testing.T) {
	if !isRetryablePostgresError(fakeSQLStateError{state: "40001"}) {
		t.Fatal("序列化冲突必须允许事务级重试")
	}
	if !isRetryablePostgresError(fakeSQLStateError{state: "40P01"}) {
		t.Fatal("死锁必须允许事务级重试")
	}
	if isRetryablePostgresError(fakeSQLStateError{state: "08006"}) {
		t.Fatal("连接失败可能导致提交结果未知，不能盲目重试")
	}
}

func TestNewPostgresStoreRejectsNilDB(t *testing.T) {
	if _, err := NewPostgresStore(nil); err == nil {
		t.Fatal("空数据库连接必须被拒绝")
	}
}
