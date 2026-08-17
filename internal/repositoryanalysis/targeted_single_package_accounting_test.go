package repositoryanalysis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type targetedReviewStep func(providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error)

type targetedScriptedReviewer struct {
	steps    []targetedReviewStep
	requests []providers.RepositoryAnalysisRequest
}

type targetedUsageErrorReviewer struct {
	requests []providers.RepositoryAnalysisRequest
	usage    providers.Usage
}

func (reviewer *targetedUsageErrorReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	result := targetedSuccessfulResult(request, reviewer.usage, true)
	return result, errors.New("simulated invalid structured model response")
}

type targetedRateLimitUsageReviewer struct {
	requests []providers.RepositoryAnalysisRequest
	first    providers.Usage
	second   providers.Usage
}

func (reviewer *targetedRateLimitUsageReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	if len(reviewer.requests) == 1 {
		return targetedSuccessfulResult(request, reviewer.first, false), &providers.RemoteRateLimitError{
			Provider: "OpenAI", Message: "temporary rate limit", RetryAfter: time.Second,
		}
	}
	return targetedSuccessfulResult(request, reviewer.second, false), nil
}

func (reviewer *targetedScriptedReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	index := len(reviewer.requests) - 1
	if index >= len(reviewer.steps) {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("unexpected targeted request %d", index+1)
	}
	return reviewer.steps[index](request)
}

func TestRunSingleTargetedInitialFailureReturnsAttemptedNonSemanticCoverage(t *testing.T) {
	reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
		func(providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			return providers.RepositoryAnalysisResult{}, errors.New("simulated initial provider failure")
		},
	}}

	result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
	if err == nil {
		t.Fatal("expected initial provider failure")
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertIncompleteNonSemanticResult(t, result)
	if result.Coverage.Mode != providers.RepositoryAnalysisTargeted || result.Coverage.Subsystems != 0 {
		t.Fatalf("initial failure coverage mode/batches = %#v", result.Coverage)
	}
}

func TestRunSingleTargetedOutputRecoveryFailureCountsBothTransfersAndIncompleteUsage(t *testing.T) {
	incompleteUsage := providers.Usage{PromptTokens: 4_200, CompletionTokens: 4_096, ReasoningTokens: 3_000}
	reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
		func(providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			return providers.RepositoryAnalysisResult{}, targetedIncompleteError(incompleteUsage)
		},
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			if !request.OutputRecovery || request.AllowFollowUp {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("second request is not compact output recovery: %#v", request)
			}
			return providers.RepositoryAnalysisResult{}, errors.New("simulated output recovery failure")
		},
	}}

	result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
	if err == nil {
		t.Fatal("expected output recovery failure")
	}
	if len(reviewer.requests) != 2 {
		t.Fatalf("output recovery request count = %d, want 2", len(reviewer.requests))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertIncompleteNonSemanticResult(t, result)
	assertUsageEquals(t, result.Usage, incompleteUsage, "failed output recovery")
}

func TestRunSingleTargetedFollowUpFailurePreservesAccountingWithoutInitialSemantics(t *testing.T) {
	initialUsage := providers.Usage{PromptTokens: 800, CompletionTokens: 120, ReasoningTokens: 70}
	reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			result := targetedSuccessfulResult(request, initialUsage, true)
			result.FollowUpPlan = targetedTestFollowUpPlan()
			return result, nil
		},
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			if request.AllowFollowUp || len(request.Files) < 2 {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("final follow-up package = %#v", request)
			}
			return providers.RepositoryAnalysisResult{}, errors.New("simulated follow-up provider failure")
		},
	}}

	result, err := runSingleTargetedFixture(reviewer, targetedFollowUpRepository())
	if err == nil {
		t.Fatal("expected follow-up provider failure")
	}
	if len(reviewer.requests) != 2 {
		t.Fatalf("follow-up request count = %d, want 2", len(reviewer.requests))
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertIncompleteNonSemanticResult(t, result)
	assertUsageEquals(t, result.Usage, initialUsage, "failed follow-up")
}

