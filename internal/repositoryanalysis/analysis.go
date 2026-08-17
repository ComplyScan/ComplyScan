// Package repositoryanalysis prepares safe targeted model context by default
// and retains explicit broad one-pass and hierarchical analysis modes.
package repositoryanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const (
	DefaultRemoteInputTokens   = 180_000
	DefaultLocalInputTokens    = 24_000
	minimumInputTokens         = 8_000
	charactersPerToken         = 3
	contextReservePercent      = 20
	minimumRateLimitCooldown   = 60 * time.Second
	maxRateLimitTotalWait      = 10 * time.Minute
	minimumAdaptiveOutput      = 1024
	minimumRecoveryOutput      = 4096
	maximumRecoveryOutput      = 8192
	maxConcurrentSourceBatches = 32
	sourceBatchTokenOverhead   = 8192
)

type Reviewer interface {
	ReviewRepository(context.Context, providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error)
}

type Mode string

const (
	ModeAuto         Mode = "auto"
	ModeTargeted     Mode = "targeted"
	ModeDeep         Mode = "deep"
	ModeFull         Mode = "full"
	ModeHierarchical Mode = "hierarchical"
)

type Options struct {
	Mode              Mode
	MaxInputTokens    int
	Provider          providers.Kind
	Model             string
	Ownership         []ownership.Rule
	ConfirmedAIUses   []providers.RepositoryConfirmedAIUse
	TargetedBatches   bool
	OnProgress        func(Progress) error
	Wait              func(context.Context, time.Duration) error
	InitialRateLimits providers.RateLimitSnapshot
	ProbeRateLimits   func(context.Context) (providers.RateLimitSnapshot, error)
}

type Progress struct {
	Stage        string
	Completed    int
	Total        int
	Scope        string
	InputBytes   int64
	Wait         time.Duration
	OriginalWait time.Duration
	Detail       string
}

// Run selects compact structural evidence in auto and targeted modes. Deep,
// full, and hierarchical modes retain broad repository analysis.
func Run(ctx context.Context, reviewer Reviewer, repository discovery.Repository, evidence []framework.TechnicalEvidenceReport, systems []profile.System, options Options) (providers.RepositoryAnalysisResult, error) {
	if reviewer == nil {
		return providers.RepositoryAnalysisResult{}, errors.New("repository analysis reviewer is required")
	}
	if options.Mode == "" {
		options.Mode = ModeAuto
	}
	if options.Mode != ModeAuto && options.Mode != ModeTargeted && options.Mode != ModeDeep && options.Mode != ModeFull && options.Mode != ModeHierarchical {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("unsupported repository analysis mode %q", options.Mode)
	}
	if options.MaxInputTokens == 0 {
		options.MaxInputTokens = DefaultRemoteInputTokens
		if options.Provider == providers.Ollama {
			options.MaxInputTokens = DefaultLocalInputTokens
		}
	}
	if options.MaxInputTokens < minimumInputTokens {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("repository analysis input budget must be at least %d tokens", minimumInputTokens)
	}
	files := repositoryFiles(repository)
	if len(files) == 0 {
		return providers.RepositoryAnalysisResult{
			Provider: options.Provider, Model: options.Model,
			Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository)},
			Result:   providers.RepositorySectionResult{Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
			Notes:    []string{"No eligible text files were available for repository model analysis."},
		}, nil
	}
	objectives := repositoryObjectives(evidence)
	systemContext := repositorySystems(systems, options.Ownership)
	confirmedUses := append([]providers.RepositoryConfirmedAIUse(nil), options.ConfirmedAIUses...)
	budget := sourceBudget(options.MaxInputTokens, objectives, systemContext, confirmedUses)
	graph := codegraph.Build(repository)
	if options.Mode == ModeAuto || options.Mode == ModeTargeted {
		return runTargeted(ctx, reviewer, repository, graph, evidence, objectives, systemContext, confirmedUses, systems, budget, options)
	}
	if options.Mode == ModeDeep {
		options.Mode = ModeAuto
	}
	fullGraph := repositoryGraphContext(graph, files)
	fullBytes := requestContextBytes(files, fullGraph)
	if options.Mode != ModeHierarchical && fullBytes <= budget {
		if err := progress(options, Progress{Stage: "full-repository", Completed: 0, Total: 1, Scope: ".", InputBytes: fullBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		request := providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisFull, Scope: ".", RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
			MaxOutputTokens: repositoryOutputTokens(options.Provider, 0), Files: files, Objectives: objectives, Systems: systemContext,
			ConfirmedAIUses: bindConfirmedAIUses(confirmedUses, sourceFilePaths(files)), Graph: fullGraph,
		}
		result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge && options.Mode == ModeAuto {
				batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, options)
				mergeRepositoryAttemptAccounting(&batched, result)
				batched.Notes = append(batched.Notes, "The initial broad request exceeded the provider limit; its attempted source transfer and known usage are included before the hierarchical retry.")
				return batched, batchErr
			}
			if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && incomplete.Reason == "max_output_tokens" && options.Mode == ModeAuto {
				batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, options)
				mergeRepositoryAttemptAccounting(&batched, result)
				batched.Notes = append(batched.Notes, "The initial broad response exhausted its output allowance; its attempted source transfer and known usage are included before the hierarchical retry.")
				return batched, batchErr
			}
			return incompleteRepositoryAttempt(result, options, repository, "The broad repository AI review did not complete, so no model-authored conclusion was retained."), err
		}
		if err := validateSystemAttribution(result.Result, systems, options.Ownership, confirmedUses); err != nil {
			return incompleteRepositoryAttempt(result, options, repository, "The broad repository response failed trusted attribution validation, so no model-authored conclusion was retained."), err
		}
		observations, err := namespaceSubsystemCandidateIDs(result.Result, 0, ".")
		if err != nil {
			return incompleteRepositoryAttempt(result, options, repository, "The broad repository response could not be assigned trusted observation identities, so no model-authored conclusion was retained."), err
		}
		grouped, err := assignSynthesisCandidateIDs(observations, confirmedUses)
		if err != nil {
			return incompleteRepositoryAttempt(result, options, repository, "The broad repository response could not be assigned trusted inferred-use identities, so no model-authored conclusion was retained."), err
		}
		result.Result = grouped
		if err := progress(options, Progress{Stage: "full-repository", Completed: 1, Total: 1, Scope: ".", InputBytes: fullBytes}); err != nil {
			return incompleteRepositoryAttempt(result, options, repository, "The broad repository review completed at the provider but local completion was interrupted, so no model-authored conclusion was retained."), err
		}
		result.Notes = append(result.Notes, "The complete relevant discovered repository fit in one model request.")
		return result, nil
	}
	if options.Mode == ModeFull {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("relevant repository context is %d bytes, exceeding the configured full-analysis budget of %d bytes", fullBytes, budget)
	}
	return runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, options)
}

func incompleteRepositoryAttempt(result providers.RepositoryAnalysisResult, options Options, repository discovery.Repository, detail string) providers.RepositoryAnalysisResult {
	if result.Provider == providers.None {
		result.Provider = options.Provider
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = options.Model
	}
	if result.Coverage.Mode == "" {
		result.Coverage.Mode = providers.RepositoryAnalysisFull
	}
	result.Coverage.RepositoryFiles = len(repository.Files)
	result.Coverage.RepositoryBytes = repositorySize(repository)
	result.Result = providers.RepositorySectionResult{
		Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
		UnresolvedQuestions: []string{detail},
	}
	result.Notes = append(result.Notes, "Attempted source-transfer coverage and known provider usage are retained for diagnostics; incomplete model conclusions are discarded.")
	return result
}

func mergeRepositoryAttemptAccounting(target *providers.RepositoryAnalysisResult, attempt providers.RepositoryAnalysisResult) {
	if target == nil {
		return
	}
	target.Coverage.FilesSubmitted += attempt.Coverage.FilesSubmitted
	target.Coverage.BytesSubmitted += attempt.Coverage.BytesSubmitted
	target.Coverage.CitationsChecked += attempt.Coverage.CitationsChecked
	addUsage(&target.Usage, attempt.Usage)
}

