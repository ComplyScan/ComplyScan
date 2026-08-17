package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestReviewRepositorySynthesisPreservesEveryMergedCandidateCitation(t *testing.T) {
	a := RepositoryCitation{Path: "a.go", Line: 1, Summary: "The route starts the AI workflow."}
	b := RepositoryCitation{Path: "b.go", Line: 1, Summary: "The model client completes the AI workflow."}
	summaries := []RepositorySectionResult{
		repositoryCandidateSummary("source-a", "observation-a", a, nil),
		repositoryCandidateSummary("source-b", "observation-b", b, nil),
	}
	output := repositoryMergedCandidateResult([]RepositoryCitation{a}, nil)

	err := runRepositorySynthesisResult(t, summaries, output, []RepositoryFileReference{
		{Path: "a.go", Kind: "source", LineCount: 1},
		{Path: "b.go", Kind: "source", LineCount: 1},
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "dropped checked AI-use citation b.go:1") {
		t.Fatalf("error = %v, want per-input candidate citation preservation failure", err)
	}
}

func TestReviewRepositorySynthesisPreservesCandidateFactValuesAndEvidence(t *testing.T) {
	a := RepositoryCitation{Path: "a.go", Line: 1, Summary: "The runtime invokes inference."}
	b := RepositoryCitation{Path: "b.go", Line: 1, Summary: "The runtime invokes evaluation."}
	summaries := []RepositorySectionResult{
		repositoryCandidateSummary("source-a", "observation-a", a, &RepositoryAIUseFact{
			Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high", Rationale: "Inference is invoked.", Evidence: []RepositoryCitation{a},
		}),
		repositoryCandidateSummary("source-b", "observation-b", b, &RepositoryAIUseFact{
			Field: profile.CodeFactAIActivities, Values: []string{"evaluation"}, Confidence: "high", Rationale: "Evaluation is invoked.", Evidence: []RepositoryCitation{b},
		}),
	}
	files := []RepositoryFileReference{{Path: "a.go", Kind: "source", LineCount: 1}, {Path: "b.go", Kind: "source", LineCount: 1}}

	for _, testCase := range []struct {
		name     string
		values   []string
		evidence []RepositoryCitation
		wantErr  string
	}{
		{name: "preserved", values: []string{"inference", "evaluation"}, evidence: []RepositoryCitation{a, b}},
		{name: "value dropped", values: []string{"inference"}, evidence: []RepositoryCitation{a, b}, wantErr: `dropped positive fact value "evaluation"`},
		{name: "evidence dropped", values: []string{"inference", "evaluation"}, evidence: []RepositoryCitation{a}, wantErr: "dropped checked fact citation b.go:1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fact := RepositoryAIUseFact{
				Field: profile.CodeFactAIActivities, Values: testCase.values, Confidence: "high",
				Rationale: "The merged workflow invokes both activities.", Evidence: testCase.evidence,
			}
			output := repositoryMergedCandidateResult([]RepositoryCitation{a, b}, &fact)
			err := runRepositorySynthesisResult(t, summaries, output, files, nil, nil)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("preserved facts rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestReviewRepositorySynthesisPreservesGenericObjectiveEvidenceSemantics(t *testing.T) {
	supporting := RepositoryCitation{Path: "support.go", Line: 1, Summary: "The implementation calls the safeguard."}
	contradictory := RepositoryCitation{Path: "bypass.go", Line: 1, Summary: "A bypass skips the safeguard."}
	input := RepositorySectionResult{
		Scope: "source", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{},
		ObjectiveObservations: []RepositoryObjectiveObservation{{
			ObjectiveID: "OBJ-1", Strength: StrengthPartial, Confidence: "high", Rationale: "A safeguard and bypass coexist.",
			SupportingEvidence: []RepositoryCitation{supporting}, ContradictoryEvidence: []RepositoryCitation{contradictory},
			MissingEvidence: []string{"Bypass prevention"}, UnresolvedQuestions: []string{"Is the bypass reachable?"},
		}},
		UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	base := cloneRepositorySectionResult(t, input)
	base.Scope = "synthesis"
	files := []RepositoryFileReference{{Path: "support.go", Kind: "source", LineCount: 1}, {Path: "bypass.go", Kind: "source", LineCount: 1}}
	objectives := []RepositoryObjective{{ID: "OBJ-1", Title: "Safeguard"}}

	for _, testCase := range []struct {
		name    string
		mutate  func(*RepositoryObjectiveObservation)
		wantErr string
	}{
		{name: "preserved"},
		{
			name: "support reclassified as contradiction",
			mutate: func(value *RepositoryObjectiveObservation) {
				value.SupportingEvidence = []RepositoryCitation{contradictory}
				value.ContradictoryEvidence = []RepositoryCitation{supporting, contradictory}
			},
			wantErr: "dropped supporting evidence support.go:1",
		},
		{name: "contradiction dropped", mutate: func(value *RepositoryObjectiveObservation) { value.ContradictoryEvidence = nil }, wantErr: "dropped contradictory evidence bypass.go:1"},
		{name: "missing context dropped", mutate: func(value *RepositoryObjectiveObservation) { value.MissingEvidence = nil }, wantErr: "dropped missing-evidence context"},
		{name: "question dropped", mutate: func(value *RepositoryObjectiveObservation) { value.UnresolvedQuestions = nil }, wantErr: "dropped unresolved context"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			output := cloneRepositorySectionResult(t, base)
			if testCase.mutate != nil {
				testCase.mutate(&output.ObjectiveObservations[0])
			}
			err := runRepositorySynthesisResult(t, []RepositorySectionResult{input}, output, files, objectives, nil)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("preserved generic observation rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestReviewRepositoryRejectsUncitedDefiniteGenericObjective(t *testing.T) {
	result := RepositorySectionResult{
		Scope: ".", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{},
		ObjectiveObservations: []RepositoryObjectiveObservation{{
			ObjectiveID: "OBJ-1", Strength: StrengthNotSupported, Confidence: "high", Rationale: "The safeguard is not supported.",
			SupportingEvidence: []RepositoryCitation{}, ContradictoryEvidence: []RepositoryCitation{}, MissingEvidence: []string{}, UnresolvedQuestions: []string{},
		}},
		UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	content, err := json.Marshal(repositoryAnalysisPayload{Result: result})
	if err != nil {
		t.Fatal(err)
	}
	provider := repositoryResultProvider(string(content), nil)
	_, err = provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisSubsystem, Scope: ".", RepositoryFiles: 1,
		Files:      []RepositorySourceFile{{Path: "main.go", Kind: "source", Content: "package main\n"}},
		Objectives: []RepositoryObjective{{ID: "OBJ-1", Title: "Safeguard"}},
	})
	if err == nil || !strings.Contains(err.Error(), "definite strength without a checked citation") {
		t.Fatalf("error = %v, want uncited definite generic objective rejection", err)
	}
}

func TestRepositorySynthesisRepresentationCeilingsFailBeforeProviderCall(t *testing.T) {
	t.Run("member observations", func(t *testing.T) {
		members := make([]string, maxRepositoryUses+1)
		for index := range members {
			members[index] = fmt.Sprintf("observation-%03d", index)
		}
		citation := RepositoryCitation{Path: "main.go", Line: 1, Summary: "The workflow invokes a model."}
		summary := repositoryCandidateSummary("source", "temporary", citation, nil)
		summary.AIUses[0].MemberObservationIDs = members
		calls := 0
		provider := repositoryResultProvider("", &calls)
		_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
			Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: 1,
			FileIndex: []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 1}}, SubsystemSummaries: []RepositorySectionResult{summary},
		})
		_, representation := AsRepositoryRepresentationError(err)
		if err == nil || !representation || !strings.Contains(err.Error(), "maximum representable in one validated group is 100") || calls != 0 {
			t.Fatalf("error = %v, calls = %d; want explicit preflight representation error", err, calls)
		}
	})

	t.Run("distinct citationless unmapped observations", func(t *testing.T) {
		summaries := repositoryCitationlessUnmappedSummaries(101, false)
		calls := 0
		provider := repositoryResultProvider("", &calls)
		_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
			Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: 1,
			FileIndex: []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 1}}, SubsystemSummaries: summaries,
		})
		_, representation := AsRepositoryRepresentationError(err)
		if err == nil || !representation || !strings.Contains(err.Error(), "101 distinct citationless unmapped observations") || calls != 0 {
			t.Fatalf("error = %v, calls = %d; want explicit preflight representation error", err, calls)
		}
	})

	t.Run("confirmed fact value union", func(t *testing.T) {
		citation := RepositoryCitation{Path: "main.go", Line: 1, Summary: "The confirmed workflow names its users."}
		factSet := func(values ...string) RepositorySectionResult {
			return RepositorySectionResult{
				Scope: "source", AIUses: []RepositoryAIUse{},
				AIUseFacts: []RepositoryAIUseFactSet{{AIUseID: "confirmed", Facts: []RepositoryAIUseFact{{
					Field: profile.CodeFactUsers, Values: values, Confidence: "high", Rationale: "The workflow names these users.", Evidence: []RepositoryCitation{citation},
				}}, UnresolvedQuestions: []string{}}},
				ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
			}
		}
		calls := 0
		provider := repositoryResultProvider("", &calls)
		_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
			Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: 1,
			FileIndex:       []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 1}},
			ConfirmedAIUses: []RepositoryConfirmedAIUse{{ID: "confirmed", Name: "Confirmed", SubmittedFiles: []string{"main.go"}}},
			SubsystemSummaries: []RepositorySectionResult{
				factSet("user-0", "user-1", "user-2", "user-3", "user-4"),
				factSet("user-4", "user-5", "user-6", "user-7", "user-8"),
			},
		})
		_, representation := AsRepositoryRepresentationError(err)
		if err == nil || !representation || !strings.Contains(err.Error(), "exceed one validated fact's representation limits") || calls != 0 {
			t.Fatalf("error = %v, calls = %d; want typed confirmed-fact representation error", err, calls)
		}
	})

	t.Run("generic evidence union", func(t *testing.T) {
		observation := func(start int) RepositorySectionResult {
			citations := make([]RepositoryCitation, 11)
			for index := range citations {
				line := start + index
				citations[index] = RepositoryCitation{Path: "main.go", Line: line, Summary: fmt.Sprintf("Checked evidence %d.", line)}
			}
			return RepositorySectionResult{
				Scope: "source", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{},
				ObjectiveObservations: []RepositoryObjectiveObservation{{
					ObjectiveID: "OBJ-1", Strength: StrengthPartial, Confidence: "high", Rationale: "The objective has partial evidence.",
					SupportingEvidence: citations, ContradictoryEvidence: []RepositoryCitation{}, MissingEvidence: []string{"Remaining connection"}, UnresolvedQuestions: []string{},
				}},
				UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
			}
		}
		calls := 0
		provider := repositoryResultProvider("", &calls)
		_, err := provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
			Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: 1,
			FileIndex:          []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 22}},
			Objectives:         []RepositoryObjective{{ID: "OBJ-1", Title: "Objective"}},
			SubsystemSummaries: []RepositorySectionResult{observation(1), observation(12)},
		})
		_, representation := AsRepositoryRepresentationError(err)
		if err == nil || !representation || !strings.Contains(err.Error(), "more checked evidence than one validated observation can represent") || calls != 0 {
			t.Fatalf("error = %v, calls = %d; want typed generic-evidence representation error", err, calls)
		}
	})
}

