package repositoryanalysis

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type compactValidationFallbackReviewer struct {
	mu sync.Mutex

	failFollowUp bool
	requests     []providers.RepositoryAnalysisRequest
}

func (reviewer *compactValidationFallbackReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	reviewer.mu.Unlock()

	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 17, CompletionTokens: 4}, false)
	if request.Scope == "." && request.Mode == providers.RepositoryAnalysisTargeted {
		if reviewer.failFollowUp {
			if request.AllowFollowUp {
				result.FollowUpPlan = targetedTestFollowUpPlan()
				return result, nil
			}
			if len(request.Files) > 1 {
				return result, &providers.RepositoryValidationError{Diagnostic: "the enlarged follow-up response did not match its checked evidence"}
			}
		} else {
			return result, &providers.RepositoryValidationError{Diagnostic: "the compact response did not match its checked evidence"}
		}
	}
	return result, nil
}

func (reviewer *compactValidationFallbackReviewer) recordedRequests() []providers.RepositoryAnalysisRequest {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return append([]providers.RepositoryAnalysisRequest(nil), reviewer.requests...)
}

func TestPersistentCompactValidationFailureFallsBackToSmallerTargetedBatches(t *testing.T) {
	repository := twoDirectoryCompactTargetedRepository()
	reviewer := &compactValidationFallbackReviewer{}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	requests := reviewer.recordedRequests()
	assertPersistentCompactValidationAttempts(t, requests, "the compact response did not match its checked evidence", false)
	assertTargetedValidationFallbackCoverage(t, result, requests, []string{"alpha/client.py", "beta/client.py"})
}

func TestPersistentCompactFollowUpValidationFailureFallsBackToSmallerTargetedBatches(t *testing.T) {
	reviewer := &compactValidationFallbackReviewer{failFollowUp: true}

	result, err := runSingleTargetedFixture(reviewer, targetedFollowUpRepository())
	if err != nil {
		t.Fatal(err)
	}
	requests := reviewer.recordedRequests()
	assertPersistentCompactValidationAttempts(t, requests, "the enlarged follow-up response did not match its checked evidence", true)
	assertTargetedValidationFallbackCoverage(t, result, requests, []string{"app.py", "review/approval.py"})
	if !result.FollowUpRequested || result.FollowUpExcerpts != 1 {
		t.Fatalf("follow-up fallback metadata = requested %t / excerpts %d, want true/1", result.FollowUpRequested, result.FollowUpExcerpts)
	}
}

func twoDirectoryCompactTargetedRepository() discovery.Repository {
	repository := discovery.Repository{Root: "."}
	for _, path := range []string{"alpha/client.py", "beta/client.py"} {
		content := []byte("from openai import OpenAI\nclient = OpenAI()\ndef generate(value):\n    return client.responses.create(model='gpt-test', input=value)\n")
		repository.Files = append(repository.Files, discovery.File{Path: path, Kind: discovery.KindSource, Content: content, Size: int64(len(content))})
	}
	return repository
}

func assertPersistentCompactValidationAttempts(t *testing.T, requests []providers.RepositoryAnalysisRequest, diagnostic string, followUp bool) {
	t.Helper()
	var rejected []providers.RepositoryAnalysisRequest
	for _, request := range requests {
		if request.Scope != "." || request.Mode != providers.RepositoryAnalysisTargeted {
			continue
		}
		if followUp && (request.AllowFollowUp || len(request.Files) < 2) {
			continue
		}
		rejected = append(rejected, request)
	}
	if len(rejected) != 3 {
		t.Fatalf("persistent compact validation attempts = %d, want initial response plus two repairs; requests=%#v", len(rejected), requests)
	}
	if rejected[0].ValidationFeedback != "" || rejected[1].ValidationFeedback != diagnostic || rejected[2].ValidationFeedback != diagnostic {
		t.Fatalf("compact validation feedback = %q, %q, %q", rejected[0].ValidationFeedback, rejected[1].ValidationFeedback, rejected[2].ValidationFeedback)
	}
	for index := 1; index < len(rejected); index++ {
		left, right := rejected[0], rejected[index]
		left.ValidationFeedback = ""
		right.ValidationFeedback = ""
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("compact repair %d changed trusted evidence\nfirst: %#v\nrepair: %#v", index, left, right)
		}
	}
}