func runHierarchical(ctx context.Context, reviewer Reviewer, repository discovery.Repository, graph codegraph.Graph, files []providers.RepositorySourceFile, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64, options Options) (providers.RepositoryAnalysisResult, error) {
	chunks, err := partitionRepository(files, budget*80/100)
	if err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	summaries := make([]providers.RepositorySectionResult, 0, len(chunks))
	aggregate := providers.RepositoryAnalysisResult{
		Provider: options.Provider,
		Model:    options.Model,
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisSubsystem, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
		},
	}
	if options.TargetedBatches {
		aggregate.Coverage.Mode = providers.RepositoryAnalysisTargeted
	}
	sourceBatchesCompleted := 0
	prefetched := make(map[string]repositorySourceBatchResponse)
	partialFailure := func(scope string, cause error) (providers.RepositoryAnalysisResult, error) {
		for key, response := range prefetched {
			mergeRepositoryAttemptAccounting(&aggregate, response.result)
			delete(prefetched, key)
		}
		aggregate.Coverage.Subsystems = sourceBatchesCompleted
		aggregate.Coverage.SourceBatchesCompleted = sourceBatchesCompleted
		aggregate.Coverage.SourceBatchesTotal = len(chunks)
		detail := fmt.Sprintf("repository model review completed %d of %d bounded source batch(es)", sourceBatchesCompleted, len(chunks))
		status := detail + "; remaining candidate evidence was not reviewed and completed batches were not globally synthesized"
		if sourceBatchesCompleted == len(chunks) {
			status = detail + "; all candidate evidence batches were reviewed, but global synthesis did not complete"
		}
		// Validated subsystem answers are not a valid global answer until synthesis
		// reconciles duplicate confirmed-use facts and objective observations. Keep
		// only truthful transfer/completion coverage when the run stops early.
		aggregate.Result = providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
			UnresolvedQuestions: []string{status},
		}
		aggregate.Notes = append(aggregate.Notes, detail+" before the provider review stopped. This partial result must not be treated as a completed zero-use review.")
		if aggregate.Coverage.FilesSubmitted > 0 {
			aggregate.Notes = append(aggregate.Notes, "Attempted-transfer coverage includes every concurrently started source request, including responses that were not retained after another batch failed.")
		}
		return aggregate, fmt.Errorf("%s: %s: %w", detail, scope, cause)
	}
	sourceStage := "subsystem"
	if options.TargetedBatches {
		sourceStage = "targeted-batch"
		if err := progress(options, Progress{Stage: "targeted-batch-queue", Total: len(chunks), Scope: "."}); err != nil {
			return partialFailure("targeted evidence queue", err)
		}
	}
	adaptiveTokenLimit := 0
	observedLimits := options.InitialRateLimits
	if len(chunks) > 1 && !observedLimits.Available() && options.ProbeRateLimits != nil {
		if err := progress(options, Progress{Stage: "rate-limit-probe", Total: len(chunks), Scope: ".", Detail: "checking live provider capacity without repository source"}); err != nil {
			return partialFailure("provider capacity probe", err)
		}
		probed, probeErr := options.ProbeRateLimits(ctx)
		if probeErr != nil || !probed.Available() {
			detail := "the provider returned no usable capacity headers; the first source batch will calibrate this run"
			if probeErr != nil {
				detail = "the source-free capacity probe did not complete; the first source batch will calibrate this run: " + probeErr.Error()
			}
			if err := progress(options, Progress{Stage: "rate-limit-probe-fallback", Total: len(chunks), Scope: ".", Detail: detail}); err != nil {
				return partialFailure("provider capacity probe", err)
			}
		} else {
			observedLimits = probed
			if err := progress(options, Progress{Stage: "rate-limit-probe-complete", Total: len(chunks), Scope: ".", Detail: rateLimitCapacityDetail(probed)}); err != nil {
				return partialFailure("provider capacity probe", err)
			}
		}
	}
	capacityWait := time.Duration(0)
	for index := 0; index < len(chunks); {
		chunk := chunks[index]
		prepared := prepareRepositorySourceBatch(chunk, repository, graph, objectives, systems, confirmedUses, budget, adaptiveTokenLimit, options)
		if prepared.inputBytes > budget && chunk.maxOutputTokens == 0 {
			parts, split := splitRepositoryChunk(chunk, 0)
			if !split {
				return partialFailure(chunk.scope, fmt.Errorf("requires %d encoded context bytes and cannot be safely divided to fit the per-request budget of %d bytes", prepared.inputBytes, budget))
			}
			chunks = replaceRepositoryChunk(chunks, index, parts)
			if err := progress(options, Progress{
				Stage: "adaptive-context-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
				InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("encoded request exceeded %d bytes; split into %d smaller part(s)", budget, len(parts)),
			}); err != nil {
				return partialFailure(chunk.scope, err)
			}
			continue
		}

		batchResponse, ready := prefetched[chunk.scope]
		if ready {
			delete(prefetched, chunk.scope)
		} else {
			waveLimit, wait := sourceBatchWaveLimit(observedLimits, prepared.estimatedTokens, len(chunks)-index)
			if waveLimit == 0 {
				if wait <= 0 {
					wait = minimumRateLimitCooldown
				}
				if capacityWait+wait > maxRateLimitTotalWait {
					return partialFailure(chunk.scope, fmt.Errorf("provider capacity remained exhausted beyond the %s automatic wait budget", maxRateLimitTotalWait))
				}
				capacityWait += wait
				if err := progress(options, Progress{Stage: "batch-capacity-wait", Scope: chunk.scope, Wait: wait, OriginalWait: wait, Detail: "waiting for provider request/token capacity before starting the next source batch wave"}); err != nil {
					return partialFailure(chunk.scope, err)
				}
				if err := waitForConfiguredRateLimit(ctx, options, wait); err != nil {
					return partialFailure(chunk.scope, err)
				}
				observedLimits = replenishRateLimitSnapshot(observedLimits, wait)
				continue
			}

			wave := make([]repositorySourceBatchCall, 0, waveLimit)
			estimatedTokens := 0
			for candidateIndex := index; candidateIndex < len(chunks) && len(wave) < waveLimit; candidateIndex++ {
				candidate := chunks[candidateIndex]
				if _, alreadyStarted := prefetched[candidate.scope]; alreadyStarted {
					break
				}
				candidateCall := prepareRepositorySourceBatch(candidate, repository, graph, objectives, systems, confirmedUses, budget, adaptiveTokenLimit, options)
				if candidateCall.inputBytes > budget && candidate.maxOutputTokens == 0 {
					break
				}
				if observedLimits.TokensKnown && len(wave) > 0 && estimatedTokens+candidateCall.estimatedTokens > observedLimits.RemainingTokens {
					break
				}
				candidateCall.index = candidateIndex
				wave = append(wave, candidateCall)
				estimatedTokens += candidateCall.estimatedTokens
			}
			if len(wave) == 0 {
				wave = append(wave, prepared)
			}
			if len(wave) > 1 {
				if err := progress(options, Progress{
					Stage: "targeted-batch-concurrency", Completed: len(wave), Total: len(chunks) - index, Scope: chunk.scope,
					Detail: fmt.Sprintf("provider response headers allow %d source batches to run concurrently", len(wave)),
				}); err != nil {
					return partialFailure(chunk.scope, err)
				}
			}
			for _, call := range wave {
				startStage := sourceStage
				startCompleted := call.index
				if options.TargetedBatches {
					startStage = "targeted-batch-start"
					startCompleted = call.index + 1
				}
				if err := progress(options, Progress{Stage: startStage, Completed: startCompleted, Total: len(chunks), Scope: call.chunk.scope, InputBytes: call.inputBytes}); err != nil {
					return partialFailure(call.chunk.scope, err)
				}
			}
			observedLimits = reserveRateLimitSnapshot(observedLimits, len(wave), estimatedTokens)
			responses := runRepositorySourceBatchWave(ctx, reviewer, wave, options)
			for _, response := range responses {
				prefetched[response.scope] = response
				observedLimits = conservativeRateLimitSnapshot(observedLimits, response.result.RateLimits)
			}
			batchResponse = prefetched[chunk.scope]
			delete(prefetched, chunk.scope)
		}

		result, err := batchResponse.result, batchResponse.err
		aggregate.Coverage.FilesSubmitted += result.Coverage.FilesSubmitted
		aggregate.Coverage.BytesSubmitted += result.Coverage.BytesSubmitted
		if err != nil {
			addUsage(&aggregate.Usage, result.Usage)
			incomplete, isIncomplete := providers.AsRemoteIncompleteError(err)
			if isIncomplete && usageIsZero(result.Usage) {
				addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
			}
			if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge {
				adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, rateLimit.LimitTokens)
				reducedOutput := repositorySourceOutputTokens(options, adaptiveTokenLimit)
				if reducedOutput < prepared.maxOutputTokens {
					chunks[index].maxOutputTokens = reducedOutput
					if err := progress(options, Progress{
						Stage: "adaptive-limit-retry", Completed: index, Total: len(chunks), Scope: chunk.scope,
						InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("provider limit %d tokens; reduce output allowance from %d to %d", rateLimit.LimitTokens, prepared.maxOutputTokens, reducedOutput),
					}); err != nil {
						return partialFailure(chunk.scope, err)
					}
					continue
				}
				parts, split := splitRepositoryChunk(chunk, reducedOutput)
				if !split {
					return partialFailure(chunk.scope, fmt.Errorf("provider token limit %d is too small for the smallest safe repository segment: %w", rateLimit.LimitTokens, err))
				}
				chunks = replaceRepositoryChunk(chunks, index, parts)
				if err := progress(options, Progress{
					Stage: "adaptive-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
					InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("provider limit %d tokens; split into %d smaller part(s)", rateLimit.LimitTokens, len(parts)),
				}); err != nil {
					return partialFailure(chunk.scope, err)
				}
				continue
			}
			if isIncomplete && incomplete.Reason == "max_output_tokens" {
				recoveryOutput := repositoryRecoveryOutputTokens(prepared.maxOutputTokens)
				if recoveryOutput > prepared.maxOutputTokens && outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit) {
					chunks[index].maxOutputTokens = recoveryOutput
					if err := progress(options, Progress{
						Stage: "adaptive-output-retry", Completed: index, Total: len(chunks), Scope: chunk.scope,
						InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("increase output allowance from %d to %d tokens", prepared.maxOutputTokens, recoveryOutput),
					}); err != nil {
						return partialFailure(chunk.scope, err)
					}
					continue
				}
				parts, split := splitRepositoryChunk(chunk, recoveryOutput)
				if !split {
					return partialFailure(chunk.scope, fmt.Errorf("model exhausted %d output tokens for the smallest safe repository segment: %w", prepared.maxOutputTokens, err))
				}
				chunks = replaceRepositoryChunk(chunks, index, parts)
				if err := progress(options, Progress{
					Stage: "adaptive-output-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
					InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("output limit %d tokens; split into %d smaller part(s) with %d output tokens", prepared.maxOutputTokens, len(parts), recoveryOutput),
				}); err != nil {
					return partialFailure(chunk.scope, err)
				}
				continue
			}
			return partialFailure(chunk.scope, err)
		}
		addUsage(&aggregate.Usage, result.Usage)
		aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
		if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership, confirmedUses); err != nil {
			return partialFailure(chunk.scope, err)
		}
		namespaced, err := namespaceSubsystemCandidateIDs(result.Result, index, chunk.scope)
		if err != nil {
			return partialFailure(chunk.scope, err)
		}
		summaries = append(summaries, namespaced)
		sourceBatchesCompleted++
		if err := progress(options, Progress{Stage: sourceStage, Completed: index + 1, Total: len(chunks), Scope: chunk.scope, InputBytes: prepared.inputBytes}); err != nil {
			return partialFailure(chunk.scope, err)
		}
		index++
	}
	levels := 0
	for len(summaries) > 1 {
		levels++
		plainGroups := partitionSummaries(summaries, budget)
		groups := make([]repositorySummaryGroup, len(plainGroups))
		for index := range plainGroups {
			groups[index] = repositorySummaryGroup{summaries: plainGroups[index]}
		}
		next := make([]providers.RepositorySectionResult, 0, len(groups))
		for index := 0; index < len(groups); {
			group := groups[index]
			scope := fmt.Sprintf("synthesis-level-%d-part-%d", levels, index+1)
			if err := progress(options, Progress{Stage: "synthesis", Completed: index, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group.summaries)}); err != nil {
				return partialFailure(scope, err)
			}
			maxOutputTokens := group.maxOutputTokens
			if maxOutputTokens == 0 {
				maxOutputTokens = repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
			}
			fileIndex := citedFileIndex(group.summaries, files)
			request := providers.RepositoryAnalysisRequest{
				Mode: providers.RepositoryAnalysisSynthesis, Scope: scope,
				RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: fileIndex,
				Objectives: objectives, Systems: systems, ConfirmedAIUses: bindConfirmedAIUsesForSynthesis(confirmedUses, fileReferencePaths(fileIndex), group.summaries), SubsystemSummaries: group.summaries, MaxOutputTokens: maxOutputTokens,
			}
			result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
			if err != nil {
				addUsage(&aggregate.Usage, result.Usage)
				incomplete, isIncomplete := providers.AsRemoteIncompleteError(err)
				if isIncomplete && usageIsZero(result.Usage) {
					addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
				}
				if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge {
					adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, rateLimit.LimitTokens)
					reducedOutput := repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
					if reducedOutput < maxOutputTokens {
						groups[index].maxOutputTokens = reducedOutput
						if err := progress(options, Progress{
							Stage: "adaptive-limit-retry", Completed: index, Total: len(groups), Scope: scope,
							InputBytes: summaryBytes(group.summaries), Detail: fmt.Sprintf("provider limit %d tokens; reduce output allowance from %d to %d", rateLimit.LimitTokens, maxOutputTokens, reducedOutput),
						}); err != nil {
							return partialFailure(scope, err)
						}
						continue
					}
					parts, split := splitRepositorySummaryGroup(group, reducedOutput)
					if !split {
						return partialFailure(scope, fmt.Errorf("provider token limit %d is too small for one subsystem summary: %w", rateLimit.LimitTokens, err))
					}
					groups = replaceRepositorySummaryGroup(groups, index, parts)
					if err := progress(options, Progress{
						Stage: "adaptive-split", Completed: index, Total: len(groups), Scope: scope,
						InputBytes: summaryBytes(group.summaries), Detail: fmt.Sprintf("provider limit %d tokens; split synthesis into %d smaller part(s)", rateLimit.LimitTokens, len(parts)),
					}); err != nil {
						return partialFailure(scope, err)
					}
					continue
				}
				if isIncomplete && incomplete.Reason == "max_output_tokens" {
					recoveryOutput := repositoryRecoveryOutputTokens(maxOutputTokens)
					if recoveryOutput > maxOutputTokens && outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit) {
						groups[index].maxOutputTokens = recoveryOutput
						if err := progress(options, Progress{
							Stage: "adaptive-output-retry", Completed: index, Total: len(groups), Scope: scope,
							InputBytes: summaryBytes(group.summaries), Detail: fmt.Sprintf("increase synthesis output allowance from %d to %d tokens", maxOutputTokens, recoveryOutput),
						}); err != nil {
							return partialFailure(scope, err)
						}
						continue
					}
					parts, split := splitRepositorySummaryGroup(group, recoveryOutput)
					if !split {
						return partialFailure(scope, fmt.Errorf("model exhausted %d output tokens and the synthesis input cannot be divided further: %w", maxOutputTokens, err))
					}
					groups = replaceRepositorySummaryGroup(groups, index, parts)
					if err := progress(options, Progress{
						Stage: "adaptive-output-split", Completed: index, Total: len(groups), Scope: scope,
						InputBytes: summaryBytes(group.summaries), Detail: fmt.Sprintf("synthesis output limit %d tokens; split into %d smaller part(s) with %d output tokens", maxOutputTokens, len(parts), recoveryOutput),
					}); err != nil {
						return partialFailure(scope, err)
					}
					continue
				}
				return partialFailure(scope, err)
			}
			addUsage(&aggregate.Usage, result.Usage)
			aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
			if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership, confirmedUses); err != nil {
				return partialFailure(scope, err)
			}
			grouped, err := assignSynthesisCandidateIDs(result.Result, confirmedUses)
			if err != nil {
				return partialFailure(scope, err)
			}
			next = append(next, grouped)
			if err := progress(options, Progress{Stage: "synthesis", Completed: index + 1, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group.summaries)}); err != nil {
				return partialFailure(scope, err)
			}
			index++
		}
		if len(next) >= len(summaries) {
			// A first synthesis level can legitimately contain one oversized
			// subsystem summary per request. In that case the number of
			// summaries does not fall, but each request can compact its summary
			// enough for the next level to combine several of them. Only stop
			// when the compacted results still cannot be grouped more tightly;
			// otherwise a large, fully reviewed repository is incorrectly marked
			// incomplete immediately after the compaction pass.
			if len(partitionSummaries(next, budget)) >= len(next) {
				return partialFailure(fmt.Sprintf("synthesis level %d", levels), errors.New("repository synthesis could not reduce subsystem summaries within the configured input budget"))
			}
		}
		summaries = next
	}
	if len(summaries) == 1 && len(chunks) == 1 {
		// A forced hierarchical analysis still gets an explicit synthesis pass so
		// its output follows the same global contract as larger repositories.
		group := summaries
		maxOutputTokens := repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
		var result providers.RepositoryAnalysisResult
		for {
			fileIndex := citedFileIndex(group, files)
			request := providers.RepositoryAnalysisRequest{
				Mode: providers.RepositoryAnalysisSynthesis, Scope: "repository-synthesis",
				RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: fileIndex,
				Objectives: objectives, Systems: systems, ConfirmedAIUses: bindConfirmedAIUsesForSynthesis(confirmedUses, fileReferencePaths(fileIndex), group), SubsystemSummaries: group, MaxOutputTokens: maxOutputTokens,
			}
			result, err = reviewRepositoryWithRetry(ctx, reviewer, request, options)
			if err == nil {
				break
			}
			addUsage(&aggregate.Usage, result.Usage)
			if rateLimit, rateLimited := providers.AsRemoteRateLimitError(err); rateLimited && rateLimit.RequestTooLarge {
				adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, rateLimit.LimitTokens)
				reducedOutput := repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
				if reducedOutput < maxOutputTokens {
					if progressErr := progress(options, Progress{
						Stage: "adaptive-limit-retry", Scope: "repository-synthesis", InputBytes: summaryBytes(group),
						Detail: fmt.Sprintf("provider limit %d tokens; reduce output allowance from %d to %d", rateLimit.LimitTokens, maxOutputTokens, reducedOutput),
					}); progressErr != nil {
						return partialFailure("repository synthesis", progressErr)
					}
					maxOutputTokens = reducedOutput
					continue
				}
				return partialFailure("repository synthesis", fmt.Errorf("provider token limit %d is too small for the final bounded synthesis: %w", rateLimit.LimitTokens, err))
			}
			incomplete, ok := providers.AsRemoteIncompleteError(err)
			if ok && usageIsZero(result.Usage) {
				addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
			}
			recoveryOutput := repositoryRecoveryOutputTokens(maxOutputTokens)
			if !ok || incomplete.Reason != "max_output_tokens" || recoveryOutput <= maxOutputTokens || !outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit) {
				return partialFailure("repository synthesis", err)
			}
			if progressErr := progress(options, Progress{
				Stage: "adaptive-output-retry", Scope: "repository-synthesis", InputBytes: summaryBytes(group),
				Detail: fmt.Sprintf("increase synthesis output allowance from %d to %d tokens", maxOutputTokens, recoveryOutput),
			}); progressErr != nil {
				return partialFailure("repository synthesis", progressErr)
			}
			maxOutputTokens = recoveryOutput
		}
		addUsage(&aggregate.Usage, result.Usage)
		aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
		if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership, confirmedUses); err != nil {
			return partialFailure("repository synthesis", err)
		}
		grouped, err := assignSynthesisCandidateIDs(result.Result, confirmedUses)
		if err != nil {
			return partialFailure("repository synthesis", err)
		}
		summaries[0] = grouped
	}
	if options.TargetedBatches {
		aggregate.Coverage.Mode = providers.RepositoryAnalysisTargeted
	} else {
		aggregate.Coverage.Mode = providers.RepositoryAnalysisSynthesis
	}
	aggregate.Coverage.RepositoryFiles = len(repository.Files)
	aggregate.Coverage.RepositoryBytes = repositorySize(repository)
	aggregate.Coverage.Subsystems = len(chunks)
	aggregate.Coverage.SourceBatchesCompleted = len(chunks)
	aggregate.Coverage.SourceBatchesTotal = len(chunks)
	aggregate.Result = summaries[0]
	aggregate.Result.Scope = "."
	if options.TargetedBatches {
		aggregate.Notes = []string{
			fmt.Sprintf("All %d structurally selected source batch(es) were reviewed within the per-request provider boundary and then globally synthesized.", len(chunks)),
			"Batch boundaries are a context-management mechanism, not declared AI-system boundaries; files outside the local structural candidate set were not model-reviewed.",
			"Repository model analysis is advisory; deterministic findings remain available for comparison.",
		}
	} else {
		aggregate.Notes = []string{
			fmt.Sprintf("The repository exceeded the one-request context budget and was analyzed as %d subsystem slice(s) followed by global synthesis.", len(chunks)),
			"Subsystem boundaries are a context-management mechanism, not declared AI-system boundaries.",
			"Repository-wide model analysis is advisory; deterministic findings and the bounded evidence investigation remain available for comparison.",
		}
	}
	return aggregate, nil
}

