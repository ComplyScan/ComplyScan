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
	targetedMaximumFileBytes    = 6_000
	targetedContextLines        = 60
	targetedMaxFollowUpExcerpts = 3
	targetedRemoteInputTokens   = 6_500
	targetedLocalInputTokens    = 16_000
	targetedRemoteOutputTokens  = 4_096
)

type targetedCandidate struct {
	path        string
	tier        targetedCandidateTier
	score       int
	anchor      int
	anchorTier  targetedCandidateTier
	anchorScore int
}

// targetedCandidateTier keeps executable AI workflows and their structural
// neighborhood ahead of passive provider catalogues. ScopeRuntime only means
// that a signal appeared outside a test path; it does not prove execution.
type targetedCandidateTier int

const (
	targetedReferenceCandidate targetedCandidateTier = iota + 1
	targetedImportCandidate
	targetedEvidenceCandidate
	targetedWorkflowCandidate
	targetedInvocationCandidate
)

var targetedSDKInvocationMarkers = []string{
	".responses.create",
	".responses.new",
	".chat.completions.create",
	".completions.new",
	".embeddings.create",
	".embeddings.new",
	".messages.create",
	".messages.new",
	".generate_content",
	".generatecontent",
	".generatetext",
	".streamtext",
	"ollama.chat",
	"ollama.generate",
	".invoke",
	".ainvoke",
	".predict",
	".apredict",
	".complete",
	".acomplete",
}

var targetedTransportMarkers = []string{
	"http.newrequest",
	"http.newrequestwithcontext",
	"httprequest.newbuilder",
	"requests.post",
	"requests.request",
	"urllib.request",
	"fetch",
	"axios.post",
	"client.do",
}

