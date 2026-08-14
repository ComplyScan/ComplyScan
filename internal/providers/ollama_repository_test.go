package providers

import (
	"context"
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
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"generation","name":"Text generation","purpose":"Generate summaries","lifecycle":"runtime","confidence":"high","evidence":[{"path":"main.go","line":2,"summary":"Runtime code invokes the model client."}],"unresolved_questions":[]}],"objective_observations":[{"objective_id":"OBJ-1","system_id":"system","strength":"partial","confidence":"medium","rationale":"The call is connected but runtime safeguards are not established.","supporting_evidence":[{"path":"main.go","line":2,"summary":"The runtime path invokes the client."}],"contradictory_evidence":[],"missing_evidence":["Runtime configuration"],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
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
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]},"follow_up":{"needed":true,"queries":[{"text":"approve_response","path_hint":"review","reason":"Find the human approval path."}],"reason":"Approval code could change the oversight conclusion."}}`
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
			response.Message.Content = `{"result":{"scope":".","ai_uses":[{"id":"use","name":"Use","purpose":"Purpose","lifecycle":"unknown","confidence":"low","evidence":[{"path":"invented.go","line":1,"summary":"Invented path"}],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
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
					response.Message.Content = `{"result":{"scope":"segment","ai_uses":[{"id":"use","name":"Use","purpose":"Purpose","lifecycle":"unknown","confidence":"low","evidence":[{"path":"main.go","line":` + fmt.Sprint(testCase.line) + `,"summary":"Segment citation"}],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
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
			response.Message.Content = `{"result":{"scope":".","ai_uses":[],"objective_observations":[{"objective_id":"INVENTED","system_id":"","strength":"uncertain","confidence":"low","rationale":"Unknown","supporting_evidence":[],"contradictory_evidence":[],"missing_evidence":[],"unresolved_questions":[]}],"unmapped_observations":[],"unresolved_questions":[]}}`
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
