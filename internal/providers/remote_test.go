package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/rules"
)

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
				if strings.Contains(string(encoded), "test-key") {
					t.Errorf("credential leaked into request body: %s", encoded)
				}
				return testJSONResponse(http.StatusOK, testCase.response), nil
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
	if !ok || incomplete.Reason != "max_output_tokens" || incomplete.InputTokens != 3210 || incomplete.OutputTokens != 4096 || incomplete.ReasoningTokens != 3000 || incomplete.TokenLimit != 10000 {
		t.Fatalf("incomplete response was not structured: %#v, %t", incomplete, ok)
	}
	if incomplete.RateLimits.LimitTokens != 10_000 || incomplete.RateLimits.RemainingTokens != 6_789 || incomplete.RateLimits.ResetTokens != 90*time.Second {
		t.Fatalf("incomplete response rate limits = %#v", incomplete.RateLimits)
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
		return testJSONResponse(http.StatusOK, map[string]any{
			"choices": []any{map[string]any{
				"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(structured)},
			}},
			"usage": map[string]any{"prompt_tokens": 13, "completion_tokens": 8},
		}), nil
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
