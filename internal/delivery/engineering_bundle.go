package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/william-lbn/DecisionProof/internal/product"
)

// EngineeringBundle 是从产品决策层交付给 GitLab 工程系统的确定性结果。
// 它只渲染 Issue 与 Docs-as-Code 文件，不直接产生 GitLab 副作用；
// Branch、Commit、MR 必须分别经 Tool Gateway 执行和对账。
type EngineeringBundle struct {
	PackageID        string            `json:"package_id"`
	PackageDigest    string            `json:"package_digest"`
	IssueTitle       string            `json:"issue_title"`
	IssueDescription string            `json:"issue_description"`
	SuggestedLabels  []string          `json:"suggested_labels"`
	SuggestedBranch  string            `json:"suggested_branch"`
	Files            map[string]string `json:"files"`
}

// RenderEngineeringBundle 把不可变工程就绪包与其决策快照渲染成 GitLab 可消费内容。
func RenderEngineeringBundle(pkg product.EngineeringReadinessPackage, decisions []product.ProductDecision) (EngineeringBundle, error) {
	if strings.TrimSpace(pkg.ID) == "" || strings.TrimSpace(pkg.Digest) == "" {
		return EngineeringBundle{}, errors.New("工程就绪包 ID 和 Digest 不能为空")
	}
	byID := make(map[string]product.ProductDecision, len(decisions))
	for _, item := range decisions {
		byID[item.ID] = item
	}
	ordered := make([]product.ProductDecision, 0, len(pkg.DecisionIDs))
	for _, id := range pkg.DecisionIDs {
		item, ok := byID[id]
		if !ok {
			return EngineeringBundle{}, fmt.Errorf("工程就绪包引用的决策不存在: %s", id)
		}
		if item.Status != product.DecisionApproved {
			return EngineeringBundle{}, fmt.Errorf("工程就绪包只能包含 APPROVED 决策: %s", id)
		}
		ordered = append(ordered, item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Kind == ordered[j].Kind {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].Kind < ordered[j].Kind
	})

	manifest, err := json.MarshalIndent(struct {
		APIVersion string                              `json:"api_version"`
		Kind       string                              `json:"kind"`
		Package    product.EngineeringReadinessPackage `json:"package"`
		Decisions  []product.ProductDecision           `json:"decisions"`
	}{"decisionproof.io/v1alpha1", "EngineeringReadinessPackage", pkg, ordered}, "", "  ")
	if err != nil {
		return EngineeringBundle{}, err
	}

	base := ".decisionproof/packages/" + pkg.ID
	markdown := renderMarkdown(pkg, ordered)
	return EngineeringBundle{
		PackageID: pkg.ID, PackageDigest: pkg.Digest,
		IssueTitle:       "[DecisionProof] 工程就绪包 " + pkg.ID,
		IssueDescription: markdown,
		SuggestedLabels:  []string{"decisionproof", "engineering-ready"},
		SuggestedBranch:  "decisionproof/" + sanitizeBranch(pkg.ID),
		Files: map[string]string{
			base + "/manifest.json": string(manifest) + "\n",
			base + "/README.md":     markdown,
		},
	}, nil
}

func renderMarkdown(pkg product.EngineeringReadinessPackage, decisions []product.ProductDecision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 工程就绪包 %s\n\n", pkg.ID)
	fmt.Fprintf(&b, "> Package Digest: `%s`  \n", pkg.Digest)
	fmt.Fprintf(&b, "> Project: `%s`  \n", pkg.ProjectID)
	fmt.Fprintf(&b, "> Created by: `%s`\n\n", pkg.CreatedBy)
	b.WriteString("此文档由 DecisionProof 根据已批准产品决策确定性生成。修改源决策后必须生成新的工程就绪包，不能直接修改本文件伪造批准结果。\n\n")

	groups := []struct {
		Kind  product.DecisionKind
		Title string
	}{
		{product.DecisionProductObjective, "产品目标"},
		{product.DecisionBusinessCase, "商业论证"},
		{product.DecisionRequirement, "需求"},
		{product.DecisionAcceptanceCriterion, "验收标准"},
		{product.DecisionNonFunctional, "非功能需求"},
		{product.DecisionArchitectureDecision, "架构决策"},
		{product.DecisionRisk, "风险"},
		{product.DecisionConstraint, "约束"},
		{product.DecisionDependency, "依赖"},
		{product.DecisionOutOfScope, "明确不做"},
		{product.DecisionOutcomeContract, "生产结果契约"},
	}
	for _, group := range groups {
		items := filterKind(decisions, group.Kind)
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", group.Title)
		for _, item := range items {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", item.Title, item.Content)
			fmt.Fprintf(&b, "- 决策 ID：`%s`\n- 决策摘要：`%s`\n- 来源：%s\n\n", item.ID, item.CanonicalDigest, renderAnchors(item))
		}
	}
	b.WriteString("## 工程执行约束\n\n")
	b.WriteString("- MR、CI、制品、部署和 Outcome 证据必须绑定本工程就绪包 Digest。\n")
	b.WriteString("- 任何需求或架构变更都会使本包失效，必须重新批准并生成新包。\n")
	b.WriteString("- Agent 只能提交变更，不得批准、合并或直接生产发布。\n")
	return b.String()
}

func filterKind(decisions []product.ProductDecision, kind product.DecisionKind) []product.ProductDecision {
	result := make([]product.ProductDecision, 0)
	for _, item := range decisions {
		if item.Kind == kind {
			result = append(result, item)
		}
	}
	return result
}

func renderAnchors(item product.ProductDecision) string {
	if len(item.Anchors) == 0 {
		return "人工创建"
	}
	parts := make([]string, 0, len(item.Anchors))
	for _, anchor := range item.Anchors {
		parts = append(parts, fmt.Sprintf("`%s`（%s）", anchor.Locator, anchor.Digest))
	}
	return strings.Join(parts, "、")
}

func sanitizeBranch(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
