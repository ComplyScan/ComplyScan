package repositoryanalysis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type validationRepairAccountingReviewer struct {
	requests []providers.RepositoryAnalysisRequest
}

type capacityProbeBackoffReviewer struct {
	mu               sync.Mutex
	waited           bool
	sourceBeforeWait bool
	sourceCalls      int
	waits            []time.Duration
}

func (reviewer *capacityProbeBackoffReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	if len(request.Files) > 0 {
		reviewer.sourceCalls++
		if !reviewer.waited {
			reviewer.sourceBeforeWait = true
		}
	}
	reviewer.mu.Unlock()
	return targetedSuccessfulResult(request, providers.Usage{PromptTokens: 10, CompletionTokens: 2}, false), nil
}

func (reviewer *capacityProbeBackoffReviewer) wait(_ context.Context, delay time.Duration) error {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.waited = true
	reviewer.waits = append(reviewer.waits, delay)
	return nil
}

func (reviewer *capacityProbeBackoffReviewer) snapshot() ([]time.Duration, int, bool) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return append([]time.Duration(nil), reviewer.waits...), reviewer.sourceCalls, reviewer.sourceBeforeWait
}

type parentSplitCoverageReviewer struct {
	mu       sync.Mutex
	requests []providers.RepositoryAnalysisRequest
}

func (reviewer *parentSplitCoverageReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	reviewer.mu.Unlock()
	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 10, CompletionTokens: 2}, false)
	if len(request.Files) > 1 {
		return result, &providers.RepositoryValidationError{Diagnostic: "split the rejected parent source batch"}
	}
	return result, nil
}

func (reviewer *parentSplitCoverageReviewer) callCount() int {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return len(reviewer.requests)
}

func (reviewer *validationRepairAccountingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	call := len(reviewer.requests)
	usage := providers.Usage{PromptTokens: 100 * call, CompletionTokens: 10 * call, ReasoningTokens: call, TotalDurationNS: int64(call)}
	result := targetedSuccessfulResult(request, usage, false)
	result.RateLimits = providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 100, RemainingRequests: 100 - call,
		TokensKnown: true, LimitTokens: 100_000, RemainingTokens: 100_000 - usage.PromptTokens - usage.CompletionTokens,
	}
	if call == 1 {
		return result, &providers.RepositoryValidationError{Diagnostic: "citation app.py:99 is outside submitted lines 1-4"}
	}
	return result, nil
}

