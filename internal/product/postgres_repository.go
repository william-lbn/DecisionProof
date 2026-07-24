package product

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PostgresRepository 是产品输入、决策和工程就绪包的 PostgreSQL 实现。
// 每个方法都在事务内设置 RLS 租户上下文；调用方不能依靠 SQL 参数绕过租户隔离。
type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL 数据库连接不能为空")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) withTenant(ctx context.Context, tenantID string, fn func(*sql.Tx) error) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("PostgreSQL 产品事务缺少租户")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("开启产品数据库事务失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID); err != nil {
		return fmt.Errorf("设置产品数据库租户上下文失败: %w", err)
	}
	if err := fn(tx); err != nil {
		return mapProductPostgresError(err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交产品数据库事务失败: %w", err)
	}
	return nil
}

func (r *PostgresRepository) SaveDocument(ctx context.Context, v SourceDocument) error {
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO source_documents
(tenant_id,document_id,project_id,name,media_type,storage_uri,digest,version,status,block_count,
 error_message,uploaded_by,created_at,processed_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			v.TenantID, v.ID, v.ProjectID, v.Name, v.MediaType, v.StorageURI, v.Digest, v.Version,
			v.Status, v.BlockCount, v.Error, v.UploadedBy, v.CreatedAt, v.ProcessedAt)
		return err
	})
}

func (r *PostgresRepository) UpdateDocument(ctx context.Context, v SourceDocument) error {
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		result, err := tx.Exec(`
UPDATE source_documents SET project_id=$3,name=$4,media_type=$5,storage_uri=$6,digest=$7,
 version=$8,status=$9,block_count=$10,error_message=$11,uploaded_by=$12,processed_at=$13
			WHERE tenant_id=$1 AND document_id=$2`, v.TenantID, v.ID, v.ProjectID, v.Name, v.MediaType,
			v.StorageURI, v.Digest, v.Version, v.Status, v.BlockCount, v.Error, v.UploadedBy, v.ProcessedAt)
		if err != nil {
			return err
		}
		return requireProductRow(result)
	})
}

func (r *PostgresRepository) GetDocument(ctx context.Context, tenantID, id string) (SourceDocument, error) {
	var out SourceDocument
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return tx.QueryRow(`
SELECT document_id,tenant_id,project_id,name,media_type,storage_uri,digest,version,status,
       block_count,error_message,uploaded_by,created_at,processed_at
FROM source_documents WHERE tenant_id=$1 AND document_id=$2`, tenantID, id).Scan(
			&out.ID, &out.TenantID, &out.ProjectID, &out.Name, &out.MediaType, &out.StorageURI,
			&out.Digest, &out.Version, &out.Status, &out.BlockCount, &out.Error, &out.UploadedBy,
			&out.CreatedAt, &out.ProcessedAt)
	})
	return out, err
}

