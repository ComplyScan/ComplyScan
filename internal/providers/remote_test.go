package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type truncatedRemoteResponseBody struct{}

func (truncatedRemoteResponseBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (truncatedRemoteResponseBody) Close() error             { return nil }

func TestRemoteProvidersUseFixedEndpointsEnvironmentCredentialAndStructuredOutput(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	structured, err := json.Marshal(ollamaReviewPayload{Observations: []ollamaObservation{{
		Fingerprint: fingerprint, RuleID: "AI-LOG-001", Verdict: VerdictConfirmed,
		Confidence: "high", Rationale: "The bounded evidence supports the finding.", SuggestedAction: "Review the logging path.",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		kind        Kind
		endpoint    string
		keyHeader   string
		keyValue    string
		constructor func(RemoteOptions) (*OllamaProvider, error)
		response    map[string]any
	}{
		{
			name: "OpenAI", kind: OpenAI, endpoint: openAIResponsesURL, keyHeader: "Authorization", keyValue: "Bearer test-key",
			constructor: NewOpenAI,
			response: map[string]any{
				"status": "completed",
				"output": []any{map[string]any{
					"type":    "message",
					"content": []any{map[string]any{"type": "output_text", "text": string(structured)}},
				}},
				"usage": map[string]any{"input_tokens": 11, "output_tokens": 7},
			},
		},
		{
			name: "Anthropic", kind: Anthropic, endpoint: anthropicMessagesURL, keyHeader: "x-api-key", keyValue: "test-key",
			constructor: NewAnthropic,
			response: map[string]any{
				"stop_reason": "end_turn",
				"content":     []any{map[string]any{"type": "text", "text": string(structured)}},
				"usage":       map[string]any{"input_tokens": 11, "output_tokens": 7},
			},
		},
		{
			name: "Gemini", kind: Gemini, endpoint: geminiInteractionsURL, keyHeader: "x-goog-api-key", keyValue: "test-key",
			constructor: NewGemini,
			response: map[string]any{
				"status": "completed",
				"steps": []any{map[string]any{
					"type":    "model_output",
					"content": []any{map[string]any{"type": "text", "text": string(structured)}},
				}},
				"usage": map[string]any{"total_input_tokens": 11, "total_output_tokens": 7},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != testCase.endpoint || request.Method != http.MethodPost {
					t.Errorf("request = %s %s", request.Method, request.URL)
				}
				if request.Header.Get(testCase.keyHeader) != testCase.keyValue {
					t.Errorf("credential header %s = %q", testCase.keyHeader, request.Header.Get(testCase.keyHeader))
				}
				if testCase.kind == Anthropic && request.Header.Get("anthropic-version") != "2023-06-01" {
					t.Errorf("anthropic-version = %q", request.Header.Get("anthropic-version"))
				}
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				encoded, _ := json.Marshal(body)
				if !strings.Contains(string(encoded), "json_schema") && !strings.Contains(string(encoded), "application/json") {
					t.Errorf("request does not contain a structured-output contract: %s", encoded)
				}
				if testCase.kind != Anthropic && body["store"] != false {
					t.Errorf("remote request did not disable storage: %s", encoded)
				}
				if testCase.kind == Gemini {
					generationConfig, _ := body["generation_config"].(map[string]any)
					if generationConfig["max_output_tokens"] != float64(maxRemoteOutputTokens) {
						t.Errorf("Gemini output limit was not sent: %s", encoded)
					}
					systemInstruction, systemOK := body["system_instruction"].(string)
					input, inputOK := body["input"].(string)
					if !systemOK || !inputOK || strings.TrimSpace(systemInstruction) == "" || strings.TrimSpace(input) == "" {
						t.Errorf("Gemini request did not separate trusted system instructions from user input: %s", encoded)
					}
					if strings.Contains(systemInstruction, fingerprint) || !strings.Contains(input, fingerprint) {
						t.Errorf("Gemini repository evidence crossed the system/user trust boundary: %s", encoded)
					}
				}
				if strings.Contains(string(encoded), "test-key") {
					t.Errorf("credential leaked into request body: %s", encoded)
				}
				response := testJSONResponse(http.StatusOK, testCase.response)
				if testCase.kind == Anthropic {
					response.Header.Set("anthropic-ratelimit-requests-limit", "50")
					response.Header.Set("anthropic-ratelimit-requests-remaining", "49")
					response.Header.Set("anthropic-ratelimit-requests-reset", time.Now().Add(30*time.Second).UTC().Format(time.RFC3339Nano))
					response.Header.Set("anthropic-ratelimit-tokens-limit", "40000")
					response.Header.Set("anthropic-ratelimit-tokens-remaining", "31000")
					response.Header.Set("anthropic-ratelimit-tokens-reset", time.Now().Add(90*time.Second).UTC().Format(time.RFC3339Nano))
				}
				return response, nil
			})}
			provider, err := testCase.constructor(RemoteOptions{APIKey: "test-key", Model: "test-model", Timeout: time.Second, MaxFindings: 10, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{
				Fingerprint: fingerprint, RuleID: "AI-LOG-001", Title: "Logging", Severity: rules.SeverityMedium,
				Category: "observability", Message: "Review logging.", Remediation: "Add review.", Confidence: "high",
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Provider != testCase.kind || result.Model != "test-model" || result.Reviewed != 1 || result.Usage.PromptTokens != 11 {
				t.Fatalf("result = %#v", result)
			}
			if testCase.kind == Anthropic {
				if result.RateLimits.LimitRequests != 50 || result.RateLimits.RemainingRequests != 49 || result.RateLimits.LimitTokens != 40_000 || result.RateLimits.RemainingTokens != 31_000 {
					t.Fatalf("Anthropic rate-limit snapshot = %#v", result.RateLimits)
				}
				if result.RateLimits.ResetRequests < 25*time.Second || result.RateLimits.ResetRequests > 30*time.Second || result.RateLimits.ResetTokens < 85*time.Second || result.RateLimits.ResetTokens > 90*time.Second {
					t.Fatalf("Anthropic reset durations = %#v", result.RateLimits)
				}
			}
		})
	}
}

func TestRemoteProviderErrorsDoNotExposeCredential(t *testing.T) {
	secret := "sk-proj-" + strings.Repeat("x", 24)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testJSONResponse(http.StatusUnauthorized, map[string]any{"error": map[string]string{"message": "invalid key " + secret}}), nil
	})}
	provider, err := NewOpenAI(RemoteOptions{APIKey: secret, Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{RuleID: "AI-LOG-001", Message: "test"}}})
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestRemoteProviderRetriesTruncatedResponseBodiesAsTransient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: truncatedRemoteResponseBody{}}, nil
	})}
	provider, err := NewOpenAI(RemoteOptions{APIKey: "test-key", Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{RuleID: "AI-LOG-001", Message: "test"}}})
	if _, ok := AsRemoteTransientError(err); !ok {
		t.Fatalf("truncated response error = %v, want typed transient", err)
	}
}

