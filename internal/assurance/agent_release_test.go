package assurance

import "testing"

func TestAgentReleaseQualification(t *testing.T) {
	manifest, err := FinalizeAgentManifest(AgentReleaseManifest{
		ID: "AR-1", AgentName: "coding-agent", AgentCodeCommitSHA: "abcdef123456",
		Provider: "codex-compatible", ModelID: "configured-model",
		ModelConfigurationDigest: "sha256:model", PromptDigest: "sha256:prompt",
		ToolCatalogDigest: "sha256:tools", PolicyDigest: "sha256:policy",
		SandboxProfileDigest: "sha256:sandbox", EvaluationSuiteDigest: "sha256:eval",
		EvaluationEvidenceIDs: []string{"E-2", "E-1", "E-1"},
		AllowedCapabilities:   []string{"git.read", "git.branch.write", "git.merge_request.create"},
		MCPServers:            map[string]string{"gitlab": "sha256:mcp-server"}, CreatedBy: "platform-owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ManifestDigest == "" || len(manifest.EvaluationEvidenceIDs) != 2 {
		t.Fatalf("Manifest 未规范化: %#v", manifest)
	}
	policy := AgentReleasePolicy{
		MinimumTaskSuccessRate: 0.80, MaximumUnsafeActionRate: 0,
		MaximumPromptInjectionRate: 0.01, MaximumEvidenceMismatchRate: 0.01,
		MinimumSampleCount: 100, MaximumP95LatencySeconds: 120, MaximumMeanCost: 2,
		ForbiddenCapabilities: []string{"production.kubernetes.write"},
	}
	summary := AgentEvaluationSummary{TaskSuccessRate: 0.90, SampleCount: 200, P95LatencySeconds: 80, MeanCost: 1}
	if err := QualifyAgentRelease(manifest, summary, policy); err != nil {
		t.Fatal(err)
	}
}

func TestAgentReleaseRejectsWildcardAndUnsafeMetric(t *testing.T) {
	_, err := FinalizeAgentManifest(AgentReleaseManifest{
		ID: "AR-1", AgentName: "agent", AgentCodeCommitSHA: "a", Provider: "p", ModelID: "m",
		ModelConfigurationDigest: "sha256:m", PromptDigest: "sha256:p", ToolCatalogDigest: "sha256:t",
		PolicyDigest: "sha256:policy", SandboxProfileDigest: "sha256:s", EvaluationSuiteDigest: "sha256:e",
		EvaluationEvidenceIDs: []string{"E-1"}, AllowedCapabilities: []string{"*"},
	})
	if err == nil {
		t.Fatal("通配能力应被拒绝")
	}

	manifest, err := FinalizeAgentManifest(AgentReleaseManifest{
		ID: "AR-2", AgentName: "agent", AgentCodeCommitSHA: "a", Provider: "p", ModelID: "m",
		ModelConfigurationDigest: "sha256:m", PromptDigest: "sha256:p", ToolCatalogDigest: "sha256:t",
		PolicyDigest: "sha256:policy", SandboxProfileDigest: "sha256:s", EvaluationSuiteDigest: "sha256:e",
		EvaluationEvidenceIDs: []string{"E-1"}, AllowedCapabilities: []string{"git.read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = QualifyAgentRelease(manifest, AgentEvaluationSummary{TaskSuccessRate: 0.9, UnsafeActionRate: 0.01, SampleCount: 100}, AgentReleasePolicy{MinimumTaskSuccessRate: 0.8, MaximumUnsafeActionRate: 0, MinimumSampleCount: 100})
	if err == nil {
		t.Fatal("不安全动作率超限应被拒绝")
	}
}
