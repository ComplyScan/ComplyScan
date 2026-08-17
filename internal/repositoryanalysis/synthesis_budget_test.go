package repositoryanalysis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type wideSynthesisBudgetReviewer struct {
	mu                     sync.Mutex
	sourceRequests         int
	synthesisRequests      int
	synthesisInputs        []int
	synthesisOutputLimits  []int
	minimumSynthesisOutput int
}

func (reviewer *wideSynthesisBudgetReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	section := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		reviewer.synthesisRequests++
		reviewer.synthesisInputs = append(reviewer.synthesisInputs, len(request.SubsystemSummaries))
		reviewer.synthesisOutputLimits = append(reviewer.synthesisOutputLimits, request.MaxOutputTokens)
		if request.MaxOutputTokens < reviewer.minimumSynthesisOutput {
			return providers.RepositoryAnalysisResult{
					Provider: providers.OpenAI, Model: "test", Result: section,
					Coverage: providers.RepositoryCoverage{Mode: request.Mode},
					Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: request.MaxOutputTokens},
				}, &providers.RemoteIncompleteError{
					Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens",
					InputTokens: 50_000, OutputTokens: request.MaxOutputTokens,
				}
		}
	} else {
		reviewer.sourceRequests++
		// Reproduce the real failure shape: every bounded source result is
		// individually too large for the 6,500-token source-request budget, but
		// all thirteen validated summaries fit the configured model context.
		section.UnresolvedQuestions = []string{strings.Repeat("validated source detail ", 650)}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: section,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode},
		Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func TestGlobalSynthesisGrowsPastEightKBeforeSplittingAndRecombining(t *testing.T) {
	repository := synthesisBudgetRepository()
	reviewer := &wideSynthesisBudgetReviewer{minimumSynthesisOutput: 16_384}
	result, err := runWideSynthesisBudgetFixture(repository, reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.sourceRequests != 13 {
		t.Fatalf("source requests = %d, want 13 bounded requests", reviewer.sourceRequests)
	}
	if reviewer.synthesisRequests != 3 {
		t.Fatalf("synthesis requests = %d, want 4K, 8K, and 16K attempts", reviewer.synthesisRequests)
	}
	for index, count := range reviewer.synthesisInputs {
		if count != 13 {
			t.Fatalf("synthesis attempt %d received %d summaries, want all 13 without a split/recombine cycle", index+1, count)
		}
	}
	if got := reviewer.synthesisOutputLimits; len(got) != 3 || got[0] != 4_096 || got[1] != 8_192 || got[2] != 16_384 {
		t.Fatalf("synthesis output allowances = %v, want [4096 8192 16384]", got)
	}
	if result.Coverage.SourceBatchesCompleted != 13 || result.Coverage.SourceBatchesTotal != 13 {
		t.Fatalf("source coverage = %#v", result.Coverage)
	}
}

func TestTargetedBatchesUseFullConfiguredBudgetForGlobalSynthesis(t *testing.T) {
	repository := synthesisBudgetRepository()
	reviewer := &wideSynthesisBudgetReviewer{}
	result, err := runWideSynthesisBudgetFixture(repository, reviewer)
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.sourceRequests != 13 {
		t.Fatalf("source requests = %d, want 13 bounded requests", reviewer.sourceRequests)
	}
	if reviewer.synthesisRequests != 1 || len(reviewer.synthesisInputs) != 1 || reviewer.synthesisInputs[0] != 13 {
		t.Fatalf("synthesis requests/inputs = %d/%v, want one global request containing all 13 validated summaries", reviewer.synthesisRequests, reviewer.synthesisInputs)
	}
	if result.Coverage.SourceBatchesCompleted != 13 || result.Coverage.SourceBatchesTotal != 13 {
		t.Fatalf("source coverage = %#v", result.Coverage)
	}
}

func synthesisBudgetRepository() discovery.Repository {
	repository := discovery.Repository{Root: "."}
	for index := 0; index < 13; index++ {
		content := []byte("package ai\n// " + strings.Repeat("bounded implementation evidence ", 230) + "\n")
		repository.Files = append(repository.Files, discovery.File{
			Path: fmt.Sprintf("internal/use_%02d.go", index), Kind: discovery.KindSource, Content: content, Size: int64(len(content)),
		})
	}
	return repository
}

func runWideSynthesisBudgetFixture(repository discovery.Repository, reviewer *wideSynthesisBudgetReviewer) (providers.RepositoryAnalysisResult, error) {
	sourceRequestBudget := sourceBudget(targetedRemoteInputTokens, nil, nil, nil)
	return runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), repositoryFiles(repository), nil, nil, nil, sourceRequestBudget, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true, MaxInputTokens: DefaultRemoteInputTokens,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 1_000, RemainingRequests: 1_000,
			TokensKnown: true, LimitTokens: 10_000_000, RemainingTokens: 10_000_000,
		},
	})
}
