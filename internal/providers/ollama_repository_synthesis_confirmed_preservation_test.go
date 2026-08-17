package providers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestReviewRepositorySynthesisPreservesConfirmedFactsAndCitationlessUnmappedCount(t *testing.T) {
	citation := RepositoryCitation{Path: "confirmed.go", Line: 1, Summary: "The confirmed workflow exposes a model API."}
	localCitation := RepositoryCitation{Path: "confirmed.go", Line: 2, Summary: "The confirmed workflow also exposes a local command."}
	summary := RepositorySectionResult{
		Scope:  "subsystem",
		AIUses: []RepositoryAIUse{},
		AIUseFacts: []RepositoryAIUseFactSet{{
			AIUseID: "confirmed-use",
			Facts: []RepositoryAIUseFact{{
				Field: profile.CodeFactDeploymentModels, Values: []string{"api", "local-cli"}, Confidence: "high",
				Rationale: "The executable routes expose the model workflow through an API and local command.", Evidence: []RepositoryCitation{citation, localCitation},
			}},
			UnresolvedQuestions: []string{"Which deployment enables this route?"},
		}},
		ObjectiveObservations: []RepositoryObjectiveObservation{},
		UnmappedObservations: []RepositoryUnmappedObservation{
			{Summary: "First uncited context", Reason: "No exact line was available.", Confidence: "low", Evidence: []RepositoryCitation{}, SuggestedReview: "Trace the first context."},
			{Summary: "Second uncited context", Reason: "No exact line was available.", Confidence: "low", Evidence: []RepositoryCitation{}, SuggestedReview: "Trace the second context."},
		},
		UnresolvedQuestions: []string{},
	}

	for _, testCase := range []struct {
		name    string
		mutate  func(*RepositorySectionResult)
		wantErr string
	}{
		{name: "preserved"},
		{
			name: "confirmed positive fact field dropped",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].Facts = []RepositoryAIUseFact{}
			},
			wantErr: `dropped positive fact field "deployment-models" for confirmed AI use "confirmed-use"`,
		},
		{
			name: "confirmed fact question dropped",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].UnresolvedQuestions = []string{}
			},
			wantErr: `dropped unresolved fact context for confirmed AI use "confirmed-use"`,
		},
		{
			name: "confirmed positive fact value dropped",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].Facts[0].Values = []string{"api"}
			},
			wantErr: `dropped positive fact value "local-cli" for field "deployment-models" and confirmed AI use "confirmed-use"`,
		},
		{
			name: "confirmed checked fact citation dropped",
			mutate: func(result *RepositorySectionResult) {
				result.AIUseFacts[0].Facts[0].Evidence = []RepositoryCitation{citation}
			},
			wantErr: `dropped checked fact citation confirmed.go:2 for field "deployment-models" and confirmed AI use "confirmed-use"`,
		},
		{
			name: "citationless unmapped observation dropped",
			mutate: func(result *RepositorySectionResult) {
				result.UnmappedObservations = result.UnmappedObservations[:1]
			},
			wantErr: "omitted 1 citationless unmapped observation(s)",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output := cloneRepositorySectionResult(t, summary)
			output.Scope = "repository-synthesis"
			if testCase.mutate != nil {
				testCase.mutate(&output)
			}
			content, err := json.Marshal(repositoryAnalysisPayload{Result: output})
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
				FileIndex: []RepositoryFileReference{{Path: "confirmed.go", Kind: "source", LineCount: 2}},
				ConfirmedAIUses: []RepositoryConfirmedAIUse{{
					ID: "confirmed-use", Name: "Confirmed workflow", SubmittedFiles: []string{"confirmed.go"},
				}},
				SubsystemSummaries: []RepositorySectionResult{summary},
			})
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("fully preserved synthesis was rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func cloneRepositorySectionResult(t *testing.T, value RepositorySectionResult) RepositorySectionResult {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var result RepositorySectionResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
