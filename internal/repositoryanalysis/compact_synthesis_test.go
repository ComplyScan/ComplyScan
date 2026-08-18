package repositoryanalysis

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type groupingOnlySynthesisReviewer struct {
	mu                sync.Mutex
	sourceCalls       int
	synthesisCalls    int
	compactInputValid bool
}

func (reviewer *groupingOnlySynthesisReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	section := emptyRepositoryGroupingResult(request.Scope)
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		reviewer.synthesisCalls++
		reviewer.compactInputValid = request.CompactSynthesis && len(request.Objectives) == 0 && len(request.Systems) == 0 && len(request.ConfirmedAIUses) == 0 && len(request.FileIndex) == 0
		members := make([]string, 0)
		for _, summary := range request.SubsystemSummaries {
			if len(summary.ObjectiveObservations) != 0 || len(summary.UnmappedObservations) != 0 || len(summary.UnresolvedQuestions) != 0 {
				reviewer.compactInputValid = false
			}
			for _, use := range summary.AIUses {
				members = append(members, use.MemberObservationIDs...)
				if len(use.Evidence) > 2 {
					reviewer.compactInputValid = false
				}
			}
			for _, set := range summary.AIUseFacts {
				for _, fact := range set.Facts {
					if fact.Rationale != "" || len(fact.Evidence) != 0 {
						reviewer.compactInputValid = false
					}
				}
			}
		}
		section.AIUses = []providers.RepositoryAIUse{{
			ID: "temporary-group", Name: "Support reply drafting", Purpose: "Draft replies requested by the support route",
			Lifecycle: "development", Confidence: "high", MemberObservationIDs: members,
		}}
		for _, summary := range request.SubsystemSummaries {
			for _, gap := range summary.EvidenceGaps {
				if gap.Text != "The provider call is outside this source batch." {
					continue
				}
				for _, resolverSummary := range request.SubsystemSummaries {
					for _, use := range resolverSummary.AIUses {
						if len(use.MemberObservationIDs) == 0 || len(use.Evidence) == 0 || use.MemberObservationIDs[0] == gap.OriginObservationIDs[0] {
							continue
						}
						section.ResolvedEvidenceGaps = append(section.ResolvedEvidenceGaps, providers.RepositoryResolvedEvidenceGap{
							GapID: gap.ID, ResolvingObservationIDs: []string{use.MemberObservationIDs[0]}, Evidence: []providers.RepositoryCitation{use.Evidence[0]},
							Reason: "The connected source member contains the provider call.",
						})
						break
					}
				}
			}
		}
	} else {
		index := reviewer.sourceCalls
		reviewer.sourceCalls++
		file := request.Files[0]
		citation := providers.RepositoryCitation{Path: file.Path, Line: file.ContentStartLine, Summary: fmt.Sprintf("Validated model workflow part %d.", index+1)}
		section.AIUses = []providers.RepositoryAIUse{{
			ID: "local-use", Name: "Workflow part", Purpose: "One part of support reply drafting", Lifecycle: "development", Confidence: "medium",
			Evidence: []providers.RepositoryCitation{citation}, UnresolvedQuestions: []string{fmt.Sprintf("Use question %d", index+1)},
		}}
		field := profile.CodeFactAIActivities
		value := "inference"
		if index == 1 {
			field = profile.CodeFactHumanOversight
			value = "available"
		}
		section.AIUseFacts = []providers.RepositoryAIUseFactSet{{
			AIUseID:             "local-use",
			Facts:               []providers.RepositoryAIUseFact{{Field: field, Values: []string{value}, Confidence: "high", Rationale: "Directly shown by the submitted workflow.", Evidence: []providers.RepositoryCitation{citation}}},
			UnresolvedQuestions: []string{},
		}}
		section.ObjectiveObservations = []providers.RepositoryObjectiveObservation{{
			ObjectiveID: "OBJ", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "The submitted flow contains direct evidence.",
			SupportingEvidence: []providers.RepositoryCitation{citation}, ContradictoryEvidence: []providers.RepositoryCitation{}, MissingEvidence: []string{}, UnresolvedQuestions: []string{},
		}}
		if index == 0 {
			section.ObjectiveObservations[0].MissingEvidence = []string{"The provider call is outside this source batch."}
		}
		section.UnmappedObservations = []providers.RepositoryUnmappedObservation{{
			Summary: fmt.Sprintf("Unmapped signal %d", index+1), Reason: "Needs product context", Confidence: "medium", Evidence: []providers.RepositoryCitation{citation}, SuggestedReview: "Review deployment context",
		}}
		section.UnresolvedQuestions = []string{fmt.Sprintf("Repository question %d", index+1)}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: section,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files)},
		Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func TestCompactSynthesisGroupsOnceAndReattachesValidatedEvidenceLocally(t *testing.T) {
	content := []byte("package ai\n// " + strings.Repeat("bounded implementation evidence ", 180) + "\n")
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		{Path: "route/use.go", Kind: discovery.KindSource, Content: content, Size: int64(len(content))},
		{Path: "model/use.go", Kind: discovery.KindSource, Content: content, Size: int64(len(content))},
	}}
	reviewer := &groupingOnlySynthesisReviewer{}
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Build(repository), repositoryFiles(repository), []providers.RepositoryObjective{{ID: "OBJ", Title: "Review flow"}}, nil, nil, 8_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true, MaxInputTokens: DefaultRemoteInputTokens,
		InitialRateLimits: providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 100, RemainingRequests: 100, TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 1_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reviewer.sourceCalls != 2 || reviewer.synthesisCalls != 1 || !reviewer.compactInputValid {
		t.Fatalf("pipeline calls/compact input = source:%d synthesis:%d valid:%t", reviewer.sourceCalls, reviewer.synthesisCalls, reviewer.compactInputValid)
	}
	if len(result.Result.AIUses) != 1 || len(result.Result.AIUses[0].MemberObservationIDs) != 2 || len(result.Result.AIUses[0].Evidence) != 2 {
		t.Fatalf("hydrated grouped AI use = %#v", result.Result.AIUses)
	}
	if len(result.Result.AIUseFacts) != 1 || len(result.Result.AIUseFacts[0].Facts) != 2 {
		t.Fatalf("validated facts were not reattached: %#v", result.Result.AIUseFacts)
	}
	if len(result.Result.ObjectiveObservations) != 1 || len(result.Result.ObjectiveObservations[0].SupportingEvidence) != 2 {
		t.Fatalf("validated objective evidence was not reattached: %#v", result.Result.ObjectiveObservations)
	}
	if len(result.Result.ObjectiveObservations[0].MissingEvidence) != 0 || len(result.Result.ResolvedEvidenceGaps) != 1 {
		t.Fatalf("cross-batch resolution was not validated and applied: observation=%#v resolved=%#v", result.Result.ObjectiveObservations[0], result.Result.ResolvedEvidenceGaps)
	}
	if len(result.Result.UnmappedObservations) != 2 || len(result.Result.UnresolvedQuestions) != 2 {
		t.Fatalf("validated residual evidence was not reattached: unmapped=%#v questions=%#v", result.Result.UnmappedObservations, result.Result.UnresolvedQuestions)
	}
	if result.Result.AIUses[0].ID != inferredCandidateID(result.Result.AIUses[0].MemberObservationIDs) {
		t.Fatalf("final ID was not derived locally from model grouping: %#v", result.Result.AIUses[0])
	}
}

