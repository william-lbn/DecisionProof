# DecisionProof V2 本地真实性验证报告

日期：2026-07-24
验证对象：产品决策到生产结果的可信闭环平台 V2 工程底座

## 一、结论

本轮在当前沙箱中实际完成并通过：

- Go 格式化；
- `go vet ./...`；
- 全部 Go 单元与本地集成测试；
- `go test -race ./...`；
- 四个可执行文件构建；
- P0 控制内核演示；
- 产品资料到工程就绪包演示；
- 真实 HTTP API 进程冒烟；
- 内嵌中文工作台访问；
- Markdown 上传与来源块解析；
- 产品决策提取、独立确认和批准；
- 工程就绪包与 GitLab Bundle；
- GitLab Webhook HMAC 与重复事件去重；
- JSON/YAML 语法检查。

这些结果证明当前代码是可编译、可执行、可测试的工程底座。PostgreSQL/Kubernetes 单节点真实性结果见 `POSTGRES_K8S_VALIDATION_REPORT.md`。它们**不能证明**真实 GitLab、Temporal、Argo CD、Argo Rollouts、Cosign 或生产高可用已经完成。

## 二、执行命令

```bash
./scripts/verify.sh
```

该脚本执行：

```bash
gofmt -w .
go vet ./...
go test -count=1 -coverprofile=validation/coverage-final.out ./...
go test -count=1 -race ./...
go build ./cmd/control-api
go build ./cmd/demo
go build ./cmd/product-demo
./bin/demo
./bin/product-demo
./scripts/check-docs.sh
```

并使用 Python 解析 JSON/YAML。

## 三、覆盖率

全工程语句覆盖率：**51.4%**。

部分重要包：

| Package | 覆盖率 |
|---|---:|
| `internal/delivery` | 85.2% |
| `internal/service` | 77.1% |
| `internal/assurance` | 76.5% |
| `internal/agent` | 73.9% |
| `internal/document` | 68.5% |
| `internal/integrations/prometheus` | 65.9% |
| `internal/product` | 58.1% |
| `internal/productapi` | 54.6% |
| `internal/integrations/gitlab` | 52.6% |
| `internal/httpapi` | 44.5% |
| `internal/store` | 17.4% |

覆盖率尚未达到生产候选门槛。尤其 PostgreSQL Adapter、Control API 全路由、Telemetry Logger 和真实 Provider 故障路径仍需补充。

## 四、HTTP 端到端冒烟结果

真实启动 `control-api` 后执行的结果：

```json
{
  "health": "PASS",
  "ui": "PASS",
  "source_blocks": 13,
  "decisions": 9,
  "approved_decisions": 9,
  "gitlab_bundle_files": 2,
  "webhook_duplicate_detected": true
}
```

完整结果见：

- `final-api-smoke-summary.json`；
- `final-api-upload.json`；
- `final-api-extract.json`；
- `final-api-decisions.json`；
- `final-api-package.json`；
- `final-api-gitlab-bundle.json`；
- `final-webhook-first.json`；
- `final-webhook-duplicate.json`。

## 五、已验证的重要故障语义

- 相同幂等动作不会重复产生外部效果；
- commit-then-timeout 可进入 `UNKNOWN` 并通过查询对账；
- Agent CLI 超时会终止整个进程组，避免子进程残留；
- File Product Repository 磁盘提交失败会回滚内存状态；
- File Webhook Inbox 磁盘提交失败会回滚内存状态；
- GitLab Webhook 重复 Event UUID 被识别；
- Author 自审被拒绝；
- 发布清单内容修改后签名失败；
- 发布清单增加签名密钥标识并检查本地信任集合；
- Subject-bound Qualifier 会拒绝错误 Commit、Artifact 或 Release 的证据；
- Outcome 无数据返回 `INCONCLUSIVE`，不会默认通过。

## 六、当前环境与未执行验证

当前环境已具备并真实使用 Docker Desktop、Kubernetes v1.30.5、Helm v3.9.0 和 PostgreSQL 16 实验容器；已经执行 Helm Lint、API Server Dry-run、Kubernetes Apply、PostgreSQL RLS、Pod 重启持久化和数据库不可用 Readiness 故障测试。

当前仍没有可用的 Disposable GitLab、Temporal Server、Argo CD/Rollouts、Cosign/Syft/Trivy 或企业 AI Provider，因此没有声称完成：

- PostgreSQL 复制、PITR、主从切换和跨故障域恢复；
- Temporal Worker 崩溃恢复；
- 真实 GitLab Branch/MR/Pipeline/Registry；
- Codex/OpenHands Kubernetes Agent Job；
- 制品签名、SBOM 和 SLSA Provenance；
- Canary、自动回滚和真实 Outcome。

## 七、生产候选前必须完成

1. PostgreSQL 完成 TLS、备份恢复、复制和 HA；
2. Temporal Workflow/Activity/Signal/Versioning；
3. GitLab OAuth、Project Onboarding 和 Reconciler；
4. 真实 Disposable GitLab Project Provider Truth Test；
5. Kubernetes Agent Scheduler 和隔离；
6. GitLab CI Evidence Ingest；
7. Cosign、Syft、扫描和 Provenance 实际执行；
8. Release Attempt V2 替换旧 P0 单清单模型；
9. GitOps、Argo CD/Rollouts、Prometheus；
10. OIDC/KMS/Secret Provider；
11. 多副本、Chaos、升级回滚和 7×24；
12. 全工程覆盖率达到验收要求。

## 八、真实性声明

当前状态应表述为：

```text
产品和架构：V2 总体设计完成
本地工程底座：已实现并验证
PostgreSQL/Kubernetes：单节点实验已真实验证
产品资料到工程就绪包：已实现并验证
GitLab/Prometheus/Agent 协议 Adapter：代码完成，真实 Provider 未验证
Temporal/Kubernetes/Argo/供应链执行：尚未完成
完整生产平台：尚未完成
```
