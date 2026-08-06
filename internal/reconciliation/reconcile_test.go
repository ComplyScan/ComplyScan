package reconciliation

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
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
	report := Build([]profile.System{system}, profile.AssessEUAIAct([]profile.System{system}), technical, components, nil)
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
	if report.Systems[0].Objectives[0].EvidenceReferences[0].Ownership != ownership.StatusInferred {
		t.Fatalf("single-system inference was not disclosed: %#v", report.Systems[0].Objectives[0].EvidenceReferences)
	}
}

func TestBuildUsesPackActivityConditionsInsteadOfObjectiveIDRules(t *testing.T) {
	system := highRiskSystem()
	technical := candidateTechnical()
	technical.Objectives = technical.Objectives[:1]
	technical.Objectives[0].Applicability.ActivitiesAnyOf = []string{"synthetic-content"}
	report := Build([]profile.System{system}, profile.AssessEUAIAct([]profile.System{system}), technical, inventory.Report{}, nil)
	if report.Systems[0].Objectives[0].Mapping != MappingEvidenceMismatch {
		t.Fatalf("pack activity condition was ignored: %#v", report.Systems[0].Objectives[0])
	}
	technical.Objectives[0].Applicability.ActivitiesAnyOf = []string{"inference"}
	report = Build([]profile.System{system}, profile.AssessEUAIAct([]profile.System{system}), technical, inventory.Report{}, nil)
	if report.Systems[0].Objectives[0].Mapping != MappingRequirementWithEvidence {
		t.Fatalf("updated pack activity condition was ignored: %#v", report.Systems[0].Objectives[0])
	}
}

func TestBuildDoesNotGuessEvidenceOwnershipAcrossSystems(t *testing.T) {
	first := highRiskSystem()
	second := highRiskSystem()
	second.ID, second.Name = "support", "Support assistant"
	systems := []profile.System{first, second}
	report := Build(systems, profile.AssessEUAIAct(systems), candidateTechnical(), inventory.Report{}, nil)
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
	report := Build(nil, profile.AssessEUAIAct(nil), candidateTechnical(), inventory.Report{Components: []inventory.Component{{Name: "Ollama", Kind: inventory.KindProvider}}}, nil)
	if len(report.Systems) != 0 || len(report.Unmapped) != 3 {
		t.Fatalf("unexpected no-system reconciliation: %#v", report)
	}
	for _, evidence := range report.Unmapped {
		if evidence.Reason.Code != "no-declared-system" {
			t.Fatalf("unexpected reason: %#v", evidence)
		}
	}
}

func TestBuildAssignsEvidenceUsingExplicitPathOwnership(t *testing.T) {
	ranking := highRiskSystem()
	support := highRiskSystem()
	support.ID, support.Name = "support", "Support assistant"
	systems := []profile.System{ranking, support}
	rules := []ownership.Rule{
		{Paths: []string{"review.go"}, Systems: []string{"ranking"}},
		{Paths: []string{"watermark.go"}, Systems: []string{"ranking", "support"}},
		{Paths: []string{"clients/**"}, Systems: []string{"support"}},
	}
	components := inventory.Report{Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high",
		Locations: []inventory.Location{{Path: "clients/openai.go", Line: 3, EvidenceType: inventory.EvidenceImport}},
	}}}

	report := Build(systems, profile.AssessEUAIAct(systems), candidateTechnical(), components, rules)
	if !report.Ownership.Configured || len(report.Ownership.Rules) != 3 {
		t.Fatalf("ownership metadata = %#v", report.Ownership)
	}
	if got := report.Systems[0].Objectives[0]; len(got.EvidenceReferences) != 1 || got.EvidenceReferences[0].Ownership != ownership.StatusAssigned {
		t.Fatalf("ranking evidence = %#v", got)
	}
	if got := report.Systems[1].Objectives[0]; len(got.EvidenceReferences) != 0 || got.Mapping != MappingRequirementWithoutEvidence {
		t.Fatalf("support received ranking evidence: %#v", got)
	}
	for _, system := range report.Systems {
		got := system.Objectives[2]
		if len(got.EvidenceReferences) != 1 || got.EvidenceReferences[0].Ownership != ownership.StatusShared {
			t.Fatalf("shared evidence missing for %s: %#v", system.SystemID, got)
		}
	}
	if len(report.Systems[0].ObservedComponents) != 0 || len(report.Systems[1].ObservedComponents) != 1 {
		t.Fatalf("component ownership was not applied: %#v", report.Systems)
	}
	if len(report.Unmapped) != 0 || report.Summary.AssignedReferences != 2 || report.Summary.SharedReferences != 1 {
		t.Fatalf("unexpected ownership summary: %#v; unmapped=%#v", report.Summary, report.Unmapped)
	}
}

func TestBuildKeepsConflictingAndUnassignedEvidenceUnmapped(t *testing.T) {
	ranking := highRiskSystem()
	support := highRiskSystem()
	support.ID, support.Name = "support", "Support assistant"
	systems := []profile.System{ranking, support}
	technical := candidateTechnical()
	technical.Objectives = technical.Objectives[:1]
	technical.Objectives[0].Matches = append(technical.Objectives[0].Matches,
		framework.EvidenceMatch{Fingerprint: "conflict", Path: "shared/review.go", StartLine: 4, Kind: "source"},
	)
	rules := []ownership.Rule{
		{Paths: []string{"shared/**"}, Systems: []string{"ranking"}},
		{Paths: []string{"shared/review.go"}, Systems: []string{"support"}},
	}

	report := Build(systems, profile.AssessEUAIAct(systems), technical, inventory.Report{}, rules)
	if len(report.Unmapped) != 1 || len(report.Unmapped[0].References) != 2 {
		t.Fatalf("unresolved evidence = %#v", report.Unmapped)
	}
	if report.Unmapped[0].Reason.Code != "conflicting-path-ownership" {
		t.Fatalf("unresolved reason = %#v", report.Unmapped[0].Reason)
	}
	if report.Summary.ConflictingReferences != 1 || report.Summary.UnassignedReferences != 1 {
		t.Fatalf("ownership counts = %#v", report.Summary)
	}
	for _, system := range report.Systems {
		if got := system.Objectives[0]; got.Mapping != MappingUnassigned || len(got.EvidenceReferences) != 0 {
			t.Fatalf("unresolved evidence was assigned to %s: %#v", system.SystemID, got)
		}
	}
}

func TestBuildExplicitOwnershipDisablesSingleSystemGuessing(t *testing.T) {
	system := highRiskSystem()
	rules := []ownership.Rule{{Paths: []string{"services/**"}, Systems: []string{system.ID}}}
	technical := candidateTechnical()
	technical.Objectives = technical.Objectives[:1]
	report := Build([]profile.System{system}, profile.AssessEUAIAct([]profile.System{system}), technical, inventory.Report{}, rules)
	if report.Systems[0].Objectives[0].Mapping != MappingUnassigned || len(report.Unmapped) != 1 {
		t.Fatalf("explicit ownership unexpectedly fell back to inference: %#v", report)
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