func TestFinalEvidenceAssemblyRetainsMoreFactValuesThanOneModelResponseCanEmit(t *testing.T) {
	sets := make([]providers.RepositoryAIUseFactSet, 0, 9)
	for index := 0; index < 9; index++ {
		citation := providers.RepositoryCitation{Path: fmt.Sprintf("part-%d.go", index+1), Line: 1, Summary: "Checked source evidence."}
		sets = append(sets, providers.RepositoryAIUseFactSet{
			AIUseID: "local-use",
			Facts: []providers.RepositoryAIUseFact{{
				Field: profile.CodeFactIntendedPurpose, Values: []string{fmt.Sprintf("Purpose %d", index+1)},
				Confidence: "medium", Rationale: "Directly shown by the source batch.", Evidence: []providers.RepositoryCitation{citation},
			}},
		})
	}

	merged, err := mergeRepositoryFactSets("grouped-use", sets, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Facts) != 1 || len(merged.Facts[0].Values) != 9 || len(merged.Facts[0].Evidence) != 9 {
		t.Fatalf("final validated fact union = %#v", merged)
	}

	compact, err := mergeRepositoryFactSets("grouped-use", sets, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact.Facts) != 1 || len(compact.Facts[0].Values) != 8 || len(compact.Facts[0].Evidence) != 0 {
		t.Fatalf("compact grouping hint = %#v", compact)
	}
}
