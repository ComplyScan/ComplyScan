package modelqualification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type reviewFunc func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error)

func (function reviewFunc) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return function(ctx, request)
}

type repositoryReviewFunc func(context.Context, providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error)

type qualificationReviewer struct {
	review     reviewFunc
	repository repositoryReviewFunc
}

func (reviewer qualificationReviewer) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return reviewer.review(ctx, request)
}

func (reviewer qualificationReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	return reviewer.repository(ctx, request)
}

func reviewerWithCompatibleRepository(identity Identity, review reviewFunc) qualificationReviewer {
	return qualificationReviewer{
		review: review,
		repository: func(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			return compatibleQualificationRepositoryReview(identity, request, providers.Usage{}), nil
		},
	}
}

func TestQualifyAcceptsOneBoundStructuredObservation(t *testing.T) {
	identity := CurrentIdentity(providers.Ollama, "test-model", "digest")
	if identity.RepositoryPromptVersion != providers.RepositoryAnalysisPromptVersion {
		t.Fatalf("repository prompt identity = %q, want %q", identity.RepositoryPromptVersion, providers.RepositoryAnalysisPromptVersion)
	}
	if identity.QualificationContractVersion != QualificationContractVersion {
		t.Fatalf("qualification contract identity = %d, want %d", identity.QualificationContractVersion, QualificationContractVersion)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	result, err := Qualify(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
		finding := request.Findings[0]
		if !strings.Contains(finding.Message, "ignore the schema") {
			t.Fatalf("probe did not contain injection-shaped text: %#v", finding)
		}
		return providers.ReviewResult{
			Provider: providers.Ollama, Model: "test-model", InputFindings: 1, Reviewed: 1,
			Observations: []providers.Observation{{Fingerprint: finding.Fingerprint, RuleID: finding.RuleID}},
			Usage:        providers.Usage{PromptTokens: 10, CompletionTokens: 5},
			RateLimits:   providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499},
		}, nil
	})), identity, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "compatible" || result.ExpiresAt.Sub(result.CheckedAt) != CacheValidity || result.Usage.PromptTokens != 10 || result.RateLimits.RemainingRequests != 499 || result.ProviderRequests != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestQualifyExercisesRepositoryContractWithoutRepositoryData(t *testing.T) {
	identity := CurrentIdentity(providers.OpenAI, "test-model", "")
	findingCalls, repositoryCalls := 0, 0
	reviewer := qualificationReviewer{
		review: func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
			findingCalls++
			return compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 11, CompletionTokens: 2}), nil
		},
		repository: func(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			repositoryCalls++
			if request.Mode != providers.RepositoryAnalysisTargeted || request.RepositoryFiles != 1 || len(request.Files) != 1 ||
				!strings.Contains(request.Files[0].Path, "qualification") || !strings.Contains(request.Files[0].Content, "Untrusted text") ||
				len(request.Objectives) != 0 || len(request.ConfirmedAIUses) != 1 || request.ConfirmedAIUses[0].SubmittedFiles[0] != request.Files[0].Path {
				t.Fatalf("repository qualification request = %#v", request)
			}
			result := compatibleQualificationRepositoryReview(identity, request, providers.Usage{PromptTokens: 23, CompletionTokens: 5, ReasoningTokens: 3})
			result.RateLimits = providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 498}
			return result, nil
		},
	}
	result, err := Qualify(context.Background(), reviewer, identity, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if findingCalls != 1 || repositoryCalls != 1 || result.ProviderRequests != 2 {
		t.Fatalf("finding calls=%d repository calls=%d result=%#v, want one request for each contract", findingCalls, repositoryCalls, result)
	}
	wantUsage := providers.Usage{PromptTokens: 34, CompletionTokens: 7, ReasoningTokens: 3}
	if result.Usage != wantUsage || result.RateLimits.RemainingRequests != 498 {
		t.Fatalf("qualification accounting = %#v / %#v, want %#v and latest capacity", result.Usage, result.RateLimits, wantUsage)
	}
}

