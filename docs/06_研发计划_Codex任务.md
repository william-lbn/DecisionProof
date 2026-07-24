# 研发计划与 Codex 任务

## 1. 执行原则

Codex 每次只实现一个可独立验收任务。禁止一次性重写整个项目、用 Mock 冒充真实 Provider、删除安全门禁或只生成代码不执行测试。

## 2. 里程碑

### M0 V2 基线（本包已部分完成）

产品文档解析、决策模型、GitLab/Prometheus Client、Agent 协议、Evidence V2、产品工作台、测试和文档。

### M1 PostgreSQL 与身份

- 加入 pgx（已完成）；
- 产品 Repository PostgreSQL 实现（已完成）；
- Migration CLI（已完成）；
- PostgreSQL Webhook Inbox、RLS Truth Test 和 Readiness（已完成）；
- GitLab OAuth/OIDC；
- Session、CSRF、RBAC；

### M2 GitLab Truth Harness

- Project Onboarding；
- Webhook Inbox/Reconciler；
- Disposable Project；
- Branch/MR/Pipeline/Job/Registry；
- 429、超时、乱序、丢失验证。

### M3 Temporal

- ChangeWorkflow；
- RepositoryChangeWorkflow；
- ReleaseWorkflow；
- Signal、Query、Activity；
- Versioning；
- Worker Kill Test。

### M4 Agent Runtime

- K8s Job Scheduler；
- Codex CLI Provider；
- OpenHands Remote Provider；
- 临时 Git Token；
- Workspace、日志、取消和预算；
- Sandbox Security Test。

### M5 CI 与供应链

- GitLab CI Template；
- Test/Security/SBOM/Build；
- Cosign/Provenance；
- Evidence Ingest；
- Merge Qualifier 和 Bot Merge。

### M6 Release 与 Outcome

- 多 Release Attempt；
- GitOps MR；
- Argo CD；
- Staging；
- Rollouts；
- Prometheus；
- Rollback；
- Outcome Evaluator。

### M7 生产化

- HA；
- KMS/OpenBao；
- OPA；
- Backup/Restore；
- Load/Chaos/Security；
- 7×24；
- Upgrade/Rollback。

## 3. 每个 Codex 任务 DoD

- 中文设计说明；
- 代码和中文注释；
- 数据库迁移和 OpenAPI；
- 单元、集成、并发、故障路径测试；
- gofmt、go test、Race、go vet；
- 真实输出；
- 已知限制；
- 不把未运行的外部集成标记为通过。
