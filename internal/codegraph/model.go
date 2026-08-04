package codegraph

import "sort"

// Language identifies a source language understood by an indexer.
type Language string

const (
	LanguageGo     Language = "go"
	LanguagePython Language = "python"
)

// SymbolKind classifies a repository symbol without tying consumers to a
// language-specific syntax tree.
type SymbolKind string

const (
	SymbolFunction SymbolKind = "function"
	SymbolMethod   SymbolKind = "method"
	SymbolType     SymbolKind = "type"
	SymbolTest     SymbolKind = "test"
)

// EdgeKind describes a relationship between two symbols or between a symbol
// and an external technical concern.
type EdgeKind string

const (
	EdgeCall          EdgeKind = "call"
	EdgeRoute         EdgeKind = "route"
	EdgeTest          EdgeKind = "test"
	EdgeAuthorization EdgeKind = "authorization"
	EdgePersistence   EdgeKind = "persistence"
	EdgeLogging       EdgeKind = "logging"
	EdgeConfiguration EdgeKind = "configuration"
)

// Reachability is a conservative classification derived from the repository
// graph. It is evidence for a reviewer, not proof that a path executes.
type Reachability string

const (
	ReachableProduction Reachability = "production-reachable"
	ReachableExported   Reachability = "exported-entry-candidate"
	ReachableTestOnly   Reachability = "test-only"
	ReachableUnknown    Reachability = "not-reached"
)

// Symbol is a language-neutral code location.
type Symbol struct {
	ID            string       `json:"id" yaml:"id"`
	Name          string       `json:"name" yaml:"name"`
	QualifiedName string       `json:"qualified_name" yaml:"qualified_name"`
	Kind          SymbolKind   `json:"kind" yaml:"kind"`
	Language      Language     `json:"language" yaml:"language"`
	Package       string       `json:"package,omitempty" yaml:"package,omitempty"`
	Path          string       `json:"path" yaml:"path"`
	StartLine     int          `json:"start_line" yaml:"start_line"`
	EndLine       int          `json:"end_line" yaml:"end_line"`
	Exported      bool         `json:"exported" yaml:"exported"`
	EntryPoint    bool         `json:"entry_point" yaml:"entry_point"`
	Reachability  Reachability `json:"reachability" yaml:"reachability"`
	source        string
}

// Edge is a best-effort relationship extracted from source code. Resolved is
// false when the target is an external concern or could not be bound to a
// repository symbol.
type Edge struct {
	Kind     EdgeKind `json:"kind" yaml:"kind"`
	From     string   `json:"from" yaml:"from"`
	To       string   `json:"to" yaml:"to"`
	Label    string   `json:"label,omitempty" yaml:"label,omitempty"`
	Path     string   `json:"path" yaml:"path"`
	Line     int      `json:"line" yaml:"line"`
	Resolved bool     `json:"resolved" yaml:"resolved"`
}

// Graph contains only repository metadata and relationships. Source excerpts
// are retained in unexported fields so they cannot accidentally enter reports.
type Graph struct {
	Languages              []Language `json:"languages" yaml:"languages"`
	SourceFilesSeen        int        `json:"source_files_seen" yaml:"source_files_seen"`
	FilesIndexed           int        `json:"files_indexed" yaml:"files_indexed"`
	IndexedSourceFiles     []string   `json:"indexed_source_files,omitempty" yaml:"indexed_source_files,omitempty"`
	UnsupportedSourceFiles []string   `json:"unsupported_source_files,omitempty" yaml:"unsupported_source_files,omitempty"`
	Imports                []Import   `json:"imports,omitempty" yaml:"imports,omitempty"`
	Symbols                []Symbol   `json:"symbols" yaml:"symbols"`
	Edges                  []Edge     `json:"edges" yaml:"edges"`
	Warnings               []string   `json:"warnings,omitempty" yaml:"warnings,omitempty"`

	symbolByID map[string]int
}

func (graph *Graph) finalize() {
	sort.Slice(graph.Symbols, func(i, j int) bool {
		if graph.Symbols[i].Path != graph.Symbols[j].Path {
			return graph.Symbols[i].Path < graph.Symbols[j].Path
		}
		if graph.Symbols[i].StartLine != graph.Symbols[j].StartLine {
			return graph.Symbols[i].StartLine < graph.Symbols[j].StartLine
		}
		return graph.Symbols[i].ID < graph.Symbols[j].ID
	})
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].Path != graph.Edges[j].Path {
			return graph.Edges[i].Path < graph.Edges[j].Path
		}
		if graph.Edges[i].Line != graph.Edges[j].Line {
			return graph.Edges[i].Line < graph.Edges[j].Line
		}
		if graph.Edges[i].Kind != graph.Edges[j].Kind {
			return graph.Edges[i].Kind < graph.Edges[j].Kind
		}
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		return graph.Edges[i].To < graph.Edges[j].To
	})
	sort.Strings(graph.IndexedSourceFiles)
	sort.Strings(graph.UnsupportedSourceFiles)
	sort.Strings(graph.Warnings)
	sort.Slice(graph.Imports, func(i, j int) bool {
		if graph.Imports[i].Path != graph.Imports[j].Path {
			return graph.Imports[i].Path < graph.Imports[j].Path
		}
		return graph.Imports[i].ImportedPath < graph.Imports[j].ImportedPath
	})

	graph.symbolByID = make(map[string]int, len(graph.Symbols))
	for index := range graph.Symbols {
		graph.symbolByID[graph.Symbols[index].ID] = index
	}
}

// SupportsSourcePath reports whether a language analyzer successfully parsed
// the source file. It prevents keyword matches from implying semantic coverage
// for unsupported languages.
func (graph Graph) SupportsSourcePath(path string) bool {
	index := sort.SearchStrings(graph.IndexedSourceFiles, path)
	return index < len(graph.IndexedSourceFiles) && graph.IndexedSourceFiles[index] == path
}

// Import records a language-level dependency declared by a source file.
type Import struct {
	Language     Language `json:"language" yaml:"language"`
	Path         string   `json:"path" yaml:"path"`
	Package      string   `json:"package,omitempty" yaml:"package,omitempty"`
	Alias        string   `json:"alias,omitempty" yaml:"alias,omitempty"`
	ImportedPath string   `json:"imported_path" yaml:"imported_path"`
}

func (graph Graph) symbol(id string) (Symbol, bool) {
	index, ok := graph.symbolByID[id]
	if !ok || index < 0 || index >= len(graph.Symbols) {
		return Symbol{}, false
	}
	return graph.Symbols[index], true
}