func TestRunSingleTargetedSuccessfulSecondCallsReportTotalTransfersAndUsage(t *testing.T) {
	t.Run("hosted output recovery without context ceiling header", func(t *testing.T) {
		incompleteUsage := providers.Usage{PromptTokens: 4_200, CompletionTokens: 4_096, ReasoningTokens: 3_000}
		recoveryUsage := providers.Usage{PromptTokens: 4_100, CompletionTokens: 700, ReasoningTokens: 300}
		reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
			func(providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				return providers.RepositoryAnalysisResult{}, &providers.RemoteIncompleteError{
					Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens",
					InputTokens: incompleteUsage.PromptTokens, OutputTokens: incompleteUsage.CompletionTokens, ReasoningTokens: incompleteUsage.ReasoningTokens,
				}
			},
			func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				if request.MaxOutputTokens != 8_192 {
					return providers.RepositoryAnalysisResult{}, fmt.Errorf("hosted recovery output = %d, want 8192 without conflating TPM headers with context limits", request.MaxOutputTokens)
				}
				return targetedSuccessfulResult(request, recoveryUsage, false), nil
			},
		}}

		result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
		if err != nil {
			t.Fatal(err)
		}
		assertTargetedAttemptCoverage(t, result, reviewer.requests)
		wantUsage := recoveryUsage
		addUsage(&wantUsage, incompleteUsage)
		assertUsageEquals(t, result.Usage, wantUsage, "hosted output recovery without context ceiling")
	})

	t.Run("output recovery", func(t *testing.T) {
		incompleteUsage := providers.Usage{PromptTokens: 4_200, CompletionTokens: 4_096, ReasoningTokens: 3_000}
		recoveryUsage := providers.Usage{PromptTokens: 4_100, CompletionTokens: 350, ReasoningTokens: 180}
		reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
			func(providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				return providers.RepositoryAnalysisResult{}, targetedIncompleteError(incompleteUsage)
			},
			func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				return targetedSuccessfulResult(request, recoveryUsage, false), nil
			},
		}}

		result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
		if err != nil {
			t.Fatal(err)
		}
		assertTargetedAttemptCoverage(t, result, reviewer.requests)
		wantUsage := recoveryUsage
		addUsage(&wantUsage, incompleteUsage)
		assertUsageEquals(t, result.Usage, wantUsage, "successful output recovery")
		if !result.OutputRecoveryUsed {
			t.Fatal("successful recovery was not recorded")
		}
	})

	t.Run("bounded follow-up", func(t *testing.T) {
		initialUsage := providers.Usage{PromptTokens: 800, CompletionTokens: 120, ReasoningTokens: 70}
		finalUsage := providers.Usage{PromptTokens: 1_100, CompletionTokens: 160, ReasoningTokens: 90}
		reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
			func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				result := targetedSuccessfulResult(request, initialUsage, true)
				result.FollowUpPlan = targetedTestFollowUpPlan()
				return result, nil
			},
			func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				return targetedSuccessfulResult(request, finalUsage, true), nil
			},
		}}

		result, err := runSingleTargetedFixture(reviewer, targetedFollowUpRepository())
		if err != nil {
			t.Fatal(err)
		}
		assertTargetedAttemptCoverage(t, result, reviewer.requests)
		wantUsage := finalUsage
		addUsage(&wantUsage, initialUsage)
		assertUsageEquals(t, result.Usage, wantUsage, "successful follow-up")
		if !result.FollowUpRequested || result.FollowUpExcerpts != 1 {
			t.Fatalf("successful follow-up metadata = %#v", result)
		}
	})
}

func TestRunSingleTargetedFollowUpRequestTooLargeRetriesWithoutDroppingEvidenceOrUsage(t *testing.T) {
	initialUsage := providers.Usage{PromptTokens: 800, CompletionTokens: 120, ReasoningTokens: 70}
	limitUsage := providers.Usage{PromptTokens: 6_700, CompletionTokens: 2_900, ReasoningTokens: 2_200}
	retryUsage := providers.Usage{PromptTokens: 6_700, CompletionTokens: 450, ReasoningTokens: 260}
	reviewer := &targetedScriptedReviewer{steps: []targetedReviewStep{
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			result := targetedSuccessfulResult(request, initialUsage, true)
			result.FollowUpPlan = targetedTestFollowUpPlan()
			return result, nil
		},
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			if request.AllowFollowUp || !hasRepositorySourcePath(request.Files, "review/approval.py") {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("enlarged follow-up package = %#v", request)
			}
			return targetedSuccessfulResult(request, limitUsage, false), requestTooLargeAtTenThousand()
		},
		func(request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
			if request.AllowFollowUp || !hasRepositorySourcePath(request.Files, "review/approval.py") {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("provider-limit retry lost follow-up evidence: %#v", request)
			}
			result := targetedSuccessfulResult(request, retryUsage, true)
			result.Result.AIUses[0].Evidence[0].Path = "review/approval.py"
			return result, nil
		},
	}}

	result, err := runSingleTargetedFixture(reviewer, targetedFollowUpRepository())
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 3 {
		t.Fatalf("targeted follow-up request count = %d, want initial, rejected enlarged follow-up, and bounded retry", len(reviewer.requests))
	}
	failedFollowUp, retriedFollowUp := reviewer.requests[1], reviewer.requests[2]
	if !reflect.DeepEqual(sourceFilePaths(failedFollowUp.Files), sourceFilePaths(retriedFollowUp.Files)) {
		t.Fatalf("provider-limit retry changed the enlarged follow-up evidence: failed=%v retry=%v", sourceFilePaths(failedFollowUp.Files), sourceFilePaths(retriedFollowUp.Files))
	}
	if retriedFollowUp.MaxOutputTokens >= failedFollowUp.MaxOutputTokens {
		t.Fatalf("provider-limit retry output allowance = %d, want less than %d", retriedFollowUp.MaxOutputTokens, failedFollowUp.MaxOutputTokens)
	}
	if retriedFollowUp.MaxOutputTokens != 2_000 {
		t.Fatalf("targeted follow-up 10k retry output allowance = %d, want 2000", retriedFollowUp.MaxOutputTokens)
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	wantUsage := initialUsage
	addUsage(&wantUsage, limitUsage)
	addUsage(&wantUsage, retryUsage)
	assertUsageEquals(t, result.Usage, wantUsage, "successful provider-limit follow-up retry")
	if !result.FollowUpRequested || result.FollowUpExcerpts != 1 {
		t.Fatalf("provider-limit follow-up metadata = requested %v / excerpts %d, want true/1", result.FollowUpRequested, result.FollowUpExcerpts)
	}
	if len(result.Result.AIUses) != 1 || len(result.Result.AIUses[0].Evidence) != 1 || result.Result.AIUses[0].Evidence[0].Path != "review/approval.py" {
		t.Fatalf("successful provider-limit follow-up retry lost reviewed semantics: %#v", result.Result.AIUses)
	}
}

