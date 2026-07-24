package product

import (
	"context"
	"errors"
	"sort"
	"sync"
)

var (
	ErrNotFound      = errors.New("产品对象不存在")
	ErrConflict      = errors.New("产品对象版本冲突")
	ErrAlreadyExists = errors.New("产品对象已经存在")
)

// Repository 是产品输入与决策层的存储抽象。
type Repository interface {
	SaveDocument(context.Context, SourceDocument) error
	UpdateDocument(context.Context, SourceDocument) error
	GetDocument(context.Context, string, string) (SourceDocument, error)
	ListDocuments(context.Context, string, string) ([]SourceDocument, error)
	SaveBlocks(context.Context, []SourceBlock) error
	ListBlocks(context.Context, string, string) ([]SourceBlock, error)
	SaveDecision(context.Context, ProductDecision) error
	UpdateDecision(context.Context, ProductDecision, int) error
	GetDecision(context.Context, string, string) (ProductDecision, error)
	ListDecisions(context.Context, string, string) ([]ProductDecision, error)
	SavePackage(context.Context, EngineeringReadinessPackage) error
	GetPackage(context.Context, string, string) (EngineeringReadinessPackage, error)
	SaveExtractionRun(context.Context, ExtractionRun) error
	SaveExtraction(context.Context, SourceDocument, ExtractionRun, []ProductDecision) error
}

// MemoryRepository 是线程安全的开发与测试实现。
type MemoryRepository struct {
	mu        sync.RWMutex
	documents map[string]SourceDocument
	blocks    map[string][]SourceBlock
	decisions map[string]ProductDecision
	packages  map[string]EngineeringReadinessPackage
	runs      map[string]ExtractionRun
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		documents: map[string]SourceDocument{}, blocks: map[string][]SourceBlock{},
		decisions: map[string]ProductDecision{}, packages: map[string]EngineeringReadinessPackage{},
		runs: map[string]ExtractionRun{},
	}
}

func key(tenantID, id string) string { return tenantID + "\x00" + id }

func (r *MemoryRepository) SaveDocument(ctx context.Context, v SourceDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	if _, ok := r.documents[k]; ok {
		return ErrAlreadyExists
	}
	r.documents[k] = v
	return nil
}
func (r *MemoryRepository) UpdateDocument(ctx context.Context, v SourceDocument) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	if _, ok := r.documents[k]; !ok {
		return ErrNotFound
	}
	r.documents[k] = v
	return nil
}
func (r *MemoryRepository) GetDocument(ctx context.Context, t, id string) (SourceDocument, error) {
	if err := ctx.Err(); err != nil {
		return SourceDocument{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.documents[key(t, id)]
	if !ok {
		return SourceDocument{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) ListDocuments(ctx context.Context, t, p string) ([]SourceDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []SourceDocument{}
	for _, v := range r.documents {
		if v.TenantID == t && (p == "" || v.ProjectID == p) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (r *MemoryRepository) SaveBlocks(ctx context.Context, v []SourceBlock) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(v) == 0 {
		return nil
	}
	k := key(v[0].TenantID, v[0].DocumentID)
	cp := append([]SourceBlock(nil), v...)
	r.blocks[k] = cp
	return nil
}
func (r *MemoryRepository) ListBlocks(ctx context.Context, t, d string) ([]SourceBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.blocks[key(t, d)]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]SourceBlock(nil), v...), nil
}
func (r *MemoryRepository) SaveDecision(ctx context.Context, v ProductDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	if _, ok := r.decisions[k]; ok {
		return ErrAlreadyExists
	}
	r.decisions[k] = v
	return nil
}
func (r *MemoryRepository) UpdateDecision(ctx context.Context, v ProductDecision, expected int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	old, ok := r.decisions[k]
	if !ok {
		return ErrNotFound
	}
	if old.Version != expected {
		return ErrConflict
	}
	r.decisions[k] = v
	return nil
}
func (r *MemoryRepository) GetDecision(ctx context.Context, t, id string) (ProductDecision, error) {
	if err := ctx.Err(); err != nil {
		return ProductDecision{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.decisions[key(t, id)]
	if !ok {
		return ProductDecision{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) ListDecisions(ctx context.Context, t, p string) ([]ProductDecision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []ProductDecision{}
	for _, v := range r.decisions {
		if v.TenantID == t && (p == "" || v.ProjectID == p) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (r *MemoryRepository) SavePackage(ctx context.Context, v EngineeringReadinessPackage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	if _, ok := r.packages[k]; ok {
		return ErrAlreadyExists
	}
	r.packages[k] = v
	return nil
}
func (r *MemoryRepository) GetPackage(ctx context.Context, t, id string) (EngineeringReadinessPackage, error) {
	if err := ctx.Err(); err != nil {
		return EngineeringReadinessPackage{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.packages[key(t, id)]
	if !ok {
		return EngineeringReadinessPackage{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) SaveExtractionRun(ctx context.Context, v ExtractionRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.TenantID, v.ID)
	if _, ok := r.runs[k]; ok {
		return ErrAlreadyExists
	}
	r.runs[k] = v
	return nil
}

// SaveExtraction 原子保存文档处理状态、候选决策和提取执行记录。
// 任一对象冲突时不留下部分提取结果。
func (r *MemoryRepository) SaveExtraction(ctx context.Context, doc SourceDocument, run ExtractionRun, decisions []ProductDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	docKey := key(doc.TenantID, doc.ID)
	if _, ok := r.documents[docKey]; !ok {
		return ErrNotFound
	}
	runKey := key(run.TenantID, run.ID)
	if _, ok := r.runs[runKey]; ok {
		return ErrAlreadyExists
	}
	for _, decision := range decisions {
		if decision.TenantID != doc.TenantID || decision.ProjectID != doc.ProjectID {
			return errors.New("提取决策必须与来源文档属于同一租户和项目")
		}
		if _, ok := r.decisions[key(decision.TenantID, decision.ID)]; ok {
			return ErrAlreadyExists
		}
	}
	r.documents[docKey] = doc
	for _, decision := range decisions {
		r.decisions[key(decision.TenantID, decision.ID)] = decision
	}
	r.runs[runKey] = run
	return nil
}
