package reconciliation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

// Build reconciles the applicability profile and technical evidence. Repository
// evidence is attributed automatically only when exactly one system is declared.
func Build(systems []profile.System, assessments profile.AssessmentReport, technical framework.TechnicalEvidenceReport, components inventory.Report) Report {
	mappingVersion := technical.Pack.Version
	if mappingVersion == "" {
		mappingVersion = "unknown"
	}
	report := Report{
		SchemaVersion: 1, MappingVersion: mappingVersion,
		Systems: make([]SystemResult, 0, len(systems)), Unmapped: []UnmappedEvidence{},
		Notes: []string{
			"Requirement statuses are conservative screening results, not legal determinations.",
			"Candidate evidence must be verified for reachability, production use, effectiveness, and system ownership.",
			"Without detected evidence means only that this bounded repository scan did not locate the configured signal.",
		},
	}
	assessmentBySystem := make(map[string]profile.Assessment, len(assessments.Systems))
	for _, assessment := range assessments.Systems {
		assessmentBySystem[assessment.SystemID] = assessment
	}

	singleSystem := len(systems) == 1
	for _, system := range systems {
		result := SystemResult{
			SystemID: system.ID, SystemName: system.Name,
			Objectives: make([]ObjectiveResult, 0, len(technical.Objectives)), ObservedComponents: []ComponentResult{},
		}
		assessment := assessmentBySystem[system.ID]
		for _, objective := range technical.Objectives {
			requirement, reasons := screenRequirement(system, assessment, objective)
			mapped := mapObjective(requirement, objective, reasons, singleSystem)
			result.Objectives = append(result.Objectives, mapped)
			addObjectiveSummary(&report.Summary, mapped)
		}
		if singleSystem {
			for _, component := range components.Components {
				result.ObservedComponents = append(result.ObservedComponents, associateComponent(system, component))
			}
		}
		report.Systems = append(report.Systems, result)
	}

	if !singleSystem {
		reason := Reason{Code: "no-declared-system", Message: "No system profile exists, so repository evidence cannot be assigned to a system."}
		if len(systems) > 1 {
			reason = Reason{Code: "multiple-systems-no-ownership", Message: "Multiple systems are declared, but no path-to-system ownership mapping exists; ComplyScan will not guess which system owns repository evidence."}
		}
		for _, objective := range technical.Objectives {
			if objective.Status != framework.ObjectiveCandidate {
				continue
			}
			report.Unmapped = append(report.Unmapped, UnmappedEvidence{
				Kind: UnmappedTechnicalObjective, ID: objective.ID, Title: objective.Title,
				Reason: reason, References: objectiveReferences(objective),
			})
		}
		for _, component := range components.Components {
			report.Unmapped = append(report.Unmapped, UnmappedEvidence{
				Kind: UnmappedAIComponent, ID: component.Name, Title: component.Name,
				Reason: reason, References: componentReferences(component),
			})
		}
	}
	sort.Slice(report.Unmapped, func(i, j int) bool {
		if report.Unmapped[i].Kind != report.Unmapped[j].Kind {
			return report.Unmapped[i].Kind < report.Unmapped[j].Kind
		}
		return report.Unmapped[i].ID < report.Unmapped[j].ID
	})
	report.Summary.Systems = len(report.Systems)
	report.Summary.UnmappedEvidence = len(report.Unmapped)
	return report
}

func screenRequirement(system profile.System, assessment profile.Assessment, objective framework.ObjectiveAssessment) (RequirementStatus, []Reason) {
	rule := objective.Applicability
	if err := rule.Validate(); err != nil {
		return RequirementUnresolved, []Reason{{Code: "objective-mapping-missing", Message: "This technical objective has no valid applicability conditions in its technical control pack."}}
	}
	if decision := euDecision(system); decision != nil {
		switch decision.Status {
		case profile.ApplicabilityNotApplicable:
			return RequirementNotCurrentlyIndicated, []Reason{{Code: "human-decision-not-applicable", Message: "The attributed human decision records the EU AI Act as not applicable; detected evidence remains visible for review."}}
		case profile.ApplicabilityApplicable:
			return screenApplicableActivity(system, rule)
		}
	}

	if rule.LegalScope == framework.ApplicabilityTransparencyObligation {
		if status, reasons, decided := screenActivity(system, rule); decided {
			return status, reasons
		}
	}
	highRiskStatus, reasons := screenHighRiskRequirement(assessment, objective.SourceReference)
	if highRiskStatus != RequirementLikelyRequired {
		return highRiskStatus, reasons
	}
	if len(rule.ActivitiesAnyOf) > 0 {
		if status, activityReasons, decided := screenActivity(system, rule); decided {
			return status, append(reasons, activityReasons...)
		}
	}
	return highRiskStatus, reasons
}

