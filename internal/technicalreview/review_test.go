package technicalreview

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

type rateLimitedReviewer struct {
	reviewCalls int
	planCalls   int
}

type transientMeteredReviewer struct {
	reviewCalls int
	planCalls   int
}

type permanentPlanningReviewer struct {
	fakeReviewer
	planCalls int
}

func (reviewer *rateLimitedReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.OpenAI, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.reviewCalls++
	if reviewer.reviewCalls == 1 {
		return providers.TechnicalReviewResult{}, &providers.RemoteRateLimitError{Provider: "OpenAI", RetryAfter: 20 * time.Second}
	}
	result.Reviewed = 1
	result.Observations = []providers.TechnicalObservation{testObservation(request.Candidates[0])}
	return result, nil
}

func (reviewer *rateLimitedReviewer) PlanTechnicalSearch(_ context.Context, _ providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	reviewer.planCalls++
	if reviewer.planCalls == 1 {
		return providers.TechnicalSearchPlan{}, providers.Usage{}, &providers.RemoteRateLimitError{Provider: "OpenAI", RetryAfter: 15 * time.Second}
	}
	return providers.TechnicalSearchPlan{Needed: false, Reason: "No follow-up needed."}, providers.Usage{PromptTokens: 10}, nil
}

func (reviewer *transientMeteredReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{
		Provider: providers.Gemini, Model: "test", InputCandidates: len(request.Candidates),
		Observations: []providers.TechnicalObservation{},
	}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.reviewCalls++
	result.Usage = providers.Usage{PromptTokens: reviewer.reviewCalls, CompletionTokens: reviewer.reviewCalls, ReasoningTokens: reviewer.reviewCalls, TotalDurationNS: int64(reviewer.reviewCalls)}
	if reviewer.reviewCalls == 1 {
		return result, &providers.RemoteTransientError{Provider: "Gemini", Message: "temporary overload"}
	}
	result.Reviewed = 1
	result.Observations = []providers.TechnicalObservation{testObservation(request.Candidates[0])}
	return result, nil
}

func (reviewer *transientMeteredReviewer) PlanTechnicalSearch(_ context.Context, _ providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	reviewer.planCalls++
	usage := providers.Usage{PromptTokens: 4 * reviewer.planCalls, CompletionTokens: reviewer.planCalls, ReasoningTokens: reviewer.planCalls, TotalDurationNS: int64(reviewer.planCalls)}
	if reviewer.planCalls == 1 {
		return providers.TechnicalSearchPlan{}, usage, &providers.RemoteTransientError{Provider: "Gemini", Message: "temporary overload"}
	}
	return providers.TechnicalSearchPlan{Needed: false, Reason: "No follow-up needed."}, usage, nil
}