var targetedGenerationEndpointMarkers = []string{
	"/v1/responses",
	"/v1/messages",
	"/v1beta/interactions",
	"/chat/completions",
	"/api/chat",
	":generatecontent",
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
	selected, considered := targetedRepositoryCandidateFiles(repository, graph, evidence, confirmedUses)
	if len(selected) == 0 {
		return providers.RepositoryAnalysisResult{
			Provider: options.Provider, Model: options.Model,
			Coverage: providers.RepositoryCoverage{
				Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
			},
			Result: providers.RepositorySectionResult{
				Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
				UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{
					"No eligible AI implementation or technical-objective candidate could be selected for model review.",
				},
			},
			Notes: []string{
				"Targeted analysis found no eligible structural anchors, so no repository source was sent to the model.",
				"Deterministic inventory and technical evidence remain available; absence of a selected anchor does not prove that the repository contains no AI implementation.",
			},
		}, nil
	}
	// Source that already exceeds one encoded request can never be made to fit
	// by trimming optional graph metadata. Enter the bounded batch queue before
	// constructing the all-candidate graph, avoiding quadratic trimming work on
	// large repositories.
	graphContext := providers.RepositoryGraphContext{}
	inputBytes := requestContextBytes(selected, graphContext)
	if inputBytes <= budget {
		graphContext = boundedRepositoryGraphContext(graph, selected, budget)
		inputBytes = requestContextBytes(selected, graphContext)
	}
	if inputBytes > budget {
		batchOptions := options
		batchOptions.TargetedBatches = true
		result, err := runHierarchical(ctx, reviewer, repository, graph, selected, objectives, systems, confirmedUses, budget, batchOptions)
		result.Notes = append(result.Notes,
			fmt.Sprintf("Targeted analysis queued all %d structural candidate file excerpt(s) instead of treating one model request as a repository-wide evidence cap.", considered),
			"Each source batch stayed within the provider request boundary; files outside the deterministic candidate set were not reviewed by the model.",
		)
		return result, err
	}
	if err := progress(options, Progress{
		Stage: "targeted-selection", Completed: len(selected), Total: considered, Scope: ".", InputBytes: inputBytes,
		Detail: fmt.Sprintf("selected %d of %d structural candidate file(s)", len(selected), considered),
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
	audit := providers.RepositoryAnalysisResult{
		Provider: options.Provider, Model: options.Model,
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
		},
	}
	recordResult := func(value providers.RepositoryAnalysisResult) {
		audit.Coverage.FilesSubmitted += value.Coverage.FilesSubmitted
		audit.Coverage.BytesSubmitted += value.Coverage.BytesSubmitted
		addUsage(&audit.Usage, value.Usage)
		audit.Coverage.CitationsChecked += value.Coverage.CitationsChecked
	}
	recordErrorResult := func(value providers.RepositoryAnalysisResult, err error) {
		audit.Coverage.FilesSubmitted += value.Coverage.FilesSubmitted
		audit.Coverage.BytesSubmitted += value.Coverage.BytesSubmitted
		addUsage(&audit.Usage, value.Usage)
		if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && usageIsZero(value.Usage) {
			addUsage(&audit.Usage, usageFromIncomplete(incomplete))
		}
	}
	partialFailure := func(stage string, cause error) (providers.RepositoryAnalysisResult, error) {
		audit.Result = providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
			UnresolvedQuestions: []string{"The targeted AI review did not complete, so no model-authored repository conclusion was retained."},
		}
		audit.Notes = []string{"Source-transfer coverage and known token usage include attempted requests; incomplete model answers were discarded."}
		return audit, fmt.Errorf("%s: %w", stage, cause)
	}
	applyAudit := func(value providers.RepositoryAnalysisResult) providers.RepositoryAnalysisResult {
		value.Provider = options.Provider
		value.Model = options.Model
		value.Coverage.Mode = providers.RepositoryAnalysisTargeted
		value.Coverage.RepositoryFiles = audit.Coverage.RepositoryFiles
		value.Coverage.RepositoryBytes = audit.Coverage.RepositoryBytes
		value.Coverage.FilesSubmitted = audit.Coverage.FilesSubmitted
		value.Coverage.BytesSubmitted = audit.Coverage.BytesSubmitted
		value.Coverage.CitationsChecked = audit.Coverage.CitationsChecked
		value.Usage = audit.Usage
		return value
	}
	fallBackToBatches := func(files []providers.RepositorySourceFile, cause error) (providers.RepositoryAnalysisResult, error) {
		batchOptions := options
		batchOptions.TargetedBatches = true
		if progressErr := progress(options, Progress{
			Stage: "adaptive-split", Scope: ".", InputBytes: requestContextBytes(files, providers.RepositoryGraphContext{}),
			Detail: "the provider limit still rejects the compact package; switching to bounded source batches",
		}); progressErr != nil {
			return partialFailure("prepare bounded targeted batches", progressErr)
		}
		batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systems, confirmedUses, budget, batchOptions)
		batched.Coverage.FilesSubmitted += audit.Coverage.FilesSubmitted
		batched.Coverage.BytesSubmitted += audit.Coverage.BytesSubmitted
		batched.Coverage.CitationsChecked += audit.Coverage.CitationsChecked
		addUsage(&batched.Usage, audit.Usage)
		batched.Notes = append(batched.Notes, "The initial compact package exceeded the provider token limit, so ComplyScan continued with bounded source batches instead of dropping selected evidence.")
		if batchErr != nil {
			return batched, fmt.Errorf("continue targeted review after compact-package limit (%v): %w", cause, batchErr)
		}
		return batched, nil
	}
	result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
	if err != nil {
		recordErrorResult(result, err)
		if rateLimit, tooLarge := providers.AsRemoteRateLimitError(err); tooLarge && rateLimit.RequestTooLarge {
			reducedOutput := repositoryOutputTokens(options.Provider, rateLimit.LimitTokens)
			if reducedOutput < request.MaxOutputTokens {
				if progressErr := progress(options, Progress{
					Stage: "adaptive-limit-retry", Scope: ".", InputBytes: inputBytes,
					Detail: fmt.Sprintf("provider limit %d tokens; reduce output allowance from %d to %d", rateLimit.LimitTokens, request.MaxOutputTokens, reducedOutput),
				}); progressErr != nil {
					return partialFailure("prepare targeted provider-limit retry", progressErr)
				}
				request.MaxOutputTokens = reducedOutput
				result, err = reviewRepositoryWithRetry(ctx, reviewer, request, options)
				if err != nil {
					recordErrorResult(result, err)
				}
			}
			if err != nil {
				if repeated, stillTooLarge := providers.AsRemoteRateLimitError(err); stillTooLarge && repeated.RequestTooLarge {
					return fallBackToBatches(selected, err)
				}
			}
		}
	}
	if err != nil {
		incomplete, canRecover := providers.AsRemoteIncompleteError(err)
		if !canRecover || incomplete.Reason != "max_output_tokens" {
			return partialFailure("analyze targeted repository evidence", err)
		}
		recoveryOutputTokens := targetedRecoveryOutputTokens(request.MaxOutputTokens, incomplete)
		if progressErr := progress(options, Progress{
			Stage: "targeted-output-recovery", Completed: 0, Total: 1, Scope: ".", InputBytes: inputBytes,
			Detail: fmt.Sprintf("retry compact output with %d token(s) after %d output token(s), including %d reasoning token(s)", recoveryOutputTokens, incomplete.OutputTokens, incomplete.ReasoningTokens),
		}); progressErr != nil {
			return partialFailure("prepare targeted repository output recovery", progressErr)
		}
		request.AllowFollowUp = false
		request.OutputRecovery = true
		request.MaxOutputTokens = recoveryOutputTokens
		result, err = reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			recordErrorResult(result, err)
			return partialFailure("recover targeted repository output", err)
		}
		recordResult(result)
		result.OutputRecoveryUsed = true
		result.Notes = append(result.Notes, fmt.Sprintf("The initial targeted response exhausted its output allowance. ComplyScan used its sole second call for a terse no-follow-up recovery response with medium reasoning and an output allowance of %d tokens.", request.MaxOutputTokens))
		if progressErr := progress(options, Progress{
			Stage: "targeted-output-recovery", Completed: 1, Total: 1, Scope: ".", InputBytes: inputBytes,
			Detail: "compact structured result completed",
		}); progressErr != nil {
			return partialFailure("complete targeted repository output recovery", progressErr)
		}
	} else {
		recordResult(result)
	}
	if err := validateSystemAttribution(result.Result, profileSystems, options.Ownership, confirmedUses); err != nil {
		return partialFailure("validate targeted repository attribution", err)
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
				return partialFailure("prepare targeted repository follow-up", err)
			}
			finalFiles := append(append([]providers.RepositorySourceFile(nil), selected...), followUpFiles...)
			finalGraph := repositoryGraphContext(graph, finalFiles)
			request.Files = finalFiles
			request.ConfirmedAIUses = bindConfirmedAIUses(confirmedUses, sourceFilePaths(finalFiles))
			request.Graph = finalGraph
			request.AllowFollowUp = false
			final, finalErr := reviewRepositoryWithRetry(ctx, reviewer, request, options)
			if finalErr != nil {
				recordErrorResult(final, finalErr)
				if rateLimit, tooLarge := providers.AsRemoteRateLimitError(finalErr); tooLarge && rateLimit.RequestTooLarge {
					reducedOutput := repositoryOutputTokens(options.Provider, rateLimit.LimitTokens)
					if reducedOutput < request.MaxOutputTokens {
						if progressErr := progress(options, Progress{
							Stage: "adaptive-limit-retry", Scope: ".", InputBytes: requestContextBytes(finalFiles, finalGraph),
							Detail: fmt.Sprintf("follow-up package reached provider limit %d tokens; reduce output allowance from %d to %d", rateLimit.LimitTokens, request.MaxOutputTokens, reducedOutput),
						}); progressErr != nil {
							return partialFailure("prepare targeted follow-up provider-limit retry", progressErr)
						}
						request.MaxOutputTokens = reducedOutput
						final, finalErr = reviewRepositoryWithRetry(ctx, reviewer, request, options)
						if finalErr != nil {
							recordErrorResult(final, finalErr)
						}
					}
					if finalErr != nil {
						if repeated, stillTooLarge := providers.AsRemoteRateLimitError(finalErr); stillTooLarge && repeated.RequestTooLarge {
							return fallBackToBatches(finalFiles, finalErr)
						}
					}
				}
			}
			if finalErr != nil {
				incomplete, canRecover := providers.AsRemoteIncompleteError(finalErr)
				if !canRecover || incomplete.Reason != "max_output_tokens" {
					return partialFailure("analyze targeted repository follow-up", finalErr)
				}
				recoveryOutput := targetedRecoveryOutputTokens(request.MaxOutputTokens, incomplete)
				if recoveryOutput <= request.MaxOutputTokens || !outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, incomplete.TokenLimit) {
					return fallBackToBatches(finalFiles, finalErr)
				}
				if progressErr := progress(options, Progress{
					Stage: "adaptive-output-retry", Scope: ".", InputBytes: requestContextBytes(finalFiles, finalGraph),
					Detail: fmt.Sprintf("increase follow-up output allowance from %d to %d tokens", request.MaxOutputTokens, recoveryOutput),
				}); progressErr != nil {
					return partialFailure("prepare targeted follow-up output recovery", progressErr)
				}
				request.MaxOutputTokens = recoveryOutput
				request.OutputRecovery = true
				final, finalErr = reviewRepositoryWithRetry(ctx, reviewer, request, options)
				if finalErr != nil {
					recordErrorResult(final, finalErr)
					if repeated, stillTooLarge := providers.AsRemoteRateLimitError(finalErr); stillTooLarge && repeated.RequestTooLarge {
						return fallBackToBatches(finalFiles, finalErr)
					}
					return partialFailure("recover targeted repository follow-up output", finalErr)
				}
				final.OutputRecoveryUsed = true
			}
			recordResult(final)
			if err := validateSystemAttribution(final.Result, profileSystems, options.Ownership, confirmedUses); err != nil {
				return partialFailure("validate targeted repository follow-up attribution", err)
			}
			final.FollowUpRequested = true
			final.FollowUpQueries = result.FollowUpQueries
			final.FollowUpExcerpts = len(followUpFiles)
			result = final
			selected = finalFiles
			if err := progress(options, Progress{
				Stage: "targeted-follow-up", Completed: 1, Total: 1, Scope: ".",
				InputBytes: requestContextBytes(finalFiles, finalGraph), Detail: fmt.Sprintf("reviewed %d bounded excerpt(s)", len(followUpFiles)),
			}); err != nil {
				return partialFailure("complete targeted repository follow-up", err)
			}
		} else {
			result.Notes = append(result.Notes, "The model requested a bounded follow-up, but trusted local retrieval found no new eligible excerpt; the initial grounded result was retained.")
		}
	}
	observations, err := namespaceSubsystemCandidateIDs(result.Result, 0, ".")
	if err != nil {
		return partialFailure("assign targeted evidence observation identities", err)
	}
	grouped, err := assignSynthesisCandidateIDs(observations, confirmedUses)
	if err != nil {
		return partialFailure("assign targeted inferred-use identities", err)
	}
	result.Result = grouped
	result.Coverage.Mode = providers.RepositoryAnalysisTargeted
	result.Notes = append(result.Notes,
		fmt.Sprintf("Targeted analysis selected %d of %d structural candidate file(s) from %d discovered repository file(s).", len(selected), considered, len(repository.Files)),
		"Selection prioritized executable AI call paths, their bounded caller and safeguard neighborhood, technical-objective matches, confirmed AI-use paths, and then passive provider references.",
		"Files outside the evidence package were not reviewed by the model; absence of model evidence is not proof that an implementation is absent.",
	)
	result = applyAudit(result)
	if err := progress(options, Progress{Stage: "targeted-analysis", Completed: 1, Total: 1, Scope: ".", InputBytes: inputBytes}); err != nil {
		return partialFailure("complete targeted repository analysis", err)
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

func targetedRepositoryCandidates(repository discovery.Repository, graph codegraph.Graph, reports []framework.TechnicalEvidenceReport, confirmedUses []providers.RepositoryConfirmedAIUse) (map[string]discovery.File, []targetedCandidate) {
	files := make(map[string]discovery.File, len(repository.Files))
	for _, file := range repository.Files {
		files[file.Path] = file
	}
	candidates := make(map[string]*targetedCandidate)
	add := func(path string, line int, tier targetedCandidateTier, score int) {
		file, exists := files[path]
		if !exists || !targetedFileKind(file.Kind) {
			return
		}
		candidate := candidates[path]
		if candidate == nil {
			candidate = &targetedCandidate{path: path}
			candidates[path] = candidate
		}
		if tier > candidate.tier || tier == candidate.tier && score > candidate.score {
			candidate.tier, candidate.score = tier, score
		}
		if line > 0 && (candidate.anchor == 0 || tier > candidate.anchorTier ||
			tier == candidate.anchorTier && (score > candidate.anchorScore || score == candidate.anchorScore && line < candidate.anchor)) {
			candidate.anchor, candidate.anchorTier, candidate.anchorScore = line, tier, score
		}
	}
	addContext := func(value codegraph.ContextPackage, tier targetedCandidateTier, score int) {
		if value.Anchor != nil {
			add(value.Anchor.Path, value.Anchor.StartLine, tier, score)
		}
		for _, related := range value.RelatedSymbols {
			add(related.Path, related.StartLine, tier, score-10)
		}
		for _, relationship := range value.Relationships {
			add(relationship.Path, relationship.Line, tier, score-15)
		}
	}

	for _, signal := range inventory.Analyze(repository) {
		file, exists := files[signal.Path]
		if !exists || !targetedInventoryAnchor(file.Kind, signal) {
			continue
		}
		tier, score := targetedReferenceCandidate, 55
		if signal.EvidenceType == inventory.EvidenceImport {
			tier, score = targetedImportCandidate, 70
		}
		if signal.Scope == inventory.ScopeTest {
			tier, score = targetedReferenceCandidate, 45
		}
		anchor := signal.Line
		if signal.Scope == inventory.ScopeRuntime && file.Kind == discovery.KindSource {
			if invocationLine := targetedInvocationAnchor(file, graph); invocationLine > 0 {
				tier, score, anchor = targetedInvocationCandidate, 120, invocationLine
			}
		}
		add(signal.Path, anchor, tier, score)
		if file.Kind == discovery.KindSource && anchor > 0 {
			contextTier := tier
			if tier == targetedInvocationCandidate {
				contextTier = targetedWorkflowCandidate
			}
			addContext(graph.ContextFor(signal.Path, anchor, 30), contextTier, score-5)
		}
	}
	for _, report := range reports {
		for _, objective := range report.Objectives {
			for _, match := range objective.Matches {
				add(match.Path, match.StartLine, targetedEvidenceCandidate, 90)
				addContext(match.Context, targetedEvidenceCandidate, 85)
			}
		}
	}
	for _, use := range confirmedUses {
		definition := aiuse.Use{Paths: append([]string(nil), use.Paths...)}
		for path, file := range files {
			if targetedFileKind(file.Kind) && aiuse.UseMatchesPath(definition, path) {
				add(path, 1, targetedEvidenceCandidate, 80)
			}
		}
	}
	for depth := 0; depth < 2; depth++ {
		known := make(map[string]targetedCandidate, len(candidates))
		for path, candidate := range candidates {
			known[path] = *candidate
		}
		for _, imported := range graph.Imports {
			matchedPaths := targetedImportedPaths(imported.Path, imported.ImportedPath, files)
			if candidate, exists := known[imported.Path]; exists && candidate.tier >= targetedImportCandidate {
				for _, path := range matchedPaths {
					add(path, 1, lowerTargetedTier(candidate.tier), candidate.score-20)
				}
			}
			for _, path := range matchedPaths {
				if candidate, exists := known[path]; exists && candidate.tier >= targetedImportCandidate {
					add(imported.Path, 1, lowerTargetedTier(candidate.tier), candidate.score-20)
				}
			}
		}
	}
	if len(candidates) == 0 {
		for _, symbol := range graph.Symbols {
			if symbol.EntryPoint && symbol.Reachability != codegraph.ReachableTestOnly {
				add(symbol.Path, symbol.StartLine, targetedReferenceCandidate, 35)
			}
		}
	}

	ordered := make([]targetedCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, *candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return targetedCandidateBefore(ordered[i], ordered[j])
	})
	return files, ordered
}

