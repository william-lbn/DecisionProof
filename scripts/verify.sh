#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p validation bin

echo "[1/8] Go 格式化"
gofmt -w .

echo "[2/8] 静态检查"
go vet ./... | tee validation/go-vet-final.log

echo "[3/8] 单元和集成测试及覆盖率"
# 先验证所有包；覆盖率只对含测试文件的包生成 covdata，避免无测试命令包在资源紧张时触发工具链异常。
go test -p 1 -count=1 ./... | tee validation/go-test-final.log
TEST_PACKAGES="$(go list -f '{{if .TestGoFiles}}{{.ImportPath}}{{end}}' ./...)"
# shellcheck disable=SC2086
go test -p 1 -count=1 -coverprofile=validation/coverage-final.out $TEST_PACKAGES | tee -a validation/go-test-final.log
go tool cover -func=validation/coverage-final.out | tee validation/coverage-final-summary.txt

echo "[4/8] 数据竞争检查"
go test -p 1 -count=1 -race ./... | tee validation/go-test-race-final.log

echo "[5/8] 可执行文件构建"
CGO_ENABLED=0 go build -trimpath -o bin/control-api ./cmd/control-api
CGO_ENABLED=0 go build -trimpath -o bin/migrate ./cmd/migrate
CGO_ENABLED=0 go build -trimpath -o bin/demo ./cmd/demo
CGO_ENABLED=0 go build -trimpath -o bin/product-demo ./cmd/product-demo

echo "[6/8] 本地演示"
./bin/demo > validation/core-demo-final.json
./bin/product-demo > validation/product-demo-final.json
python3 -m json.tool validation/core-demo-final.json >/dev/null
python3 -m json.tool validation/product-demo-final.json >/dev/null

echo "[7/8] 文档检查"
./scripts/check-docs.sh | tee validation/docs-check-final.log

echo "[8/8] JSON/YAML 语法检查"
python3 - <<'PY'
from pathlib import Path
import json
import yaml
for p in Path('schemas').rglob('*.json'):
    json.loads(p.read_text(encoding='utf-8'))
yaml_files = [Path('.gitlab-ci.yml'), Path('schemas/openapi.yaml')]
yaml_files += [*Path('.github').rglob('*.yaml'), *Path('.github').rglob('*.yml')]
yaml_files += [
    p for p in [*Path('deploy').rglob('*.yaml'), *Path('deploy').rglob('*.yml')]
    if 'templates' not in p.parts
]
for p in yaml_files:
    list(yaml.safe_load_all(p.read_text(encoding='utf-8')))
print('JSON/YAML 语法检查通过')
PY

if command -v helm >/dev/null 2>&1; then
  helm lint deploy/helm/decisionproof
fi

echo "本地可执行验证全部通过；PostgreSQL/Kubernetes 真实性结果见实验报告，GitLab/Temporal/Agent/Argo Provider 仍需真实环境验证。"
