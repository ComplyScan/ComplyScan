package repositoryanalysis

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type rateAwareConcurrentReviewer struct {
	mu             sync.Mutex
	rateLimits     providers.RateLimitSnapshot
	activeSources  int
	maximumSources int
	sourceCalls    int
}

type parallelAdaptiveReviewer struct {
	mu           sync.Mutex
	failed       bool
	sourceScopes map[string]int
	rateLimits   providers.RateLimitSnapshot
}

func (reviewer *parallelAdaptiveReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	result := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", RateLimits: reviewer.rateLimits,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files)},
		Result: providers.RepositorySectionResult{
			Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return result, nil
	}
	reviewer.mu.Lock()
	reviewer.sourceScopes[request.Scope]++
	shouldFail := !reviewer.failed && strings.HasSuffix(request.Scope, "(part 1)")
	if shouldFail {
		reviewer.failed = true
	}
	reviewer.mu.Unlock()
	if shouldFail {
		return result, &providers.RemoteRateLimitError{
			Provider: "OpenAI", Message: "request too large", LimitTokens: 10_000, RequestedTokens: 12_000, RequestTooLarge: true, RateLimits: reviewer.rateLimits,
		}
	}
	return result, nil
}

func (reviewer *rateAwareConcurrentReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	result := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", RateLimits: reviewer.rateLimits,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files)},
		Result: providers.RepositorySectionResult{
			Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return result, nil
	}

	reviewer.mu.Lock()
	reviewer.sourceCalls++
	reviewer.activeSources++
	if reviewer.activeSources > reviewer.maximumSources {
		reviewer.maximumSources = reviewer.activeSources
	}
	reviewer.mu.Unlock()
	time.Sleep(25 * time.Millisecond)
	reviewer.mu.Lock()
	reviewer.activeSources--
	reviewer.mu.Unlock()
	return result, nil
}

func (reviewer *rateAwareConcurrentReviewer) counts() (int, int) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return reviewer.sourceCalls, reviewer.maximumSources
}

func TestRunTargetedUsesObservedProviderLimitsForSourceBatchConcurrency(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		limits      providers.RateLimitSnapshot
		initial     bool
		probe       bool
		wantMaximum int
		wantAtLeast int
		wantAll     bool
		wantRestAll bool
		wantProbes  int
	}{
		{
			name: "live qualification capacity starts the complete queue together",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
				TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 490_000, ResetTokens: time.Minute,
			},
			initial: true, wantAtLeast: 3, wantAll: true,
		},
		{
			name: "source-free capacity probe starts the complete queue together",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
				TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 490_000, ResetTokens: time.Minute,
			},
			probe: true, wantAtLeast: 3, wantAll: true, wantProbes: 1,
		},
		{
			name: "high capacity runs the remaining queue concurrently",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
				TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 490_000, ResetTokens: time.Minute,
			},
			wantAtLeast: 3, wantRestAll: true,
		},
		{
			name: "request allowance bounds each wave",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 2, RemainingRequests: 2, ResetRequests: time.Minute,
			},
			wantMaximum: 2, wantAtLeast: 2,
		},
		{name: "missing headers preserve sequential execution", wantMaximum: 1, wantAtLeast: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository, _ := exhaustiveCandidateRepository(30)
			reviewer := &rateAwareConcurrentReviewer{rateLimits: testCase.limits}
			probes := 0
			options := Options{
				Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
				Wait: func(context.Context, time.Duration) error { return nil },
			}
			if testCase.initial {
				options.InitialRateLimits = testCase.limits
			}
			if testCase.probe {
				options.ProbeRateLimits = func(context.Context) (providers.RateLimitSnapshot, error) {
					probes++
					return testCase.limits, nil
				}
			}
			result, err := Run(context.Background(), reviewer, repository, nil, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			calls, maximum := reviewer.counts()
			if calls < 3 {
				t.Fatalf("source calls = %d, want a multi-batch fixture", calls)
			}
			if maximum < testCase.wantAtLeast {
				t.Fatalf("maximum concurrent source calls = %d, want at least %d", maximum, testCase.wantAtLeast)
			}
			if testCase.wantMaximum > 0 && maximum > testCase.wantMaximum {
				t.Fatalf("maximum concurrent source calls = %d, provider allowance = %d", maximum, testCase.wantMaximum)
			}
			if testCase.wantAll && maximum != calls {
				t.Fatalf("maximum concurrent source calls = %d, total calls = %d; live qualification capacity should start the complete queue", maximum, calls)
			}
			if testCase.wantRestAll && maximum != calls-1 {
				t.Fatalf("maximum concurrent source calls = %d, total calls = %d; the first calibration call should be followed by one complete concurrent wave", maximum, calls)
			}
			if result.Coverage.SourceBatchesCompleted != calls || result.Coverage.SourceBatchesTotal != calls {
				t.Fatalf("batch coverage = %d/%d, source calls = %d", result.Coverage.SourceBatchesCompleted, result.Coverage.SourceBatchesTotal, calls)
			}
			if probes != testCase.wantProbes {
				t.Fatalf("source-free capacity probes = %d, want %d", probes, testCase.wantProbes)
			}
		})
	}
}