// targetedRepositoryCandidateFiles prepares every structurally selected file.
// The returned list is not a single-request package: callers must partition it
// into provider-sized batches. This keeps a per-request safety boundary without
// turning that boundary into a repository-wide evidence cap.
func targetedRepositoryCandidateFiles(repository discovery.Repository, graph codegraph.Graph, reports []framework.TechnicalEvidenceReport, confirmedUses []providers.RepositoryConfirmedAIUse) ([]providers.RepositorySourceFile, int) {
	files, ordered := targetedRepositoryCandidates(repository, graph, reports, confirmedUses)
	selected := make([]providers.RepositorySourceFile, 0, len(ordered))
	for _, candidate := range ordered {
		selected = append(selected, targetedSourceFile(files[candidate.path], []int{candidate.anchor}, targetedCandidateMaximumBytes(candidate.tier)))
	}
	return selected, len(ordered)
}

func targetedCandidateMaximumBytes(tier targetedCandidateTier) int {
	switch tier {
	case targetedInvocationCandidate:
		return targetedMaximumFileBytes
	case targetedWorkflowCandidate:
		return 2_500
	case targetedEvidenceCandidate:
		return 3_000
	case targetedImportCandidate:
		return 1_800
	default:
		return 1_200
	}
}

func targetedRepositoryFiles(repository discovery.Repository, graph codegraph.Graph, reports []framework.TechnicalEvidenceReport, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64) ([]providers.RepositorySourceFile, int) {
	files, ordered := targetedRepositoryCandidates(repository, graph, reports, confirmedUses)

	selected := make([]providers.RepositorySourceFile, 0, len(ordered))
	selectedPaths := make(map[string]struct{}, len(ordered))
	selectCandidate := func(candidate targetedCandidate) {
		if _, exists := selectedPaths[candidate.path]; exists {
			return
		}
		remaining := budget - requestContextBytes(selected, repositoryGraphContext(graph, selected))
		if remaining <= 256 {
			return
		}
		maximum := targetedMaximumFileBytes
		if int64(maximum) > remaining/2 {
			maximum = int(remaining / 2)
		}
		if maximum < 256 {
			return
		}
		prepared := targetedSourceFile(files[candidate.path], []int{candidate.anchor}, maximum)
		trial := append(append([]providers.RepositorySourceFile(nil), selected...), prepared)
		if requestContextBytes(trial, repositoryGraphContext(graph, trial)) > budget {
			return
		}
		selected = trial
		selectedPaths[candidate.path] = struct{}{}
	}
	// A human-confirmed use is stronger scope information than a speculative
	// graph neighbor. Give each confirmed use one representative before the
	// global ranked fill so it cannot silently disappear from a tight request.
	for _, candidate := range targetedConfirmedReservations(ordered, confirmedUses) {
		selectCandidate(candidate)
	}
	for _, candidate := range ordered {
		selectCandidate(candidate)
	}
	return selected, len(ordered)
}

