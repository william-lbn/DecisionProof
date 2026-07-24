package domain

import "testing"

func TestControlledStateAndEvidenceValues(t *testing.T) {
	states := []ChangeState{StateDraft, StateSpecReview, StateSpecApproved, StateArchitectureReview,
		StateReadyForImplementation, StateImplementing, StatePatchFrozen, StateIndependentReview,
		StateCIVerified, StateArtifactQualified, StateStagingVerified, StateReleaseApproved,
		StateCanary, StatePromoted, StateRolledBack, StateOutcomeVerified, StateOutcomeFailed}
	for _, state := range states {
		if !state.Valid() {
			t.Fatalf("合法状态被拒绝: %s", state)
		}
		parsed, err := ParseChangeState(string(state))
		if err != nil || parsed != state {
			t.Fatalf("状态解析失败: state=%s parsed=%s err=%v", state, parsed, err)
		}
	}
	if ChangeState("UNTRUSTED").Valid() {
		t.Fatal("任意状态字符串不得进入状态机")
	}
	if _, err := ParseChangeState("UNTRUSTED"); err == nil {
		t.Fatal("非法状态必须返回错误")
	}

	if !EvidenceCITest.Valid() || EvidenceType("CUSTOM").Valid() {
		t.Fatal("证据类型必须使用受控集合")
	}
	if !VerdictPass.Valid() || EvidenceVerdict("MAYBE").Valid() {
		t.Fatal("证据结论必须使用受控集合")
	}
}
