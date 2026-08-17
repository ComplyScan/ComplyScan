package repositoryanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

// exhaustiveBatchReviewer is deliberately source-agnostic: it records every
// bounded package the orchestration layer submits and returns a grounded empty
// answer. That makes these tests exercise coverage and batching rather than a
// fake model's ability to rediscover the fixture.
type exhaustiveBatchReviewer struct {
	mu             sync.Mutex
	requests       []providers.RepositoryAnalysisRequest
	sourceRequests int
	failSourceAt   int
}

type semanticPartialReviewer struct {
	exhaustiveBatchReviewer
}

type recoveryAccountingReviewer struct {
	mu              sync.Mutex
	requests        []providers.RepositoryAnalysisRequest
	incompleteMode  providers.RepositoryAnalysisMode
	incompleteUsed  bool
	incompleteScope string
	incompleteLimit int
	primeAdaptive   bool
	adaptivePrimed  bool
	failSynthesis   bool
	successfulUsage providers.Usage
	incompleteUsage providers.Usage
}

type compactingSynthesisReviewer struct {
	mu                    sync.Mutex
	requests              []providers.RepositoryAnalysisRequest
	singleSummaryPasses   int
	combinedSummaryPasses int
}

type crossBatchGroupingReviewer struct {
	mu             sync.Mutex
	requests       []providers.RepositoryAnalysisRequest
	sourceRequests int
}

