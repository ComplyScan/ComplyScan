package profile

import "strings"

// CodeFactField identifies a profile answer that repository evidence may
// support as an unconfirmed technical fact. Jurisdiction, organisation role,
// actual deployment state, and legal applicability are deliberately excluded.
type CodeFactField string

const (
	CodeFactIntendedPurpose     CodeFactField = "intended-purpose"
	CodeFactLifecycleStage      CodeFactField = "lifecycle-stage"
	CodeFactUseCaseDomains      CodeFactField = "use-case-domains"
	CodeFactDecisionImpact      CodeFactField = "decision-impact"
	CodeFactHumanOversight      CodeFactField = "human-oversight"
	CodeFactAIActivities        CodeFactField = "ai-activities"
	CodeFactDeploymentModels    CodeFactField = "deployment-models"
	CodeFactUsers               CodeFactField = "users"
	CodeFactAffectedGroups      CodeFactField = "affected-groups"
	CodeFactPersonalData        CodeFactField = "personal-data"
	CodeFactSpecialCategoryData CodeFactField = "special-category-data"
	CodeFactChildrenData        CodeFactField = "children-data"
)

var codeFactFields = []CodeFactField{
	CodeFactIntendedPurpose,
	CodeFactLifecycleStage,
	CodeFactUseCaseDomains,
	CodeFactDecisionImpact,
	CodeFactHumanOversight,
	CodeFactAIActivities,
	CodeFactDeploymentModels,
	CodeFactUsers,
	CodeFactAffectedGroups,
	CodeFactPersonalData,
	CodeFactSpecialCategoryData,
	CodeFactChildrenData,
}

var codeFactAllowedValues = map[CodeFactField][]string{
	CodeFactIntendedPurpose: nil,
	CodeFactLifecycleStage: {
		string(LifecycleDevelopment), string(LifecycleTesting),
	},
	CodeFactUseCaseDomains: {
		string(DomainBiometrics), string(DomainCriticalInfrastructure), string(DomainEducation),
		string(DomainEmployment), string(DomainEssentialServices), string(DomainLawEnforcement),
		string(DomainMigrationBorderControl), string(DomainJusticeDemocraticProcess), string(DomainHealthcare),
		string(DomainSoftwareDevelopment), string(DomainGeneralPurpose), string(DomainOther),
	},
	CodeFactDecisionImpact: {
		string(ImpactAdvisory), string(ImpactLow), string(ImpactSignificant), string(ImpactAutonomous),
	},
	CodeFactHumanOversight: {
		string(OversightRequired), string(OversightAvailable), string(OversightLimited),
	},
	CodeFactAIActivities: {
		string(ActivityInference), string(ActivityTraining), string(ActivityFineTuning),
		string(ActivityEvaluation), string(ActivityAutomatedDecision), string(ActivityAgentToolUse),
		string(ActivitySyntheticContent),
	},
	CodeFactDeploymentModels: {
		string(DeploymentEmbedded), string(DeploymentAPI), string(DeploymentLocalCLI),
	},
	CodeFactUsers:               nil,
	CodeFactAffectedGroups:      nil,
	CodeFactPersonalData:        {string(TriYes)},
	CodeFactSpecialCategoryData: {string(TriYes)},
	CodeFactChildrenData:        {string(TriYes)},
}

// CodeFactFields returns the supported repository-evident profile fields in a
// stable order. The returned slice is safe for the caller to modify.
func CodeFactFields() []CodeFactField {
	return append([]CodeFactField(nil), codeFactFields...)
}

// ParseCodeFactField accepts only the canonical repository-evident field
// names. It deliberately performs no fuzzy matching.
func ParseCodeFactField(value string) (CodeFactField, bool) {
	field := CodeFactField(value)
	_, ok := codeFactAllowedValues[field]
	return field, ok
}

// CodeFactAllowedValues returns the bounded values for field. A nil value list
// means the supported field is free text; ok is false for an unknown field.
func CodeFactAllowedValues(field CodeFactField) (values []string, ok bool) {
	values, ok = codeFactAllowedValues[field]
	if !ok || values == nil {
		return nil, ok
	}
	return append([]string(nil), values...), true
}

// CodeFactAllowsValue applies the canonical positive repository-fact value
// contract. Free-text fields accept any non-empty value after caller-side text
// sanitization.
func CodeFactAllowsValue(field CodeFactField, value string) bool {
	values, ok := codeFactAllowedValues[field]
	if !ok || value == "" {
		return false
	}
	if values == nil {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "unknown", "none", "no", "not established", "not detected":
			return false
		}
		return true
	}
	for _, allowed := range values {
		if value == allowed {
			return true
		}
	}
	return false
}

// CodeFactPositiveOnly identifies fields for which repository evidence may
// support yes, but absence can never establish no.
func CodeFactPositiveOnly(field CodeFactField) bool {
	switch field {
	case CodeFactPersonalData, CodeFactSpecialCategoryData, CodeFactChildrenData:
		return true
	default:
		return false
	}
}

// CodeFactRequiresPositiveEvidence identifies controlled conclusions that
// must not be derived from missing repository evidence.
func CodeFactRequiresPositiveEvidence(field CodeFactField) bool {
	switch field {
	case CodeFactLifecycleStage, CodeFactDecisionImpact, CodeFactHumanOversight, CodeFactDeploymentModels:
		return true
	default:
		return CodeFactPositiveOnly(field)
	}
}

// CodeFactValueLimit bounds free text before it is retained in a model result.
func CodeFactValueLimit(field CodeFactField) int {
	switch field {
	case CodeFactIntendedPurpose:
		return 1000
	case CodeFactUsers, CodeFactAffectedGroups:
		return 200
	default:
		return 80
	}
}
