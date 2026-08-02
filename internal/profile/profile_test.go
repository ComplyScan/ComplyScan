package profile

import (
	"strings"
	"testing"
)

func validSystem() System {
	return System{
		ID: "candidate-ranking", Name: "Candidate ranking", IntendedPurpose: "Rank job applications for recruiter review.",
		LifecycleStage: LifecycleDevelopment, OrganizationRoles: []OrganizationRole{RoleProvider},
		OperatingRegions: []OperatingRegion{RegionEU}, UseCaseDomains: []UseCaseDomain{DomainEmployment},
		Users: []string{"recruiters"}, AffectedGroups: []string{"job applicants"},
		DecisionImpact: ImpactAdvisory, HumanOversight: OversightRequired,
		Data:             DataProfile{PersonalData: TriYes, SpecialCategoryData: TriUnknown, ChildrenData: TriNo},
		DeploymentModels: []DeploymentModel{DeploymentPrivateCustomer},
		ProfileReview:    ProfileReview{Status: ReviewDraft},
		Applicability:    []ApplicabilityDecision{{Framework: FrameworkEUAIAct, Status: ApplicabilityNeedsReview}},
	}
}

func TestSystemValidationAcceptsExplicitUnknowns(t *testing.T) {
	system := validSystem()
	system.IntendedPurpose = "unknown"
	system.OrganizationRoles = []OrganizationRole{RoleUnknown}
	system.OperatingRegions = []OperatingRegion{RegionUnknown}
	system.UseCaseDomains = []UseCaseDomain{DomainUnknown}
	system.Users = []string{"unknown"}
	system.AffectedGroups = []string{"unknown"}
	system.DecisionImpact = ImpactUnknown
	system.HumanOversight = OversightUnknown
	system.Data = DataProfile{PersonalData: TriUnknown, SpecialCategoryData: TriUnknown, ChildrenData: TriUnknown}
	system.DeploymentModels = []DeploymentModel{DeploymentUnknown}
	if err := system.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSystemValidationRejectsMissingAndUnsupportedContext(t *testing.T) {
	tests := []struct {
		name   string
		change func(*System)
		want   string
	}{
		{name: "invalid ID", change: func(value *System) { value.ID = "Candidate Ranking" }, want: "id must"},
		{name: "missing purpose", change: func(value *System) { value.IntendedPurpose = "" }, want: "intended-purpose"},
		{name: "missing regions", change: func(value *System) { value.OperatingRegions = nil }, want: "operating-regions"},
		{name: "unsupported role", change: func(value *System) { value.OrganizationRoles = []OrganizationRole{"owner"} }, want: "not supported"},
		{name: "duplicate domains", change: func(value *System) { value.UseCaseDomains = []UseCaseDomain{DomainEmployment, DomainEmployment} }, want: "duplicated"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			value := validSystem()
			testCase.change(&value)
			if err := value.Validate(); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("got error %v", err)
			}
		})
	}
}

func TestConfirmedReviewsAndDecisionsAreAttributable(t *testing.T) {
	system := validSystem()
	system.ProfileReview = ProfileReview{Status: ReviewConfirmed}
	if err := system.Validate(); err == nil || !strings.Contains(err.Error(), "reviewed-by") {
		t.Fatalf("got error %v", err)
	}
	system.ProfileReview = ProfileReview{Status: ReviewConfirmed, ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02"}
	system.Applicability = []ApplicabilityDecision{{Framework: FrameworkEUAIAct, Status: ApplicabilityApplicable}}
	if err := system.Validate(); err == nil || !strings.Contains(err.Error(), "rationale") {
		t.Fatalf("got error %v", err)
	}
	system.Applicability[0] = ApplicabilityDecision{
		Framework: FrameworkEUAIAct, Status: ApplicabilityApplicable, Rationale: "Offered in the EU.",
		ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02",
	}
	if err := system.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateSystemsRejectsDuplicateIDs(t *testing.T) {
	if err := ValidateSystems([]System{validSystem(), validSystem()}); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("got error %v", err)
	}
}
