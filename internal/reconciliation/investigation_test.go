package reconciliation

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/ownership"
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

func TestAttachTechnicalInvestigationsFollowsOwnedEvidenceFingerprint(t *testing.T) {
	report := Report{Systems: []SystemResult{
		{SystemID: "ranking", Objectives: []ObjectiveResult{{
			ObjectiveID: "objective", EvidenceReferences: []EvidenceReference{{Fingerprint: "ranking-fingerprint", Path: "ranking/review.go", Ownership: ownership.StatusAssigned, Systems: []string{"ranking"}}},
		}}},
		{SystemID: "support", Objectives: []ObjectiveResult{{
			ObjectiveID: "objective", EvidenceReferences: []EvidenceReference{{Fingerprint: "support-fingerprint", Path: "support/review.go", Ownership: ownership.StatusAssigned, Systems: []string{"support"}}},
		}}},
	}}
	review := providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{
		{SystemID: "ranking", ObjectiveID: "objective", EvidenceFingerprint: "ranking-fingerprint", InvestigationMode: "candidate-validation", Assurance: providers.AssuranceStructurallyVerified},
		{SystemID: "support", ObjectiveID: "objective", EvidenceFingerprint: "support-fingerprint", InvestigationMode: "candidate-validation", Assurance: providers.AssuranceSignalDetected},
	}}
	AttachTechnicalInvestigations(&report, review)
	if got := report.Systems[0].Objectives[0].Investigation; got == nil || got.Assurance != providers.AssuranceStructurallyVerified || got.Observations != 1 {
		t.Fatalf("ranking investigation = %#v", got)
	}
	if got := report.Systems[1].Objectives[0].Investigation; got == nil || got.Assurance != providers.AssuranceSignalDetected || got.Observations != 1 {
		t.Fatalf("support investigation = %#v", got)
	}
}

func TestAttachExtendedInvestigationOnlyToItsSystem(t *testing.T) {
	report := Report{Systems: []SystemResult{
		{SystemID: "ranking", Objectives: []ObjectiveResult{{ObjectiveID: "objective"}}},
		{SystemID: "support", Objectives: []ObjectiveResult{{ObjectiveID: "objective"}}},
	}}
	review := providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{{
		SystemID: "ranking", OwnershipScope: "explicit", RepositoryFiles: 12,
		ObjectiveID: "objective", EvidenceFingerprint: "search-fingerprint", InvestigationMode: "extended-search",
		Assurance: providers.AssuranceInvestigationNoEvidence,
	}}}
	AttachTechnicalInvestigations(&report, review)
	if got := report.Systems[0].Objectives[0].Investigation; got == nil || got.SystemID != "ranking" || got.RepositoryFiles != 12 {
		t.Fatalf("ranking investigation = %#v", got)
	}
	if report.Systems[1].Objectives[0].Investigation != nil {
		t.Fatalf("ranking investigation escaped into support: %#v", report.Systems[1].Objectives[0].Investigation)
	}
}