type repositorySummaryGroup struct {
	summaries       []providers.RepositorySectionResult
	maxOutputTokens int
}

type repositoryChunk struct {
	scope           string
	files           []providers.RepositorySourceFile
	maxOutputTokens int
}

type repositorySourceBatchCall struct {
	index           int
	chunk           repositoryChunk
	request         providers.RepositoryAnalysisRequest
	inputBytes      int64
	estimatedTokens int
	maxOutputTokens int
}

type repositorySourceBatchResponse struct {
	scope  string
	result providers.RepositoryAnalysisResult
	err    error
}

func prepareRepositorySourceBatch(chunk repositoryChunk, repository discovery.Repository, graph codegraph.Graph, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64, adaptiveTokenLimit int, options Options) repositorySourceBatchCall {
	chunkGraph := boundedRepositoryGraphContext(graph, chunk.files, budget)
	inputBytes := requestContextBytes(chunk.files, chunkGraph)
	maxOutputTokens := chunk.maxOutputTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = repositorySourceOutputTokens(options, adaptiveTokenLimit)
	}
	requestMode := providers.RepositoryAnalysisSubsystem
	if options.TargetedBatches {
		requestMode = providers.RepositoryAnalysisTargeted
	}
	request := providers.RepositoryAnalysisRequest{
		Mode: requestMode, Scope: chunk.scope,
		RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), Files: chunk.files,
		Objectives: objectives, Systems: systems, ConfirmedAIUses: bindConfirmedAIUses(confirmedUses, sourceFilePaths(chunk.files)), Graph: chunkGraph, MaxOutputTokens: maxOutputTokens,
	}
	requestBytes := inputBytes
	if encoded, err := json.Marshal(request); err == nil {
		requestBytes = int64(len(encoded))
	}
	estimatedTokens := int(requestBytes)/charactersPerToken + maxOutputTokens + sourceBatchTokenOverhead
	return repositorySourceBatchCall{
		chunk: chunk, request: request, inputBytes: inputBytes,
		estimatedTokens: estimatedTokens, maxOutputTokens: maxOutputTokens,
	}
}