func targetedCandidateBefore(left, right targetedCandidate) bool {
	if left.tier != right.tier {
		return left.tier > right.tier
	}
	if left.score != right.score {
		return left.score > right.score
	}
	return left.path < right.path
}

func targetedConfirmedReservations(ordered []targetedCandidate, confirmedUses []providers.RepositoryConfirmedAIUse) []targetedCandidate {
	reservedPaths := make(map[string]struct{}, len(confirmedUses))
	result := make([]targetedCandidate, 0, len(confirmedUses))
	for _, use := range confirmedUses {
		definition := aiuse.Use{Paths: append([]string(nil), use.Paths...)}
		for _, candidate := range ordered {
			if _, reserved := reservedPaths[candidate.path]; reserved || !aiuse.UseMatchesPath(definition, candidate.path) {
				continue
			}
			result = append(result, candidate)
			reservedPaths[candidate.path] = struct{}{}
			break
		}
	}
	return result
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
	if anchor > len(lines) {
		anchor = len(lines)
	}
	anchorIndex := anchor - 1
	if len(lines[anchorIndex]) > maximum {
		return providers.RepositorySourceFile{
			Path: file.Path, Kind: string(file.Kind), LineCount: lineCount, ContentStartLine: anchor,
			Content: lines[anchorIndex][:maximum],
		}
	}
	start, end := anchorIndex, anchorIndex+1
	bytes := len(lines[anchorIndex])
	beforeBlocked, afterBlocked := false, false
	for (!beforeBlocked || !afterBlocked) && (anchorIndex-start < targetedContextLines || end-anchorIndex-1 < targetedContextLines) {
		if !afterBlocked && end-anchorIndex-1 < targetedContextLines && end < len(lines) {
			lineBytes := len(lines[end]) + 1
			if bytes+lineBytes <= maximum {
				bytes += lineBytes
				end++
			} else {
				afterBlocked = true
			}
		} else {
			afterBlocked = true
		}
		if !beforeBlocked && anchorIndex-start < targetedContextLines && start > 0 {
			lineBytes := len(lines[start-1]) + 1
			if bytes+lineBytes <= maximum {
				bytes += lineBytes
				start--
			} else {
				beforeBlocked = true
			}
		} else {
			beforeBlocked = true
		}
	}
	return providers.RepositorySourceFile{
		Path: file.Path, Kind: string(file.Kind), LineCount: lineCount, ContentStartLine: start + 1, Content: strings.Join(lines[start:end], "\n"),
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

func targetedInvocationAnchor(file discovery.File, graph codegraph.Graph) int {
	if file.Kind != discovery.KindSource {
		return 0
	}
	content := strings.ToLower(strings.ReplaceAll(string(file.Content), "\r\n", "\n"))
	lines := strings.Split(content, "\n")
	endpointNames := targetedGenerationEndpointNames(lines)
	semanticTransportLine := 0
	for _, edge := range graph.Edges {
		if edge.Path != file.Path {
			continue
		}
		label := strings.ToLower(strings.ReplaceAll(edge.Label, " ", ""))
		for _, marker := range targetedSDKInvocationMarkers {
			if strings.HasSuffix(label, marker) {
				return edge.Line
			}
		}
		if edge.Line > 0 && edge.Line <= len(lines) && (containsAnyTargetedMarker(lines[edge.Line-1], targetedGenerationEndpointMarkers) ||
			containsAnyTargetedMarker(lines[edge.Line-1], endpointNames)) {
			return edge.Line
		}
		if semanticTransportLine == 0 && containsAnyTargetedMarker(label, targetedTransportMarkers) {
			context := graph.ContextFor(file.Path, edge.Line, 1)
			if context.Anchor != nil && targetedGenerationSymbol(context.Anchor.QualifiedName) {
				semanticTransportLine = edge.Line
			}
		}
	}
	return semanticTransportLine
}

func containsAnyTargetedMarker(value string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func targetedGenerationEndpointNames(lines []string) []string {
	var names []string
	for _, line := range lines {
		if !containsAnyTargetedMarker(line, targetedGenerationEndpointMarkers) {
			continue
		}
		assignment := strings.Index(line, "=")
		if assignment < 0 {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line[:assignment]))
		if len(fields) == 0 || targetedControlKeyword(strings.Trim(fields[0], " \t:,")) {
			continue
		}
		for _, field := range fields {
			name := strings.Trim(field, " \t:,")
			switch name {
			case "const", "var", "let", "static", "final", "public", "private", "protected":
				continue
			}
			if targetedIdentifier(name) && !containsTargetedString(names, name) {
				names = append(names, name)
				break
			}
		}
	}
	return names
}

func targetedControlKeyword(value string) bool {
	switch value {
	case "if", "else", "elif", "for", "while", "switch", "case", "return", "when", "match", "catch", "except":
		return true
	default:
		return false
	}
}

func containsTargetedString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func targetedIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if character == '_' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' && index > 0 {
			continue
		}
		return false
	}
	return true
}