func TestRepositoryRepresentationErrorSupportsWrappedMatching(t *testing.T) {
	cause := errors.New("representation ceiling")
	original := &RepositoryRepresentationError{Diagnostic: "cannot represent validated synthesis input", cause: cause}
	wrapped := fmt.Errorf("outer orchestration context: %w", original)
	matched, ok := AsRepositoryRepresentationError(wrapped)
	if !ok || matched != original {
		t.Fatalf("AsRepositoryRepresentationError() = %#v, %t; want original typed error", matched, ok)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("RepositoryRepresentationError did not preserve its cause")
	}
	if _, ok := AsRepositoryRepresentationError(errors.New("ordinary error")); ok {
		t.Fatal("ordinary error was classified as a representation error")
	}
}

func TestRepositorySynthesisDeduplicatesCitationlessUnmappedRequirements(t *testing.T) {
	summaries := repositoryCitationlessUnmappedSummaries(101, true)
	output := RepositorySectionResult{
		Scope: "synthesis", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{}, ObjectiveObservations: []RepositoryObjectiveObservation{},
		UnmappedObservations: []RepositoryUnmappedObservation{{Summary: "Repeated context", Reason: "Needs separate review.", Confidence: "low", Evidence: []RepositoryCitation{}, SuggestedReview: "Trace callers."}},
		UnresolvedQuestions:  []string{},
	}
	err := runRepositorySynthesisResult(t, summaries, output, []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 1}}, nil, nil)
	if err != nil {
		t.Fatalf("identity-safe citationless deduplication rejected: %v", err)
	}
}

