package profile

import (
	"fmt"
	"strings"
)

type ScopeStatus string
type HighRiskScreening string

const (
	ScopePotentiallyApplicable ScopeStatus = "potentially-applicable"
	ScopeNeedsContext          ScopeStatus = "needs-context"
	ScopeManualReview          ScopeStatus = "manual-review"

	HighRiskPotential HighRiskScreening = "potential-high-risk"
	HighRiskNoSignal  HighRiskScreening = "no-direct-high-risk-signal"
	HighRiskUnknown   HighRiskScreening = "needs-context"
)

type AssessmentReport struct {
	Framework    string       `json:"framework"`
	FrameworkURL string       `json:"framework_url"`
	Systems      []Assessment `json:"systems"`
	Notes        []string     `json:"notes"`
}

type Assessment struct {
	SystemID          string                 `json:"system_id"`
	SystemName        string                 `json:"system_name"`
	AutomatedScope    ScopeStatus            `json:"automated_scope"`
	HighRiskScreening HighRiskScreening      `json:"high_risk_screening"`
	Signals           []string               `json:"signals"`
	MissingContext    []string               `json:"missing_context"`
	HumanDecision     *ApplicabilityDecision `json:"human_decision,omitempty"`
}

const euAIActURL = "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689"

// AssessEUAIAct performs a conservative screening from declared profile facts.
// It does not make a legal classification or determine compliance.
func AssessEUAIAct(systems []System) AssessmentReport {
	report := AssessmentReport{
		Framework: "EU AI Act", FrameworkURL: euAIActURL, Systems: make([]Assessment, 0, len(systems)),
		Notes: []string{
			"Automated scope and high-risk screening are provisional and use only the declared system profile.",
			"No result is a legal determination, conformity assessment, or compliance certificate.",
		},
	}
	for _, system := range systems {
		report.Systems = append(report.Systems, assessSystem(system))
	}
	return report
}

func assessSystem(system System) Assessment {
	assessment := Assessment{
		SystemID: system.ID, SystemName: system.Name,
		Signals: []string{}, MissingContext: []string{},
	}

	if contains(system.OperatingRegions, RegionEU) || contains(system.OperatingRegions, RegionEEA) || contains(system.OperatingRegions, RegionGlobal) {
		assessment.AutomatedScope = ScopePotentiallyApplicable
		assessment.Signals = append(assessment.Signals, "Declared operations include the EU, EEA, or a global market.")
	} else if contains(system.OperatingRegions, RegionUnknown) {
		assessment.AutomatedScope = ScopeNeedsContext
		assessment.MissingContext = append(assessment.MissingContext, "Operating regions have not been established.")
	} else {
		assessment.AutomatedScope = ScopeManualReview
		assessment.Signals = append(assessment.Signals, "No direct EU, EEA, or global operating-region signal was declared; other territorial connections still require review.")
	}

	highRiskDomains := []UseCaseDomain{
		DomainBiometrics, DomainCriticalInfrastructure, DomainEducation, DomainEmployment,
		DomainEssentialServices, DomainLawEnforcement, DomainMigrationBorderControl, DomainJusticeDemocraticProcess,
	}
	matchedDomains := make([]UseCaseDomain, 0)
	for _, domain := range highRiskDomains {
		if contains(system.UseCaseDomains, domain) {
			matchedDomains = append(matchedDomains, domain)
		}
	}
	if len(matchedDomains) > 0 {
		assessment.HighRiskScreening = HighRiskPotential
		for _, domain := range matchedDomains {
			assessment.Signals = append(assessment.Signals, fmt.Sprintf("Declared use-case domain %q warrants EU AI Act Article 6 and Annex III review.", domain))
		}
	} else if contains(system.UseCaseDomains, DomainUnknown) {
		assessment.HighRiskScreening = HighRiskUnknown
		assessment.MissingContext = append(assessment.MissingContext, "Use-case domains have not been established.")
	} else {
		assessment.HighRiskScreening = HighRiskNoSignal
		assessment.Signals = append(assessment.Signals, "No direct Annex III domain signal was declared; product-safety and other classification routes remain manual.")
	}

	if contains(system.OrganizationRoles, RoleUnknown) {
		assessment.MissingContext = append(assessment.MissingContext, "The organization's AI value-chain role has not been established.")
	}
	if strings.EqualFold(strings.TrimSpace(system.IntendedPurpose), "unknown") {
		assessment.MissingContext = append(assessment.MissingContext, "The intended purpose has not been established.")
	}
	if system.LifecycleStage == LifecycleUnknown {
		assessment.MissingContext = append(assessment.MissingContext, "The lifecycle stage has not been established.")
	}
	if containsText(system.Users, "unknown") {
		assessment.MissingContext = append(assessment.MissingContext, "The system's users have not been established.")
	}
	if containsText(system.AffectedGroups, "unknown") {
		assessment.MissingContext = append(assessment.MissingContext, "Potentially affected groups have not been established.")
	}
	if system.DecisionImpact == ImpactUnknown {
		assessment.MissingContext = append(assessment.MissingContext, "Decision impact has not been established.")
	}
	if system.HumanOversight == OversightUnknown {
		assessment.MissingContext = append(assessment.MissingContext, "Human oversight has not been established.")
	}
	if len(system.AIActivities) == 0 || contains(system.AIActivities, ActivityUnknown) {
		assessment.MissingContext = append(assessment.MissingContext, "AI activities such as inference, training, evaluation, automated decisions, agent tool use, or synthetic-content generation have not been established.")
	}
	if system.Data.PersonalData == TriUnknown || system.Data.SpecialCategoryData == TriUnknown || system.Data.ChildrenData == TriUnknown {
		assessment.MissingContext = append(assessment.MissingContext, "One or more data categories have not been established.")
	}
	if contains(system.DeploymentModels, DeploymentUnknown) {
		assessment.MissingContext = append(assessment.MissingContext, "Deployment models have not been established.")
	}
	if system.ProfileReview.Status != ReviewConfirmed {
		assessment.MissingContext = append(assessment.MissingContext, "The factual system profile has not been confirmed by a named reviewer.")
	}
	for _, decision := range system.Applicability {
		if decision.Framework == FrameworkEUAIAct {
			value := decision
			assessment.HumanDecision = &value
			break
		}
	}
	return assessment
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsText(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}