func (r *PostgresRepository) ListDocuments(ctx context.Context, tenantID, projectID string) ([]SourceDocument, error) {
	out := make([]SourceDocument, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.Query(`
SELECT document_id,tenant_id,project_id,name,media_type,storage_uri,digest,version,status,
       block_count,error_message,uploaded_by,created_at,processed_at
FROM source_documents
WHERE tenant_id=$1 AND ($2='' OR project_id=$2) ORDER BY created_at`, tenantID, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v SourceDocument
			if err := rows.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.Name, &v.MediaType, &v.StorageURI,
				&v.Digest, &v.Version, &v.Status, &v.BlockCount, &v.Error, &v.UploadedBy,
				&v.CreatedAt, &v.ProcessedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

func (r *PostgresRepository) SaveBlocks(ctx context.Context, blocks []SourceBlock) error {
	if len(blocks) == 0 {
		return nil
	}
	tenantID := blocks[0].TenantID
	return r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		for _, v := range blocks {
			if v.TenantID != tenantID || v.DocumentID != blocks[0].DocumentID {
				return errors.New("同一批来源块必须属于同一租户和文档")
			}
			if _, err := tx.Exec(`
INSERT INTO source_blocks
(tenant_id,block_id,project_id,document_id,kind,locator,text_content,digest,sequence,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.TenantID, v.ID, v.ProjectID, v.DocumentID,
				v.Kind, v.Locator, v.Text, v.Digest, v.Sequence, v.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *PostgresRepository) ListBlocks(ctx context.Context, tenantID, documentID string) ([]SourceBlock, error) {
	out := make([]SourceBlock, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.Query(`
SELECT block_id,tenant_id,project_id,document_id,kind,locator,text_content,digest,sequence,created_at
FROM source_blocks WHERE tenant_id=$1 AND document_id=$2 ORDER BY sequence`, tenantID, documentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v SourceBlock
			if err := rows.Scan(&v.ID, &v.TenantID, &v.ProjectID, &v.DocumentID, &v.Kind, &v.Locator,
				&v.Text, &v.Digest, &v.Sequence, &v.CreatedAt); err != nil {
				return err
			}
			v.Anchor = SourceAnchor{DocumentID: v.DocumentID, BlockID: v.ID, Locator: v.Locator, Digest: v.Digest}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) == 0 {
			return ErrNotFound
		}
		return nil
	})
	return out, err
}

func (r *PostgresRepository) SaveDecision(ctx context.Context, v ProductDecision) error {
	anchors, err := json.Marshal(v.Anchors)
	if err != nil {
		return err
	}
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO product_decisions
(tenant_id,decision_id,project_id,kind,title,content,status,confidence,anchors,owner_id,
 confirmed_by,approved_by,version,canonical_digest,supersedes_id,created_at,updated_at,
 confirmed_at,approved_at,origin,rejected_by,rejection_reason,rejected_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
			v.TenantID, v.ID, v.ProjectID, v.Kind, v.Title, v.Content, v.Status, v.Confidence, anchors,
			v.Owner, v.ConfirmedBy, v.ApprovedBy, v.Version, v.CanonicalDigest, v.SupersedesID,
			v.CreatedAt, v.UpdatedAt, v.ConfirmedAt, v.ApprovedAt, v.Origin, v.RejectedBy,
			v.RejectionReason, v.RejectedAt)
		return err
	})
}

func (r *PostgresRepository) UpdateDecision(ctx context.Context, v ProductDecision, expected int) error {
	anchors, err := json.Marshal(v.Anchors)
	if err != nil {
		return err
	}
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		result, err := tx.Exec(`
UPDATE product_decisions SET project_id=$3,kind=$4,title=$5,content=$6,status=$7,confidence=$8,
 anchors=$9,owner_id=$10,confirmed_by=$11,approved_by=$12,version=$13,canonical_digest=$14,
 supersedes_id=$15,updated_at=$16,confirmed_at=$17,approved_at=$18,origin=$19,rejected_by=$20,
 rejection_reason=$21,rejected_at=$22
WHERE tenant_id=$1 AND decision_id=$2 AND version=$23`, v.TenantID, v.ID, v.ProjectID, v.Kind,
			v.Title, v.Content, v.Status, v.Confidence, anchors, v.Owner, v.ConfirmedBy, v.ApprovedBy,
			v.Version, v.CanonicalDigest, v.SupersedesID, v.UpdatedAt, v.ConfirmedAt, v.ApprovedAt,
			v.Origin, v.RejectedBy, v.RejectionReason, v.RejectedAt, expected)
		if err != nil {
			return err
		}
		return requireProductRow(result)
	})
}

func (r *PostgresRepository) GetDecision(ctx context.Context, tenantID, id string) (ProductDecision, error) {
	var out ProductDecision
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		return scanDecision(tx.QueryRow(`
SELECT decision_id,tenant_id,project_id,kind,origin,title,content,status,confidence,anchors,
 owner_id,confirmed_by,approved_by,rejected_by,rejection_reason,version,canonical_digest,
 supersedes_id,created_at,updated_at,confirmed_at,approved_at,rejected_at
FROM product_decisions WHERE tenant_id=$1 AND decision_id=$2`, tenantID, id), &out)
	})
	return out, err
}

func (r *PostgresRepository) ListDecisions(ctx context.Context, tenantID, projectID string) ([]ProductDecision, error) {
	out := make([]ProductDecision, 0)
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		rows, err := tx.Query(`
SELECT decision_id,tenant_id,project_id,kind,origin,title,content,status,confidence,anchors,
 owner_id,confirmed_by,approved_by,rejected_by,rejection_reason,version,canonical_digest,
 supersedes_id,created_at,updated_at,confirmed_at,approved_at,rejected_at
FROM product_decisions
WHERE tenant_id=$1 AND ($2='' OR project_id=$2) ORDER BY created_at`, tenantID, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v ProductDecision
			if err := scanDecision(rows, &v); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}

type rowScanner interface {
	Scan(...any) error
}

func scanDecision(row rowScanner, out *ProductDecision) error {
	var anchors []byte
	if err := row.Scan(&out.ID, &out.TenantID, &out.ProjectID, &out.Kind, &out.Origin, &out.Title,
		&out.Content, &out.Status, &out.Confidence, &anchors, &out.Owner, &out.ConfirmedBy,
		&out.ApprovedBy, &out.RejectedBy, &out.RejectionReason, &out.Version, &out.CanonicalDigest,
		&out.SupersedesID, &out.CreatedAt, &out.UpdatedAt, &out.ConfirmedAt, &out.ApprovedAt,
		&out.RejectedAt); err != nil {
		return err
	}
	return json.Unmarshal(anchors, &out.Anchors)
}

func (r *PostgresRepository) SavePackage(ctx context.Context, v EngineeringReadinessPackage) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO engineering_readiness_packages
(tenant_id,package_id,project_id,digest,package,created_by,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)`, v.TenantID, v.ID, v.ProjectID, v.Digest, body, v.CreatedBy, v.CreatedAt)
		return err
	})
}

func (r *PostgresRepository) GetPackage(ctx context.Context, tenantID, id string) (EngineeringReadinessPackage, error) {
	var out EngineeringReadinessPackage
	err := r.withTenant(ctx, tenantID, func(tx *sql.Tx) error {
		var body []byte
		if err := tx.QueryRow(`
SELECT package FROM engineering_readiness_packages WHERE tenant_id=$1 AND package_id=$2`,
			tenantID, id).Scan(&body); err != nil {
			return err
		}
		return json.Unmarshal(body, &out)
	})
	return out, err
}

func (r *PostgresRepository) SaveExtractionRun(ctx context.Context, v ExtractionRun) error {
	ids, err := json.Marshal(v.DecisionIDs)
	if err != nil {
		return err
	}
	return r.withTenant(ctx, v.TenantID, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
INSERT INTO extraction_runs
(tenant_id,extraction_run_id,project_id,document_id,provider,model,prompt_digest,config_digest,
 decision_ids,started_at,finished_at,status,error_message)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, v.TenantID, v.ID, v.ProjectID,
			v.DocumentID, v.Provider, v.Model, v.PromptDigest, v.ConfigDigest, ids, v.StartedAt,
			v.FinishedAt, v.Status, v.Error)
		return err
	})
}