func runRepositorySourceBatchWave(ctx context.Context, reviewer Reviewer, calls []repositorySourceBatchCall, options Options) []repositorySourceBatchResponse {
	results := make([]repositorySourceBatchResponse, len(calls))
	if len(calls) == 1 {
		result, err := reviewRepositoryWithRetry(ctx, reviewer, calls[0].request, options)
		results[0] = repositorySourceBatchResponse{scope: calls[0].chunk.scope, result: result, err: err}
		return results
	}

	parallelOptions := options
	var callbackMu sync.Mutex
	if options.OnProgress != nil {
		parallelOptions.OnProgress = func(value Progress) error {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			return options.OnProgress(value)
		}
	}
	if options.Wait != nil {
		parallelOptions.Wait = func(ctx context.Context, delay time.Duration) error {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			return options.Wait(ctx, delay)
		}
	}
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(calls))
	for index := range calls {
		go func(index int) {
			defer waitGroup.Done()
			result, err := reviewRepositoryWithRetry(ctx, reviewer, calls[index].request, parallelOptions)
			results[index] = repositorySourceBatchResponse{scope: calls[index].chunk.scope, result: result, err: err}
		}(index)
	}
	waitGroup.Wait()
	return results
}

func sourceBatchWaveLimit(snapshot providers.RateLimitSnapshot, estimatedTokens, pending int) (int, time.Duration) {
	if pending <= 0 {
		return 0, 0
	}
	if !snapshot.Available() {
		return 1, 0
	}
	limit := pending
	if limit > maxConcurrentSourceBatches {
		limit = maxConcurrentSourceBatches
	}
	var wait time.Duration
	if snapshot.RequestsKnown {
		if snapshot.RemainingRequests <= 0 {
			wait = snapshot.ResetRequests
			limit = 0
		} else if limit > snapshot.RemainingRequests {
			limit = snapshot.RemainingRequests
		}
	}
	if snapshot.TokensKnown {
		if snapshot.RemainingTokens < estimatedTokens && snapshot.LimitTokens >= estimatedTokens {
			if snapshot.ResetTokens > wait {
				wait = snapshot.ResetTokens
			}
			limit = 0
		} else if snapshot.RemainingTokens < estimatedTokens {
			if limit > 1 {
				limit = 1
			}
		} else if estimatedTokens > 0 {
			tokenLimit := snapshot.RemainingTokens / estimatedTokens
			if tokenLimit > 0 && tokenLimit < limit {
				limit = tokenLimit
			}
		}
	}
	return limit, wait
}

