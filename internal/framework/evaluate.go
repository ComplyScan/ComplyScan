package framework

import (
	"sort"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
)

type ActivationStatus string
type ControlStatus string
type EvidenceStatus string

const (
	ActivationCandidate    ActivationStatus = "candidate"
	ActivationNeedsReview  ActivationStatus = "needs-review"
	ActivationNotEvaluated ActivationStatus = "not-evaluated"

	ControlMissing       ControlStatus = "missing"
	ControlPartial       ControlStatus = "partial"
	ControlEvidenceFound ControlStatus = "evidence-found"

	EvidenceMissing   EvidenceStatus = "missing"
	EvidenceCandidate EvidenceStatus = "candidate-evidence"

	maxEvidenceMatches = 5
)

type AssessmentReport struct {
	SchemaVersion int                `json:"schema_version"`
	Pack          PackReference      `json:"pack"`
	Source        Source             `json:"source"`
	Coverage      Coverage           `json:"coverage"`
	Systems       []SystemAssessment `json:"systems"`
	Notes         []string           `json:"notes"`
}

type PackReference struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Released string `json:"released"`
}

type SystemAssessment struct {
	SystemID          string              `json:"system_id"`
	SystemName        string              `json:"system_name"`
	Activation        ActivationStatus    `json:"activation"`
	ActivationReasons []string            `json:"activation_reasons"`
	Controls          []ControlAssessment `json:"controls"`
	Summary           ControlSummary      `json:"summary"`
}

type ControlSummary struct {
	Total         int `json:"total"`
	Missing       int `json:"missing"`
	Partial       int `json:"partial"`
	EvidenceFound int `json:"evidence_found"`
}

type ControlAssessment struct {
	ID                   string               `json:"id"`
	Title                string               `json:"title"`
	SourceReference      string               `json:"source_reference"`
	Objective            string               `json:"objective"`
	ApplicabilityNote    string               `json:"applicability_note,omitempty"`
	Status               ControlStatus        `json:"status"`
	EvidenceRequirements []EvidenceAssessment `json:"evidence"`
}

type EvidenceAssessment struct {
	ID           string          `json:"id"`
	Description  string          `json:"description"`
	Status       EvidenceStatus  `json:"status"`
	Verification string          `json:"verification"`
	Matches      []EvidenceMatch `json:"matches"`
}

type EvidenceMatch struct {
	Path         string   `json:"path"`
	Kind         string   `json:"kind"`
	MatchedTerms []string `json:"matched_terms"`
}

// Evaluate maps repository evidence candidates to a pack. Candidate evidence
// is never treated as proof that a legal requirement is satisfied.
func Evaluate(pack Pack, systems []profile.System, repository discovery.Repository) AssessmentReport {
	profileReport := profile.AssessEUAIAct(systems)
	profileAssessments := make(map[string]profile.Assessment, len(profileReport.Systems))
	for _, assessment := range profileReport.Systems {
		profileAssessments[assessment.SystemID] = assessment
	}
	controlCandidates := evaluateControls(pack, repository)
	report := AssessmentReport{
		SchemaVersion: 1,
		Pack:          PackReference{ID: pack.ID, Name: pack.Name, Version: pack.Version, Released: pack.Released},
		Source:        pack.Source, Coverage: pack.Coverage,
		Systems: make([]SystemAssessment, 0, len(systems)),
		Notes: []string{
			"Statuses describe repository evidence candidates, not legal compliance or operational effectiveness.",
			"The strongest automated status is evidence-found; every match still requires the pack's stated semantic, technical, and human verification.",
		},
	}
	for _, system := range systems {
		assessment := SystemAssessment{
			SystemID: system.ID, SystemName: system.Name,
			ActivationReasons: []string{}, Controls: []ControlAssessment{},
		}
		assessment.Activation, assessment.ActivationReasons = activationFor(system, profileAssessments[system.ID])
		if assessment.Activation == ActivationCandidate {
			assessment.Controls = cloneControlAssessments(controlCandidates)
			assessment.Summary = summarizeControls(assessment.Controls)
		}
		report.Systems = append(report.Systems, assessment)
	}
	return report
}

func activationFor(system profile.System, applicability profile.Assessment) (ActivationStatus, []string) {
	for _, decision := range system.Applicability {
		if decision.Framework == profile.FrameworkEUAIAct && decision.Status == profile.ApplicabilityNotApplicable {
			return ActivationNotEvaluated, []string{"A human-recorded decision marks the EU AI Act not applicable; ComplyScan does not independently validate that decision."}
		}
	}
	if !contains(system.OrganizationRoles, profile.RoleProvider) {
		if contains(system.OrganizationRoles, profile.RoleUnknown) {
			return ActivationNeedsReview, []string{"The provider role required by this pack has not been established."}
		}
		return ActivationNeedsReview, []string{"This pack currently covers providers only; the declared role requires a different control pack or role review."}
	}
	scopeCandidate := applicability.AutomatedScope == profile.ScopePotentiallyApplicable
	for _, decision := range system.Applicability {
		if decision.Framework == profile.FrameworkEUAIAct && decision.Status == profile.ApplicabilityApplicable {
			scopeCandidate = true
		}
	}
	if !scopeCandidate {
		return ActivationNeedsReview, []string{"EU AI Act scope is not established from the declared profile."}
	}
	if applicability.HighRiskScreening != profile.HighRiskPotential {
		return ActivationNeedsReview, []string{"A high-risk classification is not established; this Articles 9–15 pack is not automatically activated."}
	}
	reasons := []string{"Declared facts make provider-facing high-risk system requirements a candidate for assessment."}
	if system.ProfileReview.Status != profile.ReviewConfirmed {
		reasons = append(reasons, "The factual profile is still draft, so activation requires human confirmation.")
	}
	return ActivationCandidate, reasons
}

