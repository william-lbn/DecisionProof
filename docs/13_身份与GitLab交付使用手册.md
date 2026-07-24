# 身份与 GitLab 交付使用手册

版本：V2.2 Stage C Contract
日期：2026-07-24
适用范围：OIDC Bearer API、GitLab Project Onboarding、工程包交付和 Webhook Read-back Reconciler。

## 1. 真实性边界

当前代码已经完成本地签名 OIDC 集成测试、GitLab HTTP Contract Test、真实 PostgreSQL
RLS/幂等/租约测试和 Kubernetes 模板验证。当前环境没有企业 IdP 或可写的真实 GitLab
项目凭据，因此以下内容**尚不能声明真实 Provider 通过**：

- 企业 IdP Discovery、JWKS 轮换、Claim Mapping 和撤销策略；
- 真实 GitLab Project 上的 Issue、Branch、Commit、Draft MR；
- GitLab 429、写后超时、重复和乱序 Webhook 的真实 Provider 故障注入；
- GitLab 默认分支保护和 Bot 权限配置的回读证明。

在这些 Truth Test 完成前，Stage C 的状态是 `CONTRACT_VERIFIED / PROVIDER_NOT_RUN`，
不是生产完成。

## 2. 身份模式

### 2.1 隔离实验模式

仅在本机或隔离 namespace 使用：

```bash
export SDLC_AUTH_MODE=development_headers
export SDLC_ALLOW_INSECURE_DEV_AUTH=true
```

调用者自行提交：

```text
X-Tenant-ID: tenant-demo
X-Actor-ID: user-alice
X-Actor-Roles: product_manager
```

应用在 `SDLC_ENV=production` 时，若没有显式的
`SDLC_ALLOW_INSECURE_DEV_AUTH=true`，会拒绝以开发身份头启动。

### 2.2 OIDC Bearer 模式

```bash
export SDLC_ENV=production
export SDLC_AUTH_MODE=oidc_bearer
export SDLC_ALLOW_INSECURE_DEV_AUTH=false
export SDLC_OIDC_ISSUER_URL=https://id.example.com/realms/decisionproof
export SDLC_OIDC_AUDIENCE=decisionproof-api
export SDLC_OIDC_TENANT_CLAIM=tenant_id
export SDLC_OIDC_ROLES_CLAIM=roles
```

Token 必须通过 Discovery/JWKS 签名、Issuer、Audience 和有效期验证。平台会删除客户端
提供的 `X-Tenant-ID`、`X-Actor-ID`、`X-Actor-Roles`，再从验证后的 Claim 注入：

```text
X-Tenant-ID  ← tenant_id Claim
X-Actor-ID   ← oidc:<sub>
X-Actor-Roles ← roles Claim
```

调用示例：

```bash
curl -fsS http://127.0.0.1:8080/product/v1/decisions?project_id=PROJECT_ID \
  -H "Authorization: Bearer ${DECISIONPROOF_ACCESS_TOKEN}"
```

`/healthz`、`/readyz`、静态 UI 和 `/webhooks/gitlab` 不要求 Bearer；Webhook 使用独立
HMAC/Token 验证。当前 UI 没有 OIDC Authorization Code + PKCE/BFF 登录，因此 OIDC 模式
先面向 API 客户端；公网浏览器体验仍是生产 No-Go。

## 3. 角色矩阵

| 操作 | 所需角色 |
|---|---|
| 上传、提取、创建、修订、确认决策 | `product_manager` |
| 批准、否决决策 | `product_approver` |
| 生成工程包、执行 GitLab 交付 | `delivery_manager` |
| 查询产品决策 | 上述三种角色或 `auditor` |
| 接入 GitLab Project | `integration_admin` 或 `platform_admin` |

服务层仍强制“创建者不能批准自己的决策”；角色相同也不能绕过职责分离。

## 4. GitLab 部署配置

GitLab Base URL 和 Token 只能由运维配置，API 请求不能提交任意 URL 或 Token。Token
只存在于 Kubernetes Secret/进程内存，数据库仅保存 `secret_reference`。

```bash
kubectl -n decisionproof-experiment create secret generic decisionproof-gitlab-api \
  --from-literal=api-token='REPLACE_WITH_SCOPED_TOKEN'
```

建议 Token 仅有目标项目的 API 权限，且不能 Push/Merge 默认分支。Helm values：

