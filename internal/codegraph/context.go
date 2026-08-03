package codegraph

import (
	"fmt"
	"sort"
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
	RelatedSymbols      []SymbolReference `json:"related_symbols,omitempty" yaml:"related_symbols,omitempty"`
	Relationships       []Relationship    `json:"relationships,omitempty" yaml:"relationships,omitempty"`
	UnresolvedQuestions []string          `json:"unresolved_questions,omitempty" yaml:"unresolved_questions,omitempty"`
}

// ContextFor returns the enclosing symbol and a bounded one-hop neighborhood.
func (graph Graph) ContextFor(path string, line, maxRelationships int) ContextPackage {
	if maxRelationships <= 0 {
		maxRelationships = 20
	}
	anchor, found := graph.enclosingSymbol(path, line)
	if !found {
		return ContextPackage{UnresolvedQuestions: []string{
			fmt.Sprintf("No supported-language symbol encloses %s:%d.", path, line),
		}}
	}

	context := ContextPackage{Anchor: symbolReference(anchor)}
	related := make(map[string]SymbolReference)
	for _, edge := range graph.Edges {
		if len(context.Relationships) >= maxRelationships {
			break
		}
		if edge.From != anchor.ID && edge.To != anchor.ID {
			continue
		}
		from := graph.displayName(edge.From)
		to := graph.displayName(edge.To)
		context.Relationships = append(context.Relationships, Relationship{
			Kind: edge.Kind, From: from, To: to, Label: edge.Label,
			Path: edge.Path, Line: edge.Line, Resolved: edge.Resolved,
		})
		for _, id := range []string{edge.From, edge.To} {
			if id == anchor.ID {
				continue
			}
			if symbol, ok := graph.symbol(id); ok {
				related[id] = *symbolReference(symbol)
			}
		}
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