func rateLimitCapacityDetail(snapshot providers.RateLimitSnapshot) string {
	parts := make([]string, 0, 2)
	if snapshot.RequestsKnown {
		parts = append(parts, fmt.Sprintf("%d request slot(s) currently remaining", snapshot.RemainingRequests))
	}
	if snapshot.TokensKnown {
		parts = append(parts, fmt.Sprintf("%d token(s) currently remaining", snapshot.RemainingTokens))
	}
	return strings.Join(parts, "; ")
}

func replenishRateLimitSnapshot(snapshot providers.RateLimitSnapshot, waited time.Duration) providers.RateLimitSnapshot {
	if snapshot.RequestsKnown && (snapshot.ResetRequests <= 0 || waited >= snapshot.ResetRequests) {
		snapshot.RemainingRequests = snapshot.LimitRequests
	}
	if snapshot.TokensKnown && (snapshot.ResetTokens <= 0 || waited >= snapshot.ResetTokens) {
		snapshot.RemainingTokens = snapshot.LimitTokens
	}
	return snapshot
}

func reserveRateLimitSnapshot(snapshot providers.RateLimitSnapshot, requests, estimatedTokens int) providers.RateLimitSnapshot {
	if snapshot.RequestsKnown {
		snapshot.RemainingRequests = max(0, snapshot.RemainingRequests-requests)
	}
	if snapshot.TokensKnown {
		snapshot.RemainingTokens = max(0, snapshot.RemainingTokens-estimatedTokens)
	}
	return snapshot
}

func conservativeRateLimitSnapshot(current, next providers.RateLimitSnapshot) providers.RateLimitSnapshot {
	if !next.Available() {
		return current
	}
	if !current.Available() {
		return next
	}
	result := current
	if next.RequestsKnown {
		if !current.RequestsKnown {
			result.RequestsKnown = true
			result.LimitRequests = next.LimitRequests
			result.RemainingRequests = next.RemainingRequests
			result.ResetRequests = next.ResetRequests
		} else {
			result.LimitRequests = smallerPositive(current.LimitRequests, next.LimitRequests)
			result.RemainingRequests = min(current.RemainingRequests, next.RemainingRequests)
			result.ResetRequests = max(current.ResetRequests, next.ResetRequests)
		}
	}
	if next.TokensKnown {
		if !current.TokensKnown {
			result.TokensKnown = true
			result.LimitTokens = next.LimitTokens
			result.RemainingTokens = next.RemainingTokens
			result.ResetTokens = next.ResetTokens
		} else {
			result.LimitTokens = smallerPositive(current.LimitTokens, next.LimitTokens)
			result.RemainingTokens = min(current.RemainingTokens, next.RemainingTokens)
			result.ResetTokens = max(current.ResetTokens, next.ResetTokens)
		}
	}
	return result
}

func waitForConfiguredRateLimit(ctx context.Context, options Options, delay time.Duration) error {
	if options.Wait != nil {
		return options.Wait(ctx, delay)
	}
	return waitForRateLimit(ctx, delay, func(remaining time.Duration) error {
		return progress(options, Progress{Stage: "batch-capacity-wait", Wait: remaining, OriginalWait: delay, Detail: "waiting for provider request/token capacity"})
	})
}

// namespaceSubsystemCandidateIDs replaces model-authored batch keys with
// trusted scan-local observation identities before synthesis. The identity is
// derived from the checked evidence position and local orchestration order,
// not from the model's proposed product name. Confirmed operator-owned IDs are
// not changed.
func namespaceSubsystemCandidateIDs(summary providers.RepositorySectionResult, subsystemIndex int, scope string) (providers.RepositorySectionResult, error) {
	summary.AIUses = append([]providers.RepositoryAIUse(nil), summary.AIUses...)
	summary.AIUseFacts = append([]providers.RepositoryAIUseFactSet(nil), summary.AIUseFacts...)
	rewritten := make(map[string]string, len(summary.AIUses))
	seen := make(map[string]struct{}, len(summary.AIUses))
	for index := range summary.AIUses {
		oldID := summary.AIUses[index].ID
		if oldID == "" {
			return providers.RepositorySectionResult{}, errors.New("cannot namespace an empty subsystem candidate AI-use ID")
		}
		locations := make([]string, 0, len(summary.AIUses[index].Evidence))
		for _, citation := range summary.AIUses[index].Evidence {
			locations = append(locations, fmt.Sprintf("%s:%d", citation.Path, citation.Line))
		}
		sort.Strings(locations)
		seed := fmt.Sprintf("%d\x00%s\x00%d\x00%s", subsystemIndex, scope, index, strings.Join(locations, "\x00"))
		digest := sha256.Sum256([]byte(seed))
		newID := fmt.Sprintf("observation-%04d-%x", subsystemIndex+1, digest[:12])
		if _, duplicate := seen[newID]; duplicate {
			return providers.RepositorySectionResult{}, fmt.Errorf("subsystem candidate namespace collision for %q", oldID)
		}
		seen[newID] = struct{}{}
		rewritten[oldID] = newID
		summary.AIUses[index].ID = newID
		summary.AIUses[index].MemberObservationIDs = []string{newID}
	}
	for index := range summary.AIUseFacts {
		if newID, candidate := rewritten[summary.AIUseFacts[index].AIUseID]; candidate {
			summary.AIUseFacts[index].AIUseID = newID
		}
	}
	return summary, nil
}

// assignSynthesisCandidateIDs replaces temporary model grouping keys with a
// trusted candidate identity derived solely from exact observation membership.
// Names and descriptions may change without changing the join key for the same
// synthesized group. These inferred IDs remain report-local; durable IDs are
// still owned by the optional AI-use register.
func assignSynthesisCandidateIDs(summary providers.RepositorySectionResult, confirmedUses []providers.RepositoryConfirmedAIUse) (providers.RepositorySectionResult, error) {
	summary.AIUses = append([]providers.RepositoryAIUse(nil), summary.AIUses...)
	summary.AIUseFacts = append([]providers.RepositoryAIUseFactSet(nil), summary.AIUseFacts...)
	confirmed := make(map[string]struct{}, len(confirmedUses))
	for _, use := range confirmedUses {
		confirmed[use.ID] = struct{}{}
	}
	rewritten := make(map[string]string, len(summary.AIUses))
	seen := make(map[string]struct{}, len(summary.AIUses))
	for index := range summary.AIUses {
		oldID := summary.AIUses[index].ID
		members := append([]string(nil), summary.AIUses[index].MemberObservationIDs...)
		if oldID == "" || len(members) == 0 {
			return providers.RepositorySectionResult{}, fmt.Errorf("cannot assign trusted ID to incomplete synthesized candidate %q", oldID)
		}
		sort.Strings(members)
		for memberIndex := 1; memberIndex < len(members); memberIndex++ {
			if members[memberIndex] == members[memberIndex-1] {
				return providers.RepositorySectionResult{}, fmt.Errorf("synthesized candidate %q repeats observation %q", oldID, members[memberIndex])
			}
		}
		newID := inferredCandidateID(members)
		if _, collision := confirmed[newID]; collision {
			return providers.RepositorySectionResult{}, fmt.Errorf("trusted inferred candidate ID %q conflicts with a confirmed AI use", newID)
		}
		if _, duplicate := seen[newID]; duplicate {
			return providers.RepositorySectionResult{}, fmt.Errorf("synthesis returned duplicate observation membership for candidate %q", oldID)
		}
		seen[newID] = struct{}{}
		rewritten[oldID] = newID
		summary.AIUses[index].ID = newID
		summary.AIUses[index].MemberObservationIDs = members
	}
	for index := range summary.AIUseFacts {
		if newID, candidate := rewritten[summary.AIUseFacts[index].AIUseID]; candidate {
			summary.AIUseFacts[index].AIUseID = newID
		}
	}
	return summary, nil
}

func inferredCandidateID(sortedObservationIDs []string) string {
	digest := sha256.Sum256([]byte(strings.Join(sortedObservationIDs, "\x00")))
	return fmt.Sprintf("inferred-use-%x", digest[:16])
}

