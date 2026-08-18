package repositoryanalysis

import (
	"context"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestTargetedSourceInputTokensUsesModelContextAndLiveTokenCapacity(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(54)
	selected, _ := targetedRepositoryCandidateFiles(repository, codegraph.Build(repository), nil, nil)
	if len(selected) == 0 {
		t.Fatal("fixture selected no structural evidence")
	}

	known := targetedSourceInputTokens(Options{
		Provider: providers.OpenAI, MaxInputTokens: DefaultRemoteInputTokens, ModelContextTokens: 1_050_000,
	}, selected)
	if known < targetedRemoteBalancedInputTokens || known > targetedRemoteLatencyInputTokens {
		t.Fatalf("known large-context target = %d, want balanced %d-%d", known, targetedRemoteBalancedInputTokens, targetedRemoteLatencyInputTokens)
	}
	unknown := targetedSourceInputTokens(Options{
		Provider: providers.Compatible, MaxInputTokens: DefaultRemoteInputTokens,
	}, selected)
	if unknown > targetedRemoteFallbackInputTokens {
		t.Fatalf("unknown-provider target = %d, conservative ceiling %d", unknown, targetedRemoteFallbackInputTokens)
	}
	rateBounded := targetedSourceInputTokens(Options{
		Provider: providers.OpenAI, MaxInputTokens: DefaultRemoteInputTokens, ModelContextTokens: 1_050_000,
		InitialRateLimits: providers.RateLimitSnapshot{TokensKnown: true, LimitTokens: 50_000, RemainingTokens: 50_000},
	}, selected)
	if rateBounded >= known {
		t.Fatalf("rate-bounded target = %d, want below unconstrained %d", rateBounded, known)
	}
}

func TestRunTargetedPacksMediumKnownContextQueueIntoTwoSourceRequests(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(54)
	reviewer := &exhaustiveBatchReviewer{}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "gpt-5.6-sol",
		ModelContextTokens: 1_050_000,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 500, RemainingRequests: 500,
			TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 500_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	if reviewer.sourceRequests != 2 {
		selected, _ := targetedRepositoryCandidateFiles(repository, codegraph.Build(repository), nil, nil)
		target := targetedSourceInputTokens(Options{
			Provider: providers.OpenAI, ModelContextTokens: 1_050_000,
			InitialRateLimits: providers.RateLimitSnapshot{TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 500_000},
		}, selected)
		var fileCounts []int
		for _, request := range reviewer.requests {
			if request.Mode != providers.RepositoryAnalysisSynthesis {
				fileCounts = append(fileCounts, len(request.Files))
			}
		}
		t.Fatalf("adaptive source requests = %d, want two balanced requests (target=%d, selected bytes=%d, file counts=%v)", reviewer.sourceRequests, target, requestContextBytes(selected, providers.RepositoryGraphContext{}), fileCounts)
	}
	if result.Coverage.SourceBatchesTotal != 2 || result.Coverage.SourceBatchesCompleted != 2 {
		t.Fatalf("adaptive batch coverage = %#v", result.Coverage)
	}
	if result.Coverage.ProviderRequests != 3 || len(result.RequestDiagnostics) != 3 {
		t.Fatalf("source plus synthesis diagnostics = %d request(s), %#v", result.Coverage.ProviderRequests, result.RequestDiagnostics)
	}
	for _, diagnostic := range result.RequestDiagnostics {
		if diagnostic.DurationNS < 0 || diagnostic.Outcome != "completed" {
			t.Fatalf("successful request diagnostic = %#v", diagnostic)
		}
	}
}

func TestRepositoryRequestDiagnosticsRecordEveryAttemptAndRetryCause(t *testing.T) {
	reviewer := &validationRepairAccountingReviewer{}
	request := providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisTargeted, Scope: "evidence bundle",
		Files: []providers.RepositorySourceFile{{Path: "app.py", Kind: "source", Content: "model()\n"}},
	}
	result, err := reviewRepositoryWithRetry(context.Background(), reviewer, request, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RequestDiagnostics) != 2 {
		t.Fatalf("request diagnostics = %#v, want rejected attempt plus repair", result.RequestDiagnostics)
	}
	first, second := result.RequestDiagnostics[0], result.RequestDiagnostics[1]
	if first.Phase != "targeted" || first.Outcome != "retryable-error" || first.RetryReason != "structured-validation" || first.InputFiles != 1 || first.InputBytes == 0 || first.DurationNS < 0 {
		t.Fatalf("first request diagnostic = %#v", first)
	}
	if second.Outcome != "completed" || second.RetryReason != "" || second.Attempt != 2 || second.DurationNS < 0 {
		t.Fatalf("repair diagnostic = %#v", second)
	}
}
