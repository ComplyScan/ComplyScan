package profile

import "testing"

func TestAssessEUAIActSeparatesAutomatedSignalsAndHumanDecision(t *testing.T) {
	system := validSystem()
	system.Data.SpecialCategoryData = TriNo
	system.ProfileReview = ProfileReview{Status: ReviewConfirmed, ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02"}
	system.Applicability = []ApplicabilityDecision{{
		Framework: FrameworkEUAIAct, Status: ApplicabilityApplicable, Rationale: "Offered in the EU.",
		ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02",
	}}
	report := AssessEUAIAct([]System{system})
	if len(report.Systems) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	assessment := report.Systems[0]
	if assessment.AutomatedScope != ScopePotentiallyApplicable || assessment.HighRiskScreening != HighRiskPotential {
		t.Fatalf("unexpected automated assessment: %#v", assessment)
	}
	if assessment.HumanDecision == nil || assessment.HumanDecision.Status != ApplicabilityApplicable {
		t.Fatalf("unexpected human decision: %#v", assessment.HumanDecision)
	}
	if len(assessment.MissingContext) != 0 {
		t.Fatalf("unexpected missing context: %#v", assessment.MissingContext)
	}
	if assessment.MappingReadiness != MappingHumanReviewed {
		t.Fatalf("mapping readiness = %q", assessment.MappingReadiness)
	}
}

func TestAssessEUAIActKeepsUnknownContextVisible(t *testing.T) {
	system := NewDraftSystem("unknown-system", "Unknown system")
	report := AssessEUAIAct([]System{system})
	assessment := report.Systems[0]
	if assessment.AutomatedScope != ScopeNeedsContext || assessment.HighRiskScreening != HighRiskUnknown {
		t.Fatalf("unexpected automated assessment: %#v", assessment)
	}
	if len(assessment.MissingContext) < 6 {
		t.Fatalf("missing context was hidden: %#v", assessment.MissingContext)
	}
	if assessment.MappingReadiness != MappingIncomplete {
		t.Fatalf("mapping readiness = %q", assessment.MappingReadiness)
	}
	if assessment.HumanDecision == nil || assessment.HumanDecision.Status != ApplicabilityNeedsReview {
		t.Fatalf("unexpected human decision: %#v", assessment.HumanDecision)
	}
}

func TestAssessEUAIActOnlyRequiresConditionalContextWhenRelevant(t *testing.T) {
	system := validSystem()
	system.UseCaseDomains = []UseCaseDomain{DomainSoftwareDevelopment}
	system.DecisionImpact = ImpactLow
	system.AIActivities = []AIActivity{ActivityInference}
	system.Users = []string{"unknown"}
	system.AffectedGroups = []string{"unknown"}
	system.Data = DataProfile{PersonalData: TriUnknown, SpecialCategoryData: TriUnknown, ChildrenData: TriUnknown}
	system.ProfileReview = ProfileReview{Status: ReviewDraft}

	assessment := AssessEUAIAct([]System{system}).Systems[0]
	if len(assessment.MissingContext) != 0 || assessment.MappingReadiness != MappingFactuallyReady {
		t.Fatalf("irrelevant conditional context blocked mapping: %#v", assessment)
	}
}

func TestAssessEUAIActDoesNotInferExemptionWithoutEURegion(t *testing.T) {
	system := validSystem()
	system.OperatingRegions = []OperatingRegion{RegionUK}
	system.UseCaseDomains = []UseCaseDomain{DomainSoftwareDevelopment}
	assessment := AssessEUAIAct([]System{system}).Systems[0]
	if assessment.AutomatedScope != ScopeManualReview || assessment.HighRiskScreening != HighRiskNoSignal {
		t.Fatalf("unexpected automated assessment: %#v", assessment)
	}
}