func reviewRepositoryWithRetry(ctx context.Context, reviewer Reviewer, request providers.RepositoryAnalysisRequest, options Options) (providers.RepositoryAnalysisResult, error) {
	totalWait := time.Duration(0)
	var attemptedUsage providers.Usage
	attemptedFiles := 0
	var attemptedSourceBytes int64
	for attempt := 0; ; attempt++ {
		attemptedFiles += len(request.Files)
		attemptedSourceBytes += repositorySourceContentBytes(request.Files)
		result, err := reviewer.ReviewRepository(ctx, request)
		addUsage(&attemptedUsage, result.Usage)
		if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && usageIsZero(result.Usage) {
			addUsage(&attemptedUsage, usageFromIncomplete(incomplete))
		}
		result.Usage = attemptedUsage
		result.Coverage.FilesSubmitted = attemptedFiles
		result.Coverage.BytesSubmitted = attemptedSourceBytes
		if err == nil {
			return result, nil
		}
		rateLimit, ok := providers.AsRemoteRateLimitError(err)
		if !ok || rateLimit.RequestTooLarge {
			return result, err
		}
		delay := rateLimit.RetryAfter
		if delay < minimumRateLimitCooldown {
			delay = minimumRateLimitCooldown
		}
		if totalWait+delay > maxRateLimitTotalWait {
			return result, fmt.Errorf("provider rate limit exceeded the %s automatic wait budget after %d retry cycle(s): %w", maxRateLimitTotalWait, attempt, err)
		}
		totalWait += delay
		if err := progress(options, Progress{
			Stage: "rate-limit-wait", Completed: attempt + 1,
			Scope: request.Scope, Wait: delay, OriginalWait: delay, Detail: "temporary provider token limit",
		}); err != nil {
			return result, err
		}
		if options.Wait != nil {
			if err := options.Wait(ctx, delay); err != nil {
				return result, err
			}
			continue
		}
		if err := waitForRateLimit(ctx, delay, func(remaining time.Duration) error {
			return progress(options, Progress{
				Stage: "rate-limit-wait", Completed: attempt + 1,
				Scope: request.Scope, Wait: remaining, OriginalWait: delay, Detail: "temporary provider token limit",
			})
		}); err != nil {
			return result, err
		}
		if err := progress(options, Progress{
			Stage: "rate-limit-resume", Completed: attempt + 1,
			Scope: request.Scope, OriginalWait: delay, Detail: "temporary provider token limit",
		}); err != nil {
			return result, err
		}
	}
}

func waitForRateLimit(ctx context.Context, delay time.Duration, onTick func(time.Duration) error) error {
	deadline := time.Now().Add(delay)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			remaining := time.Until(deadline)
			if remaining <= 0 {
				continue
			}
			remaining = ((remaining + time.Second - 1) / time.Second) * time.Second
			if onTick != nil {
				if err := onTick(remaining); err != nil {
					return err
				}
			}
		}
	}
}

func repositoryOutputTokens(provider providers.Kind, tokenLimit int) int {
	if provider == providers.Ollama || tokenLimit <= 0 {
		return 8192
	}
	value := tokenLimit / 5
	if value < minimumAdaptiveOutput {
		return minimumAdaptiveOutput
	}
	if value > 4096 {
		return 4096
	}
	return value
}

func repositorySourceOutputTokens(options Options, tokenLimit int) int {
	if options.TargetedBatches && tokenLimit <= 0 {
		return targetedOutputTokens(options.Provider)
	}
	return repositoryOutputTokens(options.Provider, tokenLimit)
}

func repositoryRecoveryOutputTokens(current int) int {
	if current < minimumRecoveryOutput {
		return minimumRecoveryOutput
	}
	if current >= maximumRecoveryOutput {
		return maximumRecoveryOutput
	}
	value := current * 2
	if value > maximumRecoveryOutput {
		return maximumRecoveryOutput
	}
	return value
}

func outputFitsTokenLimit(inputTokens, outputTokens, tokenLimit int) bool {
	return tokenLimit <= 0 || inputTokens+outputTokens <= tokenLimit
}

func smallerPositive(current, candidate int) int {
	if candidate <= 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}

func splitRepositoryChunk(chunk repositoryChunk, maxOutputTokens int) ([]repositoryChunk, bool) {
	if len(chunk.files) > 1 {
		split := balancedFileSplit(chunk.files)
		if split <= 0 || split >= len(chunk.files) {
			return nil, false
		}
		return []repositoryChunk{
			{scope: chunk.scope + " (adaptive part 1)", files: append([]providers.RepositorySourceFile(nil), chunk.files[:split]...), maxOutputTokens: maxOutputTokens},
			{scope: chunk.scope + " (adaptive part 2)", files: append([]providers.RepositorySourceFile(nil), chunk.files[split:]...), maxOutputTokens: maxOutputTokens},
		}, true
	}
	if len(chunk.files) != 1 {
		return nil, false
	}
	left, right, ok := splitRepositorySourceFile(chunk.files[0])
	if !ok {
		return nil, false
	}
	return []repositoryChunk{
		{scope: chunk.scope + " (adaptive segment 1)", files: []providers.RepositorySourceFile{left}, maxOutputTokens: maxOutputTokens},
		{scope: chunk.scope + " (adaptive segment 2)", files: []providers.RepositorySourceFile{right}, maxOutputTokens: maxOutputTokens},
	}, true
}

func balancedFileSplit(files []providers.RepositorySourceFile) int {
	total := sourceFileBytes(files)
	var size int64
	for index := 0; index < len(files)-1; index++ {
		size += sourceFileBytes(files[index : index+1])
		if size >= total/2 {
			return index + 1
		}
	}
	return len(files) / 2
}

func splitRepositorySourceFile(file providers.RepositorySourceFile) (providers.RepositorySourceFile, providers.RepositorySourceFile, bool) {
	if len(file.Content) < 2 {
		return providers.RepositorySourceFile{}, providers.RepositorySourceFile{}, false
	}
	start := file.ContentStartLine
	if start <= 0 {
		start = 1
	}
	middle := len(file.Content) / 2
	leftBreak := strings.LastIndex(file.Content[:middle], "\n")
	rightBreak := strings.Index(file.Content[middle:], "\n")
	split := middle
	if leftBreak >= 0 {
		split = leftBreak + 1
	}
	if rightBreak >= 0 && (leftBreak < 0 || rightBreak < middle-leftBreak) {
		split = middle + rightBreak + 1
	}
	if split <= 0 || split >= len(file.Content) {
		return providers.RepositorySourceFile{}, providers.RepositorySourceFile{}, false
	}
	left, right := file, file
	left.Content = file.Content[:split]
	right.Content = file.Content[split:]
	left.ContentStartLine = start
	right.ContentStartLine = start + strings.Count(left.Content, "\n")
	return left, right, true
}

func replaceRepositoryChunk(values []repositoryChunk, index int, replacements []repositoryChunk) []repositoryChunk {
	result := make([]repositoryChunk, 0, len(values)-1+len(replacements))
	result = append(result, values[:index]...)
	result = append(result, replacements...)
	result = append(result, values[index+1:]...)
	return result
}

func splitRepositorySummaryGroup(group repositorySummaryGroup, maxOutputTokens int) ([]repositorySummaryGroup, bool) {
	if len(group.summaries) < 3 {
		return nil, false
	}
	middle := len(group.summaries) / 2
	return []repositorySummaryGroup{
		{summaries: append([]providers.RepositorySectionResult(nil), group.summaries[:middle]...), maxOutputTokens: maxOutputTokens},
		{summaries: append([]providers.RepositorySectionResult(nil), group.summaries[middle:]...), maxOutputTokens: maxOutputTokens},
	}, true
}

func replaceRepositorySummaryGroup(values []repositorySummaryGroup, index int, replacements []repositorySummaryGroup) []repositorySummaryGroup {
	result := make([]repositorySummaryGroup, 0, len(values)-1+len(replacements))
	result = append(result, values[:index]...)
	result = append(result, replacements...)
	result = append(result, values[index+1:]...)
	return result
}

