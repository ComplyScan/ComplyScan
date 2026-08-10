package technicalreview

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type fakeReviewer struct {
	calls int
}

type planningReviewer struct {
	fakeReviewer
	plans int
}

type intermittentReviewer struct {
	calls int
}

func (reviewer *intermittentReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.Ollama, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	if reviewer.calls == 1 {
		return providers.TechnicalReviewResult{}, errors.New("model cited a path outside the submitted context")
	}
	result.Reviewed = 1
	result.Observations = []providers.TechnicalObservation{testObservation(request.Candidates[0])}
	return result, nil
}

func (reviewer *planningReviewer) PlanTechnicalSearch(_ context.Context, _ providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	reviewer.plans++
	return providers.TechnicalSearchPlan{
		Needed: true, Reason: "A caller could change the conclusion.",
		Queries: []providers.TechnicalSearchQuery{{Text: "evaluate", PathHint: "routes", Reason: "Find caller."}},
	}, providers.Usage{PromptTokens: 30, CompletionTokens: 10, TotalDurationNS: 20}, nil
}

func (reviewer *fakeReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{
		Provider: providers.Ollama, Model: "qwen3:8b", InputCandidates: len(request.Candidates),
		Observations: []providers.TechnicalObservation{}, Notes: []string{"advisory", "No likely technical objectives or deterministic candidates were available for evidence investigation."},
	}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	candidate := request.Candidates[0]
	result.Reviewed = 1
	result.Observations = append(result.Observations, testObservation(candidate))
	result.Usage = providers.Usage{PromptTokens: 100, CompletionTokens: 20, TotalDurationNS: 50}
	return result, nil
}

