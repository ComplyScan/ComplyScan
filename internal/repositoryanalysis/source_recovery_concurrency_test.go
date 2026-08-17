package repositoryanalysis

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type concurrentSourceOutputRecoveryReviewer struct {
	mu          sync.Mutex
	active      int
	maximum     int
	requests    []providers.RepositoryAnalysisRequest
	releaseWave chan struct{}
	expected    int
}

func (reviewer *concurrentSourceOutputRecoveryReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	reviewer.active++
	if reviewer.active > reviewer.maximum {
		reviewer.maximum = reviewer.active
	}
	if len(reviewer.requests) == reviewer.expected {
		close(reviewer.releaseWave)
	}
	reviewer.mu.Unlock()

	select {
	case <-reviewer.releaseWave:
	case <-ctx.Done():
		return providers.RepositoryAnalysisResult{}, ctx.Err()
	case <-time.After(time.Second):
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("source-output recovery calls did not overlap")
	}

	reviewer.mu.Lock()
	reviewer.active--
	reviewer.mu.Unlock()
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI,
		Model:    "test",
		Usage:    providers.Usage{PromptTokens: 200, CompletionTokens: 20, ReasoningTokens: 5},
		RateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 500, RemainingRequests: 480,
			TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 400_000,
		},
		Result: providers.RepositorySectionResult{
			Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}, nil
}

func TestSourceOutputRecoveryRunsInOneConcurrentCapacityWave(t *testing.T) {
	const batchCount = 3
	reviewer := &concurrentSourceOutputRecoveryReviewer{releaseWave: make(chan struct{}), expected: batchCount}
	calls := make([]repositorySourceBatchCall, 0, batchCount)
	responses := make([]repositorySourceBatchResponse, 0, batchCount)
	chunks := make([]repositoryChunk, 0, batchCount)
	prefetched := make(map[string]repositorySourceBatchResponse, batchCount)
	for index := 0; index < batchCount; index++ {
		path := fmt.Sprintf("part-%d.go", index+1)
		content := fmt.Sprintf("package part%d\n", index+1)
		chunk := repositoryChunk{id: fmt.Sprintf("source-%06d", index+1), scope: fmt.Sprintf("part-%d", index+1), files: []providers.RepositorySourceFile{{Path: path, Kind: "source", Content: content, ContentStartLine: 1}}}
		request := providers.RepositoryAnalysisRequest{Mode: providers.RepositoryAnalysisTargeted, Scope: chunk.scope, MaxOutputTokens: targetedRemoteOutputTokens, Files: chunk.files}
		call := repositorySourceBatchCall{index: index, chunk: chunk, request: request, inputBytes: int64(len(content)), estimatedTokens: 13_000, maxOutputTokens: targetedRemoteOutputTokens}
		incompleteResult := providers.RepositoryAnalysisResult{
			Provider: providers.OpenAI, Model: "test",
			Coverage: providers.RepositoryCoverage{FilesSubmitted: 1, BytesSubmitted: int64(len(content)), ProviderRequests: 1},
			Usage:    providers.Usage{PromptTokens: 1_000, CompletionTokens: targetedRemoteOutputTokens, ReasoningTokens: 500},
		}
		response := repositorySourceBatchResponse{
			id: chunk.id, scope: chunk.scope, result: incompleteResult,
			err: &providers.RemoteIncompleteError{Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens", InputTokens: 1_000, OutputTokens: targetedRemoteOutputTokens},
		}
		calls = append(calls, call)
		responses = append(responses, response)
		chunks = append(chunks, chunk)
		prefetched[chunk.id] = response
	}

	limits := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 487,
		TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 450_000,
	}
	progressConcurrency := 0
	updated, _, err := recoverRepositorySourceWaveOutputs(
		context.Background(), reviewer, calls, responses, chunks, prefetched, limits, 0, batchCount,
		Options{Provider: providers.OpenAI, TargetedBatches: true, OnProgress: func(value Progress) error {
			if value.Stage == "targeted-batch-concurrency" && value.Completed == batchCount {
				progressConcurrency++
			}
			return nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reviewer.mu.Lock()
	maximum := reviewer.maximum
	requests := append([]providers.RepositoryAnalysisRequest(nil), reviewer.requests...)
	reviewer.mu.Unlock()
	if maximum != batchCount || len(requests) != batchCount || progressConcurrency != 1 {
		t.Fatalf("recovery concurrency = max:%d requests:%d progress:%d, want %d/%d/1", maximum, len(requests), progressConcurrency, batchCount, batchCount)
	}
	for index, request := range requests {
		if !request.OutputRecovery || request.MaxOutputTokens != maximumRecoveryOutput {
			t.Fatalf("recovery request %d = output_recovery:%t max:%d", index+1, request.OutputRecovery, request.MaxOutputTokens)
		}
	}
	for _, chunk := range chunks {
		response := prefetched[chunk.id]
		if response.err != nil || response.result.Coverage.ProviderRequests != 2 || response.result.Coverage.FilesSubmitted != 2 {
			t.Fatalf("combined recovery response for %s = %#v, err=%v", chunk.scope, response.result, response.err)
		}
		if response.result.Usage.PromptTokens != 1_200 || response.result.Usage.CompletionTokens != targetedRemoteOutputTokens+20 || response.result.Usage.ReasoningTokens != 505 {
			t.Fatalf("combined recovery usage for %s = %#v", chunk.scope, response.result.Usage)
		}
		if chunk.maxOutputTokens != maximumRecoveryOutput {
			t.Fatalf("chunk %s output allowance = %d, want %d", chunk.scope, chunk.maxOutputTokens, maximumRecoveryOutput)
		}
	}
	if updated.RemainingRequests >= limits.RemainingRequests || updated.RemainingTokens >= limits.RemainingTokens {
		t.Fatalf("recovery wave did not consume observed capacity: before=%#v after=%#v", limits, updated)
	}
}
