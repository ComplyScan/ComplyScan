package technicalreview

import (
	"context"
	"path/filepath"
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