func screenApplicableActivity(system profile.System, rule framework.ObjectiveApplicability) (RequirementStatus, []Reason) {
	baseReason := Reason{Code: "human-decision-applicable", Message: "An attributed human decision records the EU AI Act as applicable."}
	if len(rule.ActivitiesAnyOf) == 0 {
		return RequirementLikelyRequired, []Reason{baseReason}
	}
	if status, reasons, decided := screenActivity(system, rule); decided {
		return status, append([]Reason{baseReason}, reasons...)
	}
	return RequirementLikelyRequired, []Reason{baseReason}
}

func screenActivity(system profile.System, rule framework.ObjectiveApplicability) (RequirementStatus, []Reason, bool) {
	if len(rule.ActivitiesAnyOf) == 0 {
		return "", nil, false
	}
	if len(system.AIActivities) == 0 || hasActivity(system.AIActivities, profile.ActivityUnknown) {
		return RequirementUnresolved, []Reason{{Code: "ai-activities-not-established", Message: "The system's AI activities have not been established."}}, true
	}
	if !hasAnyActivity(system.AIActivities, rule.ActivitiesAnyOf) {
		return RequirementNotCurrentlyIndicated, []Reason{{Code: "activity-not-declared", Message: "The declared AI activities do not currently indicate this objective."}}, true
	}
	if rule.ExternalUseRequired && !externallyUsed(system.DeploymentModels) {
		if len(system.DeploymentModels) == 0 || containsDeployment(system.DeploymentModels, profile.DeploymentUnknown) {
			return RequirementUnresolved, []Reason{{Code: "deployment-not-established", Message: "Deployment context is required to screen AI interaction disclosure."}}, true
		}
		return RequirementContextDependent, []Reason{{Code: "interaction-context-dependent", Message: "An interactive AI activity is declared, but the deployment model does not establish direct interaction with a natural person."}}, true
	}
	return RequirementLikelyRequired, []Reason{{Code: "matching-ai-activity", Message: "The declared AI activity indicates that this objective is likely relevant."}}, true
}

func screenHighRiskRequirement(assessment profile.Assessment, sourceReference string) (RequirementStatus, []Reason) {
	if assessment.AutomatedScope == profile.ScopeNeedsContext || assessment.HighRiskScreening == profile.HighRiskUnknown {
		return RequirementUnresolved, []Reason{{Code: "applicability-context-missing", Message: "Declared context is insufficient to screen " + sourceReference + "."}}
	}
	if assessment.AutomatedScope == profile.ScopePotentiallyApplicable && assessment.HighRiskScreening == profile.HighRiskPotential {
		return RequirementLikelyRequired, []Reason{{Code: "potential-high-risk-system", Message: "Declared EU/EEA/global operations and a potential high-risk use-case indicate that this technical objective is likely required."}}
	}
	return RequirementContextDependent, []Reason{{Code: "alternative-classification-review", Message: "No direct high-risk signal was established, but product-safety and other classification routes still require review."}}
}

func mapObjective(requirement RequirementStatus, objective framework.ObjectiveAssessment, reasons []Reason, includeEvidence bool) ObjectiveResult {
	result := ObjectiveResult{
		ObjectiveID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
		Requirement: requirement, Evidence: objective.Status, Reasons: append([]Reason(nil), reasons...), EvidenceReferences: []EvidenceReference{},
	}
	if includeEvidence {
		result.EvidenceReferences = objectiveReferences(objective)
	} else if objective.Status == framework.ObjectiveCandidate {
		result.Reasons = append(result.Reasons, Reason{Code: "evidence-system-unresolved", Message: "Candidate repository evidence exists but cannot be assigned to this system without an ownership mapping."})
	}

	if objective.Status == framework.ObjectiveNotEvaluated {
		result.Mapping = MappingUnableToEvaluate
		result.Reasons = append(result.Reasons, Reason{Code: "objective-not-evaluated", Message: "One or more relevant source files could not be evaluated by a supported analyzer."})
		return result
	}
	if !includeEvidence && objective.Status == framework.ObjectiveCandidate {
		result.Mapping = MappingUnassigned
		return result
	}
	switch requirement {
	case RequirementLikelyRequired:
		if objective.Status == framework.ObjectiveCandidate {
			result.Mapping = MappingRequirementWithEvidence
		} else {
			result.Mapping = MappingRequirementWithoutEvidence
			result.Reasons = append(result.Reasons, Reason{Code: "candidate-evidence-not-detected", Message: "The bounded scan did not detect configured evidence for this likely requirement; this does not prove the implementation is absent."})
		}
	case RequirementNotCurrentlyIndicated:
		if objective.Status == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceMismatch
			result.Reasons = append(result.Reasons, Reason{Code: "observed-evidence-not-declared", Message: "Repository evidence suggests activity or controls not indicated by the declared configuration; review the configuration and evidence ownership."})
		} else {
			result.Mapping = MappingNotCurrentlyIndicated
		}
	case RequirementContextDependent:
		if objective.Status == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceUnclear
		} else {
			result.Mapping = MappingApplicabilityUnresolved
		}
	case RequirementUnresolved:
		if objective.Status == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceUnclear
		} else {
			result.Mapping = MappingApplicabilityUnresolved
		}
	}
	return result
}

