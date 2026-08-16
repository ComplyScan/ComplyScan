package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReviewRepositoryValidatesCitationsAndRedactsSource(t *testing.T) {
	secret := repositoryTestCredential()
	redacted := "sk-proj-" + "****3456"
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			encoded := request.Messages[1].Content
			if strings.Contains(encoded, secret) {
				t.Fatal("repository source contained an unredacted credential")
			}
			if !strings.Contains(encoded, redacted) {
				t.Fatalf("repository source did not contain the expected redaction: %s", encoded)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"generation","name":"Text generation","purpose":"Generate summaries","lifecycle":"runtime","confidence":"high","evidence":[{"path":"main.go","line":2,"summary":"Runtime code invokes the model client."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"generation","facts":[],"unresolved_questions":[]}],"objective_observations":[{"objective_id":"OBJ-1","system_id":"system","strength":"partial","confidence":"medium","rationale":"The call is connected but runtime safeguards are not established.","supporting_evidence":[{"path":"main.go","line":2,"summary":"The runtime path invokes the client."}],"contradictory_evidence":[],"missing_evidence":["Runtime configuration"],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
			response.PromptEvalCount = 20
			response.EvalCount = 10
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1, RepositoryBytes: 80,
		Files:      []RepositorySourceFile{{Path: "main.go", Kind: "source", Content: "package main\nvar key = \"" + secret + "\"\n"}},
		Objectives: []RepositoryObjective{{ID: "OBJ-1", Title: "Document runtime", Description: "Map runtime use"}},
		Systems:    []RepositorySystemContext{{ID: "system", Name: "System"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.Mode != RepositoryAnalysisFull || result.Coverage.CitationsChecked != 2 || len(result.Result.AIUses) != 1 {
		t.Fatalf("unexpected repository result: %#v", result)
	}
	if result.Result.ObjectiveObservations[0].TechnicalVerdict != RepositoryVerdictPartial {
		t.Fatalf("technical verdict = %q", result.Result.ObjectiveObservations[0].TechnicalVerdict)
	}
}

func TestReviewRepositoryReturnsTypedCandidateFacts(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			encodedSchema, _ := json.Marshal(request.Format)
			if !strings.Contains(string(encodedSchema), `"ai_use_facts"`) || !strings.Contains(string(encodedSchema), `"const":"ai-activities"`) {
				t.Fatalf("repository schema omitted typed AI-use facts: %s", encodedSchema)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"support-drafter","name":"Support drafter","purpose":"Draft customer replies","lifecycle":"runtime path","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"The handler calls the model."},{"path":"app_test.go","line":2,"summary":"The test invokes the model-backed handler."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"support-drafter","facts":[{"field":"ai-activities","values":["inference","agent-tool-use"],"confidence":"high","rationale":"The handler invokes a model with a tool definition.","evidence":[{"path":"app.go","line":2,"summary":"The request sends a callable tool to the model."}]},{"field":"lifecycle-stage","values":["testing"],"confidence":"medium","rationale":"The submitted path is exercised by a test harness.","evidence":[{"path":"app_test.go","line":2,"summary":"The test invokes the model-backed handler."}]}],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 2,
		Files: []RepositorySourceFile{
			{Path: "app.go", Kind: "source", Content: "package app\nfunc draft() {}\n"},
			{Path: "app_test.go", Kind: "source", Content: "package app\nfunc testDraft() {}\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.CitationsChecked != 4 || len(result.Result.AIUseFacts) != 1 || len(result.Result.AIUseFacts[0].Facts) != 2 {
		t.Fatalf("typed AI-use facts = %#v", result)
	}
	if got := result.Result.AIUseFacts[0].Facts[0].Field; got != "ai-activities" {
		t.Fatalf("fact field = %q", got)
	}
}

func TestReviewRepositoryRequiresCheckedCandidateEvidenceAndFactCoverage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "uncited candidate",
			content: `{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"low","evidence":[],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantErr: `AI use "assistant" has no checked evidence citation`,
		},
		{
			name:    "missing candidate fact coverage",
			content: `{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"low","evidence":[{"path":"app.go","line":2,"summary":"The handler calls a model."}],"unresolved_questions":[]}],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantErr: `omitted required fact set for candidate AI use "assistant"`,
		},
		{
			name:    "candidate fact borrows sibling path",
			content: `{"result":{"scope":".","ai_uses":[{"id":"assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"medium","evidence":[{"path":"app.go","line":2,"summary":"The handler calls a model."}],"unresolved_questions":[]},{"id":"ranking","name":"Ranking","purpose":"Rank results","lifecycle":"unknown","confidence":"medium","evidence":[{"path":"rank.go","line":2,"summary":"The ranker calls a model."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"assistant","facts":[{"field":"ai-activities","values":["inference"],"confidence":"high","rationale":"The ranker invokes a model.","evidence":[{"path":"rank.go","line":2,"summary":"The ranker calls a model."}]}],"unresolved_questions":[]},{"ai_use_id":"ranking","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantErr: `outside candidate AI use "assistant" evidence paths`,
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
					return response, nil
				},
			}
			_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 2,
				Files: []RepositorySourceFile{
					{Path: "app.go", Kind: "source", Content: "package app\nfunc draft() {}\n"},
					{Path: "rank.go", Kind: "source", Content: "package app\nfunc rank() {}\n"},
				},
			})
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected %q, got %v", testCase.wantErr, err)
			}
		})
	}
}

func TestReviewRepositorySynthesisPreservesReviewedCandidatesAndExactCitations(t *testing.T) {
	summary := RepositorySectionResult{
		Scope: "subsystem-1",
		AIUses: []RepositoryAIUse{{
			ID: "subsystem-0001-assistant", Name: "Assistant", Purpose: "Draft replies", Lifecycle: "unknown", Confidence: "medium",
			Evidence: []RepositoryCitation{{Path: "app.go", Line: 2, Summary: "The handler calls a model."}},
		}},
		AIUseFacts:            []RepositoryAIUseFactSet{{AIUseID: "subsystem-0001-assistant", Facts: []RepositoryAIUseFact{}, UnresolvedQuestions: []string{"Deployment is not established."}}},
		ObjectiveObservations: []RepositoryObjectiveObservation{},
		UnmappedObservations:  []RepositoryUnmappedObservation{},
		UnresolvedQuestions:   []string{},
	}
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "preserves reviewed empty fact set",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"subsystem-0001-assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"medium","evidence":[{"path":"app.go","line":2,"summary":"The handler calls a model."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"subsystem-0001-assistant","facts":[],"unresolved_questions":["Deployment is not established."]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
		},
		{
			name:    "rejects invented line in checked file",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"subsystem-0001-assistant","name":"Assistant","purpose":"Draft replies","lifecycle":"unknown","confidence":"medium","evidence":[{"path":"app.go","line":3,"summary":"A different line allegedly calls a model."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"subsystem-0001-assistant","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantErr: `synthesis citation app.go:3 was not present in a checked subsystem result`,
		},
		{
			name:    "rejects omitted reviewed candidate",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			wantErr: `repository synthesis omitted reviewed candidate AI use "subsystem-0001-assistant"`,
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
					return response, nil
				},
			}
			result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisSynthesis, Scope: "repository-synthesis", RepositoryFiles: 1,
				FileIndex:          []RepositoryFileReference{{Path: "app.go", Kind: "source", LineCount: 3}},
				SubsystemSummaries: []RepositorySectionResult{summary},
			})
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("expected %q, got %v", testCase.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Result.AIUseFacts) != 1 || len(result.Result.AIUseFacts[0].Facts) != 0 {
				t.Fatalf("reviewed-empty fact coverage was not preserved: %#v", result.Result.AIUseFacts)
			}
		})
	}
}

func TestReviewRepositoryRejectsInvalidAIUseFactIdentityAndDuplicates(t *testing.T) {
	tests := []struct {
		name      string
		facts     string
		wantErr   string
		confirmed []RepositoryConfirmedAIUse
		candidate string
	}{
		{
			name: "unknown ID", facts: `[{"ai_use_id":"other-use","facts":[{"field":"ai-activities","values":["inference"],"confidence":"high","rationale":"The handler invokes a model.","evidence":[{"path":"app.go","line":2,"summary":"Model call."}]}],"unresolved_questions":[]}]`,
			wantErr: `facts for unknown AI use "other-use"`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "duplicate field", facts: `[{"ai_use_id":"known-use","facts":[{"field":"ai-activities","values":["inference"],"confidence":"high","rationale":"The handler invokes a model.","evidence":[{"path":"app.go","line":2,"summary":"Model call."}]},{"field":"ai-activities","values":["training"],"confidence":"medium","rationale":"The handler trains a model.","evidence":[{"path":"app.go","line":2,"summary":"Training call."}]}],"unresolved_questions":[]}]`,
			wantErr: `duplicate fact field "ai-activities"`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "duplicate value", facts: `[{"ai_use_id":"known-use","facts":[{"field":"ai-activities","values":["inference","inference"],"confidence":"high","rationale":"The handler invokes a model.","evidence":[{"path":"app.go","line":2,"summary":"Model call."}]}],"unresolved_questions":[]}]`,
			wantErr: `duplicate ai-activities value "inference"`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "non-canonical exact ID", facts: `[{"ai_use_id":" known-use ","facts":[{"field":"ai-activities","values":["inference"],"confidence":"high","rationale":"The handler invokes a model.","evidence":[{"path":"app.go","line":2,"summary":"Model call."}]}],"unresolved_questions":[]}]`,
			wantErr: `non-canonical fact AI-use ID`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "actual production conclusion", facts: `[{"ai_use_id":"known-use","facts":[{"field":"lifecycle-stage","values":["production"],"confidence":"high","rationale":"A deployment file exists.","evidence":[{"path":"app.go","line":2,"summary":"Deployment entry point."}]}],"unresolved_questions":[]}]`,
			wantErr: `unsupported lifecycle-stage value "production"`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "distribution conclusion", facts: `[{"ai_use_id":"known-use","facts":[{"field":"deployment-models","values":["public"],"confidence":"high","rationale":"A release file exists.","evidence":[{"path":"app.go","line":2,"summary":"Release configuration."}]}],"unresolved_questions":[]}]`,
			wantErr: `unsupported deployment-models value "public"`, candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
		},
		{
			name: "confirmed ID recreated as candidate", facts: `[]`, wantErr: `recreated confirmed AI use "known-use" as a candidate`,
			candidate: `[{"id":"known-use","name":"Known use","purpose":"Generate text","lifecycle":"runtime","confidence":"high","evidence":[{"path":"app.go","line":2,"summary":"Model call."}],"unresolved_questions":[]}]`,
			confirmed: []RepositoryConfirmedAIUse{{ID: "known-use", Name: "Confirmed use", Paths: []string{"app.go"}, SubmittedFiles: []string{"app.go"}}},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					content := `{"result":{"scope":".","ai_uses":` + testCase.candidate + `,"ai_use_facts":` + testCase.facts + `,"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = content
					return response, nil
				},
			}
			_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
				Files: []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\nfunc run() {}\n"}}, ConfirmedAIUses: testCase.confirmed,
			})
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("expected %q, got %v", testCase.wantErr, err)
			}
		})
	}
}

