package sdlcproof.release

# 缺省拒绝：任何未被明确证明满足条件的发布都不得进入生产。
default allow := false

# 生产发布资格策略示例。
# 实际部署时必须把策略 Bundle 的摘要写入 TransitionRecord 和 ReleaseEvidenceManifest。
allow if {
    input.change.state == "RELEASE_APPROVED"
    startswith(input.change.artifact_digest, "sha256:")
    startswith(input.change.spec_digest, "sha256:")
    startswith(input.change.architecture_digest, "sha256:")
    startswith(input.change.patch_digest, "sha256:")

    input.evidence.independent_review.verdict == "PASS"
    input.evidence.ci_test.verdict == "PASS"
    input.evidence.security_scan.verdict == "PASS"
    input.evidence.security_scan.critical_findings == 0
    input.evidence.build_provenance.verdict == "PASS"
    input.evidence.artifact_signature.verdict == "PASS"
    input.evidence.staging_test.verdict == "PASS"
    input.evidence.release_approval.verdict == "PASS"

    input.manifest.signature_valid == true
    input.actor.roles[_] == "release_manager"
}

# 高风险变更必须包含两类相互独立的人类批准。
deny_reason contains "高风险变更缺少产品或发布双重批准" if {
    input.change.risk_level == "high"
    not input.approvals.product_approved
}

deny_reason contains "高风险变更缺少产品或发布双重批准" if {
    input.change.risk_level == "high"
    not input.approvals.release_approved
}
