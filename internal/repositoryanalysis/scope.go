package repositoryanalysis

import (
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const (
	changedReviewGraphDepth = 2
	// ChangedReviewConnectedFileLimit is the hard maximum of unchanged files
	// added to one changed-files model review.
	ChangedReviewConnectedFileLimit = 8
)

// ChangedReviewScope records the local boundary used to prepare an explicit
// AI review for a changed-files scan. The complete repository is still used by
// deterministic governance checks; only IncludedRepository may be used as
// model source context.
type ChangedReviewScope struct {
	RepositoryFiles        int
	RepositoryBytes        int64
	IncludedFiles          int
	IncludedBytes          int64
	ChangedFilesIncluded   int
	ConnectedFilesIncluded int
}

// ScopeChangedReview keeps every changed model-eligible file and adds at most
// eight unchanged source files that the local code graph connects within two
// relationship hops. Selection is deterministic and never adds an unrelated
// file merely because it contains an AI or compliance keyword.
func ScopeChangedReview(fullRepository, changedRepository discovery.Repository) (discovery.Repository, ChangedReviewScope) {
	fullFiles := make(map[string]discovery.File, len(fullRepository.Files))
	for _, file := range fullRepository.Files {
		fullFiles[file.Path] = file
	}

	changedPaths := make(map[string]struct{}, len(changedRepository.Files))
	includedPaths := make(map[string]struct{}, len(changedRepository.Files)+ChangedReviewConnectedFileLimit)
	for _, file := range changedRepository.Files {
		fullFile, exists := fullFiles[file.Path]
		if !exists || !targetedFileKind(fullFile.Kind) {
			continue
		}
		changedPaths[file.Path] = struct{}{}
		includedPaths[file.Path] = struct{}{}
	}

	type candidate struct {
		path          string
		depth         int
		relationships int
	}
	candidates := make(map[string]*candidate)
	addCandidate := func(path string, depth int) {
		if _, changed := changedPaths[path]; changed {
			return
		}
		file, exists := fullFiles[path]
		if !exists || !targetedFileKind(file.Kind) {
			return
		}
		value := candidates[path]
		if value == nil {
			value = &candidate{path: path, depth: depth}
			candidates[path] = value
		}
		if depth < value.depth {
			value.depth = depth
		}
		value.relationships++
	}

	graph := codegraph.Build(fullRepository)
	symbolPaths := make(map[string]string, len(graph.Symbols))
	frontier := make(map[string]struct{})
	visited := make(map[string]struct{})
	for _, symbol := range graph.Symbols {
		symbolPaths[symbol.ID] = symbol.Path
		if _, changed := changedPaths[symbol.Path]; changed {
			frontier[symbol.ID] = struct{}{}
			visited[symbol.ID] = struct{}{}
		}
	}
	for depth := 1; depth <= changedReviewGraphDepth && len(frontier) > 0; depth++ {
		next := make(map[string]struct{})
		for _, edge := range graph.Edges {
			_, fromFrontier := frontier[edge.From]
			_, toFrontier := frontier[edge.To]
			if !fromFrontier && !toFrontier {
				continue
			}
			other := edge.To
			if toFrontier {
				other = edge.From
			}
			path := symbolPaths[other]
			if path == "" {
				continue
			}
			addCandidate(path, depth)
			if _, seen := visited[other]; !seen {
				next[other] = struct{}{}
			}
		}
		for id := range next {
			visited[id] = struct{}{}
		}
		frontier = next
	}

	// Calls are not always statically resolvable. Direct imports still provide
	// a bounded, explainable connection in either direction.
	for _, repositoryImport := range graph.Imports {
		matchedPaths := targetedImportedPaths(repositoryImport.Path, repositoryImport.ImportedPath, fullFiles)
		if _, changed := changedPaths[repositoryImport.Path]; changed {
			for _, path := range matchedPaths {
				addCandidate(path, 1)
			}
		}
		for _, matchedPath := range matchedPaths {
			if _, changed := changedPaths[matchedPath]; changed {
				addCandidate(repositoryImport.Path, 1)
			}
		}
	}

	ordered := make([]candidate, 0, len(candidates))
	for _, value := range candidates {
		ordered = append(ordered, *value)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].depth != ordered[j].depth {
			return ordered[i].depth < ordered[j].depth
		}
		if ordered[i].relationships != ordered[j].relationships {
			return ordered[i].relationships > ordered[j].relationships
		}
		return ordered[i].path < ordered[j].path
	})
	connected := 0
	for _, value := range ordered {
		if connected >= ChangedReviewConnectedFileLimit {
			break
		}
		includedPaths[value.path] = struct{}{}
		connected++
	}

	scoped := discovery.Repository{Root: fullRepository.Root, Files: make([]discovery.File, 0, len(includedPaths))}
	for path := range includedPaths {
		scoped.Files = append(scoped.Files, fullFiles[path])
	}
	sort.Slice(scoped.Files, func(i, j int) bool { return scoped.Files[i].Path < scoped.Files[j].Path })
	scope := ChangedReviewScope{
		RepositoryFiles:        len(fullRepository.Files),
		RepositoryBytes:        repositorySize(fullRepository),
		IncludedFiles:          len(scoped.Files),
		IncludedBytes:          repositorySize(scoped),
		ChangedFilesIncluded:   len(changedPaths),
		ConnectedFilesIncluded: connected,
	}
	return scoped, scope
}

// Apply records the changed-files model boundary without changing the
// provider-reported submitted excerpt and citation counts.
func (scope ChangedReviewScope) Apply(result *providers.RepositoryAnalysisResult) {
	if result == nil {
		return
	}
	result.Coverage.ReviewScope = providers.RepositoryReviewScopeChanged
	result.Coverage.RepositoryFiles = scope.RepositoryFiles
	result.Coverage.RepositoryBytes = scope.RepositoryBytes
	result.Coverage.ScopeFiles = scope.IncludedFiles
	result.Coverage.ScopeBytes = scope.IncludedBytes
	result.Coverage.ChangedFiles = scope.ChangedFilesIncluded
	result.Coverage.ConnectedFiles = scope.ConnectedFilesIncluded
	result.Notes = append(result.Notes,
		"Changed-files AI review was limited to changed eligible files and at most eight unchanged files connected within two local code-graph hops.",
		"Repository-wide governance checks remained local and files outside this changed-plus-connected boundary were not sent to the model.",
	)
}