func (reviewer *crossBatchGroupingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.requests = append(reviewer.requests, request)
	result := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if request.Mode != providers.RepositoryAnalysisSynthesis {
		reviewer.sourceRequests++
		citation := providers.RepositoryCitation{Path: request.Files[0].Path, Line: request.Files[0].ContentStartLine, Summary: "One part of the same connected workflow."}
		result.AIUses = []providers.RepositoryAIUse{{
			ID: "model-local-key", Name: "Workflow part", Purpose: "One bounded observation", Lifecycle: "unknown", Confidence: "medium", Evidence: []providers.RepositoryCitation{citation},
		}}
		result.AIUseFacts = []providers.RepositoryAIUseFactSet{{AIUseID: "model-local-key", Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}}
	} else {
		members := make([]string, 0)
		evidence := make([]providers.RepositoryCitation, 0)
		for _, summary := range request.SubsystemSummaries {
			for _, use := range summary.AIUses {
				members = append(members, use.MemberObservationIDs...)
				evidence = append(evidence, use.Evidence...)
			}
		}
		result.AIUses = []providers.RepositoryAIUse{{
			ID: "model-proposed-group", Name: "Connected AI workflow", Purpose: "One use spanning bounded source batches", Lifecycle: "unknown", Confidence: "high",
			Evidence: evidence, MemberObservationIDs: members,
		}}
		result.AIUseFacts = []providers.RepositoryAIUseFactSet{{AIUseID: "model-proposed-group", Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: result,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: sourceFileBytes(request.Files)},
		Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func (reviewer *compactingSynthesisReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.requests = append(reviewer.requests, request)
	result := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if request.Mode != providers.RepositoryAnalysisSynthesis {
		// Make every source-batch result too large to share a synthesis
		// request. The first synthesis level must compact these individually
		// before a later level can combine them.
		result.UnresolvedQuestions = []string{strings.Repeat("source-batch detail ", 700)}
	} else if len(request.SubsystemSummaries) == 1 {
		reviewer.singleSummaryPasses++
		result.UnresolvedQuestions = []string{"Compacted source-batch summary."}
	} else {
		reviewer.combinedSummaryPasses++
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: result,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: sourceFileBytes(request.Files)},
		Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func (reviewer *recoveryAccountingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.requests = append(reviewer.requests, request)
	if reviewer.primeAdaptive && !reviewer.adaptivePrimed && len(request.Files) > 0 {
		reviewer.adaptivePrimed = true
		return providers.RepositoryAnalysisResult{}, &providers.RemoteRateLimitError{
			Provider: "OpenAI", Message: "Request too large for the test provider.", LimitTokens: 10_000, RequestedTokens: 20_000, RequestTooLarge: true,
		}
	}
	if request.Mode == reviewer.incompleteMode && !reviewer.incompleteUsed {
		reviewer.incompleteUsed = true
		reviewer.incompleteScope = request.Scope
		reviewer.incompleteLimit = request.MaxOutputTokens
		reviewer.incompleteUsage = providers.Usage{PromptTokens: 4_000, CompletionTokens: request.MaxOutputTokens, ReasoningTokens: 1_500}
		return providers.RepositoryAnalysisResult{}, &providers.RemoteIncompleteError{
			Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens",
			InputTokens: reviewer.incompleteUsage.PromptTokens, OutputTokens: reviewer.incompleteUsage.CompletionTokens,
			ReasoningTokens: reviewer.incompleteUsage.ReasoningTokens, TokenLimit: 12_000,
		}
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis && reviewer.failSynthesis {
		return providers.RepositoryAnalysisResult{}, errors.New("simulated synthesis failure")
	}
	usage := providers.Usage{PromptTokens: 101, CompletionTokens: 23, ReasoningTokens: 7}
	addUsage(&reviewer.successfulUsage, usage)
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Usage: usage,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files)},
		Result: providers.RepositorySectionResult{
			Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}, nil
}

func (reviewer *semanticPartialReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	result, err := reviewer.exhaustiveBatchReviewer.ReviewRepository(ctx, request)
	if err != nil || len(request.Files) == 0 {
		return result, err
	}
	citation := providers.RepositoryCitation{Path: request.Files[0].Path, Line: request.Files[0].ContentStartLine, Summary: "Submitted model invocation"}
	result.Result.AIUses = []providers.RepositoryAIUse{{
		ID: "unsynthesized-candidate", Name: "Unsynthesized candidate", Purpose: "Draft output", Lifecycle: "development", Confidence: "medium", Evidence: []providers.RepositoryCitation{citation},
	}}
	result.Result.AIUseFacts = []providers.RepositoryAIUseFactSet{
		{AIUseID: "unsynthesized-candidate", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{string(profile.ActivityInference)}, Confidence: "medium", Rationale: "The excerpt calls a model.", Evidence: []providers.RepositoryCitation{citation},
		}}},
		{AIUseID: "shared-confirmed-use", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{string(profile.ActivityInference)}, Confidence: "medium", Rationale: "One bounded part of the confirmed use calls a model.", Evidence: []providers.RepositoryCitation{citation},
		}}},
	}
	result.Result.ObjectiveObservations = []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "pack/oversight", AIUseID: "shared-confirmed-use", Strength: providers.StrengthPartial, Confidence: "medium",
		Rationale: "One batch contains a possible safeguard.", SupportingEvidence: []providers.RepositoryCitation{citation},
	}}
	return result, nil
}