func (reviewer *permanentPlanningReviewer) PlanTechnicalSearch(_ context.Context, _ providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	reviewer.planCalls++
	return providers.TechnicalSearchPlan{}, providers.Usage{PromptTokens: 13, CompletionTokens: 2}, &providers.RemoteRateLimitError{
		Provider: "Gemini", Permanent: true, Message: "daily quota exhausted",
	}
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

func TestRunReturnsValidatedPartialResultAndAccountingOnOrchestrationError(t *testing.T) {
	first := testCandidate("first")
	second := testCandidate("second")
	second.Path = "second.go"
	second.EvidenceFingerprint = strings.Repeat("c", 64)
	reviewer := &fakeReviewer{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{first, second}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		OnProgress: func(progress Progress) error {
			if progress.Current == 2 {
				return errors.New("progress output unavailable")
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected orchestration error")
	}
	if reviewer.calls != 1 || result.InputCandidates != 2 || result.Reviewed != 1 || len(result.Observations) != 1 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 100 {
		t.Fatalf("partial result was not retained: calls=%d result=%#v err=%v", reviewer.calls, result, err)
	}
}

func TestRunWaitsAndRetriesRateLimitedTechnicalReview(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	reviewer := &rateLimitedReviewer{planCalls: 1}
	waits := []time.Duration{}
	progress := []Progress{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait:       func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
		OnProgress: func(value Progress) error { progress = append(progress, value); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.reviewCalls != 2 || result.Reviewed != 1 || len(waits) != 1 || waits[0] != time.Minute {
		t.Fatalf("rate-limited review was not retried: reviewer=%#v waits=%v result=%#v", reviewer, waits, result)
	}
	if len(progress) != 2 || progress[1].Stage != ProgressStageRateLimitWait || progress[1].Attempt != 1 || progress[1].Wait != time.Minute {
		t.Fatalf("retry progress = %#v", progress)
	}
}

func TestRunWaitsAndRetriesRateLimitedSearchPlanning(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	candidate.InvestigationMode = "extended-search"
	candidate.SearchCoverage.Excerpts = 0
	reviewer := &rateLimitedReviewer{reviewCalls: 1}
	waits := []time.Duration{}
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			return value, 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.planCalls != 2 || reviewer.reviewCalls != 2 || result.Reviewed != 1 || len(waits) != 1 || waits[0] != time.Minute {
		t.Fatalf("rate-limited search planning was not retried: reviewer=%#v waits=%v result=%#v", reviewer, waits, result)
	}
}

func TestRunRetriesTypedTransientFailuresAndAccumulatesEveryMeteredAttempt(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	candidate.InvestigationMode = "extended-search"
	candidate.SearchCoverage.Excerpts = 0
	reviewer := &transientMeteredReviewer{}
	var waits []time.Duration
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			return value, 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.planCalls != 2 || reviewer.reviewCalls != 2 || result.Reviewed != 1 || result.ProviderRequests != 4 {
		t.Fatalf("transient attempts were not retried: planner=%d reviewer=%d result=%#v", reviewer.planCalls, reviewer.reviewCalls, result)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != time.Second {
		t.Fatalf("transient waits = %v, want one second per first retry in each operation", waits)
	}
	// Planning attempts use 4+8 prompt tokens; review attempts use 1+2.
	if result.Usage.PromptTokens != 15 || result.Usage.CompletionTokens != 6 || result.Usage.ReasoningTokens != 6 || result.Usage.TotalDurationNS != 6 {
		t.Fatalf("cumulative usage = %#v, want all planning and review attempts", result.Usage)
	}
}

func TestRunRetainsMeteredPermanentSearchPlanningFailureWithoutReviewingTarget(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	candidate.InvestigationMode = "extended-search"
	candidate.SearchCoverage.Excerpts = 0
	reviewer := &permanentPlanningReviewer{}
	waits := 0
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(context.Context, time.Duration) error { waits++; return nil },
		RetrieveFollowUp: func(value providers.TechnicalCandidate, _ providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			return value, 0
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.planCalls != 1 || reviewer.calls != 0 || waits != 0 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 13 || result.Usage.CompletionTokens != 2 || result.Reviewed != 0 {
		t.Fatalf("planning failure accounting = plans %d reviews %d waits %d result %#v", reviewer.planCalls, reviewer.calls, waits, result)
	}
	if !strings.Contains(strings.Join(result.Notes, "\n"), "during follow-up planning") {
		t.Fatalf("planning failure was not disclosed: %#v", result.Notes)
	}
}

func TestRunDoesNotRetryIntrinsicallyOversizedTechnicalRequest(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	waits := 0
	oversized := &oversizedTechnicalReviewer{}
	result, err := Run(context.Background(), oversized, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(_ context.Context, _ time.Duration) error { waits++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if oversized.calls != 1 || waits != 0 || result.Reviewed != 0 || !strings.Contains(strings.Join(result.Notes, "\n"), "Request too large") {
		t.Fatalf("oversized request should remain unresolved without waiting: calls=%d waits=%d result=%#v", oversized.calls, waits, result)
	}
}

type outputExhaustedTechnicalReviewer struct {
	calls      int
	allowances []int
}

func (reviewer *outputExhaustedTechnicalReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.OpenAI, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	reviewer.allowances = append(reviewer.allowances, request.MaxOutputTokens)
	result.Usage = providers.Usage{PromptTokens: 10, CompletionTokens: 4}
	if reviewer.calls == 1 {
		return result, &providers.RemoteIncompleteError{Provider: "OpenAI", Reason: "max_output_tokens", InputTokens: 10, OutputTokens: 4}
	}
	candidate := request.Candidates[0]
	result.Observations = []providers.TechnicalObservation{testObservation(candidate)}
	result.Reviewed = 1
	return result, nil
}

func TestRunGrowsOutputAllowanceAfterTechnicalReviewTruncation(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	reviewer := &outputExhaustedTechnicalReviewer{}
	var waits []time.Duration
	var progress []Progress
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait:       func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
		OnProgress: func(value Progress) error { progress = append(progress, value); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 2 || len(reviewer.allowances) != 2 || reviewer.allowances[0] != 0 || reviewer.allowances[1] != 8192 {
		t.Fatalf("technical output recovery calls=%d allowances=%v", reviewer.calls, reviewer.allowances)
	}
	if len(waits) != 0 || result.ProviderRequests != 2 || result.Reviewed != 1 || result.Usage.PromptTokens != 20 || result.Usage.CompletionTokens != 8 {
		t.Fatalf("technical output recovery accounting waits=%v result=%#v", waits, result)
	}
	if len(progress) < 2 || progress[1].Stage != ProgressStageOutputRecovery {
		t.Fatalf("technical output recovery progress = %#v", progress)
	}
}

type invalidThenValidTechnicalReviewer struct {
	calls int
}

func (reviewer *invalidThenValidTechnicalReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.Anthropic, Model: "test", InputCandidates: len(request.Candidates)}
	if len(request.Candidates) == 0 {
		result.Observations = []providers.TechnicalObservation{}
		return result, nil
	}
	result.Usage = providers.Usage{PromptTokens: 8, CompletionTokens: 2}
	reviewer.calls++
	if reviewer.calls == 1 {
		result.Observations = []providers.TechnicalObservation{}
		return result, &providers.StructuredOutputValidationError{Diagnostic: "invalid citation binding"}
	}
	result.Observations = []providers.TechnicalObservation{testObservation(request.Candidates[0])}
	result.Reviewed = 1
	return result, nil
}

func TestRunRegeneratesInvalidTechnicalStructuredOutput(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	reviewer := &invalidThenValidTechnicalReviewer{}
	var progress []Progress
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity:   testIdentity(),
		OnProgress: func(value Progress) error { progress = append(progress, value); return nil },
		Wait: func(context.Context, time.Duration) error {
			t.Fatal("structured validation repair should not enter a capacity wait")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 2 || result.ProviderRequests != 2 || result.Reviewed != 1 || result.Usage.PromptTokens != 16 || result.Usage.CompletionTokens != 4 {
		t.Fatalf("technical validation repair calls=%d result=%#v", reviewer.calls, result)
	}
	found := false
	for _, item := range progress {
		if item.Stage == ProgressStageValidationRepair {
			found = true
		}
	}
	if !found {
		t.Fatalf("technical validation-repair progress = %#v", progress)
	}
}

type oversizedTechnicalReviewer struct {
	calls int
}

func (reviewer *oversizedTechnicalReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.OpenAI, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	return providers.TechnicalReviewResult{}, &providers.RemoteRateLimitError{Provider: "OpenAI", Message: "Request too large", RequestTooLarge: true}
}

type alwaysRateLimitedTechnicalReviewer struct {
	calls int
}

func (reviewer *alwaysRateLimitedTechnicalReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.OpenAI, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	return providers.TechnicalReviewResult{}, &providers.RemoteRateLimitError{Provider: "OpenAI", RetryAfter: time.Second}
}

func TestRunStopsAfterBoundedRateLimitRetries(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	reviewer := &alwaysRateLimitedTechnicalReviewer{}
	var waits []time.Duration
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(_ context.Context, delay time.Duration) error { waits = append(waits, delay); return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != maxTechnicalProviderAttempts || len(waits) != maxTechnicalProviderAttempts-1 || result.ProviderRequests != maxTechnicalProviderAttempts || result.Reviewed != 0 || !strings.Contains(strings.Join(result.Notes, "\n"), "after 4 request attempts") {
		t.Fatalf("retry attempt bound was not enforced: calls=%d waits=%v result=%#v", reviewer.calls, waits, result)
	}
	for _, wait := range waits {
		if wait != time.Minute {
			t.Fatalf("wait = %s, want one full rolling window", wait)
		}
	}
}

type permanentRateLimitedTechnicalReviewer struct {
	calls int
}

func (reviewer *permanentRateLimitedTechnicalReviewer) ReviewTechnical(_ context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	result := providers.TechnicalReviewResult{Provider: providers.Anthropic, Model: "test", InputCandidates: len(request.Candidates), Observations: []providers.TechnicalObservation{}}
	if len(request.Candidates) == 0 {
		return result, nil
	}
	reviewer.calls++
	result.Usage = providers.Usage{PromptTokens: 9}
	return result, &providers.RemoteRateLimitError{Provider: "Anthropic", Permanent: true, Message: "credit balance exhausted"}
}

func TestRunDoesNotRetryPermanentQuotaAndRetainsMeteredFailure(t *testing.T) {
	candidate := testCandidate("return evaluate(output)")
	reviewer := &permanentRateLimitedTechnicalReviewer{}
	waits := 0
	result, err := Run(context.Background(), reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, Options{
		Identity: testIdentity(), MaxCandidates: 20,
		Wait: func(context.Context, time.Duration) error { waits++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.calls != 1 || waits != 0 || result.ProviderRequests != 1 || result.Reviewed != 0 || result.Usage.PromptTokens != 9 {
		t.Fatalf("permanent quota handling = calls %d waits %d result %#v", reviewer.calls, waits, result)
	}
}
