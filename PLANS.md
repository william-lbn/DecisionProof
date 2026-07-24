# DecisionProof 可执行研发计划

当前权威计划见 `docs/06_研发计划_Codex任务.md`。近期顺序固定如下：

1. PostgreSQL 运行时、Migration CLI、Readiness 和真实集成测试（已完成单节点实验验收）；
2. OIDC、Project Onboarding、幂等 GitLab Delivery 和 Webhook Reconciler（Contract 与 PostgreSQL Truth 已完成；真实 IdP/GitLab Provider Truth 待执行）；
3. Temporal Change/RepositoryChange/Release Workflow（Stage C Provider Truth 为 Go/No-Go 前置条件）；
4. Kubernetes Agent Scheduler 与 Codex/OpenHands Provider Truth Test；
5. GitLab CI Evidence Ingest、SBOM、Cosign 和 Provenance；
6. Merge Qualifier 和 Bot Merge；
7. GitOps、Argo CD/Rollouts、Prometheus 与 Outcome；
8. HA、KMS、备份恢复、Chaos 和 7×24。

任何阶段均不得跳过对应 Go/No-Go 验收门槛。