func TestReviewRepositoryWithRetryRepairsValidationWithoutChangingEvidenceAndCountsEveryAttempt(t *testing.T) {
	reviewer := &validationRepairAccountingReviewer{}
	request := providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisTargeted, Scope: "batch-1", RepositoryFiles: 1, RepositoryBytes: 42,
		Files: []providers.RepositorySourceFile{{Path: "app.py", Kind: "source", ContentStartLine: 1, LineCount: 4, Content: "model()\n"}},
		Graph: providers.RepositoryGraphContext{Relationships: []providers.RepositoryGraphRelationship{{Kind: "call", From: "app.generate", To: "model", Path: "app.py", Line: 1}}},
	}
	var progressEvents []Progress
	result, err := reviewRepositoryWithRetry(context.Background(), reviewer, request, Options{
		OnProgress: func(value Progress) error {
			progressEvents = append(progressEvents, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 2 {
		t.Fatalf("provider calls = %d, want rejected response plus corrective regeneration", len(reviewer.requests))
	}
	first, repaired := reviewer.requests[0], reviewer.requests[1]
	if first.ValidationFeedback != "" || repaired.ValidationFeedback != "citation app.py:99 is outside submitted lines 1-4" {
		t.Fatalf("validation feedback sequence = %q then %q", first.ValidationFeedback, repaired.ValidationFeedback)
	}
	repaired.ValidationFeedback = ""
	if !reflect.DeepEqual(first, repaired) {
		t.Fatalf("corrective regeneration changed trusted evidence\nfirst: %#v\nrepair: %#v", first, repaired)
	}
	if len(progressEvents) != 1 || progressEvents[0].Stage != "validation-repair" || progressEvents[0].Completed != 1 || progressEvents[0].Total != maxValidationRepairRetries {
		t.Fatalf("validation repair was not scheduler-visible: %#v", progressEvents)
	}
	wantUsage := providers.Usage{PromptTokens: 300, CompletionTokens: 30, ReasoningTokens: 3, TotalDurationNS: 3}
	if !reflect.DeepEqual(result.Usage, wantUsage) {
		t.Fatalf("repair usage = %#v, want every metered response %#v", result.Usage, wantUsage)
	}
	if result.Coverage.FilesSubmitted != 2 || result.Coverage.BytesSubmitted != 2*int64(len("model()\n")) || result.Coverage.ProviderRequests != 2 {
		t.Fatalf("repair transfer/request accounting = %#v, want two exact submissions", result.Coverage)
	}
	if result.RateLimits.RemainingRequests != 98 {
		t.Fatalf("repair did not retain the newest capacity snapshot: %#v", result.RateLimits)
	}
}

func TestSourceBatchWaveLimitTreatsPartialCapacityAsSequential(t *testing.T) {
	tests := []struct {
		name     string
		snapshot providers.RateLimitSnapshot
	}{
		{
			name: "request limit only",
			snapshot: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
			},
		},
		{
			name: "token limit only",
			snapshot: providers.RateLimitSnapshot{
				TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 900_000, ResetTokens: time.Minute,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limit, wait := sourceBatchWaveLimit(test.snapshot, 10_000, 20, 16)
			if limit != 1 || wait != 0 {
				t.Fatalf("partial capacity produced wave %d and wait %s, want one request and no wait", limit, wait)
			}
		})
	}
}

func TestTemporaryCapacityProbeBackoffPrecedesFirstSourceTransfer(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		limits providers.RateLimitSnapshot
		want   time.Duration
	}{
		{
			name: "exhausted request header waits for reset",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 0, ResetRequests: 45 * time.Second,
			},
			want: 45 * time.Second,
		},
		{name: "missing timing metadata uses retry floor", want: minimumRateLimitCooldown},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, _ := exhaustiveCandidateRepository(30)
			reviewer := &capacityProbeBackoffReviewer{}
			result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
				Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
				ProbeRateLimits: func(context.Context) (CapacityProbeResult, error) {
					return CapacityProbeResult{RateLimits: testCase.limits, ProviderRequests: 1}, &providers.RemoteRateLimitError{
						Provider: "OpenAI", Message: "temporary provider capacity limit", RateLimits: testCase.limits,
					}
				},
				Wait: reviewer.wait,
			})
			if err != nil {
				t.Fatal(err)
			}
			waits, sourceCalls, sourceBeforeWait := reviewer.snapshot()
			if sourceCalls == 0 {
				t.Fatal("fixture made no repository source request")
			}
			if sourceBeforeWait {
				t.Fatal("repository source reached the reviewer before the temporary capacity-probe backoff")
			}
			if len(waits) != 1 || waits[0] != testCase.want {
				t.Fatalf("capacity-probe waits = %v, want [%s]", waits, testCase.want)
			}
			if result.Coverage.ProviderRequests <= 1 {
				t.Fatalf("provider request accounting = %#v, want probe plus repository requests", result.Coverage)
			}
		})
	}
}

