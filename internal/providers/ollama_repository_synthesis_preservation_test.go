package providers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestReviewRepositorySynthesisRejectsDroppedValidatedSemantics(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RepositorySectionResult)
	}{
		{
			name: "generic objective observation",
			mutate: func(result *RepositorySectionResult) {
				result.ObjectiveObservations = []RepositoryObjectiveObservation{}
			},
		},
		{
			name: "unmapped observation",
			mutate: func(result *RepositorySectionResult) {
				result.UnmappedObservations = []RepositoryUnmappedObservation{}
			},
		},
		{
			name: "unmapped observation citation",
			mutate: func(result *RepositorySectionResult) {
				result.UnmappedObservations[0].Evidence = []RepositoryCitation{}
			},
		},
		{
			name: "top-level unresolved question",
			mutate: func(result *RepositorySectionResult) {
				result.UnresolvedQuestions = []string{}
			},
		},
		{
			name: "candidate unresolved question",
			mutate: func(result *RepositorySectionResult) {
				result.AIUses[0].UnresolvedQuestions = []string{}
			},
		},
		{
			name: "use fact unresolved question",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].UnresolvedQuestions = []string{}
			},
		},
		{
			name: "positive fact field",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].Facts = []RepositoryAIUseFact{}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			summary, synthesis := repositorySynthesisPreservationFixture()
			testCase.mutate(&synthesis)
			content, err := json.Marshal(repositoryAnalysisPayload{Result: synthesis})
			if err != nil {
				t.Fatal(err)
			}
			provider := &OllamaProvider{
				kind: OpenAI, label: "OpenAI", model: "test-model",
				completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
					var response ollamaChatResponse
					response.Done = true
					response.Message.Content = string(content)
					return response, nil
				},
			}

			_, err = provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
				Mode: RepositoryAnalysisSynthesis, Scope: "repository-synthesis", RepositoryFiles: 1,
				FileIndex:          []RepositoryFileReference{{Path: "app.go", Kind: "source", LineCount: 2}},
				Objectives:         []RepositoryObjective{{ID: "OBJ-1", Title: "Review the model path"}},
				SubsystemSummaries: []RepositorySectionResult{summary},
			})
			if err == nil {
				t.Fatalf("synthesis dropped %s without a validation error", testCase.name)
			}
			if _, ok := AsRepositoryValidationError(err); !ok {
				t.Fatalf("dropping %s returned %T, want *RepositoryValidationError: %v", testCase.name, err, err)
			}
		})
	}
}

func TestReviewRepositorySynthesisAcceptsPreservedValidatedSemantics(t *testing.T) {
	summary, synthesis := repositorySynthesisPreservationFixture()
	content, err := json.Marshal(repositoryAnalysisPayload{Result: synthesis})
	if err != nil {
		t.Fatal(err)
	}
	provider := &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = string(content)
			return response, nil
		},
	}

	_, err = provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisSynthesis, Scope: "repository-synthesis", RepositoryFiles: 1,
		FileIndex:          []RepositoryFileReference{{Path: "app.go", Kind: "source", LineCount: 2}},
		Objectives:         []RepositoryObjective{{ID: "OBJ-1", Title: "Review the model path"}},
		SubsystemSummaries: []RepositorySectionResult{summary},
	})
	if err != nil {
		t.Fatalf("fully preserved synthesis was rejected: %v", err)
	}
}

func repositorySynthesisPreservationFixture() (RepositorySectionResult, RepositorySectionResult) {
	build := func(scope, useID string) RepositorySectionResult {
		citation := RepositoryCitation{Path: "app.go", Line: 2, Summary: "The handler invokes the model."}
		return RepositorySectionResult{
			Scope: scope,
			AIUses: []RepositoryAIUse{{
				ID: useID, Name: "Support drafter", Purpose: "Draft support replies", Lifecycle: "runtime", Confidence: "high",
				Evidence: []RepositoryCitation{citation}, MemberObservationIDs: []string{"observation-1"},
				UnresolvedQuestions: []string{"Is this candidate reachable outside tests?"},
			}},
			AIUseFacts: []RepositoryAIUseFactSet{{
				AIUseID: useID,
				Facts: []RepositoryAIUseFact{{
					Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high",
					Rationale: "The executable handler invokes the model.", Evidence: []RepositoryCitation{citation},
				}},
				UnresolvedQuestions: []string{"Which deployed model serves this path?"},
			}},
			ObjectiveObservations: []RepositoryObjectiveObservation{{
				ObjectiveID: "OBJ-1", Strength: StrengthStrong, Confidence: "high",
				Rationale: "The model path is implemented.", SupportingEvidence: []RepositoryCitation{citation},
				ContradictoryEvidence: []RepositoryCitation{}, MissingEvidence: []string{}, UnresolvedQuestions: []string{},
			}},
			UnmappedObservations: []RepositoryUnmappedObservation{{
				Summary: "A separate model-adjacent hook needs review.", Reason: "It does not map to the supplied objective.",
				Confidence: "medium", Evidence: []RepositoryCitation{citation}, SuggestedReview: "Trace the hook's callers.",
			}},
			UnresolvedQuestions: []string{"Is the hook enabled in production?"},
		}
	}
	return build("subsystem", "source-use"), build("repository-synthesis", "synthesized-use")
}
