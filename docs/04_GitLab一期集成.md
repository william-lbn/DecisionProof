# GitLab 一期集成

## 1. GitLab 的位置

GitLab 负责工程事实：Issue、Branch、Commit、MR、Pipeline、Job、Artifact 和 Registry。产品立项、原始文档、商业决策和正式审批由 DecisionProof 管理，并向 GitLab 同步工程就绪内容。

## 2. Webhook

Webhook Gateway：验证 HMAC/Legacy Token、限制 Body、生成 Digest、持久化 Inbox、快速返回 202、后台 API Read-back、Signal Workflow。

Webhook 重复和乱序是正常情况，不得直接按到达顺序推进状态。

## 3. API 可靠性

- GET 在 429/5xx 时有限退避重试；
- POST/PUT 不自动重试；
- Create Branch/MR/Merge 超时后查询外部状态；
- Merge 必须携带当前 Head SHA；
- Token 不进入 Agent 日志。

## 4. GitLab CE 兼容

一期核心闭环不依赖 Premium/Ultimate External Status Checks 或 Deployment Approvals。默认分支应禁止普通开发者和 Agent 直接 Push/Merge，只允许平台 Bot 在门禁通过后合并。

## 5. CI 证据

CI 上传的报告不能被直接信任。平台必须调用 GitLab API 验证 Job、Pipeline、Commit SHA 和 MR，并重新计算报告 Digest。