func TestQualifyRetriesOnlyRepositoryPhaseWithinSharedRequestBudget(t *testing.T) {
	identity := CurrentIdentity(providers.Gemini, "test-model", "")
	findingCalls, repositoryCalls := 0, 0
	reviewer := qualificationReviewer{
		review: func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
			findingCalls++
			return compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 5}), nil
		},
		repository: func(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			repositoryCalls++
			result := compatibleQualificationRepositoryReview(identity, request, providers.Usage{PromptTokens: 10 * repositoryCalls, CompletionTokens: repositoryCalls})
			if repositoryCalls == 1 {
				return result, &providers.RemoteTransientError{Provider: "Gemini", StatusCode: 503}
			}
			return result, nil
		},
	}
	var waits []time.Duration
	result, err := qualifyWithRetry(context.Background(), reviewer, identity, time.Now(), qualificationRetryPolicy{
		MaximumAttempts: MaximumProviderRequests, InitialWait: time.Millisecond, MaximumWait: time.Second,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if findingCalls != 1 || repositoryCalls != 2 || result.ProviderRequests != 3 || len(waits) != 1 {
		t.Fatalf("finding calls=%d repository calls=%d requests=%d waits=%v, want 1/2/3 and one wait", findingCalls, repositoryCalls, result.ProviderRequests, waits)
	}
	if result.Usage.PromptTokens != 35 || result.Usage.CompletionTokens != 3 {
		t.Fatalf("cumulative retry usage = %#v", result.Usage)
	}
}

func TestQualifyNeverExceedsSharedFourRequestBudget(t *testing.T) {
	identity := CurrentIdentity(providers.Anthropic, "test-model", "")
	findingCalls, repositoryCalls := 0, 0
	reviewer := qualificationReviewer{
		review: func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
			findingCalls++
			return compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 1}), nil
		},
		repository: func(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			repositoryCalls++
			result := compatibleQualificationRepositoryReview(identity, request, providers.Usage{PromptTokens: 2})
			return result, &providers.RemoteTransientError{Provider: "Anthropic", StatusCode: 529}
		},
	}
	result, err := qualifyWithRetry(context.Background(), reviewer, identity, time.Now(), qualificationRetryPolicy{
		MaximumAttempts: MaximumProviderRequests,
		Wait:            func(context.Context, time.Duration) error { return nil },
	})
	if err == nil || findingCalls != 1 || repositoryCalls != 3 || result.ProviderRequests != MaximumProviderRequests {
		t.Fatalf("result=%#v err=%v finding calls=%d repository calls=%d, want exact shared four-request ceiling", result, err, findingCalls, repositoryCalls)
	}
}

func TestQualifyRejectsWrongBinding(t *testing.T) {
	identity := CurrentIdentity(providers.OpenAI, "test-model", "")
	_, err := Qualify(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		return providers.ReviewResult{
			Provider: providers.OpenAI, Model: "test-model", InputFindings: 1, Reviewed: 1,
			Observations: []providers.Observation{{Fingerprint: strings.Repeat("b", 64), RuleID: "AI-DOC-001"}},
		}, nil
	})), identity, time.Now())
	if err == nil || !strings.Contains(err.Error(), "correctly bound") {
		t.Fatalf("expected binding error, got %v", err)
	}
}

func TestQualifyPreservesMeteredAccountingWhenCompatibilityResponseFails(t *testing.T) {
	identity := CurrentIdentity(providers.OpenAI, "test-model", "")
	wantUsage := providers.Usage{PromptTokens: 123, CompletionTokens: 45, ReasoningTokens: 6}
	wantLimits := providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499}
	result, err := Qualify(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		return providers.ReviewResult{
			Provider: providers.OpenAI, Model: "test-model", InputFindings: 1,
			Usage: wantUsage, RateLimits: wantLimits,
		}, errors.New("invalid structured response")
	})), identity, time.Now())
	if err == nil || !strings.Contains(err.Error(), "compatibility request failed") {
		t.Fatalf("expected compatibility error, got %v", err)
	}
	if result.Usage != wantUsage || result.RateLimits.RemainingRequests != wantLimits.RemainingRequests || result.Identity != identity || result.ProviderRequests != 1 {
		t.Fatalf("partial qualification result = %#v, want identity and metered accounting preserved", result)
	}
}

