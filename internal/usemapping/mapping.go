// Package usemapping maps human-confirmed AI uses to framework objectives and
// repository evidence without turning either into a legal conclusion.
package usemapping

import (
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

const SchemaVersion = 1

type AssociationStatus string

const (
	AssociationConfigured AssociationStatus = "configured-system"
	AssociationMissing    AssociationStatus = "missing-system"
	AssociationNone       AssociationStatus = "no-system-association"
)

type ReviewAttribution string

const ReviewAttributionMatchingCitations ReviewAttribution = "all-citations-within-ai-use"

// FrameworkInput keeps the builder independent from the public report
// package, which embeds the resulting mapping.
type FrameworkInput struct {
	ID                string
	Name              string
	Nature            string
	Applicability     *profile.AssessmentReport
	TechnicalEvidence framework.TechnicalEvidenceReport
	TechnicalReview   *providers.TechnicalReviewResult
}

type Summary struct {
	Uses                                 int `json:"uses"`
	FrameworkSystemContexts              int `json:"framework_system_contexts"`
	Objectives                           int `json:"objectives"`
	LikelyRequired                       int `json:"likely_required"`
	Recommended                          int `json:"recommended_practices"`
	ContextDependent                     int `json:"context_dependent"`
	Unresolved                           int `json:"unresolved"`
	WithInScopeCodeEvidence              int `json:"with_in_scope_code_evidence"`
	LikelyRequiredWithoutInScopeEvidence int `json:"likely_required_without_in_scope_code_evidence"`
	RecommendedWithoutInScopeEvidence    int `json:"recommended_without_in_scope_code_evidence"`
	ObjectivesWithEvidenceOutsideUse     int `json:"objectives_with_evidence_outside_use"`
	AIReviewed                           int `json:"ai_reviewed"`
	UnassociatedUses                     int `json:"unassociated_uses"`
	MissingSystemReferences              int `json:"missing_system_references"`
}

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	Uses          []UseResult `json:"uses"`
	Summary       Summary     `json:"summary"`
	Notes         []string    `json:"notes"`
}

type UseResult struct {
	UseID       string            `json:"use_id"`
	UseName     string            `json:"use_name"`
	Description string            `json:"description"`
	SystemIDs   []string          `json:"system_ids"`
	Paths       []string          `json:"paths"`
	Frameworks  []FrameworkResult `json:"frameworks"`
	Summary     Summary           `json:"summary"`
}

type FrameworkResult struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Nature   string          `json:"nature"`
	Contexts []ContextResult `json:"contexts"`
	Summary  Summary         `json:"summary"`
}

type Association struct {
	Status     AssociationStatus `json:"status"`
	SystemID   string            `json:"system_id,omitempty"`
	SystemName string            `json:"system_name,omitempty"`
	Message    string            `json:"message"`
}

type ContextResult struct {
	Association Association       `json:"association"`
	Objectives  []ObjectiveResult `json:"objectives"`
	Summary     Summary           `json:"summary"`
}

type ObjectiveResult struct {
	reconciliation.ObjectiveResult
	EvidenceOutsideUse []EvidenceLocation `json:"evidence_outside_use,omitempty"`
	AIReview           *CodeReview        `json:"ai_review,omitempty"`
}

