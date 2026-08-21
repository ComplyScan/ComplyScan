package providers

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestCompactSourceReturnsAtomicObservationWithoutModelAuthoredUseID(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if request.ReasoningEffort != reasoningEffortLow {
				t.Fatalf("source reasoning effort = %q, want low", request.ReasoningEffort)
			}
			encodedSchema, _ := json.Marshal(request.Format)
			schema := string(encodedSchema)
			if !strings.Contains(schema, `"source_result"`) || !strings.Contains(schema, `"block_id"`) || !strings.Contains(schema, `"observations"`) {
				t.Fatalf("compact source schema omitted atomic observation fields: %s", schema)
			}
			if strings.Contains(schema, `"observations":{"type":"array","items":{"type":"object","properties":{"id"`) {
				t.Fatalf("compact source observation unexpectedly asks the model for an ID: %s", schema)
			}
			input := request.Messages[1].Content
			if !strings.Contains(input, `"block_id":"block-0001"`) || !strings.Contains(input, `"block_id":"block-0002"`) {
				t.Fatalf("compact source input omitted trusted block identities: %s", input)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{
				"source_result": {
					"scope": "provider flow",
					"observations": [{
						"name": "Hosted repository review",
						"purpose": "Analyze selected repository evidence with a hosted model",
						"lifecycle": "development",
						"confidence": "high",
						"evidence": [{"block_id":"block-0001","line":2,"summary":"The review path calls the configured model."}],
						"facts": [{
							"field":"deployment-models","values":["api"],"confidence":"high",
							"rationale":"The adapter defines a hosted API endpoint.",
							"evidence":[{"block_id":"block-0002","line":2,"summary":"The adapter uses a hosted endpoint."}]
						}],
						"unresolved_questions": []
					}],
					"confirmed_ai_uses": [],
					"objective_observations": [],
					"unmapped_observations": [],
					"unresolved_questions": []
				}
			}`
			return response, nil
		},
	}

	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: "provider flow", RepositoryFiles: 2, CompactSource: true,
		Files: []RepositorySourceFile{
			{Path: "internal/providers/review.go", Kind: "go", Content: "package providers\nfunc review() {}\n"},
			{Path: "internal/providers/remote.go", Kind: "go", Content: "package providers\nconst endpoint = \"https://api.example.test\"\n"},
		},
		Objectives: []RepositoryObjective{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUses) != 1 || result.Result.AIUses[0].ID != "source-observation-0001" {
		t.Fatalf("locally assigned observation = %#v", result.Result.AIUses)
	}
	use := result.Result.AIUses[0]
	if len(use.Evidence) != 2 || use.Evidence[0].Path != "internal/providers/review.go" || use.Evidence[1].Path != "internal/providers/remote.go" {
		t.Fatalf("fact evidence was not unioned into its observation: %#v", use.Evidence)
	}
	if len(result.Result.AIUseFacts) != 1 || result.Result.AIUseFacts[0].AIUseID != use.ID || len(result.Result.AIUseFacts[0].Facts) != 1 {
		t.Fatalf("atomic facts were not bound locally: %#v", result.Result.AIUseFacts)
	}
}

func TestCompactSourceRejectsUnknownOrOutOfRangeBlockEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		blockID   string
		line      int
		wantError string
	}{
		{name: "unknown block", blockID: "block-9999", line: 2, wantError: `unknown block "block-9999"`},
		{name: "outside block", blockID: "block-0001", line: 99, wantError: `outside block "block-0001" lines 1-3`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					content := `{"source_result":{"scope":".","observations":[{"name":"Review","purpose":"Review code","lifecycle":"development","confidence":"high","evidence":[{"block_id":"` + testCase.blockID + `","line":` + strconv.Itoa(testCase.line) + `,"summary":"Model call."}],"facts":[],"unresolved_questions":[]}],"confirmed_ai_uses":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = content
					return response, nil
				},
			}
			_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1, CompactSource: true,
				Files: []RepositorySourceFile{{Path: "app.go", Kind: "go", Content: "package app\nfunc review() {}\n"}},
			})
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestCompactSourcePreservesExactConfirmedUseBindingWithoutCandidateID(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"source_result":{"scope":"qualification","observations":[],"confirmed_ai_uses":[{"ai_use_id":"owned-use","facts":[],"objective_observations":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: "qualification", RepositoryFiles: 1, CompactSource: true,
		Files: []RepositorySourceFile{{Path: "fixture.txt", Kind: "text", Content: "synthetic fixture\n"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "owned-use", Name: "Owned use", Paths: []string{"fixture.txt"}, SubmittedFiles: []string{"fixture.txt"},
			SystemIDs: []string{}, Objectives: []RepositoryAIUseObjectiveContext{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUses) != 0 || len(result.Result.AIUseFacts) != 1 || result.Result.AIUseFacts[0].AIUseID != "owned-use" {
		t.Fatalf("confirmed binding = %#v", result.Result)
	}
}
