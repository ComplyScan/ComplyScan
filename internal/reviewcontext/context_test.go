package reviewcontext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/providers"
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

func TestBuildInvestigationsUsesOnlyOwnedCandidatesInMultiSystemRepository(t *testing.T) {
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
		ID: "objective", Status: framework.ObjectiveCandidate,
		Matches: []framework.EvidenceMatch{
			{Fingerprint: "ranking", Path: "ranking/review.go"},
			{Fingerprint: "unassigned", Path: "misc/review.go"},
		},
	}}}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{
		{SystemID: "ranking", Objectives: []reconciliation.ObjectiveResult{{
			ObjectiveID: "objective", EvidenceReferences: []reconciliation.EvidenceReference{{
				Fingerprint: "ranking", Path: "ranking/review.go", Ownership: ownership.StatusAssigned, Systems: []string{"ranking"},
			}},
		}}},
		{SystemID: "support", Objectives: []reconciliation.ObjectiveResult{{ObjectiveID: "objective"}}},
	}}
	request := BuildInvestigations(evidence, discovery.Repository{}, mapping)
	if len(request.Candidates) != 1 || request.Candidates[0].EvidenceFingerprint != "ranking" {
		t.Fatalf("investigation candidates = %#v", request.Candidates)
	}
}

func TestBuildInvestigationsSkipsRepositoryWideSearchAcrossMultipleSystems(t *testing.T) {
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
		ID: "objective", Status: framework.ObjectiveNotDetected, EligibleFileKinds: []string{"source"},
	}}}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{
		{SystemID: "ranking", Objectives: []reconciliation.ObjectiveResult{{ObjectiveID: "objective", Requirement: reconciliation.RequirementLikelyRequired}}},
		{SystemID: "support", Objectives: []reconciliation.ObjectiveResult{{ObjectiveID: "objective", Requirement: reconciliation.RequirementLikelyRequired}}},
	}}
	request := BuildInvestigations(evidence, discovery.Repository{}, mapping)
	if len(request.Candidates) != 0 {
		t.Fatalf("multi-system repository-wide investigation was not bounded: %#v", request.Candidates)
	}
}

func TestRepositoryDigestCoversEveryDiscoveredFile(t *testing.T) {
	base := discovery.Repository{Files: []discovery.File{{Path: "control.go", Kind: discovery.KindSource, Content: []byte("package control")}}}
	changed := discovery.Repository{Files: []discovery.File{{Path: "control.go", Kind: discovery.KindSource, Content: []byte("package changed")}}}
	first := Build(framework.TechnicalEvidenceReport{}, base)
	second := Build(framework.TechnicalEvidenceReport{}, changed)
	if repositoryDigest(base) == repositoryDigest(changed) {
		t.Fatal("repository digest did not change with repository content")
	}
	if len(first.Candidates) != 0 || len(second.Candidates) != 0 {
		t.Fatalf("unexpected candidates: %#v %#v", first, second)
	}
}

func TestApplyFollowUpUsesLiteralEligibleSearchAndBoundsExcerpts(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "src/routes.go", Kind: discovery.KindSource, Content: []byte("package src\nfunc route() { handleOverride() }")},
		{Path: "src/control.go", Kind: discovery.KindSource, Content: []byte("package src\nfunc handleOverride() {}")},
		{Path: "src/second.go", Kind: discovery.KindSource, Content: []byte("package src\nvar handler = handleOverride")},
		{Path: "src/fourth.go", Kind: discovery.KindSource, Content: []byte("package src\nvar fallback = handleOverride")},
		{Path: "README.md", Kind: discovery.KindReadme, Content: []byte("handleOverride documentation")},
	}}
	candidate := providers.TechnicalCandidate{
		EligibleFileKinds: []string{"source"},
		SourceContexts:    []providers.TechnicalSourceContext{{Role: "eligible-file-manifest", Source: "src/routes.go"}},
	}
	updated, count := ApplyFollowUp(candidate, providers.TechnicalSearchPlan{
		Needed: true, Queries: []providers.TechnicalSearchQuery{{Text: "handleOverride", PathHint: "routes", Reason: "find caller"}},
	}, repository)
	if count != 3 || len(updated.SourceContexts) != 3 {
		t.Fatalf("follow-up count=%d contexts=%#v", count, updated.SourceContexts)
	}
	for _, context := range updated.SourceContexts {
		if context.Role != "model-directed-follow-up" || context.Path == "README.md" || context.Role == "eligible-file-manifest" {
			t.Fatalf("unbounded or ineligible follow-up context: %#v", context)
		}
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
