package framework

import (
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
)

func candidateProviderSystem() profile.System {
	system := profile.NewDraftSystem("candidate-ranking", "Candidate ranking")
	system.IntendedPurpose = "Rank job applications for recruiter review."
	system.LifecycleStage = profile.LifecycleDevelopment
	system.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.Users = []string{"recruiters"}
	system.AffectedGroups = []string{"job applicants"}
	system.DecisionImpact = profile.ImpactAdvisory
	system.HumanOversight = profile.OversightRequired
	system.Data = profile.DataProfile{PersonalData: profile.TriYes, SpecialCategoryData: profile.TriNo, ChildrenData: profile.TriNo}
	system.DeploymentModels = []profile.DeploymentModel{profile.DeploymentPrivateCustomer}
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02"}
	return system
}

func TestEvaluateMapsCandidateEvidenceWithoutClaimingSatisfaction(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "docs/risk-assessment.md", Kind: discovery.KindRisk, Content: []byte("Risk assessment and mitigation controls cover intended purpose, foreseeable misuse, fundamental rights, testing, and post-market monitoring.")},
		{Path: "docs/AI_SYSTEM.md", Kind: discovery.KindAIGovernance, Content: []byte("Intended purpose, provider and version. Architecture and data flow describe deployment interactions. Evaluation metric limitations link to risk controls and changelog revisions.")},
	}}
	report := Evaluate(pack, []profile.System{candidateProviderSystem()}, repository)
	if len(report.Systems) != 1 || report.Systems[0].Activation != ActivationCandidate {
		t.Fatalf("unexpected activation: %#v", report.Systems)
	}
	assessment := report.Systems[0]
	if len(assessment.Controls) != 7 || assessment.Summary.Total != 7 || assessment.Summary.Partial == 0 {
		t.Fatalf("unexpected control assessment: %#v", assessment)
	}
	if assessment.Controls[0].Status != ControlEvidenceFound {
		t.Fatalf("Article 9 status = %q; evidence=%#v", assessment.Controls[0].Status, assessment.Controls[0].EvidenceRequirements)
	}
	for _, control := range assessment.Controls {
		if string(control.Status) == "satisfied" || string(control.Status) == "compliant" {
			t.Fatalf("control made an unsupported conclusion: %#v", control)
		}
		for _, evidence := range control.EvidenceRequirements {
			for _, match := range evidence.Matches {
				if match.Path == "" || len(match.MatchedTerms) == 0 {
					t.Fatalf("untraceable evidence match: %#v", match)
				}
			}
		}
	}
}

func TestEvaluateDoesNotActivateProviderPackForOtherRoles(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	system := candidateProviderSystem()
	system.OrganizationRoles = []profile.OrganizationRole{profile.RoleDeployer}
	assessment := Evaluate(pack, []profile.System{system}, discovery.Repository{}).Systems[0]
	if assessment.Activation != ActivationNeedsReview || len(assessment.Controls) != 0 || !strings.Contains(assessment.ActivationReasons[0], "providers only") {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
}

func TestEvaluateRespectsButDoesNotValidateHumanNotApplicableDecision(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	system := candidateProviderSystem()
	system.Applicability = []profile.ApplicabilityDecision{{
		Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityNotApplicable,
		Rationale: "Qualified review found no territorial scope.", ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02",
	}}
	assessment := Evaluate(pack, []profile.System{system}, discovery.Repository{}).Systems[0]
	if assessment.Activation != ActivationNotEvaluated || len(assessment.Controls) != 0 || !strings.Contains(assessment.ActivationReasons[0], "does not independently validate") {
		t.Fatalf("unexpected assessment: %#v", assessment)
	}
}

func TestEvidenceMatchesAreBoundedAndDeterministic(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]discovery.File, 0, 10)
	for index := 9; index >= 0; index-- {
		files = append(files, discovery.File{
			Path: "docs/risk-" + string(rune('a'+index)) + ".md", Kind: discovery.KindRisk,
			Content: []byte("risk assessment mitigation controls"),
		})
	}
	report := Evaluate(pack, []profile.System{candidateProviderSystem()}, discovery.Repository{Files: files})
	matches := report.Systems[0].Controls[0].EvidenceRequirements[0].Matches
	if len(matches) != maxEvidenceMatches {
		t.Fatalf("matches = %d", len(matches))
	}
	for index := 1; index < len(matches); index++ {
		if matches[index-1].Path > matches[index].Path {
			t.Fatalf("matches are not sorted: %#v", matches)
		}
	}
}