func TestOpenAIIncompleteResponsePreservesReasonAndUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testJSONResponse(http.StatusOK, map[string]any{
			"status":             "incomplete",
			"incomplete_details": map[string]string{"reason": "max_output_tokens"},
			"usage": map[string]any{
				"input_tokens": 3210, "output_tokens": 4096,
				"output_tokens_details": map[string]int{"reasoning_tokens": 3000},
			},
		})
		response.Header.Set("x-ratelimit-limit-tokens", "10000")
		response.Header.Set("x-ratelimit-limit-project-tokens", "12000")
		response.Header.Set("x-ratelimit-remaining-tokens", "6789")
		response.Header.Set("x-ratelimit-reset-tokens", "1m30s")
		return response, nil
	})}
	provider, err := NewOpenAI(RemoteOptions{APIKey: "test-key", Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{RuleID: "AI-LOG-001", Message: "test"}}})
	if err == nil || !strings.Contains(err.Error(), "reason: max_output_tokens") || !strings.Contains(err.Error(), "input tokens: 3210") || !strings.Contains(err.Error(), "output tokens: 4096") {
		t.Fatalf("incomplete response details were lost: %v", err)
	}
	incomplete, ok := AsRemoteIncompleteError(err)
	if !ok || incomplete.Reason != "max_output_tokens" || incomplete.InputTokens != 3210 || incomplete.OutputTokens != 4096 || incomplete.ReasoningTokens != 3000 || incomplete.TokenLimit != 0 {
		t.Fatalf("incomplete response was not structured: %#v, %t", incomplete, ok)
	}
	if incomplete.RateLimits.LimitTokens != 10_000 || incomplete.RateLimits.RemainingTokens != 6_789 || incomplete.RateLimits.ResetTokens != 90*time.Second {
		t.Fatalf("incomplete response rate limits = %#v", incomplete.RateLimits)
	}
}

