package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestFindingReviewPreservesUsageWhenStructuredResponseCannotBeDecoded(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test", maxFindings: 1,
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if request.ReasoningEffort != reasoningEffortLow {
				t.Fatalf("finding-review reasoning effort = %q, want low", request.ReasoningEffort)
			}
			response := ollamaChatResponse{Done: true, PromptEvalCount: 123, EvalCount: 45, ReasoningCount: 6, TotalDuration: 7}
			response.Message.Content = "{"
			response.RateLimits = RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499}
			return response, nil
		},
	}
	result, err := provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{
		RuleID: "AI-DOC-001", Title: "test", Message: "test", Remediation: "test",
	}}})
	if err == nil {
		t.Fatal("expected malformed structured response to fail")
	}
	if result.Provider != OpenAI || result.Model != "test" || result.Usage.PromptTokens != 123 || result.Usage.CompletionTokens != 45 || result.Usage.ReasoningTokens != 6 || result.RateLimits.RemainingRequests != 499 {
		t.Fatalf("partial finding result = %#v, want identity, usage, and capacity preserved", result)
	}
	if len(result.Observations) != 0 || result.Reviewed != 0 {
		t.Fatalf("invalid response leaked semantics: %#v", result)
	}
}

func TestFindingReviewPreservesIncompleteUsageWithoutDoubleCounting(t *testing.T) {
	provider := &OllamaProvider{
		kind: Anthropic, label: "Anthropic", model: "test", maxFindings: 1,
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			return ollamaChatResponse{PromptEvalCount: 50, EvalCount: 20}, &RemoteIncompleteError{
				Provider: "Anthropic", Reason: "max_output_tokens", InputTokens: 50, OutputTokens: 20,
			}
		},
	}
	result, err := provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{RuleID: "AI-DOC-001"}}})
	var incomplete *RemoteIncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error = %v, want typed incomplete error", err)
	}
	if result.Usage.PromptTokens != 50 || result.Usage.CompletionTokens != 20 {
		t.Fatalf("usage = %#v, want one incomplete attempt without double counting", result.Usage)
	}
}
