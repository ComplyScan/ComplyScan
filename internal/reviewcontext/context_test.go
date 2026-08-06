package reviewcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

func TestBoundedRepositoryExcerptIncludesWiderMatchedFileContext(t *testing.T) {
	var content strings.Builder
	for line := 1; line <= 180; line++ {
		fmt.Fprintf(&content, "line-%03d\n", line)
	}
	excerpt := boundedRepositoryExcerpt([]byte(content.String()), 90, 20_000)
	if !strings.Contains(excerpt, "line-031") || !strings.Contains(excerpt, "line-149") {
		t.Fatalf("matched-file context did not include the bounded 60-line window: %s", excerpt)
	}
	if strings.Contains(excerpt, "line-001") || strings.Contains(excerpt, "line-180") {
		t.Fatalf("matched-file context escaped its bounded window: %s", excerpt)
	}
	if got := boundedRepositoryExcerpt([]byte(content.String()), 90, 80); len([]rune(got)) > 80 {
		t.Fatalf("character cap was exceeded: %d", len([]rune(got)))
	}
}

func TestBuildInvestigationsSearchesLikelyRequiredObjectiveWithoutCandidate(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "src/review/approval.go", Kind: discovery.KindSource, Content: []byte("package review\nfunc approveModelResult() bool { return true }\n")},
		{Path: "README.md", Kind: discovery.KindReadme, Content: []byte("human approval documentation")},
	}}
	evidence := framework.TechnicalEvidenceReport{
		Pack: framework.PackReference{ID: "eu-ai-act", Version: "test", Digest: strings.Repeat("a", 64)},
		Objectives: []framework.ObjectiveAssessment{{
			ID: "eu-aia-14-human-review-gate", Title: "Human review gate", SourceReference: "Article 14",
			Description: "A person approves an AI result before action.", Status: framework.ObjectiveNotDetected,
			EligibleFileKinds: []string{"source"}, InvestigationTerms: []string{"human approval", "approve", "model result"},
		}},
	}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{{Objectives: []reconciliation.ObjectiveResult{{
		ObjectiveID: "eu-aia-14-human-review-gate", Requirement: reconciliation.RequirementLikelyRequired,
	}}}}}

	request := BuildInvestigations(evidence, repository, mapping)
	if len(request.Candidates) != 1 {
		t.Fatalf("investigation target count = %d, want 1: %#v", len(request.Candidates), request)
	}
	candidate := request.Candidates[0]
	if candidate.InvestigationMode != investigationModeSearch || candidate.EvidenceStatus != string(framework.ObjectiveNotDetected) {
		t.Fatalf("unexpected investigation identity: %#v", candidate)
	}
	if len(candidate.EvidenceFingerprint) != 64 || candidate.SearchCoverage.EligibleFiles != 1 || candidate.SearchCoverage.MatchingFiles != 1 {
		t.Fatalf("unexpected search coverage or fingerprint: %#v", candidate)
	}
	if len(candidate.SourceContexts) < 1 || candidate.SourceContexts[0].Path != "src/review/approval.go" {
		t.Fatalf("relevant source was not retrieved: %#v", candidate.SourceContexts)
	}
}

func TestBuildInvestigationsDoesNotSearchUnresolvedObjective(t *testing.T) {
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
		ID: "objective", Status: framework.ObjectiveNotDetected, EligibleFileKinds: []string{"source"},
	}}}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{{Objectives: []reconciliation.ObjectiveResult{{
		ObjectiveID: "objective", Requirement: reconciliation.RequirementUnresolved,
	}}}}}
	request := BuildInvestigations(evidence, discovery.Repository{}, mapping)
	if len(request.Candidates) != 0 {
		t.Fatalf("unexpected investigation targets: %#v", request.Candidates)
	}
}