func assertTargetedValidationFallbackCoverage(t *testing.T, result providers.RepositoryAnalysisResult, requests []providers.RepositoryAnalysisRequest, wantedPaths []string) {
	t.Helper()
	if result.Coverage.SourceBatchesTotal < 2 || result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal {
		t.Fatalf("validation fallback source coverage = %#v, want at least two completed smaller batches", result.Coverage)
	}
	seen := make(map[string]bool)
	for _, request := range requests {
		if request.Scope == "." {
			continue
		}
		for _, file := range request.Files {
			seen[file.Path] = true
		}
	}
	for _, path := range wantedPaths {
		if !seen[path] {
			t.Errorf("smaller-batch fallback omitted selected evidence %q; seen=%v", path, seen)
		}
	}
	if result.Coverage.ProviderRequests != len(requests) {
		t.Fatalf("fallback provider request accounting = %d, want all %d calls", result.Coverage.ProviderRequests, len(requests))
	}
}

type synthesisFragmentPrefetchReviewer struct {
	mu sync.Mutex

	failedPartOneAttempts int
	providerCalls         int
	summaryCalls          map[string]int
	initialSiblingInputs  map[string]struct{}
}

func newSynthesisFragmentPrefetchReviewer() *synthesisFragmentPrefetchReviewer {
	return &synthesisFragmentPrefetchReviewer{
		summaryCalls:         make(map[string]int),
		initialSiblingInputs: make(map[string]struct{}),
	}
}

func (reviewer *synthesisFragmentPrefetchReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	usage := providers.Usage{PromptTokens: 23, CompletionTokens: 5, ReasoningTokens: 2}
	result := targetedSuccessfulResult(request, usage, false)
	result.RateLimits = completeSynthesisCapacity()

	if request.Mode != providers.RepositoryAnalysisSynthesis {
		result.Result.UnresolvedQuestions = []string{strings.Repeat("bounded source detail ", 600)}
		if len(request.Files) > 0 {
			file := request.Files[0]
			result.Result.AIUses = []providers.RepositoryAIUse{{
				ID: "provider-local", Name: "Bounded candidate", Purpose: "Exercise semantic fragmentation", Lifecycle: "development", Confidence: "medium",
				Evidence: []providers.RepositoryCitation{{Path: file.Path, Line: file.ContentStartLine, Summary: "Submitted model invocation"}},
			}}
		}
		reviewer.mu.Lock()
		reviewer.providerCalls++
		reviewer.mu.Unlock()
		return result, nil
	}

	encoded, _ := json.Marshal(request.SubsystemSummaries)
	signature := string(encoded)
	level, part, levelScoped := synthesisLevelAndPart(request.Scope)
	reviewer.mu.Lock()
	reviewer.providerCalls++
	reviewer.summaryCalls[signature]++
	if levelScoped && level == 1 && part > 1 && len(request.SubsystemSummaries) == 1 {
		summary := request.SubsystemSummaries[0]
		if len(summary.AIUses) > 0 && len(summary.UnresolvedQuestions) > 0 {
			reviewer.initialSiblingInputs[signature] = struct{}{}
		}
	}
	shouldReject := levelScoped && level == 1 && part == 1 && reviewer.failedPartOneAttempts < maxValidationRepairRetries+1
	if shouldReject {
		reviewer.failedPartOneAttempts++
	}
	reviewer.mu.Unlock()

	if shouldReject {
		return result, &providers.RepositoryValidationError{Diagnostic: "the first singleton synthesis group stayed structurally invalid"}
	}
	for _, summary := range request.SubsystemSummaries {
		result.Result.AIUses = append(result.Result.AIUses, summary.AIUses...)
		result.Result.AIUseFacts = append(result.Result.AIUseFacts, summary.AIUseFacts...)
		result.Result.ObjectiveObservations = append(result.Result.ObjectiveObservations, summary.ObjectiveObservations...)
		result.Result.UnmappedObservations = append(result.Result.UnmappedObservations, summary.UnmappedObservations...)
	}
	result.Result.UnresolvedQuestions = []string{"completed:" + request.Scope}
	return result, nil
}

