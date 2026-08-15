package cli

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/usemapping"
)

func TestBuildConfirmedAIUseReviewContextsSelectsApplicableChecks(t *testing.T) {
	mappings := &usemapping.Report{Uses: []usemapping.UseResult{{
		UseID: "support-replies", UseName: "Support replies", Description: "Draft replies", Paths: []string{"support/**"}, SystemIDs: []string{"support"},
		Frameworks: []usemapping.FrameworkResult{{ID: "eu-ai-act", Contexts: []usemapping.ContextResult{{
			Association: usemapping.Association{Status: usemapping.AssociationConfigured, SystemID: "support"},
			Objectives: []usemapping.ObjectiveResult{
				{ObjectiveResult: reconciliation.ObjectiveResult{ObjectiveID: "review", Requirement: reconciliation.RequirementLikelyRequired}},
				{ObjectiveResult: reconciliation.ObjectiveResult{ObjectiveID: "unknown", Requirement: reconciliation.RequirementUnresolved}},
			},
		}}}},
	}}}
	frameworks := []report.FrameworkResult{{
		ID: "eu-ai-act", TechnicalEvidence: framework.TechnicalEvidenceReport{Pack: framework.PackReference{ID: "eu-pack"}},
	}}

	contexts := buildConfirmedAIUseReviewContexts(mappings, frameworks)
	if len(contexts) != 1 || contexts[0].ID != "support-replies" || len(contexts[0].Objectives) != 1 {
		t.Fatalf("confirmed use contexts = %#v", contexts)
	}
	objective := contexts[0].Objectives[0]
	if objective.ObjectiveID != "eu-pack/review" || objective.SystemID != "support" || objective.Requirement != string(reconciliation.RequirementLikelyRequired) {
		t.Fatalf("review objective = %#v", objective)
	}
}

func TestBuildConfirmedAIUseReviewContextsKeepsFrameworkWideRecommendationWithoutSystem(t *testing.T) {
	mappings := &usemapping.Report{Uses: []usemapping.UseResult{{
		UseID: "assistant", UseName: "Assistant", Paths: []string{"assistant/**"},
		Frameworks: []usemapping.FrameworkResult{{ID: "nist-ai-rmf", Contexts: []usemapping.ContextResult{{
			Association: usemapping.Association{Status: usemapping.AssociationNone},
			Objectives: []usemapping.ObjectiveResult{{ObjectiveResult: reconciliation.ObjectiveResult{
				ObjectiveID: "monitor", Requirement: reconciliation.RequirementRecommended,
			}}},
		}}}},
	}}}
	contexts := buildConfirmedAIUseReviewContexts(mappings, []report.FrameworkResult{{
		ID: "nist-ai-rmf", TechnicalEvidence: framework.TechnicalEvidenceReport{Pack: framework.PackReference{ID: "nist-pack"}},
	}})
	if len(contexts) != 1 || len(contexts[0].Objectives) != 1 || contexts[0].Objectives[0].SystemID != "" || contexts[0].Objectives[0].ObjectiveID != "nist-pack/monitor" {
		t.Fatalf("voluntary use context = %#v", contexts)
	}
}
