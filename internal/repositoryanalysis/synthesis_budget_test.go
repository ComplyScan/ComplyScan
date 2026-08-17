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
	mu                sync.Mutex
	sourceRequests    int
	synthesisRequests int
	synthesisInputs   []int
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

func TestTargetedBatchesUseFullConfiguredBudgetForGlobalSynthesis(t *testing.T) {
	repository := discovery.Repository{Root: "."}
	for index := 0; index < 13; index++ {
		content := []byte("package ai\n// " + strings.Repeat("bounded implementation evidence ", 230) + "\n")
		repository.Files = append(repository.Files, discovery.File{
			Path: fmt.Sprintf("internal/use_%02d.go", index), Kind: discovery.KindSource, Content: content, Size: int64(len(content)),
		})
	}
	reviewer := &wideSynthesisBudgetReviewer{}
	sourceRequestBudget := sourceBudget(targetedRemoteInputTokens, nil, nil, nil)
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), repositoryFiles(repository), nil, nil, nil, sourceRequestBudget, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true, MaxInputTokens: DefaultRemoteInputTokens,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 1_000, RemainingRequests: 1_000,
			TokensKnown: true, LimitTokens: 10_000_000, RemainingTokens: 10_000_000,
		},
	})
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
