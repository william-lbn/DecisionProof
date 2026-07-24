## 变更目标

说明本变更服务于哪一段可信闭环，以及明确不在本次范围内的内容。

## 安全语义

- [ ] Agent 没有批准自己、合并默认分支或执行生产发布
- [ ] 外部写动作具有幂等键与 Read-back 对账
- [ ] `UNKNOWN` 不会被盲目重试
- [ ] 证据绑定精确 Subject Digest
- [ ] 缺失证据、指标无数据或策略不可用时 Fail-closed/Pause
- [ ] 未提交凭据、客户数据、运行数据、构建产物或可变发布身份

## 一致性

- [ ] 数据库迁移、API/OpenAPI、Schema、测试和文档已同步
- [ ] Go 核心代码有说明设计意图与失败语义的汉语注释

## 验证

列出真实执行的命令和结果，并分别注明单元、Contract、Provider Truth、Kubernetes E2E、Chaos 以及未执行项。
