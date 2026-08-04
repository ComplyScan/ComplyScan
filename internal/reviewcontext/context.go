// Package reviewcontext builds bounded local-model input for existing
// technical-evidence candidates.
package reviewcontext

import (
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

// Build preserves deterministic objective and evidence bindings while adding
// bounded connected source and repository-graph context.
func Build(evidence framework.TechnicalEvidenceReport, repository discovery.Repository) providers.TechnicalReviewRequest {
	graph := codegraph.Build(repository)
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{}}
	for _, objective := range evidence.Objectives {
		for _, match := range objective.Matches {
			candidate := providers.TechnicalCandidate{
				ObjectiveID: objective.ID, Title: objective.Title, SourceReference: objective.SourceReference,
				Description: objective.Description, EvidenceFingerprint: match.Fingerprint,
				Path: match.Path, StartLine: match.StartLine,
				Imports: []string{}, Relationships: []providers.TechnicalRelationship{},
				UnresolvedQuestions: append([]string(nil), objective.UnresolvedQuestions...),
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
			} else if content, ok := repositoryFileContent(repository, match.Path); ok {
				candidate.SourceContexts = append(candidate.SourceContexts, providers.TechnicalSourceContext{
					Role: "matched-file", Symbol: "file:" + match.Path, Path: match.Path,
					StartLine: match.StartLine, EndLine: match.StartLine,
					Source: boundedRepositoryExcerpt(content, match.StartLine, remainingSource),
				})
			}
			request.Candidates = append(request.Candidates, candidate)
		}
	}
	return request
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
	start := line - 20
	if start < 0 {
		start = 0
	}
	end := line + 20
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