func TestRemoteAdaptersPreserveOutputExhaustion(t *testing.T) {
	tests := []struct {
		name      string
		factory   remoteCompletionFactory
		response  map[string]any
		bodyLimit func(map[string]any) any
		reasoning int
	}{
		{
			name: "Anthropic", factory: anthropicCompletion,
			response: map[string]any{
				"stop_reason": "max_tokens", "content": []any{map[string]any{"type": "text", "text": "{"}},
				"usage": map[string]any{
					"input_tokens": 100, "output_tokens": 2048,
					"output_tokens_details": map[string]any{"thinking_tokens": 321},
				},
			},
			bodyLimit: func(body map[string]any) any { return body["max_tokens"] },
			reasoning: 321,
		},
		{
			name: "Gemini", factory: geminiCompletion,
			response: map[string]any{
				"status": "incomplete", "steps": []any{map[string]any{"type": "model_output", "content": []any{map[string]any{"type": "text", "text": "{"}}}},
				"usage": map[string]any{"total_input_tokens": 100, "total_output_tokens": 1800, "total_thought_tokens": 248},
			},
			bodyLimit: func(body map[string]any) any {
				config, _ := body["generation_config"].(map[string]any)
				return config["max_output_tokens"]
			},
		},
		{
			name: "OpenAI compatible", factory: openAICompatibleCompletion("https://models.example.com/v1", "Compatible"),
			response: map[string]any{
				"choices": []any{map[string]any{"finish_reason": "length", "message": map[string]any{"content": "{"}}},
				"usage": map[string]any{
					"prompt_tokens": 100, "completion_tokens": 2048,
					"completion_tokens_details": map[string]any{"reasoning_tokens": 654},
				},
			},
			bodyLimit: func(body map[string]any) any { return body["max_tokens"] },
			reasoning: 654,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if limit := testCase.bodyLimit(body); limit != float64(2048) {
					t.Fatalf("output limit = %#v, body = %#v", limit, body)
				}
				return testJSONResponse(http.StatusOK, testCase.response), nil
			})}
			completion := testCase.factory(client, "test-key", "test-model")
			_, err := completion(context.Background(), ollamaChatRequest{
				Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
				Format:   map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 2048,
			})
			incomplete, ok := AsRemoteIncompleteError(err)
			if !ok || incomplete.Reason != "max_output_tokens" || incomplete.InputTokens != 100 || incomplete.OutputTokens <= 0 {
				t.Fatalf("output exhaustion was not structured: %#v, %t, error = %v", incomplete, ok, err)
			}
			if testCase.name == "Gemini" && incomplete.ReasoningTokens != 248 {
				t.Fatalf("Gemini reasoning usage = %#v", incomplete)
			}
			if testCase.reasoning > 0 && incomplete.ReasoningTokens != testCase.reasoning {
				t.Fatalf("%s reasoning usage = %#v, want %d", testCase.name, incomplete, testCase.reasoning)
			}
		})
	}
}

func TestRemoteAdaptersPreserveDocumentedReasoningUsageOnSuccess(t *testing.T) {
	tests := []struct {
		name     string
		factory  remoteCompletionFactory
		response map[string]any
		want     int
	}{
		{
			name: "Anthropic thinking tokens", factory: anthropicCompletion, want: 123,
			response: map[string]any{
				"stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": `{}`}},
				"usage": map[string]any{
					"input_tokens": 10, "output_tokens": 20,
					"output_tokens_details": map[string]any{"thinking_tokens": 123},
				},
			},
		},
		{
			name: "OpenAI-compatible reasoning tokens", factory: openAICompatibleCompletion("https://models.example.com/v1", "Compatible"), want: 456,
			response: map[string]any{
				"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": `{}`}}},
				"usage": map[string]any{
					"prompt_tokens": 10, "completion_tokens": 20,
					"completion_tokens_details": map[string]any{"reasoning_tokens": 456},
				},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testJSONResponse(http.StatusOK, testCase.response), nil
			})}
			response, err := testCase.factory(client, "test-key", "test-model")(context.Background(), ollamaChatRequest{
				Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
				Format:   map[string]any{"type": "object", "additionalProperties": false},
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.ReasoningCount != testCase.want {
				t.Fatalf("reasoning tokens = %d, want %d", response.ReasoningCount, testCase.want)
			}
		})
	}
}

