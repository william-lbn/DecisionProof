# GitLab 一期集成

## 1. GitLab 的位置

GitLab 负责工程事实：Issue、Branch、Commit、MR、Pipeline、Job、Artifact 和 Registry。产品立项、原始文档、商业决策和正式审批由 DecisionProof 管理，并向 GitLab 同步工程就绪内容。

## 2. Webhook

Webhook Gateway：验证 HMAC/Legacy Token、限制 Body、生成 Digest、持久化 Inbox、快速返回 202、后台 API Read-back、Signal Workflow。

Webhook 重复和乱序是正常情况，不得直接按到达顺序推进状态。

当前实现使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取、处理租约和最大尝试次数。
Reconciler 对 MR、Pipeline 和 Push 事件回读 GitLab API 的 IID/SHA；只结算 Inbox，
不因 Webhook 到达直接合并或发布。

## 3. API 可靠性

- GET 在 429/5xx 时有限退避重试；
- POST/PUT 不自动重试；
- Create Branch/MR/Merge 超时后查询外部状态；
- Merge 必须携带当前 Head SHA；
- Token 不进入 Agent 日志。

当前 Engineering Package Executor 的固定顺序是 Issue → Branch → Commit → Draft MR。
每一步先读、最多写一次、写后 Read-back；无法证明写入结果时持久化 `UNKNOWN`，禁止盲目重试。

## 4. 身份与接入

- API 生产模式使用 OIDC Bearer；客户端自报身份头会被删除；
- GitLab Base URL 和 Token 只能由部署配置固定；
- Onboarding 必须先调用 GitLab Project API 回读 ID 和默认分支；
- 数据库只保存 Secret Reference，不保存 Token；
- 接入需要 `integration_admin/platform_admin`，交付需要 `delivery_manager`。

## 5. GitLab CE 兼容

一期核心闭环不依赖 Premium/Ultimate External Status Checks 或 Deployment Approvals。默认分支应禁止普通开发者和 Agent 直接 Push/Merge，只允许平台 Bot 在门禁通过后合并。

## 6. CI 证据

CI 上传的报告不能被直接信任。平台必须调用 GitLab API 验证 Job、Pipeline、Commit SHA 和 MR，并重新计算报告 Digest。

## 7. 当前验收边界

本地 OIDC 密码学集成、GitLab HTTP Contract 和 PostgreSQL Truth 已通过。企业 IdP、
Disposable GitLab Project、真实 429/超时/乱序和默认分支权限回读尚未执行，详见
`docs/13_身份与GitLab交付使用手册.md`。
