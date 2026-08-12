package framework

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

type ObjectiveStatus string

const (
	ObjectiveCandidate    ObjectiveStatus = "candidate-evidence"
	ObjectiveNotDetected  ObjectiveStatus = "not-detected"
	ObjectiveNotEvaluated ObjectiveStatus = "not-evaluated"

	maxEvidenceMatches    = 5
	maxEvidenceLineSpan   = 20
	maxKeywordOccurrences = 128

	ignoreMarkerPrefix = "complyscan:"
	ignoreMarkerSuffix = "ignore-technical-evidence"
)

type TechnicalEvidenceReport struct {
	SchemaVersion int                   `json:"schema_version"`
	Target        string                `json:"target,omitempty"`
	Pack          PackReference         `json:"pack"`
	Source        Source                `json:"source"`
	Coverage      Coverage              `json:"coverage"`
	Analysis      RepositoryAnalysis    `json:"repository_analysis"`
	Systems       []SystemReference     `json:"systems"`
	Objectives    []ObjectiveAssessment `json:"objectives"`
	Summary       ObjectiveSummary      `json:"summary"`
	Warnings      []string              `json:"warnings,omitempty"`
	Notes         []string              `json:"notes"`
}

type RepositoryAnalysis struct {
	GraphSchemaVersion     int                  `json:"graph_schema_version"`
	Languages              []codegraph.Language `json:"languages"`
	SourceFilesSeen        int                  `json:"source_files_seen"`
	FilesIndexed           int                  `json:"files_indexed"`
	UnsupportedSourceFiles []string             `json:"unsupported_source_files,omitempty"`
	SymbolsIndexed         int                  `json:"symbols_indexed"`
	RelationshipsIndexed   int                  `json:"relationships_indexed"`
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
	ID                  string                 `json:"id"`
	ControlID           string                 `json:"control_id"`
	Title               string                 `json:"title"`
	SourceReference     string                 `json:"source_reference"`
	Description         string                 `json:"description"`
	ApplicabilityNote   string                 `json:"applicability_note,omitempty"`
	Applicability       ObjectiveApplicability `json:"applicability"`
	Status              ObjectiveStatus        `json:"status"`
	Verification        string                 `json:"verification"`
	EligibleFileKinds   []string               `json:"eligible_file_kinds,omitempty"`
	InvestigationTerms  []string               `json:"investigation_terms,omitempty"`
	Matches             []EvidenceMatch        `json:"matches"`
	UnresolvedQuestions []string               `json:"unresolved_questions,omitempty"`
}

type EvidenceMatch struct {
	Fingerprint  string                   `json:"fingerprint"`
	Path         string                   `json:"path"`
	Kind         string                   `json:"kind"`
	StartLine    int                      `json:"start_line,omitempty"`
	MatchedTerms []string                 `json:"matched_terms"`
	Context      codegraph.ContextPackage `json:"context"`
}

