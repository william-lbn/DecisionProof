# DecisionProof Codex/Agent 开发规则

本文件适用于 Codex、OpenHands 及其他自动化开发 Agent。

## 一、产品目标

本仓库实现“产品决策到生产结果的可信闭环平台”。任何代码变更都必须服务于以下数字主线：

```text
原始产品资料 → 结构化决策 → 人工确认 → 工程就绪包 → GitLab 交付
→ 可验证制品 → GitOps 发布 → 灰度 → 技术与业务结果 → 审计证据
```

## 二、不可破坏的安全语义

1. Agent 不能批准自己。
2. Agent 不能直接合并默认分支。
3. Agent 不能直接执行生产发布。
4. 外部写动作必须具有幂等键和 Read-back 对账。
5. `UNKNOWN` 不能盲目重试。
6. 证据必须绑定精确 Subject Digest。
7. 可变镜像标签不能作为发布身份。
8. 缺失证据、指标无数据或策略不可用时必须 Fail-closed 或 Pause。
9. 不得保存或输出模型隐藏思维链；只保存任务输入、公开输出、工具调用和结果。
10. 不得用 Mock、参考 YAML 或本地演示冒充真实 GitLab/Kubernetes 生产验证。

## 三、编码要求

- Go 核心代码必须使用汉语注释说明设计意图和失败语义。
- Domain 不得直接依赖 GitLab、Temporal、Kubernetes、HTTP 或数据库 SDK。
- 任何外部 Adapter 都要有超时、稳定错误、脱敏、幂等和对账设计。
- 新增字段必须同步数据库迁移、OpenAPI/Schema、测试和文档。
- 大对象进入对象存储，只在数据库和 Workflow 中保存 URI 与 Digest。
- 使用不可变版本和 Digest；不要引入 `latest`。

## 四、测试要求

每个任务至少执行：

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
```

涉及外部 Provider 时，测试层级必须区分：

- 单元测试；
- Mock/Contract 测试；
- Disposable Provider Truth Test；
- Kubernetes E2E；
- Chaos/故障测试。

## 五、提交说明

每次输出必须列明：

```text
已完成：
未完成：
真实执行的测试：
仅模拟或未执行的测试：
数据库/API/Schema 变化：
安全影响：
已知限制：
下一任务：
```