func TestRepositorySynthesisDoesNotCountDuplicateCitationlessOutputsAsDistinct(t *testing.T) {
	summaries := repositoryCitationlessUnmappedSummaries(2, false)
	duplicate := RepositoryUnmappedObservation{Summary: "Merged context", Reason: "Needs separate review.", Confidence: "low", Evidence: []RepositoryCitation{}, SuggestedReview: "Trace callers."}
	output := RepositorySectionResult{
		Scope: "synthesis", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{}, ObjectiveObservations: []RepositoryObjectiveObservation{},
		UnmappedObservations: []RepositoryUnmappedObservation{duplicate, duplicate}, UnresolvedQuestions: []string{},
	}
	err := runRepositorySynthesisResult(t, summaries, output, []RepositoryFileReference{{Path: "main.go", Kind: "source", LineCount: 1}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "omitted 1 citationless unmapped observation") {
		t.Fatalf("error = %v, want duplicate output identities not to satisfy distinct preservation requirements", err)
	}
}

func repositoryCandidateSummary(id, observationID string, citation RepositoryCitation, fact *RepositoryAIUseFact) RepositorySectionResult {
	facts := []RepositoryAIUseFact{}
	if fact != nil {
		facts = append(facts, *fact)
	}
	return RepositorySectionResult{
		Scope: "source",
		AIUses: []RepositoryAIUse{{
			ID: id, Name: id, Purpose: "Process a request with AI", Lifecycle: "runtime", Confidence: "high",
			Evidence: []RepositoryCitation{citation}, MemberObservationIDs: []string{observationID}, UnresolvedQuestions: []string{},
		}},
		AIUseFacts:            []RepositoryAIUseFactSet{{AIUseID: id, Facts: facts, UnresolvedQuestions: []string{}}},
		ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
}

func repositoryMergedCandidateResult(evidence []RepositoryCitation, fact *RepositoryAIUseFact) RepositorySectionResult {
	facts := []RepositoryAIUseFact{}
	if fact != nil {
		facts = append(facts, *fact)
	}
	return RepositorySectionResult{
		Scope: "synthesis",
		AIUses: []RepositoryAIUse{{
			ID: "merged", Name: "Merged workflow", Purpose: "Process a request with AI", Lifecycle: "runtime", Confidence: "high",
			Evidence: evidence, MemberObservationIDs: []string{"observation-a", "observation-b"}, UnresolvedQuestions: []string{},
		}},
		AIUseFacts:            []RepositoryAIUseFactSet{{AIUseID: "merged", Facts: facts, UnresolvedQuestions: []string{}}},
		ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
}

func runRepositorySynthesisResult(t *testing.T, summaries []RepositorySectionResult, output RepositorySectionResult, files []RepositoryFileReference, objectives []RepositoryObjective, confirmed []RepositoryConfirmedAIUse) error {
	t.Helper()
	content, err := json.Marshal(repositoryAnalysisPayload{Result: output})
	if err != nil {
		t.Fatal(err)
	}
	provider := repositoryResultProvider(string(content), nil)
	_, err = provider.ReviewRepository(context.Background(), RepositoryAnalysisRequest{
		Mode: RepositoryAnalysisSynthesis, Scope: "synthesis", RepositoryFiles: len(files), FileIndex: files,
		Objectives: objectives, ConfirmedAIUses: confirmed, SubsystemSummaries: summaries,
	})
	return err
}

func repositoryResultProvider(content string, calls *int) *OllamaProvider {
	return &OllamaProvider{
		kind: OpenAI, label: "OpenAI", model: "test-model",
		completion: func(context.Context, ollamaChatRequest) (ollamaChatResponse, error) {
			if calls != nil {
				*calls++
			}
			var response ollamaChatResponse
			response.Done = true
			response.Message.Content = content
			return response, nil
		},
	}
}

func repositoryCitationlessUnmappedSummaries(count int, duplicate bool) []RepositorySectionResult {
	summaries := []RepositorySectionResult{
		{Scope: "one", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{}, ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
		{Scope: "two", AIUses: []RepositoryAIUse{}, AIUseFacts: []RepositoryAIUseFactSet{}, ObjectiveObservations: []RepositoryObjectiveObservation{}, UnmappedObservations: []RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
	}
	for index := 0; index < count; index++ {
		summary := "Repeated context"
		if !duplicate {
			summary = fmt.Sprintf("Distinct context %03d", index)
		}
		observation := RepositoryUnmappedObservation{Summary: summary, Reason: "Needs separate review.", Confidence: "low", Evidence: []RepositoryCitation{}, SuggestedReview: "Trace callers."}
		summaries[index%len(summaries)].UnmappedObservations = append(summaries[index%len(summaries)].UnmappedObservations, observation)
	}
	return summaries
}
