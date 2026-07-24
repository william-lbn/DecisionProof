package gitlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// InboxStatus 表示 GitLab Webhook 的处理状态。
// Webhook 只负责可靠接收，复杂业务逻辑必须由后台 Reconciler 异步执行。
type InboxStatus string

const (
	InboxPending    InboxStatus = "PENDING"
	InboxProcessing InboxStatus = "PROCESSING"
	InboxProcessed  InboxStatus = "PROCESSED"
	InboxFailed     InboxStatus = "FAILED"
)

// InboxRecord 是经过签名校验后持久化的 Webhook 原始事实。
type InboxRecord struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id"`
	IntegrationID string          `json:"integration_id"`
	EventUUID     string          `json:"event_uuid"`
	EventType     string          `json:"event_type"`
	ProjectID     int64           `json:"project_id,omitempty"`
	PayloadDigest string          `json:"payload_digest"`
	Payload       json.RawMessage `json:"payload"`
	Status        InboxStatus     `json:"status"`
	Attempts      int             `json:"attempts"`
	LastError     string          `json:"last_error,omitempty"`
	ReceivedAt    time.Time       `json:"received_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	ProcessedAt   *time.Time      `json:"processed_at,omitempty"`
}

// InboxStore 定义 Webhook Inbox 的最小持久化能力。
// PutIfAbsent 必须原子去重，防止 GitLab 重复投递触发重复副作用。
type InboxStore interface {
	PutIfAbsent(record InboxRecord) (stored bool, existing InboxRecord, err error)
	Get(id string) (InboxRecord, error)
	List(status InboxStatus, limit int) ([]InboxRecord, error)
	Update(record InboxRecord) error
}

// MemoryInbox 用于单元测试和本地开发，不用于生产多副本。
type MemoryInbox struct {
	mu       sync.RWMutex
	records  map[string]InboxRecord
	byDedupe map[string]string
}

func NewMemoryInbox() *MemoryInbox {
	return &MemoryInbox{records: map[string]InboxRecord{}, byDedupe: map[string]string{}}
}

func (m *MemoryInbox) PutIfAbsent(record InboxRecord) (bool, InboxRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := inboxDedupeKey(record)
	if id, ok := m.byDedupe[key]; ok {
		return false, m.records[id], nil
	}
	m.records[record.ID] = cloneInboxRecord(record)
	m.byDedupe[key] = record.ID
	return true, record, nil
}

func (m *MemoryInbox) Get(id string) (InboxRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.records[id]
	if !ok {
		return InboxRecord{}, os.ErrNotExist
	}
	return cloneInboxRecord(record), nil
}

func (m *MemoryInbox) List(status InboxStatus, limit int) ([]InboxRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]InboxRecord, 0, len(m.records))
	for _, record := range m.records {
		if status != "" && record.Status != status {
			continue
		}
		result = append(result, cloneInboxRecord(record))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ReceivedAt.Before(result[j].ReceivedAt) })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MemoryInbox) Update(record InboxRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.records[record.ID]; !ok {
		return os.ErrNotExist
	}
	m.records[record.ID] = cloneInboxRecord(record)
	return nil
}

// FileInbox 使用原子 JSON 快照实现单节点持久化，便于在没有 PostgreSQL 的环境验证 Webhook 语义。
// 生产多副本必须替换为 PostgreSQL Inbox，并对去重键建立唯一约束。
type FileInbox struct {
	mu   sync.Mutex
	path string
	mem  *MemoryInbox
}

type inboxSnapshot struct {
	Records []InboxRecord `json:"records"`
}

func NewFileInbox(path string) (*FileInbox, error) {
	if path == "" {
		return nil, errors.New("Webhook Inbox 文件路径不能为空")
	}
	f := &FileInbox{path: path, mem: NewMemoryInbox()}
	if err := f.load(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *FileInbox) PutIfAbsent(record InboxRecord) (bool, InboxRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := f.snapshot()
	stored, existing, err := f.mem.PutIfAbsent(record)
	if err != nil || !stored {
		return stored, existing, err
	}
	if err := f.persist(); err != nil {
		f.restore(before)
		return false, InboxRecord{}, err
	}
	return true, record, nil
}

func (f *FileInbox) Get(id string) (InboxRecord, error) { return f.mem.Get(id) }
func (f *FileInbox) List(status InboxStatus, limit int) ([]InboxRecord, error) {
	return f.mem.List(status, limit)
}
func (f *FileInbox) Update(record InboxRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	before := f.snapshot()
	if err := f.mem.Update(record); err != nil {
		return err
	}
	if err := f.persist(); err != nil {
		f.restore(before)
		return err
	}
	return nil
}

func (f *FileInbox) snapshot() []InboxRecord {
	records, _ := f.mem.List("", 0)
	return records
}

func (f *FileInbox) restore(records []InboxRecord) {
	mem := NewMemoryInbox()
	for _, record := range records {
		_, _, _ = mem.PutIfAbsent(record)
	}
	f.mem = mem
}

func (f *FileInbox) load() error {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 Webhook Inbox 失败: %w", err)
	}
	var snapshot inboxSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("解析 Webhook Inbox 失败: %w", err)
	}
	mem := NewMemoryInbox()
	for _, record := range snapshot.Records {
		_, _, _ = mem.PutIfAbsent(record)
	}
	f.mem = mem
	return nil
}

func (f *FileInbox) persist() error {
	records, err := f.mem.List("", 0)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(inboxSnapshot{Records: records}, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 Webhook Inbox 失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
		return fmt.Errorf("创建 Webhook Inbox 目录失败: %w", err)
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入 Webhook Inbox 临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("提交 Webhook Inbox 快照失败: %w", err)
	}
	return nil
}

func inboxDedupeKey(record InboxRecord) string {
	if record.EventUUID != "" {
		return record.TenantID + ":" + record.IntegrationID + ":uuid:" + record.EventUUID
	}
	return record.TenantID + ":" + record.IntegrationID + ":digest:" + record.EventType + ":" + record.PayloadDigest
}

func cloneInboxRecord(record InboxRecord) InboxRecord {
	record.Payload = append(json.RawMessage(nil), record.Payload...)
	return record
}
