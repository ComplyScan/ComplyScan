package repositoryanalysis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const (
	targetedSourceBudgetPercent = 70
	targetedMaximumFileBytes    = 12_000
	targetedContextLines        = 60
)

type targetedCandidate struct {
	path    string
	score   int
	anchors []int
}

func runTargeted(
	ctx context.Context,
	reviewer Reviewer,
	repository discovery.Repository,
	graph codegraph.Graph,
	evidence []framework.TechnicalEvidenceReport,
	objectives []providers.RepositoryObjective,
	systems []providers.RepositorySystemContext,
	profileSystems []profile.System,
	budget int64,
	options Options,
) (providers.RepositoryAnalysisResult, error) {
	selected, considered := targetedRepositoryFiles(repository, graph, evidence, budget*targetedSourceBudgetPercent/100)
	if len(selected) == 0 {
		return providers.RepositoryAnalysisResult{
			Provider: options.Provider, Model: options.Model,
			Coverage: providers.RepositoryCoverage{
				Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
			},
			Result: providers.RepositorySectionResult{
				Scope: ".", AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
				UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{
					"No structurally relevant AI implementation or technical-objective code could be selected for model review.",
				},
			},
			Notes: []string{
				"Targeted analysis found no eligible structural anchors, so no repository source was sent to the model.",
				"Deterministic inventory and technical evidence remain available; absence of a selected anchor does not prove that the repository contains no AI implementation.",
			},
		}, nil
	}
	graphContext := repositoryGraphContext(graph, selected)
	inputBytes := requestContextBytes(selected, graphContext)
	if err := progress(options, Progress{
		Stage: "targeted-selection", Completed: len(selected), Total: considered, Scope: ".", InputBytes: inputBytes,
		Detail: fmt.Sprintf("selected %d of %d structurally relevant file(s)", len(selected), considered),
	}); err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	request := providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisTargeted, Scope: ".",
		RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
		Files: selected, Objectives: objectives, Systems: systems, Graph: graphContext,
		MaxOutputTokens: repositoryOutputTokens(options.Provider, 0),
	}
	result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
	if err != nil {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze targeted repository evidence: %w", err)
	}
	if err := validateSystemAttribution(result.Result, profileSystems, options.Ownership); err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	result.Coverage.Mode = providers.RepositoryAnalysisTargeted
	result.Notes = append(result.Notes,
		fmt.Sprintf("Targeted analysis selected %d of %d structurally relevant file(s) from %d discovered repository file(s).", len(selected), considered, len(repository.Files)),
		"Selection used deterministic AI inventory signals, technical-objective matches, production entry points, and their bounded repository-graph neighborhood.",
		"Files outside the evidence package were not reviewed by the model; absence of model evidence is not proof that an implementation is absent.",
	)
	if err := progress(options, Progress{Stage: "targeted-analysis", Completed: 1, Total: 1, Scope: ".", InputBytes: inputBytes}); err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	return result, nil
}

func targetedRepositoryFiles(repository discovery.Repository, graph codegraph.Graph, reports []framework.TechnicalEvidenceReport, budget int64) ([]providers.RepositorySourceFile, int) {
	files := make(map[string]discovery.File, len(repository.Files))
	for _, file := range repository.Files {
		files[file.Path] = file
	}
	candidates := make(map[string]*targetedCandidate)
	add := func(path string, line, score int) {
		file, exists := files[path]
		if !exists || !targetedFileKind(file.Kind) {
			return
		}
		candidate := candidates[path]
		if candidate == nil {
			candidate = &targetedCandidate{path: path}
			candidates[path] = candidate
		}
		if score > candidate.score {
			candidate.score = score
		}
		if line > 0 && !containsInt(candidate.anchors, line) {
			candidate.anchors = append(candidate.anchors, line)
		}
	}
	addContext := func(value codegraph.ContextPackage, score int) {
		if value.Anchor != nil {
			add(value.Anchor.Path, value.Anchor.StartLine, score)
		}
		for _, related := range value.RelatedSymbols {
			add(related.Path, related.StartLine, score-10)
		}
		for _, relationship := range value.Relationships {
			add(relationship.Path, relationship.Line, score-15)
		}
	}

	for _, signal := range inventory.Analyze(repository) {
		file, exists := files[signal.Path]
		if !exists || !targetedInventoryAnchor(file.Kind, signal) {
			continue
		}
		score := 65
		switch signal.Scope {
		case inventory.ScopeRuntime:
			score = 100
		case inventory.ScopeTest:
			score = 55
		}
		add(signal.Path, signal.Line, score)
		if file.Kind == discovery.KindSource && signal.Line > 0 {
			addContext(graph.ContextFor(signal.Path, signal.Line, 30), score-5)
		}
	}
	for _, report := range reports {
		for _, objective := range report.Objectives {
			for _, match := range objective.Matches {
				add(match.Path, match.StartLine, 90)
				addContext(match.Context, 85)
			}
		}
	}
	for depth := 0; depth < 2; depth++ {
		known := make(map[string]int, len(candidates))
		for path, candidate := range candidates {
			known[path] = candidate.score
		}
		for _, imported := range graph.Imports {
			if score, exists := known[imported.Path]; exists {
				for path := range files {
					if importedPathMatchesFile(imported.ImportedPath, path) {
						add(path, 1, score-20)
					}
				}
			}
			for path, score := range known {
				if importedPathMatchesFile(imported.ImportedPath, path) {
					add(imported.Path, 1, score-20)
				}
			}
		}
	}
	if len(candidates) == 0 {
		for _, symbol := range graph.Symbols {
			if symbol.EntryPoint && symbol.Reachability != codegraph.ReachableTestOnly {
				add(symbol.Path, symbol.StartLine, 35)
			}
		}
	}

	ordered := make([]targetedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		sort.Ints(candidate.anchors)
		ordered = append(ordered, *candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		return ordered[i].path < ordered[j].path
	})

	selected := make([]providers.RepositorySourceFile, 0, len(ordered))
	for _, candidate := range ordered {
		remaining := budget - requestContextBytes(selected, repositoryGraphContext(graph, selected))
		if remaining <= 256 {
			break
		}
		maximum := targetedMaximumFileBytes
		if int64(maximum) > remaining/2 {
			maximum = int(remaining / 2)
		}
		if maximum < 256 {
			break
		}
		prepared := targetedSourceFile(files[candidate.path], candidate.anchors, maximum)
		trial := append(append([]providers.RepositorySourceFile(nil), selected...), prepared)
		if requestContextBytes(trial, repositoryGraphContext(graph, trial)) > budget {
			continue
		}
		selected = trial
	}
	return selected, len(ordered)
}

