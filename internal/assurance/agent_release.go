package assurance

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/william-lbn/DecisionProof/internal/integrity"
)

// AgentReleaseManifest 把一个生产 Agent 的代码、模型、Prompt、工具、策略、检索、记忆和沙箱绑定为不可变发布视图。
// 这避免运行中的 Agent 在各组件独立升级后形成无法复现的混合版本。
type AgentReleaseManifest struct {
	ID                       string            `json:"id"`
	AgentName                string            `json:"agent_name"`
	AgentCodeCommitSHA       string            `json:"agent_code_commit_sha"`
	Provider                 string            `json:"provider"`
	ModelID                  string            `json:"model_id"`
	ModelConfigurationDigest string            `json:"model_configuration_digest"`
	PromptDigest             string            `json:"prompt_digest"`
	ToolCatalogDigest        string            `json:"tool_catalog_digest"`
	MCPServers               map[string]string `json:"mcp_servers"`
	PolicyDigest             string            `json:"policy_digest"`
	RetrievalIndexDigest     string            `json:"retrieval_index_digest,omitempty"`
	MemorySchemaDigest       string            `json:"memory_schema_digest,omitempty"`
	RendererDigest           string            `json:"renderer_digest,omitempty"`
	SandboxProfileDigest     string            `json:"sandbox_profile_digest"`
	EvaluationSuiteDigest    string            `json:"evaluation_suite_digest"`
	EvaluationEvidenceIDs    []string          `json:"evaluation_evidence_ids"`
	AllowedCapabilities      []string          `json:"allowed_capabilities"`
	CreatedBy                string            `json:"created_by"`
	CreatedAt                time.Time         `json:"created_at"`
	ManifestDigest           string            `json:"manifest_digest"`
}

// AgentEvaluationSummary 是 Agent 发布资格的确定性质量与安全摘要。
type AgentEvaluationSummary struct {
	TaskSuccessRate            float64 `json:"task_success_rate"`
	UnsafeActionRate           float64 `json:"unsafe_action_rate"`
	PromptInjectionSuccessRate float64 `json:"prompt_injection_success_rate"`
	EvidenceMismatchRate       float64 `json:"evidence_mismatch_rate"`
	HumanOverrideRate          float64 `json:"human_override_rate"`
	P95LatencySeconds          float64 `json:"p95_latency_seconds"`
	MeanCost                   float64 `json:"mean_cost"`
	SampleCount                int     `json:"sample_count"`
}

// AgentReleasePolicy 是组织对生产 Agent 的发布门槛。
type AgentReleasePolicy struct {
	MinimumTaskSuccessRate      float64
	MaximumUnsafeActionRate     float64
	MaximumPromptInjectionRate  float64
	MaximumEvidenceMismatchRate float64
	MinimumSampleCount          int
	MaximumP95LatencySeconds    float64
	MaximumMeanCost             float64
	ForbiddenCapabilities       []string
}

// FinalizeAgentManifest 校验并计算 Manifest Digest。
func FinalizeAgentManifest(manifest AgentReleaseManifest) (AgentReleaseManifest, error) {
	if strings.TrimSpace(manifest.ID) == "" || strings.TrimSpace(manifest.AgentName) == "" {
		return AgentReleaseManifest{}, errors.New("Agent Release ID 和名称不能为空")
	}
	required := map[string]string{
		"agent_code_commit_sha":      manifest.AgentCodeCommitSHA,
		"provider":                   manifest.Provider,
		"model_id":                   manifest.ModelID,
		"model_configuration_digest": manifest.ModelConfigurationDigest,
		"prompt_digest":              manifest.PromptDigest,
		"tool_catalog_digest":        manifest.ToolCatalogDigest,
		"policy_digest":              manifest.PolicyDigest,
		"sandbox_profile_digest":     manifest.SandboxProfileDigest,
		"evaluation_suite_digest":    manifest.EvaluationSuiteDigest,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return AgentReleaseManifest{}, fmt.Errorf("Agent Release 缺少 %s", name)
		}
	}
	if len(manifest.EvaluationEvidenceIDs) == 0 {
		return AgentReleaseManifest{}, errors.New("Agent Release 必须绑定评估证据")
	}
	for name, digest := range manifest.MCPServers {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(digest) == "" {
			return AgentReleaseManifest{}, errors.New("MCP Server 名称和身份摘要不能为空")
		}
	}
	for _, capability := range manifest.AllowedCapabilities {
		if strings.TrimSpace(capability) == "*" {
			return AgentReleaseManifest{}, errors.New("生产 Agent 不允许使用通配能力 *")
		}
	}
	manifest.AllowedCapabilities = uniqueSorted(manifest.AllowedCapabilities)
	manifest.EvaluationEvidenceIDs = uniqueSorted(manifest.EvaluationEvidenceIDs)
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = time.Now().UTC()
	}
	unsigned := manifest
	unsigned.ManifestDigest = ""
	digest, err := integrity.SHA256Object(unsigned)
	if err != nil {
		return AgentReleaseManifest{}, err
	}
	manifest.ManifestDigest = digest
	return manifest, nil
}

// QualifyAgentRelease 根据可复现评估结果判断 Agent 是否允许成为生产候选。
func QualifyAgentRelease(manifest AgentReleaseManifest, summary AgentEvaluationSummary, policy AgentReleasePolicy) error {
	if manifest.ManifestDigest == "" {
		return errors.New("Agent Release Manifest 尚未 Finalize")
	}
	if summary.SampleCount < policy.MinimumSampleCount {
		return fmt.Errorf("评估样本不足: %d < %d", summary.SampleCount, policy.MinimumSampleCount)
	}
	if summary.TaskSuccessRate < policy.MinimumTaskSuccessRate {
		return fmt.Errorf("任务成功率不足: %.4f < %.4f", summary.TaskSuccessRate, policy.MinimumTaskSuccessRate)
	}
	if summary.UnsafeActionRate > policy.MaximumUnsafeActionRate {
		return fmt.Errorf("不安全动作率超限: %.4f > %.4f", summary.UnsafeActionRate, policy.MaximumUnsafeActionRate)
	}
	if summary.PromptInjectionSuccessRate > policy.MaximumPromptInjectionRate {
		return fmt.Errorf("Prompt Injection 成功率超限: %.4f > %.4f", summary.PromptInjectionSuccessRate, policy.MaximumPromptInjectionRate)
	}
	if summary.EvidenceMismatchRate > policy.MaximumEvidenceMismatchRate {
		return fmt.Errorf("证据错配率超限: %.4f > %.4f", summary.EvidenceMismatchRate, policy.MaximumEvidenceMismatchRate)
	}
	if policy.MaximumP95LatencySeconds > 0 && summary.P95LatencySeconds > policy.MaximumP95LatencySeconds {
		return fmt.Errorf("P95 延迟超限: %.2fs > %.2fs", summary.P95LatencySeconds, policy.MaximumP95LatencySeconds)
	}
	if policy.MaximumMeanCost > 0 && summary.MeanCost > policy.MaximumMeanCost {
		return fmt.Errorf("平均成本超限: %.4f > %.4f", summary.MeanCost, policy.MaximumMeanCost)
	}
	forbidden := make(map[string]struct{}, len(policy.ForbiddenCapabilities))
	for _, capability := range policy.ForbiddenCapabilities {
		forbidden[capability] = struct{}{}
	}
	for _, capability := range manifest.AllowedCapabilities {
		if _, ok := forbidden[capability]; ok {
			return fmt.Errorf("Agent Release 包含禁止能力: %s", capability)
		}
	}
	return nil
}

func uniqueSorted(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}
