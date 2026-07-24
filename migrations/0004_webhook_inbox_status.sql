-- 0003 已把新事件统一为 PENDING；历史索引仍只覆盖 RECEIVED/RETRY，
-- 因此必须用新迁移修复，不能修改已经登记摘要的旧迁移。
UPDATE webhook_inbox SET status = 'PENDING' WHERE status = 'RECEIVED';
UPDATE webhook_inbox SET status = 'FAILED' WHERE status = 'RETRY';

DROP INDEX IF EXISTS idx_webhook_inbox_pending;
CREATE INDEX idx_webhook_inbox_pending
    ON webhook_inbox(received_at)
    WHERE status IN ('PENDING', 'FAILED');

ALTER TABLE webhook_inbox
    ADD CONSTRAINT webhook_inbox_status_check
    CHECK (status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'FAILED'));
