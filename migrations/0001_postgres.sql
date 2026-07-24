-- SDLCProof PostgreSQL 初始架构。
-- 所有核心表都以 tenant_id 作为联合主键的一部分，避免跨租户对象碰撞。

CREATE TABLE IF NOT EXISTS changes (
    tenant_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    risk_level TEXT NOT NULL,
    state TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    spec_digest TEXT NOT NULL DEFAULT '',
    architecture_digest TEXT NOT NULL DEFAULT '',
    patch_digest TEXT NOT NULL DEFAULT '',
    artifact_digest TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, change_id)
);

CREATE TABLE IF NOT EXISTS evidence_nodes (
    tenant_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    verdict TEXT NOT NULL,
    producer TEXT NOT NULL,
    trust_class TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL,
    payload_digest TEXT NOT NULL,
    prev_hash TEXT NOT NULL DEFAULT '',
    record_hash TEXT NOT NULL,
    sequence BIGINT NOT NULL CHECK (sequence > 0),
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, evidence_id),
    UNIQUE (tenant_id, change_id, sequence),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);
CREATE INDEX IF NOT EXISTS idx_evidence_change_type
    ON evidence_nodes(tenant_id, change_id, evidence_type, sequence DESC);

CREATE TABLE IF NOT EXISTS evidence_edges (
    tenant_id TEXT NOT NULL,
    edge_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    from_node_id TEXT NOT NULL,
    relation TEXT NOT NULL,
    to_node_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, edge_id),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);

CREATE TABLE IF NOT EXISTS transition_records (
    tenant_id TEXT NOT NULL,
    transition_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    actor TEXT NOT NULL,
    actor_roles JSONB NOT NULL,
    idempotency_key TEXT NOT NULL,
    policy_digest TEXT NOT NULL,
    evidence_root TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, transition_id),
    UNIQUE (tenant_id, change_id, idempotency_key),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);

CREATE TABLE IF NOT EXISTS action_records (
    tenant_id TEXT NOT NULL,
    action_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    proposal JSONB NOT NULL,
    status TEXT NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    external_ref TEXT NOT NULL DEFAULT '',
    receipt JSONB,
    error_message TEXT NOT NULL DEFAULT '',
    policy_digest TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    last_checked_at TIMESTAMPTZ,
    settlement_note TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, action_id),
    UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS release_manifests (
    tenant_id TEXT NOT NULL,
    manifest_id TEXT NOT NULL,
    change_id TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    manifest JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (tenant_id, manifest_id),
    UNIQUE (tenant_id, change_id),
    FOREIGN KEY (tenant_id, change_id) REFERENCES changes(tenant_id, change_id)
);

-- 事务性 Outbox 用于把数据库内事实可靠发布为 CloudEvents/CDEvents。
CREATE TABLE IF NOT EXISTS outbox_events (
    tenant_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, event_id)
);
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON outbox_events(created_at) WHERE published_at IS NULL;

-- RLS 需要由连接池在每个事务开始时设置：SET LOCAL app.tenant_id = '...';
ALTER TABLE changes ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_nodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE evidence_edges ENABLE ROW LEVEL SECURITY;
ALTER TABLE transition_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE action_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE release_manifests ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;

ALTER TABLE changes FORCE ROW LEVEL SECURITY;
ALTER TABLE evidence_nodes FORCE ROW LEVEL SECURITY;
ALTER TABLE evidence_edges FORCE ROW LEVEL SECURITY;
ALTER TABLE transition_records FORCE ROW LEVEL SECURITY;
ALTER TABLE action_records FORCE ROW LEVEL SECURITY;
ALTER TABLE release_manifests FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;

DO $$ BEGIN
    CREATE POLICY tenant_isolation_changes ON changes
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_evidence_nodes ON evidence_nodes
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_evidence_edges ON evidence_edges
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_transitions ON transition_records
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_actions ON action_records
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
    CREATE POLICY tenant_isolation_manifests ON release_manifests
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    CREATE POLICY tenant_isolation_outbox ON outbox_events
        USING (tenant_id = current_setting('app.tenant_id', true))
        WITH CHECK (tenant_id = current_setting('app.tenant_id', true));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
