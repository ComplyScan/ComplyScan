package codegraph

import (
	"fmt"
	"sort"
	"strings"
)

// SymbolReference is the report-safe subset of a symbol.
type SymbolReference struct {
	ID            string       `json:"id" yaml:"id"`
	QualifiedName string       `json:"qualified_name" yaml:"qualified_name"`
	Kind          SymbolKind   `json:"kind" yaml:"kind"`
	Language      Language     `json:"language" yaml:"language"`
	Path          string       `json:"path" yaml:"path"`
	StartLine     int          `json:"start_line" yaml:"start_line"`
	EndLine       int          `json:"end_line" yaml:"end_line"`
	Reachability  Reachability `json:"reachability" yaml:"reachability"`
}

// Relationship is a report-safe, human-readable edge around an anchor.
type Relationship struct {
	Kind     EdgeKind `json:"kind" yaml:"kind"`
	From     string   `json:"from" yaml:"from"`
	To       string   `json:"to" yaml:"to"`
	Label    string   `json:"label,omitempty" yaml:"label,omitempty"`
	Path     string   `json:"path" yaml:"path"`
	Line     int      `json:"line" yaml:"line"`
	Resolved bool     `json:"resolved" yaml:"resolved"`
}

// ContextPackage is bounded structural context for one evidence location.
// It intentionally excludes source text from report serialization.
type ContextPackage struct {
	Anchor              *SymbolReference  `json:"anchor,omitempty" yaml:"anchor,omitempty"`
	Imports             []Import          `json:"imports,omitempty" yaml:"imports,omitempty"`
	RelatedSymbols      []SymbolReference `json:"related_symbols,omitempty" yaml:"related_symbols,omitempty"`
	Relationships       []Relationship    `json:"relationships,omitempty" yaml:"relationships,omitempty"`
	UnresolvedQuestions []string          `json:"unresolved_questions,omitempty" yaml:"unresolved_questions,omitempty"`
}

// ContextFor returns the enclosing symbol and a bounded structural neighborhood.
func (graph Graph) ContextFor(path string, line, maxRelationships int) ContextPackage {
	return graph.ContextForMatch(path, line, nil, maxRelationships)
}

// ContextForMatch uses objective terms to prefer an implementation symbol over
// an earlier caller in the same file.
func (graph Graph) ContextForMatch(path string, line int, matchedTerms []string, maxRelationships int) ContextPackage {
	if maxRelationships <= 0 {
		maxRelationships = 20
	}
	anchor, found := graph.matchingSymbol(path, line, matchedTerms)
	if !found {
		return ContextPackage{UnresolvedQuestions: []string{
			fmt.Sprintf("No supported-language symbol encloses %s:%d.", path, line),
		}}
	}

	context := ContextPackage{Anchor: symbolReference(anchor)}
	for _, repositoryImport := range graph.Imports {
		if repositoryImport.Path == anchor.Path {
			context.Imports = append(context.Imports, repositoryImport)
		}
	}
	related := make(map[string]SymbolReference)
	frontier := map[string]bool{anchor.ID: true}
	visitedSymbols := make(map[string]bool)
	visitedEdges := make(map[int]bool)
	for depth := 0; depth < 2 && len(frontier) > 0 && len(context.Relationships) < maxRelationships; depth++ {
		next := make(map[string]bool)
		for id := range frontier {
			visitedSymbols[id] = true
		}
		for edgeIndex, edge := range graph.Edges {
			if visitedEdges[edgeIndex] || (!frontier[edge.From] && !frontier[edge.To]) {
				continue
			}
			visitedEdges[edgeIndex] = true
			context.Relationships = append(context.Relationships, Relationship{
				Kind: edge.Kind, From: graph.displayName(edge.From), To: graph.displayName(edge.To),
				Label: edge.Label, Path: edge.Path, Line: edge.Line, Resolved: edge.Resolved,
			})
			for _, id := range []string{edge.From, edge.To} {
				if id == anchor.ID {
					continue
				}
				if symbol, ok := graph.symbol(id); ok {
					related[id] = *symbolReference(symbol)
					if !visitedSymbols[id] {
						next[id] = true
					}
				}
			}
			if len(context.Relationships) >= maxRelationships {
				break
			}
		}
		frontier = next
	}
	for _, reference := range related {
		context.RelatedSymbols = append(context.RelatedSymbols, reference)
	}
	sort.Slice(context.RelatedSymbols, func(i, j int) bool {
		return context.RelatedSymbols[i].ID < context.RelatedSymbols[j].ID
	})

	switch anchor.Reachability {
	case ReachableTestOnly:
		context.UnresolvedQuestions = append(context.UnresolvedQuestions,
			"The anchor is reachable only from indexed tests; confirm that equivalent production code exists.")
	case ReachableUnknown:
		context.UnresolvedQuestions = append(context.UnresolvedQuestions,
			"The anchor was not reached from an indexed production entry point; confirm whether it is live or dead code.")
	case ReachableExported:
		context.UnresolvedQuestions = append(context.UnresolvedQuestions,
			"The exported anchor may be called externally; repository-only reachability cannot confirm its runtime path.")
	}
	return context
}

// SourceForSymbol returns an internal-only bounded source excerpt. Callers must
// treat it as untrusted input and must not serialize it into a report.
func (graph Graph) SourceForSymbol(id string, maxBytes int) string {
	symbol, ok := graph.symbol(id)
	if !ok || maxBytes <= 0 {
		return ""
	}
	if len(symbol.source) <= maxBytes {
		return symbol.source
	}
	return symbol.source[:maxBytes]
}

func (graph Graph) matchingSymbol(path string, line int, matchedTerms []string) (Symbol, bool) {
	selected, found := graph.enclosingSymbol(path, line)
	bestScore := 0
	bestDistance := 0
	for _, symbol := range graph.Symbols {
		if symbol.Path != path || symbol.Kind == SymbolType {
			continue
		}
		candidate := compactSymbolTerm(symbol.QualifiedName)
		score := 0
		for _, term := range matchedTerms {
			if strings.HasPrefix(strings.ToLower(term), "path:") {
				continue
			}
			if compact := compactSymbolTerm(term); compact != "" && strings.Contains(candidate, compact) {
				score++
			}
		}
		distance := symbol.StartLine - line
		if distance < 0 {
			distance = -distance
		}
		if score > bestScore || (score == bestScore && score > 0 && distance < bestDistance) {
			selected, found, bestScore, bestDistance = symbol, true, score, distance
		}
	}
	return selected, found
}

func compactSymbolTerm(value string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(value))
}

func (graph Graph) enclosingSymbol(path string, line int) (Symbol, bool) {
	var selected Symbol
	found := false
	for _, symbol := range graph.Symbols {
		if symbol.Path != path || line < symbol.StartLine || line > symbol.EndLine {
			continue
		}
		if !found || symbol.EndLine-symbol.StartLine < selected.EndLine-selected.StartLine {
			selected = symbol
			found = true
		}
	}
	return selected, found
}

func (graph Graph) displayName(id string) string {
	if symbol, ok := graph.symbol(id); ok {
		return symbol.QualifiedName
	}
	return id
}

func symbolReference(symbol Symbol) *SymbolReference {
	return &SymbolReference{
		ID: symbol.ID, QualifiedName: symbol.QualifiedName, Kind: symbol.Kind,
		Language: symbol.Language, Path: symbol.Path, StartLine: symbol.StartLine,
		EndLine: symbol.EndLine, Reachability: symbol.Reachability,
	}
}