func (reviewer *synthesisFragmentPrefetchReviewer) snapshot() (int, int, map[string]int, []string) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	calls := make(map[string]int, len(reviewer.summaryCalls))
	for signature, count := range reviewer.summaryCalls {
		calls[signature] = count
	}
	siblings := make([]string, 0, len(reviewer.initialSiblingInputs))
	for signature := range reviewer.initialSiblingInputs {
		siblings = append(siblings, signature)
	}
	return reviewer.providerCalls, reviewer.failedPartOneAttempts, calls, siblings
}

func TestSingletonSynthesisValidationFailureFragmentsSemanticsAndRetainsPrefetchedSiblings(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(20)
	reviewer := newSynthesisFragmentPrefetchReviewer()
	var sawValidationSplit bool

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: completeSynthesisCapacity(),
		OnProgress: func(value Progress) error {
			if value.Stage == "validation-split" && strings.Contains(value.Scope, "synthesis-level-1") {
				sawValidationSplit = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	providerCalls, rejected, summaryCalls, siblingInputs := reviewer.snapshot()
	if rejected != maxValidationRepairRetries+1 {
		t.Fatalf("rejected singleton synthesis attempts = %d, want %d", rejected, maxValidationRepairRetries+1)
	}
	if !sawValidationSplit {
		t.Fatal("persistent singleton synthesis validation failure did not enter semantic fragmentation")
	}
	if len(siblingInputs) == 0 {
		t.Fatal("fixture did not start a successful sibling synthesis group in the rejected group's concurrent wave")
	}
	for _, signature := range siblingInputs {
		if summaryCalls[signature] != 1 {
			t.Errorf("successful prefetched sibling input was submitted %d times, want exactly once", summaryCalls[signature])
		}
	}
	if result.Coverage.ProviderRequests != providerCalls {
		t.Fatalf("fragmentation provider request accounting = %d, want every call %d", result.Coverage.ProviderRequests, providerCalls)
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal {
		t.Fatalf("semantic-fragmentation run did not complete all source batches: %#v", result.Coverage)
	}
}

type retryCalibratingReviewer struct {
	mu sync.Mutex

	failedFirstSource bool
}

func (reviewer *retryCalibratingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 11, CompletionTokens: 3}, false)
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return result, nil
	}
	reviewer.mu.Lock()
	shouldFail := !reviewer.failedFirstSource
	if shouldFail {
		reviewer.failedFirstSource = true
	}
	reviewer.mu.Unlock()
	if shouldFail {
		return result, &providers.RemoteTransientError{Provider: "test", StatusCode: 503, Message: "temporary first-call failure", RetryAfter: time.Millisecond}
	}
	time.Sleep(10 * time.Millisecond)
	return result, nil
}

