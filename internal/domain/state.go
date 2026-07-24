package domain

import "fmt"

// ChangeState 表示一次研发变更在可信软件交付生命周期中的权威状态。
//
// 设计原则：
//   - 状态只能由控制内核迁移，Agent 只能提交“迁移建议”；
//   - 每次迁移都必须绑定操作者、幂等键、策略版本和证据摘要；
//   - 缺少必需证据时采用 fail-closed，即拒绝进入下一阶段。
type ChangeState string

const (
	StateDraft                  ChangeState = "DRAFT"
	StateSpecReview             ChangeState = "SPEC_REVIEW"
	StateSpecApproved           ChangeState = "SPEC_APPROVED"
	StateArchitectureReview     ChangeState = "ARCHITECTURE_REVIEW"
	StateReadyForImplementation ChangeState = "READY_FOR_IMPLEMENTATION"
	StateImplementing           ChangeState = "IMPLEMENTING"
	StatePatchFrozen            ChangeState = "PATCH_FROZEN"
	StateIndependentReview      ChangeState = "INDEPENDENT_REVIEW"
	StateCIVerified             ChangeState = "CI_VERIFIED"
	StateArtifactQualified      ChangeState = "ARTIFACT_QUALIFIED"
	StateStagingVerified        ChangeState = "STAGING_VERIFIED"
	StateReleaseApproved        ChangeState = "RELEASE_APPROVED"
	StateCanary                 ChangeState = "CANARY"
	StatePromoted               ChangeState = "PROMOTED"
	StateRolledBack             ChangeState = "ROLLED_BACK"
	StateOutcomeVerified        ChangeState = "OUTCOME_VERIFIED"
	StateOutcomeFailed          ChangeState = "OUTCOME_FAILED"
)

// Valid 检查状态值是否属于平台定义的有限状态集合。
func (s ChangeState) Valid() bool {
	switch s {
	case StateDraft, StateSpecReview, StateSpecApproved, StateArchitectureReview,
		StateReadyForImplementation, StateImplementing, StatePatchFrozen,
		StateIndependentReview, StateCIVerified, StateArtifactQualified,
		StateStagingVerified, StateReleaseApproved, StateCanary, StatePromoted,
		StateRolledBack, StateOutcomeVerified, StateOutcomeFailed:
		return true
	default:
		return false
	}
}

// ParseChangeState 将字符串解析为受控状态，避免任意字符串污染状态机。
func ParseChangeState(raw string) (ChangeState, error) {
	s := ChangeState(raw)
	if !s.Valid() {
		return "", fmt.Errorf("非法的变更状态: %q", raw)
	}
	return s, nil
}
