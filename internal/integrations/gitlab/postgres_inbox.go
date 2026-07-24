package gitlab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostgresInbox 为多 Pod Webhook Gateway 提供原子去重和 RLS 租户隔离。
type PostgresInbox struct {
	db       *sql.DB
	tenantID string
}

func NewPostgresInbox(db *sql.DB, tenantID string) (*PostgresInbox, error) {
	if db == nil {
		return nil, errors.New("PostgreSQL Webhook Inbox 数据库不能为空")
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("PostgreSQL Webhook Inbox 必须绑定租户")
	}
	return &PostgresInbox{db: db, tenantID: strings.TrimSpace(tenantID)}, nil
}

func (p *PostgresInbox) transact(fn func(context.Context, *sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("开启 Webhook Inbox 事务失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, p.tenantID); err != nil {
		return fmt.Errorf("设置 Webhook Inbox 租户失败: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 Webhook Inbox 事务失败: %w", err)
	}
	return nil
}

func (p *PostgresInbox) PutIfAbsent(record InboxRecord) (bool, InboxRecord, error) {
	record.TenantID = p.tenantID
	var stored bool
	var existing InboxRecord
	err := p.transact(func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
INSERT INTO webhook_inbox
(tenant_id,inbox_id,integration_id,provider,event_uuid,event_type,payload_digest,payload,status,
 attempt_count,received_at,updated_at,last_error)
VALUES ($1,$2,$3,'gitlab',$4,$5,$6,$7,$8,$9,$10,$10,$11)
ON CONFLICT (tenant_id,integration_id,event_uuid) DO NOTHING`, record.TenantID, record.ID,
			record.IntegrationID, record.EventUUID, record.EventType, record.PayloadDigest,
			[]byte(record.Payload), record.Status, record.Attempts, record.ReceivedAt, record.LastError)
		if err != nil {
			return fmt.Errorf("写入 PostgreSQL Webhook Inbox 失败: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 1 {
			stored = true
			existing = record
			return nil
		}
		return scanInbox(tx.QueryRowContext(ctx, `
SELECT inbox_id,tenant_id,integration_id,event_uuid,event_type,payload_digest,payload,status,
       attempt_count,last_error,received_at,updated_at,processed_at
FROM webhook_inbox WHERE tenant_id=$1 AND integration_id=$2 AND event_uuid=$3`,
			p.tenantID, record.IntegrationID, record.EventUUID), &existing)
	})
	return stored, existing, err
}

func (p *PostgresInbox) Get(id string) (InboxRecord, error) {
	var out InboxRecord
	err := p.transact(func(ctx context.Context, tx *sql.Tx) error {
		return scanInbox(tx.QueryRowContext(ctx, `
SELECT inbox_id,tenant_id,integration_id,event_uuid,event_type,payload_digest,payload,status,
       attempt_count,last_error,received_at,updated_at,processed_at
FROM webhook_inbox WHERE tenant_id=$1 AND inbox_id=$2`, p.tenantID, id), &out)
	})
	return out, mapInboxError(err)
}

func (p *PostgresInbox) List(status InboxStatus, limit int) ([]InboxRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := make([]InboxRecord, 0)
	err := p.transact(func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT inbox_id,tenant_id,integration_id,event_uuid,event_type,payload_digest,payload,status,
       attempt_count,last_error,received_at,updated_at,processed_at
FROM webhook_inbox WHERE tenant_id=$1 AND ($2='' OR status=$2)
ORDER BY received_at LIMIT $3`, p.tenantID, status, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var record InboxRecord
			if err := scanInbox(rows, &record); err != nil {
				return err
			}
			out = append(out, record)
		}
		return rows.Err()
	})
	return out, err
}

func (p *PostgresInbox) Update(record InboxRecord) error {
	record.TenantID = p.tenantID
	return p.transact(func(ctx context.Context, tx *sql.Tx) error {
		var processed any
		if record.Status == InboxProcessed {
			now := record.UpdatedAt
			processed = now
		}
		result, err := tx.ExecContext(ctx, `
UPDATE webhook_inbox SET status=$3,attempt_count=$4,last_error=$5,processed_at=$6,updated_at=$7
WHERE tenant_id=$1 AND inbox_id=$2`, p.tenantID, record.ID, record.Status, record.Attempts,
			record.LastError, processed, record.UpdatedAt)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return sql.ErrNoRows
		}
		return nil
	})
}

func (p *PostgresInbox) ClaimNext(now time.Time, maxAttempts int) (InboxRecord, bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	var out InboxRecord
	claimed := false
	err := p.transact(func(ctx context.Context, tx *sql.Tx) error {
		err := scanInbox(tx.QueryRowContext(ctx, `
WITH candidate AS (
  SELECT inbox_id FROM webhook_inbox
  WHERE tenant_id=$1 AND status IN ('PENDING','FAILED') AND attempt_count < $2
  ORDER BY received_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE webhook_inbox AS w SET
 status='PROCESSING', attempt_count=w.attempt_count+1, last_error='', updated_at=$3
FROM candidate
WHERE w.tenant_id=$1 AND w.inbox_id=candidate.inbox_id
RETURNING w.inbox_id,w.tenant_id,w.integration_id,w.event_uuid,w.event_type,w.payload_digest,
 w.payload,w.status,w.attempt_count,w.last_error,w.received_at,w.updated_at,w.processed_at`,
			p.tenantID, maxAttempts, now.UTC()), &out)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err == nil {
			claimed = true
		}
		return err
	})
	return out, claimed, err
}

func (p *PostgresInbox) RequeueStale(before time.Time, maxAttempts int) (int, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	count := 0
	err := p.transact(func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
UPDATE webhook_inbox SET
 status='FAILED',
 last_error=CASE WHEN attempt_count >= $3
   THEN '处理租约超时且已达到最大尝试次数'
   ELSE '处理租约超时，等待只读对账重试' END,
 updated_at=now()
WHERE tenant_id=$1 AND status='PROCESSING' AND updated_at < $2`,
			p.tenantID, before.UTC(), maxAttempts)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		count = int(affected)
		return err
	})
	return count, err
}

type inboxScanner interface {
	Scan(...any) error
}

func scanInbox(row inboxScanner, out *InboxRecord) error {
	var payload []byte
	var processed sql.NullTime
	if err := row.Scan(&out.ID, &out.TenantID, &out.IntegrationID, &out.EventUUID, &out.EventType,
		&out.PayloadDigest, &payload, &out.Status, &out.Attempts, &out.LastError, &out.ReceivedAt,
		&out.UpdatedAt, &processed); err != nil {
		return err
	}
	out.Payload = append(json.RawMessage(nil), payload...)
	if processed.Valid {
		value := processed.Time
		out.ProcessedAt = &value
	}
	return nil
}

func mapInboxError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("Webhook Inbox 记录不存在")
	}
	return err
}
