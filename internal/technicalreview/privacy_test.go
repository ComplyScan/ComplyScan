package technicalreview

import (
	"context"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type privacyBoundaryReviewer struct {
	t                 *testing.T
	secrets           []string
	plannedCandidate  *providers.TechnicalCandidate
	reviewedCandidate *providers.TechnicalCandidate
}

func (reviewer *privacyBoundaryReviewer) assertRedacted(candidate providers.TechnicalCandidate) {
	reviewer.t.Helper()
	for _, source := range candidate.SourceContexts {
		for _, secret := range reviewer.secrets {
			if strings.Contains(source.Source, secret) {
				reviewer.t.Fatalf("fake provider received raw secret in %s context: %q", source.Role, source.Source)
			}
		}
		if strings.Count(source.Source, "\n") != 2 {
			reviewer.t.Fatalf("redaction changed line structure for %s: %q", source.Role, source.Source)
		}
	}
}

func (reviewer *privacyBoundaryReviewer) PlanTechnicalSearch(_ context.Context, candidate providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	reviewer.assertRedacted(candidate)
	copy := candidate
	reviewer.plannedCandidate = &copy
	return providers.TechnicalSearchPlan{
		Needed: true, Reason: "Inspect the caller.",
		Queries: []providers.TechnicalSearchQuery{{Text: "inspectCaller", Reason: "Find the caller."}},
	}, providers.Usage{}, nil
}

func (reviewer *privacyBoundaryReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{
		Provider: providers.OpenAI, Model: "fake", InputCandidates: len(request.Candidates),
		Observations: []providers.TechnicalObservation{},
	}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	candidate := request.Candidates[0]
	reviewer.assertRedacted(candidate)
	copy := candidate
	reviewer.reviewedCandidate = &copy
	observation := testObservation(candidate)
	for _, source := range candidate.SourceContexts {
		observation.SupportingEvidence = append(observation.SupportingEvidence, providers.TechnicalEvidenceClaim{
			Path: source.Path, Line: source.StartLine, Summary: "Bound citation for " + source.Role,
		})
	}
	result.Reviewed = 1
	result.Observations = []providers.TechnicalObservation{observation}
	return result, nil
}

func TestRunRedactsEveryTechnicalSourcePathBeforePlannerAndReviewer(t *testing.T) {
	roles := []string{"matched-evidence", "anchor", "related", "relationship", "extended-search-hit"}
	secrets := []string{
		"sk-proj-" + strings.Repeat("a", 24),
		"sk-ant-api03-" + strings.Repeat("b", 24),
		"sk-or-v1-" + strings.Repeat("c", 24),
		"AIza" + strings.Repeat("d", 24),
		"hf_" + strings.Repeat("e", 24),
		"sk-svcacct-" + strings.Repeat("f", 24),
	}
	candidate := testCandidate("unused")
	candidate.InvestigationMode = "extended-search"
	candidate.SearchCoverage.Excerpts = 0
	candidate.SourceContexts = make([]providers.TechnicalSourceContext, 0, len(roles))
	for index, role := range roles {
		candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
			Role: role, Path: role + ".go", StartLine: 10 + index, EndLine: 12 + index,
			Source: "before\n" + secrets[index] + "\nafter",
		})
	}
	reviewer := &privacyBoundaryReviewer{t: t, secrets: secrets}
	retrieved := false
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{
		Candidates: []providers.TechnicalCandidate{candidate},
	}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			retrieved = true
			for _, source := range value.SourceContexts {
				if strings.Contains(source.Source, secrets[0]) {
					t.Fatal("follow-up retriever received unredacted base context")
				}
			}
			value.SourceContexts = append(value.SourceContexts, providers.TechnicalSourceContext{
				Role: "model-directed-follow-up", Path: "follow_up.go", StartLine: 42, EndLine: 44,
				Source: "before\n" + secrets[len(secrets)-1] + "\nafter",
			})
			return value, 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !retrieved || reviewer.plannedCandidate == nil || reviewer.reviewedCandidate == nil {
		t.Fatalf("expected planner, retriever, and reviewer calls: planner=%v retriever=%t reviewer=%v", reviewer.plannedCandidate != nil, retrieved, reviewer.reviewedCandidate != nil)
	}
	if len(result.Observations) != 1 || len(result.Observations[0].SupportingEvidence) != len(roles)+1 {
		t.Fatalf("redacted review lost bound citations: %#v", result)
	}
	for index, claim := range result.Observations[0].SupportingEvidence {
		wantLine := 10 + index
		if index == len(roles) {
			wantLine = 42
		}
		if claim.Line != wantLine {
			t.Fatalf("citation %d line = %d, want %d: %#v", index, claim.Line, wantLine, claim)
		}
	}
	// The privacy copy must not rewrite the deterministic evidence bundle held
	// by the caller.
	if !strings.Contains(candidate.SourceContexts[0].Source, secrets[0]) {
		t.Fatal("technical review redaction mutated the caller's source context")
	}
}
