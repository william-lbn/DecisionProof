-- Webhook Reconciler 需要处理租约时间，才能在 Worker 崩溃后安全回收只读对账任务。
ALTER TABLE webhook_inbox
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE webhook_inbox SET updated_at = received_at WHERE updated_at IS NULL;

DROP INDEX IF EXISTS idx_webhook_inbox_pending;
CREATE INDEX idx_webhook_inbox_pending
    ON webhook_inbox(tenant_id, status, updated_at, received_at)
    WHERE status IN ('PENDING', 'FAILED');
