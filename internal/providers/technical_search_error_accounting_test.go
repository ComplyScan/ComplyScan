package providers

import (
	"context"
	"errors"
	"testing"
)

func TestTechnicalSearchPlanPreservesIncompleteUsage(t *testing.T) {
	provider := &OllamaProvider{
		kind: Gemini, label: "Gemini", model: "test", maxFindings: 1,
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if request.ReasoningEffort != reasoningEffortLow {
				t.Fatalf("technical-search reasoning effort = %q, want low", request.ReasoningEffort)
			}
			return ollamaChatResponse{PromptEvalCount: 31, EvalCount: 12, ReasoningCount: 4, TotalDuration: 9}, &RemoteIncompleteError{
				Provider: "Gemini", Reason: "max_output_tokens", InputTokens: 31, OutputTokens: 12, ReasoningTokens: 4,
			}
		},
	}
	_, usage, err := provider.PlanTechnicalSearch(context.Background(), TechnicalCandidate{ObjectiveID: "objective"})
	var incomplete *RemoteIncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("error = %v, want typed incomplete error", err)
	}
	if usage.PromptTokens != 31 || usage.CompletionTokens != 12 || usage.ReasoningTokens != 4 || usage.TotalDurationNS != 9 {
		t.Fatalf("usage = %#v, want metered incomplete attempt", usage)
	}
}

func TestTechnicalReviewRequestsMediumReasoning(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test", maxFindings: 1,
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if request.ReasoningEffort != reasoningEffortMedium {
				t.Fatalf("technical-review reasoning effort = %q, want medium", request.ReasoningEffort)
			}
			return ollamaChatResponse{}, errors.New("stop after inspecting request")
		},
	}
	_, _, _, err := provider.reviewTechnicalCandidate(context.Background(), TechnicalCandidate{}, 0)
	if err == nil {
		t.Fatal("expected inspection completion to stop")
	}
}

func TestTechnicalSearchPlanPreservesUsageWhenStructuredResponseCannotBeDecoded(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test", maxFindings: 1,
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			response := ollamaChatResponse{PromptEvalCount: 21, EvalCount: 7, ReasoningCount: 2, TotalDuration: 5}
			response.Message.Content = "{"
			return response, nil
		},
	}
	_, usage, err := provider.PlanTechnicalSearch(context.Background(), TechnicalCandidate{ObjectiveID: "objective"})
	if err == nil {
		t.Fatal("expected malformed plan to fail")
	}
	if usage.PromptTokens != 21 || usage.CompletionTokens != 7 || usage.ReasoningTokens != 2 || usage.TotalDurationNS != 5 {
		t.Fatalf("usage = %#v, want metered malformed response", usage)
	}
}
