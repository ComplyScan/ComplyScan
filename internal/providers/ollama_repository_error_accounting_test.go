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
			validationErr, ok := AsRepositoryValidationError(err)
			if !ok || validationErr.Diagnostic == "" || validationErr.Diagnostic != err.Error() {
				t.Fatalf("ReviewRepository() error was not typed corrective feedback: %#v", err)
			}
		})
	}
}

func TestReviewRepositoryRepairsInvalidCitationWithSameEvidence(t *testing.T) {
	invalidRawOutputMarker := "invalid-raw-output-must-not-be-replayed"
	responses := []string{
		`{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"runtime","confidence":"high","evidence":[{"path":"main.go","line":4,"summary":"` + invalidRawOutputMarker + `"}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
		`{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"runtime","confidence":"high","evidence":[{"path":"main.go","line":2,"summary":"The handler invokes the model client."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
	}
	var prompts []string
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			prompts = append(prompts, request.Messages[1].Content)
			index := len(prompts) - 1
			if index >= len(responses) {
				t.Fatalf("unexpected repair request %d", index+1)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = responses[index]
			response.PromptEvalCount = 100 + index
			response.EvalCount = 20 + index
			response.ReasoningCount = 5 + index
			response.TotalDuration = int64(1_000 + index)
			response.RateLimits = RateLimitSnapshot{RequestsKnown: true, LimitRequests: 100, RemainingRequests: 99 - index}
			return response, nil
		},
	}
	request := RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1,
		Files: []RepositorySourceFile{{Path: "main.go", Kind: "source", LineCount: 3, ContentStartLine: 1, Content: "package main\nfunc draft() {}\n"}},
	}
	invalidResult, err := provider.ReviewRepository(context.Background(), request)
	validationErr, ok := AsRepositoryValidationError(err)
	if !ok || !strings.Contains(validationErr.Diagnostic, "citation main.go:4 is outside submitted lines 1-3") {
		t.Fatalf("initial validation error = %#v", err)
	}
	request.ValidationFeedback = validationErr.Diagnostic
	result, err := provider.ReviewRepository(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(prompts))
	}
	if !strings.Contains(prompts[1], "citation main.go:4 is outside submitted lines 1-3") {
		t.Fatalf("repair prompt omitted local validation diagnostic: %s", prompts[1])
	}
	if strings.Contains(prompts[1], invalidRawOutputMarker) {
		t.Fatalf("repair prompt leaked discarded raw model output: %s", prompts[1])
	}
	firstInput := prompts[0][strings.LastIndex(prompts[0], "\n\n")+2:]
	secondInput := prompts[1][strings.LastIndex(prompts[1], "\n\n")+2:]
	if firstInput != secondInput {
		t.Fatalf("repair changed the sanitized repository input\nfirst: %s\nsecond: %s", firstInput, secondInput)
	}
	if strings.Contains(secondInput, "ValidationFeedback") || strings.Contains(secondInput, "validation_feedback") {
		t.Fatalf("local validation feedback leaked into submitted repository data: %s", secondInput)
	}
	if invalidResult.Usage.PromptTokens != 100 || invalidResult.Usage.CompletionTokens != 20 || invalidResult.Usage.ReasoningTokens != 5 || invalidResult.Usage.TotalDurationNS != 1_000 {
		t.Fatalf("invalid attempt usage = %#v", invalidResult.Usage)
	}
	if result.Usage.PromptTokens != 101 || result.Usage.CompletionTokens != 21 || result.Usage.ReasoningTokens != 6 || result.Usage.TotalDurationNS != 1_001 {
		t.Fatalf("replacement attempt usage = %#v", result.Usage)
	}
	if result.RateLimits.RemainingRequests != 98 {
		t.Fatalf("repair rate limits = %#v", result.RateLimits)
	}
	if result.Coverage.CitationsChecked != 1 || len(result.Result.AIUses) != 1 {
		t.Fatalf("repaired result = %#v", result)
	}
	if len(result.Notes) != 2 {
		t.Fatalf("replacement notes = %#v", result.Notes)
	}
}

