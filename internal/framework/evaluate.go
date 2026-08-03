package framework

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
)

type ObjectiveStatus string

const (
	ObjectiveCandidate    ObjectiveStatus = "candidate-evidence"
	ObjectiveNotDetected  ObjectiveStatus = "not-detected"
	ObjectiveNotEvaluated ObjectiveStatus = "not-evaluated"

	maxEvidenceMatches = 5

	ignoreMarkerPrefix = "complyscan:"
	ignoreMarkerSuffix = "ignore-technical-evidence"
)

type TechnicalEvidenceReport struct {
	SchemaVersion int                   `json:"schema_version"`
	Target        string                `json:"target,omitempty"`
	Pack          PackReference         `json:"pack"`
	Source        Source                `json:"source"`
	Coverage      Coverage              `json:"coverage"`
	Systems       []SystemReference     `json:"systems"`
	Objectives    []ObjectiveAssessment `json:"objectives"`
	Summary       ObjectiveSummary      `json:"summary"`
	Warnings      []string              `json:"warnings,omitempty"`
	Notes         []string              `json:"notes"`
}

type PackReference struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Released string `json:"released"`
	Digest   string `json:"digest"`
}

type SystemReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ObjectiveSummary struct {
	Total             int `json:"total"`
	CandidateEvidence int `json:"candidate_evidence"`
	NotDetected       int `json:"not_detected"`
	NotEvaluated      int `json:"not_evaluated"`
}

type ObjectiveAssessment struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	SourceReference   string          `json:"source_reference"`
	Description       string          `json:"description"`
	ApplicabilityNote string          `json:"applicability_note,omitempty"`
	Status            ObjectiveStatus `json:"status"`
	Verification      string          `json:"verification"`
	Matches           []EvidenceMatch `json:"matches"`
}

type EvidenceMatch struct {
	Fingerprint  string   `json:"fingerprint"`
	Path         string   `json:"path"`
	Kind         string   `json:"kind"`
	StartLine    int      `json:"start_line,omitempty"`
	MatchedTerms []string `json:"matched_terms"`
}

// Evaluate maps repository code and configuration to technical objectives.
// Candidate evidence is never treated as proof of legal applicability,
// operational effectiveness, or compliance.
func Evaluate(pack Pack, systems []profile.System, repository discovery.Repository) TechnicalEvidenceReport {
	report := TechnicalEvidenceReport{
		SchemaVersion: 1,
		Pack: PackReference{
			ID: pack.ID, Name: pack.Name, Version: pack.Version,
			Released: pack.Released, Digest: pack.Digest,
		},
		Source:     pack.Source,
		Coverage:   pack.Coverage,
		Systems:    make([]SystemReference, 0, len(systems)),
		Objectives: evaluateObjectives(pack, repository),
		Warnings:   []string{},
		Notes: []string{
			"This report contains technical code evidence only; documentary, organisational, operational, and attestation evidence is outside the CLI boundary.",
			"Candidate evidence requires technical and human verification and is not a legal compliance conclusion.",
			"No evidence detected means only that this bounded scan did not locate the configured signal.",
		},
	}
	for _, system := range systems {
		report.Systems = append(report.Systems, SystemReference{ID: system.ID, Name: system.Name})
	}
	report.Summary = summarizeObjectives(report.Objectives)
	return report
}

func evaluateObjectives(pack Pack, repository discovery.Repository) []ObjectiveAssessment {
	assessments := make([]ObjectiveAssessment, len(pack.Objectives))
	usedKinds := make(map[string]struct{})
	for index, objective := range pack.Objectives {
		assessments[index] = ObjectiveAssessment{
			ID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
			Description: objective.Description, ApplicabilityNote: objective.ApplicabilityNote,
			Status: ObjectiveNotDetected, Verification: objective.Verification, Matches: []EvidenceMatch{},
		}
		for _, kind := range objective.FileKinds {
			usedKinds[kind] = struct{}{}
		}
	}

	ignoreMarker := ignoreMarkerPrefix + ignoreMarkerSuffix
	for _, file := range repository.Files {
		if _, relevant := usedKinds[string(file.Kind)]; !relevant {
			continue
		}
		content := strings.ToLower(string(file.Content))
		if strings.Contains(content, ignoreMarker) {
			continue
		}
		path := strings.ToLower(file.Path)
		for index, objective := range pack.Objectives {
			if !containsString(objective.FileKinds, string(file.Kind)) || len(assessments[index].Matches) >= maxEvidenceMatches {
				continue
			}
			matched, terms, line := matchesObjective(path, content, objective)
			if !matched {
				continue
			}
			assessments[index].Matches = append(assessments[index].Matches, EvidenceMatch{
				Fingerprint:  evidenceFingerprint(objective.ID, file.Path, line, terms),
				Path:         file.Path,
				Kind:         string(file.Kind),
				StartLine:    line,
				MatchedTerms: terms,
			})
		}
	}

	for index := range assessments {
		sort.Slice(assessments[index].Matches, func(left, right int) bool {
			if assessments[index].Matches[left].Path != assessments[index].Matches[right].Path {
				return assessments[index].Matches[left].Path < assessments[index].Matches[right].Path
			}
			return assessments[index].Matches[left].StartLine < assessments[index].Matches[right].StartLine
		})
		if len(assessments[index].Matches) > 0 {
			assessments[index].Status = ObjectiveCandidate
		}
	}
	return assessments
}

func matchesObjective(path, content string, objective TechnicalObjective) (bool, []string, int) {
	terms := make([]string, 0, len(objective.KeywordGroups)+1)
	firstOffset := -1
	for _, group := range objective.KeywordGroups {
		matched := ""
		matchedOffset := -1
		for _, keyword := range group {
			offset := strings.Index(content, strings.ToLower(keyword))
			if offset >= 0 {
				matched = keyword
				matchedOffset = offset
				break
			}
		}
		if matched == "" {
			return false, nil, 0
		}
		terms = append(terms, matched)
		if firstOffset < 0 || matchedOffset < firstOffset {
			firstOffset = matchedOffset
		}
	}
	pathMatched := false
	for _, keyword := range objective.PathKeywords {
		if strings.Contains(path, strings.ToLower(keyword)) {
			terms = append(terms, "path:"+keyword)
			pathMatched = true
			break
		}
	}
	if len(objective.PathKeywords) > 0 && !pathMatched {
		return false, nil, 0
	}
	if len(objective.KeywordGroups) == 0 && len(objective.PathKeywords) == 0 {
		return false, nil, 0
	}
	line := 0
	if firstOffset >= 0 {
		line = strings.Count(content[:firstOffset], "\n") + 1
	}
	return true, terms, line
}

func evidenceFingerprint(objectiveID, path string, line int, terms []string) string {
	value := strings.Join([]string{objectiveID, path, strconv.Itoa(line), strings.Join(terms, "\x00")}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func summarizeObjectives(objectives []ObjectiveAssessment) ObjectiveSummary {
	summary := ObjectiveSummary{Total: len(objectives)}
	for _, objective := range objectives {
		switch objective.Status {
		case ObjectiveCandidate:
			summary.CandidateEvidence++
		case ObjectiveNotDetected:
			summary.NotDetected++
		case ObjectiveNotEvaluated:
			summary.NotEvaluated++
		}
	}
	return summary
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