func TestUnknownCapacityDoesNotRampAfterLogicalBatchSucceedsOnlyAfterRetry(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(100)
	reviewer := &retryCalibratingReviewer{}
	completedSources := 0
	firstConcurrentWaveAfter := -1

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.Gemini, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return nil },
		OnProgress: func(value Progress) error {
			switch value.Stage {
			case "targeted-batch":
				completedSources++
			case "targeted-batch-concurrency":
				if firstConcurrentWaveAfter < 0 {
					firstConcurrentWaveAfter = completedSources
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstConcurrentWaveAfter < 0 {
		t.Fatal("fixture never reached a concurrent unknown-capacity source wave")
	}
	if firstConcurrentWaveAfter != 2 {
		t.Fatalf("first concurrent source wave began after %d cleanly completed logical batch(es), want 2: the retried first batch must not increase the slow-start window", firstConcurrentWaveAfter)
	}
	if result.Coverage.ProviderRequests <= result.Coverage.SourceBatchesTotal {
		t.Fatalf("retry fixture provider requests = %d, source batches = %d; retried attempt was not accounted", result.Coverage.ProviderRequests, result.Coverage.SourceBatchesTotal)
	}
}

type forcedSingleValidationReviewer struct {
	mu sync.Mutex

	requests []providers.RepositoryAnalysisRequest
}

func (reviewer *forcedSingleValidationReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	reviewer.mu.Unlock()
	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 29, CompletionTokens: 6, ReasoningTokens: 2}, request.Mode != providers.RepositoryAnalysisSynthesis)
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return result, &providers.RepositoryValidationError{Diagnostic: "forced-single synthesis did not preserve its validated source observation"}
	}
	return result, nil
}

func (reviewer *forcedSingleValidationReviewer) requestCount() int {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return len(reviewer.requests)
}

func TestForcedSinglePersistentSynthesisValidationFailureRetainsValidatedSourceFallback(t *testing.T) {
	reviewer := &forcedSingleValidationReviewer{}

	result, err := Run(context.Background(), reviewer, singleTargetedRepository(), nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatalf("forced-single validated-source fallback returned an incomplete error: %v", err)
	}
	if len(result.Result.AIUses) != 1 {
		t.Fatalf("forced-single fallback AI uses = %#v, want the validated source observation", result.Result.AIUses)
	}
	use := result.Result.AIUses[0]
	if len(use.MemberObservationIDs) != 1 || use.ID != inferredCandidateID(use.MemberObservationIDs) {
		t.Fatalf("forced-single fallback did not assign a trusted membership-derived ID: %#v", use)
	}
	if result.Coverage.SourceBatchesStarted != 1 || result.Coverage.SourceBatchesCompleted != 1 || result.Coverage.SourceBatchesTotal != 1 {
		t.Fatalf("forced-single fallback source coverage = %#v, want 1/1/1", result.Coverage)
	}
	if result.Coverage.ProviderRequests != reviewer.requestCount() || reviewer.requestCount() != 1+maxValidationRepairRetries+1 {
		t.Fatalf("forced-single fallback provider requests = %d/%d, want source plus %d synthesis attempts", result.Coverage.ProviderRequests, reviewer.requestCount(), maxValidationRepairRetries+1)
	}
	if len(result.Result.UnresolvedQuestions) != 0 {
		t.Fatalf("forced-single fallback was falsely marked incomplete: %#v", result.Result.UnresolvedQuestions)
	}
}

type forcedSingleTransientSynthesisReviewer struct {
	mu       sync.Mutex
	requests []providers.RepositoryAnalysisRequest
}

func (reviewer *forcedSingleTransientSynthesisReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	reviewer.mu.Unlock()
	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 17, CompletionTokens: 3}, request.Mode != providers.RepositoryAnalysisSynthesis)
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return result, &providers.RemoteTransientError{Provider: "test", StatusCode: 503, Message: "temporary synthesis outage"}
	}
	return result, nil
}

func TestForcedSingleTransientSynthesisFailureRetainsValidatedSourceResult(t *testing.T) {
	reviewer := &forcedSingleTransientSynthesisReviewer{}
	result, err := Run(context.Background(), reviewer, singleTargetedRepository(), nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUses) == 0 || result.Coverage.SourceBatchesCompleted != 1 || result.Coverage.SourceBatchesTotal != 1 {
		t.Fatalf("validated source fallback was not retained: %#v", result)
	}
	if !strings.Contains(strings.Join(result.Notes, "\n"), "optional synthesis did not complete") {
		t.Fatalf("fallback note missing: %#v", result.Notes)
	}
	if result.Coverage.ProviderRequests != 1+maxTransientRetryAttempts+1 {
		t.Fatalf("provider requests = %d, want source plus bounded synthesis attempts", result.Coverage.ProviderRequests)
	}
}
