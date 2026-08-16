package repositoryanalysis

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type billedAttributionReviewer struct {
	invalidMode providers.RepositoryAnalysisMode
	usage       providers.Usage
	totalUsage  providers.Usage
	requests    []providers.RepositoryAnalysisRequest
}

func (reviewer *billedAttributionReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	addUsage(&reviewer.totalUsage, reviewer.usage)
	result := targetedSuccessfulResult(request, reviewer.usage, false)
	if request.Mode == reviewer.invalidMode {
		result.Result.ObjectiveObservations = []providers.RepositoryObjectiveObservation{{
			ObjectiveID: "pack/objective", SystemID: "unconfigured-system", Strength: providers.StrengthPartial, Confidence: "medium", Rationale: "Billed but locally invalid attribution.",
		}}
	}
	return result, nil
}

type requestTooLargeRetryReviewer struct {
	requests           []providers.RepositoryAnalysisRequest
	failFirstSource    bool
	failFirstSynthesis bool
	failedSource       bool
	failedSynthesis    bool
}

func (reviewer *requestTooLargeRetryReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	if len(request.Files) > 0 && reviewer.failFirstSource && !reviewer.failedSource {
		reviewer.failedSource = true
		return providers.RepositoryAnalysisResult{}, requestTooLargeAtTenThousand()
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis && reviewer.failFirstSynthesis && !reviewer.failedSynthesis {
		reviewer.failedSynthesis = true
		return providers.RepositoryAnalysisResult{}, requestTooLargeAtTenThousand()
	}
	return targetedSuccessfulResult(request, providers.Usage{PromptTokens: 20, CompletionTokens: 4}, false), nil
}

func TestRunTargetedCountsBilledUsageWhenSourceAttributionValidationFails(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	usage := providers.Usage{PromptTokens: 640, CompletionTokens: 80, ReasoningTokens: 30, TotalDurationNS: 900}
	reviewer := &billedAttributionReviewer{invalidMode: providers.RepositoryAnalysisTargeted, usage: usage}

	result, err := Run(context.Background(), reviewer, repository, nil, attributionFailureSystems(), Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil || !strings.Contains(err.Error(), "attributed system") {
		t.Fatalf("source attribution error = %v", err)
	}
	assertUsageEquals(t, result.Usage, usage, "billed source attribution rejection")
	assertIncompleteNonSemanticResult(t, result)
}

func TestRunTargetedCountsBilledUsageWhenSynthesisAttributionValidationFails(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	usage := providers.Usage{PromptTokens: 410, CompletionTokens: 55, ReasoningTokens: 20, TotalDurationNS: 700}
	reviewer := &billedAttributionReviewer{invalidMode: providers.RepositoryAnalysisSynthesis, usage: usage}

	result, err := Run(context.Background(), reviewer, repository, nil, attributionFailureSystems(), Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil || !strings.Contains(err.Error(), "attributed system") {
		t.Fatalf("synthesis attribution error = %v", err)
	}
	assertUsageEquals(t, result.Usage, reviewer.totalUsage, "billed synthesis attribution rejection")
	assertIncompleteNonSemanticResult(t, result)
}

func TestRunTargetedRequestTooLargeRetriesSameSourcePackageWithReducedOutput(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &requestTooLargeRetryReviewer{failFirstSource: true}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var sourceRequests []providers.RepositoryAnalysisRequest
	for _, request := range reviewer.requests {
		if len(request.Files) > 0 {
			sourceRequests = append(sourceRequests, request)
		}
	}
	if len(sourceRequests) < 3 {
		t.Fatalf("source request count = %d, want failed attempt, same-package retry, and remaining batch", len(sourceRequests))
	}
	firstPaths, secondPaths := sourceFilePaths(sourceRequests[0].Files), sourceFilePaths(sourceRequests[1].Files)
	if sourceRequests[0].Scope != sourceRequests[1].Scope || !reflect.DeepEqual(firstPaths, secondPaths) || sourceRequests[1].MaxOutputTokens >= sourceRequests[0].MaxOutputTokens {
		t.Fatalf("RequestTooLarge did not retry the same source package with reduced output: first=%s %v/%d second=%s %v/%d", sourceRequests[0].Scope, firstPaths, sourceRequests[0].MaxOutputTokens, sourceRequests[1].Scope, secondPaths, sourceRequests[1].MaxOutputTokens)
	}
	seen := make(map[string]bool)
	for _, request := range sourceRequests[1:] {
		for _, file := range request.Files {
			seen[file.Path] = true
		}
	}
	for _, path := range expectedPaths {
		if !seen[path] {
			t.Errorf("candidate %q disappeared after same-package retry", path)
		}
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal || result.Coverage.SourceBatchesTotal < 2 {
		t.Fatalf("successful reduced-output retry coverage = %#v", result.Coverage)
	}
}

func TestRunSinglePackageTargetedRequestTooLargeRetriesWithReducedOutput(t *testing.T) {
	reviewer := &requestTooLargeRetryReviewer{failFirstSource: true}

	result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
	if err != nil {
		t.Fatal(err)
	}
	var sourceRequests []providers.RepositoryAnalysisRequest
	for _, request := range reviewer.requests {
		if len(request.Files) > 0 {
			sourceRequests = append(sourceRequests, request)
		}
	}
	if len(sourceRequests) != 2 {
		t.Fatalf("single-package source request count = %d, want failed request plus reduced-output retry: %#v", len(sourceRequests), sourceRequests)
	}
	firstPaths, secondPaths := sourceFilePaths(sourceRequests[0].Files), sourceFilePaths(sourceRequests[1].Files)
	if !reflect.DeepEqual(firstPaths, secondPaths) || sourceRequests[0].MaxOutputTokens != targetedRemoteOutputTokens || sourceRequests[1].MaxOutputTokens >= sourceRequests[0].MaxOutputTokens {
		t.Fatalf("single-package provider-limit retry changed evidence or did not reduce output: first=%v/%d second=%v/%d", firstPaths, sourceRequests[0].MaxOutputTokens, secondPaths, sourceRequests[1].MaxOutputTokens)
	}
	if sourceRequests[1].MaxOutputTokens != 2_000 {
		t.Fatalf("single-package 10k retry output allowance = %d, want 2000", sourceRequests[1].MaxOutputTokens)
	}
	if result.Coverage.FilesSubmitted != 2 || result.Coverage.SourceBatchesTotal != 0 {
		t.Fatalf("single-package retry coverage = %#v", result.Coverage)
	}
}

func TestRunTargetedRequestTooLargeRetriesTwoSummarySynthesisWithReducedOutput(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &requestTooLargeRetryReviewer{failFirstSynthesis: true}

	_, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSameSynthesisRetriedWithReducedOutput(t, reviewer.requests, 2)
}

func TestRunHierarchicalRequestTooLargeRetriesForcedOneSummarySynthesis(t *testing.T) {
	repository := discovery.Repository{Root: ".", Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\nfunc main() {}\n"), Size: 28,
	}}}
	reviewer := &requestTooLargeRetryReviewer{failFirstSynthesis: true}

	_, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSameSynthesisRetriedWithReducedOutput(t, reviewer.requests, 1)
}

func TestRepositoryCacheRejectsIncompleteSourceBatchCounters(t *testing.T) {
	identity := CacheIdentity{
		Provider: providers.OpenAI, Model: "test-model", PromptVersion: providers.RepositoryAnalysisPromptVersion,
		EndpointDigest: DigestEndpoint("https://api.example.test/v1"),
	}
	digest := strings.Repeat("a", 64)
	cache, err := OpenCache(t.TempDir() + "/repository-analysis.json")
	if err != nil {
		t.Fatal(err)
	}
	base := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, Subsystems: 2, SourceBatchesCompleted: 2, SourceBatchesTotal: 2,
		},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}
	if err := cache.Store(identity, digest, base); err != nil {
		t.Fatalf("complete batch counters were rejected: %v", err)
	}
	invalid := []providers.RepositoryCoverage{
		{Mode: providers.RepositoryAnalysisTargeted, Subsystems: 1, SourceBatchesCompleted: 1, SourceBatchesTotal: 2},
		{Mode: providers.RepositoryAnalysisTargeted, Subsystems: 2, SourceBatchesCompleted: 3, SourceBatchesTotal: 2},
		{Mode: providers.RepositoryAnalysisTargeted, Subsystems: 1, SourceBatchesCompleted: 2, SourceBatchesTotal: 2},
	}
	for index, coverage := range invalid {
		value := base
		value.Coverage = coverage
		if err := cache.Store(identity, digest, value); err == nil {
			t.Errorf("cache accepted incomplete/impossible source batch counters in case %d: %#v", index, coverage)
		}
	}
}