func TestQualifyRetriesTemporaryProviderFailuresAndAggregatesAccounting(t *testing.T) {
	identity := CurrentIdentity(providers.Anthropic, "test-model", "")
	requestLimits := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 100, RemainingRequests: 0, ResetRequests: 2 * time.Second,
	}
	tokenLimits := providers.RateLimitSnapshot{
		TokensKnown: true, LimitTokens: 10_000, RemainingTokens: 0, ResetTokens: 4 * time.Second,
	}
	latestRequestLimits := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 100, RemainingRequests: 97, ResetRequests: time.Minute,
	}
	usage := []providers.Usage{
		{PromptTokens: 10, CompletionTokens: 1, TotalDurationNS: 100},
		{PromptTokens: 20, CompletionTokens: 2, ReasoningTokens: 1, TotalDurationNS: 200},
		{PromptTokens: 30, CompletionTokens: 3, ReasoningTokens: 2, TotalDurationNS: 300},
	}
	calls := 0
	reviewer := reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		result := compatibleQualificationReview(identity, request, usage[calls-1])
		switch calls {
		case 1:
			result.RateLimits = requestLimits
			return result, &providers.RemoteRateLimitError{
				Provider: "Anthropic", StatusCode: 429, RetryAfter: time.Second, RateLimits: requestLimits,
			}
		case 2:
			result.RateLimits = tokenLimits
			return result, &providers.RemoteTransientError{
				Provider: "Anthropic", StatusCode: 529, RetryAfter: 3 * time.Second, RateLimits: tokenLimits,
			}
		default:
			result.RateLimits = latestRequestLimits
			return result, nil
		}
	}))
	var waits []time.Duration
	result, err := qualifyWithRetry(context.Background(), reviewer, identity, time.Now(), qualificationRetryPolicy{
		MaximumAttempts: 4, InitialWait: 10 * time.Millisecond, MaximumWait: 20 * time.Millisecond,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || result.ProviderRequests != 4 {
		t.Fatalf("finding calls/result provider requests = %d/%d, want 3/4 including repository contract", calls, result.ProviderRequests)
	}
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 4*time.Second {
		t.Fatalf("retry waits = %v, want [2s 4s] from provider capacity data", waits)
	}
	wantUsage := providers.Usage{PromptTokens: 60, CompletionTokens: 6, ReasoningTokens: 3, TotalDurationNS: 600}
	if result.Usage != wantUsage {
		t.Fatalf("cumulative usage = %#v, want %#v", result.Usage, wantUsage)
	}
	if result.RateLimits.RemainingRequests != 97 || result.RateLimits.RemainingTokens != 0 || result.RateLimits.LimitTokens != 10_000 {
		t.Fatalf("latest per-dimension rate limits = %#v", result.RateLimits)
	}
}

func TestQualifyDoesNotRetryPermanentOrOversizedRequests(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{name: "permanent quota", err: &providers.RemoteRateLimitError{Provider: "OpenAI", StatusCode: 429, Permanent: true}},
		{name: "request too large", err: &providers.RemoteRateLimitError{Provider: "Gemini", StatusCode: 413, RequestTooLarge: true}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			identity := CurrentIdentity(providers.OpenAI, "test-model", "")
			calls, waits := 0, 0
			result, err := qualifyWithRetry(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
				calls++
				value := compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 11})
				return value, testCase.err
			})), identity, time.Now(), qualificationRetryPolicy{
				MaximumAttempts: 4,
				Wait: func(context.Context, time.Duration) error {
					waits++
					return nil
				},
			})
			if err == nil || calls != 1 || waits != 0 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 11 {
				t.Fatalf("result=%#v err=%v calls=%d waits=%d, want one prompt failure", result, err, calls, waits)
			}
		})
	}
}

func TestQualifyCancellationBoundsRetryWait(t *testing.T) {
	identity := CurrentIdentity(providers.Gemini, "test-model", "")
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	result, err := qualifyWithRetry(ctx, reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		return compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 7}), &providers.RemoteTransientError{
			Provider: "Gemini", StatusCode: 503,
		}
	})), identity, time.Now(), qualificationRetryPolicy{
		MaximumAttempts: 4,
		Wait: func(waitContext context.Context, _ time.Duration) error {
			cancel()
			return waitForQualificationRetry(waitContext, time.Hour)
		},
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 7 {
		t.Fatalf("result=%#v err=%v calls=%d, want cancellation after one accounted request", result, err, calls)
	}
}