func (reviewer *exhaustiveBatchReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.requests = append(reviewer.requests, request)
	if len(request.Files) > 0 {
		reviewer.sourceRequests++
		if reviewer.failSourceAt > 0 && reviewer.sourceRequests == reviewer.failSourceAt {
			return providers.RepositoryAnalysisResult{}, errors.New("simulated provider failure")
		}
	}
	result := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI,
		Model:    "test",
		Coverage: providers.RepositoryCoverage{
			Mode:           request.Mode,
			FilesSubmitted: len(request.Files),
			BytesSubmitted: sourceFileBytes(request.Files),
		},
		Result: result,
		Usage:  providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func TestRunTargetedReviewsEveryCandidateAcrossBoundedBatches(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &exhaustiveBatchReviewer{}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	if reviewer.sourceRequests < 2 {
		t.Fatalf("targeted review used %d source request; fixture must be reviewed across multiple bounded requests", reviewer.sourceRequests)
	}
	if result.Coverage.FilesSubmitted != len(expectedPaths) {
		t.Fatalf("coverage reports %d submitted file(s), want all %d candidate files: %#v", result.Coverage.FilesSubmitted, len(expectedPaths), result.Coverage)
	}
	if result.Coverage.Mode != providers.RepositoryAnalysisTargeted {
		t.Fatalf("batched targeted review mode = %q, want %q", result.Coverage.Mode, providers.RepositoryAnalysisTargeted)
	}
	if result.Coverage.Subsystems != reviewer.sourceRequests {
		t.Fatalf("coverage reports %d batch(es), want %d: %#v", result.Coverage.Subsystems, reviewer.sourceRequests, result.Coverage)
	}
	if result.Coverage.SourceBatchesCompleted != reviewer.sourceRequests || result.Coverage.SourceBatchesTotal != reviewer.sourceRequests {
		t.Fatalf("completed targeted batch counters = %d/%d, want %d/%d: %#v", result.Coverage.SourceBatchesCompleted, result.Coverage.SourceBatchesTotal, reviewer.sourceRequests, reviewer.sourceRequests, result.Coverage)
	}

	perRequestBudget := sourceBudget(targetedRemoteInputTokens, nil, nil, nil)
	for index, request := range reviewer.requests {
		if len(request.Files) == 0 {
			continue
		}
		if request.Mode != providers.RepositoryAnalysisTargeted {
			t.Errorf("source request %d mode = %q, want compact targeted schema", index+1, request.Mode)
		}
		if request.MaxOutputTokens != targetedRemoteOutputTokens {
			t.Errorf("source request %d output allowance = %d, want targeted bootstrap %d", index+1, request.MaxOutputTokens, targetedRemoteOutputTokens)
		}
		if size := requestContextBytes(request.Files, request.Graph); size > perRequestBudget {
			t.Errorf("source request %d contains %d bytes, exceeding per-request budget %d", index+1, size, perRequestBudget)
		}
	}
}

func TestRunTargetedDropsBulkyNonGroupingFieldsBeforeOneSynthesis(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &compactingSynthesisReviewer{}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	if reviewer.singleSummaryPasses != 0 {
		t.Fatalf("singleton synthesis passes = %d, want no per-summary compaction calls", reviewer.singleSummaryPasses)
	}
	if reviewer.combinedSummaryPasses != 1 {
		t.Fatalf("combined synthesis passes = %d, want one compact grouping request", reviewer.combinedSummaryPasses)
	}
	for _, request := range reviewer.requests {
		if request.Mode != providers.RepositoryAnalysisSynthesis {
			continue
		}
		if !request.CompactSynthesis {
			t.Fatal("synthesis request did not use the compact grouping contract")
		}
		for _, summary := range request.SubsystemSummaries {
			if len(summary.UnresolvedQuestions) != 0 {
				t.Fatal("bulky source-batch questions leaked into compact synthesis input")
			}
		}
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal || result.Coverage.SourceBatchesTotal < 2 {
		t.Fatalf("completed compacted synthesis coverage = %#v", result.Coverage)
	}
}

func TestRunTargetedAssignsTrustedIDAfterCrossBatchGrouping(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &crossBatchGroupingReviewer{}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	if reviewer.sourceRequests < 2 {
		t.Fatalf("source requests = %d, want a cross-batch grouping fixture", reviewer.sourceRequests)
	}
	if len(result.Result.AIUses) != 1 {
		t.Fatalf("final inferred uses = %#v, want one grouped use", result.Result.AIUses)
	}
	use := result.Result.AIUses[0]
	if len(use.MemberObservationIDs) != reviewer.sourceRequests {
		t.Fatalf("group membership = %v, want one observation from each of %d source batches", use.MemberObservationIDs, reviewer.sourceRequests)
	}
	if use.ID != inferredCandidateID(use.MemberObservationIDs) || strings.Contains(use.ID, "model-proposed-group") || strings.Contains(use.ID, "model-local-key") {
		t.Fatalf("candidate ID %q was not assigned locally from exact membership %v", use.ID, use.MemberObservationIDs)
	}
	if len(result.Result.AIUseFacts) != 1 || result.Result.AIUseFacts[0].AIUseID != use.ID {
		t.Fatalf("group fact binding was not rewritten to trusted ID: use=%#v facts=%#v", use, result.Result.AIUseFacts)
	}
}

func TestRunTargetedPreservesOversizedInvocationAnchorByTrimmingSecondaryContext(t *testing.T) {
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		oversizedInvocationFile("ai/primary.py", 140),
		{Path: "ai/catalog.py", Kind: discovery.KindSource, Content: []byte("AVAILABLE_MODELS = ['gpt-test']\n")},
	}}
	reviewer := &exhaustiveBatchReviewer{}

	_, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	foundInvocation := false
	for _, request := range reviewer.requests {
		for _, file := range request.Files {
			if file.Path == "ai/primary.py" && strings.Contains(file.Content, "client.responses.create") {
				foundInvocation = true
			}
		}
	}
	if !foundInvocation {
		t.Fatalf("the highest-tier executable invocation was dropped instead of preserving source and trimming secondary graph context; requests=%#v", sourceRequestPaths(reviewer.requests))
	}
}

