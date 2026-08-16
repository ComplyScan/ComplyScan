package repositoryanalysis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type temporaryRateLimitAccountingReviewer struct {
	requests   []providers.RepositoryAnalysisRequest
	usage      providers.Usage
	totalUsage providers.Usage
	limited    bool
}

func (reviewer *temporaryRateLimitAccountingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	addUsage(&reviewer.totalUsage, reviewer.usage)
	result := targetedSuccessfulResult(request, reviewer.usage, false)
	if !reviewer.limited && len(request.Files) > 0 {
		reviewer.limited = true
		return result, &providers.RemoteRateLimitError{Provider: "OpenAI", Message: "temporary token limit"}
	}
	return result, nil
}

type genericFullFailureReviewer struct {
	requests []providers.RepositoryAnalysisRequest
	usage    providers.Usage
}

func (reviewer *genericFullFailureReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	return targetedSuccessfulResult(request, reviewer.usage, true), errors.New("simulated provider transport failure")
}

type broadLimitFallbackReviewer struct {
	requests   []providers.RepositoryAnalysisRequest
	broadUsage providers.Usage
	batchUsage providers.Usage
	totalUsage providers.Usage
}

func (reviewer *broadLimitFallbackReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	usage := reviewer.batchUsage
	semantic := false
	if request.Mode == providers.RepositoryAnalysisFull {
		usage = reviewer.broadUsage
		semantic = true
	}
	addUsage(&reviewer.totalUsage, usage)
	result := targetedSuccessfulResult(request, usage, semantic)
	if request.Mode == providers.RepositoryAnalysisFull {
		return result, requestTooLargeAtTenThousand()
	}
	return result, nil
}

func TestRunSingleTargetedTemporaryRateLimitCountsEverySourceTransfer(t *testing.T) {
	usage := providers.Usage{PromptTokens: 480, CompletionTokens: 60, ReasoningTokens: 25}
	reviewer := &temporaryRateLimitAccountingReviewer{usage: usage}

	result, err := Run(context.Background(), reviewer, singleTargetedRepository(), nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 2 {
		t.Fatalf("single targeted temporary-limit request count = %d, want failed attempt plus retry", len(reviewer.requests))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, reviewer.totalUsage, "single targeted temporary-limit retry")
}

func TestRunBatchedTargetedTemporaryRateLimitCountsEverySourceTransfer(t *testing.T) {
	usage := providers.Usage{PromptTokens: 320, CompletionTokens: 45, ReasoningTokens: 18}
	reviewer := &temporaryRateLimitAccountingReviewer{usage: usage}
	repository, _ := exhaustiveCandidateRepository(10)

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) < 4 {
		t.Fatalf("batched targeted request count = %d, want failed source attempt, retry, another source batch, and synthesis", len(reviewer.requests))
	}
	if len(reviewer.requests[0].Files) == 0 || !reflect.DeepEqual(sourceFilePaths(reviewer.requests[0].Files), sourceFilePaths(reviewer.requests[1].Files)) {
		t.Fatalf("temporary-limit retry did not repeat the first bounded source package: first=%v second=%v", sourceFilePaths(reviewer.requests[0].Files), sourceFilePaths(reviewer.requests[1].Files))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, reviewer.totalUsage, "batched targeted temporary-limit retry")
}

func TestRunSingleTargetedCancelledRateLimitWaitRetainsFirstAttempt(t *testing.T) {
	usage := providers.Usage{PromptTokens: 510, CompletionTokens: 55, ReasoningTokens: 22}
	reviewer := &temporaryRateLimitAccountingReviewer{usage: usage}

	result, err := Run(context.Background(), reviewer, singleTargetedRepository(), nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return context.Canceled },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rate-limit wait error = %v, want context cancellation", err)
	}
	if len(reviewer.requests) != 1 {
		t.Fatalf("cancelled rate-limit wait made %d provider attempts, want 1", len(reviewer.requests))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, usage, "cancelled rate-limit wait")
	assertIncompleteNonSemanticResult(t, result)
}

func TestRunBatchedTargetedCancelledRateLimitWaitRetainsFirstAttempt(t *testing.T) {
	usage := providers.Usage{PromptTokens: 530, CompletionTokens: 58, ReasoningTokens: 24}
	reviewer := &temporaryRateLimitAccountingReviewer{usage: usage}
	repository, _ := exhaustiveCandidateRepository(10)

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(context.Context, time.Duration) error { return context.Canceled },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled batched rate-limit wait error = %v, want context cancellation", err)
	}
	if len(reviewer.requests) != 1 || len(reviewer.requests[0].Files) == 0 {
		t.Fatalf("cancelled batched rate-limit wait requests = %#v, want one attempted source package", reviewer.requests)
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, usage, "cancelled batched rate-limit wait")
	assertIncompleteNonSemanticResult(t, result)
	if result.Coverage.SourceBatchesCompleted != 0 || result.Coverage.SourceBatchesTotal < 2 {
		t.Fatalf("cancelled batched rate-limit coverage = %#v", result.Coverage)
	}
}

func TestRunFullGenericProviderFailureRetainsAttemptWithoutSemantics(t *testing.T) {
	usage := providers.Usage{PromptTokens: 900, CompletionTokens: 110, ReasoningTokens: 50}
	reviewer := &genericFullFailureReviewer{usage: usage}
	repository := singleTargetedRepository()

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeFull, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil || !strings.Contains(err.Error(), "simulated provider transport failure") {
		t.Fatalf("full provider failure error = %v", err)
	}
	if len(reviewer.requests) != 1 {
		t.Fatalf("full provider failure request count = %d, want 1", len(reviewer.requests))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, usage, "full provider failure")
	assertIncompleteNonSemanticResult(t, result)
	if result.Coverage.Mode != providers.RepositoryAnalysisFull || result.Provider != providers.OpenAI || result.Model != "test" {
		t.Fatalf("full provider failure identity/coverage = %#v", result)
	}
}

func TestRunDeepBroadLimitFallbackIncludesFailedBroadAttempt(t *testing.T) {
	reviewer := &broadLimitFallbackReviewer{
		broadUsage: providers.Usage{PromptTokens: 6_900, CompletionTokens: 2_600, ReasoningTokens: 2_000},
		batchUsage: providers.Usage{PromptTokens: 700, CompletionTokens: 90, ReasoningTokens: 40},
	}
	repository := singleTargetedRepository()

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeDeep, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 3 || reviewer.requests[0].Mode != providers.RepositoryAnalysisFull || reviewer.requests[1].Mode != providers.RepositoryAnalysisSubsystem || reviewer.requests[2].Mode != providers.RepositoryAnalysisSynthesis {
		t.Fatalf("deep broad-limit fallback request sequence = %#v", reviewer.requests)
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, reviewer.totalUsage, "deep broad-limit fallback")
	if len(result.Result.AIUses) != 0 {
		t.Fatalf("deep broad-limit fallback retained semantics from the rejected broad response: %#v", result.Result.AIUses)
	}
	if result.Coverage.Mode != providers.RepositoryAnalysisSynthesis || result.Coverage.SourceBatchesCompleted != 1 || result.Coverage.SourceBatchesTotal != 1 {
		t.Fatalf("deep broad-limit fallback completion coverage = %#v", result.Coverage)
	}
}
