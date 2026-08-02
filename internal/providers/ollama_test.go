package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestOllamaReviewUsesStructuredLocalRequestAndRedactsEvidence(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	secret := "sk-proj-" + strings.Repeat("x", 24)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/chat" || request.Method != http.MethodPost {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(body)
		if body["stream"] != false || body["format"] == nil {
			t.Errorf("request did not use non-streaming structured output: %s", encoded)
		}
		if strings.Contains(string(encoded), secret) || !strings.Contains(string(encoded), "sk-proj-****xxxx") {
			t.Errorf("request evidence was not redacted: %s", encoded)
		}
		content, _ := json.Marshal(ollamaReviewPayload{Observations: []ollamaObservation{{
			Fingerprint: fingerprint, RuleID: "AI-SEC-001", Verdict: VerdictConfirmed,
			Confidence: "high", Rationale: "The supplied evidence has a credential-shaped assignment.",
			SuggestedAction: "Rotate the credential if it is real.",
		}}})
		return testJSONResponse(http.StatusOK, map[string]any{
			"message": map[string]string{"content": string(content)}, "done": true,
			"prompt_eval_count": 100, "eval_count": 40, "total_duration": 123,
		}), nil
	})}

	provider, err := NewOllama(OllamaOptions{
		Endpoint: "http://127.0.0.1:11434", Model: "test-model", Timeout: time.Second, MaxFindings: 10, HTTPClient: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{
		Fingerprint: fingerprint, RuleID: "AI-SEC-001", Title: "Potential credential",
		Severity: rules.SeverityHigh, Category: "secrets-management",
		Message: "A credential may be hardcoded.", Evidence: `token = "` + secret + `"`,
		Remediation: "Rotate it.", Confidence: "high",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != Ollama || result.Model != "test-model" || result.Reviewed != 1 || result.Usage.PromptTokens != 100 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Observations[0].Verdict != VerdictConfirmed || result.Observations[0].Fingerprint != fingerprint {
		t.Fatalf("unexpected observation: %#v", result.Observations)
	}
}

func TestOllamaReviewRejectsUnboundObservation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		content, _ := json.Marshal(ollamaReviewPayload{Observations: []ollamaObservation{{
			Fingerprint: strings.Repeat("b", 64), RuleID: "AI-LOG-001", Verdict: VerdictConfirmed,
			Confidence: "high", Rationale: "Invented", SuggestedAction: "None",
		}}})
		return testJSONResponse(http.StatusOK, map[string]any{"message": map[string]string{"content": string(content)}, "done": true}), nil
	})}
	provider, err := NewOllama(OllamaOptions{Endpoint: "http://127.0.0.1:11434", Model: "test", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{
		Fingerprint: strings.Repeat("a", 64), RuleID: "AI-LOG-001",
	}}})
	if err == nil || !strings.Contains(err.Error(), "unknown fingerprint") {
		t.Fatalf("got error %v", err)
	}
}

func TestOllamaReviewReportsAPIError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testJSONResponse(http.StatusNotFound, map[string]string{"error": "model not found"}), nil
	})}
	provider, err := NewOllama(OllamaOptions{Endpoint: "http://127.0.0.1:11434/api", Model: "missing", Timeout: time.Second, MaxFindings: 1, HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Review(context.Background(), ReviewRequest{Findings: []rules.Finding{{RuleID: "AI-DOC-001"}}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404: model not found") {
		t.Fatalf("got error %v", err)
	}
}

func TestNewOllamaRejectsRemoteEndpoint(t *testing.T) {
	_, err := NewOllama(OllamaOptions{
		Endpoint: "https://example.com", Model: "test", Timeout: time.Second, MaxFindings: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("got error %v", err)
	}
}

func TestOllamaReviewSkipsHTTPWhenThereAreNoFindings(t *testing.T) {
	provider, err := NewOllama(OllamaOptions{
		Endpoint: "http://127.0.0.1:1", Model: "test", Timeout: time.Millisecond, MaxFindings: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Review(context.Background(), ReviewRequest{})
	if err != nil || result.Reviewed != 0 || len(result.Notes) < 2 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testJSONResponse(status int, value any) *http.Response {
	data, _ := json.Marshal(value)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(data))),
	}
}