func TestRunCachesAndReusesTechnicalObservations(t *testing.T) {
	cache, err := Open(filepath.Join(t.TempDir(), cacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	candidate := testCandidate("return evaluate(output)")
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}
	progress := []Progress{}
	firstReviewer := &fakeReviewer{}
	first, err := Run(context.Background(), firstReviewer, request, Options{
		Identity: testIdentity(), Cache: cache, MaxCandidates: 20,
		OnProgress: func(value Progress) error { progress = append(progress, value); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstReviewer.calls != 1 || first.Reviewed != 1 || first.Usage.PromptTokens != 100 || len(progress) != 1 || progress[0].Cached {
		t.Fatalf("unexpected first review: calls=%d result=%#v progress=%#v", firstReviewer.calls, first, progress)
	}

	progress = nil
	secondReviewer := &fakeReviewer{}
	second, err := Run(context.Background(), secondReviewer, request, Options{
		Identity: testIdentity(), Cache: cache, MaxCandidates: 20,
		OnProgress: func(value Progress) error { progress = append(progress, value); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondReviewer.calls != 0 || second.Reviewed != 1 || second.Usage.PromptTokens != 0 || len(progress) != 1 || !progress[0].Cached {
		t.Fatalf("unexpected cached review: calls=%d result=%#v progress=%#v", secondReviewer.calls, second, progress)
	}
}

func TestRunRefreshesCachedObservation(t *testing.T) {
	cache, err := Open(filepath.Join(t.TempDir(), cacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	candidate := testCandidate("return evaluate(output)")
	if err := cache.Store(testIdentity(), candidate, testObservation(candidate)); err != nil {
		t.Fatal(err)
	}
	reviewer := &fakeReviewer{}
	if _, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), Cache: cache, Refresh: true, MaxCandidates: 20,
	}); err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 1 {
		t.Fatalf("refresh model calls = %d, want 1", reviewer.calls)
	}
}

func TestRunPlansAndCachesOneBoundedFollowUp(t *testing.T) {
	cache, err := Open(filepath.Join(t.TempDir(), cacheFileName))
	if err != nil {
		t.Fatal(err)
	}
	candidate := testCandidate("return evaluate(output)")
	candidate.InvestigationMode = "extended-search"
	candidate.SearchCoverage.Excerpts = 0
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}
	retrievalCalls := 0
	reviewer := &planningReviewer{}
	result, err := Run(context.Background(), reviewer, request, Options{
		Identity: testIdentity(), Cache: cache, MaxCandidates: 20,
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			retrievalCalls++
			value.SourceContexts = append(value.SourceContexts, providers.TechnicalSourceContext{Role: "model-directed-follow-up", Path: "routes.go", Source: "evaluate()"})
			return value, 1
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.plans != 1 || reviewer.calls != 1 || retrievalCalls != 1 || result.Usage.PromptTokens != 130 {
		t.Fatalf("unexpected calls or usage: plans=%d reviews=%d retrievals=%d result=%#v", reviewer.plans, reviewer.calls, retrievalCalls, result)
	}
	observation := result.Observations[0]
	if !observation.FollowUpRequested || observation.FollowUpExcerpts != 1 || len(observation.FollowUpQueries) != 1 {
		t.Fatalf("follow-up provenance missing: %#v", observation)
	}

	cachedReviewer := &planningReviewer{}
	cached, err := Run(context.Background(), cachedReviewer, request, Options{
		Identity: testIdentity(), Cache: cache, MaxCandidates: 20,
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			t.Fatal("cached review performed follow-up retrieval")
			return value, 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cachedReviewer.plans != 0 || cachedReviewer.calls != 0 || !cached.Observations[0].FollowUpRequested {
		t.Fatalf("cached follow-up was not reused: reviewer=%#v result=%#v", cachedReviewer, cached)
	}
}

func TestRunSkipsModelSearchPlanningWhenDeterministicContextExists(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	candidate.InvestigationMode = "candidate-validation"
	reviewer := &planningReviewer{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			t.Fatal("deterministically contextualized candidate requested model-directed retrieval")
			return value, 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.plans != 0 || reviewer.calls != 1 || result.Reviewed != 1 {
		t.Fatalf("unexpected planning or review calls: plans=%d reviews=%d result=%#v", reviewer.plans, reviewer.calls, result)
	}
}

func TestRunLimitsRepetitiveCandidatesPerSystemObjective(t *testing.T) {
	candidates := make([]providers.TechnicalCandidate, 0, 4)
	for index, character := range []string{"b", "c", "d"} {
		candidate := testCandidate(character)
		candidate.Path = character + ".go"
		candidate.EvidenceFingerprint = strings.Repeat(character, 64)
		candidate.StartLine = index + 1
		candidates = append(candidates, candidate)
	}
	other := testCandidate("other objective")
	other.ObjectiveID = "eu-aia-15-robustness"
	other.EvidenceFingerprint = strings.Repeat("e", 64)
	candidates = append(candidates, other)
	reviewer := &fakeReviewer{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: candidates}, Options{
		Identity: testIdentity(), MaxCandidates: 20, MaxPerObjective: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 3 || result.InputCandidates != 4 || result.Reviewed != 3 {
		t.Fatalf("representative limit was not applied: calls=%d result=%#v", reviewer.calls, result)
	}
	if !strings.Contains(strings.Join(result.Notes, "\n"), "1 repetitive candidate") {
		t.Fatalf("representative omission was not disclosed: %#v", result.Notes)
	}
}

func TestRunContinuesAfterOneInvalidModelResponse(t *testing.T) {
	first := testCandidate("first")
	second := testCandidate("second")
	second.Path = "second.go"
	second.EvidenceFingerprint = strings.Repeat("c", 64)
	reviewer := &intermittentReviewer{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{first, second}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 2 || result.Reviewed != 1 || len(result.Observations) != 1 || result.Observations[0].EvidenceFingerprint != second.EvidenceFingerprint {
		t.Fatalf("review did not continue after the invalid response: calls=%d result=%#v", reviewer.calls, result)
	}
	joined := strings.Join(result.Notes, "\n")
	if !strings.Contains(joined, "AI investigation incomplete") || !strings.Contains(joined, "review continued") {
		t.Fatalf("partial failure was not recorded: %s", joined)
	}
}