func TestQualifyRejectsUnboundedProviderRetryWait(t *testing.T) {
	identity := CurrentIdentity(providers.OpenAI, "test-model", "")
	calls, waits := 0, 0
	result, err := qualifyWithRetry(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		return compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: 7}), &providers.RemoteRateLimitError{Provider: "OpenAI", StatusCode: 429, RetryAfter: 24 * time.Hour}
	})), identity, time.Now(), qualificationRetryPolicy{
		MaximumAttempts: 4,
		Wait:            func(context.Context, time.Duration) error { waits++; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "beyond the 2m0s") || calls != 1 || waits != 0 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 7 {
		t.Fatalf("unbounded qualification wait result=%#v err=%v calls=%d waits=%d", result, err, calls, waits)
	}
}

func TestQualifyRetryStateIsConcurrentSafe(t *testing.T) {
	const workers = 12
	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			identity := CurrentIdentity(providers.OpenAI, "test-model", "")
			calls := 0
			result, err := qualifyWithRetry(context.Background(), reviewerWithCompatibleRepository(identity, reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
				calls++
				value := compatibleQualificationReview(identity, request, providers.Usage{PromptTokens: calls})
				if calls == 1 {
					return value, &providers.RemoteTransientError{Provider: "OpenAI", StatusCode: 503}
				}
				return value, nil
			})), identity, time.Now(), qualificationRetryPolicy{
				MaximumAttempts: 3,
				Wait:            func(context.Context, time.Duration) error { return nil },
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			if result.ProviderRequests != 3 || result.Usage.PromptTokens != 3 {
				errorsChannel <- errors.New("concurrent qualification returned incorrect accounting")
			}
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
}

func compatibleQualificationReview(identity Identity, request providers.ReviewRequest, usage providers.Usage) providers.ReviewResult {
	finding := request.Findings[0]
	return providers.ReviewResult{
		Provider: identity.Provider, Model: identity.Model, InputFindings: 1, Reviewed: 1,
		Observations: []providers.Observation{{Fingerprint: finding.Fingerprint, RuleID: finding.RuleID}},
		Usage:        usage,
	}
}

func compatibleQualificationRepositoryReview(identity Identity, request providers.RepositoryAnalysisRequest, usage providers.Usage) providers.RepositoryAnalysisResult {
	return providers.RepositoryAnalysisResult{
		Provider: identity.Provider,
		Model:    identity.Model,
		Coverage: providers.RepositoryCoverage{
			Mode: request.Mode, RepositoryFiles: request.RepositoryFiles, RepositoryBytes: request.RepositoryBytes,
			FilesSubmitted: len(request.Files), BytesSubmitted: int64(len(request.Files[0].Content)),
		},
		Result: providers.RepositorySectionResult{
			Scope:  request.Scope,
			AIUses: []providers.RepositoryAIUse{},
			AIUseFacts: []providers.RepositoryAIUseFactSet{{
				AIUseID: request.ConfirmedAIUses[0].ID, Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{},
			}},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
			UnmappedObservations:  []providers.RepositoryUnmappedObservation{},
			UnresolvedQuestions:   []string{},
		},
		Usage: usage,
	}
}

func TestCacheBindsIdentityExpiresAndUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", cacheFileName)
	cache, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	identity := CurrentIdentity(providers.Gemini, "model", "")
	result := Result{Identity: identity, Status: "compatible", CheckedAt: now, ExpiresAt: now.Add(CacheValidity), Detail: "Passed.", ProviderRequests: 7}
	if err := cache.Store(result); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions = %o", info.Mode().Perm())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := reopened.Lookup(identity, now.Add(time.Hour))
	if err != nil || !found || !got.FromCache || got.ProviderRequests != 0 {
		t.Fatalf("lookup result=%#v found=%t err=%v", got, found, err)
	}
	changed := identity
	changed.RepositoryPromptVersion = "older-contract"
	if _, found, err := reopened.Lookup(changed, now.Add(time.Hour)); err != nil || found {
		t.Fatalf("changed contract found=%t err=%v", found, err)
	}
	legacy := identity
	legacy.QualificationContractVersion--
	if _, _, err := reopened.Lookup(legacy, now.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "unsupported qualification contract version") {
		t.Fatalf("legacy qualification contract lookup error = %v, want invalidated identity", err)
	}
	if cacheSchemaVersion != 5 || cacheFileName != "model-qualification-v5.json" {
		t.Fatalf("qualification cache contract = schema %d file %q", cacheSchemaVersion, cacheFileName)
	}
	if _, found, err := reopened.Lookup(identity, result.ExpiresAt); err != nil || found {
		t.Fatalf("expired result found=%t err=%v", found, err)
	}
}
