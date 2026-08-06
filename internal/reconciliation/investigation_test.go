package reconciliation

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestAttachTechnicalInvestigationsPreservesMappingAndSelectsStrongestAssurance(t *testing.T) {
	report := Report{Systems: []SystemResult{{Objectives: []ObjectiveResult{{
		ObjectiveID: "objective", Requirement: RequirementLikelyRequired, Mapping: MappingRequirementWithEvidence,
	}}}}}
	review := providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{
		{ObjectiveID: "objective", Conclusion: providers.ConclusionPartial, Assurance: providers.AssuranceSignalDetected, Confidence: "low", SupportingEvidence: []providers.TechnicalEvidenceClaim{{Path: "test.go", Summary: "Test signal"}}},
		{ObjectiveID: "objective", Conclusion: providers.ConclusionSubstantiated, Assurance: providers.AssuranceStructurallyVerified, Confidence: "high", SupportingEvidence: []providers.TechnicalEvidenceClaim{{Path: "main.go", Summary: "Production path"}}, RuntimeVerificationRequired: true, LegalReviewRequired: true},
	}}
	AttachTechnicalInvestigations(&report, review)
	objective := report.Systems[0].Objectives[0]
	if objective.Mapping != MappingRequirementWithEvidence || objective.Requirement != RequirementLikelyRequired {
		t.Fatalf("deterministic mapping changed: %#v", objective)
	}
	if objective.Investigation == nil || objective.Investigation.Assurance != providers.AssuranceStructurallyVerified || objective.Investigation.Observations != 2 || objective.Investigation.SupportingEvidence != 2 {
		t.Fatalf("unexpected investigation summary: %#v", objective.Investigation)
	}
	if report.Summary.StructurallyVerified != 1 {
		t.Fatalf("unexpected report summary: %#v", report.Summary)
	}
}
