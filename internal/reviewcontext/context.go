// Package reviewcontext builds bounded local-model input for existing
// technical-evidence candidates.
package reviewcontext

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

const (
	investigationModeCandidate = "candidate-validation"
	investigationModeSearch    = "extended-search"
)

// Build preserves deterministic objective and evidence bindings while adding
// bounded connected source and repository-graph context.
func Build(evidence framework.TechnicalEvidenceReport, repository discovery.Repository) providers.TechnicalReviewRequest {
	graph := codegraph.Build(repository)
	repositoryFingerprint := repositoryDigest(repository)
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{}}
	for _, objective := range evidence.Objectives {
		for _, match := range objective.Matches {
			candidate := providers.TechnicalCandidate{
				ObjectiveID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
				Description: objective.Description, EvidenceStatus: string(objective.Status),
				InvestigationMode: investigationModeCandidate, RepositoryDigest: repositoryFingerprint,
				EvidenceFingerprint: match.Fingerprint,
				Path:                match.Path, StartLine: match.StartLine,
				Imports: []string{}, Relationships: []providers.TechnicalRelationship{},
				UnresolvedQuestions: append([]string(nil), objective.UnresolvedQuestions...),
				SearchTerms:         append([]string(nil), objective.InvestigationTerms...),
				EligibleFileKinds:   append([]string(nil), objective.EligibleFileKinds...),
				SourceContexts:      []providers.TechnicalSourceContext{},
			}
			for _, repositoryImport := range match.Context.Imports {
				value := repositoryImport.ImportedPath
				if repositoryImport.Alias != "" {
					value = repositoryImport.Alias + "=" + value
				}
				candidate.Imports = append(candidate.Imports, value)
			}
			for _, relationship := range match.Context.Relationships {
				candidate.Relationships = append(candidate.Relationships, providers.TechnicalRelationship{
					Kind: string(relationship.Kind), From: relationship.From, To: relationship.To,
					Label: relationship.Label, Resolved: relationship.Resolved,
				})
			}
			candidate.UnresolvedQuestions = append(candidate.UnresolvedQuestions, match.Context.UnresolvedQuestions...)

			remainingSource := 12_000
			remainingSource = appendMatchedEvidenceSource(&candidate, repository, match.Path, match.StartLine, match.Context.Anchor != nil, remainingSource)
			if match.Context.Anchor != nil {
				candidate.Anchor = match.Context.Anchor.QualifiedName
				candidate.Reachability = string(match.Context.Anchor.Reachability)
				remainingSource = appendTechnicalSource(&candidate, graph, *match.Context.Anchor, "anchor", remainingSource)
				for _, relationship := range match.Context.Relationships {
					if relationship.Kind != codegraph.EdgeRoute || remainingSource <= 0 || len(candidate.SourceContexts) >= 6 {
						continue
					}
					remainingSource = appendTechnicalRelationshipSource(&candidate, repository, relationship, remainingSource)
				}
				for _, related := range match.Context.RelatedSymbols {
					if remainingSource <= 0 || len(candidate.SourceContexts) >= 6 {
						break
					}
					remainingSource = appendTechnicalSource(&candidate, graph, related, "related", remainingSource)
				}
			}
			request.Candidates = append(request.Candidates, candidate)
		}
	}
	return request
}

// BuildInvestigations adds one bounded repository-search target for every
// likely-required objective that had no deterministic candidate. Existing
// candidates keep their exact fingerprints and candidate-level context.
func BuildInvestigations(evidence framework.TechnicalEvidenceReport, repository discovery.Repository, mapping reconciliation.Report) providers.TechnicalReviewRequest {
	request := Build(evidence, repository)
	if len(mapping.Systems) > 0 {
		owned := ownedEvidenceFingerprints(mapping)
		filtered := make([]providers.TechnicalCandidate, 0, len(request.Candidates))
		for _, candidate := range request.Candidates {
			if owned[candidate.ObjectiveID][candidate.EvidenceFingerprint] {
				filtered = append(filtered, candidate)
			}
		}
		request.Candidates = filtered
	}
	// Repository-wide missing-evidence investigations are not yet system-bound.
	// Running one across a multi-system repository could use code owned by a
	// different system, so keep that question unresolved instead of guessing.
	if len(mapping.Systems) > 1 {
		return request
	}
	likely := likelyRequiredObjectives(mapping)
	for _, objective := range evidence.Objectives {
		if objective.Status == framework.ObjectiveCandidate || !likely[objective.ID] {
			continue
		}
		candidate := buildMissingEvidenceInvestigation(evidence.Pack, objective, repository)
		request.Candidates = append(request.Candidates, candidate)
	}
	return request
}

func ownedEvidenceFingerprints(mapping reconciliation.Report) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	for _, system := range mapping.Systems {
		for _, objective := range system.Objectives {
			for _, reference := range objective.EvidenceReferences {
				if reference.Fingerprint == "" {
					continue
				}
				if result[objective.ObjectiveID] == nil {
					result[objective.ObjectiveID] = make(map[string]bool)
				}
				result[objective.ObjectiveID][reference.Fingerprint] = true
			}
		}
	}
	return result
}