// Evaluate maps repository code and configuration to technical objectives.
// Candidate evidence is never treated as proof of legal applicability,
// operational effectiveness, or compliance.
func Evaluate(pack Pack, systems []profile.System, repository discovery.Repository) TechnicalEvidenceReport {
	graph := codegraph.Build(repository)
	report := TechnicalEvidenceReport{
		SchemaVersion: 2,
		Pack: PackReference{
			ID: pack.ID, Name: pack.Name, Version: pack.Version,
			Released: pack.Released, Digest: pack.Digest,
		},
		Source:   pack.Source,
		Coverage: pack.Coverage,
		Analysis: RepositoryAnalysis{
			GraphSchemaVersion:     1,
			Languages:              append([]codegraph.Language(nil), graph.Languages...),
			SourceFilesSeen:        graph.SourceFilesSeen,
			FilesIndexed:           graph.FilesIndexed,
			UnsupportedSourceFiles: append([]string(nil), graph.UnsupportedSourceFiles...),
			SymbolsIndexed:         len(graph.Symbols), RelationshipsIndexed: len(graph.Edges),
		},
		Systems:    make([]SystemReference, 0, len(systems)),
		Objectives: evaluateObjectives(pack, repository, graph),
		Warnings:   append([]string(nil), graph.Warnings...),
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

func evaluateObjectives(pack Pack, repository discovery.Repository, graph codegraph.Graph) []ObjectiveAssessment {
	assessments := make([]ObjectiveAssessment, len(pack.Objectives))
	usedKinds := make(map[string]struct{})
	for index, objective := range pack.Objectives {
		assessments[index] = ObjectiveAssessment{
			ID: objective.ID, ControlID: objective.ControlID, Title: objective.Title, SourceReference: objective.SourceReference,
			Description: objective.Description, ApplicabilityNote: objective.ApplicabilityNote, Applicability: objective.Applicability,
			Status: ObjectiveNotDetected, Verification: objective.Verification,
			EligibleFileKinds:  append([]string(nil), objective.FileKinds...),
			InvestigationTerms: objectiveInvestigationTerms(objective), Matches: []EvidenceMatch{},
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
		if file.Kind == discovery.KindSource && !graph.SupportsSourcePath(file.Path) {
			continue
		}
		rawContent := string(file.Content)
		content := normalizeSearchContent(rawContent)
		if strings.Contains(content, ignoreMarker) {
			continue
		}
		path := file.Path
		for index, objective := range pack.Objectives {
			if !containsString(objective.FileKinds, string(file.Kind)) || len(assessments[index].Matches) >= maxEvidenceMatches {
				continue
			}
			matched, terms, line := matchesObjective(path, content, objective)
			if !matched {
				continue
			}
			if !candidatePassesStaticScope(objective.ControlID, path, rawContent, line) {
				continue
			}
			assessments[index].Matches = append(assessments[index].Matches, EvidenceMatch{
				Fingerprint:  evidenceFingerprint(objective.ControlID, file.Path, line, terms),
				Path:         file.Path,
				Kind:         string(file.Kind),
				StartLine:    line,
				MatchedTerms: terms,
				Context:      graph.ContextForMatch(file.Path, line, terms, 20),
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
			for matchIndex := range assessments[index].Matches {
				addObjectiveContextQuestions(&assessments[index].Matches[matchIndex].Context, assessments[index].ID)
			}
		} else if paths := unsupportedSourcesRelevantToObjective(repository, graph, pack.Objectives[index]); len(paths) > 0 {
			assessments[index].Status = ObjectiveNotEvaluated
			assessments[index].UnresolvedQuestions = []string{
				"This objective was not fully evaluated because a potentially relevant source file could not be indexed by a supported language analyzer: " + strings.Join(paths, ", ") + ".",
			}
		}
	}
	return assessments
}

func unsupportedSourcesRelevantToObjective(repository discovery.Repository, graph codegraph.Graph, objective TechnicalObjective) []string {
	if !containsString(objective.FileKinds, string(discovery.KindSource)) {
		return nil
	}
	paths := make([]string, 0)
	ignoreMarker := ignoreMarkerPrefix + ignoreMarkerSuffix
	for _, file := range repository.Files {
		if file.Kind != discovery.KindSource || graph.SupportsSourcePath(file.Path) {
			continue
		}
		rawContent := string(file.Content)
		content := normalizeSearchContent(rawContent)
		if strings.Contains(content, ignoreMarker) {
			continue
		}
		matched, _, line := matchesObjective(file.Path, content, objective)
		if !matched || !candidatePassesStaticScope(objective.ControlID, file.Path, rawContent, line) {
			continue
		}
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

func objectiveInvestigationTerms(objective TechnicalObjective) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0, len(objective.PathKeywords)+len(objective.KeywordGroups)*2)
	appendTerm := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if len(value) < 3 {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		terms = append(terms, value)
	}
	for _, value := range objective.PathKeywords {
		appendTerm(value)
	}
	for _, group := range objective.KeywordGroups {
		for _, value := range group {
			appendTerm(value)
		}
	}
	sort.Strings(terms)
	return terms
}

// candidatePassesStaticScope rejects bounded categories that satisfy lexical
// rules but cannot represent the objective they matched. Semantic review still
// handles contextual ambiguity; these checks cover structural false positives.
func candidatePassesStaticScope(controlID, path, content string, line int) bool {
	normalizedPath := normalizeSearchContent(path)
	if strings.HasPrefix(path, ".agents/skills/") {
		return false
	}

	switch controlID {
	case "dataset-validation":
		window := normalizeSearchContent(contentLineWindow(content, line, 3))
		if strings.Contains(window, "dataset name") && !containsAny(window,
			"schema", "completeness", "missing field", "missing value", "quality") {
			return false
		}
	case "bias-evaluation":
		if containsAny(normalizedPath, "/dataset/", "/datasets/", " dataset ") &&
			!containsAny(normalizedPath, "evaluation", "benchmark", "scorer") {
			return false
		}
	case "safe-stop":
		if containsAny(normalizedPath, "tracing", "telemetry", "logging", "audit") &&
			!containsAny(normalizedPath, "model", "inference", "prediction", "decision", "shutdown", "stop", "kill") {
			return false
		}
	case "performance-thresholds":
		window := normalizeSearchContent(contentLineWindow(content, line, 10))
		if containsAny(normalizedPath, "test", "spec") &&
			!containsAny(window, "expect", "assert", "pass", "fail", "greater than", "less than", "minimum", ">=", "<=") {
			return false
		}
	}

	if strings.HasSuffix(strings.ToLower(path), ".py") &&
		containsAny(normalizedPath, "test", "parser", "markdown") &&
		lineInLongTripleQuotedLiteral(content, line, 100) {
		return false
	}
	return true
}

func contentLineWindow(content string, line, radius int) string {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	start := line - 1 - radius
	if start < 0 {
		start = 0
	}
	end := line + radius
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}

func lineInLongTripleQuotedLiteral(content string, line, minimumLines int) bool {
	lineStarts := contentLineStarts(content)
	for _, delimiter := range []string{`"""`, `'''`} {
		searchFrom := 0
		for searchFrom < len(content) {
			openingRelative := strings.Index(content[searchFrom:], delimiter)
			if openingRelative < 0 {
				break
			}
			opening := searchFrom + openingRelative
			closingRelative := strings.Index(content[opening+len(delimiter):], delimiter)
			if closingRelative < 0 {
				break
			}
			closing := opening + len(delimiter) + closingRelative
			startLine := lineForOffset(lineStarts, opening)
			endLine := lineForOffset(lineStarts, closing)
			if endLine-startLine >= minimumLines && line >= startLine && line <= endLine {
				return true
			}
			searchFrom = closing + len(delimiter)
		}
	}
	return false
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func addObjectiveContextQuestions(context *codegraph.ContextPackage, objectiveID string) {
	if context.Anchor == nil {
		return
	}
	has := func(kind codegraph.EdgeKind) bool {
		for _, relationship := range context.Relationships {
			if relationship.Kind == kind {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(objectiveID, "eu-aia-14-"):
		if !has(codegraph.EdgeAuthorization) {
			context.UnresolvedQuestions = append(context.UnresolvedQuestions,
				"No connected authorization check was resolved; confirm who may invoke this mechanism.")
		}
	case objectiveID == "eu-aia-12-automatic-event-logging":
		if !has(codegraph.EdgeLogging) {
			context.UnresolvedQuestions = append(context.UnresolvedQuestions,
				"No connected audit or telemetry call was resolved from this anchor.")
		}
	case objectiveID == "eu-aia-9-risk-control-testing" || objectiveID == "eu-aia-10-bias-evaluation" || objectiveID == "eu-aia-15-performance-thresholds":
		if !has(codegraph.EdgeTest) && context.Anchor.Kind != codegraph.SymbolTest {
			context.UnresolvedQuestions = append(context.UnresolvedQuestions,
				"No connected test relationship was resolved for this candidate.")
		}
	}
}

func matchesObjective(path, content string, objective TechnicalObjective) (bool, []string, int) {
	lineStarts := contentLineStarts(content)
	groupedOccurrences := make([][]objectiveTermOccurrence, len(objective.KeywordGroups))
	for groupIndex, group := range objective.KeywordGroups {
		for _, keyword := range group {
			for _, offset := range keywordOffsets(content, strings.ToLower(keyword)) {
				groupedOccurrences[groupIndex] = append(groupedOccurrences[groupIndex], objectiveTermOccurrence{
					group: groupIndex, term: keyword, offset: offset, line: lineForOffset(lineStarts, offset),
				})
			}
		}
		if len(groupedOccurrences[groupIndex]) == 0 {
			return false, nil, 0
		}
	}
	terms, firstOffset, lineSpan := closestObjectiveTerms(groupedOccurrences)
	if len(objective.KeywordGroups) > 1 && lineSpan > maxEvidenceLineSpan {
		return false, nil, 0
	}
	pathMatched := false
	for _, keyword := range objective.PathKeywords {
		if keywordOffset(normalizeSearchContent(path), strings.ToLower(keyword)) >= 0 {
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
		line = lineForOffset(lineStarts, firstOffset)
	}
	return true, terms, line
}

type objectiveTermOccurrence struct {
	group  int
	term   string
	offset int
	line   int
}

func closestObjectiveTerms(groups [][]objectiveTermOccurrence) ([]string, int, int) {
	if len(groups) == 0 {
		return nil, -1, 0
	}
	all := make([]objectiveTermOccurrence, 0)
	for _, group := range groups {
		all = append(all, group...)
	}
	sort.Slice(all, func(left, right int) bool {
		if all[left].line != all[right].line {
			return all[left].line < all[right].line
		}
		if all[left].offset != all[right].offset {
			return all[left].offset < all[right].offset
		}
		return all[left].group < all[right].group
	})
	counts := make([]int, len(groups))
	covered := 0
	left := 0
	bestLeft, bestRight := 0, len(all)-1
	bestSpan := int(^uint(0) >> 1)
	bestByteSpan := bestSpan
	for right, occurrence := range all {
		if counts[occurrence.group] == 0 {
			covered++
		}
		counts[occurrence.group]++
		for covered == len(groups) && left <= right {
			lineSpan := all[right].line - all[left].line
			byteSpan := all[right].offset - all[left].offset
			if lineSpan < bestSpan || lineSpan == bestSpan && byteSpan < bestByteSpan {
				bestLeft, bestRight = left, right
				bestSpan, bestByteSpan = lineSpan, byteSpan
			}
			counts[all[left].group]--
			if counts[all[left].group] == 0 {
				covered--
			}
			left++
		}
	}
	selected := make([]objectiveTermOccurrence, len(groups))
	selectedSet := make([]bool, len(groups))
	for _, occurrence := range all[bestLeft : bestRight+1] {
		if !selectedSet[occurrence.group] {
			selected[occurrence.group] = occurrence
			selectedSet[occurrence.group] = true
		}
	}
	terms := make([]string, len(groups))
	firstOffset := selected[0].offset
	for index, occurrence := range selected {
		terms[index] = occurrence.term
		if occurrence.offset < firstOffset {
			firstOffset = occurrence.offset
		}
	}
	return terms, firstOffset, bestSpan
}

func contentLineStarts(content string) []int {
	starts := []int{0}
	for index := 0; index < len(content); index++ {
		if content[index] == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}

func lineForOffset(lineStarts []int, offset int) int {
	return sort.Search(len(lineStarts), func(index int) bool { return lineStarts[index] > offset })
}

func keywordOffset(content, keyword string) int {
	offsets := keywordOffsets(content, keyword)
	if len(offsets) == 0 {
		return -1
	}
	return offsets[0]
}

func keywordOffsets(content, keyword string) []int {
	if keyword == "" {
		return nil
	}
	offsets := make([]int, 0, 4)
	searchFrom := 0
	for searchFrom <= len(content)-len(keyword) && len(offsets) < maxKeywordOccurrences {
		relative := strings.Index(content[searchFrom:], keyword)
		if relative < 0 {
			break
		}
		offset := searchFrom + relative
		end := offset + len(keyword)
		beforeBoundary := offset == 0 || !keywordWordByte(content[offset-1])
		afterBoundary := end == len(content) || !keywordWordByte(content[end])
		if beforeBoundary && afterBoundary {
			offsets = append(offsets, offset)
		}
		searchFrom = offset + 1
	}
	return offsets
}

func normalizeSearchContent(content string) string {
	var normalized strings.Builder
	normalized.Grow(len(content))
	previousLowerOrDigit := false
	previousUpper := false
	for index := 0; index < len(content); index++ {
		value := content[index]
		if value >= 'A' && value <= 'Z' {
			nextLower := index+1 < len(content) && content[index+1] >= 'a' && content[index+1] <= 'z'
			if previousLowerOrDigit || previousUpper && nextLower {
				normalized.WriteByte(' ')
			}
			normalized.WriteByte(value + ('a' - 'A'))
			previousLowerOrDigit = false
			previousUpper = true
			continue
		}
		if value == '_' {
			normalized.WriteByte(' ')
			previousLowerOrDigit = false
			previousUpper = false
			continue
		}
		normalized.WriteByte(value)
		previousLowerOrDigit = value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
		previousUpper = false
	}
	return normalized.String()
}

func keywordWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
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
