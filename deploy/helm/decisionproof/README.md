# DecisionProof Helm Chart

此 Chart 用于**单节点集成验证**，不是多副本生产 Chart。

Chart 支持 `storage.mode=file|postgres`。PostgreSQL 模式会接通核心、产品元数据和 Webhook Inbox，
并通过 Init Container 运行摘要校验迁移；上传原始对象仍使用本地 PVC，所以当前仍限制单副本。

使用不可变镜像摘要：

```bash
helm upgrade --install decisionproof ./deploy/helm/decisionproof \
  --set image.repository=registry.example/decisionproof/control-api \
  --set image.digest=sha256:REPLACE_WITH_REAL_DIGEST
```

生产上线前必须先完成 Temporal、OIDC/KMS、外部对象存储、PDB/HPA、数据库高可用与备份恢复和真实 Provider E2E。

完整 PostgreSQL 实验部署见 `docs/12_实验部署与使用手册.md`。