func likelyRequiredObjectives(mapping reconciliation.Report) map[string]bool {
	result := make(map[string]bool)
	for _, system := range mapping.Systems {
		for _, objective := range system.Objectives {
			if objective.Requirement == reconciliation.RequirementLikelyRequired {
				result[objective.ObjectiveID] = true
			}
		}
	}
	return result
}

type investigationHit struct {
	file  discovery.File
	score int
	line  int
}

func buildMissingEvidenceInvestigation(pack framework.PackReference, objective framework.ObjectiveAssessment, repository discovery.Repository) providers.TechnicalCandidate {
	candidate := providers.TechnicalCandidate{
		ObjectiveID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
		Description: objective.Description, EvidenceStatus: string(objective.Status), InvestigationMode: investigationModeSearch,
		RepositoryDigest: repositoryDigest(repository),
		Path:             "(repository-wide)", Imports: []string{}, Relationships: []providers.TechnicalRelationship{},
		UnresolvedQuestions: append([]string(nil), objective.UnresolvedQuestions...),
		SearchTerms:         append([]string(nil), objective.InvestigationTerms...),
		EligibleFileKinds:   append([]string(nil), objective.EligibleFileKinds...), SourceContexts: []providers.TechnicalSourceContext{},
	}
	candidate.UnresolvedQuestions = append(candidate.UnresolvedQuestions,
		"The deterministic matcher found no candidate; determine whether the bounded wider search reveals an indirect implementation or whether more evidence is required.")

	eligibleKinds := make(map[string]struct{}, len(objective.EligibleFileKinds))
	for _, kind := range objective.EligibleFileKinds {
		eligibleKinds[kind] = struct{}{}
	}
	hits := make([]investigationHit, 0)
	manifest := make([]string, 0)
	for _, file := range repository.Files {
		if _, eligible := eligibleKinds[string(file.Kind)]; !eligible {
			continue
		}
		candidate.SearchCoverage.EligibleFiles++
		if len(manifest) < 200 {
			manifest = append(manifest, file.Path)
		}
		score, line := investigationRelevance(file, objective.InvestigationTerms)
		if score > 0 {
			hits = append(hits, investigationHit{file: file, score: score, line: line})
		}
	}
	candidate.SearchCoverage.MatchingFiles = len(hits)
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].file.Path < hits[j].file.Path
	})
	for _, hit := range hits {
		if len(candidate.SourceContexts) >= 6 {
			break
		}
		excerpt := boundedRepositoryExcerpt(hit.file.Content, hit.line, 2_000)
		if excerpt == "" {
			continue
		}
		candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
			Role: "extended-search-hit", Symbol: "file:" + hit.file.Path, Path: hit.file.Path,
			StartLine: hit.line, EndLine: hit.line, Source: excerpt,
		})
	}
	candidate.SearchCoverage.Excerpts = len(candidate.SourceContexts)
	if len(manifest) > 0 && len(candidate.SourceContexts) < 8 {
		candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
			Role: "eligible-file-manifest", Symbol: "repository-manifest", Path: "(repository-wide)",
			Source: strings.Join(manifest, "\n"),
		})
	}
	digest, err := providers.TechnicalCandidateDigest(candidate)
	if err != nil {
		fallback := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s", pack.Digest, objective.ID, objective.Status)))
		candidate.EvidenceFingerprint = fmt.Sprintf("%x", fallback)
	} else {
		candidate.EvidenceFingerprint = digest
	}
	return candidate
}

// ApplyFollowUp executes a model-proposed plan as bounded literal searches.
// It never interprets queries as commands, globs, regular expressions, or
// filesystem paths and returns at most three additional excerpts.
func ApplyFollowUp(candidate providers.TechnicalCandidate, plan providers.TechnicalSearchPlan, repository discovery.Repository) (providers.TechnicalCandidate, int) {
	if !plan.Needed || len(plan.Queries) == 0 {
		return candidate, 0
	}
	eligibleKinds := make(map[string]struct{}, len(candidate.EligibleFileKinds))
	for _, kind := range candidate.EligibleFileKinds {
		eligibleKinds[kind] = struct{}{}
	}
	type followUpHit struct {
		file  discovery.File
		score int
		line  int
		query string
	}
	hits := make([]followUpHit, 0)
	for _, file := range repository.Files {
		if len(eligibleKinds) > 0 {
			if _, eligible := eligibleKinds[string(file.Kind)]; !eligible {
				continue
			}
		}
		path := strings.ToLower(file.Path)
		content := strings.ToLower(string(file.Content))
		for _, query := range plan.Queries {
			term := strings.ToLower(query.Text)
			index := strings.Index(content, term)
			pathMatch := strings.Contains(path, term)
			if index < 0 && !pathMatch {
				continue
			}
			score := strings.Count(content, term)
			if pathMatch {
				score += 4
			}
			if query.PathHint != "" && strings.Contains(path, strings.ToLower(query.PathHint)) {
				score += 6
			}
			line := 1
			if index >= 0 {
				line += strings.Count(content[:index], "\n")
			}
			hits = append(hits, followUpHit{file: file, score: score, line: line, query: query.Text})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].file.Path != hits[j].file.Path {
			return hits[i].file.Path < hits[j].file.Path
		}
		return hits[i].line < hits[j].line
	})
	removeRepositoryManifest(&candidate)
	added := 0
	seen := make(map[string]struct{})
	for _, hit := range hits {
		if added >= 3 {
			break
		}
		key := fmt.Sprintf("%s:%d", hit.file.Path, hit.line)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		excerpt := boundedRepositoryExcerpt(hit.file.Content, hit.line, 2_000)
		if excerpt == "" {
			continue
		}
		candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
			Role: "model-directed-follow-up", Symbol: "search:" + hit.query, Path: hit.file.Path,
			StartLine: hit.line, EndLine: hit.line, Source: excerpt,
		})
		added++
	}
	return candidate, added
}

