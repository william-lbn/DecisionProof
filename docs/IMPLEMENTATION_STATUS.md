# 实现状态与真实性声明

## 已实现并在当前环境执行验证

- P0 状态机、证据哈希链、Action UNKNOWN、Release Manifest 原型；
- DOCX/PPTX/Markdown/TXT 文本解析；
- 来源锚点；
- 产品决策状态、人工创建/修订/否决、确认与批准职责分离；
- 工程就绪包和 GitLab Issue/Docs-as-Code Bundle；
- 规则提取器和通用 HTTP JSON AI 接口；
- GitLab REST Client 基础操作；
- GitLab Webhook HMAC/Legacy Token、File/PostgreSQL Inbox、Event UUID/Digest 去重；
- Prometheus Instant Query；
- Agent CLI Task/Event/Result 协议；
- Evidence V2 数据结构；
- Subject-bound Qualifier；
- Agent Release Manifest 和确定性 Agent 评估门；
- Outcome Evaluator；
- 产品工作台；
- PostgreSQL V2 DDL；
- PostgreSQL Migration CLI、核心与产品运行时 Repository；
- PostgreSQL Webhook Inbox、RLS 租户隔离和 `/readyz`；
- Webhook Inbox 状态约束与 `PENDING/FAILED` 待处理索引；
- OIDC Bearer Discovery/JWKS、签名/Issuer/Audience/有效期验证和可信 Claim 映射；
- 产品 API 和 GitLab Integration API 的角色门禁；
- GitLab Project Onboarding 与 Token Secret Reference；
- Engineering Package → Issue → Branch → Commit → Draft MR 幂等执行和 Read-back；
- GitLab Delivery Run PostgreSQL 持久化、`UNKNOWN` 禁止盲目重试；
- Webhook Inbox 原子领取、处理租约、最大尝试次数和后台 Read-back Reconciler；
- MR/Pipeline/Push Webhook 的 GitLab API SHA Contract 验证；
- Helm PostgreSQL 模式与迁移 Init Container；
- Docker Desktop Kubernetes PostgreSQL 实验部署、Pod 重启持久化和数据库故障 Readiness；
- Stage C 精确提交镜像的 Docker Desktop Kubernetes RBAC、产品主链、Webhook 去重和 Pod 重建持久化 E2E；
- Go Test、Race、Vet、Build。

## 已提供代码或配置，但未连接真实外部系统验证

- HTTP JSON AI Provider；
- OIDC Adapter（已通过本地签名 Discovery/JWKS 集成测试，未连接企业 IdP）；
- GitLab API Adapter、Onboarding、Delivery Executor 和 Reconciler（已通过 HTTP Contract，
  未连接真实 GitLab Project）；
- Prometheus Adapter；
- GitLab Client、Kubernetes Agent 协议、Argo 和供应链仍只有基础代码、协议或参考配置。

## 仅有架构和任务设计，尚未实现

- Temporal SDK Workflow；
- K8s Agent Scheduler；
- 真实 Codex/OpenHands Runner；
- CI Evidence Ingest；
- Cosign/Syft/Provenance 执行；
- GitOps/Argo Adapter；
- OIDC 浏览器 Authorization Code + PKCE/BFF、KMS、HA/DR。

本文件是验收边界。不得把后两类内容宣称为已经生产完成。

## P0 兼容层的已知限制

- 旧 `ReleaseService` 仍按一条 Change 只保存一份 Manifest，不能代表 V2 的多环境、多次发布和回滚后重发；V2 领域设计已经定义 `Release Attempt`，但尚未完成持久化和运行时替换。
- 旧 P0 生命周期仍使用单一 `Change.State`；真实产品必须由 Change 高层阶段与 RepositoryChange、VerificationRun、Release、Deployment 等子聚合共同表达。
- 发布清单已经增加 `SigningKeyID`，并先按清单公钥验证签名，再校验当前本地信任集合；生产仍需 KMS/Sigstore 信任根、密钥轮换和历史密钥注册表。
- Subject-bound `Qualifier` 已实现并有测试，但旧 P0 `ReleaseService` 尚未全部切换到 V2 Release 聚合；因此 P0 清单只能用于控制语义演示，不能作为生产授权对象。
- OIDC 当前只支持 Bearer API。静态工作台没有安全的浏览器登录/会话，不得以开发身份头暴露公网。
- `UNKNOWN` Delivery 已持久化并阻止重复写，但人工 Reconciliation 管理 API 尚未实现，当前需数据库只读审计和 GitLab 人工对账。

## 已清理内容

- 删除与 Helm Chart 重复的旧单 Pod Kubernetes 清单；
- 将 Argo Rollouts 文件移动为明确的 `deploy/examples` 示例；
- 一期主链统一为 GitLab CI/Registry，Tekton、Harbor、Backstage、GUAC 等保留为后续 Adapter，不混入当前可执行路径；
- 增加 `.gitignore`、`SECURITY.md` 和 `CHANGELOG.md`。
