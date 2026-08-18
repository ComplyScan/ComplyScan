package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/modelqualification"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type findingReviewFunc func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error)

func (function findingReviewFunc) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return function(ctx, request)
}

func TestFindingReviewSkipsDeterministicInventoryNoticesAndEmptyProviderCall(t *testing.T) {
	findings := []rules.Finding{
		{RuleID: "AI-DISC-001", Severity: rules.SeverityInfo, Category: "ai-inventory"},
		{RuleID: "CODE-001", Severity: rules.SeverityLow, Category: "robustness"},
	}
	selected := findingsForAdvisoryReview(findings)
	if len(selected) != 1 || selected[0].RuleID != "CODE-001" {
		t.Fatalf("advisory finding selection = %#v, want only the actionable code finding", selected)
	}

	calls := 0
	result, err := reviewFindingsWithRetry(context.Background(), findingReviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		return providers.ReviewResult{}, nil
	}), providers.ReviewRequest{}, findingReviewRetryPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || result.InputFindings != 0 || result.ProviderRequests != 0 {
		t.Fatalf("empty finding review made %d provider call(s): %#v", calls, result)
	}
}

func TestFindingReviewRetriesTypedTemporaryFailuresWithExactCumulativeAccounting(t *testing.T) {
	request := providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}, {RuleID: "two"}}}
	calls := 0
	reviewer := findingReviewFunc(func(_ context.Context, _ providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		result := providers.ReviewResult{
			Provider: providers.OpenAI, Model: "test", InputFindings: 2, ProviderRequests: 1,
			Usage: providers.Usage{PromptTokens: calls * 10, CompletionTokens: calls, ReasoningTokens: calls - 1, TotalDurationNS: int64(calls)},
		}
		switch calls {
		case 1:
			result.RateLimits = providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 0, ResetRequests: 3 * time.Millisecond}
			return result, &providers.RemoteTransientError{Provider: "OpenAI", StatusCode: 503, RetryAfter: 3 * time.Millisecond}
		case 2:
			result.RateLimits = providers.RateLimitSnapshot{TokensKnown: true, LimitTokens: 1000, RemainingTokens: 0, ResetTokens: 5 * time.Millisecond}
			return result, &providers.RemoteRateLimitError{Provider: "OpenAI", StatusCode: 429, RetryAfter: 5 * time.Millisecond}
		default:
			result.Reviewed = 2
			result.Observations = []providers.Observation{{RuleID: "one"}, {RuleID: "two"}}
			result.RateLimits = providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 497}
			return result, nil
		}
	})
	waits := []time.Duration{}
	result, err := reviewFindingsWithRetry(context.Background(), reviewer, request, findingReviewRetryPolicy{
		MaximumAttempts: 4, InitialWait: time.Millisecond, MaximumWait: 10 * time.Millisecond,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || result.ProviderRequests != 3 || result.Usage.PromptTokens != 60 || result.Usage.CompletionTokens != 6 || result.Usage.ReasoningTokens != 3 || result.Usage.TotalDurationNS != 6 {
		t.Fatalf("result=%#v calls=%d", result, calls)
	}
	if !reflect.DeepEqual(waits, []time.Duration{3 * time.Millisecond, 5 * time.Millisecond}) {
		t.Fatalf("waits=%v", waits)
	}
	if result.Reviewed != 2 || len(result.Observations) != 2 {
		t.Fatalf("final semantics=%#v", result)
	}
	if !result.RateLimits.RequestsKnown || result.RateLimits.RemainingRequests != 497 || !result.RateLimits.TokensKnown || result.RateLimits.RemainingTokens != 0 {
		t.Fatalf("latest per-dimension limits=%#v", result.RateLimits)
	}
}

func TestFindingReviewSplitsOutputExhaustedBatchAndMergesBoundResults(t *testing.T) {
	request := providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}, {RuleID: "two"}, {RuleID: "three"}, {RuleID: "four"}}}
	calls := 0
	result, err := reviewFindingsWithRetry(context.Background(), findingReviewFunc(func(_ context.Context, batch providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		value := providers.ReviewResult{
			Provider: providers.OpenAI, Model: "test", InputFindings: len(batch.Findings),
			Usage: providers.Usage{PromptTokens: 10, CompletionTokens: 4},
		}
		if len(batch.Findings) == 4 {
			return value, &providers.RemoteIncompleteError{Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens", InputTokens: 10, OutputTokens: 4}
		}
		for _, finding := range batch.Findings {
			value.Observations = append(value.Observations, providers.Observation{RuleID: finding.RuleID})
		}
		value.Reviewed = len(value.Observations)
		return value, nil
	}), request, findingReviewRetryPolicy{MaximumAttempts: 4, Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || result.ProviderRequests != 3 || result.InputFindings != 4 || result.Reviewed != 4 || len(result.Observations) != 4 {
		t.Fatalf("split result=%#v calls=%d", result, calls)
	}
	if result.Usage.PromptTokens != 30 || result.Usage.CompletionTokens != 12 || !strings.Contains(strings.Join(result.Notes, "\n"), "continued with two smaller") {
		t.Fatalf("split accounting/notes=%#v", result)
	}
}

func TestFindingReviewRegeneratesInvalidStructuredOutputWithExactAccounting(t *testing.T) {
	request := providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}}}
	calls := 0
	result, err := reviewFindingsWithRetry(context.Background(), findingReviewFunc(func(_ context.Context, _ providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		value := providers.ReviewResult{Provider: providers.OpenAI, Model: "test", InputFindings: 1, Usage: providers.Usage{PromptTokens: 10, CompletionTokens: 2}}
		if calls == 1 {
			return value, &providers.StructuredOutputValidationError{Diagnostic: "invalid bound observation"}
		}
		value.Reviewed = 1
		value.Observations = []providers.Observation{{RuleID: "one"}}
		return value, nil
	}), request, findingReviewRetryPolicy{
		MaximumAttempts: 4,
		Wait: func(context.Context, time.Duration) error {
			t.Fatal("structured-output regeneration should not enter a capacity wait")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.ProviderRequests != 2 || result.Reviewed != 1 || result.Usage.PromptTokens != 20 || result.Usage.CompletionTokens != 4 {
		t.Fatalf("structured-output repair calls=%d result=%#v", calls, result)
	}
}

func TestFindingReviewRejectsUnboundedProviderRetryWait(t *testing.T) {
	calls, waits := 0, 0
	result, err := reviewFindingsWithRetry(context.Background(), findingReviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		calls++
		return providers.ReviewResult{Provider: providers.OpenAI, Model: "test", InputFindings: 1, Usage: providers.Usage{PromptTokens: 7}}, &providers.RemoteRateLimitError{Provider: "OpenAI", StatusCode: 429, RetryAfter: 24 * time.Hour}
	}), providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}}}, findingReviewRetryPolicy{
		MaximumAttempts: 4,
		Wait:            func(context.Context, time.Duration) error { waits++; return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "beyond the 10m0s") || calls != 1 || waits != 0 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 7 {
		t.Fatalf("unbounded wait result=%#v err=%v calls=%d waits=%d", result, err, calls, waits)
	}
}