func TestRemoteAdaptersPreserveMeteredUsageOnPostResponseErrors(t *testing.T) {
	tests := []struct {
		name     string
		factory  remoteCompletionFactory
		response map[string]any
	}{
		{
			name: "OpenAI empty output", factory: func(client *http.Client, key, model string) func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
				return openAICompletion(client, key, model)
			},
			response: map[string]any{
				"status": "completed", "output": []any{},
				"usage": map[string]any{"input_tokens": 5000, "output_tokens": 1000, "output_tokens_details": map[string]any{"reasoning_tokens": 600}},
			},
		},
		{
			name: "Anthropic unexpected stop", factory: anthropicCompletion,
			response: map[string]any{"stop_reason": "tool_use", "content": []any{}, "usage": map[string]any{"input_tokens": 5000, "output_tokens": 1000}},
		},
		{
			name: "Gemini unexpected status", factory: geminiCompletion,
			response: map[string]any{"status": "failed", "steps": []any{}, "usage": map[string]any{"total_input_tokens": 5000, "total_output_tokens": 1000, "total_thought_tokens": 600}},
		},
		{
			name: "compatible refusal", factory: openAICompatibleCompletion("https://models.example.com/v1", "Compatible"),
			response: map[string]any{
				"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": "", "refusal": "declined"}}},
				"usage":   map[string]any{"prompt_tokens": 5000, "completion_tokens": 1000},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return testJSONResponse(http.StatusOK, testCase.response), nil
			})}
			response, err := testCase.factory(client, "test-key", "test-model")(context.Background(), ollamaChatRequest{
				Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
				Format:   map[string]any{"type": "object", "additionalProperties": false}, MaxOutputTokens: 4096,
			})
			if err == nil {
				t.Fatal("expected post-response error")
			}
			if response.PromptEvalCount != 5000 || response.EvalCount != 1000 {
				t.Fatalf("metered error response = %#v, want 5000 input / 1000 output", response)
			}
			if (testCase.name == "OpenAI empty output" || testCase.name == "Gemini unexpected status") && response.ReasoningCount != 600 {
				t.Fatalf("reasoning usage was lost: %#v", response)
			}
		})
	}
}