func TestRunTargetedPartialProviderFailureReturnsExactIncompleteCoverage(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &exhaustiveBatchReviewer{failSourceAt: 2}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil {
		t.Fatal("expected the second source batch failure to make repository analysis incomplete")
	}
	if reviewer.sourceRequests != 2 {
		t.Fatalf("provider failure occurred after %d source request(s), want 2", reviewer.sourceRequests)
	}

	attemptedPaths, attemptedBytes := sourceAttemptCoverage(reviewer.requests, reviewer.failSourceAt)
	completedPaths, _ := sourceAttemptCoverage(reviewer.requests, reviewer.failSourceAt-1)
	if len(completedPaths) == 0 || len(completedPaths) >= len(expectedPaths) || len(attemptedPaths) <= len(completedPaths) || len(attemptedPaths) > len(expectedPaths) {
		t.Fatalf("fixture did not create meaningful attempted partial coverage: completed=%#v attempted=%#v expected=%#v", completedPaths, attemptedPaths, expectedPaths)
	}
	if result.Coverage.FilesSubmitted != len(attemptedPaths) || result.Coverage.BytesSubmitted != attemptedBytes {
		t.Fatalf("partial transfer coverage = %d file(s)/%d byte(s), want attempted %d/%d including failed request: %#v", result.Coverage.FilesSubmitted, result.Coverage.BytesSubmitted, len(attemptedPaths), attemptedBytes, result.Coverage)
	}
	if result.Coverage.Subsystems != 1 {
		t.Fatalf("partial result reports %d completed batches, want 1: %#v", result.Coverage.Subsystems, result.Coverage)
	}
	if result.Coverage.SourceBatchesCompleted != 1 || result.Coverage.SourceBatchesTotal != reviewer.sourceRequests {
		t.Fatalf("partial targeted batch counters = %d/%d, want 1/%d: %#v", result.Coverage.SourceBatchesCompleted, result.Coverage.SourceBatchesTotal, reviewer.sourceRequests, result.Coverage)
	}
	if len(result.Result.AIUses) == 0 && len(result.Result.UnresolvedQuestions) == 0 && len(result.Notes) == 0 {
		t.Fatal("partial result is indistinguishable from a completed zero-use review")
	}
}

func TestRunTargetedSplitsEscapeHeavyCandidatesByEncodedRequestSize(t *testing.T) {
	repository := discovery.Repository{Root: "."}
	expectedPaths := []string{"ai/escaped_00.py", "ai/escaped_01.py", "ai/escaped_02.py"}
	for _, path := range expectedPaths {
		repository.Files = append(repository.Files, escapeHeavyInvocationFile(path))
	}
	reviewer := &exhaustiveBatchReviewer{}

	_, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := sourceBudget(targetedRemoteInputTokens, nil, nil, nil)
	seen := make(map[string]bool, len(expectedPaths))
	submissions := make(map[string]int, len(expectedPaths))
	sourceRequests := 0
	for index, request := range reviewer.requests {
		if len(request.Files) == 0 {
			continue
		}
		sourceRequests++
		if size := encodedRequestContextBytes(t, request); size > budget {
			t.Errorf("source request %d has %d encoded context bytes, exceeding per-request budget %d", index+1, size, budget)
		}
		for _, file := range request.Files {
			seen[file.Path] = true
			submissions[file.Path]++
		}
	}
	if sourceRequests < 2 {
		t.Fatalf("escape-heavy candidates used %d source request(s), want bounded multi-request partitioning", sourceRequests)
	}
	for _, path := range expectedPaths {
		if !seen[path] {
			t.Errorf("escape-heavy candidate %q was never submitted", path)
		}
		if submissions[path] != 1 {
			t.Errorf("escape-heavy candidate %q was submitted %d time(s), want exactly once across bounded requests", path, submissions[path])
		}
	}
}

