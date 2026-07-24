# 参与 DecisionProof

感谢参与 DecisionProof。项目接受缺陷修复、测试、文档、Provider Adapter、产品研究和安全改进。

## 开发原则

所有贡献必须遵守 `AGENTS.md` 的可信闭环和安全语义。尤其不能让 Agent 自批、直接合并默认分支或执行生产发布；外部写入必须幂等并 Read-back；证据必须绑定精确 Subject Digest。

不要在代码、测试、Issue、Pull Request、日志或截图中提交 Token、私钥、客户代码、真实生产地址或个人数据。安全问题按 `SECURITY.md` 私下报告。

## 本地验证

要求 Go 1.24.0（模块语言版本为 Go 1.23）以及可选的 Helm 3。

```bash
gofmt -w .
go vet ./...
go test -p 1 -count=1 ./...
go test -p 1 -count=1 -race ./...
./scripts/check-docs.sh
helm lint deploy/helm/decisionproof
```

如变更涉及 PostgreSQL、GitLab、Kubernetes、Temporal、供应链或 GitOps，请把测试结果分成单元测试、Contract、Disposable Provider Truth Test、Kubernetes E2E、Chaos 和未执行项。Mock 或 Dry-run 不能标记为真实 Provider 验证。

## Pull Request

1. 从最新 `main` 创建主题分支，保持一个 PR 只有一个可审计目标。
2. 同步修改数据库迁移、OpenAPI/Schema、测试和文档。
3. Go 核心代码使用汉语注释解释设计意图和失败语义。
4. 提交信息使用祈使句并描述结果；不要提交 `bin/`、`data/`、`.env*` 或验证中间文件。
5. 完整填写 PR 模板。默认分支合并由具备权限的人类维护者执行。

贡献按仓库 Apache-2.0 许可证提供。
