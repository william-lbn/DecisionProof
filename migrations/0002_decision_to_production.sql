-- DecisionProof V2：产品决策、GitLab 集成、证据 V2 与多次发布模型。

ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT 'evidence.v2';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS producer_identity TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS subject_type TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS subject_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS subject_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS source_system TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS source_uri TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS supersedes_evidence_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ;
ALTER TABLE evidence_nodes ADD COLUMN IF NOT EXISTS invalidated_at TIMESTAMPTZ;
UPDATE evidence_nodes SET subject_digest = subject WHERE subject_digest = '' AND subject <> '';
UPDATE evidence_nodes SET valid_from = created_at WHERE valid_from IS NULL;
CREATE INDEX IF NOT EXISTS idx_evidence_subject
    ON evidence_nodes(tenant_id, change_id, evidence_type, subject_digest, sequence DESC)
    WHERE invalidated_at IS NULL;

CREATE TABLE IF NOT EXISTS source_documents (
    tenant_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    name TEXT NOT NULL,
    media_type TEXT NOT NULL,
    storage_uri TEXT NOT NULL,
    digest TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL,
    block_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    uploaded_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    processed_at TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, document_id),
    UNIQUE (tenant_id, project_id, digest)
);

CREATE TABLE IF NOT EXISTS source_blocks (
    tenant_id TEXT NOT NULL,
    block_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    document_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    locator TEXT NOT NULL,
    text_content TEXT NOT NULL,
    digest TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, block_id),
    UNIQUE (tenant_id, document_id, sequence),
    FOREIGN KEY (tenant_id, document_id) REFERENCES source_documents(tenant_id, document_id)
);

CREATE TABLE IF NOT EXISTS product_decisions (
    tenant_id TEXT NOT NULL,
    decision_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    anchors JSONB NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    confirmed_by TEXT NOT NULL DEFAULT '',
    approved_by TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version > 0),
    canonical_digest TEXT NOT NULL,
    supersedes_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    confirmed_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, decision_id)
);
CREATE INDEX IF NOT EXISTS idx_product_decisions_project_status
    ON product_decisions(tenant_id, project_id, status, kind);

CREATE TABLE IF NOT EXISTS engineering_readiness_packages (
    tenant_id TEXT NOT NULL,
    package_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    digest TEXT NOT NULL,
    package JSONB NOT NULL,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, package_id),
    UNIQUE (tenant_id, project_id, digest)
);

CREATE TABLE IF NOT EXISTS project_integrations (
    tenant_id TEXT NOT NULL,
    integration_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    external_project_id TEXT NOT NULL,
    base_url TEXT NOT NULL,
    configuration JSONB NOT NULL,
    secret_reference TEXT NOT NULL,
    status TEXT NOT NULL,
    last_verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, integration_id),
    UNIQUE (tenant_id, provider, external_project_id)
);

CREATE TABLE IF NOT EXISTS webhook_inbox (
    tenant_id TEXT NOT NULL,
    inbox_id TEXT NOT NULL,
    integration_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    event_uuid TEXT NOT NULL,
    webhook_uuid TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'RECEIVED',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, inbox_id),
    UNIQUE (tenant_id, integration_id, event_uuid)
);
CREATE INDEX IF NOT EXISTS idx_webhook_inbox_pending
    ON webhook_inbox(received_at) WHERE status IN ('RECEIVED','RETRY');

CREATE TABLE IF NOT EXISTS repository_changes (
    tenant_id TEXT NOT NULL,
    repository_change_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    external_project_id TEXT NOT NULL,
    base_sha TEXT NOT NULL,
    branch_name TEXT NOT NULL,
    head_sha TEXT NOT NULL DEFAULT '',
    merge_request_iid BIGINT,
    patch_digest TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, repository_change_id),
    UNIQUE (tenant_id, external_project_id, branch_name),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);

CREATE TABLE IF NOT EXISTS agent_runs (
    tenant_id TEXT NOT NULL,
    agent_run_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    repository_change_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    role TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    status TEXT NOT NULL,
    output_commit_sha TEXT NOT NULL DEFAULT '',
    patch_digest TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    failure_reason TEXT NOT NULL DEFAULT '',
    usage JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id, agent_run_id)
);

CREATE TABLE IF NOT EXISTS releases (
    tenant_id TEXT NOT NULL,
    release_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    commit_sha TEXT NOT NULL,
    artifact_digest TEXT NOT NULL,
    target_environment TEXT NOT NULL,
    status TEXT NOT NULL,
    manifest_digest TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, release_id),
    UNIQUE (tenant_id, change_id, attempt),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);

CREATE TABLE IF NOT EXISTS deployments (
    tenant_id TEXT NOT NULL,
    deployment_id TEXT NOT NULL,
    release_id TEXT NOT NULL,
    cluster_id TEXT NOT NULL,
    namespace TEXT NOT NULL,
    workload TEXT NOT NULL,
    artifact_digest TEXT NOT NULL,
    external_uid TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    PRIMARY KEY (tenant_id, deployment_id),
    FOREIGN KEY (tenant_id, release_id) REFERENCES releases(tenant_id, release_id)
);

CREATE TABLE IF NOT EXISTS outcome_contracts (
    tenant_id TEXT NOT NULL,
    contract_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    contract JSONB NOT NULL,
    digest TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, contract_id),
    UNIQUE (tenant_id, change_id, digest)
);

-- 新表统一启用并强制 RLS。
DO $$
DECLARE t TEXT;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'source_documents','source_blocks','product_decisions','engineering_readiness_packages',
    'project_integrations','webhook_inbox','repository_changes','agent_runs','releases',
    'deployments','outcome_contracts'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
    BEGIN
      EXECUTE format(
        'CREATE POLICY %I ON %I USING (tenant_id = current_setting(''app.tenant_id'', true)) WITH CHECK (tenant_id = current_setting(''app.tenant_id'', true))',
        'tenant_isolation_' || t, t);
    EXCEPTION WHEN duplicate_object THEN NULL;
    END;
  END LOOP;
END $$;

-- 产品决策的人机来源和否决审计字段；使用 IF NOT EXISTS 兼容早期 V2 数据库。
ALTER TABLE product_decisions ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'IMPORTED';
ALTER TABLE product_decisions ADD COLUMN IF NOT EXISTS rejected_by TEXT NOT NULL DEFAULT '';
ALTER TABLE product_decisions ADD COLUMN IF NOT EXISTS rejection_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE product_decisions ADD COLUMN IF NOT EXISTS rejected_at TIMESTAMPTZ;
