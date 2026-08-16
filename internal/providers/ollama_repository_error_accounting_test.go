package providers

import (
	"context"
	"strings"
	"testing"
)

func TestReviewRepositoryInvalidModelOutputPreservesResponseAccounting(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		allowFollowUp bool
		wantError     string
	}{
		{
			name: "decode error", content: `{not-json`,
			wantError: "decode OpenAI structured repository analysis",
		},
		{
			name:      "section validation error",
			content:   `{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"low","evidence":[],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantError: "validate OpenAI repository analysis",
		},
		{
			name: "follow-up validation error", allowFollowUp: true,
			content:   `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]},"follow_up":{"needed":true,"queries":[],"reason":"More code may matter."}}`,
			wantError: "validate OpenAI repository follow-up plan",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = testCase.content
					response.PromptEvalCount = 321
					response.EvalCount = 45
					response.ReasoningCount = 23
					response.TotalDuration = 987_654
					return response, nil
				},
			}
			content := "package main\nfunc main() {}\n"
			result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1, RepositoryBytes: int64(len(content)), AllowFollowUp: testCase.allowFollowUp,
				Files: []RepositorySourceFile{{Path: "main.go", Kind: "source", LineCount: 3, ContentStartLine: 1, Content: content}},
			})
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("ReviewRepository() error = %v, want %q", err, testCase.wantError)
			}
			if result.Provider != OpenAI || result.Model != "test-model" {
				t.Fatalf("invalid response lost provider identity: %#v", result)
			}
			if result.Coverage.Mode != RepositoryAnalysisTargeted || result.Coverage.RepositoryFiles != 1 || result.Coverage.FilesSubmitted != 1 || result.Coverage.BytesSubmitted != int64(len(content)) {
				t.Fatalf("invalid response lost transfer coverage: %#v", result.Coverage)
			}
			if result.Usage.PromptTokens != 321 || result.Usage.CompletionTokens != 45 || result.Usage.ReasoningTokens != 23 || result.Usage.TotalDurationNS != 987_654 {
				t.Fatalf("invalid response lost provider usage: %#v", result.Usage)
			}
		})
	}
}