func TestReviewRepositoryRequiresAIUseFactsArray(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
		Files: []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\n"}},
	})
	if err == nil || !strings.Contains(err.Error(), "omitted required ai_use_facts array") {
		t.Fatalf("expected missing facts-array error, got %v", err)
	}
}

func TestReviewRepositoryRequiresConfirmedUseFactCoverage(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
		Files:           []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\n"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{ID: "support-replies", Name: "Support replies", Paths: []string{"app.go"}, SubmittedFiles: []string{"app.go"}}},
	})
	if err == nil || !strings.Contains(err.Error(), `omitted required fact set for confirmed AI use "support-replies"`) {
		t.Fatalf("expected confirmed-use fact coverage error, got %v", err)
	}
}

func TestReviewRepositoryPreservesUseSpecificUnknownsWithoutInventingFact(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"support-drafter","name":"Support drafter","purpose":"Draft replies","lifecycle":"unclear","confidence":"medium","evidence":[{"path":"app.go","line":2,"summary":"The handler invokes a model."}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"support-drafter","facts":[],"unresolved_questions":["Is this handler enabled outside tests?"]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
		Files: []RepositorySourceFile{{Path: "app.go", Kind: "source", Content: "package app\nfunc draft() {}\n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUseFacts) != 1 || len(result.Result.AIUseFacts[0].Facts) != 0 || len(result.Result.AIUseFacts[0].UnresolvedQuestions) != 1 {
		t.Fatalf("question-only fact review = %#v", result.Result.AIUseFacts)
	}
}

func TestReviewRepositoryRejectsConfirmedFactCitationFromAnotherUse(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[{"ai_use_id":"support-replies","facts":[{"field":"human-oversight","values":["required"],"confidence":"high","rationale":"The workflow calls an approval gate.","evidence":[{"path":"ranking/review.go","line":2,"summary":"Ranking approval gate."}]}],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 2,
		Files: []RepositorySourceFile{
			{Path: "support/review.go", Kind: "source", Content: "package support\nfunc approve() {}\n"},
			{Path: "ranking/review.go", Kind: "source", Content: "package ranking\nfunc approve() {}\n"},
		},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{
			{ID: "support-replies", Name: "Support replies", Paths: []string{"support/**"}, SubmittedFiles: []string{"support/review.go"}},
			{ID: "ranking", Name: "Ranking", Paths: []string{"ranking/**"}, SubmittedFiles: []string{"ranking/review.go"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `outside confirmed AI use "support-replies" submitted scope`) {
		t.Fatalf("expected cross-use fact citation error, got %v", err)
	}
}

func TestRepositoryObjectiveObservationDerivesTechnicalVerdict(t *testing.T) {
	support := []RepositoryCitation{{Path: "app.go", Line: 10, Summary: "Production path implements the safeguard."}}
	for _, testCase := range []struct {
		name  string
		value RepositoryObjectiveObservation
		want  RepositoryTechnicalVerdict
	}{
		{name: "implemented", value: RepositoryObjectiveObservation{Strength: StrengthStrong, Confidence: "high", SupportingEvidence: support}, want: RepositoryVerdictImplemented},
		{name: "strong with missing part", value: RepositoryObjectiveObservation{Strength: StrengthStrong, Confidence: "high", SupportingEvidence: support, MissingEvidence: []string{"Bypass handling"}}, want: RepositoryVerdictPartial},
		{name: "partial", value: RepositoryObjectiveObservation{Strength: StrengthPartial, Confidence: "medium", SupportingEvidence: support}, want: RepositoryVerdictPartial},
		{name: "not implemented", value: RepositoryObjectiveObservation{Strength: StrengthNotSupported, Confidence: "high"}, want: RepositoryVerdictNotImplemented},
		{name: "untrusted supplied verdict ignored", value: RepositoryObjectiveObservation{Strength: StrengthNotSupported, Confidence: "high", TechnicalVerdict: RepositoryVerdictImplemented}, want: RepositoryVerdictNotImplemented},
		{name: "low confidence", value: RepositoryObjectiveObservation{Strength: StrengthStrong, Confidence: "low", SupportingEvidence: support}, want: RepositoryVerdictCannotDetermine},
		{name: "uncertain", value: RepositoryObjectiveObservation{Strength: StrengthUncertain, Confidence: "medium"}, want: RepositoryVerdictCannotDetermine},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.value.DerivedTechnicalVerdict(); got != testCase.want {
				t.Fatalf("verdict = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestReviewRepositoryReturnsValidatedBoundedFollowUp(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if !strings.Contains(request.Messages[1].Content, `"allow_follow_up":true`) {
				t.Fatal("follow-up permission was not explicit in the request")
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]},"follow_up":{"needed":true,"queries":[{"text":"approve_response","path_hint":"review","reason":"Find the human approval path."}],"reason":"Approval code could change the oversight conclusion."}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1, AllowFollowUp: true,
		Files: []RepositorySourceFile{{Path: "main.go", Kind: "source", Content: "package main\n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FollowUpPlan.Needed || len(result.FollowUpPlan.Queries) != 1 || result.FollowUpPlan.Queries[0].Text != "approve_response" {
		t.Fatalf("follow-up plan = %#v", result.FollowUpPlan)
	}
}

func repositoryTestCredential() string {
	return "sk-" + "proj-" + "abcdefghijklmnopqrstuvwxyz" + "123456"
}

func TestReviewRepositoryRejectsInventedCitation(t *testing.T) {
	provider := &OllamaProvider{
		kind: Anthropic, label: "Anthropic", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"use","name":"Use","purpose":"Purpose","lifecycle":"unknown","confidence":"low","evidence":[{"path":"invented.go","line":1,"summary":"Invented path"}],"unresolved_questions":[]}],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
		Files: []RepositorySourceFile{{Path: "main.go", Kind: "source", Content: "package main\n"}},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown path "invented.go"`) {
		t.Fatalf("expected invented citation error, got %v", err)
	}
}

func TestReviewRepositoryValidatesSegmentOriginalLineNumbers(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		line    int
		wantErr bool
	}{
		{name: "original segment line", line: 51},
		{name: "line outside segment", line: 1, wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = `{"result":{"scope":"segment","ai_uses":[{"id":"use","name":"Use","purpose":"Purpose","lifecycle":"unknown","confidence":"low","evidence":[{"path":"main.go","line":` + fmt.Sprint(testCase.line) + `,"summary":"Segment citation"}],"unresolved_questions":[]}],"ai_use_facts":[{"ai_use_id":"use","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
					return response, nil
				},
			}
			_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisSubsystem, Scope: "segment", RepositoryFiles: 1,
				Files: []RepositorySourceFile{{
					Path: "main.go", Kind: "source", LineCount: 100, ContentStartLine: 51,
					Content: "func segment() {}\nfunc next() {}",
				}},
			})
			if testCase.wantErr && (err == nil || !strings.Contains(err.Error(), "outside submitted lines 51-52")) {
				t.Fatalf("expected segment citation error, got %v", err)
			}
			if !testCase.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReviewRepositoryRejectsUnknownObjective(t *testing.T) {
	provider := &OllamaProvider{
		kind: Gemini, label: "Gemini", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[],"objective_observations":[{"objective_id":"INVENTED","system_id":"","strength":"uncertain","confidence":"low","rationale":"Unknown","supporting_evidence":[],"contradictory_evidence":[],"missing_evidence":[],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisFull, Scope: ".", RepositoryFiles: 1,
		Files:      []RepositorySourceFile{{Path: "main.go", Kind: "source", Content: "package main\n"}},
		Objectives: []RepositoryObjective{{ID: "OBJ-1"}},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown objective "INVENTED"`) {
		t.Fatalf("expected unknown objective error, got %v", err)
	}
}

func TestReviewRepositoryValidatesExplicitConfirmedUseScope(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			if !strings.Contains(request.Messages[1].Content, `"confirmed_ai_uses":[{"id":"support-replies"`) ||
				!strings.Contains(request.Messages[1].Content, `"submitted_files":["support/review.go"]`) {
				t.Fatalf("confirmed use context missing from bounded request: %s", request.Messages[1].Content)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[{"ai_use_id":"support-replies","facts":[],"unresolved_questions":[]}],"objective_observations":[{"objective_id":"PACK/REVIEW","ai_use_id":"support-replies","system_id":"support","strength":"partial","confidence":"high","rationale":"The reviewed flow contains a human gate but still has a bypass.","supporting_evidence":[{"path":"support/review.go","line":2,"summary":"Human approval is called."}],"contradictory_evidence":[],"missing_evidence":["Bypass prevention"],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 2,
		Files: []RepositorySourceFile{
			{Path: "support/review.go", Kind: "source", Content: "package support\nfunc approve() {}\n"},
			{Path: "ranking/review.go", Kind: "source", Content: "package ranking\nfunc approve() {}\n"},
		},
		Objectives: []RepositoryObjective{{ID: "PACK/REVIEW", Title: "Human review"}},
		Systems:    []RepositorySystemContext{{ID: "support", Name: "Support"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "support-replies", Name: "Support replies", Description: "Draft replies", Paths: []string{"support/**"},
			SystemIDs: []string{"support"}, SubmittedFiles: []string{"support/review.go"},
			Objectives: []RepositoryAIUseObjectiveContext{{ObjectiveID: "PACK/REVIEW", SystemID: "support", Requirement: "likely-required"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := result.Result.ObjectiveObservations[0]
	if observation.AIUseID != "support-replies" || observation.TechnicalVerdict != RepositoryVerdictPartial {
		t.Fatalf("use-scoped observation = %#v", observation)
	}
}

func TestReviewRepositoryRejectsConfirmedUseCitationOutsideSubmittedScope(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[{"ai_use_id":"support-replies","facts":[],"unresolved_questions":[]}],"objective_observations":[{"objective_id":"PACK/REVIEW","ai_use_id":"support-replies","system_id":"support","strength":"strong","confidence":"high","rationale":"Wrong sibling scope.","supporting_evidence":[{"path":"ranking/review.go","line":2,"summary":"Sibling review path."}],"contradictory_evidence":[],"missing_evidence":[],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 2,
		Files: []RepositorySourceFile{
			{Path: "support/review.go", Kind: "source", Content: "package support\nfunc approve() {}\n"},
			{Path: "ranking/review.go", Kind: "source", Content: "package ranking\nfunc approve() {}\n"},
		},
		Objectives: []RepositoryObjective{{ID: "PACK/REVIEW"}}, Systems: []RepositorySystemContext{{ID: "support", Name: "Support"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "support-replies", Name: "Support replies", Paths: []string{"support/**"}, SystemIDs: []string{"support"},
			SubmittedFiles: []string{"support/review.go"}, Objectives: []RepositoryAIUseObjectiveContext{{ObjectiveID: "PACK/REVIEW", SystemID: "support", Requirement: "likely-required"}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `outside confirmed AI use "support-replies" submitted scope`) {
		t.Fatalf("expected use-scope citation error, got %v", err)
	}
}

func TestReviewRepositoryAcceptsUncitedUncertainConfirmedUseObservation(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[{"ai_use_id":"support-replies","facts":[],"unresolved_questions":[]}],"objective_observations":[{"objective_id":"PACK/REVIEW","ai_use_id":"support-replies","system_id":"support","strength":"uncertain","confidence":"medium","rationale":"No bounded decision can be grounded.","supporting_evidence":[],"contradictory_evidence":[],"missing_evidence":["Reviewed executable flow"],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1,
		Files:      []RepositorySourceFile{{Path: "support/review.go", Kind: "source", Content: "package support\nfunc approve() {}\n"}},
		Objectives: []RepositoryObjective{{ID: "PACK/REVIEW"}}, Systems: []RepositorySystemContext{{ID: "support", Name: "Support"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "support-replies", Name: "Support replies", Paths: []string{"support/**"}, SystemIDs: []string{"support"},
			SubmittedFiles: []string{"support/review.go"}, Objectives: []RepositoryAIUseObjectiveContext{{ObjectiveID: "PACK/REVIEW", SystemID: "support", Requirement: "likely-required"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := result.Result.ObjectiveObservations[0]
	if observation.AIUseID != "support-replies" || observation.TechnicalVerdict != RepositoryVerdictCannotDetermine {
		t.Fatalf("uncited uncertain use observation = %#v", observation)
	}
}

func TestReviewRepositoryRejectsIncompleteConfirmedUseEvaluation(t *testing.T) {
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"ai_use_facts":[{"ai_use_id":"support-replies","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1,
		Files:      []RepositorySourceFile{{Path: "support/review.go", Kind: "source", Content: "package support\nfunc approve() {}\n"}},
		Objectives: []RepositoryObjective{{ID: "PACK/REVIEW"}}, Systems: []RepositorySystemContext{{ID: "support", Name: "Support"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "support-replies", Name: "Support replies", Paths: []string{"support/**"}, SystemIDs: []string{"support"},
			SubmittedFiles: []string{"support/review.go"}, Objectives: []RepositoryAIUseObjectiveContext{{ObjectiveID: "PACK/REVIEW", SystemID: "support", Requirement: "likely-required"}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), `omitted objective "PACK/REVIEW" for confirmed AI use "support-replies"`) {
		t.Fatalf("expected incomplete direct-use evaluation error, got %v", err)
	}
}

func TestRepositoryAnalysisRejectsTooManyDirectUseObjectivesBeforeInference(t *testing.T) {
	request := RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisTargeted, Scope: ".", RepositoryFiles: 1,
		Files:   []RepositorySourceFile{{Path: "support/review.go", Kind: "source", Content: "package support\n"}},
		Systems: []RepositorySystemContext{{ID: "support", Name: "Support"}},
		ConfirmedAIUses: []RepositoryConfirmedAIUse{{
			ID: "support-replies", Name: "Support replies", Paths: []string{"support/**"}, SystemIDs: []string{"support"},
			SubmittedFiles: []string{"support/review.go"},
		}},
	}
	for index := 0; index <= maxRepositoryObservations; index++ {
		objectiveID := fmt.Sprintf("PACK/OBJECTIVE-%03d", index)
		request.Objectives = append(request.Objectives, RepositoryObjective{ID: objectiveID})
		request.ConfirmedAIUses[0].Objectives = append(request.ConfirmedAIUses[0].Objectives, RepositoryAIUseObjectiveContext{
			ObjectiveID: objectiveID, SystemID: "support", Requirement: "likely-required",
		})
	}
	_, _, _, _, _, _, _, err := sanitizeRepositoryAnalysisRequest(request)
	if err == nil || !strings.Contains(err.Error(), "more than 500 direct objective contexts") {
		t.Fatalf("expected direct objective limit, got %v", err)
	}
}
