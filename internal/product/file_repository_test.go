package product

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRepositoryRollsBackMemoryWhenPersistFails(t *testing.T) {
	// 将快照路径指向一个已有目录，最终 Rename 必然失败。
	path := filepath.Join(t.TempDir(), "snapshot-dir")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	repo, err := NewFileRepository(path)
	if err != nil {
		// 目录读取会失败，因此先使用正常路径初始化，再切换到目录路径模拟提交失败。
		repo, err = NewFileRepository(filepath.Join(t.TempDir(), "initial.json"))
		if err != nil {
			t.Fatal(err)
		}
		repo.path = path
	}
	doc := SourceDocument{ID: "d1", TenantID: "t1", ProjectID: "p1", Name: "x", Digest: "sha256:x", Version: 1, Status: DocumentUploaded, CreatedAt: time.Now()}
	if err := repo.SaveDocument(context.Background(), doc); err == nil {
		t.Fatal("持久化失败应返回错误")
	}
	if _, err := repo.GetDocument(context.Background(), "t1", "d1"); err != ErrNotFound {
		t.Fatalf("持久化失败后内存状态未回滚: %v", err)
	}
}