func TestAdaptiveParentSplitDoesNotLeakObsoleteStartedBatchIntoLeafCoverage(t *testing.T) {
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "pkg/one.go", Kind: discovery.KindSource, Content: []byte("package pkg\nfunc one() { model() }\n"), Size: 35},
		{Path: "pkg/two.go", Kind: discovery.KindSource, Content: []byte("package pkg\nfunc two() { model() }\n"), Size: 35},
	}}
	files := repositoryFiles(repository)
	reviewer := &parentSplitCoverageReviewer{}
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), files, nil, nil, nil, 100_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true,
		retryGate: make(chan struct{}, 1), requestBudget: &providerRequestBudget{limit: 4},
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("safety ceiling of %d provider requests", MaxProviderRequestsPerRun)) {
		t.Fatalf("adaptive child failure = %v, want provider request safety-ceiling failure", err)
	}
	if calls := reviewer.callCount(); calls != 4 {
		t.Fatalf("reviewer calls = %d, want three rejected parent attempts plus one completed child", calls)
	}
	if result.Coverage.SourceBatchesStarted != 1 || result.Coverage.SourceBatchesCompleted != 1 || result.Coverage.SourceBatchesTotal != 2 {
		t.Fatalf("adaptive leaf coverage = %#v, want started/completed/total 1/1/2", result.Coverage)
	}
	partialWording := strings.Join(append(append([]string(nil), result.Result.UnresolvedQuestions...), result.Notes...), " ")
	if strings.Contains(partialWording, "every planned source batch started a provider request") {
		t.Fatalf("partial result incorrectly claimed every replacement leaf started: %q", partialWording)
	}
	if !strings.Contains(partialWording, "1 distinct source batch(es) started a provider request") {
		t.Fatalf("partial result did not describe exact replacement-leaf coverage: %q", partialWording)
	}
}

func TestRunTargetedWithoutCapacityHeadersSlowStartsOneThenTwoThenFour(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(100)
	reviewer := &rateAwareConcurrentReviewer{}
	var waveMu sync.Mutex
	var concurrentWaveSizes []int
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.Gemini, Model: "test", MaxInputTokens: 8_000,
		OnProgress: func(value Progress) error {
			if value.Stage == "targeted-batch-concurrency" {
				waveMu.Lock()
				concurrentWaveSizes = append(concurrentWaveSizes, value.Completed)
				waveMu.Unlock()
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls, maximum := reviewer.counts()
	if calls < 7 {
		t.Fatalf("fixture produced %d source calls, want enough to exercise 1, 2, and 4 request waves", calls)
	}
	waveMu.Lock()
	gotWaves := append([]int(nil), concurrentWaveSizes...)
	waveMu.Unlock()
	if len(gotWaves) < 2 || gotWaves[0] != 2 || gotWaves[1] != 4 {
		t.Fatalf("unknown-capacity concurrent waves = %v, want initial solo calibration followed by 2 then 4", gotWaves)
	}
	if maximum < 4 {
		t.Fatalf("maximum source concurrency = %d, want slow start to reach four", maximum)
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal {
		t.Fatalf("slow-start run did not finish every source batch: %#v", result.Coverage)
	}
}

func TestRunTargetedCancelledCapacityProbeStartsNoSourceRequests(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(30)
	reviewer := &rateAwareConcurrentReviewer{}
	probeUsage := providers.Usage{PromptTokens: 17, CompletionTokens: 2}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.Anthropic, Model: "test", MaxInputTokens: 8_000,
		ProbeRateLimits: func(context.Context) (CapacityProbeResult, error) {
			return CapacityProbeResult{Usage: probeUsage, ProviderRequests: 1}, context.Canceled
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("capacity probe error = %v, want context cancellation", err)
	}
	if calls, _ := reviewer.counts(); calls != 0 {
		t.Fatalf("canceled capacity probe was followed by %d source request(s)", calls)
	}
	if result.Coverage.ProviderRequests != 1 || !reflect.DeepEqual(result.Usage, probeUsage) {
		t.Fatalf("canceled metered probe accounting = coverage %#v usage %#v", result.Coverage, result.Usage)
	}
}

func TestPermanentOrGenericCapacityProbeFailureSendsNoRepositorySource(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{
			name: "permanent quota",
			err:  &providers.RemoteRateLimitError{Provider: "OpenAI", Message: "insufficient_quota", Permanent: true},
		},
		{name: "generic configuration failure", err: errors.New("provider authentication failed")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, _ := exhaustiveCandidateRepository(30)
			reviewer := &rateAwareConcurrentReviewer{}
			probeUsage := providers.Usage{PromptTokens: 19, CompletionTokens: 3, ReasoningTokens: 1}
			result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
				Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
				ProbeRateLimits: func(context.Context) (CapacityProbeResult, error) {
					return CapacityProbeResult{Usage: probeUsage, ProviderRequests: 1}, testCase.err
				},
			})
			if err == nil || !strings.Contains(err.Error(), "source-free provider check failed before repository code was sent") {
				t.Fatalf("capacity probe error = %v", err)
			}
			if calls, _ := reviewer.counts(); calls != 0 {
				t.Fatalf("non-retryable capacity probe was followed by %d source request(s)", calls)
			}
			if result.Coverage.ProviderRequests != 1 || result.Coverage.FilesSubmitted != 0 || result.Coverage.BytesSubmitted != 0 || !reflect.DeepEqual(result.Usage, probeUsage) {
				t.Fatalf("failed probe accounting = coverage %#v usage %#v", result.Coverage, result.Usage)
			}
			if len(result.Result.AIUses) != 0 || len(result.Result.UnresolvedQuestions) == 0 || len(result.Notes) == 0 {
				t.Fatalf("failed probe result is not coverage-only/incomplete: %#v", result)
			}
		})
	}
}