func targetedGenerationSymbol(value string) bool {
	value = strings.ToLower(value)
	return containsAnyTargetedMarker(value, []string{"chat", "completion", "generate", "inference", "interaction", "message", "predict", "response"})
}

func lowerTargetedTier(tier targetedCandidateTier) targetedCandidateTier {
	if tier <= targetedReferenceCandidate {
		return targetedReferenceCandidate
	}
	return tier - 1
}

func importedPathMatchesFile(importedPath, filePath string) bool {
	return targetedImportedPathMatchScore(importedPath, filePath) > 0
}

func targetedImportedPaths(importerPath, importedPath string, files map[string]discovery.File) []string {
	importedPath = targetedResolvedImportPath(importerPath, importedPath)
	if strings.EqualFold(filepath.Ext(importerPath), ".go") {
		cleaned := strings.Trim(strings.TrimSpace(strings.ReplaceAll(importedPath, "\\", "/")), `"'`)
		// Bare Go imports are standard-library or external package names, never a
		// repository-relative filename. In particular, `context` must not match a
		// local file such as internal/codegraph/context.go.
		if !strings.Contains(cleaned, "/") {
			return nil
		}
	}
	bestScore := 0
	var result []string
	for path := range files {
		score := targetedImportedPathMatchScore(importedPath, path)
		if score == 0 || score < bestScore {
			continue
		}
		if score > bestScore {
			bestScore = score
			result = result[:0]
		}
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func targetedResolvedImportPath(importerPath, importedPath string) string {
	value := strings.Trim(strings.TrimSpace(strings.ReplaceAll(importedPath, "\\", "/")), `"'`)
	if importerPath == "" || !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "../") {
		return value
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.ToSlash(importerPath)), value)))
}

