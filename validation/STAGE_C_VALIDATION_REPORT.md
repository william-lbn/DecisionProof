# Stage C 身份与 GitLab 交付验证报告

日期：2026-07-24
结论：`CONTRACT_VERIFIED / POSTGRES_TRUTH_VERIFIED / PROVIDER_NOT_RUN`
生产判定：`NO-GO`

## 1. 本阶段范围

- OIDC Bearer Discovery/JWKS、Issuer/Audience/有效期和 Claim 映射；
- 产品与 GitLab Integration API RBAC；
- GitLab Project Onboarding；
- Engineering Package → Issue → Branch → Commit → Draft MR；
- 外部写入幂等键、Read-back 和 `UNKNOWN`；
- PostgreSQL Delivery Run、Webhook 原子领取、租约回收和 Reconciler；
- OpenAPI、Migration、Helm 和 NetworkPolicy 同步。

## 2. 已真实执行

### 2.1 本地单元与集成

执行：

```bash
gofmt -w .
go vet ./...
go test ./...
```

结果：通过。OIDC 集成测试启动一次性本地 Discovery/JWKS 服务并使用 RSA 签名 JWT，
验证合法 Token、伪造身份头覆盖、Issuer/Audience/过期和跨站修改拒绝。它证明本地
密码学路径，不代表企业 IdP Truth。

### 2.2 GitLab HTTP Contract

`TestRuntimeOnboardAndDeliveryContract` 使用可计数 HTTP Contract Server 执行：

- Project API 回读；
- Issue、Branch、Commit、Draft MR；
- 同一幂等键重复交付；
- 每种外部写副作用只有一次；
- 文件内容和 Commit/MR Head SHA 回读；
- Commit 写入后超时可对账；
- 无法对账时进入 `UNKNOWN`，不盲目重试。

结果：通过。此测试不是 GitLab Provider Truth。

### 2.3 真实 PostgreSQL

目标：Docker Desktop Kubernetes namespace `decisionproof-experiment` 中的 PostgreSQL，
通过管理员和非超级应用账号分别连接。

执行：

```bash
DECISIONPROOF_TEST_ADMIN_DATABASE_URL=REDACTED \
DECISIONPROOF_TEST_APP_DATABASE_URL=REDACTED \
go test -count=1 -v ./internal/database ./internal/integrations/gitlab
```

通过：

- `TestPostgresTruth`；
- `TestPostgresInboxTruth`；
- `TestPostgresRuntimeTruth`。

已证明迁移 0005/0006、跨租户 RLS、Delivery 幂等回读、`UNKNOWN` 持久化、Webhook
`FOR UPDATE SKIP LOCKED` 领取、租约回收及最大尝试次数。

## 3. 质量与部署门禁

使用 Go 1.26.5 重新执行：

```bash
GOTOOLCHAIN=go1.26.5+auto ./scripts/verify.sh
```

结果：Vet、全量测试、Race、四个二进制、双演示、文档、JSON/YAML 和 Helm lint 全部
通过；总语句覆盖率为 **54.0%**，仍低于生产候选 75% 门槛。

OIDC/GitLab values 已完成 Helm template、`kubectl --dry-run=client` 和 Kubernetes API
Server dry-run。第一次 Server dry-run 因模板默认 10 GiB 与现有 1 GiB PVC 不一致而被
集群正确拒绝；改为现有 PVC 的 1 GiB 后通过，期间没有修改资源。

精确镜像和 Kubernetes E2E 结果如下；GitHub 分支推送与 CI Read-back 在报告提交后执行。

### 3.1 精确提交镜像与 Kubernetes

最终运行时提交：

```text
50b2d5acf978663dd1af97bcf9d87cb11af683e3
```

本地不可变 Image ID：

```text
sha256:faf9c976191d51994102ee64136c1d464b3d5afdc94c9c6b0014aef550bb24e7
```

镜像 OCI Label 回读：

```text
org.opencontainers.image.revision=50b2d5acf978663dd1af97bcf9d87cb11af683e3
org.opencontainers.image.version=2.2.0-stage-c-contract
```

Docker Desktop Kubernetes 最终 Helm revision 10，Chart/App 0.4.0。Migration Init
Container exit 0，API Pod Ready，两个容器状态均回读到上述同一 Image ID。

