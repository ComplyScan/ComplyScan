package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOpenAIOptionalTuningIsSentOnlyToKnownSupportingFamilies(t *testing.T) {
	for _, testCase := range []struct {
		model         string
		wantReasoning bool
		wantVerbosity bool
	}{
		{model: "gpt-5.6-terra", wantReasoning: true, wantVerbosity: true},
		{model: "gpt-5-pro"},
		{model: "o3", wantReasoning: true},
		{model: "gpt-4o"},
		{model: "gpt-4.1"},
		{model: "vendor-unknown-model"},
	} {
		t.Run(testCase.model, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				_, hasReasoning := body["reasoning"]
				textConfig, _ := body["text"].(map[string]any)
				_, hasVerbosity := textConfig["verbosity"]
				if hasReasoning != testCase.wantReasoning || hasVerbosity != testCase.wantVerbosity {
					t.Fatalf("OpenAI optional tuning for %q: reasoning=%t verbosity=%t, body=%#v", testCase.model, hasReasoning, hasVerbosity, body)
				}
				if textConfig["format"] == nil {
					t.Fatalf("OpenAI structured-output format was omitted for %q: %#v", testCase.model, body)
				}
				return testJSONResponse(http.StatusOK, map[string]any{
					"status": "completed",
					"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": `{}`}}}},
					"usage":  map[string]any{"input_tokens": 10, "output_tokens": 5},
				}), nil
			})}
			_, err := openAICompletion(client, "test-key", testCase.model)(context.Background(), ollamaChatRequest{
				Messages:        []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
				Format:          map[string]any{"type": "object", "additionalProperties": false},
				ReasoningEffort: "medium",
				TextVerbosity:   "low",
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGeminiOutputBudgetStatusesAreRecoverableAndMetered(t *testing.T) {
	for _, status := range []string{"incomplete", "budget_exceeded"} {
		t.Run(status, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testJSONResponse(http.StatusOK, map[string]any{
					"status": status,
					"usage": map[string]any{
						"total_input_tokens": 321, "total_output_tokens": 17, "total_thought_tokens": 9,
					},
				}), nil
			})}
			response, err := geminiCompletion(client, "test-key", "gemini-test")(context.Background(), ollamaChatRequest{
				Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
				Format:   map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 4096,
			})
			incomplete, ok := AsRemoteIncompleteError(err)
			if !ok || incomplete.Status != status || incomplete.Reason != "max_output_tokens" || incomplete.InputTokens != 321 || incomplete.OutputTokens != 17 || incomplete.ReasoningTokens != 9 {
				t.Fatalf("Gemini %s result: response=%#v error=%#v, typed=%t", status, response, incomplete, ok)
			}
			if response.PromptEvalCount != 321 || response.EvalCount != 17 || response.ReasoningCount != 9 {
				t.Fatalf("Gemini %s response lost usage: %#v", status, response)
			}
		})
	}
}

func TestGeminiRetryInfoOverridesGenericBillingHintForRollingQuota(t *testing.T) {
	body := []byte(`{"error":{"status":"RESOURCE_EXHAUSTED","message":"Quota reached; check your plan and billing details.","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"2s"}]}}`)
	rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("Gemini", http.StatusTooManyRequests, body, http.Header{}))
	if !ok || rateLimit.Permanent || rateLimit.RetryAfter != 2*time.Second || rateLimit.Code != "RESOURCE_EXHAUSTED" {
		t.Fatalf("Gemini rolling quota = %#v, typed=%t", rateLimit, ok)
	}
}

func TestAnthropicIndependentTokenBucketsAreNotCollapsed(t *testing.T) {
	headers := make(http.Header)
	headers.Set("anthropic-ratelimit-requests-limit", "500")
	headers.Set("anthropic-ratelimit-requests-remaining", "499")
	headers.Set("anthropic-ratelimit-requests-reset", "1s")
	headers.Set("anthropic-ratelimit-input-tokens-limit", "2000000")
	headers.Set("anthropic-ratelimit-input-tokens-remaining", "1900000")
	headers.Set("anthropic-ratelimit-input-tokens-reset", "1s")
	headers.Set("anthropic-ratelimit-output-tokens-limit", "400000")
	headers.Set("anthropic-ratelimit-output-tokens-remaining", "390000")
	headers.Set("anthropic-ratelimit-output-tokens-reset", "1s")

	got := remoteRateLimitSnapshot(headers)
	if !got.RequestsKnown || got.RemainingRequests != 499 || got.TokensKnown {
		t.Fatalf("independent Anthropic limits collapsed into an invalid combined token bucket: %#v", got)
	}

	headers.Set("anthropic-ratelimit-tokens-limit", "400000")
	headers.Set("anthropic-ratelimit-tokens-remaining", "390000")
	headers.Set("anthropic-ratelimit-tokens-reset", "1s")
	got = remoteRateLimitSnapshot(headers)
	if !got.TokensKnown || got.LimitTokens != 400000 || got.RemainingTokens != 390000 {
		t.Fatalf("unified Anthropic token capacity was not retained: %#v", got)
	}
}

