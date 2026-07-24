package assurance

import (
	"github.com/william-lbn/DecisionProof/internal/domain"
	"testing"
)

func TestQualifierRejectsOldCommit(t *testing.T) {
	nodes := []domain.EvidenceNode{{ID: "old", Type: domain.EvidenceCITest, Verdict: domain.VerdictPass, TrustClass: "VERIFIED", SubjectDigest: "old"}, {ID: "new", Type: domain.EvidenceArtifactSignature, Verdict: domain.VerdictPass, TrustClass: "ATTESTED", SubjectDigest: "sha256:a"}}
	_, err := (Qualifier{}).Qualify(nodes, SubjectBinding{CommitSHA: "new", PatchDigest: "new", ArtifactDigest: "sha256:a"}, []domain.EvidenceType{domain.EvidenceCITest, domain.EvidenceArtifactSignature})
	if err == nil {
		t.Fatal("expected old commit evidence rejection")
	}
}
func TestQualifierPass(t *testing.T) {
	nodes := []domain.EvidenceNode{{ID: "ci", Type: domain.EvidenceCITest, Verdict: domain.VerdictPass, TrustClass: "VERIFIED", SubjectDigest: "c1"}, {ID: "sig", Type: domain.EvidenceArtifactSignature, Verdict: domain.VerdictPass, TrustClass: "ATTESTED", SubjectDigest: "sha256:a"}}
	q, err := (Qualifier{}).Qualify(nodes, SubjectBinding{CommitSHA: "c1", PatchDigest: "c1", ArtifactDigest: "sha256:a"}, []domain.EvidenceType{domain.EvidenceCITest, domain.EvidenceArtifactSignature})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.EvidenceIDs) != 2 {
		t.Fatal(q)
	}
}
