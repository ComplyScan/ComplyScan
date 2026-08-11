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