func targetedSourceFile(file discovery.File, anchors []int, maximum int) providers.RepositorySourceFile {
	content := rules.RedactSecrets(strings.ReplaceAll(string(file.Content), "\r\n", "\n"))
	lineCount := strings.Count(content, "\n") + 1
	if len(content) <= maximum {
		return providers.RepositorySourceFile{Path: file.Path, Kind: string(file.Kind), LineCount: lineCount, ContentStartLine: 1, Content: content}
	}
	lines := strings.Split(content, "\n")
	anchor := 1
	if len(anchors) > 0 && anchors[0] > 0 {
		anchor = anchors[0]
	}
	start := anchor - targetedContextLines
	if start < 1 {
		start = 1
	}
	end := anchor + targetedContextLines
	if end > len(lines) {
		end = len(lines)
	}
	var selected []string
	bytes := 0
	for _, line := range lines[start-1 : end] {
		lineBytes := len(line) + 1
		if len(selected) > 0 && bytes+lineBytes > maximum {
			break
		}
		selected = append(selected, line)
		bytes += lineBytes
	}
	return providers.RepositorySourceFile{
		Path: file.Path, Kind: string(file.Kind), LineCount: lineCount, ContentStartLine: start, Content: strings.Join(selected, "\n"),
	}
}

func targetedFileKind(kind discovery.FileKind) bool {
	switch kind {
	case discovery.KindSource, discovery.KindManifest, discovery.KindDockerfile, discovery.KindGitHubAction,
		discovery.KindCI, discovery.KindTerraform, discovery.KindEnvTemplate, discovery.KindConfig:
		return true
	default:
		return false
	}
}

func targetedInventoryAnchor(kind discovery.FileKind, signal inventory.Signal) bool {
	if !targetedFileKind(kind) {
		return false
	}
	if kind == discovery.KindSource {
		return signal.EvidenceType == inventory.EvidenceImport || signal.EvidenceType == inventory.EvidenceEndpoint || signal.EvidenceType == inventory.EvidenceEnvironment
	}
	return signal.EvidenceType == inventory.EvidenceDependency || signal.EvidenceType == inventory.EvidenceEnvironment || signal.EvidenceType == inventory.EvidenceEndpoint
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func importedPathMatchesFile(importedPath, filePath string) bool {
	importedPath = strings.Trim(strings.TrimSpace(strings.ReplaceAll(importedPath, "\\", "/")), `"'`)
	filePath = strings.TrimSuffix(strings.ReplaceAll(filePath, "\\", "/"), filepathExtension(filePath))
	filePath = strings.TrimSuffix(filePath, "/index")
	importedPath = strings.TrimSuffix(importedPath, filepathExtension(importedPath))
	importedPath = strings.TrimPrefix(importedPath, "./")
	importedPath = strings.TrimSuffix(importedPath, "/index")
	if importedPath == "" || filePath == "" {
		return false
	}
	return filePath == importedPath || strings.HasSuffix(filePath, "/"+importedPath) || strings.HasSuffix(importedPath, "/"+filePath)
}

func filepathExtension(path string) string {
	lastSlash := strings.LastIndex(path, "/")
	lastDot := strings.LastIndex(path, ".")
	if lastDot <= lastSlash {
		return ""
	}
	return path[lastDot:]
}