func TestOpenAITargetedRepositoryReviewUsesCompactReasoningAndSchema(t *testing.T) {
	for _, testCase := range []struct {
		name, effort string
		recovery     bool
	}{
		{name: "initial", effort: "medium"},
		{name: "recovery", effort: "medium", recovery: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				reasoning, _ := body["reasoning"].(map[string]any)
				textConfig, _ := body["text"].(map[string]any)
				if reasoning["effort"] != testCase.effort || textConfig["verbosity"] != "low" || body["max_output_tokens"] != float64(4096) {
					t.Fatalf("compact OpenAI settings = %#v", body)
				}
				encoded, _ := json.Marshal(textConfig["format"])
				if !strings.Contains(string(encoded), `"maxItems":12`) || !strings.Contains(string(encoded), `"maxLength":320`) {
					t.Fatalf("targeted schema was not bounded: %s", encoded)
				}
				if strings.Contains(string(encoded), `"oneOf"`) || !strings.Contains(string(encoded), `"anyOf"`) {
					t.Fatalf("targeted schema used an unsupported Structured Outputs union: %s", encoded)
				}
				result := `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
				response := testJSONResponse(http.StatusOK, map[string]any{
					"status": "completed",
					"output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": result}}}},
					"usage": map[string]any{
						"input_tokens": 120, "output_tokens": 80,
						"output_tokens_details": map[string]int{"reasoning_tokens": 30},
					},
				})
				response.Header.Set("x-ratelimit-limit-requests", "500")
				response.Header.Set("x-ratelimit-remaining-requests", "499")
				response.Header.Set("x-ratelimit-reset-requests", "120ms")
				response.Header.Set("x-ratelimit-limit-tokens", "500000")
				response.Header.Set("x-ratelimit-remaining-tokens", "490000")
				response.Header.Set("x-ratelimit-reset-tokens", "1m")
				return response, nil
			})}
			provider, err := NewOpenAI(RemoteOptions{APIKey: "test", Model: "gpt-5.6-terra", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1, OutputRecovery: testCase.recovery, MaxOutputTokens: 4096,
				Files: []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\n"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Usage.ReasoningTokens != 30 {
				t.Fatalf("reasoning tokens = %d", result.Usage.ReasoningTokens)
			}
			if result.RateLimits.LimitRequests != 500 || result.RateLimits.RemainingRequests != 499 || result.RateLimits.ResetRequests != 120*time.Millisecond || result.RateLimits.LimitTokens != 500_000 || result.RateLimits.RemainingTokens != 490_000 || result.RateLimits.ResetTokens != time.Minute {
				t.Fatalf("OpenAI rate-limit snapshot = %#v", result.RateLimits)
			}
		})
	}
}

func TestRemoteStatusErrorPreservesRateLimitDetails(t *testing.T) {
	body, err := json.Marshal(map[string]any{"error": map[string]string{
		"message": "Request too large for model on tokens per min (TPM): Limit 10000, Requested 21769.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Retry-After": []string{"2.5"}}
	headers.Set("x-ratelimit-limit-requests", "500")
	headers.Set("x-ratelimit-remaining-requests", "3")
	headers.Set("x-ratelimit-reset-requests", "45s")
	headers.Set("x-ratelimit-limit-tokens", "500000")
	headers.Set("x-ratelimit-remaining-tokens", "4000")
	headers.Set("x-ratelimit-reset-tokens", "1m")
	rateErr, ok := AsRemoteRateLimitError(remoteStatusError("OpenAI", http.StatusTooManyRequests, body, headers))
	if !ok {
		t.Fatal("HTTP 429 was not preserved as a structured rate-limit error")
	}
	if !rateErr.RequestTooLarge || rateErr.LimitTokens != 10_000 || rateErr.RequestedTokens != 21_769 || rateErr.RetryAfter != 2500*time.Millisecond {
		t.Fatalf("unexpected rate-limit details: %#v", rateErr)
	}
	if rateErr.RateLimits.LimitRequests != 500 || rateErr.RateLimits.RemainingRequests != 3 || rateErr.RateLimits.ResetRequests != 45*time.Second || rateErr.RateLimits.LimitTokens != 500_000 || rateErr.RateLimits.RemainingTokens != 4_000 || rateErr.RateLimits.ResetTokens != time.Minute {
		t.Fatalf("rate-limit response headers were not preserved: %#v", rateErr.RateLimits)
	}
}

func TestOpenAI429DistinguishesPermanentQuotaFromTemporaryThrottle(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		body      map[string]any
		permanent bool
		code      string
	}{
		{
			name: "insufficient quota is permanent",
			body: map[string]any{"error": map[string]any{
				"message": "You exceeded your current quota. Check your plan and billing details.",
				"type":    "insufficient_quota", "code": "insufficient_quota",
			}},
			permanent: true,
			code:      "insufficient_quota",
		},
		{
			name: "ordinary rate limit is temporary",
			body: map[string]any{"error": map[string]any{
				"message": "Rate limit reached for requests per minute. Please try again.",
				"type":    "rate_limit_error", "code": "rate_limit_exceeded",
			}},
			code: "rate_limit_exceeded",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body, err := json.Marshal(testCase.body)
			if err != nil {
				t.Fatal(err)
			}
			rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("OpenAI", http.StatusTooManyRequests, body, http.Header{"Retry-After": []string{"1"}}))
			if !ok {
				t.Fatal("OpenAI HTTP 429 was not returned as a structured rate-limit error")
			}
			if rateLimit.Permanent != testCase.permanent || rateLimit.Code != testCase.code {
				t.Fatalf("OpenAI 429 classification = %#v, want permanent=%t code=%q", rateLimit, testCase.permanent, testCase.code)
			}
		})
	}
}

func TestRemoteStatusErrorParsesRetryDelayFromMessage(t *testing.T) {
	body, err := json.Marshal(map[string]any{"error": map[string]string{
		"message": "Rate limit reached. Please try again in 750ms.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	rateErr, ok := AsRemoteRateLimitError(remoteStatusError("OpenAI", http.StatusTooManyRequests, body, http.Header{}))
	if !ok || rateErr.RequestTooLarge || rateErr.RetryAfter != 750*time.Millisecond {
		t.Fatalf("unexpected retry details: %#v, %t", rateErr, ok)
	}
}

func TestRemoteStatusErrorParsesGoogleRetryInfoAndRootMessage(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "Resource exhausted.",
			"details": []any{map[string]any{
				"@type": "type.googleapis.com/google.rpc.RetryInfo", "retryDelay": "1.25s",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rateErr, ok := AsRemoteRateLimitError(remoteStatusError("Gemini", http.StatusTooManyRequests, body, http.Header{}))
	if !ok || rateErr.RetryAfter != 1250*time.Millisecond {
		t.Fatalf("Gemini retry info = %#v, %t", rateErr, ok)
	}

	rootBody, err := json.Marshal(map[string]any{"message": "Organization rate limit exceeded."})
	if err != nil {
		t.Fatal(err)
	}
	rateErr, ok = AsRemoteRateLimitError(remoteStatusError("Mistral", http.StatusTooManyRequests, rootBody, http.Header{}))
	if !ok || rateErr.Message != "Organization rate limit exceeded." {
		t.Fatalf("root error message = %#v, %t", rateErr, ok)
	}
}

func TestGeminiQuotaExceededIsPermanent(t *testing.T) {
	body := []byte(`{"error":{"status":"QUOTA_EXCEEDED","message":"Project quota exceeded."}}`)
	rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("Gemini", http.StatusTooManyRequests, body, http.Header{}))
	if !ok || !rateLimit.Permanent || rateLimit.Code != "QUOTA_EXCEEDED" {
		t.Fatalf("Gemini quota classification = %#v, %t, want permanent quota error", rateLimit, ok)
	}
}

func TestRemoteStatusErrorClassifiesOnlyRetryableTransientStatuses(t *testing.T) {
	body, err := json.Marshal(map[string]any{"error": map[string]any{"message": "Temporarily unavailable."}})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name   string
		status int
	}{
		{name: "408", status: http.StatusRequestTimeout},
		{name: "500", status: http.StatusInternalServerError},
		{name: "502", status: http.StatusBadGateway},
		{name: "503", status: http.StatusServiceUnavailable},
		{name: "504", status: http.StatusGatewayTimeout},
		{name: "529", status: 529},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("Retry-After", "1.5")
			headers.Set("x-ratelimit-limit-requests", "20")
			headers.Set("x-ratelimit-remaining-requests", "7")
			transient, ok := AsRemoteTransientError(remoteStatusError("provider", testCase.status, body, headers))
			if !ok || transient.StatusCode != testCase.status || transient.RetryAfter != 1500*time.Millisecond || transient.RateLimits.RemainingRequests != 7 {
				t.Fatalf("transient error = %#v, %t", transient, ok)
			}
		})
	}

	headers := http.Header{}
	headers.Set("x-should-retry", "false")
	if transient, ok := AsRemoteTransientError(remoteStatusError("provider", http.StatusInternalServerError, body, headers)); ok {
		t.Fatalf("x-should-retry=false was ignored: %#v", transient)
	}
	if transient, ok := AsRemoteTransientError(remoteStatusError("provider", http.StatusNotImplemented, body, http.Header{})); ok {
		t.Fatalf("HTTP 501 was treated as retryable: %#v", transient)
	}
}

func TestRemoteStatusErrorRecognizesProviderSizeErrorsOutsideHTTP429(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   []byte
	}{
		{name: "OpenAI context code", status: http.StatusBadRequest, body: []byte(`{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`)},
		{name: "Anthropic prompt too long", status: http.StatusBadRequest, body: []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 201000 tokens > 200000 maximum"}}`)},
		{name: "payload too large", status: http.StatusRequestEntityTooLarge, body: []byte(`{"error":{"message":"payload too large"}}`)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("provider", testCase.status, testCase.body, http.Header{}))
			if !ok || !rateLimit.RequestTooLarge {
				t.Fatalf("size error = %#v, %t, want typed request-too-large", rateLimit, ok)
			}
		})
	}
}