func TestReviewRepositoryReturnsOneTypedValidationFailurePerCall(t *testing.T) {
	calls := 0
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, _ ollamaChatRequest) (ollamaChatResponse, error) {
			calls++
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"runtime","confidence":"high","evidence":[{"path":"main.go","line":4,"summary":"Invented line."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			response.PromptEvalCount = 10
			response.EvalCount = 2
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1,
		Files: []RepositorySourceFile{{Path: "main.go", Kind: "source", LineCount: 3, ContentStartLine: 1, Content: "package main\nfunc draft() {}\n"}},
	})
	if err == nil || !strings.Contains(err.Error(), "outside submitted lines 1-3") {
		t.Fatalf("final validation error = %v", err)
	}
	if _, ok := AsRepositoryValidationError(err); !ok {
		t.Fatalf("validation error was not typed: %#v", err)
	}
	if calls != 1 {
		t.Fatalf("provider hid %d calls inside one repository review", calls)
	}
	if result.Usage.PromptTokens != 10 || result.Usage.CompletionTokens != 2 {
		t.Fatalf("failed repair usage = %#v", result.Usage)
	}
}

func TestReviewRepositoryRepairsSynthesisMembershipCitation(t *testing.T) {
	summaries := []RepositorySectionResult{
		{
			Scope: "route-batch",
			AIUses: []RepositoryAIUse{{
				ID: "route-use", Name: "Route", Purpose: "Receives requests", Lifecycle: "runtime", Confidence: "medium",
				Evidence: []RepositoryCitation{{Path: "route.go", Line: 2, Summary: "The route invokes the drafting service."}}, MemberObservationIDs: []string{"observation-route"},
			}},
			AIUseFacts: []RepositoryAIUseFactSet{{AIUseID: "route-use", Facts: []RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}},
		},
		{
			Scope: "model-batch",
			AIUses: []RepositoryAIUse{{
				ID: "model-use", Name: "Model", Purpose: "Drafts text", Lifecycle: "runtime", Confidence: "high",
				Evidence: []RepositoryCitation{{Path: "model.go", Line: 3, Summary: "The service invokes the model client."}}, MemberObservationIDs: []string{"observation-model"},
			}},
			AIUseFacts: []RepositoryAIUseFactSet{{AIUseID: "model-use", Facts: []RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}},
		},
	}
	responses := []string{
		`{"result":{"scope":"synthesis","ai_uses":[{"id":"route-group","name":"Route","purpose":"Receives requests","lifecycle":"runtime","confidence":"medium","evidence":[{"path":"model.go","line":3,"summary":"Borrowed evidence must not survive."}],"member_observation_ids":["observation-route"],"unresolved_questions":[]},{"id":"model-group","name":"Model","purpose":"Drafts text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"model.go","line":3,"summary":"The service invokes the model client."}],"member_observation_ids":["observation-model"],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"route-group","facts":[],"unresolved_questions":[]},{"ai_use_id":"model-group","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
		`{"result":{"scope":"synthesis","ai_uses":[{"id":"route-group","name":"Route","purpose":"Receives requests","lifecycle":"runtime","confidence":"medium","evidence":[{"path":"route.go","line":2,"summary":"The route invokes the drafting service."}],"member_observation_ids":["observation-route"],"unresolved_questions":[]},{"id":"model-group","name":"Model","purpose":"Drafts text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"model.go","line":3,"summary":"The service invokes the model client."}],"member_observation_ids":["observation-model"],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"route-group","facts":[],"unresolved_questions":[]},{"ai_use_id":"model-group","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
	}
	calls := 0
	provider := &OllamaProvider{
		kind: Anthropic, label: "Anthropic", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if calls == 1 && !strings.Contains(request.Messages[1].Content, `outside its member observations`) {
				t.Fatalf("synthesis repair omitted membership diagnostic: %s", request.Messages[1].Content)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = responses[calls]
			calls++
			return response, nil
		},
	}
	request := RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: 2,
		FileIndex: []RepositoryFileReference{
			{Path: "route.go", Kind: "source", LineCount: 2},
			{Path: "model.go", Kind: "source", LineCount: 3},
		},
		SubsystemSummaries: summaries,
	}
	_, err := provider.ReviewRepository(context.Background(), request)
	validationErr, ok := AsRepositoryValidationError(err)
	if !ok || !strings.Contains(validationErr.Diagnostic, "outside its member observations") {
		t.Fatalf("initial synthesis validation error = %#v", err)
	}
	request.ValidationFeedback = validationErr.Diagnostic
	result, err := provider.ReviewRepository(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(result.Result.AIUses) != 2 {
		t.Fatalf("repaired synthesis calls=%d result=%#v", calls, result.Result)
	}
	if got := result.Result.AIUses[0].Evidence[0]; got.Path != "route.go" || got.Line != 2 {
		t.Fatalf("membership boundary was weakened: %#v", got)
	}
}