func TestTemporaryCapacityProbeFailureFallsBackToConservativeSourceScheduling(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{
			name: "temporary 429",
			err:  &providers.RemoteRateLimitError{Provider: "OpenAI", Message: "requests per minute", RetryAfter: time.Second},
		},
		{
			name: "temporary 503",
			err:  &providers.RemoteTransientError{Provider: "Anthropic", StatusCode: 503, Message: "overloaded", RetryAfter: time.Second},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, _ := exhaustiveCandidateRepository(30)
			reviewer := &rateAwareConcurrentReviewer{}
			probeUsage := providers.Usage{PromptTokens: 23, CompletionTokens: 4}
			fallbacks := 0
			result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
				Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
				ProbeRateLimits: func(context.Context) (CapacityProbeResult, error) {
					return CapacityProbeResult{Usage: probeUsage, ProviderRequests: 1}, testCase.err
				},
				OnProgress: func(value Progress) error {
					if value.Stage == "rate-limit-probe-fallback" {
						fallbacks++
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			calls, maximum := reviewer.counts()
			if calls == 0 || maximum == 0 || fallbacks != 1 {
				t.Fatalf("temporary probe fallback source calls/max/fallbacks = %d/%d/%d", calls, maximum, fallbacks)
			}
			if result.Coverage.ProviderRequests <= 1 || result.Coverage.FilesSubmitted == 0 || result.Usage.PromptTokens != probeUsage.PromptTokens || result.Usage.CompletionTokens != probeUsage.CompletionTokens {
				t.Fatalf("temporary probe fallback accounting = coverage %#v usage %#v", result.Coverage, result.Usage)
			}
		})
	}
}

type concurrentRateRetryReviewer struct {
	mu               sync.Mutex
	attempts         map[string]int
	activeRetryCalls int
	maximumRetries   int
}

func (reviewer *concurrentRateRetryReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.attempts[request.Scope]++
	attempt := reviewer.attempts[request.Scope]
	if attempt > 1 {
		reviewer.activeRetryCalls++
		if reviewer.activeRetryCalls > reviewer.maximumRetries {
			reviewer.maximumRetries = reviewer.activeRetryCalls
		}
	}
	reviewer.mu.Unlock()

	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 10, CompletionTokens: 1}, false)
	if attempt == 1 {
		return result, &providers.RemoteRateLimitError{Provider: "test", Message: "temporary quota", RetryAfter: time.Millisecond}
	}
	time.Sleep(25 * time.Millisecond)
	reviewer.mu.Lock()
	reviewer.activeRetryCalls--
	reviewer.mu.Unlock()
	return result, nil
}