func TestRemoteStatusErrorHonorsExplicitRetryDirectiveAcrossStatuses(t *testing.T) {
	noRetryHeaders := http.Header{}
	noRetryHeaders.Set("x-should-retry", "false")
	rateLimit, ok := AsRemoteRateLimitError(remoteStatusError("provider", http.StatusTooManyRequests, []byte(`{"error":{"message":"slow down"}}`), noRetryHeaders))
	if !ok || !rateLimit.Permanent {
		t.Fatalf("explicit no-retry 429 = %#v, %t, want non-retried typed limit", rateLimit, ok)
	}
	retryHeaders := http.Header{}
	retryHeaders.Set("x-should-retry", "true")
	transient, ok := AsRemoteTransientError(remoteStatusError("Gemini", http.StatusConflict, []byte(`{"error":{"status":"ABORTED","message":"retry this conflict"}}`), retryHeaders))
	if !ok || transient.StatusCode != http.StatusConflict {
		t.Fatalf("explicit retry 409 = %#v, %t, want typed transient", transient, ok)
	}
}

func TestAnthropicContextWindowStopIsAdaptiveAndMetered(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := testJSONResponse(http.StatusOK, map[string]any{
			"stop_reason": "model_context_window_exceeded",
			"content":     []any{map[string]any{"type": "text", "text": "partial"}},
			"usage": map[string]any{
				"input_tokens": 190_000, "output_tokens": 10_000,
				"output_tokens_details": map[string]any{"thinking_tokens": 7_000},
			},
		})
		response.Header.Set("anthropic-ratelimit-requests-limit", "50")
		response.Header.Set("anthropic-ratelimit-requests-remaining", "49")
		return response, nil
	})}
	response, err := anthropicCompletion(client, "test-key", "test-model")(context.Background(), ollamaChatRequest{
		Messages: []ollamaMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "user"}},
		Format:   map[string]any{"type": "object", "additionalProperties": false},
	})
	rateLimit, ok := AsRemoteRateLimitError(err)
	if !ok || !rateLimit.RequestTooLarge || rateLimit.Code != "model_context_window_exceeded" || rateLimit.StatusCode != http.StatusOK {
		t.Fatalf("Anthropic context-window result = %#v, %t, error = %v", rateLimit, ok, err)
	}
	if response.PromptEvalCount != 190_000 || response.EvalCount != 10_000 || response.ReasoningCount != 7_000 {
		t.Fatalf("Anthropic context-window usage = %#v", response)
	}
	if !response.RateLimits.RequestsKnown || response.RateLimits.RemainingRequests != 49 || rateLimit.RateLimits.RemainingRequests != 49 {
		t.Fatalf("Anthropic context-window capacity response=%#v error=%#v", response.RateLimits, rateLimit.RateLimits)
	}
}