func TestRunSingleTargetedAggregatesUsageFromInvalidModelResponse(t *testing.T) {
	usage := providers.Usage{PromptTokens: 500, CompletionTokens: 90, ReasoningTokens: 45, TotalDurationNS: 1234}
	reviewer := &targetedUsageErrorReviewer{usage: usage}

	result, err := runSingleTargetedFixture(reviewer, singleTargetedRepository())
	if err == nil {
		t.Fatal("expected invalid model response error")
	}
	assertTargetedAttemptCoverage(t, result, reviewer.requests)
	assertUsageEquals(t, result.Usage, usage, "single-package invalid response")
	assertIncompleteNonSemanticResult(t, result)
}

func TestRunHierarchicalAggregatesUsageFromInvalidModelResponse(t *testing.T) {
	usage := providers.Usage{PromptTokens: 700, CompletionTokens: 110, ReasoningTokens: 65, TotalDurationNS: 4321}
	reviewer := &targetedUsageErrorReviewer{usage: usage}
	repository, _ := exhaustiveCandidateRepository(10)

	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err == nil {
		t.Fatal("expected invalid model response error")
	}
	if len(reviewer.requests) != 1 || len(reviewer.requests[0].Files) == 0 {
		t.Fatalf("hierarchical invalid-response fixture requests = %#v", sourceRequestPaths(reviewer.requests))
	}
	assertUsageEquals(t, result.Usage, usage, "hierarchical invalid response")
	assertIncompleteNonSemanticResult(t, result)
}

