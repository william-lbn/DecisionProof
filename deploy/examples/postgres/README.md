# PostgreSQL 单节点实验依赖

此目录只用于本地 Kubernetes Truth Test，不是 PostgreSQL 高可用生产方案。

先在目标 namespace 创建随机凭据：

```bash
ADMIN_PASSWORD="$(openssl rand -hex 24)"
APP_PASSWORD="$(openssl rand -hex 24)"
kubectl -n decisionproof-experiment create secret generic decisionproof-postgres-bootstrap \
  --from-literal=admin-password="$ADMIN_PASSWORD" \
  --from-literal=app-password="$APP_PASSWORD"
kubectl -n decisionproof-experiment apply -f deploy/examples/postgres/experimental-postgres.yaml
kubectl -n decisionproof-experiment rollout status statefulset/decisionproof-postgres --timeout=120s
```

为迁移 Init Container 和 API 分别创建连接串 Secret：

```bash
kubectl -n decisionproof-experiment create secret generic decisionproof-database-admin \
  --from-literal=database-url="postgres://decisionproof_admin:${ADMIN_PASSWORD}@decisionproof-postgres:5432/decisionproof?sslmode=disable"
kubectl -n decisionproof-experiment create secret generic decisionproof-database-app \
  --from-literal=database-url="postgres://decisionproof_app:${APP_PASSWORD}@decisionproof-postgres:5432/decisionproof?sslmode=disable"
```

API 必须使用 `decisionproof-database-app`；只有迁移容器使用管理员 Secret。实验数据库仍是单副本，
没有备份、PITR、TLS、反亲和或自动故障转移，因此不能作为生产数据库。