func TestRemoteRateLimitSnapshotSupportsProviderHeaderFamilies(t *testing.T) {
	t.Run("Anthropic RFC3339 resets", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("anthropic-ratelimit-requests-limit", "50")
		headers.Set("anthropic-ratelimit-requests-remaining", "0")
		headers.Set("anthropic-ratelimit-requests-reset", time.Now().Add(30*time.Second).UTC().Format(time.RFC3339Nano))
		headers.Set("anthropic-ratelimit-tokens-limit", "40000")
		headers.Set("anthropic-ratelimit-tokens-remaining", "31000")
		headers.Set("anthropic-ratelimit-tokens-reset", time.Now().Add(90*time.Second).UTC().Format(time.RFC3339Nano))
		snapshot := remoteRateLimitSnapshot(headers)
		if !snapshot.RequestsKnown || snapshot.LimitRequests != 50 || snapshot.RemainingRequests != 0 || !snapshot.TokensKnown || snapshot.LimitTokens != 40_000 || snapshot.RemainingTokens != 31_000 {
			t.Fatalf("Anthropic snapshot = %#v", snapshot)
		}
		if snapshot.ResetRequests < 25*time.Second || snapshot.ResetRequests > 30*time.Second || snapshot.ResetTokens < 85*time.Second || snapshot.ResetTokens > 90*time.Second {
			t.Fatalf("Anthropic reset durations = %#v", snapshot)
		}
	})

	t.Run("OpenAI project constraint wins", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("x-ratelimit-limit-tokens", "500000")
		headers.Set("x-ratelimit-remaining-tokens", "490000")
		headers.Set("x-ratelimit-reset-tokens", "30s")
		headers.Set("x-ratelimit-limit-project-tokens", "100000")
		headers.Set("x-ratelimit-remaining-project-tokens", "90000")
		headers.Set("x-ratelimit-reset-project-tokens", "1m")
		snapshot := remoteRateLimitSnapshot(headers)
		if !snapshot.TokensKnown || snapshot.LimitTokens != 100_000 || snapshot.RemainingTokens != 90_000 || snapshot.ResetTokens != time.Minute {
			t.Fatalf("OpenAI project snapshot = %#v", snapshot)
		}
	})

	t.Run("generic request quota", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-RateLimit-Limit", "30")
		headers.Set("X-RateLimit-Remaining", "25")
		headers.Set("X-RateLimit-Reset", "15")
		snapshot := remoteRateLimitSnapshot(headers)
		if !snapshot.RequestsKnown || snapshot.LimitRequests != 30 || snapshot.RemainingRequests != 25 || snapshot.ResetRequests != 15*time.Second {
			t.Fatalf("generic snapshot = %#v", snapshot)
		}
	})

	t.Run("ambiguous lone remaining value is ignored", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-RateLimit-Remaining", "25")
		if snapshot := remoteRateLimitSnapshot(headers); snapshot.Available() {
			t.Fatalf("ambiguous snapshot should be unavailable: %#v", snapshot)
		}
	})

	t.Run("past Unix reset is already replenished", func(t *testing.T) {
		headers := http.Header{}
		headers.Set("X-RateLimit-Limit", "30")
		headers.Set("X-RateLimit-Remaining", "0")
		headers.Set("X-RateLimit-Reset", "1700000000")
		if snapshot := remoteRateLimitSnapshot(headers); snapshot.ResetRequests != 0 {
			t.Fatalf("past reset became a delay: %#v", snapshot)
		}
	})
}