func TestRunTargetedRunsLimitedWavesAndWaitsForProviderReset(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(30)
	reviewer := &rateAwareConcurrentReviewer{}
	waits := 0
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 3, RemainingRequests: 3, ResetRequests: 20 * time.Millisecond,
		},
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls, maximum := reviewer.counts()
	if calls < 4 {
		t.Fatalf("source calls = %d, want enough batches to require more than one wave", calls)
	}
	if maximum != 3 {
		t.Fatalf("maximum concurrent source calls = %d, want provider allowance 3", maximum)
	}
	wantWaits := (calls - 1) / 3
	if waits != wantWaits {
		t.Fatalf("capacity waits = %d, want %d for %d source calls in waves of 3", waits, wantWaits, calls)
	}
	if result.Coverage.SourceBatchesCompleted != calls || result.Coverage.SourceBatchesTotal != calls {
		t.Fatalf("batch coverage = %d/%d, source calls = %d", result.Coverage.SourceBatchesCompleted, result.Coverage.SourceBatchesTotal, calls)
	}
}

func TestRunTargetedDoesNotProbeCapacityForOneSourcePackage(t *testing.T) {
	reviewer := &rateAwareConcurrentReviewer{}
	probes := 0
	_, err := Run(context.Background(), reviewer, singleTargetedRepository(), nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		ProbeRateLimits: func(context.Context) (providers.RateLimitSnapshot, error) {
			probes++
			return providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if probes != 0 {
		t.Fatalf("source-free capacity probes = %d, want 0 for a one-package review", probes)
	}
}

func TestRunTargetedAdaptiveRetryDoesNotResubmitConcurrentPrefetches(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(30)
	limits := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
		TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 490_000, ResetTokens: time.Minute,
	}
	reviewer := &parallelAdaptiveReviewer{sourceScopes: make(map[string]int), rateLimits: limits}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000, InitialRateLimits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reviewer.failed {
		t.Fatal("fixture did not trigger the first-batch adaptive retry")
	}
	for scope, calls := range reviewer.sourceScopes {
		want := 1
		if strings.HasSuffix(scope, "(part 1)") {
			want = 2
		}
		if calls != want {
			t.Errorf("source scope %q was submitted %d time(s), want %d", scope, calls, want)
		}
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal {
		t.Fatalf("adaptive concurrent run coverage = %#v", result.Coverage)
	}
}

func TestSourceBatchWaveLimitUsesBothRequestAndTokenCapacity(t *testing.T) {
	snapshot := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 12, ResetRequests: 20 * time.Second,
		TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 25_000, ResetTokens: 45 * time.Second,
	}
	if limit, wait := sourceBatchWaveLimit(snapshot, 10_000, 13); limit != 2 || wait != 0 {
		t.Fatalf("token-bounded wave = %d, wait %s; want 2, 0", limit, wait)
	}
	snapshot.RemainingTokens = 5_000
	if limit, wait := sourceBatchWaveLimit(snapshot, 10_000, 13); limit != 0 || wait != 45*time.Second {
		t.Fatalf("exhausted token wave = %d, wait %s; want 0, 45s", limit, wait)
	}
	snapshot.RemainingTokens = 25_000
	snapshot.RemainingRequests = 0
	if limit, wait := sourceBatchWaveLimit(snapshot, 10_000, 13); limit != 0 || wait != 20*time.Second {
		t.Fatalf("exhausted request wave = %d, wait %s; want 0, 20s", limit, wait)
	}
}

func TestConservativeRateLimitSnapshotPreservesIndependentlyReportedDimensions(t *testing.T) {
	requests := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 10, ResetRequests: 20 * time.Second,
	}
	tokens := providers.RateLimitSnapshot{
		TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 250_000, ResetTokens: 45 * time.Second,
	}
	combined := conservativeRateLimitSnapshot(requests, tokens)
	if !combined.RequestsKnown || combined.RemainingRequests != 10 || !combined.TokensKnown || combined.RemainingTokens != 250_000 {
		t.Fatalf("independently reported dimensions were lost: %#v", combined)
	}
	newer := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 15, ResetRequests: 10 * time.Second,
		TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 300_000, ResetTokens: 30 * time.Second,
	}
	combined = conservativeRateLimitSnapshot(combined, newer)
	if combined.LimitRequests != 500 || combined.RemainingRequests != 10 || combined.ResetRequests != 20*time.Second || combined.LimitTokens != 500_000 || combined.RemainingTokens != 250_000 || combined.ResetTokens != 45*time.Second {
		t.Fatalf("capacity snapshot was not conservatively merged: %#v", combined)
	}
}