func (reviewer *concurrentRateRetryReviewer) retryMaximum() int {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return reviewer.maximumRetries
}

func TestConcurrentRetryWaitsRemainParallelWhileProviderRetriesUseSharedGate(t *testing.T) {
	reviewer := &concurrentRateRetryReviewer{attempts: make(map[string]int)}
	calls := []repositorySourceBatchCall{
		{chunk: repositoryChunk{scope: "batch-a"}, request: providers.RepositoryAnalysisRequest{Mode: providers.RepositoryAnalysisTargeted, Scope: "batch-a", Files: []providers.RepositorySourceFile{{Path: "a.go", Content: "a"}}}},
		{chunk: repositoryChunk{scope: "batch-b"}, request: providers.RepositoryAnalysisRequest{Mode: providers.RepositoryAnalysisTargeted, Scope: "batch-b", Files: []providers.RepositorySourceFile{{Path: "b.go", Content: "b"}}}},
	}
	var waitMu sync.Mutex
	waitsEntered := 0
	releaseWaits := make(chan struct{})
	options := Options{
		retryGate: make(chan struct{}, 1),
		Wait: func(ctx context.Context, _ time.Duration) error {
			waitMu.Lock()
			waitsEntered++
			if waitsEntered == len(calls) {
				close(releaseWaits)
			}
			waitMu.Unlock()
			select {
			case <-releaseWaits:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return errors.New("retry waits were serialized")
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	responses := runRepositorySourceBatchWave(ctx, reviewer, calls, options)
	for _, response := range responses {
		if response.err != nil {
			t.Fatalf("batch %s retry failed: %v", response.scope, response.err)
		}
		if response.result.Coverage.ProviderRequests != 2 {
			t.Fatalf("batch %s provider requests = %d, want initial plus retry", response.scope, response.result.Coverage.ProviderRequests)
		}
	}
	waitMu.Lock()
	gotWaits := waitsEntered
	waitMu.Unlock()
	if gotWaits != len(calls) {
		t.Fatalf("concurrent waits entered = %d, want %d", gotWaits, len(calls))
	}
	if maximum := reviewer.retryMaximum(); maximum != 1 {
		t.Fatalf("maximum simultaneous provider retry calls = %d, want shared gate capacity 1", maximum)
	}
}

type permanentQuotaReviewer struct {
	calls int
}

func (reviewer *permanentQuotaReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.calls++
	return targetedSuccessfulResult(request, providers.Usage{PromptTokens: 12, CompletionTokens: 3}, false), &providers.RemoteRateLimitError{
		Provider: "OpenAI", Message: "insufficient_quota", Permanent: true,
	}
}

func TestPermanentQuotaFailureReturnsWithoutWaitOrRetry(t *testing.T) {
	reviewer := &permanentQuotaReviewer{}
	waits := 0
	result, err := reviewRepositoryWithRetry(context.Background(), reviewer, providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisTargeted, Scope: "batch", Files: []providers.RepositorySourceFile{{Path: "app.go", Content: "model()"}},
	}, Options{Wait: func(context.Context, time.Duration) error {
		waits++
		return nil
	}})
	rateLimit, ok := providers.AsRemoteRateLimitError(err)
	if !ok || !rateLimit.Permanent {
		t.Fatalf("permanent quota error = %#v", err)
	}
	if reviewer.calls != 1 || waits != 0 {
		t.Fatalf("permanent quota made %d provider call(s) and %d wait(s), want immediate return", reviewer.calls, waits)
	}
	if result.Coverage.ProviderRequests != 1 || result.Coverage.FilesSubmitted != 1 || result.Usage.PromptTokens != 12 || result.Usage.CompletionTokens != 3 {
		t.Fatalf("permanent quota attempt accounting = %#v", result)
	}
}

type requestCeilingReviewer struct {
	mu       sync.Mutex
	attempts map[string]int
	requests []providers.RepositoryAnalysisRequest
}

func (reviewer *requestCeilingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.attempts[request.Scope]++
	attempt := reviewer.attempts[request.Scope]
	reviewer.requests = append(reviewer.requests, request)
	reviewer.mu.Unlock()

	result := targetedSuccessfulResult(request, providers.Usage{PromptTokens: 1, CompletionTokens: 2}, true)
	if request.Scope == "scope000" {
		switch attempt {
		case 1, 3:
			return result, &providers.RepositoryValidationError{Diagnostic: fmt.Sprintf("discard invalid structured response %d", attempt)}
		case 2, 4:
			return result, &providers.RemoteTransientError{Provider: "test", StatusCode: 503, Message: "temporary outage", RetryAfter: time.Millisecond}
		}
	}
	return result, nil
}

func (reviewer *requestCeilingReviewer) recordedRequests() []providers.RepositoryAnalysisRequest {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return append([]providers.RepositoryAnalysisRequest(nil), reviewer.requests...)
}

func TestHierarchicalRunNeverExceedsProviderRequestSafetyCeiling(t *testing.T) {
	repository := discovery.Repository{Root: "."}
	for index := 0; index < MaxProviderRequestsPerRun+44; index++ {
		content := []byte("package ai\nfunc invoke() { model() }\n")
		repository.Files = append(repository.Files, discovery.File{
			Path: fmt.Sprintf("scope%03d/use.go", index), Kind: discovery.KindSource, Content: content, Size: int64(len(content)),
		})
	}
	files := repositoryFiles(repository)
	reviewer := &requestCeilingReviewer{attempts: make(map[string]int)}
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), files, nil, nil, nil, 8_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true,
		Wait:          func(context.Context, time.Duration) error { return nil },
		retryGate:     make(chan struct{}, 1),
		requestBudget: &providerRequestBudget{limit: MaxProviderRequestsPerRun},
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 10_000, RemainingRequests: 10_000,
			TokensKnown: true, LimitTokens: 100_000_000, RemainingTokens: 100_000_000,
		},
	})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("safety ceiling of %d provider requests", MaxProviderRequestsPerRun)) {
		t.Fatalf("request-ceiling error = %v", err)
	}
	requests := reviewer.recordedRequests()
	if len(requests) != MaxProviderRequestsPerRun {
		t.Fatalf("provider attempts = %d, want hard ceiling %d", len(requests), MaxProviderRequestsPerRun)
	}
	wantFiles := 0
	var wantBytes int64
	for _, request := range requests {
		wantFiles += len(request.Files)
		wantBytes += repositorySourceContentBytes(request.Files)
	}
	if result.Coverage.ProviderRequests != MaxProviderRequestsPerRun || result.Coverage.FilesSubmitted != wantFiles || result.Coverage.BytesSubmitted != wantBytes {
		t.Fatalf("ceiling coverage = %#v, want requests/files/bytes %d/%d/%d", result.Coverage, MaxProviderRequestsPerRun, wantFiles, wantBytes)
	}
	if result.Usage.PromptTokens != MaxProviderRequestsPerRun || result.Usage.CompletionTokens != 2*MaxProviderRequestsPerRun {
		t.Fatalf("ceiling usage = %#v, want all %d metered attempts", result.Usage, MaxProviderRequestsPerRun)
	}
	if len(result.Result.AIUses) != 0 || len(result.Result.AIUseFacts) != 0 || len(result.Result.ObjectiveObservations) != 0 || len(result.Result.UnmappedObservations) != 0 {
		t.Fatalf("request-ceiling result leaked unsynthesized semantics: %#v", result.Result)
	}
	if len(result.Result.UnresolvedQuestions) == 0 || len(result.Notes) == 0 || result.Coverage.SourceBatchesCompleted >= result.Coverage.SourceBatchesTotal {
		t.Fatalf("request-ceiling result is not explicitly incomplete: %#v", result)
	}
	if reviewer.attempts["scope000"] != 5 {
		t.Fatalf("mixed validation/transient retry fixture made %d scope000 attempts, want 5", reviewer.attempts["scope000"])
	}
}