func associateComponent(system profile.System, component inventory.Component) ComponentResult {
	result := ComponentResult{
		Name: component.Name, Kind: component.Kind, Confidence: component.Confidence,
		Mapping: MappingAssociated, Locations: componentReferences(component),
		Reasons: []Reason{{Code: "single-system-repository-inference", Message: "This repository declares one system, so the observed AI component is provisionally associated with it."}},
	}
	if len(system.AIActivities) == 0 || hasActivity(system.AIActivities, profile.ActivityUnknown) {
		result.Reasons = append(result.Reasons, Reason{Code: "ai-activities-not-established", Message: "The observed component confirms AI-related code, but the system's activities still need to be declared."})
	}
	return result
}

func objectiveReferences(objective framework.ObjectiveAssessment) []EvidenceReference {
	refs := make([]EvidenceReference, 0, len(objective.Matches))
	for _, match := range objective.Matches {
		refs = append(refs, EvidenceReference{Fingerprint: match.Fingerprint, Path: match.Path, Line: match.StartLine, Kind: match.Kind})
	}
	return refs
}

func componentReferences(component inventory.Component) []EvidenceReference {
	refs := make([]EvidenceReference, 0, len(component.Locations))
	for _, location := range component.Locations {
		refs = append(refs, EvidenceReference{Path: location.Path, Line: location.Line, Kind: string(location.EvidenceType)})
	}
	return refs
}

func addObjectiveSummary(summary *Summary, result ObjectiveResult) {
	if result.Requirement == RequirementLikelyRequired {
		summary.LikelyRequired++
	}
	switch result.Mapping {
	case MappingRequirementWithEvidence:
		summary.RequirementWithEvidence++
	case MappingRequirementWithoutEvidence:
		summary.RequirementWithoutEvidence++
	case MappingEvidenceMismatch:
		summary.EvidenceMismatches++
	case MappingEvidenceUnclear, MappingApplicabilityUnresolved, MappingUnableToEvaluate, MappingUnassigned:
		summary.Unresolved++
	}
}

func euDecision(system profile.System) *profile.ApplicabilityDecision {
	for index := range system.Applicability {
		if system.Applicability[index].Framework == profile.FrameworkEUAIAct {
			return &system.Applicability[index]
		}
	}
	return nil
}

func hasActivity(values []profile.AIActivity, wanted profile.AIActivity) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hasAnyActivity(values []profile.AIActivity, wanted []string) bool {
	for _, value := range wanted {
		if hasActivity(values, profile.AIActivity(value)) {
			return true
		}
	}
	return false
}

func externallyUsed(values []profile.DeploymentModel) bool {
	for _, value := range values {
		switch value {
		case profile.DeploymentPrivateCustomer, profile.DeploymentPublic, profile.DeploymentAPI, profile.DeploymentEmbedded:
			return true
		}
	}
	return false
}

func containsDeployment(values []profile.DeploymentModel, wanted profile.DeploymentModel) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// ValidateCoverage is used by tests and release checks to ensure the versioned
// mapping remains synchronized with the embedded technical control pack.
func ValidateCoverage(technical framework.TechnicalEvidenceReport) error {
	var missing []string
	for _, objective := range technical.Objectives {
		if err := objective.Applicability.Validate(); err != nil {
			missing = append(missing, objective.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("technical pack %s does not contain valid applicability conditions for objectives: %s", technical.Pack.Version, strings.Join(missing, ", "))
	}
	return nil
}