func attributionFailureSystems() []profile.System {
	return []profile.System{profile.NewDraftSystem("one", "One"), profile.NewDraftSystem("two", "Two")}
}

func requestTooLargeAtTenThousand() error {
	return &providers.RemoteRateLimitError{
		Provider: "OpenAI", Message: "Request too large.", LimitTokens: 10_000, RequestedTokens: 12_000, RequestTooLarge: true,
	}
}

func assertSameSynthesisRetriedWithReducedOutput(t *testing.T, requests []providers.RepositoryAnalysisRequest, summaryCount int) {
	t.Helper()
	var synthesis []providers.RepositoryAnalysisRequest
	for _, request := range requests {
		if request.Mode == providers.RepositoryAnalysisSynthesis {
			synthesis = append(synthesis, request)
		}
	}
	if len(synthesis) != 2 {
		t.Fatalf("synthesis request count = %d, want failed request plus reduced-output retry: %#v", len(synthesis), synthesis)
	}
	if len(synthesis[0].SubsystemSummaries) != summaryCount || len(synthesis[1].SubsystemSummaries) != summaryCount || synthesis[0].Scope != synthesis[1].Scope || synthesis[1].MaxOutputTokens >= synthesis[0].MaxOutputTokens {
		t.Fatalf("synthesis was split or not retried with reduced output: first scope=%q summaries=%d output=%d; second scope=%q summaries=%d output=%d", synthesis[0].Scope, len(synthesis[0].SubsystemSummaries), synthesis[0].MaxOutputTokens, synthesis[1].Scope, len(synthesis[1].SubsystemSummaries), synthesis[1].MaxOutputTokens)
	}
}