func TestRunTargetedPartialFailureExposesNoUnsynthesizedSemantics(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &semanticPartialReviewer{exhaustiveBatchReviewer: exhaustiveBatchReviewer{failSourceAt: 2}}
	confirmed := providers.RepositoryConfirmedAIUse{
		ID: "shared-confirmed-use", Name: "Shared confirmed use", Description: "Spans all bounded candidate batches", Paths: []string{"ai/**"},
	}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000, ConfirmedAIUses: []providers.RepositoryConfirmedAIUse{confirmed},
	})
	if err == nil {
		t.Fatal("expected provider failure before synthesis")
	}
	if len(result.Result.AIUses) != 0 || len(result.Result.AIUseFacts) != 0 || len(result.Result.ObjectiveObservations) != 0 || len(result.Result.UnmappedObservations) != 0 {
		t.Fatalf("partial result leaked unsynthesized semantic conclusions: %#v", result.Result)
	}
	if len(result.Result.UnresolvedQuestions) == 0 || len(result.Notes) == 0 {
		t.Fatalf("partial result lost its non-semantic incomplete-review explanation: %#v", result)
	}
}

func TestRunTargetedSourceBatchOutputRecoveryAccountsForBothAttempts(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &recoveryAccountingReviewer{incompleteMode: providers.RepositoryAnalysisTargeted, primeAdaptive: true}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryAccounting(t, reviewer, result, providers.RepositoryAnalysisTargeted)
}

func TestRunTargetedSynthesisOutputRecoveryAccountsForBothAttempts(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &recoveryAccountingReviewer{incompleteMode: providers.RepositoryAnalysisSynthesis, primeAdaptive: true}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryAccounting(t, reviewer, result, providers.RepositoryAnalysisSynthesis)
}

func TestRunTargetedSynthesisFailureDistinguishesCompleteSourceCoverage(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &recoveryAccountingReviewer{failSynthesis: true}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil {
		t.Fatal("expected synthesis failure")
	}
	sourceRequests := 0
	for _, request := range reviewer.requests {
		if len(request.Files) > 0 {
			sourceRequests++
		}
	}
	if sourceRequests < 2 || result.Coverage.Subsystems != sourceRequests {
		t.Fatalf("source coverage = %d completed batch(es), want all %d attempted source requests: %#v", result.Coverage.Subsystems, sourceRequests, result.Coverage)
	}
	if result.Coverage.SourceBatchesCompleted != sourceRequests || result.Coverage.SourceBatchesTotal != sourceRequests {
		t.Fatalf("synthesis-failed source batch counters = %d/%d, want %d/%d: %#v", result.Coverage.SourceBatchesCompleted, result.Coverage.SourceBatchesTotal, sourceRequests, sourceRequests, result.Coverage)
	}
	status := err.Error() + " " + strings.Join(result.Notes, " ") + " " + strings.Join(result.Result.UnresolvedQuestions, " ")
	if !strings.Contains(status, "all candidate evidence batches were reviewed") || !strings.Contains(status, "synthesis did not complete") {
		t.Fatalf("synthesis failure did not distinguish complete source coverage: %s", status)
	}
	if strings.Contains(status, "remaining candidate evidence was not reviewed") {
		t.Fatalf("synthesis failure falsely reports unreviewed candidate evidence: %s", status)
	}
}

