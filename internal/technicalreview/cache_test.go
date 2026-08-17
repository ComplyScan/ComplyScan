package technicalreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestCacheReusesOnlyIdenticalModelPackPromptAndCandidateContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", cacheFileName)
	cache, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity()
	candidate := testCandidate("return evaluate(output)")
	observation := testObservation(candidate)
	if err := cache.Store(identity, candidate, observation); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "return evaluate(output)") || strings.Contains(string(data), "source_contexts") {
		t.Fatalf("cache retained submitted source context:\n%s", data)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := reopened.Lookup(identity, candidate); err != nil || !found || got.Rationale != observation.Rationale {
		t.Fatalf("cached observation=%#v found=%t err=%v", got, found, err)
	}

	changedSource := testCandidate("return skipEvaluation(output)")
	if _, found, err := reopened.Lookup(identity, changedSource); err != nil || found {
		t.Fatalf("changed source cache hit=%t err=%v", found, err)
	}
	changedModel := identity
	changedModel.Model = "another-model"
	if _, found, err := reopened.Lookup(changedModel, candidate); err != nil || found {
		t.Fatalf("changed model cache hit=%t err=%v", found, err)
	}
	changedPrompt := identity
	changedPrompt.PromptVersion = "next"
	if _, found, err := reopened.Lookup(changedPrompt, candidate); err != nil || found {
		t.Fatalf("changed prompt cache hit=%t err=%v", found, err)
	}
	changedModelDigest := identity
	changedModelDigest.ModelDigest = "sha256:new-model-artifact"
	if _, found, err := reopened.Lookup(changedModelDigest, candidate); err != nil || found {
		t.Fatalf("changed model digest cache hit=%t err=%v", found, err)
	}
	changedPack := identity
	changedPack.PackDigest = strings.Repeat("9", 64)
	if _, found, err := reopened.Lookup(changedPack, candidate); err != nil || found {
		t.Fatalf("changed pack cache hit=%t err=%v", found, err)
	}
	changedSystem := candidate
	changedSystem.SystemID = "support"
	if _, found, err := reopened.Lookup(identity, changedSystem); err != nil || found {
		t.Fatalf("changed system cache hit=%t err=%v", found, err)
	}
}

func TestCacheRejectsSymlinkDestination(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, cacheFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Open(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Open symlink error = %v", err)
	}
}

func testIdentity() Identity {
	return Identity{
		Provider: providers.Ollama, Model: "qwen3:8b", PromptVersion: providers.TechnicalReviewPromptVersion,
		PackID: "eu-ai-act-technical-evidence", PackVersion: "0.1.2", PackDigest: strings.Repeat("a", 64),
	}
}

func testCandidate(source string) providers.TechnicalCandidate {
	return providers.TechnicalCandidate{
		SystemID: "ranking", SystemName: "Ranking", OwnershipScope: "explicit", RepositoryFiles: 12,
		ObjectiveID: "eu-aia-10-bias-evaluation", EvidenceFingerprint: strings.Repeat("b", 64),
		Title: "Bias evaluation", Path: "evaluator.go", Anchor: "evaluate", Reachability: "production-reachable",
		SourceContexts: []providers.TechnicalSourceContext{{Role: "anchor", Path: "evaluator.go", Symbol: "evaluate", Source: source}},
	}
}

func testObservation(candidate providers.TechnicalCandidate) providers.TechnicalObservation {
	return providers.TechnicalObservation{
		SystemID: candidate.SystemID, SystemName: candidate.SystemName, OwnershipScope: candidate.OwnershipScope, RepositoryFiles: candidate.RepositoryFiles,
		ObjectiveID: candidate.ObjectiveID, EvidenceFingerprint: candidate.EvidenceFingerprint,
		Strength: providers.StrengthPartial, Conclusion: providers.ConclusionPartial, Assurance: providers.AssuranceAISubstantiated,
		Confidence: "medium", Rationale: "The evaluator checks model output for the stated behavior.",
		SupportingEvidence: []providers.TechnicalEvidenceClaim{}, ContradictoryEvidence: []providers.TechnicalEvidenceClaim{},
		RuntimeVerificationRequired: true, LegalReviewRequired: true,
	}
}
