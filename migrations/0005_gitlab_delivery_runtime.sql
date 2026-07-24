-- GitLab Project Onboarding 和外部交付运行记录。
-- Token 不进入数据库；secret_reference 只指向部署时注入的受控 Secret。
CREATE TABLE IF NOT EXISTS gitlab_delivery_runs (
    tenant_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    integration_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    package_digest TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL,
    issue_iid BIGINT,
    branch_name TEXT NOT NULL DEFAULT '',
    commit_sha TEXT NOT NULL DEFAULT '',
    merge_request_iid BIGINT,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_error TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, delivery_id),
    UNIQUE (tenant_id, integration_id, package_digest),
    UNIQUE (tenant_id, integration_id, idempotency_key),
    FOREIGN KEY (tenant_id, integration_id)
        REFERENCES project_integrations(tenant_id, integration_id),
    CHECK (status IN ('PENDING', 'EXECUTING', 'SUCCEEDED', 'FAILED', 'UNKNOWN'))
);
CREATE INDEX IF NOT EXISTS idx_gitlab_delivery_runs_status
    ON gitlab_delivery_runs(tenant_id, status, updated_at);

ALTER TABLE gitlab_delivery_runs ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation_gitlab_delivery_runs ON gitlab_delivery_runs
    USING (tenant_id = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id = current_setting('app.tenant_id', true));

ALTER TABLE project_integrations
    ADD CONSTRAINT project_integrations_status_check
    CHECK (status IN ('ACTIVE', 'DISABLED', 'ERROR'));