func TestFindingReviewDoesNotRetryPermanentSizeIncompleteOrUntypedFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "permanent quota", err: &providers.RemoteRateLimitError{Provider: "Gemini", StatusCode: 429, Permanent: true}},
		{name: "request too large", err: &providers.RemoteRateLimitError{Provider: "Anthropic", StatusCode: 413, RequestTooLarge: true}},
		{name: "incomplete output", err: &providers.RemoteIncompleteError{Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens"}},
		{name: "schema", err: errors.New("invalid structured response")},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			calls := 0
			result, err := reviewFindingsWithRetry(context.Background(), findingReviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
				calls++
				return providers.ReviewResult{
					Provider: providers.Gemini, Model: "test", InputFindings: 1,
					Usage: providers.Usage{PromptTokens: 11, CompletionTokens: 2},
				}, testCase.err
			}), providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}}}, findingReviewRetryPolicy{
				MaximumAttempts: 4,
				Wait: func(context.Context, time.Duration) error {
					t.Fatal("non-retryable error entered retry wait")
					return nil
				},
			})
			if err == nil || calls != 1 || result.ProviderRequests != 1 || result.Usage.PromptTokens != 11 || result.Usage.CompletionTokens != 2 {
				t.Fatalf("result=%#v err=%v calls=%d", result, err, calls)
			}
			if result.Reviewed != 0 || len(result.Observations) != 0 {
				t.Fatalf("failed result retained semantics: %#v", result)
			}
		})
	}
}

