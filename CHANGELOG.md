# 变更记录

## V2.1.0-postgres-experimental — 2026-07-24

### 新增

- pgx PostgreSQL 运行时与管理员/应用账号分离；
- 摘要校验 Migration CLI 和迁移 Init Container；
- 核心、产品决策和 Webhook Inbox PostgreSQL Repository；
- RLS、跨租户、幂等迁移和 Webhook 去重 Truth Test；
- `/readyz` 数据库故障 Fail-closed；
- Docker Desktop Kubernetes PostgreSQL 实验部署和完整操作手册。
- GitHub Actions、Dependabot、贡献指南、行为准则和协作模板；
- Go Module 迁移到 `github.com/william-lbn/DecisionProof`；
- Webhook Inbox 状态约束和 `PENDING/FAILED` 待处理索引迁移。

### 修复

- Kubernetes 无法验证 distroless 字符串用户的问题，固定 UID/GID 65532；
- 产品提取结果改为原子保存，避免部分决策提交；
- 上传解析/注册失败时清理孤儿临时对象；
- Docker 构建基础镜像绑定 Registry Digest；
- PostgreSQL 模式 NetworkPolicy 显式允许同 namespace 5432。

### 尚未完成

- GitLab OAuth、Project Onboarding 和 Webhook Reconciler；
- Temporal、Kubernetes Agent、CI 供应链和 GitOps/Argo；
- 外部对象存储、KMS、数据库 HA/备份恢复和生产身份。

## V2.0.0-engineering-foundation — 2026-07-24

### 新增

- 产品资料输入与 DOCX/PPTX/Markdown/TXT 原生解析；
- 来源锚点、AI 提取、人工确认和独立批准；
- 工程就绪包及 GitLab Issue/Docs-as-Code Bundle；
- 中文产品工作台；
- GitLab REST Client、Webhook HMAC、Inbox 幂等；
- 通用 HTTP JSON AI Provider；
- Agent CLI Task/Event/Result 协议；
- Evidence V2 Subject Binding；
- 软件 Release Qualifier、Outcome Contract 和 Agent Release Manifest；
- PostgreSQL V2 领域 DDL；
- GitLab CI、Helm 单节点验证 Chart；
- 完整中文产品、架构、研发和验证文档。

### 安全修复

- Agent CLI 并发读取 stdout/stderr，超时时终止整个进程组；
- File Repository 和 Webhook Inbox 磁盘提交失败时回滚内存状态；
- 发布清单增加签名密钥标识，并使用清单公钥验证后再校验本地信任集合。

### 尚未完成

- Temporal Workflow；
- GitLab OAuth 和真实 Provider E2E；
- PostgreSQL 默认运行时；
- Kubernetes Agent Scheduler；
- CI Evidence Ingest、Cosign/SBOM/Provenance 真实执行；
- Argo CD/Rollouts Adapter；
- OIDC/KMS/HA/DR。