// EvidenceLocation records a deterministic match that exists in the
// repository but falls outside this AI use's human-owned path scope.
type EvidenceLocation struct {
	Fingerprint string `json:"fingerprint,omitempty"`
	Path        string `json:"path"`
	Line        int    `json:"line,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

// CodeReview is included only when every returned citation belongs to the
// confirmed AI use. Repository-wide negative conclusions without citations
// are deliberately not attributed to one use.
type CodeReview struct {
	RepositoryObjectiveID string                               `json:"repository_objective_id"`
	SystemID              string                               `json:"system_id,omitempty"`
	Verdict               providers.RepositoryTechnicalVerdict `json:"verdict"`
	Strength              providers.EvidenceStrength           `json:"strength"`
	Confidence            string                               `json:"confidence"`
	Rationale             string                               `json:"rationale"`
	SupportingEvidence    []providers.RepositoryCitation       `json:"supporting_evidence"`
	ContradictoryEvidence []providers.RepositoryCitation       `json:"contradictory_evidence"`
	MissingEvidence       []string                             `json:"missing_evidence,omitempty"`
	UnresolvedQuestions   []string                             `json:"unresolved_questions,omitempty"`
	Attribution           ReviewAttribution                    `json:"attribution"`
}

// Build evaluates active, developer-confirmed AI uses. Draft and retired
// records remain visible in the AI-use inventory but do not create current
// requirement mappings.
func Build(manifest aiuse.Manifest, systems []profile.System, frameworks []FrameworkInput, components inventory.Report, analysis *providers.RepositoryAnalysisResult) Report {
	value := Report{
		SchemaVersion: SchemaVersion,
		Uses:          []UseResult{},
		Notes: []string{
			"Each mapping uses human-confirmed AI-use paths for code scope and an associated configured system for declared applicability context.",
			"Likely-required and recommended statuses are conservative screening results, not legal conclusions.",
			"Code evidence and AI review verdicts do not establish production enablement, operational effectiveness, or compliance.",
			"A repository AI verdict is use-scoped only when its citations identify exactly one confirmed AI use; overlapping path scopes remain ambiguous.",
		},
	}
	systemsByID := make(map[string]profile.System, len(systems))
	for _, system := range systems {
		systemsByID[system.ID] = system
	}

	uses := append([]aiuse.Use(nil), manifest.Uses...)
	sort.SliceStable(uses, func(i, j int) bool { return uses[i].ID < uses[j].ID })
	confirmedUses := make([]aiuse.Use, 0, len(uses))
	for _, definition := range uses {
		if definition.Status != aiuse.StatusActive || definition.Review.Status != profile.ReviewConfirmed {
			continue
		}
		confirmedUses = append(confirmedUses, definition)
	}
	for _, definition := range confirmedUses {
		result := UseResult{
			UseID: definition.ID, UseName: definition.Name, Description: definition.Description,
			SystemIDs: append([]string(nil), definition.SystemIDs...), Paths: append([]string(nil), definition.Paths...),
			Frameworks: []FrameworkResult{},
		}
		associations := resolveAssociations(definition, systemsByID)
		if len(definition.SystemIDs) == 0 {
			result.Summary.UnassociatedUses = 1
		}
		for _, association := range associations {
			if association.Status == AssociationMissing {
				result.Summary.MissingSystemReferences++
			}
		}
		for _, input := range frameworks {
			frameworkResult := buildFramework(definition, confirmedUses, associations, systemsByID, input, components, analysis)
			result.Frameworks = append(result.Frameworks, frameworkResult)
			addSummary(&result.Summary, frameworkResult.Summary)
		}
		result.Summary.Uses = 1
		value.Uses = append(value.Uses, result)
		addSummary(&value.Summary, result.Summary)
	}
	value.Summary.Uses = len(value.Uses)
	return value
}

func resolveAssociations(definition aiuse.Use, systems map[string]profile.System) []Association {
	if len(definition.SystemIDs) == 0 {
		return []Association{{
			Status:  AssociationNone,
			Message: "This AI use is not associated with a configured system, so legal and other context-dependent applicability remains unresolved.",
		}}
	}
	result := make([]Association, 0, len(definition.SystemIDs))
	for _, id := range definition.SystemIDs {
		if system, exists := systems[id]; exists {
			result = append(result, Association{
				Status: AssociationConfigured, SystemID: id, SystemName: system.Name,
				Message: "Declared applicability facts are inherited from this configured system; code evidence is limited to the AI-use paths.",
			})
			continue
		}
		result = append(result, Association{
			Status: AssociationMissing, SystemID: id,
			Message: "The AI-use register references a system that is not present in the active ComplyScan configuration.",
		})
	}
	return result
}

func buildFramework(definition aiuse.Use, confirmedUses []aiuse.Use, associations []Association, systems map[string]profile.System, input FrameworkInput, components inventory.Report, analysis *providers.RepositoryAnalysisResult) FrameworkResult {
	filteredEvidence := filterTechnicalEvidence(definition, input.TechnicalEvidence)
	outsideEvidence := evidenceOutsideUse(definition, input.TechnicalEvidence)
	filteredComponents := filterInventory(definition, components)
	result := FrameworkResult{ID: input.ID, Name: input.Name, Nature: input.Nature, Contexts: []ContextResult{}}
	for _, association := range associations {
		context := buildContext(definition, confirmedUses, association, systems, input, filteredEvidence, filteredComponents, analysis)
		for index := range context.Objectives {
			context.Objectives[index].EvidenceOutsideUse = append([]EvidenceLocation(nil), outsideEvidence[context.Objectives[index].ObjectiveID]...)
			if len(context.Objectives[index].EvidenceOutsideUse) > 0 {
				context.Summary.ObjectivesWithEvidenceOutsideUse++
			}
		}
		result.Contexts = append(result.Contexts, context)
		addSummary(&result.Summary, context.Summary)
	}
	return result
}

func evidenceOutsideUse(definition aiuse.Use, value framework.TechnicalEvidenceReport) map[string][]EvidenceLocation {
	result := make(map[string][]EvidenceLocation)
	for _, objective := range value.Objectives {
		for _, match := range objective.Matches {
			if aiuse.UseMatchesPath(definition, match.Path) {
				continue
			}
			result[objective.ID] = append(result[objective.ID], EvidenceLocation{
				Fingerprint: match.Fingerprint, Path: match.Path, Line: match.StartLine, Kind: match.Kind,
			})
		}
	}
	return result
}

func buildContext(definition aiuse.Use, confirmedUses []aiuse.Use, association Association, systems map[string]profile.System, input FrameworkInput, technical framework.TechnicalEvidenceReport, components inventory.Report, analysis *providers.RepositoryAnalysisResult) ContextResult {
	system, assessment := contextSystem(association, systems, input.Applicability)
	rules := []ownership.Rule{{Paths: append([]string(nil), definition.Paths...), Systems: []string{system.ID}}}
	mapping := reconciliation.Build([]profile.System{system}, assessment, technical, components, rules)
	if input.TechnicalReview != nil {
		filtered := filterTechnicalReview(definition, system.ID, mapping, *input.TechnicalReview)
		reconciliation.AttachTechnicalInvestigations(&mapping, filtered)
	}
	context := ContextResult{Association: association, Objectives: []ObjectiveResult{}}
	if len(mapping.Systems) == 0 {
		return context
	}
	for _, objective := range mapping.Systems[0].Objectives {
		if association.Status != AssociationConfigured {
			if objective.Requirement != reconciliation.RequirementRecommended {
				objective.Reasons = append([]reconciliation.Reason{{
					Code: "ai-use-system-context-missing", Message: association.Message,
				}}, objective.Reasons...)
			}
			for index := range objective.EvidenceReferences {
				objective.EvidenceReferences[index].Ownership = ownership.StatusUnassigned
				objective.EvidenceReferences[index].Systems = []string{}
			}
		}
		wrapped := ObjectiveResult{ObjectiveResult: objective}
		wrapped.AIReview = attributableRepositoryReview(definition, confirmedUses, association, input.TechnicalEvidence.Pack.ID, objective.ObjectiveID, analysis)
		context.Objectives = append(context.Objectives, wrapped)
		addObjectiveSummary(&context.Summary, wrapped)
	}
	context.Summary.FrameworkSystemContexts = 1
	return context
}

func contextSystem(association Association, systems map[string]profile.System, applicability *profile.AssessmentReport) (profile.System, profile.AssessmentReport) {
	if association.Status == AssociationConfigured {
		system := systems[association.SystemID]
		return system, assessmentForSystem(applicability, system)
	}
	placeholder := profile.System{
		ID: "unassociated-ai-use", Name: "Unassociated AI use", IntendedPurpose: "unknown",
		LifecycleStage: profile.LifecycleUnknown, OrganizationRoles: []profile.OrganizationRole{profile.RoleUnknown},
		OperatingRegions: []profile.OperatingRegion{profile.RegionUnknown}, UseCaseDomains: []profile.UseCaseDomain{profile.DomainUnknown},
		Users: []string{"unknown"}, AffectedGroups: []string{"unknown"}, DecisionImpact: profile.ImpactUnknown,
		HumanOversight: profile.OversightUnknown, AIActivities: []profile.AIActivity{profile.ActivityUnknown},
		DeploymentModels: []profile.DeploymentModel{profile.DeploymentUnknown},
	}
	assessment := profile.AssessmentReport{Systems: []profile.Assessment{{
		SystemID: placeholder.ID, SystemName: placeholder.Name, AutomatedScope: profile.ScopeNeedsContext,
		HighRiskScreening: profile.HighRiskUnknown, MappingReadiness: profile.MappingIncomplete,
		Signals: []string{}, MissingContext: []string{"Associate this AI use with a configured system."},
	}}}
	if applicability != nil {
		assessment.Framework = applicability.Framework
		assessment.FrameworkURL = applicability.FrameworkURL
		assessment.Notes = append([]string(nil), applicability.Notes...)
	}
	return placeholder, assessment
}

func assessmentForSystem(value *profile.AssessmentReport, system profile.System) profile.AssessmentReport {
	if value == nil {
		return profile.AssessmentReport{Systems: []profile.Assessment{{
			SystemID: system.ID, SystemName: system.Name, AutomatedScope: profile.ScopeNeedsContext,
			HighRiskScreening: profile.HighRiskUnknown, MappingReadiness: profile.MappingIncomplete,
			Signals: []string{}, MissingContext: []string{"This framework does not yet provide an automated applicability assessment for the configured system."},
		}}}
	}
	result := profile.AssessmentReport{
		Framework: value.Framework, FrameworkURL: value.FrameworkURL, Notes: append([]string(nil), value.Notes...), Systems: []profile.Assessment{},
	}
	for _, assessment := range value.Systems {
		if assessment.SystemID == system.ID {
			result.Systems = append(result.Systems, assessment)
			break
		}
	}
	if len(result.Systems) == 0 {
		result.Systems = append(result.Systems, profile.Assessment{
			SystemID: system.ID, SystemName: system.Name, AutomatedScope: profile.ScopeNeedsContext,
			HighRiskScreening: profile.HighRiskUnknown, MappingReadiness: profile.MappingIncomplete,
			Signals: []string{}, MissingContext: []string{"This framework has no applicability assessment for the configured system."},
		})
	}
	return result
}

func filterTechnicalEvidence(definition aiuse.Use, value framework.TechnicalEvidenceReport) framework.TechnicalEvidenceReport {
	result := value
	result.Systems = nil
	result.Objectives = make([]framework.ObjectiveAssessment, 0, len(value.Objectives))
	result.Summary = framework.ObjectiveSummary{}
	result.Analysis.UnsupportedSourceFiles = filterPaths(definition, value.Analysis.UnsupportedSourceFiles)
	for _, objective := range value.Objectives {
		copy := objective
		copy.Matches = nil
		for _, match := range objective.Matches {
			if aiuse.UseMatchesPath(definition, match.Path) {
				copy.Matches = append(copy.Matches, match)
			}
		}
		switch {
		case len(copy.Matches) > 0:
			copy.Status = framework.ObjectiveCandidate
			result.Summary.CandidateEvidence++
		case (objective.Status == framework.ObjectiveNotEvaluated || objectiveCanUseSourceFiles(objective)) && len(result.Analysis.UnsupportedSourceFiles) > 0:
			copy.Status = framework.ObjectiveNotEvaluated
			result.Summary.NotEvaluated++
		default:
			copy.Status = framework.ObjectiveNotDetected
			result.Summary.NotDetected++
		}
		result.Objectives = append(result.Objectives, copy)
	}
	result.Summary.Total = len(result.Objectives)
	return result
}

func objectiveCanUseSourceFiles(value framework.ObjectiveAssessment) bool {
	for _, kind := range value.EligibleFileKinds {
		if kind == "source" {
			return true
		}
	}
	return false
}

func filterPaths(definition aiuse.Use, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if aiuse.UseMatchesPath(definition, value) {
			result = append(result, value)
		}
	}
	return result
}

func filterInventory(definition aiuse.Use, value inventory.Report) inventory.Report {
	result := value
	result.Components = []inventory.Component{}
	result.Summary = inventory.Summary{}
	for _, component := range value.Components {
		copy := component
		copy.Locations = nil
		for _, location := range component.Locations {
			if aiuse.UseMatchesPath(definition, location.Path) {
				copy.Locations = append(copy.Locations, location)
			}
		}
		if len(copy.Locations) == 0 {
			continue
		}
		copy.Occurrences = len(copy.Locations)
		result.Components = append(result.Components, copy)
	}
	return result
}

func filterTechnicalReview(definition aiuse.Use, systemID string, mapping reconciliation.Report, value providers.TechnicalReviewResult) providers.TechnicalReviewResult {
	allowedFingerprints := make(map[string]struct{})
	if len(mapping.Systems) > 0 {
		for _, objective := range mapping.Systems[0].Objectives {
			for _, reference := range objective.EvidenceReferences {
				if reference.Fingerprint != "" {
					allowedFingerprints[reference.Fingerprint] = struct{}{}
				}
			}
		}
	}
	result := value
	result.Observations = []providers.TechnicalObservation{}
	for _, observation := range value.Observations {
		if observation.SystemID != "" && observation.SystemID != systemID {
			continue
		}
		_, fingerprintMatches := allowedFingerprints[observation.EvidenceFingerprint]
		claimsWithinUse, hasClaims := technicalClaimsWithinUse(definition, observation)
		if fingerprintMatches && hasClaims && !claimsWithinUse {
			continue
		}
		if !fingerprintMatches && (!hasClaims || !claimsWithinUse) {
			continue
		}
		result.Observations = append(result.Observations, observation)
	}
	result.Reviewed = len(result.Observations)
	return result
}

func technicalClaimsWithinUse(definition aiuse.Use, observation providers.TechnicalObservation) (bool, bool) {
	count := 0
	for _, claim := range append(append([]providers.TechnicalEvidenceClaim(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...) {
		count++
		if !aiuse.UseMatchesPath(definition, claim.Path) {
			return false, true
		}
	}
	return count > 0, count > 0
}

func attributableRepositoryReview(definition aiuse.Use, confirmedUses []aiuse.Use, association Association, packID, objectiveID string, analysis *providers.RepositoryAnalysisResult) *CodeReview {
	if analysis == nil {
		return nil
	}
	repositoryObjectiveID := objectiveID
	if packID != "" {
		repositoryObjectiveID = packID + "/" + objectiveID
	}
	exact := make([]CodeReview, 0, 1)
	unscoped := make([]CodeReview, 0, 1)
	for _, observation := range analysis.Result.ObjectiveObservations {
		if observation.ObjectiveID != repositoryObjectiveID {
			continue
		}
		if association.Status == AssociationConfigured {
			if observation.SystemID != "" && observation.SystemID != association.SystemID {
				continue
			}
		} else if observation.SystemID != "" {
			continue
		}
		citations := append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...)
		if len(citations) == 0 || !allRepositoryCitationsWithinUse(definition, citations) {
			continue
		}
		matches := 0
		matchedID := ""
		for _, candidate := range confirmedUses {
			if !repositoryObservationCanBelongToUse(candidate, observation.SystemID) || !allRepositoryCitationsWithinUse(candidate, citations) {
				continue
			}
			matches++
			matchedID = candidate.ID
		}
		if matches != 1 || matchedID != definition.ID {
			continue
		}
		review := CodeReview{
			RepositoryObjectiveID: observation.ObjectiveID, SystemID: observation.SystemID,
			Verdict: observation.DerivedTechnicalVerdict(), Strength: observation.Strength, Confidence: observation.Confidence,
			Rationale: observation.Rationale, SupportingEvidence: append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...),
			ContradictoryEvidence: append([]providers.RepositoryCitation(nil), observation.ContradictoryEvidence...),
			MissingEvidence:       append([]string(nil), observation.MissingEvidence...), UnresolvedQuestions: append([]string(nil), observation.UnresolvedQuestions...),
			Attribution: ReviewAttributionMatchingCitations,
		}
		if association.Status == AssociationConfigured && observation.SystemID == association.SystemID {
			exact = append(exact, review)
		} else {
			unscoped = append(unscoped, review)
		}
	}
	if len(exact) == 1 {
		return &exact[0]
	}
	if len(exact) > 1 {
		return nil
	}
	if len(unscoped) == 1 {
		return &unscoped[0]
	}
	return nil
}

func repositoryObservationCanBelongToUse(definition aiuse.Use, systemID string) bool {
	if systemID == "" {
		return true
	}
	for _, candidate := range definition.SystemIDs {
		if candidate == systemID {
			return true
		}
	}
	return false
}

func allRepositoryCitationsWithinUse(definition aiuse.Use, citations []providers.RepositoryCitation) bool {
	for _, citation := range citations {
		if !aiuse.UseMatchesPath(definition, citation.Path) {
			return false
		}
	}
	return true
}

func addObjectiveSummary(summary *Summary, objective ObjectiveResult) {
	summary.Objectives++
	switch objective.Requirement {
	case reconciliation.RequirementLikelyRequired:
		summary.LikelyRequired++
	case reconciliation.RequirementRecommended:
		summary.Recommended++
	case reconciliation.RequirementContextDependent:
		summary.ContextDependent++
	case reconciliation.RequirementUnresolved:
		summary.Unresolved++
	}
	if objective.Evidence == framework.ObjectiveCandidate {
		summary.WithInScopeCodeEvidence++
	}
	if objective.Mapping == reconciliation.MappingRequirementWithoutEvidence {
		summary.LikelyRequiredWithoutInScopeEvidence++
	}
	if objective.Mapping == reconciliation.MappingRecommendedWithoutEvidence {
		summary.RecommendedWithoutInScopeEvidence++
	}
	if objective.AIReview != nil || objective.Investigation != nil {
		summary.AIReviewed++
	}
}

func addSummary(target *Summary, value Summary) {
	target.FrameworkSystemContexts += value.FrameworkSystemContexts
	target.Objectives += value.Objectives
	target.LikelyRequired += value.LikelyRequired
	target.Recommended += value.Recommended
	target.ContextDependent += value.ContextDependent
	target.Unresolved += value.Unresolved
	target.WithInScopeCodeEvidence += value.WithInScopeCodeEvidence
	target.LikelyRequiredWithoutInScopeEvidence += value.LikelyRequiredWithoutInScopeEvidence
	target.RecommendedWithoutInScopeEvidence += value.RecommendedWithoutInScopeEvidence
	target.ObjectivesWithEvidenceOutsideUse += value.ObjectivesWithEvidenceOutsideUse
	target.AIReviewed += value.AIReviewed
	target.UnassociatedUses += value.UnassociatedUses
	target.MissingSystemReferences += value.MissingSystemReferences
}