func TestExplicitNoRetryWinsOverGeminiAbortedFallback(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-should-retry", "false")
	err := remoteStatusError("Gemini", http.StatusConflict, []byte(`{"error":{"status":"ABORTED","message":"do not retry this conflict"}}`), headers)
	if transient, ok := AsRemoteTransientError(err); ok {
		t.Fatalf("explicit no-retry 409 became transient: %#v", transient)
	}
}

func TestOpenRouterNestedMetadataClassifiesOversizedRequest(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Input field is too long.","metadata":{"error_type":"string_too_long"}}}`)
	rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("OpenRouter", http.StatusBadRequest, body, http.Header{}))
	if !ok || !rateLimit.RequestTooLarge || rateLimit.Code != "string_too_long" {
		t.Fatalf("OpenRouter nested size error = %#v, typed=%t", rateLimit, ok)
	}
}

func TestOllamaHTTP200EnvelopeFailuresAreTypedTransient(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "malformed outer JSON", body: `{"done":`},
		{name: "unfinished non-streaming envelope", body: `{"done":false,"prompt_eval_count":31,"eval_count":7}`},
		{name: "empty structured content", body: `{"done":true,"message":{"content":""},"prompt_eval_count":31,"eval_count":7}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(testCase.body)),
				}, nil
			})}
			provider, err := NewOllama(OllamaOptions{
				Endpoint: "http://127.0.0.1:11434", Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client,
			})
			if err != nil {
				t.Fatal(err)
			}
			response, err := provider.chat(context.Background(), ollamaChatRequest{Model: "test"})
			if _, ok := AsRemoteTransientError(err); !ok {
				t.Fatalf("Ollama envelope error = %T %v, want typed transient", err, err)
			}
			if testCase.name != "malformed outer JSON" && (response.PromptEvalCount != 31 || response.EvalCount != 7) {
				t.Fatalf("Ollama envelope usage was lost: %#v", response)
			}
		})
	}
}

func TestOllamaChatSendsExplicitContextWindowForLargeReview(t *testing.T) {
	requestBody := ollamaChatRequest{
		Model: "test", Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: strings.Repeat("x", 15_000)}},
		Stream: false, Format: map[string]any{"type": "object"}, Options: map[string]any{"num_predict": 4096}, MaxOutputTokens: 4096,
	}
	wantContext := ollamaRequestContextTokens(requestBody)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		options, _ := body["options"].(map[string]any)
		if options["num_ctx"] != float64(wantContext) || wantContext <= 4096 {
			t.Fatalf("Ollama context window = %#v, want %d; request=%#v", options["num_ctx"], wantContext, body)
		}
		return testJSONResponse(http.StatusOK, map[string]any{"done": true, "done_reason": "stop", "message": map[string]string{"content": `{}`}}), nil
	})}
	provider, err := NewOllama(OllamaOptions{
		Endpoint: "http://127.0.0.1:11434", Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.chat(context.Background(), requestBody); err != nil {
		t.Fatal(err)
	}
}

func TestOllamaContextWindowIncludesStructuredOutputSchema(t *testing.T) {
	withoutSchema := ollamaRequestContextTokens(ollamaChatRequest{
		Messages: []ollamaMessage{{Role: "user", Content: "small prompt"}}, MaxOutputTokens: 4096,
	})
	withSchema := ollamaRequestContextTokens(ollamaChatRequest{
		Messages: []ollamaMessage{{Role: "user", Content: "small prompt"}}, MaxOutputTokens: 4096,
		Format: map[string]any{"type": "object", "description": strings.Repeat("schema", 3_000)},
	})
	if withSchema <= withoutSchema {
		t.Fatalf("schema-aware context = %d, without schema = %d", withSchema, withoutSchema)
	}
}

func TestDraftProfilePreservesUsageOnMeteredErrors(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		completion func(context.Context, ollamaChatRequest) (ollamaChatResponse, error)
	}{
		{
			name: "provider incomplete",
			completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
				return ollamaChatResponse{PromptEvalCount: 80, EvalCount: 20, ReasoningCount: 5}, &RemoteIncompleteError{
					Provider: "Ollama", Status: "incomplete", Reason: "max_output_tokens", InputTokens: 80, OutputTokens: 20, ReasoningTokens: 5,
				}
			},
		},
		{
			name: "invalid structured payload",
			completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
				var response ollamaChatResponse
				response.Message.Content = `{not-json`
				response.PromptEvalCount, response.EvalCount, response.ReasoningCount = 80, 20, 5
				return response, nil
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &OllamaProvider{kind: Ollama, label: "Ollama", model: "test", completion: testCase.completion}
			result, err := provider.DraftProfile(context.Background(), ProfileDraftRequest{
				Contexts: []ProfileSourceContext{{Path: "app.go", Kind: "source", Source: "package app"}},
			})
			if err == nil {
				t.Fatal("expected profile draft error")
			}
			if result.Provider != Ollama || result.Model != "test" || result.Usage.PromptTokens != 80 || result.Usage.CompletionTokens != 20 || result.Usage.ReasoningTokens != 5 {
				t.Fatalf("profile draft metered error result = %#v", result)
			}
		})
	}
}