func TestRemoteOutputTokenLimitHonorsAdaptiveReduction(t *testing.T) {
	if got := remoteOutputTokenLimit(ollamaChatRequest{MaxOutputTokens: 2_000}, 16_384); got != 2_000 {
		t.Fatalf("adaptive output limit = %d, want 2000", got)
	}
	if got := remoteOutputTokenLimit(ollamaChatRequest{MaxOutputTokens: OpenAIMaxOutputTokens}, OpenAIMaxOutputTokens); got != OpenAIMaxOutputTokens {
		t.Fatalf("OpenAI output limit = %d, want %d", got, OpenAIMaxOutputTokens)
	}
	if got := remoteOutputTokenLimit(ollamaChatRequest{}, 16_384); got != maxRemoteOutputTokens {
		t.Fatalf("default output limit = %d, want %d", got, maxRemoteOutputTokens)
	}
	if got := remoteOutputTokenLimit(ollamaChatRequest{MaxOutputTokens: 32_000}, 16_384); got != 16_384 {
		t.Fatalf("oversized adaptive limit = %d, want provider ceiling 16384", got)
	}
	if got := remoteOutputTokenLimit(ollamaChatRequest{}, 2_000); got != 2_000 {
		t.Fatalf("default limit above provider ceiling = %d, want 2000", got)
	}
}

func TestRemoteProviderRequiresCredential(t *testing.T) {
	if _, err := NewGemini(RemoteOptions{Model: "test", Timeout: time.Second, MaxFindings: 1}); err == nil || !strings.Contains(err.Error(), "API key is not available") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAICompatibleProviderUsesConfiguredEndpointAndStructuredChat(t *testing.T) {
	fingerprint := strings.Repeat("b", 64)
	structured, err := json.Marshal(ollamaReviewPayload{Observations: []ollamaObservation{{
		Fingerprint: fingerprint, RuleID: "AI-LOG-001", Verdict: VerdictConfirmed,
		Confidence: "high", Rationale: "The bounded evidence supports the finding.",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://models.example.com/v1/chat/completions" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		responseFormat, ok := body["response_format"].(map[string]any)
		if !ok || responseFormat["type"] != "json_schema" || body["model"] != "custom-review-model" {
			t.Fatalf("request body = %#v", body)
		}
		response := testJSONResponse(http.StatusOK, map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(structured)},
			}},
			"usage": map[string]any{"prompt_tokens": 13, "completion_tokens": 8},
		})
		response.Header.Set("x-ratelimit-limit-requests", "20")
		response.Header.Set("x-ratelimit-remaining-requests", "17")
		response.Header.Set("x-ratelimit-reset-requests", "2s")
		return response, nil
	})}
	provider, err := NewOpenAICompatible(Compatible, "Acme gateway", RemoteOptions{
		APIKey: "test-key", BaseURL: "https://models.example.com/v1/", Model: "custom-review-model",
		Timeout: time.Second, MaxFindings: 10, HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{
		Fingerprint: fingerprint, RuleID: "AI-LOG-001", Title: "Logging", Severity: rules.SeverityMedium,
		Category: "observability", Message: "Review logging.", Remediation: "Add review.", Confidence: "high",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != Compatible || result.Model != "custom-review-model" || result.Reviewed != 1 || result.Usage.PromptTokens != 13 {
		t.Fatalf("result = %#v", result)
	}
	if !result.RateLimits.RequestsKnown || result.RateLimits.LimitRequests != 20 || result.RateLimits.RemainingRequests != 17 || result.RateLimits.ResetRequests != 2*time.Second {
		t.Fatalf("compatible provider rate-limit snapshot = %#v", result.RateLimits)
	}
}

func TestOpenAICompatibleProviderRejectsUnsafeEndpoint(t *testing.T) {
	for _, baseURL := range []string{"", "http://models.example.com/v1", "https://user:secret@models.example.com/v1", "https://models.example.com/v1?key=secret"} {
		_, err := NewOpenAICompatible(Compatible, "Acme", RemoteOptions{
			APIKey: "test-key", BaseURL: baseURL, Model: "test", Timeout: time.Second, MaxFindings: 1,
		})
		if err == nil {
			t.Fatalf("expected base URL %q to fail", baseURL)
		}
	}
}
