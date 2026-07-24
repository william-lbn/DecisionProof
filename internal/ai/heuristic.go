package ai

import (
	"context"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/integrity"
	"github.com/william-lbn/DecisionProof/internal/product"
)

// HeuristicExtractor 是开发环境使用的确定性提取器。
// 它不是大模型替代品，但能验证产品输入、来源锚点、人工确认和工程就绪包闭环。
type HeuristicExtractor struct{}

func (HeuristicExtractor) Name() string  { return "deterministic" }
func (HeuristicExtractor) Model() string { return "keyword-v2" }

func (HeuristicExtractor) Extract(ctx context.Context, blocks []product.SourceBlock) ([]product.DecisionDraft, string, error) {
	_ = ctx
	promptDigest, _ := integrity.SHA256Object(map[string]any{"extractor": "keyword-v2", "rules": orderedRules})
	out := make([]product.DecisionDraft, 0)
	for _, b := range blocks {
		kind, title, ok := classify(b.Text)
		if !ok {
			continue
		}
		out = append(out, product.DecisionDraft{Kind: kind, Title: title, Content: b.Text, Confidence: 0.65, Anchors: []product.SourceAnchor{{DocumentID: b.DocumentID, BlockID: b.ID, Locator: b.Locator, Digest: b.Digest}}})
	}
	return out, promptDigest, nil
}

type keywordRule struct {
	Kind  product.DecisionKind
	Words []string
}

// 顺序是语义优先级，避免“验收标准中的必须”被错误识别为普通需求。
var orderedRules = []keywordRule{
	{product.DecisionAcceptanceCriterion, []string{"验收", "acceptance", "通过标准", "完成条件"}},
	{product.DecisionOutcomeContract, []string{"业务结果", "成功标准", "outcome", "kpi", "指标"}},
	{product.DecisionArchitectureDecision, []string{"架构", "architecture", "技术方案", "adr"}},
	{product.DecisionRisk, []string{"风险", "risk", "隐患"}},
	{product.DecisionConstraint, []string{"约束", "constraint", "限制"}},
	{product.DecisionOutOfScope, []string{"不做", "非范围", "out of scope"}},
	{product.DecisionBusinessCase, []string{"商业价值", "收益", "成本", "roi", "business case"}},
	{product.DecisionCustomerInsight, []string{"客户反馈", "客户访谈", "用户反馈", "customer insight"}},
	{product.DecisionProductObjective, []string{"产品目标", "objective", "okr", "目标"}},
	{product.DecisionRequirement, []string{"需求", "requirement", "必须", "应当", "支持"}},
}

func classify(text string) (product.DecisionKind, string, bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	// 优先识别显式字段前缀，避免“高风险发布目标”被关键词“风险”误分类。
	prefixes := []struct {
		Prefix string
		Kind   product.DecisionKind
	}{
		{"产品目标", product.DecisionProductObjective}, {"目标", product.DecisionProductObjective},
		{"需求", product.DecisionRequirement}, {"验收标准", product.DecisionAcceptanceCriterion},
		{"验收", product.DecisionAcceptanceCriterion}, {"风险", product.DecisionRisk},
		{"架构", product.DecisionArchitectureDecision}, {"业务结果指标", product.DecisionOutcomeContract},
		{"成功标准", product.DecisionOutcomeContract}, {"不做", product.DecisionOutOfScope},
	}
	for _, item := range prefixes {
		if strings.HasPrefix(lower, strings.ToLower(item.Prefix)) {
			title := text
			if len([]rune(title)) > 40 {
				title = string([]rune(title)[:40]) + "…"
			}
			return item.Kind, title, true
		}
	}
	for _, rule := range orderedRules {
		for _, word := range rule.Words {
			if strings.Contains(lower, strings.ToLower(word)) {
				title := text
				if len([]rune(title)) > 40 {
					title = string([]rune(title)[:40]) + "…"
				}
				return rule.Kind, title, true
			}
		}
	}
	return "", "", false
}