func partitionRepository(files []providers.RepositorySourceFile, budget int64) ([]repositoryChunk, error) {
	groups := make(map[string][]providers.RepositorySourceFile)
	for _, file := range files {
		parts := strings.Split(filepath.ToSlash(file.Path), "/")
		scope := "repository-root"
		if len(parts) > 1 {
			scope = parts[0]
		}
		groups[scope] = append(groups[scope], file)
	}
	scopes := make([]string, 0, len(groups))
	for scope := range groups {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	var chunks []repositoryChunk
	for _, scope := range scopes {
		part := 1
		current := repositoryChunk{scope: scope}
		var size int64
		for _, file := range groups[scope] {
			segments, err := repositoryFileSegments(file, budget)
			if err != nil {
				return nil, err
			}
			for _, segment := range segments {
				fileSize := int64(len(segment.Content) + len(segment.Path) + 100)
				if len(current.files) > 0 && size+fileSize > budget {
					current.scope = fmt.Sprintf("%s (part %d)", scope, part)
					chunks = append(chunks, current)
					part++
					current = repositoryChunk{scope: scope}
					size = 0
				}
				current.files = append(current.files, segment)
				size += fileSize
			}
		}
		if len(current.files) > 0 {
			if part > 1 {
				current.scope = fmt.Sprintf("%s (part %d)", scope, part)
			}
			chunks = append(chunks, current)
		}
	}
	return chunks, nil
}

func repositoryFileSegments(file providers.RepositorySourceFile, budget int64) ([]providers.RepositorySourceFile, error) {
	queue := []providers.RepositorySourceFile{file}
	result := make([]providers.RepositorySourceFile, 0, 1)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		fileSize := int64(len(current.Content) + len(current.Path) + 100)
		if fileSize <= budget {
			result = append(result, current)
			continue
		}
		left, right, ok := splitRepositorySourceFile(current)
		if !ok {
			return nil, fmt.Errorf("repository file %s requires %d bytes and cannot be safely divided to fit the configured analysis budget of %d bytes", file.Path, fileSize, budget)
		}
		queue = append([]providers.RepositorySourceFile{left, right}, queue...)
	}
	return result, nil
}

func partitionSummaries(values []providers.RepositorySectionResult, budget int64) [][]providers.RepositorySectionResult {
	var result [][]providers.RepositorySectionResult
	var current []providers.RepositorySectionResult
	var size int64
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		valueSize := int64(len(encoded) + 200)
		if len(current) > 0 && size+valueSize > budget {
			result = append(result, current)
			current = nil
			size = 0
		}
		current = append(current, value)
		size += valueSize
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result
}

func repositoryFiles(repository discovery.Repository) []providers.RepositorySourceFile {
	files := make([]providers.RepositorySourceFile, 0, len(repository.Files))
	for _, file := range repository.Files {
		if file.Kind == discovery.KindOtherText {
			continue
		}
		content := rules.RedactSecrets(strings.ReplaceAll(string(file.Content), "\r\n", "\n"))
		files = append(files, providers.RepositorySourceFile{
			Path: file.Path, Kind: string(file.Kind), LineCount: strings.Count(content, "\n") + 1, ContentStartLine: 1, Content: content,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func repositoryObjectives(reports []framework.TechnicalEvidenceReport) []providers.RepositoryObjective {
	var result []providers.RepositoryObjective
	seen := make(map[string]struct{})
	for _, report := range reports {
		for _, objective := range report.Objectives {
			id := report.Pack.ID + "/" + objective.ID
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, providers.RepositoryObjective{
				ID: id, Title: objective.Title, SourceReference: objective.SourceReference,
				Description: objective.Description, Verification: objective.Verification,
			})
		}
	}
	return result
}

func repositorySystems(systems []profile.System, rules []ownership.Rule) []providers.RepositorySystemContext {
	result := make([]providers.RepositorySystemContext, 0, len(systems))
	for _, system := range systems {
		facts := []string{
			"Intended purpose: " + system.IntendedPurpose,
			"Lifecycle stage: " + string(system.LifecycleStage),
			"Decision impact: " + string(system.DecisionImpact),
			"Human oversight: " + string(system.HumanOversight),
		}
		paths := []string{}
		for _, rule := range rules {
			for _, owner := range rule.Systems {
				if owner == system.ID {
					paths = append(paths, rule.Paths...)
					break
				}
			}
		}
		if len(systems) > 1 && len(paths) == 0 {
			facts = append(facts, "Repository path ownership is not established for this system; leave system_id empty unless cited evidence is explicitly owned by this system.")
		}
		result = append(result, providers.RepositorySystemContext{ID: system.ID, Name: system.Name, Paths: paths, DeclaredFacts: facts})
	}
	return result
}

func profileSystems(values []providers.RepositorySystemContext) []profile.System {
	result := make([]profile.System, 0, len(values))
	for _, value := range values {
		result = append(result, profile.System{ID: value.ID, Name: value.Name})
	}
	return result
}

func validateSystemAttribution(result providers.RepositorySectionResult, systems []profile.System, rules []ownership.Rule, confirmedUses []providers.RepositoryConfirmedAIUse) error {
	resolver := ownership.New(rules)
	for _, observation := range result.ObjectiveObservations {
		if observation.AIUseID != "" {
			use, found := confirmedAIUseByID(confirmedUses, observation.AIUseID)
			if !found {
				return fmt.Errorf("objective %q references unknown confirmed AI use %q", observation.ObjectiveID, observation.AIUseID)
			}
			if observation.SystemID != "" && !containsSystem(use.SystemIDs, observation.SystemID) {
				return fmt.Errorf("objective %q attributed confirmed AI use %q to unassociated system %q", observation.ObjectiveID, observation.AIUseID, observation.SystemID)
			}
			definition := aiuse.Use{Paths: append([]string(nil), use.Paths...)}
			for _, citation := range append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...) {
				if !aiuse.UseMatchesPath(definition, citation.Path) {
					return fmt.Errorf("objective %q attributed %s outside confirmed AI use %q paths", observation.ObjectiveID, citation.Path, observation.AIUseID)
				}
			}
			continue
		}
		if len(systems) <= 1 && len(rules) == 0 {
			continue
		}
		if observation.SystemID == "" {
			continue
		}
		citations := append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...)
		if len(citations) == 0 {
			if len(systems) == 1 && systems[0].ID == observation.SystemID {
				// An objective with no claimed repository evidence can safely remain
				// associated with the only configured system. There is no evidence
				// path to own and no competing system to confuse it with.
				continue
			}
			return fmt.Errorf("objective %q attributed system %q without cited evidence", observation.ObjectiveID, observation.SystemID)
		}
		if !resolver.Configured() {
			return fmt.Errorf("objective %q attributed evidence to system %q without configured path ownership", observation.ObjectiveID, observation.SystemID)
		}
		for _, citation := range citations {
			resolution := resolver.Resolve(citation.Path)
			if resolution.Status == ownership.StatusConflicting || !containsSystem(resolution.Systems, observation.SystemID) {
				return fmt.Errorf("objective %q attributed %s to system %q but path ownership is %s for %v", observation.ObjectiveID, citation.Path, observation.SystemID, resolution.Status, resolution.Systems)
			}
		}
	}
	return nil
}

func confirmedAIUseByID(values []providers.RepositoryConfirmedAIUse, id string) (providers.RepositoryConfirmedAIUse, bool) {
	for _, value := range values {
		if value.ID == id {
			return value, true
		}
	}
	return providers.RepositoryConfirmedAIUse{}, false
}

func containsSystem(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sourceBudget(tokens int, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, confirmedUses []providers.RepositoryConfirmedAIUse) int64 {
	overhead, _ := json.Marshal(struct {
		Objectives      []providers.RepositoryObjective
		Systems         []providers.RepositorySystemContext
		ConfirmedAIUses []providers.RepositoryConfirmedAIUse
	}{objectives, systems, confirmedUses})
	total := int64(tokens * charactersPerToken)
	reserve := total * contextReservePercent / 100
	budget := total - reserve - int64(len(overhead))
	if budget < 1 {
		return 1
	}
	return budget
}

func bindConfirmedAIUses(values []providers.RepositoryConfirmedAIUse, submittedPaths []string) []providers.RepositoryConfirmedAIUse {
	result := make([]providers.RepositoryConfirmedAIUse, 0, len(values))
	for _, value := range values {
		copy := value
		copy.SubmittedFiles = nil
		definition := aiuse.Use{Paths: append([]string(nil), value.Paths...)}
		for _, path := range submittedPaths {
			if aiuse.UseMatchesPath(definition, path) {
				copy.SubmittedFiles = append(copy.SubmittedFiles, path)
			}
		}
		if len(copy.SubmittedFiles) == 0 {
			continue
		}
		sort.Strings(copy.SubmittedFiles)
		result = append(result, copy)
	}
	return result
}

// bindConfirmedAIUsesForSynthesis preserves an explicitly reviewed confirmed
// use even when its subsystem returned zero positive facts and therefore cited
// no file. An empty SubmittedFiles scope carries only the reviewed-empty status
// and unresolved questions forward; it cannot authorize a new fact citation.
func bindConfirmedAIUsesForSynthesis(values []providers.RepositoryConfirmedAIUse, citedPaths []string, summaries []providers.RepositorySectionResult) []providers.RepositoryConfirmedAIUse {
	reviewed := make(map[string]struct{})
	for _, summary := range summaries {
		for _, factSet := range summary.AIUseFacts {
			reviewed[factSet.AIUseID] = struct{}{}
		}
	}
	result := make([]providers.RepositoryConfirmedAIUse, 0, len(values))
	for _, value := range values {
		copy := value
		copy.SubmittedFiles = nil
		definition := aiuse.Use{Paths: append([]string(nil), value.Paths...)}
		for _, path := range citedPaths {
			if aiuse.UseMatchesPath(definition, path) {
				copy.SubmittedFiles = append(copy.SubmittedFiles, path)
			}
		}
		if len(copy.SubmittedFiles) == 0 {
			if _, wasReviewed := reviewed[value.ID]; !wasReviewed {
				continue
			}
			copy.SubmittedFiles = []string{}
		}
		sort.Strings(copy.SubmittedFiles)
		result = append(result, copy)
	}
	return result
}

func sourceFilePaths(values []providers.RepositorySourceFile) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Path)
	}
	return result
}