func removeRepositoryManifest(candidate *providers.TechnicalCandidate) {
	contexts := candidate.SourceContexts[:0]
	for _, context := range candidate.SourceContexts {
		if context.Role != "eligible-file-manifest" {
			contexts = append(contexts, context)
		}
	}
	candidate.SourceContexts = contexts
}

func repositoryDigest(repository discovery.Repository) string {
	hash := sha256.New()
	files := append([]discovery.File(nil), repository.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, file := range files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.Kind))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(file.Content)
		_, _ = hash.Write([]byte{0})
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func investigationRelevance(file discovery.File, terms []string) (int, int) {
	path := strings.ToLower(file.Path)
	content := strings.ToLower(string(file.Content))
	score, firstLine := 0, 0
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(path, term) {
			score += 4
		}
		index := strings.Index(content, term)
		if index < 0 {
			continue
		}
		score += 1 + strings.Count(content, term)
		if firstLine == 0 {
			firstLine = 1 + strings.Count(content[:index], "\n")
		}
	}
	if firstLine == 0 {
		firstLine = 1
	}
	return score, firstLine
}

func appendMatchedEvidenceSource(candidate *providers.TechnicalCandidate, repository discovery.Repository, path string, line int, hasAnchor bool, remaining int) int {
	if remaining <= 0 {
		return 0
	}
	content, ok := repositoryFileContent(repository, path)
	if !ok {
		return remaining
	}
	limit := remaining
	if hasAnchor && limit > 6_000 {
		limit = 6_000
	}
	source := boundedRepositoryExcerpt(content, line, limit)
	if source == "" {
		return remaining
	}
	candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
		Role: "matched-evidence", Symbol: "file:" + path, Path: path,
		StartLine: line, EndLine: line, Source: source,
	})
	return remaining - len([]rune(source))
}

func appendTechnicalRelationshipSource(candidate *providers.TechnicalCandidate, repository discovery.Repository, relationship codegraph.Relationship, remaining int) int {
	if remaining <= 0 || relationship.Path == "" || relationship.Line <= 0 {
		return remaining
	}
	for _, source := range candidate.SourceContexts {
		if source.Path == relationship.Path && relationship.Line >= source.StartLine && relationship.Line <= source.EndLine {
			return remaining
		}
	}
	content, ok := repositoryFileContent(repository, relationship.Path)
	if !ok {
		return remaining
	}
	limit := 2_000
	if remaining < limit {
		limit = remaining
	}
	source := boundedRepositoryExcerpt(content, relationship.Line, limit)
	if source == "" {
		return remaining
	}
	candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
		Role: "relationship", Symbol: relationship.Label, Path: relationship.Path,
		StartLine: relationship.Line, EndLine: relationship.Line, Source: source,
	})
	return remaining - len([]rune(source))
}

func appendTechnicalSource(candidate *providers.TechnicalCandidate, graph codegraph.Graph, reference codegraph.SymbolReference, role string, remaining int) int {
	if remaining <= 0 {
		return 0
	}
	limit := 4_000
	if role == "anchor" {
		limit = 6_000
	}
	if remaining < limit {
		limit = remaining
	}
	source := graph.SourceForSymbol(reference.ID, limit)
	if source == "" {
		return remaining
	}
	candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
		Role: role, Symbol: reference.QualifiedName, Path: reference.Path,
		StartLine: reference.StartLine, EndLine: reference.EndLine,
		Reachability: string(reference.Reachability), Source: source,
	})
	return remaining - len([]rune(source))
}

func repositoryFileContent(repository discovery.Repository, path string) ([]byte, bool) {
	for _, file := range repository.Files {
		if file.Path == path {
			return file.Content, true
		}
	}
	return nil, false
}

func boundedRepositoryExcerpt(content []byte, line, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	start := line - 60
	if start < 0 {
		start = 0
	}
	end := line + 60
	if line <= 0 {
		start, end = 0, len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	value := strings.Join(lines[start:end], "\n")
	runes := []rune(value)
	if len(runes) > maxChars {
		return string(runes[:maxChars])
	}
	return value
}
