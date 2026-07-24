# DecisionProof PostgreSQL / Kubernetes 实验真实性报告

日期：2026-07-24
Kubernetes Context：`docker-desktop`
Kubernetes Server：v1.30.5
Helm：v3.9.0

## 1. 结论

本轮已真实完成单节点 PostgreSQL Kubernetes 实验部署，不再只是 Helm Render 或 Mock：

- DecisionProof API 和 PostgreSQL Pod 实际运行；
- Migration Init Container 实际执行 0001、0002、0003；
- 0004 已通过本地 Migration CLI 对集群 PostgreSQL 实际执行并回读索引、状态约束与版本记录；
- API 使用非超级用户 `decisionproof_app`；
- 产品资料 → 来源块 → 决策 → 独立批准 → 工程包 → GitLab Bundle 实际通过；
- 核心 Change PostgreSQL Store 实际创建和读取；
- 跨租户读取返回 404；
- 非超级用户未设置租户上下文时 RLS 查询返回 0 行；
- API Pod 重建后产品决策仍存在；
- PostgreSQL 缩容到 0 时 `/readyz` 返回 503，恢复后返回 200；
- GitLab HMAC Webhook 首次写入和重复去重实际通过；
- API Pod 重建后重复 Webhook 仍返回同一 Inbox ID；
- DecisionProof 工作负载使用 UID/GID 65532、无 ServiceAccount Token；
- PostgreSQL 与应用镜像均以真实 Digest/Image ID 绑定。

这证明 M1 单节点实验底座可用，不证明 GitLab、Temporal、Agent、供应链、Argo 或生产高可用完成。

## 2. 实际部署身份

DecisionProof 本地不可变 Image ID：

```text
sha256:c213ba3bc6cf99b4ffc3e3a302843e17df638e80e69704a500d35c9265f78ec3
```

Pod 实际解析：

```text
docker://sha256:c213ba3bc6cf99b4ffc3e3a302843e17df638e80e69704a500d35c9265f78ec3
```

PostgreSQL Registry Digest：

```text
postgres@sha256:33f923b05f64ca54ac4401c01126a6b92afe839a0aa0a52bc5aeb5cc958e5f20
```

## 3. Kubernetes 产品主链结果

文件模式首次实验：

```json
{
  "health": "ok",
  "source_blocks": 5,
  "decisions": 5,
  "approved": 5,
  "bundle_files": 2
}
```

PostgreSQL 模式实验：

```json
{
  "ready": "ready",
  "cross_tenant_http": 404,
  "blocks": 5,
  "decisions": 5,
  "package_digest": "sha256:8518f2da9af3cf8ab8dd48f6f3e36d359170e7f56482caf8aa39be4420e05205",
  "bundle_files": 2
}
```

最终镜像回归：

```json
{
  "ready": "ready",
  "image": "sha256:c213ba3bc6cf99b4ffc3e3a302843e17df638e80e69704a500d35c9265f78ec3",
  "blocks": 5,
  "decisions": 5,
  "package_digest": "sha256:b9511c54dec807901cb3549a0ef1891d742f1aa1a1c8c912b769f4ddb168fe4f",
  "bundle_files": 2
}
```

API Pod 重建后：

```json
{"after_api_restart":5,"approved":5}
```

## 4. RLS Truth Test

最初用 `POSTGRES_USER` 超级用户查询会绕过 RLS，因此该结果被判定为无效，没有记作通过。
随后创建 `NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT` 应用角色并重新验证：

```text
without_tenant_changes=0
without_tenant_decisions=0
tenant_changes=1
tenant_decisions=5
```

真实集成测试：

```text
TestPostgresTruth — PASS
TestPostgresInboxTruth — PASS
```

## 4.1 Webhook 状态迁移回读

0004 实际执行后的数据库版本为：

```text
1|0001_postgres.sql
2|0002_decision_to_production.sql
3|0003_postgres_runtime.sql
4|0004_webhook_inbox_status.sql
```

数据库回读确认待处理索引只覆盖 `PENDING/FAILED`，约束只允许
`PENDING/PROCESSING/PROCESSED/FAILED`。历史 `RECEIVED/RETRY` 会在迁移中分别归一为
`PENDING/FAILED`。

## 5. Readiness 故障验证

执行：

```text
StatefulSet replicas 1 → 0
GET /readyz → HTTP 503 {"status":"not_ready"}
StatefulSet replicas 0 → 1
GET /readyz → HTTP 200 {"status":"ready"}
```

数据库故障期间没有把进程存活误报为依赖就绪。

## 6. Webhook PostgreSQL Inbox

无效 HMAC：

```text
HTTP 401
```

有效事件首次投递：

```json
{
  "accepted": true,
  "duplicate": false,
  "inbox_record_id": "WH-92bb6d3f13535e5d7aef5472",
  "payload_digest": "sha256:9a382c620deef189b0accdae8e0b48ac1acb7c0b1cef781d41b71fee08738919"
}
```

重复投递和 API Pod 重建后再次投递：

```json
{
  "accepted": true,
  "duplicate": true,
  "inbox_record_id": "WH-92bb6d3f13535e5d7aef5472"
}
```

数据库对账：

```text
postgres_inbox_rows=1
```

## 7. 安全上下文

DecisionProof Pod：

```text
runAsUser=65532
runAsGroup=65532
runAsNonRoot=true
automountServiceAccountToken=false
allowPrivilegeEscalation=false
readOnlyRootFilesystem=true
capabilities.drop=ALL
seccompProfile=RuntimeDefault
```

PostgreSQL 管理员连接串只供迁移 Init Container 使用；API 使用独立非超级用户 Secret。

## 8. 未执行或不能声称通过

- 真实 GitLab Project、Branch、Commit、MR、Pipeline、Job、Registry；
- GitLab OAuth/OIDC、Session、CSRF 和生产 RBAC；
- Temporal Worker Kill/Recovery；
- Kubernetes Codex/OpenHands Agent Job；
- Syft、Cosign、Trivy 和 Provenance；
- GitOps、Argo CD、Argo Rollouts 和真实 Canary；
- PostgreSQL TLS、备份/PITR、复制、自动故障转移；
- 第二集群、跨地域、7×24 和生产容量。
