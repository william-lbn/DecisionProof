package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/william-lbn/DecisionProof/internal/ai"
	"github.com/william-lbn/DecisionProof/internal/delivery"
	"github.com/william-lbn/DecisionProof/internal/document"
	"github.com/william-lbn/DecisionProof/internal/product"
)

// product-demo 演示“原始产品资料 → 结构化决策 → 人工批准 → 工程就绪包”。
func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "decisionproof-demo-")
	must(err)
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "立项资料.md")
	content := `# 智能发布平台
产品目标：把高风险发布失败率降低到 1% 以下。
需求：平台必须关联需求、代码、制品、部署和生产结果。
验收标准：未签名制品必须被拒绝发布。
风险：AI Agent 不得直接获得生产集群管理员权限。
架构：GitLab 作为工程事实源，Temporal 管理长流程，PostgreSQL 保存业务和证据事实。
业务结果指标：灰度失败时必须在 5 分钟内停止扩大流量并执行回滚。
不做：一期不支持多代码托管平台。`
	must(os.WriteFile(path, []byte(content), 0o600))
	blocks, digest, mediaType, err := document.ExtractFile(path)
	must(err)
	repo := product.NewMemoryRepository()
	svc := product.NewService(repo, product.RealClock{})
	doc, err := svc.RegisterDocument(ctx, "tenant-demo", "project-demo", filepath.Base(path), mediaType, "file://"+path, digest, "product-manager")
	must(err)
	doc, err = svc.SaveParsedBlocks(ctx, doc.TenantID, doc.ID, blocks)
	must(err)
	run, decisions, err := svc.RunExtraction(ctx, doc.TenantID, doc.ID, ai.HeuristicExtractor{})
	must(err)
	approved := make([]product.ProductDecision, 0, len(decisions))
	for _, d := range decisions {
		confirmed, err := svc.ConfirmDecision(ctx, doc.TenantID, d.ID, "product-manager", d.Version)
		must(err)
		item, err := svc.ApproveDecision(ctx, doc.TenantID, d.ID, "product-owner", confirmed.Version)
		must(err)
		approved = append(approved, item)
	}
	pkg, err := svc.BuildEngineeringPackage(ctx, doc.TenantID, doc.ProjectID, "product-owner")
	must(err)
	bundle, err := delivery.RenderEngineeringBundle(pkg, approved)
	must(err)
	result := map[string]any{"document": doc, "extraction_run": run, "approved_decisions": approved, "engineering_readiness_package": pkg, "gitlab_engineering_bundle": bundle}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