func evaluateControls(pack Pack, repository discovery.Repository) []ControlAssessment {
	assessments := make([]ControlAssessment, len(pack.Controls))
	usedKinds := make(map[string]struct{})
	for controlIndex, control := range pack.Controls {
		assessment := ControlAssessment{
			ID: control.ID, Title: control.Title, SourceReference: control.SourceReference,
			Objective: control.Objective, ApplicabilityNote: control.ApplicabilityNote,
			EvidenceRequirements: make([]EvidenceAssessment, len(control.EvidenceRequirements)),
		}
		for evidenceIndex, requirement := range control.EvidenceRequirements {
			assessment.EvidenceRequirements[evidenceIndex] = EvidenceAssessment{
				ID: requirement.ID, Description: requirement.Description, Status: EvidenceMissing,
				Verification: requirement.Verification, Matches: []EvidenceMatch{},
			}
			for _, kind := range requirement.FileKinds {
				usedKinds[kind] = struct{}{}
			}
		}
		assessments[controlIndex] = assessment
	}

	for _, file := range repository.Files {
		if _, relevant := usedKinds[string(file.Kind)]; !relevant {
			continue
		}
		content := strings.ToLower(string(file.Content))
		path := strings.ToLower(file.Path)
		for controlIndex, control := range pack.Controls {
			for evidenceIndex, requirement := range control.EvidenceRequirements {
				if !containsString(requirement.FileKinds, string(file.Kind)) {
					continue
				}
				matched, terms := matchesRequirement(path, content, requirement)
				if !matched || len(assessments[controlIndex].EvidenceRequirements[evidenceIndex].Matches) >= maxEvidenceMatches {
					continue
				}
				assessments[controlIndex].EvidenceRequirements[evidenceIndex].Matches = append(
					assessments[controlIndex].EvidenceRequirements[evidenceIndex].Matches,
					EvidenceMatch{Path: file.Path, Kind: string(file.Kind), MatchedTerms: terms},
				)
			}
		}
	}

	for controlIndex := range assessments {
		found := 0
		for evidenceIndex := range assessments[controlIndex].EvidenceRequirements {
			evidence := &assessments[controlIndex].EvidenceRequirements[evidenceIndex]
			sort.Slice(evidence.Matches, func(left, right int) bool { return evidence.Matches[left].Path < evidence.Matches[right].Path })
			if len(evidence.Matches) > 0 {
				evidence.Status = EvidenceCandidate
				found++
			}
		}
		switch {
		case found == 0:
			assessments[controlIndex].Status = ControlMissing
		case found == len(assessments[controlIndex].EvidenceRequirements):
			assessments[controlIndex].Status = ControlEvidenceFound
		default:
			assessments[controlIndex].Status = ControlPartial
		}
	}
	return assessments
}

func matchesRequirement(path, content string, requirement EvidenceRequirement) (bool, []string) {
	terms := make([]string, 0, len(requirement.KeywordGroups)+1)
	if len(requirement.KeywordGroups) > 0 {
		for _, group := range requirement.KeywordGroups {
			matched := ""
			for _, keyword := range group {
				if strings.Contains(content, strings.ToLower(keyword)) {
					matched = keyword
					break
				}
			}
			if matched == "" {
				return false, nil
			}
			terms = append(terms, matched)
		}
	}
	pathMatched := false
	for _, keyword := range requirement.PathKeywords {
		if strings.Contains(path, strings.ToLower(keyword)) {
			terms = append(terms, "path:"+keyword)
			pathMatched = true
			break
		}
	}
	if len(requirement.KeywordGroups) == 0 && !pathMatched {
		return false, nil
	}
	return true, terms
}

func summarizeControls(controls []ControlAssessment) ControlSummary {
	summary := ControlSummary{Total: len(controls)}
	for _, control := range controls {
		switch control.Status {
		case ControlMissing:
			summary.Missing++
		case ControlPartial:
			summary.Partial++
		case ControlEvidenceFound:
			summary.EvidenceFound++
		}
	}
	return summary
}

func cloneControlAssessments(values []ControlAssessment) []ControlAssessment {
	cloned := make([]ControlAssessment, len(values))
	for index, control := range values {
		cloned[index] = control
		cloned[index].EvidenceRequirements = make([]EvidenceAssessment, len(control.EvidenceRequirements))
		for evidenceIndex, evidence := range control.EvidenceRequirements {
			cloned[index].EvidenceRequirements[evidenceIndex] = evidence
			cloned[index].EvidenceRequirements[evidenceIndex].Matches = append([]EvidenceMatch(nil), evidence.Matches...)
		}
	}
	return cloned
}

func contains[T comparable](values []T, wanted T) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	return contains(values, wanted)
}