func TestRunTargetedReturnsZeroUsesOnlyAfterExhaustiveCoverage(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(10)
	reviewer := &exhaustiveBatchReviewer{}

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUses) != 0 {
		t.Fatalf("grounded empty reviewer returned unexpected AI uses: %#v", result.Result.AIUses)
	}
	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	if result.Coverage.FilesSubmitted != len(expectedPaths) || result.Coverage.Subsystems != reviewer.sourceRequests {
		t.Fatalf("zero-use result was accepted without exact complete coverage: %#v", result.Coverage)
	}
}

func TestRunTargetedExhaustsChangedScopeWithoutCrossingIt(t *testing.T) {
	full, changed, expectedPaths := exhaustiveChangedRepository()
	scoped, scope := ScopeChangedReview(full, changed)
	scopedPaths := repositoryPathSet(scoped)
	for _, path := range expectedPaths {
		if !scopedPaths[path] {
			t.Fatalf("changed-scope fixture did not include connected path %q: %#v", path, scopedPaths)
		}
	}
	if scopedPaths["unrelated/ai.py"] {
		t.Fatalf("changed-scope fixture unexpectedly included unrelated source: %#v", scopedPaths)
	}
	reviewer := &exhaustiveBatchReviewer{}

	result, err := Run(context.Background(), reviewer, scoped, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExhaustiveSourceCoverage(t, reviewer.requests, expectedPaths)
	for _, paths := range sourceRequestPaths(reviewer.requests) {
		for _, path := range paths {
			if path == "unrelated/ai.py" {
				t.Fatalf("changed-scope review leaked unrelated repository source: %#v", sourceRequestPaths(reviewer.requests))
			}
		}
	}
	scope.Apply(&result)
	if result.Coverage.ReviewScope != providers.RepositoryReviewScopeChanged || result.Coverage.RepositoryFiles != len(full.Files) || result.Coverage.ScopeFiles != len(scoped.Files) {
		t.Fatalf("changed-scope coverage was not preserved across batches: %#v", result.Coverage)
	}
}

func exhaustiveCandidateRepository(count int) (discovery.Repository, []string) {
	repository := discovery.Repository{Root: "."}
	paths := make([]string, 0, count)
	for index := 0; index < count; index++ {
		path := fmt.Sprintf("ai/use_%02d.py", index)
		paths = append(paths, path)
		repository.Files = append(repository.Files, oversizedInvocationFile(path, 24))
	}
	return repository, paths
}

func oversizedInvocationFile(path string, surroundingCalls int) discovery.File {
	var content strings.Builder
	content.WriteString("from openai import OpenAI\nclient = OpenAI()\n\ndef generate(prompt):\n")
	for index := 0; index < surroundingCalls; index++ {
		fmt.Fprintf(&content, "    audit_step_%03d(prompt)  # bounded workflow relationship %03d\n", index, index)
	}
	content.WriteString("    return client.responses.create(model='gpt-test', input=prompt)\n")
	return discovery.File{Path: path, Kind: discovery.KindSource, Content: []byte(content.String()), Size: int64(content.Len())}
}

func escapeHeavyInvocationFile(path string) discovery.File {
	var content strings.Builder
	content.WriteString("from openai import OpenAI\nclient = OpenAI()\n\ndef generate(prompt):\n")
	padding := strings.Repeat(`\\\"quoted\\path`, 8)
	for index := 0; index < 62; index++ {
		fmt.Fprintf(&content, "    # before-%03d %s\n", index, padding)
	}
	content.WriteString("    result = client.responses.create(model='gpt-test', input=prompt)\n")
	for index := 0; index < 62; index++ {
		fmt.Fprintf(&content, "    # after-%03d %s\n", index, padding)
	}
	content.WriteString("    return result\n")
	return discovery.File{Path: path, Kind: discovery.KindSource, Content: []byte(content.String()), Size: int64(content.Len())}
}

func exhaustiveChangedRepository() (discovery.Repository, discovery.Repository, []string) {
	changedFile := oversizedInvocationFile("feature/changed.py", 24)
	changedFile.Content = append([]byte("from .connected_a import invoke_a\nfrom .connected_b import invoke_b\n"), changedFile.Content...)
	changedFile.Size = int64(len(changedFile.Content))
	connectedA := oversizedInvocationFile("feature/connected_a.py", 24)
	connectedB := oversizedInvocationFile("feature/connected_b.py", 24)
	unrelated := oversizedInvocationFile("unrelated/ai.py", 24)
	full := discovery.Repository{Root: ".", Files: []discovery.File{changedFile, connectedA, connectedB, unrelated}}
	changed := discovery.Repository{Root: ".", Files: []discovery.File{changedFile}}
	return full, changed, []string{"feature/changed.py", "feature/connected_a.py", "feature/connected_b.py"}
}

func assertExhaustiveSourceCoverage(t *testing.T, requests []providers.RepositoryAnalysisRequest, expected []string) {
	t.Helper()
	want := make(map[string]bool, len(expected))
	for _, path := range expected {
		want[path] = true
	}
	seen := make(map[string]int, len(expected))
	for _, request := range requests {
		for _, file := range request.Files {
			seen[file.Path]++
		}
	}
	for path := range want {
		if seen[path] != 1 {
			t.Errorf("candidate %q was submitted %d time(s), want exactly once", path, seen[path])
		}
	}
	for path := range seen {
		if !want[path] {
			t.Errorf("unexpected source candidate %q was submitted", path)
		}
	}
	if t.Failed() {
		t.Fatalf("submitted source requests: %#v", sourceRequestPaths(requests))
	}
}

func sourceRequestPaths(requests []providers.RepositoryAnalysisRequest) [][]string {
	result := make([][]string, 0, len(requests))
	for _, request := range requests {
		if len(request.Files) == 0 {
			continue
		}
		paths := make([]string, 0, len(request.Files))
		for _, file := range request.Files {
			paths = append(paths, file.Path)
		}
		sort.Strings(paths)
		result = append(result, paths)
	}
	return result
}

func sourceAttemptCoverage(requests []providers.RepositoryAnalysisRequest, throughSourceRequest int) ([]string, int64) {
	var paths []string
	var bytes int64
	sourceRequest := 0
	for _, request := range requests {
		if len(request.Files) == 0 {
			continue
		}
		sourceRequest++
		if sourceRequest > throughSourceRequest {
			break
		}
		for _, file := range request.Files {
			paths = append(paths, file.Path)
			bytes += int64(len(file.Content))
		}
	}
	sort.Strings(paths)
	return paths, bytes
}

func encodedRequestContextBytes(t *testing.T, request providers.RepositoryAnalysisRequest) int64 {
	t.Helper()
	encoded, err := json.Marshal(struct {
		Files []providers.RepositorySourceFile `json:"files"`
		Graph providers.RepositoryGraphContext `json:"repository_graph"`
	}{Files: request.Files, Graph: request.Graph})
	if err != nil {
		t.Fatal(err)
	}
	return int64(len(encoded))
}

func assertRecoveryAccounting(t *testing.T, reviewer *recoveryAccountingReviewer, result providers.RepositoryAnalysisResult, mode providers.RepositoryAnalysisMode) {
	t.Helper()
	if !reviewer.incompleteUsed {
		t.Fatalf("%s recovery fixture never produced an incomplete response", mode)
	}
	want := reviewer.successfulUsage
	addUsage(&want, reviewer.incompleteUsage)
	if result.Usage.PromptTokens != want.PromptTokens || result.Usage.CompletionTokens != want.CompletionTokens || result.Usage.ReasoningTokens != want.ReasoningTokens {
		t.Fatalf("%s recovery usage = %#v, want incomplete attempt plus every successful response %#v", mode, result.Usage, want)
	}
	var recovery *providers.RepositoryAnalysisRequest
	for _, request := range reviewer.requests {
		if request.Mode == mode && request.Scope == reviewer.incompleteScope && request.MaxOutputTokens > reviewer.incompleteLimit {
			copy := request
			recovery = &copy
			break
		}
	}
	if recovery == nil {
		t.Fatalf("%s recovery did not retry scope %q above the exhausted %d-token output allowance", mode, reviewer.incompleteScope, reviewer.incompleteLimit)
	}
}