### 3.2 Kubernetes API E2E

在显式 `development_headers` 隔离模式执行：

- `/healthz` 为 `ok`，`/readyz` 为 `ready`；
- 无身份查询返回 401，无所需角色返回 403；
- 跨站修改请求返回 `CSRF_REJECTED`；
- 上传 Markdown，提取 5 条决策；
- 由产品经理确认、不同的批准人批准全部 5 条决策；
- 同一身份自我批准返回 409；
- 生成工程包 `erp_7d738f4916cfd93ebbd5b2db979001ae`；
- 工程包 Digest 为
  `sha256:a7a151ea26390ca9eb3e29b7c68cf0c421b0a6296f3eca92addb5749cc5abbdc`；
- Bundle 生成固定分支和 2 个 Docs-as-Code 文件；
- Webhook 首次写入 `duplicate=false`，相同 UUID 再投递为 `duplicate=true`，Record ID
  和 Payload Digest 完全相同；
- Pod 重建后同一工程包仍可读取，Webhook 仍命中同一去重记录；
- 最终 0.4.0 镜像再次通过健康、就绪和持久化工程包 Smoke Test。

这是 Docker Desktop 单节点 Kubernetes 实验，不是企业 OIDC 或真实 GitLab Provider Truth。

## 4. 依赖与漏洞扫描

首次使用 `govulncheck v1.1.4` 扫描 Go 1.24.0/pgx 5.7.6 基线时发现 30 个可达漏洞，
因此该工具链被判定为阻断并撤销交付资格。随后升级到：

- Go 1.26.5；
- `github.com/jackc/pgx/v5` 5.10.0；
- `github.com/coreos/go-oidc/v3` 3.20.0；
- `golang.org/x/text` 0.40.0。

升级后的全量测试已按上一节重新执行，漏洞复扫如下。

升级后执行：

```bash
GOTOOLCHAIN=go1.26.5+auto \
  go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
```

结果：`No vulnerabilities found.`

Gitleaks 8.30.1 扫描约 975 KB 可交付源码，结果 `no leaks found`；`bin/` 中四个大于
2 MiB 的本地构建产物被跳过，且本来就由 `.gitignore` 排除，不进入提交。

## 5. 未执行

### 5.1 企业 OIDC Provider Truth

未提供企业 IdP Issuer、Audience、Claim Mapping 和测试账号，未执行 Discovery/JWKS
轮换、撤销、时钟偏移和真实登录验证。

### 5.2 Disposable GitLab Provider Truth

当前环境没有可写的 GitLab Disposable Project/API Token，未执行：

- 真实 Issue/Branch/Commit/Draft MR；
- 默认分支保护与 Token 最小权限回读；
- 真实 429、写后超时、重复/乱序/丢失 Webhook；
- Registry、Pipeline、Job 和 Artifact；
- Provider 资源清理。

因此 `PROVIDER_NOT_RUN`，不得把 HTTP Contract 或本地 Kubernetes 验证描述为真实
GitLab 通过。

本机 Docker Desktop 只有约 9.2 GiB 总内存，且已运行 42 个容器；主机卷当时约有
27 GiB 可用空间。GitLab 官方单节点基线为 16 GiB，受限环境也要求至少 8 GiB 内存，
应用节点最低存储约 40 GiB。因此本轮没有强行启动 Omnibus GitLab CE，以免破坏现有
Kubernetes 和其他用户工作负载。依据：

- <https://docs.gitlab.com/install/requirements/>
- <https://docs.gitlab.com/omnibus/settings/memory_constrained_envs/>

## 6. 已知限制

- UI 没有 OIDC Authorization Code + PKCE/BFF，只能在隔离开发身份模式使用；
- `UNKNOWN` 已 Fail-closed，但人工 Reconciliation 管理 API 尚未实现；
- Webhook Reconciler 当前只确认外部事实并结算 Inbox，不推进 Temporal Workflow；
- GitLab Token 是单个部署 Secret，尚无 Vault/KMS 动态短期凭据；
- 单节点 PVC 对象存储、PostgreSQL 和 Chart 不满足生产 HA/DR；
- Stage D 至 G 尚未完成。