// SaveExtraction 在一个数据库事务中提交文档状态、所有候选决策和提取运行记录。
func (r *PostgresRepository) SaveExtraction(ctx context.Context, doc SourceDocument, run ExtractionRun, decisions []ProductDecision) error {
	return r.withTenant(ctx, doc.TenantID, func(tx *sql.Tx) error {
		result, err := tx.Exec(`
UPDATE source_documents SET version=$3,status=$4,block_count=$5,error_message=$6,processed_at=$7
WHERE tenant_id=$1 AND document_id=$2`, doc.TenantID, doc.ID, doc.Version, doc.Status,
			doc.BlockCount, doc.Error, doc.ProcessedAt)
		if err != nil {
			return err
		}
		if err := requireProductRow(result); err != nil {
			return err
		}
		for _, v := range decisions {
			if v.TenantID != doc.TenantID || v.ProjectID != doc.ProjectID {
				return errors.New("提取决策必须与来源文档属于同一租户和项目")
			}
			anchors, err := json.Marshal(v.Anchors)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`
INSERT INTO product_decisions
(tenant_id,decision_id,project_id,kind,title,content,status,confidence,anchors,owner_id,
 confirmed_by,approved_by,version,canonical_digest,supersedes_id,created_at,updated_at,
 confirmed_at,approved_at,origin,rejected_by,rejection_reason,rejected_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
				v.TenantID, v.ID, v.ProjectID, v.Kind, v.Title, v.Content, v.Status, v.Confidence,
				anchors, v.Owner, v.ConfirmedBy, v.ApprovedBy, v.Version, v.CanonicalDigest,
				v.SupersedesID, v.CreatedAt, v.UpdatedAt, v.ConfirmedAt, v.ApprovedAt, v.Origin,
				v.RejectedBy, v.RejectionReason, v.RejectedAt); err != nil {
				return err
			}
		}
		ids, err := json.Marshal(run.DecisionIDs)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`
INSERT INTO extraction_runs
(tenant_id,extraction_run_id,project_id,document_id,provider,model,prompt_digest,config_digest,
 decision_ids,started_at,finished_at,status,error_message)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, run.TenantID, run.ID, run.ProjectID,
			run.DocumentID, run.Provider, run.Model, run.PromptDigest, run.ConfigDigest, ids,
			run.StartedAt, run.FinishedAt, run.Status, run.Error)
		return err
	})
}

func requireProductRow(result sql.Result) error {
	if result == nil {
		return errors.New("PostgreSQL 更新结果为空")
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

type sqlStateError interface {
	SQLState() string
}

func mapProductPostgresError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var state sqlStateError
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return fmt.Errorf("%w: %v", ErrAlreadyExists, err)
	}
	return err
}
