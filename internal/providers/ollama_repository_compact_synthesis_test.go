package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewRepositoryCompactSynthesisReturnsGroupingOnly(t *testing.T) {
	summaries := compactSynthesisTestSummaries()
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(_ context.Context, request ollamaChatRequest) (ollamaChatResponse, error) {
			encodedSchema, _ := json.Marshal(request.Format)
			schema := string(encodedSchema)
			if strings.Contains(schema, `"evidence"`) || strings.Contains(schema, `"rationale"`) {
				t.Fatalf("compact grouping schema still asks the model to repeat evidence: %s", schema)
			}
			if !strings.Contains(schema, `"member_observation_ids"`) || !strings.Contains(schema, `"maxItems":0`) {
				t.Fatalf("compact grouping schema omitted membership or required-empty arrays: %s", schema)
			}
			if !strings.Contains(request.Messages[0].Content, "only task is to decide") || !strings.Contains(request.Messages[1].Content, "reattaches those validated records locally") {
				t.Fatalf("compact grouping prompts did not establish the grouping-only contract: %#v", request.Messages)
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"temporary-group","name":"Support drafting","purpose":"Draft replies requested by the support route","lifecycle":"development","confidence":"high","member_observation_ids":["observation-a","observation-b"]}],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`
			return response, nil
		},
	}
	result, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisSynthesis, Scope: "repository-synthesis", RepositoryFiles: 2,
		SubsystemSummaries: summaries, CompactSynthesis: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Result.AIUses) != 1 || len(result.Result.AIUses[0].MemberObservationIDs) != 2 {
		t.Fatalf("compact grouping result = %#v", result.Result)
	}
	if result.Coverage.CitationsChecked != 0 || len(result.Result.AIUseFacts) != 0 || len(result.Result.ObjectiveObservations) != 0 {
		t.Fatalf("compact grouping repeated locally retained evidence: %#v", result)
	}
}

func TestReviewRepositoryCompactSynthesisRejectsInvalidMembershipAndRepeatedEvidence(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "omitted observation",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"group","name":"Drafting","purpose":"Draft replies","lifecycle":"development","confidence":"high","member_observation_ids":["observation-a"]}],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			want:    `omitted reviewed evidence observation "observation-b"`,
		},
		{
			name:    "duplicate observation",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"one","name":"One","purpose":"First group","lifecycle":"development","confidence":"medium","member_observation_ids":["observation-a"]},{"id":"two","name":"Two","purpose":"Second group","lifecycle":"development","confidence":"medium","member_observation_ids":["observation-a","observation-b"]}],"ai_use_facts":[],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			want:    `assigned observation "observation-a" to both`,
		},
		{
			name:    "repeated facts",
			content: `{"result":{"scope":"repository-synthesis","ai_uses":[{"id":"group","name":"Drafting","purpose":"Draft replies","lifecycle":"development","confidence":"high","member_observation_ids":["observation-a","observation-b"]}],"ai_use_facts":[{"ai_use_id":"group","facts":[],"unresolved_questions":[]}],"objective_observations":[],"unmapped_observations":[],"unresolved_questions":[]}}`,
			want:    "repeated locally retained facts",
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
				Mode: RepositoryAnalysisSynthesis, Scope: "repository-synthesis", RepositoryFiles: 2,
				SubsystemSummaries: compactSynthesisTestSummaries(), CompactSynthesis: true,
			})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected %q, got %v", testCase.want, err)
			}
		})
	}
}

func compactSynthesisTestSummaries() []RepositorySectionResult {
	makeSummary := func(scope, useID, observationID, path string, line int) RepositorySectionResult {
		return RepositorySectionResult{
			Scope: scope,
			AIUses: []RepositoryAIUse{{
				ID: useID, Name: "Drafting observation", Purpose: "Draft a support reply", Lifecycle: "development", Confidence: "medium",
				Evidence: []RepositoryCitation{{Path: path, Line: line, Summary: "The workflow invokes a model."}}, MemberObservationIDs: []string{observationID},
			}},
			AIUseFacts:            []RepositoryAIUseFactSet{{AIUseID: useID, Facts: []RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}},
			ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		}
	}
	return []RepositorySectionResult{
		makeSummary("batch-a", "use-a", "observation-a", "route.go", 2),
		makeSummary("batch-b", "use-b", "observation-b", "model.go", 3),
	}
}
