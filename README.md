# DecisionProof V2

**产品决策到生产结果的可信闭环平台——工程底座。**

本仓库在原 SDLCProof P0 控制内核基础上，新增产品输入与决策层、Office 文档来源锚点、GitLab/Prometheus/Agent 适配基础、Evidence V2、发布资格判断和 Outcome Contract。所有核心代码和文档使用中文说明。

Go Module：`github.com/william-lbn/DecisionProof`
许可证：Apache-2.0。贡献方式见 [`CONTRIBUTING.md`](CONTRIBUTING.md)，安全报告方式见 [`SECURITY.md`](SECURITY.md)。

> 真实性边界：当前是可运行、可测试的 V2 工程底座，不是已经完成全部 GitLab、Temporal、Kubernetes 和生产高可用集成的商业平台。准确状态见 [`docs/IMPLEMENTATION_STATUS.md`](docs/IMPLEMENTATION_STATUS.md)。

## 已实现

- DOCX、PPTX、Markdown、TXT 原生文本提取；
- 原始资料 Digest、Source Block、页/段落/幻灯片来源锚点；
- 产品决策受控类型与 `EXTRACTED → CONFIRMED → APPROVED`；
- Engineering Readiness Package；
- 模型无关 `Extractor` 和 HTTP JSON AI Provider；
- GitLab REST Client、读重试、写不盲目重试；
- GitLab Webhook HMAC/Legacy Token 校验、单节点持久化 Inbox 和重复事件去重；
- Prometheus Instant Query Adapter；
- Codex/OpenHands/企业 Agent 可复用的 Task/Event/Result 协议；
- 人工创建、修订、否决和职责分离的产品决策；
- 工程就绪包到 GitLab Issue/Docs-as-Code 的确定性渲染；
- P0 状态机、Action UNKNOWN、证据哈希链；
- Evidence V2 Subject Binding；
- 软件 Release Qualifier、Agent Release Manifest 和 Outcome Evaluator；
- 内嵌中文产品工作台；
- PostgreSQL V2 DDL、GitLab CI、Helm/Argo 参考配置；
- PostgreSQL Migration CLI、核心/产品 Repository、Webhook Inbox 和 Readiness；
- GitHub Actions（动作绑定精确 Commit）、Dependabot 和开源协作模板；
- 单元测试、Race Detector、Vet 和构建验证。

## 本地运行

要求 Go 1.23+。

```bash
make verify
./bin/control-api
```

浏览器访问：

```text
http://localhost:8080/app/
```

PostgreSQL 实验运行与 Kubernetes 部署见
[`docs/12_实验部署与使用手册.md`](docs/12_实验部署与使用手册.md)。

开发工作台使用以下本地身份头，生产环境必须替换为 GitLab OAuth/OIDC：

```text
X-Tenant-ID: tenant-demo
X-Actor-ID: product-manager
X-Actor-Roles: product_manager
```

## 产品资料演示

```bash
make product-demo
```

演示链路：

```text
Markdown 产品资料
→ 来源锚点
→ 结构化决策
→ 人工确认与批准
→ 工程就绪包
```

## 核心目录

```text
cmd/control-api          控制 API 和内嵌 UI
cmd/demo                 P0 研发控制闭环演示
cmd/product-demo         产品决策输入演示
internal/product         产品资料、决策和工程就绪包
internal/document        DOCX/PPTX/Markdown/TXT 解析
internal/ai              AI 提取 Provider
internal/agent           Agent Task/Event/Result 协议
internal/integrations    GitLab、Prometheus Adapter
internal/assurance       精确证据资格和 Outcome 判定
internal/domain          P0 变更、证据、动作和发布领域
internal/service         P0 控制内核服务
migrations               PostgreSQL Schema
schemas                  API 与受控对象 Schema
deploy                   Docker、GitLab CI、Helm、Argo 示例
docs                     产品、架构、研发、验证和演进文档
```

## 文档入口

1. [`产品架构与研发总纲`](docs/00_产品架构与研发总纲.md)
2. [`产品体验与商业闭环`](docs/01_产品体验与商业闭环.md)
3. [`端到端流程与状态模型`](docs/02_端到端流程与状态模型.md)
4. [`系统架构与前沿 AI 工程`](docs/03_系统架构与前沿AI工程.md)
5. [`GitLab 一期集成`](docs/04_GitLab一期集成.md)
6. [`安全、证据与供应链`](docs/05_安全_证据与供应链.md)
7. [`研发计划与 Codex 任务`](docs/06_研发计划_Codex任务.md)
8. [`验证计划与生产验收`](docs/07_验证计划与生产验收.md)
9. [`演进路线与竞争壁垒`](docs/08_演进路线与竞争壁垒.md)
10. [`前沿产品能力与商业演进`](docs/09_前沿产品能力与商业演进.md)
11. [`外部技术依据与架构判断`](docs/10_外部技术依据与架构判断.md)
12. [`实现状态`](docs/IMPLEMENTATION_STATUS.md)
13. [`全面审计与端到端实施计划`](docs/11_全面审计与端到端实施计划.md)
14. [`PostgreSQL 实验部署与使用手册`](docs/12_实验部署与使用手册.md)

## 生产主线待实现

优先顺序：

```text
GitLab OAuth
→ GitLab Webhook Reconciler
→ Temporal Workflow
→ K8s Agent Runtime（Codex/OpenHands）
→ GitLab CI 证据、SBOM、签名、Provenance
→ Merge Qualifier
→ GitOps、Argo CD/Rollouts、Prometheus
→ Outcome 和审计包
→ HA、KMS、备份恢复、Chaos、7×24
```

不得跳过真实 Provider Truth Test，也不得把参考配置描述为已完成生产验证。
