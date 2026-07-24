-- DecisionProof V2.1：补齐 PostgreSQL 运行时所需的提取执行记录。

CREATE TABLE IF NOT EXISTS extraction_runs (
    tenant_id TEXT NOT NULL,
    extraction_run_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_digest TEXT NOT NULL DEFAULT '',
    config_digest TEXT NOT NULL DEFAULT '',
    decision_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, extraction_run_id),
    FOREIGN KEY (tenant_id, document_id)
        REFERENCES source_documents(tenant_id, document_id)
);
CREATE INDEX IF NOT EXISTS idx_extraction_runs_document
    ON extraction_runs(tenant_id, document_id, started_at DESC);

ALTER TABLE extraction_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE extraction_runs FORCE ROW LEVEL SECURITY;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_extraction_runs ON extraction_runs
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- 文件 Inbox 使用 PENDING；统一数据库默认值，避免两种运行时出现不同初始语义。
ALTER TABLE webhook_inbox ALTER COLUMN status SET DEFAULT 'PENDING';