func TestRunHierarchicalStripsOversizedNonGroupingDetailBeforeSynthesis(t *testing.T) {
	content := "package ai\n// " + strings.Repeat("bounded evidence ", 430) + "\n"
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "a/use.go", Kind: discovery.KindSource, Content: []byte(content), Size: int64(len(content))},
		{Path: "a/model.go", Kind: discovery.KindSource, Content: []byte(content), Size: int64(len(content))},
	}}
	files := repositoryFiles(repository)
	chunks, err := partitionRepository(files, 16_000*80/100)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("fixture partitions = %d, want exactly two source summaries", len(chunks))
	}
	reviewer := &compactingSynthesisReviewer{}
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), files, nil, nil, nil, 16_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.singleSummaryPasses != 0 {
		t.Fatalf("singleton compaction passes = %d, want none after local compacting", reviewer.singleSummaryPasses)
	}
	if reviewer.combinedSummaryPasses != 1 {
		t.Fatalf("combined grouping passes = %d, want one compact synthesis", reviewer.combinedSummaryPasses)
	}
	if result.Coverage.SourceBatchesCompleted != 2 || result.Coverage.SourceBatchesTotal != 2 {
		t.Fatalf("two-summary recovery coverage = %#v", result.Coverage)
	}
}

type tokenLimitedRecoveryReviewer struct {
	requests []providers.RepositoryAnalysisRequest
	failed   bool
}

func (reviewer *tokenLimitedRecoveryReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	usage := providers.Usage{PromptTokens: 3_000, CompletionTokens: request.MaxOutputTokens}
	result := targetedSuccessfulResult(request, usage, false)
	if len(request.Files) > 0 && !reviewer.failed {
		reviewer.failed = true
		return result, &providers.RemoteIncompleteError{
			Provider: "test", Status: "incomplete", Reason: "max_output_tokens",
			InputTokens: 3_000, OutputTokens: request.MaxOutputTokens, TokenLimit: 5_000,
		}
	}
	return result, nil
}

func TestHierarchicalOutputRecoveryHonorsIncompleteResponseTokenLimit(t *testing.T) {
	content := "package ai\nfunc call() { model() }\n"
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "ai/a.go", Kind: discovery.KindSource, Content: []byte(content), Size: int64(len(content))},
		{Path: "ai/b.go", Kind: discovery.KindSource, Content: []byte(content), Size: int64(len(content))},
	}}
	files := repositoryFiles(repository)
	reviewer := &tokenLimitedRecoveryReviewer{}
	_, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), files, nil, nil, nil, 8_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) < 2 {
		t.Fatalf("provider requests = %d, want bounded recovery after incomplete response", len(reviewer.requests))
	}
	const availableOutput = 2_000
	for index, request := range reviewer.requests[1:] {
		if len(request.Files) > 0 && request.MaxOutputTokens > availableOutput {
			t.Errorf("recovery source request %d output allowance = %d, exceeds TokenLimit-InputTokens = %d", index+2, request.MaxOutputTokens, availableOutput)
		}
	}
	if t.Failed() {
		t.Fatalf("token-limited request sequence: %s", formatRecoveryRequests(reviewer.requests))
	}
}

func formatRecoveryRequests(requests []providers.RepositoryAnalysisRequest) string {
	parts := make([]string, 0, len(requests))
	for _, request := range requests {
		parts = append(parts, fmt.Sprintf("%s:%s files=%d output=%d", request.Mode, request.Scope, len(request.Files), request.MaxOutputTokens))
	}
	return strings.Join(parts, "; ")
}
