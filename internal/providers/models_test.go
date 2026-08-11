package providers

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListModelsUsesProviderAuthenticationAndResponseShape(t *testing.T) {
	tests := []struct {
		name        string
		options     ModelListOptions
		endpoint    string
		header      string
		headerValue string
		body        string
		want        []RemoteModel
	}{
		{
			name: "OpenAI-compatible", options: ModelListOptions{Provider: Groq, Label: "Groq", BaseURL: "https://api.groq.com/openai/v1"},
			endpoint: "https://api.groq.com/openai/v1/models", header: "Authorization", headerValue: "Bearer test-key",
			body: `{"data":[{"id":"openai/gpt-oss-120b","owned_by":"OpenAI"}]}`,
			want: []RemoteModel{{ID: "openai/gpt-oss-120b"}},
		},
		{
			name: "Anthropic", options: ModelListOptions{Provider: Anthropic, Label: "Anthropic"},
			endpoint: "https://api.anthropic.com/v1/models?limit=1000", header: "x-api-key", headerValue: "test-key",
			body: `{"data":[{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5"}]}`,
			want: []RemoteModel{{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5"}},
		},
		{
			name: "Gemini", options: ModelListOptions{Provider: Gemini, Label: "Gemini"},
			endpoint: "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1000", header: "x-goog-api-key", headerValue: "test-key",
			body: `{"models":[{"name":"models/gemini-3.6-flash-001","baseModelId":"gemini-3.6-flash","displayName":"Gemini 3.6 Flash","supportedGenerationMethods":["generateContent"]},{"name":"models/gemini-embedding","supportedGenerationMethods":["embedContent"]}]}`,
			want: []RemoteModel{{ID: "gemini-3.6-flash", DisplayName: "Gemini 3.6 Flash"}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != testCase.endpoint || request.Header.Get(testCase.header) != testCase.headerValue {
					t.Fatalf("request=%s %s header=%q", request.Method, request.URL, request.Header.Get(testCase.header))
				}
				if testCase.options.Provider == Anthropic && request.Header.Get("anthropic-version") != "2023-06-01" {
					t.Fatalf("anthropic-version = %q", request.Header.Get("anthropic-version"))
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: ioNopCloser(strings.NewReader(testCase.body))}, nil
			})}
			options := testCase.options
			options.APIKey = "test-key"
			options.Timeout = time.Second
			options.HTTPClient = client
			models, err := ListModels(context.Background(), options)
			if err != nil {
				t.Fatal(err)
			}
			if len(models) != len(testCase.want) {
				t.Fatalf("models = %#v", models)
			}
			for index := range models {
				if models[index] != testCase.want[index] {
					t.Fatalf("models = %#v", models)
				}
			}
		})
	}
}

func TestListModelsRejectsMissingCredentialAndUnsafeCompatibleURL(t *testing.T) {
	if _, err := ListModels(context.Background(), ModelListOptions{Provider: OpenAI}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("missing-key error = %v", err)
	}
	_, err := ListModels(context.Background(), ModelListOptions{Provider: Compatible, Label: "Gateway", APIKey: "test", BaseURL: "http://models.example.com/v1"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("unsafe URL error = %v", err)
	}
}

func TestDecodeRemoteModelsFiltersExplicitlyIncompatibleModels(t *testing.T) {
	models, err := decodeRemoteModels(OpenRouter, []byte(`{"data":[{"id":"text-model","architecture":{"output_modalities":["text"]}},{"id":"image-model","architecture":{"output_modalities":["image"]}},{"id":"fim-only","capabilities":{"completion_chat":false}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "text-model" {
		t.Fatalf("models = %#v", models)
	}
	models, err = decodeRemoteModels(OpenAI, []byte(`{"data":[{"id":"gpt-5.6-terra"},{"id":"text-embedding-3-large"},{"id":"gpt-image-2"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5.6-terra" {
		t.Fatalf("OpenAI models = %#v", models)
	}
}

type nopReadCloser struct{ *strings.Reader }

func (nopReadCloser) Close() error { return nil }

func ioNopCloser(reader *strings.Reader) nopReadCloser { return nopReadCloser{Reader: reader} }
