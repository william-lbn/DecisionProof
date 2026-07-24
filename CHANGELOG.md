# 变更记录

## V2.2.0-stage-c-contract — 2026-07-24

### 新增

- OIDC Discovery/JWKS、Issuer/Audience/有效期验签和 Claim 身份映射；
- 产品 API 与 GitLab Integration API 角色门禁，服务层继续禁止自我批准；
- GitLab Project Onboarding，Token 仅由部署 Secret 注入；
- Engineering Package 到 Issue、Branch、Commit、Draft MR 的幂等执行器；
- GitLab 外部写入 Read-back、`UNKNOWN` Fail-closed 和持久化 Delivery Run；
- PostgreSQL Webhook 原子领取、处理租约、最大尝试次数与后台 Reconciler；
- MR/Pipeline/Push Webhook 的 GitLab API SHA 回读；
- OpenAPI 0.4、Helm OIDC/GitLab 配置和受控出口 NetworkPolicy；
- 身份与 GitLab 交付使用手册、Stage C 分层验证报告。

### 安全修复

- OIDC 模式删除客户端伪造身份头，只信任验签 Claim；
- 工程包交付前重算当前批准决策 Digest，决策漂移时 Fail-closed；
- GitLab Base URL 必须是无用户信息的 HTTP(S) 绝对地址；
- 无效幂等键在持久化和外部写入前拒绝；
- IdP/JWKS 与 GitLab 出口均要求 NetworkPolicy 显式 CIDR。
- Go 工具链升级到 1.26.5，pgx 升级到 5.10.0，修复漏洞扫描发现的可达标准库、
  SQL Placeholder 和 Unicode 处理漏洞。

### 真实性限制

- 已完成本地密码学 OIDC Integration、GitLab HTTP Contract 和真实 PostgreSQL Truth Test；
- 企业 IdP 与真实 Disposable GitLab Project Truth Test 尚未执行；
- 当前 UI 尚无 OIDC Authorization Code + PKCE/BFF；
- 不具备 Temporal、供应链签名、GitOps 灰度或生产 HA。

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

### 尚未完成（该版本发布时）

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
