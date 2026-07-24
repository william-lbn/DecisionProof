package store

import (
	"context"
	"encoding/json"
	"sync"
)

// MemoryStore 是并发安全的事务性内存存储，主要用于单元测试和本地演示。
// 每次事务都在快照副本上执行，只有函数成功返回后才整体替换，从而模拟原子提交。
type MemoryStore struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{snapshot: NewSnapshot()}
}

func (s *MemoryStore) Transact(ctx context.Context, fn func(Tx) error) error {
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
	s.snapshot = working
	return nil
}

// ExportSnapshot 仅供测试、备份和完整性检查使用。
func (s *MemoryStore) ExportSnapshot() (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func cloneSnapshot(source Snapshot) (Snapshot, error) {
	data, err := json.Marshal(source)
	if err != nil {
		return Snapshot{}, err
	}
	var target Snapshot
	if err := json.Unmarshal(data, &target); err != nil {
		return Snapshot{}, err
	}
	return target, nil
}
