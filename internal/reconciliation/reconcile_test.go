package reconciliation

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func candidateTechnical() framework.TechnicalEvidenceReport {
	return framework.TechnicalEvidenceReport{Pack: framework.PackReference{ID: framework.EUAIActTechnicalEvidencePackID, Version: "0.1.2"}, Objectives: []framework.ObjectiveAssessment{
		{ID: "eu-aia-14-human-review-gate", Title: "Human review gate", SourceReference: "Article 14", Applicability: framework.ObjectiveApplicability{LegalScope: framework.ApplicabilityHighRiskSystem}, Status: framework.ObjectiveCandidate, Matches: []framework.EvidenceMatch{{Fingerprint: "abc", Path: "review.go", StartLine: 12, Kind: "source"}}},
		{ID: "eu-aia-15-performance-thresholds", Title: "Performance thresholds", SourceReference: "Article 15", Applicability: framework.ObjectiveApplicability{LegalScope: framework.ApplicabilityHighRiskSystem}, Status: framework.ObjectiveNotDetected},
		{ID: "eu-aia-50-synthetic-content-marking", Title: "Synthetic content marking", SourceReference: "Article 50", Applicability: framework.ObjectiveApplicability{LegalScope: framework.ApplicabilityTransparencyObligation, ActivitiesAnyOf: []string{"synthetic-content"}}, Status: framework.ObjectiveCandidate, Matches: []framework.EvidenceMatch{{Fingerprint: "def", Path: "watermark.go", StartLine: 7, Kind: "source"}}},
	}}
}

func highRiskSystem() profile.System {
	system := profile.NewDraftSystem("ranking", "Candidate ranking")
	system.IntendedPurpose = "Rank candidates for recruiter review."
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.AIActivities = []profile.AIActivity{profile.ActivityInference, profile.ActivityAutomatedDecision}
	system.DeploymentModels = []profile.DeploymentModel{profile.DeploymentPrivateCustomer}
	return system
}

func TestBuildMapsRequirementsAndKeepsMismatchesVisible(t *testing.T) {
	system := highRiskSystem()
	technical := candidateTechnical()
	components := inventory.Report{Components: []inventory.Component{{Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high", Locations: []inventory.Location{{Path: "client.go", Line: 3, EvidenceType: inventory.EvidenceImport}}}}}
	report := Build([]profile.System{system}, profile.AssessEUAIAct([]profile.System{system}), technical, components)
	if report.Summary.RequirementWithEvidence != 1 || report.Summary.RequirementWithoutEvidence != 1 {
		t.Fatalf("unexpected requirement summary: %#v", report.Summary)
	}
	if report.Summary.EvidenceMismatches != 1 {
		t.Fatalf("synthetic evidence should conflict with undeclared activity: %#v", report.Summary)
	}
	if len(report.Systems[0].ObservedComponents) != 1 || report.Systems[0].ObservedComponents[0].Mapping != MappingAssociated {
		t.Fatalf("component not associated in single-system repository: %#v", report.Systems[0].ObservedComponents)
	}
	if len(report.Systems[0].Objectives[0].EvidenceReferences) != 1 {
		t.Fatalf("candidate evidence was not retained: %#v", report.Systems[0].Objectives[0])
	}
}

func TestBuildDoesNotGuessEvidenceOwnershipAcrossSystems(t *testing.T) {
	first := highRiskSystem()
	second := highRiskSystem()
	second.ID, second.Name = "support", "Support assistant"
	systems := []profile.System{first, second}
	report := Build(systems, profile.AssessEUAIAct(systems), candidateTechnical(), inventory.Report{})
	if len(report.Unmapped) != 2 {
		t.Fatalf("unmapped evidence = %#v", report.Unmapped)
	}
	for _, system := range report.Systems {
		if system.Objectives[0].Mapping != MappingUnassigned || len(system.Objectives[0].EvidenceReferences) != 0 {
			t.Fatalf("evidence was guessed for %s: %#v", system.SystemID, system.Objectives[0])
		}
	}
}

func TestBuildReportsEvidenceWithoutConfiguration(t *testing.T) {
	report := Build(nil, profile.AssessEUAIAct(nil), candidateTechnical(), inventory.Report{Components: []inventory.Component{{Name: "Ollama", Kind: inventory.KindProvider}}})
	if len(report.Systems) != 0 || len(report.Unmapped) != 3 {
		t.Fatalf("unexpected no-system reconciliation: %#v", report)
	}
	for _, evidence := range report.Unmapped {
		if evidence.Reason.Code != "no-declared-system" {
			t.Fatalf("unexpected reason: %#v", evidence)
		}
	}
}

func TestValidateCoverageRejectsUnknownObjective(t *testing.T) {
	report := candidateTechnical()
	if err := ValidateCoverage(report); err != nil {
		t.Fatal(err)
	}
	report.Objectives = append(report.Objectives, framework.ObjectiveAssessment{ID: "future-objective"})
	if err := ValidateCoverage(report); err == nil {
		t.Fatal("expected missing mapping error")
	}
}

func TestMappingCoversEmbeddedTechnicalPack(t *testing.T) {
	pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	report := framework.TechnicalEvidenceReport{Objectives: make([]framework.ObjectiveAssessment, 0, len(pack.Objectives))}
	for _, objective := range pack.Objectives {
		report.Objectives = append(report.Objectives, framework.ObjectiveAssessment{ID: objective.ID, Applicability: objective.Applicability})
	}
	if err := ValidateCoverage(report); err != nil {
		t.Fatal(err)
	}
}