func TestReviewRepositoryWithRetryPreservesResultUsageAcrossAttempts(t *testing.T) {
	first := providers.Usage{PromptTokens: 300, CompletionTokens: 40, ReasoningTokens: 20, TotalDurationNS: 100}
	second := providers.Usage{PromptTokens: 280, CompletionTokens: 35, ReasoningTokens: 15, TotalDurationNS: 90}
	reviewer := &targetedRateLimitUsageReviewer{first: first, second: second}

	result, err := reviewRepositoryWithRetry(context.Background(), reviewer, providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisTargeted, Scope: ".", Files: []providers.RepositorySourceFile{{Path: "app.py", Kind: "source", Content: "model()"}},
	}, Options{Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	want := first
	addUsage(&want, second)
	assertUsageEquals(t, result.Usage, want, "rate-limit retry")
	if len(reviewer.requests) != 2 {
		t.Fatalf("rate-limit request count = %d, want 2", len(reviewer.requests))
	}
	if result.Coverage.FilesSubmitted != 2 || result.Coverage.BytesSubmitted != 2*int64(len("model()")) {
		t.Fatalf("rate-limit retry transfer coverage = %#v, want both provider attempts", result.Coverage)
	}
}

func TestTargetedBatchProgressDoesNotReportAnalysisBeforeSuccess(t *testing.T) {
	repository, _ := exhaustiveCandidateRepository(10)
	reviewer := &targetedUsageErrorReviewer{usage: providers.Usage{PromptTokens: 10, CompletionTokens: 2}}
	var events []Progress

	_, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		OnProgress: func(value Progress) error {
			events = append(events, value)
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected first targeted batch to fail")
	}
	queues, starts, completions := 0, 0, 0
	queueIndex, startIndex := -1, -1
	for index, event := range events {
		switch event.Stage {
		case "targeted-batch-queue":
			queues++
			queueIndex = index
		case "targeted-batch-start":
			starts++
			startIndex = index
		case "targeted-batch":
			completions++
		}
	}
	if queues != 1 || starts != 1 || completions != 0 || queueIndex >= startIndex {
		t.Fatalf("failed targeted batch progress queue/start/completion = %d/%d/%d, want 1/1/0 in order: %#v", queues, starts, completions, events)
	}
}

func runSingleTargetedFixture(reviewer Reviewer, repository discovery.Repository) (providers.RepositoryAnalysisResult, error) {
	return Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
}

func singleTargetedRepository() discovery.Repository {
	content := []byte("from openai import OpenAI\nclient = OpenAI()\ndef generate(prompt):\n    return client.responses.create(model='gpt-test', input=prompt)\n")
	return discovery.Repository{Root: ".", Files: []discovery.File{{Path: "app.py", Kind: discovery.KindSource, Content: content, Size: int64(len(content))}}}
}

func targetedFollowUpRepository() discovery.Repository {
	repository := singleTargetedRepository()
	content := []byte("def approve_response(value):\n    return ask_human(value)\n")
	repository.Files = append(repository.Files, discovery.File{Path: "review/approval.py", Kind: discovery.KindSource, Content: content, Size: int64(len(content))})
	return repository
}

func targetedIncompleteError(usage providers.Usage) error {
	return &providers.RemoteIncompleteError{
		Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens", InputTokens: usage.PromptTokens,
		OutputTokens: usage.CompletionTokens, ReasoningTokens: usage.ReasoningTokens, TokenLimit: 10_000,
	}
}

func targetedSuccessfulResult(request providers.RepositoryAnalysisRequest, usage providers.Usage, semantic bool) providers.RepositoryAnalysisResult {
	result := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if semantic && len(request.Files) > 0 {
		result.AIUses = []providers.RepositoryAIUse{{
			ID: "initial-unsynthesized", Name: "Initial candidate", Purpose: "Draft output", Lifecycle: "development", Confidence: "medium",
			Evidence: []providers.RepositoryCitation{{Path: request.Files[0].Path, Line: request.Files[0].ContentStartLine, Summary: "Submitted model call"}},
		}}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: result, Usage: usage,
		Coverage: providers.RepositoryCoverage{
			Mode: request.Mode, RepositoryFiles: request.RepositoryFiles, RepositoryBytes: request.RepositoryBytes,
			FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files),
		},
	}
}

func targetedTestFollowUpPlan() providers.TechnicalSearchPlan {
	return providers.TechnicalSearchPlan{
		Needed: true, Reason: "Approval handling may change the result.",
		Queries: []providers.TechnicalSearchQuery{{Text: "approve_response", PathHint: "review", Reason: "Find approval implementation."}},
	}
}

func hasRepositorySourcePath(files []providers.RepositorySourceFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func assertTargetedAttemptCoverage(t *testing.T, result providers.RepositoryAnalysisResult, requests []providers.RepositoryAnalysisRequest) {
	t.Helper()
	wantFiles := 0
	var wantBytes int64
	for _, request := range requests {
		wantFiles += len(request.Files)
		wantBytes += repositorySourceContentBytes(request.Files)
	}
	if result.Coverage.FilesSubmitted != wantFiles || result.Coverage.BytesSubmitted != wantBytes || result.Coverage.ProviderRequests != len(requests) {
		t.Fatalf("targeted transfer coverage = %d file excerpt(s)/%d byte(s)/%d request(s), want all attempted transfers %d/%d/%d: %#v", result.Coverage.FilesSubmitted, result.Coverage.BytesSubmitted, result.Coverage.ProviderRequests, wantFiles, wantBytes, len(requests), result.Coverage)
	}
}

func assertIncompleteNonSemanticResult(t *testing.T, result providers.RepositoryAnalysisResult) {
	t.Helper()
	if len(result.Result.AIUses) != 0 || len(result.Result.AIUseFacts) != 0 || len(result.Result.ObjectiveObservations) != 0 || len(result.Result.UnmappedObservations) != 0 {
		t.Fatalf("incomplete targeted result exposed unsynthesized semantics: %#v", result.Result)
	}
	if len(result.Result.UnresolvedQuestions) == 0 || len(result.Notes) == 0 {
		t.Fatalf("incomplete targeted result lacks explicit incomplete-review context: %#v", result)
	}
}

func assertUsageEquals(t *testing.T, got, want providers.Usage, context string) {
	t.Helper()
	if got.PromptTokens != want.PromptTokens || got.CompletionTokens != want.CompletionTokens || got.ReasoningTokens != want.ReasoningTokens {
		t.Fatalf("%s usage = %#v, want %#v", context, got, want)
	}
}
