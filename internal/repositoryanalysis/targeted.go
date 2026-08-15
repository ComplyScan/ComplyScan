package repositoryanalysis

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
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
	targetedMaxFollowUpExcerpts = 3
	targetedRemoteInputTokens   = 6_500
	targetedLocalInputTokens    = 16_000
	targetedRemoteOutputTokens  = 4_096
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
	confirmedUses []providers.RepositoryConfirmedAIUse,
	profileSystems []profile.System,
	budget int64,
	options Options,
) (providers.RepositoryAnalysisResult, error) {
	targetTokens := targetedRemoteInputTokens
	if options.Provider == providers.Ollama {
		targetTokens = targetedLocalInputTokens
	}
	if options.MaxInputTokens > 0 && options.MaxInputTokens < targetTokens {
		targetTokens = options.MaxInputTokens
	}
	targetBudget := sourceBudget(targetTokens, objectives, systems, confirmedUses)
	if targetBudget < budget {
		budget = targetBudget
	}
	selected, considered := targetedRepositoryFiles(repository, graph, evidence, confirmedUses, budget*targetedSourceBudgetPercent/100)
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
		Files: selected, Objectives: objectives, Systems: systems,
		ConfirmedAIUses: bindConfirmedAIUses(confirmedUses, sourceFilePaths(selected)), Graph: graphContext, AllowFollowUp: true,
		MaxOutputTokens: targetedOutputTokens(options.Provider),
	}
	result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
	if err != nil {
		incomplete, canRecover := providers.AsRemoteIncompleteError(err)
		if !canRecover || incomplete.Reason != "max_output_tokens" {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze targeted repository evidence: %w", err)
		}
		recoveryOutputTokens := targetedRecoveryOutputTokens(request.MaxOutputTokens, incomplete)
		if progressErr := progress(options, Progress{
			Stage: "targeted-output-recovery", Completed: 0, Total: 1, Scope: ".", InputBytes: inputBytes,
			Detail: fmt.Sprintf("retry compact output with %d token(s) after %d output token(s), including %d reasoning token(s)", recoveryOutputTokens, incomplete.OutputTokens, incomplete.ReasoningTokens),
		}); progressErr != nil {
			return providers.RepositoryAnalysisResult{}, progressErr
		}
		request.AllowFollowUp = false
		request.OutputRecovery = true
		request.MaxOutputTokens = recoveryOutputTokens
		result, err = reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("recover targeted repository output: %w", err)
		}
		result.OutputRecoveryUsed = true
		addUsage(&result.Usage, providers.Usage{
			PromptTokens: incomplete.InputTokens, CompletionTokens: incomplete.OutputTokens, ReasoningTokens: incomplete.ReasoningTokens,
		})
		result.Notes = append(result.Notes, fmt.Sprintf("The initial targeted response exhausted its output allowance. ComplyScan used its sole second call for a terse no-follow-up recovery response with medium reasoning and an output allowance of %d tokens.", request.MaxOutputTokens))
		if progressErr := progress(options, Progress{
			Stage: "targeted-output-recovery", Completed: 1, Total: 1, Scope: ".", InputBytes: inputBytes,
			Detail: "compact structured result completed",
		}); progressErr != nil {
			return providers.RepositoryAnalysisResult{}, progressErr
		}
	}
	if err := validateSystemAttribution(result.Result, profileSystems, options.Ownership, confirmedUses); err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	if result.FollowUpPlan.Needed {
		result.FollowUpRequested = true
		result.FollowUpQueries = repositorySearchQueryLabels(result.FollowUpPlan.Queries)
		followUpFiles := targetedFollowUpFiles(repository, graph, result.FollowUpPlan, selected, budget)
		result.FollowUpExcerpts = len(followUpFiles)
		if len(followUpFiles) > 0 {
			if err := progress(options, Progress{
				Stage: "targeted-follow-up", Completed: 0, Total: 1, Scope: ".",
				InputBytes: sourceFileBytes(followUpFiles), Detail: fmt.Sprintf("retrieved %d bounded excerpt(s)", len(followUpFiles)),
			}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
			finalFiles := append(append([]providers.RepositorySourceFile(nil), selected...), followUpFiles...)
			finalGraph := repositoryGraphContext(graph, finalFiles)
			request.Files = finalFiles
			request.ConfirmedAIUses = bindConfirmedAIUses(confirmedUses, sourceFilePaths(finalFiles))
			request.Graph = finalGraph
			request.AllowFollowUp = false
			final, finalErr := reviewRepositoryWithRetry(ctx, reviewer, request, options)
			if finalErr != nil {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze targeted repository follow-up: %w", finalErr)
			}
			if err := validateSystemAttribution(final.Result, profileSystems, options.Ownership, confirmedUses); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
			addUsage(&final.Usage, result.Usage)
			final.FollowUpRequested = true
			final.FollowUpQueries = result.FollowUpQueries
			final.FollowUpExcerpts = len(followUpFiles)
			result = final
			selected = finalFiles
			if err := progress(options, Progress{
				Stage: "targeted-follow-up", Completed: 1, Total: 1, Scope: ".",
				InputBytes: requestContextBytes(finalFiles, finalGraph), Detail: fmt.Sprintf("reviewed %d bounded excerpt(s)", len(followUpFiles)),
			}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
		} else {
			result.Notes = append(result.Notes, "The model requested a bounded follow-up, but trusted local retrieval found no new eligible excerpt; the initial grounded result was retained.")
		}
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

func targetedOutputTokens(provider providers.Kind) int {
	if provider == providers.Ollama {
		return maximumRecoveryOutput
	}
	return targetedRemoteOutputTokens
}

func targetedRecoveryOutputTokens(current int, incomplete *providers.RemoteIncompleteError) int {
	if incomplete == nil || incomplete.TokenLimit <= 0 || incomplete.InputTokens <= 0 {
		return current
	}
	available := incomplete.TokenLimit - incomplete.InputTokens
	if available > providers.OpenAIMaxOutputTokens {
		available = providers.OpenAIMaxOutputTokens
	}
	if available <= current {
		return current
	}
	return available
}

func targetedFollowUpFiles(repository discovery.Repository, graph codegraph.Graph, plan providers.TechnicalSearchPlan, existing []providers.RepositorySourceFile, budget int64) []providers.RepositorySourceFile {
	selectedPaths := make(map[string]struct{}, len(existing))
	for _, file := range existing {
		selectedPaths[file.Path] = struct{}{}
	}
	result := make([]providers.RepositorySourceFile, 0, len(plan.Queries))
	for _, query := range plan.Queries {
		term := strings.ToLower(strings.TrimSpace(query.Text))
		hint := strings.ToLower(strings.TrimSpace(filepath.ToSlash(query.PathHint)))
		if term == "" {
			continue
		}
		for _, file := range repository.Files {
			if !targetedFileKind(file.Kind) {
				continue
			}
			if _, exists := selectedPaths[file.Path]; exists {
				continue
			}
			if hint != "" && !strings.Contains(strings.ToLower(filepath.ToSlash(file.Path)), hint) {
				continue
			}
			content := strings.ReplaceAll(string(file.Content), "\r\n", "\n")
			match := strings.Index(strings.ToLower(content), term)
			if match < 0 {
				continue
			}
			line := strings.Count(content[:match], "\n") + 1
			prepared := targetedSourceFile(file, []int{line}, 2_000)
			trial := append(append(append([]providers.RepositorySourceFile(nil), existing...), result...), prepared)
			if requestContextBytes(trial, repositoryGraphContext(graph, trial)) > budget {
				continue
			}
			result = append(result, prepared)
			selectedPaths[file.Path] = struct{}{}
			break
		}
		if len(result) >= targetedMaxFollowUpExcerpts {
			break
		}
	}
	return result
}

func repositorySearchQueryLabels(queries []providers.TechnicalSearchQuery) []string {
	result := make([]string, 0, len(queries))
	for _, query := range queries {
		label := query.Text
		if query.PathHint != "" {
			label += " in " + query.PathHint
		}
		result = append(result, label)
	}
	return result
}

func targetedRepositoryFiles(repository discovery.Repository, graph codegraph.Graph, reports []framework.TechnicalEvidenceReport, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64) ([]providers.RepositorySourceFile, int) {
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
	for _, use := range confirmedUses {
		definition := aiuse.Use{Paths: append([]string(nil), use.Paths...)}
		for path, file := range files {
			if targetedFileKind(file.Kind) && aiuse.UseMatchesPath(definition, path) {
				add(path, 1, 70)
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
