# DecisionProof V2 工程交付说明

## 交付定位

本仓库是“产品决策到生产结果的可信闭环平台”的 V2 工程底座，保留原 P0 控制语义并完成产品输入、决策、工程就绪、GitLab 基础适配、证据资格、Agent 协议和 Outcome 判定。

它不是已经完成全部生产集成的最终商业平台。准确实现边界以 `docs/IMPLEMENTATION_STATUS.md` 和 `validation/VALIDATION_REPORT.md` 为准。

## 交付内容

- 可编译 Go 工程；
- 内嵌中文 Web 工作台；
- DOCX/PPTX/Markdown/TXT 解析；
- 产品决策生命周期；
- 工程就绪包；
- GitLab 工程 Bundle；
- GitLab REST/Webhook 基础；
- 通用 AI 和 Agent Adapter 协议；
- Evidence V2、Release Qualifier、Agent Release Manifest、Outcome Evaluator；
- PostgreSQL 迁移；
- OpenAPI 和 JSON Schema；
- GitLab CI；
- Helm 单节点验证 Chart；
- 中文产品、架构、研发、安全和验证文档；
- 可复现验证报告；随机 ID 输出、覆盖率中间文件、日志和二进制不进入版本控制。

## 开始阅读

1. `README.md`；
2. `docs/00_产品架构与研发总纲.md`；
3. `docs/IMPLEMENTATION_STATUS.md`；
4. `docs/06_研发计划_Codex任务.md`；
5. `validation/VALIDATION_REPORT.md`。

## 开源仓库边界

- Git 只提交源码、迁移、Schema、部署模板、关键报告和可复现输入；
- `.git/` 是本地历史元数据，不作为仓库内容提交；
- `bin/`、`data/`、对象文件、`.env*`、日志、覆盖率和验证随机输出均被忽略；
- GitHub Actions 第三方 Action 使用精确 Commit SHA，不使用可变标签作为执行身份；
- 后续功能从主题分支提交 Pull Request，默认分支由人类维护者按保护规则合并。

## 开始开发

```bash
make verify
```

后续 Codex 开发必须按照 `AGENTS.md` 和 `docs/06_研发计划_Codex任务.md` 分阶段实施，不得跳过真实 Provider 验证。