func TestFindingReviewCancellationDuringBackoffPreservesFailedAttemptAccounting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	result, err := reviewFindingsWithRetry(ctx, findingReviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		return providers.ReviewResult{
			Provider: providers.Anthropic, Model: "test", InputFindings: 1,
			Usage: providers.Usage{PromptTokens: 17, CompletionTokens: 3, ReasoningTokens: 2},
		}, &providers.RemoteTransientError{Provider: "Anthropic", StatusCode: 529}
	}), providers.ReviewRequest{Findings: []rules.Finding{{RuleID: "one"}}}, findingReviewRetryPolicy{
		MaximumAttempts: 4,
		Wait: func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		},
	})
	if !errors.Is(err, context.Canceled) || result.ProviderRequests != 1 || result.Usage.PromptTokens != 17 || result.Usage.CompletionTokens != 3 || result.Usage.ReasoningTokens != 2 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestFindingReviewCompleteRequiresEveryInputFinding(t *testing.T) {
	if findingReviewComplete(providers.ReviewResult{InputFindings: 2, Reviewed: 1}) {
		t.Fatal("partial finding review was marked complete")
	}
	if !findingReviewComplete(providers.ReviewResult{InputFindings: 2, Reviewed: 2}) {
		t.Fatal("fully bound finding review was marked incomplete")
	}
}

func TestRequireAIReviewTreatsProviderLimitedFindingReviewAsIncomplete(t *testing.T) {
	transport := doctorRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/chat" {
			t.Fatalf("request path=%q", request.URL.Path)
		}
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Messages) == 0 {
			t.Fatalf("invalid request: %v", err)
		}
		parts := strings.SplitN(body.Messages[len(body.Messages)-1].Content, "\n\n", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected prompt: %q", body.Messages[len(body.Messages)-1].Content)
		}
		var findings []struct {
			Fingerprint string `json:"fingerprint"`
			RuleID      string `json:"rule_id"`
		}
		if err := json.Unmarshal([]byte(parts[1]), &findings); err != nil || len(findings) != 1 {
			t.Fatalf("unexpected finding batch: %v %#v", err, findings)
		}
		content, err := json.Marshal(map[string]any{"observations": []map[string]any{{
			"fingerprint": findings[0].Fingerprint, "rule_id": findings[0].RuleID,
			"verdict": "uncertain", "confidence": "medium", "rationale": "Bounded synthetic test response.", "suggested_action": "Review the deterministic finding.",
		}}})
		if err != nil {
			t.Fatal(err)
		}
		responseBody, err := json.Marshal(map[string]any{
			"message": map[string]any{"content": string(content)}, "done": true,
			"prompt_eval_count": 12, "eval_count": 4,
		})
		if err != nil {
			t.Fatal(err)
		}
		return doctorHTTPResponse(http.StatusOK, string(responseBody)), nil
	})
	reviewer, err := providers.NewOllama(providers.OllamaOptions{
		Endpoint: "http://127.0.0.1:11434", Model: "test-model", Timeout: 5 * time.Second, MaxFindings: 1,
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	previousReviewer := configuredReviewer
	configuredReviewer = func(config.AIConfig) (*providers.OllamaProvider, time.Duration, int, string, providers.Kind, error) {
		return reviewer, 5 * time.Second, 1, "test-model", providers.Ollama, nil
	}
	t.Cleanup(func() { configuredReviewer = previousReviewer })

	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	cfg.AI.ReviewOnScan = true
	cfg.AI.RepositoryAnalysis.Mode = "bounded-only"
	cfg.AI.Ollama = config.OllamaConfig{Endpoint: "http://127.0.0.1:11434", Model: "test-model", TimeoutSeconds: 5, MaxFindings: 1}
	configPath := filepath.Join(target, config.FileName)
	if err := config.Write(configPath, cfg, false); err != nil {
		t.Fatal(err)
	}
	grantTestAutomaticReview(t, target, cfg)

	previousQualification := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		return modelQualificationOutcome{Result: modelqualification.Result{Status: "compatible", FromCache: true}}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previousQualification })

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--require-ai-review", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild)
	if code != 2 {
		t.Fatalf("code=%d, want 2; stderr=%s\nstdout=%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode report: %v\n%s", err, stdout.String())
	}
	if decoded.Review == nil || decoded.Review.InputFindings <= 1 || decoded.Review.Reviewed != 1 {
		t.Fatalf("partial finding review was not retained: %#v", decoded.Review)
	}
	if !strings.Contains(strings.Join(decoded.Warnings, "\n"), "reviewed 1 of") || !strings.Contains(strings.Join(decoded.Review.Notes, "\n"), "1 provider request attempt") {
		t.Fatalf("partial lifecycle/accounting missing: warnings=%#v review=%#v", decoded.Warnings, decoded.Review)
	}
}
