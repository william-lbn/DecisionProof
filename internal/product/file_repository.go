package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileRepository 为单节点开发、PoC 和本地演示提供原子 JSON 快照。
// 生产多副本必须使用 PostgreSQL，实现见 migrations/0002_decision_to_production.sql。
type FileRepository struct {
	mu   sync.Mutex
	path string
	mem  *MemoryRepository
}
type fileSnapshot struct {
	Documents map[string]SourceDocument              `json:"documents"`
	Blocks    map[string][]SourceBlock               `json:"blocks"`
	Decisions map[string]ProductDecision             `json:"decisions"`
	Packages  map[string]EngineeringReadinessPackage `json:"packages"`
	Runs      map[string]ExtractionRun               `json:"runs"`
}

func NewFileRepository(path string) (*FileRepository, error) {
	if path == "" {
		return nil, errors.New("产品快照路径不能为空")
	}
	r := &FileRepository{path: path, mem: NewMemoryRepository()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var s fileSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("解析产品快照失败: %w", err)
	}
	if s.Documents != nil {
		r.mem.documents = s.Documents
	}
	if s.Blocks != nil {
		r.mem.blocks = s.Blocks
	}
	if s.Decisions != nil {
		r.mem.decisions = s.Decisions
	}
	if s.Packages != nil {
		r.mem.packages = s.Packages
	}
	if s.Runs != nil {
		r.mem.runs = s.Runs
	}
	return r, nil
}
func (r *FileRepository) persist() error {
	r.mem.mu.RLock()
	s := fileSnapshot{Documents: cloneMap(r.mem.documents), Blocks: cloneBlocks(r.mem.blocks), Decisions: cloneMap(r.mem.decisions), Packages: cloneMap(r.mem.packages), Runs: cloneMap(r.mem.runs)}
	r.mem.mu.RUnlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.path), "product-snapshot-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, r.path)
}
func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneBlocks(in map[string][]SourceBlock) map[string][]SourceBlock {
	out := make(map[string][]SourceBlock, len(in))
	for k, v := range in {
		out[k] = append([]SourceBlock(nil), v...)
	}
	return out
}
func (r *FileRepository) write(fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 原子文件提交失败时必须回滚内存状态，否则 API 会返回失败但后续读取却看见未落盘数据。
	before := r.snapshot()
	if err := fn(); err != nil {
		return err
	}
	if err := r.persist(); err != nil {
		r.restore(before)
		return err
	}
	return nil
}

func (r *FileRepository) snapshot() fileSnapshot {
	r.mem.mu.RLock()
	defer r.mem.mu.RUnlock()
	return fileSnapshot{
		Documents: cloneMap(r.mem.documents), Blocks: cloneBlocks(r.mem.blocks),
		Decisions: cloneMap(r.mem.decisions), Packages: cloneMap(r.mem.packages), Runs: cloneMap(r.mem.runs),
	}
}

func (r *FileRepository) restore(s fileSnapshot) {
	r.mem.mu.Lock()
	defer r.mem.mu.Unlock()
	r.mem.documents = cloneMap(s.Documents)
	r.mem.blocks = cloneBlocks(s.Blocks)
	r.mem.decisions = cloneMap(s.Decisions)
	r.mem.packages = cloneMap(s.Packages)
	r.mem.runs = cloneMap(s.Runs)
}
func (r *FileRepository) SaveDocument(ctx context.Context, v SourceDocument) error {
	return r.write(func() error { return r.mem.SaveDocument(ctx, v) })
}
func (r *FileRepository) UpdateDocument(ctx context.Context, v SourceDocument) error {
	return r.write(func() error { return r.mem.UpdateDocument(ctx, v) })
}
func (r *FileRepository) GetDocument(ctx context.Context, t, id string) (SourceDocument, error) {
	return r.mem.GetDocument(ctx, t, id)
}
func (r *FileRepository) ListDocuments(ctx context.Context, t, p string) ([]SourceDocument, error) {
	return r.mem.ListDocuments(ctx, t, p)
}
func (r *FileRepository) SaveBlocks(ctx context.Context, v []SourceBlock) error {
	return r.write(func() error { return r.mem.SaveBlocks(ctx, v) })
}
func (r *FileRepository) ListBlocks(ctx context.Context, t, d string) ([]SourceBlock, error) {
	return r.mem.ListBlocks(ctx, t, d)
}
func (r *FileRepository) SaveDecision(ctx context.Context, v ProductDecision) error {
	return r.write(func() error { return r.mem.SaveDecision(ctx, v) })
}
func (r *FileRepository) UpdateDecision(ctx context.Context, v ProductDecision, e int) error {
	return r.write(func() error { return r.mem.UpdateDecision(ctx, v, e) })
}
func (r *FileRepository) GetDecision(ctx context.Context, t, id string) (ProductDecision, error) {
	return r.mem.GetDecision(ctx, t, id)
}
func (r *FileRepository) ListDecisions(ctx context.Context, t, p string) ([]ProductDecision, error) {
	return r.mem.ListDecisions(ctx, t, p)
}
func (r *FileRepository) SavePackage(ctx context.Context, v EngineeringReadinessPackage) error {
	return r.write(func() error { return r.mem.SavePackage(ctx, v) })
}
func (r *FileRepository) GetPackage(ctx context.Context, t, id string) (EngineeringReadinessPackage, error) {
	return r.mem.GetPackage(ctx, t, id)
}
func (r *FileRepository) SaveExtractionRun(ctx context.Context, v ExtractionRun) error {
	return r.write(func() error { return r.mem.SaveExtractionRun(ctx, v) })
}
func (r *FileRepository) SaveExtraction(ctx context.Context, doc SourceDocument, run ExtractionRun, decisions []ProductDecision) error {
	return r.write(func() error { return r.mem.SaveExtraction(ctx, doc, run, decisions) })
}