func fileReferencePaths(values []providers.RepositoryFileReference) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Path)
	}
	return result
}

func sourceFileBytes(files []providers.RepositorySourceFile) int64 {
	var size int64
	for _, file := range files {
		size += int64(len(file.Content) + len(file.Path) + 100)
	}
	return size
}

func repositorySourceContentBytes(files []providers.RepositorySourceFile) int64 {
	var size int64
	for _, file := range files {
		size += int64(len(file.Content))
	}
	return size
}

func requestContextBytes(files []providers.RepositorySourceFile, graph providers.RepositoryGraphContext) int64 {
	encoded, _ := json.Marshal(struct {
		Files []providers.RepositorySourceFile `json:"files"`
		Graph providers.RepositoryGraphContext `json:"repository_graph"`
	}{Files: files, Graph: graph})
	return int64(len(encoded))
}

func repositoryGraphContext(graph codegraph.Graph, files []providers.RepositorySourceFile) providers.RepositoryGraphContext {
	type submittedRange struct{ start, end int }
	paths := make(map[string]submittedRange, len(files))
	for _, file := range files {
		start := file.ContentStartLine
		if start <= 0 {
			start = 1
		}
		paths[file.Path] = submittedRange{start: start, end: start + strings.Count(file.Content, "\n")}
	}
	result := providers.RepositoryGraphContext{}
	for _, language := range graph.Languages {
		result.Languages = append(result.Languages, string(language))
	}
	for _, path := range graph.UnsupportedSourceFiles {
		if _, included := paths[path]; included {
			result.UnsupportedSourceFiles = append(result.UnsupportedSourceFiles, path)
		}
	}
	for _, path := range graph.IndexedSourceFiles {
		if _, included := paths[path]; included {
			result.IndexedSourceFiles++
		}
	}
	names := make(map[string]string, len(graph.Symbols))
	for _, symbol := range graph.Symbols {
		name := symbol.QualifiedName
		if name == "" {
			name = symbol.Name
		}
		names[symbol.ID] = name
		submitted, included := paths[symbol.Path]
		if !included || symbol.StartLine < submitted.start || symbol.EndLine > submitted.end {
			continue
		}
		result.Symbols = append(result.Symbols, providers.RepositoryGraphSymbol{
			Name: name, Kind: string(symbol.Kind), Path: symbol.Path, StartLine: symbol.StartLine, EndLine: symbol.EndLine, Reachability: string(symbol.Reachability),
		})
	}
	for _, imported := range graph.Imports {
		if _, included := paths[imported.Path]; included {
			result.Imports = append(result.Imports, providers.RepositoryGraphImport{Path: imported.Path, ImportedPath: imported.ImportedPath})
		}
	}
	for _, edge := range graph.Edges {
		submitted, included := paths[edge.Path]
		if !included || edge.Line < submitted.start || edge.Line > submitted.end {
			continue
		}
		from := names[edge.From]
		if from == "" {
			from = edge.From
		}
		to := names[edge.To]
		if to == "" {
			to = edge.To
		}
		result.Relationships = append(result.Relationships, providers.RepositoryGraphRelationship{
			Kind: string(edge.Kind), From: from, To: to, Label: edge.Label, Path: edge.Path, Line: edge.Line, Resolved: edge.Resolved,
		})
	}
	return result
}

// boundedRepositoryGraphContext preserves submitted source before optional
// graph metadata. A large structural neighborhood must never cause an
// executable source excerpt to be discarded merely because one request has a
// finite context window.
func boundedRepositoryGraphContext(graph codegraph.Graph, files []providers.RepositorySourceFile, maximum int64) providers.RepositoryGraphContext {
	result := repositoryGraphContext(graph, files)
	if maximum <= 0 {
		return result
	}
	for requestContextBytes(files, result) > maximum {
		switch {
		case len(result.UnsupportedSourceFiles) > 0:
			result.UnsupportedSourceFiles = result.UnsupportedSourceFiles[:len(result.UnsupportedSourceFiles)-1]
		case len(result.Imports) > 8:
			result.Imports = result.Imports[:len(result.Imports)-1]
		case len(result.Relationships) > 12:
			result.Relationships = result.Relationships[:len(result.Relationships)-1]
		case len(result.Symbols) > 12:
			result.Symbols = result.Symbols[:len(result.Symbols)-1]
		case len(result.Imports) > 0:
			result.Imports = result.Imports[:len(result.Imports)-1]
		case len(result.Relationships) > 1:
			result.Relationships = result.Relationships[:len(result.Relationships)-1]
		case len(result.Symbols) > 1:
			result.Symbols = result.Symbols[:len(result.Symbols)-1]
		case len(result.Languages) > 0:
			result.Languages = result.Languages[:len(result.Languages)-1]
		default:
			return result
		}
	}
	return result
}

func repositorySize(repository discovery.Repository) int64 {
	var size int64
	for _, file := range repository.Files {
		size += int64(len(file.Content))
	}
	return size
}

func summaryBytes(values []providers.RepositorySectionResult) int64 {
	encoded, _ := json.Marshal(values)
	return int64(len(encoded))
}

func citedFileIndex(summaries []providers.RepositorySectionResult, files []providers.RepositorySourceFile) []providers.RepositoryFileReference {
	wanted := make(map[string]struct{})
	add := func(citations []providers.RepositoryCitation) {
		for _, citation := range citations {
			wanted[citation.Path] = struct{}{}
		}
	}
	for _, summary := range summaries {
		for _, use := range summary.AIUses {
			add(use.Evidence)
		}
		for _, factSet := range summary.AIUseFacts {
			for _, fact := range factSet.Facts {
				add(fact.Evidence)
			}
		}
		for _, observation := range summary.ObjectiveObservations {
			add(observation.SupportingEvidence)
			add(observation.ContradictoryEvidence)
		}
		for _, observation := range summary.UnmappedObservations {
			add(observation.Evidence)
		}
	}
	result := make([]providers.RepositoryFileReference, 0, len(wanted))
	for _, file := range files {
		if _, exists := wanted[file.Path]; exists {
			result = append(result, providers.RepositoryFileReference{Path: file.Path, Kind: file.Kind, LineCount: file.LineCount})
		}
	}
	return result
}

func addUsage(total *providers.Usage, value providers.Usage) {
	total.PromptTokens += value.PromptTokens
	total.CompletionTokens += value.CompletionTokens
	total.ReasoningTokens += value.ReasoningTokens
	total.TotalDurationNS += value.TotalDurationNS
}

func usageFromIncomplete(value *providers.RemoteIncompleteError) providers.Usage {
	if value == nil {
		return providers.Usage{}
	}
	return providers.Usage{
		PromptTokens: value.InputTokens, CompletionTokens: value.OutputTokens, ReasoningTokens: value.ReasoningTokens,
	}
}

func usageIsZero(value providers.Usage) bool {
	return value.PromptTokens == 0 && value.CompletionTokens == 0 && value.ReasoningTokens == 0 && value.TotalDurationNS == 0
}

func progress(options Options, value Progress) error {
	if options.OnProgress == nil {
		return nil
	}
	return options.OnProgress(value)
}