```yaml
storage:
  mode: postgres
database:
  existingSecret: decisionproof-database-app
  migrationExistingSecret: decisionproof-database-admin
auth:
  mode: oidc_bearer
  allowInsecureDevelopmentHeaders: false
  oidc:
    issuerURL: https://id.example.com/realms/decisionproof
    audience: decisionproof-api
    tenantClaim: tenant_id
    rolesClaim: roles
    egressCIDRs: ["203.0.113.10/32"]
    port: 443
gitlab:
  tenantID: tenant-demo
  integrationID: gitlab-primary
  api:
    baseURL: https://gitlab.example.com
    existingSecret: decisionproof-gitlab-api
    tokenKey: api-token
    authMode: private_token
    secretReference: gitlab-primary
    egressCIDRs: ["198.51.100.20/32"]
    port: 443
existingGitLabWebhookSecret: decisionproof-gitlab-webhook
```

标准 Kubernetes NetworkPolicy 不能使用域名；IdP/GitLab 地址变化时必须维护稳定出口
网关或 CIDR。空 CIDR 会让出站连接 Fail-closed。

## 5. Project Onboarding

先把 DecisionProof 内部项目与 GitLab 数字 Project ID 绑定：

```bash
curl -fsS -X POST http://127.0.0.1:8080/integrations/v1/gitlab/projects \
  -H "Authorization: Bearer ${DECISIONPROOF_ACCESS_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{
    "integration_id": "gitlab-primary",
    "project_id": "project-erp",
    "external_project_id": 123
  }'
```

成功返回 `201`。平台会调用 GitLab `GET /api/v4/projects/123`，回读并保存真实
`path_with_namespace`、默认分支、可见性和验证时间；响应和数据库都不会出现 Token。

## 6. 工程就绪包交付

前置条件：

1. 工程包中的全部决策仍为 `APPROVED`；
2. 当前决策 ID + Canonical Digest 必须与工程包 Digest 完全一致；
3. 工程包 Project 必须与 Integration 绑定项目一致；
4. 调用者提供稳定、唯一、无空格的幂等键。

```bash
curl -fsS -X POST http://127.0.0.1:8080/integrations/v1/gitlab/deliveries \
  -H "Authorization: Bearer ${DECISIONPROOF_ACCESS_TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{
    "integration_id": "gitlab-primary",
    "package_id": "erp_REPLACE",
    "idempotency_key": "project-erp-erp_REPLACE-v1"
  }'
```

执行顺序固定为：

```text
Engineering Package
  → Issue（幂等 Marker）
  → Feature Branch
  → Docs-as-Code Commit
  → Draft Merge Request
```

每一步都先读、最多写一次、再读回对账。成功结果绑定 Project ID、Package Digest、
Issue IID、Branch、Commit SHA 和 MR IID。相同幂等键再次请求只回读同一结果。

如果写请求超时且 Read-back 不能证明外部结果，运行记录进入 `UNKNOWN`，API 返回 `409`；
不得直接换幂等键重试。运维必须先在 GitLab 按 Marker、Branch、文件 Digest 和 MR Head SHA
对账，再通过未来的人工 Reconciliation API 解除（该管理 API 当前尚未实现）。

## 7. Webhook 与 Reconciler

GitLab Webhook 指向：

```text
POST https://decisionproof.example.com/webhooks/gitlab
```

推荐使用 `Webhook-Signature` HMAC-SHA256；兼容旧 GitLab 时可使用
`X-Gitlab-Token`。Gateway 只校验、限流、计算 Payload Digest、按 Event UUID 去重并快速
返回 `202`。

后台 Reconciler：

- PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取事件；
- Worker 崩溃后按租约回收；
- 达到最大次数后保持 `FAILED`，不会无限重试；
- MR/Pipeline/Push 事件必须调用 GitLab API 回读 IID/SHA；
- Webhook 到达顺序不直接推进业务状态；
- 当前只确认外部事实并结算 Inbox，不会合并 MR 或触发生产发布。

## 8. 验证命令

本地质量门禁：

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
./scripts/verify.sh
```

真实 PostgreSQL Truth Test：

```bash
export DECISIONPROOF_TEST_ADMIN_DATABASE_URL='postgres://ADMIN...'
export DECISIONPROOF_TEST_APP_DATABASE_URL='postgres://APP...'
go test -count=1 -v ./internal/database ./internal/integrations/gitlab
```

真实 GitLab Truth Test 的验收证据必须至少包含：

- Disposable Project ID 和默认分支保护回读；
- 首次/重复请求的 Issue、Branch、Commit、MR 数量；
- Commit SHA、MR Head SHA、文件内容 Digest；
- 429、写后超时和重复/乱序 Webhook 故障结果；
- Token 权限证明：不能直接 Merge/Push 默认分支；
- 清理记录。

没有这些证据时不得把 Contract Test 写成 Provider Truth Test。
