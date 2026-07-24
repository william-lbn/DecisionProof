package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore 是可崩溃恢复的单节点持久化实现。
// 它采用“复制快照 -> 写临时文件 -> fsync -> 原子 rename -> 替换内存快照”的顺序，
// 可用于 P0 演示、边缘部署和无数据库的故障注入实验。
// 多副本生产部署应使用 PostgreSQL 适配器，不能把共享 PVC 当成分布式事务数据库。
type FileStore struct {
	mu       sync.RWMutex
	path     string
	snapshot Snapshot
}

func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("快照文件路径不能为空")
	}
	store := &FileStore{path: path, snapshot: NewSnapshot()}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取快照失败: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("解析快照失败，拒绝以损坏数据启动: %w", err)
	}
	s.snapshot = snapshot
	return nil
}

func (s *FileStore) Transact(ctx context.Context, fn func(Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	working, err := cloneSnapshot(s.snapshot)
	if err != nil {
		return err
	}
	if err := fn(&snapshotTx{snapshot: &working}); err != nil {
		return err
	}
	if err := persistSnapshotAtomically(s.path, working); err != nil {
		return err
	}
	s.snapshot = working
	return nil
}

func persistSnapshotAtomically(path string, snapshot Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化快照失败: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sdlcproof-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时快照失败: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	defer cleanup()

	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("设置快照权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("写入临时快照失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("同步临时快照失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时快照失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("原子替换快照失败: %w", err)
	}
	// 同步目录元数据，确保 rename 在掉电后仍然可见。
	dir, err := os.Open(filepath.Dir(path))
	if err == nil {
		defer dir.Close()
		_ = dir.Sync()
	}
	return nil
}
