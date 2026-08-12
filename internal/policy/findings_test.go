package policy

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

func TestTechnicalGapFindingsRequireReviewedLikelyRequirement(t *testing.T) {
	system := profile.NewDraftSystem("ranking", "Ranking")
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{{
		SystemID: "ranking", SystemName: "Ranking",
		Objectives: []reconciliation.ObjectiveResult{{
			ObjectiveID: "eu-aia-14-human-review-gate", Title: "Human review gate", SourceReference: "Article 14",
			Requirement: reconciliation.RequirementLikelyRequired, Mapping: reconciliation.MappingRequirementWithoutEvidence,
		}},
	}}}
	if findings := TechnicalGapFindings("eu-ai-act", ".complyscan.yml", []profile.System{system}, mapping); len(findings) != 0 {
		t.Fatalf("draft profile produced blocking findings: %#v", findings)
	}
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Product owner", ReviewedAt: "2026-08-12"}
	findings := TechnicalGapFindings("eu-ai-act", ".complyscan.yml", []profile.System{system}, mapping)
	if len(findings) != 1 || findings[0].RuleID != TechnicalGapRuleID || len(findings[0].Fingerprint) != 64 || findings[0].Path != ".complyscan.yml" {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestTechnicalGapFindingsDoNotGateRecommendationsOrUnresolvedMappings(t *testing.T) {
	system := profile.NewDraftSystem("assistant", "Assistant")
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Owner", ReviewedAt: "2026-08-12"}
	mapping := reconciliation.Report{Systems: []reconciliation.SystemResult{{
		SystemID: "assistant", SystemName: "Assistant",
		Objectives: []reconciliation.ObjectiveResult{
			{ObjectiveID: "recommended", Requirement: reconciliation.RequirementRecommended, Mapping: reconciliation.MappingRecommendedWithoutEvidence},
			{ObjectiveID: "unknown", Requirement: reconciliation.RequirementUnresolved, Mapping: reconciliation.MappingApplicabilityUnresolved},
		},
	}}}
	if findings := TechnicalGapFindings("nist-ai-rmf", ".complyscan.yml", []profile.System{system}, mapping); len(findings) != 0 {
		t.Fatalf("non-required mappings produced blocking findings: %#v", findings)
	}
}
