# 安全策略

## 支持范围

当前仓库属于 DecisionProof V2 工程基础版，尚未完成生产身份、KMS、Temporal、真实 GitLab/Kubernetes 端到端集成。请先阅读 `docs/IMPLEMENTATION_STATUS.md`，不要把当前版本直接暴露到公网或用于无人值守生产发布。

## 报告安全问题

请通过私有安全渠道向项目维护团队提交，至少包含：

- 受影响版本或 Commit；
- 复现步骤；
- 影响范围；
- 是否已经公开；
- 可选的修复建议。

不要在公开 Issue 中提交 Token、私钥、客户代码、漏洞利用载荷或真实生产地址。

## 当前强制安全边界

- Agent 不得自行批准、合并或执行生产发布；
- 所有外部写操作必须经过可信工具网关；
- 不允许把 GitLab Maintainer Token、Kubernetes 管理凭据或模型密钥写入代码和日志；
- 发布对象必须使用不可变 Digest；
- Webhook 只作为通知，必须通过 Provider API 对账；
- Prometheus 无数据时不得自动推广；
- `UNKNOWN` 外部动作不得盲目重试；
- 当前开发身份头不能用于生产环境。
