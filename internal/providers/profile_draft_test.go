package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDraftProfileUsesBoundedStructuredEvidence(t *testing.T) {
	provider := &OllamaProvider{
		kind: Ollama, label: "Ollama", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			encoded, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			value := string(encoded)
			if strings.Contains(value, "sk-proj-abcdefghijklmnopqrstuvwxyz") {
				t.Fatalf("profile context was not redacted: %s", value)
			}
			if !strings.Contains(value, "untrusted data") || request.Think || request.Format["type"] != "object" {
				t.Fatalf("unsafe profile draft request: %#v", request)
			}
			var response ollamaChatResponse
			response.Message.Content = `{"suggestions":[{"field":"ai-activities","values":["inference","agent-tool-use"],"confidence":"high","rationale":"A model client invokes tools.","evidence":[{"path":"agent.go","line":12,"summary":"The agent calls a model and dispatches tools."}]}]}`
			response.PromptEvalCount = 120
			response.EvalCount = 30
			return response, nil
		},
	}
	result, err := provider.DraftProfile(context.Background(), ProfileDraftRequest{
		RepositoryName: "example",
		Languages:      []string{"Go"},
		Components:     []string{"OpenAI"},
		Contexts: []ProfileSourceContext{{
			Path: "agent.go", Kind: "source",
			Source: `const key = "sk-proj-abcdefghijklmnopqrstuvwxyz"; func run() { callModel(); dispatchTool() }`,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != Ollama || result.Model != "test-model" || len(result.Suggestions) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if strings.Join(result.Suggestions[0].Values, ",") != "inference,agent-tool-use" || result.Usage.PromptTokens != 120 {
		t.Fatalf("suggestion = %#v", result.Suggestions[0])
	}
}

func TestDraftProfileRejectsUnsupportedClaimsAndPaths(t *testing.T) {
	request := ProfileDraftRequest{Contexts: []ProfileSourceContext{{Path: "README.md", Source: "Example"}}}
	_, err := validateProfileSuggestions([]ProfileSuggestion{{
		Field: "operating-regions", Values: []string{"eu"}, Confidence: "high", Rationale: "Claimed",
		Evidence: []ProfileEvidence{{Path: "README.md", Summary: "Claimed"}},
	}}, request)
	if err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("unsupported legal fact error = %v", err)
	}
	_, err = validateProfileSuggestions([]ProfileSuggestion{{
		Field: "lifecycle-stage", Values: []string{"production"}, Confidence: "medium", Rationale: "Deployment file",
		Evidence: []ProfileEvidence{{Path: "missing.yml", Summary: "Deployment"}},
	}}, request)
	if err == nil || !strings.Contains(err.Error(), "unavailable path") {
		t.Fatalf("unsupported path error = %v", err)
	}
}

func TestDraftProfileReturnsNoSuggestionsWithoutContext(t *testing.T) {
	provider := &OllamaProvider{kind: Ollama, label: "Ollama", model: "test-model"}
	result, err := provider.DraftProfile(context.Background(), ProfileDraftRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 0 || len(result.Notes) < 3 {
		t.Fatalf("result = %#v", result)
	}
}