func targetedImportedPathMatchScore(importedPath, filePath string) int {
	importedPath = strings.Trim(strings.TrimSpace(strings.ReplaceAll(importedPath, "\\", "/")), `"'`)
	filePath = trimTargetedSourceExtension(strings.ReplaceAll(filePath, "\\", "/"))
	filePath = strings.TrimSuffix(filePath, "/index")
	importedPath = trimTargetedSourceExtension(importedPath)
	importedPath = strings.TrimPrefix(importedPath, "./")
	importedPath = strings.TrimSuffix(importedPath, "/index")
	if importedPath == "" || filePath == "" {
		return 0
	}
	primaryVariants := []string{importedPath}
	var fallbackVariants []string
	if !strings.Contains(importedPath, "/") && strings.Contains(strings.TrimLeft(importedPath, "."), ".") {
		dotted := strings.ReplaceAll(strings.TrimLeft(importedPath, "."), ".", "/")
		primaryVariants = append(primaryVariants, dotted)
		if strings.Contains(dotted, "/") {
			parent := strings.TrimSuffix(dotted, "/"+filepath.Base(dotted))
			fallbackVariants = append(fallbackVariants, parent)
		}
	}
	for _, variant := range primaryVariants {
		if score := targetedPathVariantScore(variant, filePath); score > 0 {
			return score
		}
	}
	for _, variant := range fallbackVariants {
		if score := targetedPathVariantScore(variant, filePath); score > 1 {
			return score - 1
		}
	}
	return 0
}

func targetedPathVariantScore(importedPath, filePath string) int {
	switch {
	case filePath == importedPath:
		return 4
	case strings.HasSuffix(filePath, "/"+importedPath):
		return 3
	case strings.HasSuffix(importedPath, "/"+filePath):
		return 2
	default:
		return 0
	}
}

func trimTargetedSourceExtension(path string) string {
	lower := strings.ToLower(path)
	for _, extension := range []string{".py", ".pyi", ".go", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".java", ".kt", ".kts"} {
		if strings.HasSuffix(lower, extension) {
			return path[:len(path)-len(extension)]
		}
	}
	return path
}
