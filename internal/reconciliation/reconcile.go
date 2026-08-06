package reconciliation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

// Build reconciles the applicability profile and technical evidence. Repository
// evidence is attributed from explicit path ownership. When no ownership is
// configured, exactly one declared system retains the prior provisional
// single-system inference.
func Build(systems []profile.System, assessments profile.AssessmentReport, technical framework.TechnicalEvidenceReport, components inventory.Report, ownershipRules []ownership.Rule) Report {
	mappingVersion := technical.Pack.Version
	if mappingVersion == "" {
		mappingVersion = "unknown"
	}
	resolver := ownership.New(ownershipRules)
	singleInferredSystem := ""
	if !resolver.Configured() && len(systems) == 1 {
		singleInferredSystem = systems[0].ID
	}
	report := Report{
		SchemaVersion: 2, MappingVersion: mappingVersion,
		Ownership: OwnershipReport{Configured: resolver.Configured(), Rules: cloneOwnershipRules(ownershipRules)},
		Systems:   make([]SystemResult, 0, len(systems)), Unmapped: []UnmappedEvidence{},
		Notes: []string{
			"Requirement statuses are conservative screening results, not legal determinations.",
			"Candidate evidence must be verified for reachability, production use, effectiveness, and path ownership.",
			"Without detected evidence means only that this bounded repository scan did not locate the configured signal.",
		},
	}
	assessmentBySystem := make(map[string]profile.Assessment, len(assessments.Systems))
	for _, assessment := range assessments.Systems {
		assessmentBySystem[assessment.SystemID] = assessment
	}

	objectiveRefs := make([][]EvidenceReference, len(technical.Objectives))
	for index, objective := range technical.Objectives {
		objectiveRefs[index] = resolveObjectiveReferences(objective, resolver, singleInferredSystem)
		addOwnershipSummary(&report.Summary, objectiveRefs[index])
	}
	componentRefs := make([][]EvidenceReference, len(components.Components))
	for index, component := range components.Components {
		componentRefs[index] = resolveComponentReferences(component, resolver, singleInferredSystem)
		addOwnershipSummary(&report.Summary, componentRefs[index])
	}

	for _, system := range systems {
		result := SystemResult{
			SystemID: system.ID, SystemName: system.Name,
			Objectives: make([]ObjectiveResult, 0, len(technical.Objectives)), ObservedComponents: []ComponentResult{},
		}
		assessment := assessmentBySystem[system.ID]
		for index, objective := range technical.Objectives {
			requirement, reasons := screenRequirement(system, assessment, objective)
			mapped := mapObjective(requirement, objective, reasons, referencesForSystem(objectiveRefs[index], system.ID), objectiveRefs[index])
			result.Objectives = append(result.Objectives, mapped)
			addObjectiveSummary(&report.Summary, mapped)
		}
		for index, component := range components.Components {
			owned := referencesForSystem(componentRefs[index], system.ID)
			if len(owned) == 0 && singleInferredSystem == "" {
				continue
			}
			result.ObservedComponents = append(result.ObservedComponents, associateComponent(system, component, owned, singleInferredSystem != ""))
		}
		report.Systems = append(report.Systems, result)
	}

	if singleInferredSystem == "" {
		for index, objective := range technical.Objectives {
			unresolved := unresolvedOwnershipReferences(objectiveRefs[index])
			if objective.Status == framework.ObjectiveCandidate && len(unresolved) > 0 {
				report.Unmapped = append(report.Unmapped, UnmappedEvidence{
					Kind: UnmappedTechnicalObjective, ID: objective.ID, Title: objective.Title,
					Reason: unresolvedOwnershipReason(unresolved, len(systems), resolver.Configured()), References: unresolved,
				})
			}
		}
		for index, component := range components.Components {
			unresolved := unresolvedOwnershipReferences(componentRefs[index])
			if len(unresolved) > 0 || len(componentRefs[index]) == 0 {
				report.Unmapped = append(report.Unmapped, UnmappedEvidence{
					Kind: UnmappedAIComponent, ID: component.Name, Title: component.Name,
					Reason: unresolvedOwnershipReason(unresolved, len(systems), resolver.Configured()), References: unresolved,
				})
			}
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

func mapObjective(requirement RequirementStatus, objective framework.ObjectiveAssessment, reasons []Reason, references, allReferences []EvidenceReference) ObjectiveResult {
	evidenceStatus := objective.Status
	if objective.Status == framework.ObjectiveCandidate && len(references) == 0 {
		evidenceStatus = framework.ObjectiveNotDetected
	}
	result := ObjectiveResult{
		ObjectiveID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
		Requirement: requirement, Evidence: evidenceStatus, Reasons: append([]Reason(nil), reasons...), EvidenceReferences: append([]EvidenceReference(nil), references...),
	}
	ownershipUnresolved := hasUnresolvedOwnership(allReferences)
	if objective.Status == framework.ObjectiveCandidate && len(references) == 0 {
		if ownershipUnresolved {
			result.Reasons = append(result.Reasons, Reason{Code: "evidence-system-unresolved", Message: "Candidate repository evidence exists, but at least one path is unassigned or has conflicting ownership; ComplyScan will not attach it to this system."})
		} else {
			result.Reasons = append(result.Reasons, Reason{Code: "candidate-evidence-owned-elsewhere", Message: "Candidate repository evidence exists only on paths assigned to another declared system."})
		}
	} else if len(references) > 0 && ownershipUnresolved {
		result.Reasons = append(result.Reasons, Reason{Code: "additional-evidence-system-unresolved", Message: "Additional candidate locations remain unassigned or have conflicting ownership and are reported separately."})
	}

	if evidenceStatus == framework.ObjectiveNotEvaluated {
		result.Mapping = MappingUnableToEvaluate
		result.Reasons = append(result.Reasons, Reason{Code: "objective-not-evaluated", Message: "One or more relevant source files could not be evaluated by a supported analyzer."})
		return result
	}
	if len(references) == 0 && objective.Status == framework.ObjectiveCandidate && ownershipUnresolved {
		result.Mapping = MappingUnassigned
		return result
	}
	switch requirement {
	case RequirementLikelyRequired:
		if evidenceStatus == framework.ObjectiveCandidate {
			result.Mapping = MappingRequirementWithEvidence
		} else {
			result.Mapping = MappingRequirementWithoutEvidence
			result.Reasons = append(result.Reasons, Reason{Code: "candidate-evidence-not-detected", Message: "The bounded scan did not detect configured evidence for this likely requirement; this does not prove the implementation is absent."})
		}
	case RequirementNotCurrentlyIndicated:
		if evidenceStatus == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceMismatch
			result.Reasons = append(result.Reasons, Reason{Code: "observed-evidence-not-declared", Message: "Repository evidence suggests activity or controls not indicated by the declared configuration; review the configuration and evidence ownership."})
		} else {
			result.Mapping = MappingNotCurrentlyIndicated
		}
	case RequirementContextDependent:
		if evidenceStatus == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceUnclear
		} else {
			result.Mapping = MappingApplicabilityUnresolved
		}
	case RequirementUnresolved:
		if evidenceStatus == framework.ObjectiveCandidate {
			result.Mapping = MappingEvidenceUnclear
		} else {
			result.Mapping = MappingApplicabilityUnresolved
		}
	}
	return result
}

func associateComponent(system profile.System, component inventory.Component, references []EvidenceReference, inferred bool) ComponentResult {
	reason := Reason{Code: "path-ownership-assignment", Message: "Observed component locations match an explicit path-to-system ownership rule."}
	if inferred {
		reason = Reason{Code: "single-system-repository-inference", Message: "This repository declares one system and no path ownership rules, so the observed AI component is provisionally associated with it."}
	} else if containsOwnershipStatus(references, ownership.StatusShared) {
		reason = Reason{Code: "shared-path-ownership", Message: "Observed component locations match an ownership rule explicitly shared by multiple systems."}
	}
	result := ComponentResult{
		Name: component.Name, Kind: component.Kind, Confidence: component.Confidence,
		Mapping: MappingAssociated, Locations: references,
		Reasons: []Reason{reason},
	}
	if len(system.AIActivities) == 0 || hasActivity(system.AIActivities, profile.ActivityUnknown) {
		result.Reasons = append(result.Reasons, Reason{Code: "ai-activities-not-established", Message: "The observed component confirms AI-related code, but the system's activities still need to be declared."})
	}
	return result
}

func resolveObjectiveReferences(objective framework.ObjectiveAssessment, resolver ownership.Resolver, inferredSystem string) []EvidenceReference {
	refs := make([]EvidenceReference, 0, len(objective.Matches))
	for _, match := range objective.Matches {
		refs = append(refs, resolveReference(EvidenceReference{Fingerprint: match.Fingerprint, Path: match.Path, Line: match.StartLine, Kind: match.Kind}, resolver, inferredSystem))
	}
	return refs
}

func resolveComponentReferences(component inventory.Component, resolver ownership.Resolver, inferredSystem string) []EvidenceReference {
	refs := make([]EvidenceReference, 0, len(component.Locations))
	for _, location := range component.Locations {
		refs = append(refs, resolveReference(EvidenceReference{Path: location.Path, Line: location.Line, Kind: string(location.EvidenceType)}, resolver, inferredSystem))
	}
	return refs
}

func resolveReference(reference EvidenceReference, resolver ownership.Resolver, inferredSystem string) EvidenceReference {
	if inferredSystem != "" {
		reference.Ownership = ownership.StatusInferred
		reference.Systems = []string{inferredSystem}
		return reference
	}
	resolution := resolver.Resolve(reference.Path)
	reference.Ownership = resolution.Status
	reference.Systems = append([]string(nil), resolution.Systems...)
	return reference
}

func referencesForSystem(references []EvidenceReference, systemID string) []EvidenceReference {
	result := make([]EvidenceReference, 0, len(references))
	for _, reference := range references {
		switch reference.Ownership {
		case ownership.StatusAssigned, ownership.StatusShared, ownership.StatusInferred:
			if containsString(reference.Systems, systemID) {
				result = append(result, reference)
			}
		}
	}
	return result
}

func unresolvedOwnershipReferences(references []EvidenceReference) []EvidenceReference {
	result := make([]EvidenceReference, 0, len(references))
	for _, reference := range references {
		if reference.Ownership == ownership.StatusUnassigned || reference.Ownership == ownership.StatusConflicting {
			result = append(result, reference)
		}
	}
	return result
}

func unresolvedOwnershipReason(references []EvidenceReference, systemCount int, configured bool) Reason {
	if containsOwnershipStatus(references, ownership.StatusConflicting) {
		return Reason{Code: "conflicting-path-ownership", Message: "One or more evidence paths match overlapping ownership rules with different system owners; resolve the conflict before attribution."}
	}
	if configured {
		return Reason{Code: "unassigned-path-ownership", Message: "One or more evidence paths do not match any configured ownership rule; ComplyScan will not guess their system."}
	}
	if systemCount == 0 {
		return Reason{Code: "no-declared-system", Message: "No AI system is declared, so repository evidence cannot be attributed to a system."}
	}
	return Reason{Code: "multi-system-ownership-required", Message: "Multiple AI systems are declared without path ownership rules, so repository evidence cannot be attributed safely."}
}

func hasUnresolvedOwnership(references []EvidenceReference) bool {
	for _, reference := range references {
		if reference.Ownership == ownership.StatusUnassigned || reference.Ownership == ownership.StatusConflicting {
			return true
		}
	}
	return false
}

func containsOwnershipStatus(references []EvidenceReference, wanted ownership.Status) bool {
	for _, reference := range references {
		if reference.Ownership == wanted {
			return true
		}
	}
	return false
}

func addOwnershipSummary(summary *Summary, references []EvidenceReference) {
	for _, reference := range references {
		switch reference.Ownership {
		case ownership.StatusAssigned:
			summary.AssignedReferences++
		case ownership.StatusShared:
			summary.SharedReferences++
		case ownership.StatusConflicting:
			summary.ConflictingReferences++
		case ownership.StatusUnassigned:
			summary.UnassignedReferences++
		case ownership.StatusInferred:
			summary.InferredReferences++
		}
	}
}

func cloneOwnershipRules(rules []ownership.Rule) []ownership.Rule {
	result := make([]ownership.Rule, 0, len(rules))
	for _, rule := range rules {
		result = append(result, ownership.Rule{
			Paths:   append([]string(nil), rule.Paths...),
			Systems: append([]string(nil), rule.Systems...),
		})
	}
	return result
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
