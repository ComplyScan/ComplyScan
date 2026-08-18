// Package repositoryanalysis prepares safe targeted model context by default
// and retains explicit broad one-pass and hierarchical analysis modes.
package repositoryanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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
	minimumRateLimitCooldown   = time.Second
	maxRateLimitTotalWait      = 10 * time.Minute
	maxValidationRepairRetries = 2
	maxTransientRetryAttempts  = 8
	minimumAdaptiveOutput      = 1024
	minimumRecoveryOutput      = 4096
	maximumRecoveryOutput      = 8192
	maxConcurrentSourceBatches = 32
	sourceBatchTokenOverhead   = 8192
	// The source-free compatibility probe shares model qualification's bounded
	// four-attempt transient retry contract. Reserve the maximum up front so the
	// advertised repository-layer ceiling cannot be crossed before actual probe
	// accounting is returned.
	maxCapacityProbeProviderRequests = 4
	// MaxProviderRequestsPerRun bounds remote cost and retry amplification for
	// one repository-analysis layer. Reaching it yields an honest incomplete
	// result with attempted-transfer and usage accounting.
	MaxProviderRequestsPerRun = 256
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
	Mode           Mode
	MaxInputTokens int
	// ModelContextTokens is the provider/model's stable per-request context
	// ceiling when it can be identified confidently. It is distinct from the
	// rolling TPM capacity carried by InitialRateLimits.
	ModelContextTokens int
	Provider           providers.Kind
	Model              string
	Ownership          []ownership.Rule
	ConfirmedAIUses    []providers.RepositoryConfirmedAIUse
	TargetedBatches    bool
	OnProgress         func(Progress) error
	Wait               func(context.Context, time.Duration) error
	InitialRateLimits  providers.RateLimitSnapshot
	InitialUsage       providers.Usage
	// InitialProviderRequests accounts for a live, source-free compatibility
	// request whose capacity snapshot is reused by this repository run.
	InitialProviderRequests int
	ProbeRateLimits         func(context.Context) (CapacityProbeResult, error)
	retryGate               chan struct{}
	requestBudget           *providerRequestBudget
	separateTargetedFiles   bool
}

// CapacityProbeResult records a source-free request used to discover live
// provider capacity. It is still a metered provider request and therefore
// participates in the repository review's request/token accounting.
type CapacityProbeResult struct {
	RateLimits       providers.RateLimitSnapshot
	Usage            providers.Usage
	ProviderRequests int
	// ModelDigest is the qualified provider artifact identity when one is
	// available (notably an Ollama tag digest). It never contains source data.
	ModelDigest string
}

type providerRequestBudget struct {
	mu    sync.Mutex
	used  int
	limit int
}

func (value *providerRequestBudget) reserve() bool {
	return value.reserveCount(1)
}

func (value *providerRequestBudget) reserveCount(count int) bool {
	if value == nil {
		return true
	}
	if count <= 0 {
		return true
	}
	value.mu.Lock()
	defer value.mu.Unlock()
	if value.used+count > value.limit {
		return false
	}
	value.used += count
	return true
}

func (value *providerRequestBudget) releaseCount(count int) {
	if value == nil || count <= 0 {
		return
	}
	value.mu.Lock()
	value.used = max(0, value.used-count)
	value.mu.Unlock()
}

type Progress struct {
	Stage        string
	Completed    int
	Total        int
	Scope        string
	InputBytes   int64
	Wait         time.Duration
	OriginalWait time.Duration
	Duration     time.Duration
	Detail       string
}

// Run selects compact structural evidence in auto and targeted modes. Deep,
// full, and hierarchical modes retain broad repository analysis.
func Run(ctx context.Context, reviewer Reviewer, repository discovery.Repository, evidence []framework.TechnicalEvidenceReport, systems []profile.System, options Options) (result providers.RepositoryAnalysisResult, runErr error) {
	defer func() {
		if options.InitialProviderRequests <= 0 {
			return
		}
		result.Coverage.ProviderRequests += options.InitialProviderRequests
		addUsage(&result.Usage, options.InitialUsage)
		result.Notes = append(result.Notes, fmt.Sprintf("Provider request and token totals include %d live source-free model compatibility request(s) that supplied this run's initial capacity snapshot.", options.InitialProviderRequests))
	}()
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
	if options.retryGate == nil {
		options.retryGate = make(chan struct{}, 1)
	}
	if options.requestBudget == nil {
		initialRequests := max(0, options.InitialProviderRequests)
		options.requestBudget = &providerRequestBudget{used: initialRequests, limit: MaxProviderRequestsPerRun}
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
		fullEstimate := estimatedRepositoryRequestTokens(request)
		preflightLimits, intrinsicallyTooLarge, preflightErr := waitForInitialRepositoryCapacity(ctx, options, fullEstimate, ".")
		if preflightErr != nil {
			return providers.RepositoryAnalysisResult{}, preflightErr
		}
		options.InitialRateLimits = preflightLimits
		if intrinsicallyTooLarge {
			if options.Mode == ModeFull {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("the configured provider capacity cannot admit the full repository request estimated at %d tokens; use auto or hierarchical mode so ComplyScan can split it safely", fullEstimate)
			}
			batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, options)
			batched.Notes = append(batched.Notes, "The live provider-capacity snapshot could not safely admit the broad request, so no broad source transfer was attempted and ComplyScan used bounded batches instead.")
			return batched, batchErr
		}
		result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge && options.Mode == ModeAuto {
				batchOptions := options
				if result.RateLimits.Available() {
					batchOptions.InitialRateLimits = result.RateLimits
				}
				batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, batchOptions)
				mergeRepositoryAttemptAccounting(&batched, result)
				batched.Notes = append(batched.Notes, "The initial broad request exceeded the provider limit; its attempted source transfer and known usage are included before the hierarchical retry.")
				return batched, batchErr
			}
			if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && incomplete.Reason == "max_output_tokens" && options.Mode == ModeAuto {
				batchOptions := options
				if result.RateLimits.Available() {
					batchOptions.InitialRateLimits = result.RateLimits
				}
				batched, batchErr := runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, confirmedUses, budget, batchOptions)
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
	target.Coverage.ProviderRequests += attempt.Coverage.ProviderRequests
	target.Coverage.CitationsChecked += attempt.Coverage.CitationsChecked
	addUsage(&target.Usage, attempt.Usage)
	target.RequestDiagnostics = append(target.RequestDiagnostics, attempt.RequestDiagnostics...)
}

func runHierarchical(ctx context.Context, reviewer Reviewer, repository discovery.Repository, graph codegraph.Graph, files []providers.RepositorySourceFile, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64, options Options) (providers.RepositoryAnalysisResult, error) {
	var chunks []repositoryChunk
	var err error
	if options.TargetedBatches {
		// sourceBudget already reserves 20% for the prompt envelope. Reapplying
		// that reserve here created unnecessary extra batches; actual encoded
		// requests are measured below and split again if graph metadata expands.
		chunks, err = partitionTargetedRepository(repository, graph, files, confirmedUses, budget, options.separateTargetedFiles)
	} else {
		chunks, err = partitionRepository(files, budget*80/100)
	}
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
	sourceBatchesStarted := make(map[string]struct{}, len(chunks))
	prefetched := make(map[string]repositorySourceBatchResponse)
	synthesisPrefetched := make(map[int]repositorySynthesisResponse)
	partialFailure := func(scope string, cause error) (providers.RepositoryAnalysisResult, error) {
		for key, response := range prefetched {
			mergeRepositoryAttemptAccounting(&aggregate, response.result)
			delete(prefetched, key)
		}
		drainRepositorySynthesisPrefetch(&aggregate, synthesisPrefetched)
		aggregate.Coverage.Subsystems = sourceBatchesCompleted
		aggregate.Coverage.SourceBatchesStarted = len(sourceBatchesStarted)
		aggregate.Coverage.SourceBatchesCompleted = sourceBatchesCompleted
		aggregate.Coverage.SourceBatchesTotal = len(chunks)
		detail := fmt.Sprintf("repository model review validated %d of %d bounded source batch(es); %d distinct source batch(es) started a provider request", sourceBatchesCompleted, len(chunks), len(sourceBatchesStarted))
		status := detail + "; remaining candidate evidence did not reach a validated response and completed batches were not globally synthesized"
		if len(sourceBatchesStarted) >= len(chunks) && sourceBatchesCompleted < len(chunks) {
			status = detail + "; every planned source batch started a provider request, but one or more responses could not be validated and no global synthesis was retained"
		}
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
		aggregate.Notes = append(aggregate.Notes, detail+" before the repository review stopped. This partial result must not be treated as a completed zero-use review.")
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
		if !options.requestBudget.reserveCount(maxCapacityProbeProviderRequests) {
			return partialFailure("provider capacity probe", fmt.Errorf("repository review reached the safety ceiling of %d provider requests", MaxProviderRequestsPerRun))
		}
		probed, probeErr := options.ProbeRateLimits(ctx)
		probeRequests := max(0, probed.ProviderRequests)
		if probeRequests < maxCapacityProbeProviderRequests {
			options.requestBudget.releaseCount(maxCapacityProbeProviderRequests - probeRequests)
		}
		if probeRequests > maxCapacityProbeProviderRequests {
			return partialFailure("provider capacity probe", fmt.Errorf("source-free capacity probe exceeded its safety allowance of %d provider requests", maxCapacityProbeProviderRequests))
		}
		addUsage(&aggregate.Usage, probed.Usage)
		aggregate.Coverage.ProviderRequests += probeRequests
		if probeErr != nil && (errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) || ctx.Err() != nil) {
			return partialFailure("provider capacity probe", probeErr)
		}
		if probeErr != nil {
			retryableProbeFailure := false
			probeWait := time.Duration(0)
			if rateLimit, ok := providers.AsRemoteRateLimitError(probeErr); ok {
				retryableProbeFailure = !rateLimit.Permanent && !rateLimit.RequestTooLarge
				probeWait = max(rateLimit.RetryAfter, exhaustedCapacityReset(rateLimit.RateLimits))
			}
			if transient, ok := providers.AsRemoteTransientError(probeErr); ok {
				retryableProbeFailure = true
				probeWait = max(transient.RetryAfter, exhaustedCapacityReset(transient.RateLimits))
			}
			if !retryableProbeFailure {
				return partialFailure("provider capacity probe", fmt.Errorf("source-free provider check failed before repository code was sent: %w", probeErr))
			}
			if probeWait < minimumRateLimitCooldown {
				probeWait = minimumRateLimitCooldown
			}
			if probeWait > 0 {
				if probeWait > maxRateLimitTotalWait {
					return partialFailure("provider capacity probe", fmt.Errorf("provider capacity probe requested a retry wait beyond the %s automatic budget: %w", maxRateLimitTotalWait, probeErr))
				}
				if err := progress(options, Progress{Stage: "batch-capacity-wait", Scope: ".", Wait: probeWait, OriginalWait: probeWait, Detail: "honoring provider backoff before any repository source is sent"}); err != nil {
					return partialFailure("provider capacity probe", err)
				}
				if err := waitForConfiguredRateLimit(ctx, options, probeWait); err != nil {
					return partialFailure("provider capacity probe", err)
				}
			}
			if probed.RateLimits.Available() {
				observedLimits = replenishRateLimitSnapshot(probed.RateLimits, probeWait)
			}
		}
		if probeErr != nil || !probed.RateLimits.Available() {
			detail := "the provider returned no usable capacity headers; the first source batch will calibrate this run"
			if probeErr != nil {
				detail = "the source-free capacity probe did not complete; the first source batch will calibrate this run: " + probeErr.Error()
			}
			if err := progress(options, Progress{Stage: "rate-limit-probe-fallback", Total: len(chunks), Scope: ".", Detail: detail}); err != nil {
				return partialFailure("provider capacity probe", err)
			}
		} else {
			observedLimits = probed.RateLimits
			if err := progress(options, Progress{Stage: "rate-limit-probe-complete", Total: len(chunks), Scope: ".", Detail: rateLimitCapacityDetail(probed.RateLimits)}); err != nil {
				return partialFailure("provider capacity probe", err)
			}
		}
	}
	unknownWaveLimit := 1
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
		if observedLimits.TokensKnown && observedLimits.LimitTokens > 0 && prepared.estimatedTokens > observedLimits.LimitTokens {
			inputAndEnvelope := prepared.estimatedTokens - prepared.maxOutputTokens
			availableOutput := observedLimits.LimitTokens - inputAndEnvelope
			if availableOutput >= minimumAdaptiveOutput && availableOutput < prepared.maxOutputTokens {
				chunks[index].maxOutputTokens = availableOutput
				if err := progress(options, Progress{
					Stage: "capacity-preflight-output", Completed: index, Total: len(chunks), Scope: chunk.scope,
					InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("reduce output allowance from %d to %d so one request fits the observed %d-token provider window", prepared.maxOutputTokens, availableOutput, observedLimits.LimitTokens),
				}); err != nil {
					return partialFailure(chunk.scope, err)
				}
				continue
			}
			parts, split := splitRepositoryChunk(chunk, minimumAdaptiveOutput)
			if !split {
				return partialFailure(chunk.scope, fmt.Errorf("the smallest safe source segment is estimated at %d tokens, exceeding the provider's observed %d-token window", prepared.estimatedTokens, observedLimits.LimitTokens))
			}
			chunks = replaceRepositoryChunk(chunks, index, parts)
			if err := progress(options, Progress{
				Stage: "capacity-preflight-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
				InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("split source before transfer so each request can fit the observed %d-token provider window", observedLimits.LimitTokens),
			}); err != nil {
				return partialFailure(chunk.scope, err)
			}
			continue
		}

		chunkID := repositoryChunkID(chunk)
		batchResponse, ready := prefetched[chunkID]
		if ready {
			delete(prefetched, chunkID)
		} else {
			waveLimit, wait := sourceBatchWaveLimit(observedLimits, prepared.estimatedTokens, len(chunks)-index, unknownWaveLimit)
			if waveLimit == 0 {
				if wait <= 0 {
					wait = minimumRateLimitCooldown
				}
				if wait > maxRateLimitTotalWait {
					return partialFailure(chunk.scope, fmt.Errorf("one provider-capacity reset wait exceeds the %s automatic per-window wait budget", maxRateLimitTotalWait))
				}
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
				if _, alreadyStarted := prefetched[repositoryChunkID(candidate)]; alreadyStarted {
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
				detail := fmt.Sprintf("successful bounded requests support a cautious wave of %d source batches", len(wave))
				if observedLimits.RequestsKnown && observedLimits.TokensKnown {
					detail = fmt.Sprintf("observed request and token capacity allow %d source batches", len(wave))
				}
				if err := progress(options, Progress{
					Stage: "targeted-batch-concurrency", Completed: len(wave), Total: len(chunks) - index, Scope: chunk.scope,
					Detail: detail,
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
			waveSucceeded := true
			for responseIndex, response := range responses {
				call := wave[responseIndex]
				if response.result.Coverage.ProviderRequests > 0 {
					sourceBatchesStarted[repositoryChunkID(call.chunk)] = struct{}{}
				}
				prefetched[response.id] = response
				// The wave reservation covers the first provider attempt for each
				// logical batch. Validation repairs and transient retries are real
				// additional RPM/TPM consumers; reserve them conservatively when a
				// provider did not return a fresher complete capacity snapshot.
				extraRequests := max(0, response.result.Coverage.ProviderRequests-1)
				observedLimits = reserveRateLimitSnapshot(observedLimits, extraRequests, extraRequests*call.estimatedTokens)
				observedLimits = conservativeRateLimitSnapshot(observedLimits, response.result.RateLimits)
				if response.err != nil || response.result.Coverage.ProviderRequests != 1 {
					waveSucceeded = false
				}
			}
			if !observedLimits.Available() && options.Provider != providers.Ollama {
				if waveSucceeded {
					unknownWaveLimit = min(maxConcurrentSourceBatches, max(1, unknownWaveLimit*2))
				} else {
					unknownWaveLimit = max(1, unknownWaveLimit/2)
				}
			}
			var recoveryErr error
			observedLimits, adaptiveTokenLimit, recoveryErr = recoverRepositorySourceWaveOutputs(
				ctx, reviewer, wave, responses, chunks, prefetched, observedLimits, adaptiveTokenLimit, unknownWaveLimit, options,
			)
			if recoveryErr != nil {
				return partialFailure("source output recovery", recoveryErr)
			}
			if chunks[index].maxOutputTokens > 0 && chunks[index].maxOutputTokens != prepared.maxOutputTokens {
				prepared = prepareRepositorySourceBatch(chunks[index], repository, graph, objectives, systems, confirmedUses, budget, adaptiveTokenLimit, options)
			}
			batchResponse = prefetched[chunkID]
			delete(prefetched, chunkID)
		}

		result, err := batchResponse.result, batchResponse.err
		aggregate.Coverage.FilesSubmitted += result.Coverage.FilesSubmitted
		aggregate.Coverage.BytesSubmitted += result.Coverage.BytesSubmitted
		aggregate.Coverage.ProviderRequests += result.Coverage.ProviderRequests
		aggregate.RequestDiagnostics = append(aggregate.RequestDiagnostics, result.RequestDiagnostics...)
		if err != nil {
			addUsage(&aggregate.Usage, result.Usage)
			incomplete, isIncomplete := providers.AsRemoteIncompleteError(err)
			if isIncomplete && usageIsZero(result.Usage) {
				addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
			}
			if _, invalid := providers.AsRepositoryValidationError(err); invalid {
				parts, split := splitRepositoryChunk(chunk, prepared.maxOutputTokens)
				if split {
					delete(sourceBatchesStarted, chunkID)
					chunks = replaceRepositoryChunk(chunks, index, parts)
					if progressErr := progress(options, Progress{
						Stage: "validation-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
						InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("structured output stayed invalid after corrective regeneration; split into %d smaller evidence part(s)", len(parts)),
					}); progressErr != nil {
						return partialFailure(chunk.scope, progressErr)
					}
					continue
				}
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
				delete(sourceBatchesStarted, chunkID)
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
				adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, incomplete.TokenLimit)
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
				parts, split := splitRepositoryChunk(chunk, outputWithinTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit))
				if !split {
					return partialFailure(chunk.scope, fmt.Errorf("model exhausted %d output tokens for the smallest safe repository segment: %w", prepared.maxOutputTokens, err))
				}
				delete(sourceBatchesStarted, chunkID)
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
	// Source-batch results are the durable, locally validated evidence record.
	// Synthesis receives a much smaller grouping view and decides only which
	// scan-local observations belong together. The full facts and citations are
	// retained here and reattached after final membership validation.
	sourceEvidenceSummaries := append([]providers.RepositorySectionResult(nil), summaries...)
	summaries = compactRepositoryGroupingSummaries(summaries)
	// Targeted source batches deliberately stay small so each model request sees
	// a focused code excerpt. The resulting validated summaries contain no raw
	// repository source, so reusing that same small source budget for synthesis
	// needlessly fragments each summary into many model calls. Synthesis may use
	// the caller's full configured input allowance while preserving the smaller
	// source-transfer boundary.
	synthesisBudget := budget
	if options.TargetedBatches && options.MaxInputTokens > 0 {
		configured := sourceBudget(options.MaxInputTokens, objectives, systems, confirmedUses)
		if configured > synthesisBudget {
			synthesisBudget = configured
		}
	}
	levels := 0
	for len(summaries) > 1 {
		levels++
		plainGroups := partitionSummaries(summaries, synthesisBudget)
		groups := make([]repositorySummaryGroup, len(plainGroups))
		for index := range plainGroups {
			groups[index] = repositorySummaryGroup{summaries: plainGroups[index]}
		}
		next := make([]providers.RepositorySectionResult, 0, len(groups))
		synthesisUnknownWaveLimit := 1
		for index := 0; index < len(groups); {
			group := groups[index]
			prepared := prepareRepositorySynthesisCall(levels, index, group, repository, files, objectives, systems, confirmedUses, adaptiveTokenLimit, options)
			scope := prepared.scope
			maxOutputTokens := prepared.maxOutputTokens
			if prepared.inputBytes > synthesisBudget {
				parts, split := splitRepositorySummaryGroup(group, maxOutputTokens)
				if !split && len(group.summaries) == 1 {
					if fragments, fragmented := fragmentRepositorySection(group.summaries[0]); fragmented {
						parts = make([]repositorySummaryGroup, 0, len(fragments))
						for _, fragment := range fragments {
							parts = append(parts, repositorySummaryGroup{summaries: []providers.RepositorySectionResult{fragment}, maxOutputTokens: maxOutputTokens})
						}
						split = true
					}
				}
				if !split {
					return partialFailure(scope, fmt.Errorf("synthesis request requires %d encoded bytes and one semantic summary atom cannot be divided to fit the per-request budget of %d bytes", prepared.inputBytes, synthesisBudget))
				}
				shiftRepositorySynthesisPrefetch(synthesisPrefetched, index, len(parts)-1)
				groups = replaceRepositorySummaryGroup(groups, index, parts)
				if progressErr := progress(options, Progress{
					Stage: "synthesis-context-split", Completed: index, Total: len(groups), Scope: scope,
					InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("encoded synthesis request exceeded %d bytes; split into %d semantic group(s)", synthesisBudget, len(parts)),
				}); progressErr != nil {
					return partialFailure(scope, progressErr)
				}
				continue
			}
			if observedLimits.TokensKnown && observedLimits.LimitTokens > 0 && prepared.estimatedTokens > observedLimits.LimitTokens {
				inputAndEnvelope := prepared.estimatedTokens - prepared.maxOutputTokens
				availableOutput := observedLimits.LimitTokens - inputAndEnvelope
				if availableOutput >= minimumAdaptiveOutput && availableOutput < prepared.maxOutputTokens {
					groups[index].maxOutputTokens = availableOutput
					if err := progress(options, Progress{
						Stage: "capacity-preflight-output", Completed: index, Total: len(groups), Scope: scope,
						InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("reduce synthesis output allowance from %d to %d so one request fits the observed %d-token provider window", prepared.maxOutputTokens, availableOutput, observedLimits.LimitTokens),
					}); err != nil {
						return partialFailure(scope, err)
					}
					continue
				}
				parts, split := splitRepositorySummaryGroup(group, minimumAdaptiveOutput)
				if !split && len(group.summaries) == 1 {
					if fragments, fragmented := fragmentRepositorySection(group.summaries[0]); fragmented {
						parts = make([]repositorySummaryGroup, 0, len(fragments))
						for _, fragment := range fragments {
							parts = append(parts, repositorySummaryGroup{summaries: []providers.RepositorySectionResult{fragment}, maxOutputTokens: minimumAdaptiveOutput})
						}
						split = true
					}
				}
				if !split {
					return partialFailure(scope, fmt.Errorf("the smallest safe synthesis atom is estimated at %d tokens, exceeding the provider's observed %d-token window", prepared.estimatedTokens, observedLimits.LimitTokens))
				}
				shiftRepositorySynthesisPrefetch(synthesisPrefetched, index, len(parts)-1)
				groups = replaceRepositorySummaryGroup(groups, index, parts)
				if err := progress(options, Progress{
					Stage: "capacity-preflight-split", Completed: index, Total: len(groups), Scope: scope,
					InputBytes: prepared.inputBytes, Detail: fmt.Sprintf("split synthesis before transfer so each request can fit the observed %d-token provider window", observedLimits.LimitTokens),
				}); err != nil {
					return partialFailure(scope, err)
				}
				continue
			}

			batchResponse, ready := synthesisPrefetched[index]
			if ready {
				delete(synthesisPrefetched, index)
			} else {
				waveLimit, wait := sourceBatchWaveLimit(observedLimits, prepared.estimatedTokens, len(groups)-index, synthesisUnknownWaveLimit)
				if waveLimit == 0 {
					if wait <= 0 {
						wait = minimumRateLimitCooldown
					}
					if wait > maxRateLimitTotalWait {
						return partialFailure(scope, fmt.Errorf("one provider synthesis-capacity reset wait exceeds the %s automatic per-window wait budget", maxRateLimitTotalWait))
					}
					if err := progress(options, Progress{Stage: "batch-capacity-wait", Scope: scope, Wait: wait, OriginalWait: wait, Detail: "waiting for provider request/token capacity before starting the next synthesis wave"}); err != nil {
						return partialFailure(scope, err)
					}
					if err := waitForConfiguredRateLimit(ctx, options, wait); err != nil {
						return partialFailure(scope, err)
					}
					observedLimits = replenishRateLimitSnapshot(observedLimits, wait)
					continue
				}
				wave := make([]repositorySynthesisCall, 0, waveLimit)
				estimatedTokens := 0
				for candidateIndex := index; candidateIndex < len(groups) && len(wave) < waveLimit; candidateIndex++ {
					if _, alreadyPrefetched := synthesisPrefetched[candidateIndex]; alreadyPrefetched {
						break
					}
					candidateCall := prepareRepositorySynthesisCall(levels, candidateIndex, groups[candidateIndex], repository, files, objectives, systems, confirmedUses, adaptiveTokenLimit, options)
					if candidateCall.inputBytes > synthesisBudget {
						break
					}
					if observedLimits.TokensKnown && len(wave) > 0 && estimatedTokens+candidateCall.estimatedTokens > observedLimits.RemainingTokens {
						break
					}
					wave = append(wave, candidateCall)
					estimatedTokens += candidateCall.estimatedTokens
				}
				if len(wave) == 0 {
					wave = append(wave, prepared)
				}
				if len(wave) > 1 {
					detail := fmt.Sprintf("successful bounded requests support a cautious wave of %d synthesis groups", len(wave))
					if observedLimits.RequestsKnown && observedLimits.TokensKnown {
						detail = fmt.Sprintf("observed request and token capacity allow %d synthesis groups", len(wave))
					}
					if err := progress(options, Progress{Stage: "synthesis-concurrency", Completed: len(wave), Total: len(groups) - index, Scope: scope, Detail: detail}); err != nil {
						return partialFailure(scope, err)
					}
				}
				for _, call := range wave {
					if err := progress(options, Progress{Stage: "synthesis-start", Completed: call.index + 1, Total: len(groups), Scope: call.scope, InputBytes: call.inputBytes}); err != nil {
						return partialFailure(call.scope, err)
					}
				}
				observedLimits = reserveRateLimitSnapshot(observedLimits, len(wave), estimatedTokens)
				responses := runRepositorySynthesisWave(ctx, reviewer, wave, options)
				waveSucceeded := true
				for responseIndex, response := range responses {
					call := wave[responseIndex]
					synthesisPrefetched[call.index] = response
					extraRequests := max(0, response.result.Coverage.ProviderRequests-1)
					observedLimits = reserveRateLimitSnapshot(observedLimits, extraRequests, extraRequests*call.estimatedTokens)
					observedLimits = conservativeRateLimitSnapshot(observedLimits, response.result.RateLimits)
					if response.err != nil || response.result.Coverage.ProviderRequests != 1 {
						waveSucceeded = false
					}
				}
				if !observedLimits.Available() && options.Provider != providers.Ollama {
					if waveSucceeded {
						synthesisUnknownWaveLimit = min(maxConcurrentSourceBatches, max(1, synthesisUnknownWaveLimit*2))
					} else {
						synthesisUnknownWaveLimit = max(1, synthesisUnknownWaveLimit/2)
					}
				}
				batchResponse = synthesisPrefetched[index]
				delete(synthesisPrefetched, index)
			}
			result, err := batchResponse.result, batchResponse.err
			aggregate.Coverage.ProviderRequests += result.Coverage.ProviderRequests
			aggregate.RequestDiagnostics = append(aggregate.RequestDiagnostics, result.RequestDiagnostics...)
			if err != nil {
				addUsage(&aggregate.Usage, result.Usage)
				incomplete, isIncomplete := providers.AsRemoteIncompleteError(err)
				if isIncomplete && usageIsZero(result.Usage) {
					addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
				}
				_, invalid := providers.AsRepositoryValidationError(err)
				_, representation := providers.AsRepositoryRepresentationError(err)
				if invalid || representation {
					parts, split := splitRepositorySummaryGroup(group, maxOutputTokens)
					if !split && len(group.summaries) == 1 {
						if fragments, fragmented := fragmentRepositorySection(group.summaries[0]); fragmented {
							parts = make([]repositorySummaryGroup, 0, len(fragments))
							for _, fragment := range fragments {
								parts = append(parts, repositorySummaryGroup{summaries: []providers.RepositorySectionResult{fragment}, maxOutputTokens: maxOutputTokens})
							}
							split = true
						}
					}
					if split {
						shiftRepositorySynthesisPrefetch(synthesisPrefetched, index, len(parts)-1)
						groups = replaceRepositorySummaryGroup(groups, index, parts)
						stage := "validation-split"
						detail := fmt.Sprintf("synthesis output stayed invalid after corrective regeneration; split into %d smaller group(s)", len(parts))
						if representation {
							stage = "representation-split"
							detail = fmt.Sprintf("validated synthesis semantics exceeded one result representation; split locally into %d identity-preserving group(s) before any provider transfer", len(parts))
						}
						if progressErr := progress(options, Progress{
							Stage: stage, Completed: index, Total: len(groups), Scope: scope,
							InputBytes: summaryBytes(group.summaries), Detail: detail,
						}); progressErr != nil {
							return partialFailure(scope, progressErr)
						}
						continue
					}
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
					shiftRepositorySynthesisPrefetch(synthesisPrefetched, index, len(parts)-1)
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
					adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, incomplete.TokenLimit)
					recoveryOutput := repositoryAdaptiveRecoveryOutputTokens(options.Provider, maxOutputTokens, incomplete)
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
					parts, split := splitRepositorySummaryGroup(group, outputWithinTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit))
					if !split {
						return partialFailure(scope, fmt.Errorf("model exhausted %d output tokens and the synthesis input cannot be divided further: %w", maxOutputTokens, err))
					}
					shiftRepositorySynthesisPrefetch(synthesisPrefetched, index, len(parts)-1)
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
			grouped = attachRepositoryGroupingHints(grouped, group.summaries)
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
			if len(partitionSummaries(next, synthesisBudget)) >= len(next) {
				fragmented := make([]providers.RepositorySectionResult, 0, len(next))
				fragmentedAny := false
				for _, summary := range next {
					parts, ok := fragmentRepositorySection(summary)
					if !ok {
						fragmented = append(fragmented, summary)
						continue
					}
					fragmentedAny = true
					fragmented = append(fragmented, parts...)
				}
				if !fragmentedAny {
					return partialFailure(fmt.Sprintf("synthesis level %d", levels), errors.New("repository synthesis could not reduce the remaining semantic summary atoms within the configured input budget"))
				}
				summaries = fragmented
				if progressErr := progress(options, Progress{
					Stage: "synthesis-context-split", Total: len(fragmented), Scope: fmt.Sprintf("synthesis-level-%d", levels),
					Detail: "model compaction did not reduce the encoded request; split validated summaries into smaller identity-preserving semantic atoms",
				}); progressErr != nil {
					return partialFailure(fmt.Sprintf("synthesis level %d", levels), progressErr)
				}
				continue
			}
		}
		summaries = next
	}
	usedValidatedSourceFallback := false
	if len(summaries) == 1 && len(chunks) == 1 {
		// A forced hierarchical analysis still gets an explicit synthesis pass so
		// its output follows the same global contract as larger repositories.
		group := summaries
		maxOutputTokens := repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
		var result providers.RepositoryAnalysisResult
		retainValidatedSource := func(cause error) error {
			grouped, groupErr := assignSynthesisCandidateIDs(sourceEvidenceSummaries[0], confirmedUses)
			if groupErr != nil {
				return fmt.Errorf("retain validated single-source result after optional synthesis failure: %w", groupErr)
			}
			summaries[0] = grouped
			usedValidatedSourceFallback = true
			aggregate.Notes = append(aggregate.Notes, "The optional single-batch synthesis did not complete after bounded recovery; ComplyScan retained the already validated source result and derived its report-local grouping IDs locally. Provider-attempt accounting is retained. Cause: "+cause.Error())
			return progress(options, Progress{
				Stage: "synthesis-source-fallback", Scope: "repository-synthesis", InputBytes: summaryBytes(group),
				Detail: "retained the validated single-source result after optional synthesis failed",
			})
		}
		for {
			request := providers.RepositoryAnalysisRequest{
				Mode: providers.RepositoryAnalysisSynthesis, Scope: "repository-synthesis",
				RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
				SubsystemSummaries: group, MaxOutputTokens: maxOutputTokens, CompactSynthesis: true,
			}
			estimated := estimatedRepositoryRequestTokens(request)
			preflightOptions := options
			preflightOptions.InitialRateLimits = observedLimits
			preflightLimits, intrinsicallyTooLarge, preflightErr := waitForInitialRepositoryCapacity(ctx, preflightOptions, estimated, "repository-synthesis")
			if preflightErr != nil {
				return partialFailure("repository synthesis capacity", preflightErr)
			}
			observedLimits = preflightLimits
			if intrinsicallyTooLarge {
				if fallbackErr := retainValidatedSource(fmt.Errorf("live provider token capacity could not safely admit the optional single-batch synthesis estimated at %d tokens", estimated)); fallbackErr != nil {
					return partialFailure("repository synthesis", fallbackErr)
				}
				break
			}
			result, err = reviewRepositoryWithRetry(ctx, reviewer, request, options)
			aggregate.Coverage.ProviderRequests += result.Coverage.ProviderRequests
			aggregate.RequestDiagnostics = append(aggregate.RequestDiagnostics, result.RequestDiagnostics...)
			if err == nil {
				break
			}
			addUsage(&aggregate.Usage, result.Usage)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return partialFailure("repository synthesis", err)
			}
			if _, invalid := providers.AsRepositoryValidationError(err); invalid {
				// One validated source batch has no cross-batch conflicts to
				// reconcile. If the optional forced synthesis cannot satisfy the
				// stricter grouping contract after bounded repairs, retain that
				// already checked source result and derive its report-local IDs
				// locally instead of discarding useful semantics.
				if progressErr := retainValidatedSource(err); progressErr != nil {
					return partialFailure("repository synthesis", progressErr)
				}
				break
			}
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
				if fallbackErr := retainValidatedSource(err); fallbackErr != nil {
					return partialFailure("repository synthesis", fallbackErr)
				}
				break
			}
			incomplete, ok := providers.AsRemoteIncompleteError(err)
			if ok && usageIsZero(result.Usage) {
				addUsage(&aggregate.Usage, usageFromIncomplete(incomplete))
			}
			recoveryOutput := repositoryAdaptiveRecoveryOutputTokens(options.Provider, maxOutputTokens, incomplete)
			if ok {
				adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, incomplete.TokenLimit)
			}
			if !ok || incomplete.Reason != "max_output_tokens" || recoveryOutput <= maxOutputTokens || !outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, adaptiveTokenLimit) {
				if fallbackErr := retainValidatedSource(err); fallbackErr != nil {
					return partialFailure("repository synthesis", fallbackErr)
				}
				break
			}
			if progressErr := progress(options, Progress{
				Stage: "adaptive-output-retry", Scope: "repository-synthesis", InputBytes: summaryBytes(group),
				Detail: fmt.Sprintf("increase synthesis output allowance from %d to %d tokens", maxOutputTokens, recoveryOutput),
			}); progressErr != nil {
				return partialFailure("repository synthesis", progressErr)
			}
			maxOutputTokens = recoveryOutput
		}
		if !usedValidatedSourceFallback {
			addUsage(&aggregate.Usage, result.Usage)
			aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
			if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership, confirmedUses); err != nil {
				return partialFailure("repository synthesis", err)
			}
			grouped, err := assignSynthesisCandidateIDs(result.Result, confirmedUses)
			if err != nil {
				return partialFailure("repository synthesis", err)
			}
			summaries[0] = attachRepositoryGroupingHints(grouped, group)
		}
	}
	if !usedValidatedSourceFallback {
		hydrated, err := hydrateRepositoryGroupingResult(summaries[0], sourceEvidenceSummaries, confirmedUses)
		if err != nil {
			return partialFailure("repository synthesis evidence reattachment", err)
		}
		if err := validateSystemAttribution(hydrated, profileSystems(systems), options.Ownership, confirmedUses); err != nil {
			return partialFailure("repository synthesis evidence reattachment", err)
		}
		summaries[0] = hydrated
	}
	if options.TargetedBatches {
		aggregate.Coverage.Mode = providers.RepositoryAnalysisTargeted
	} else {
		aggregate.Coverage.Mode = providers.RepositoryAnalysisSynthesis
	}
	aggregate.Coverage.RepositoryFiles = len(repository.Files)
	aggregate.Coverage.RepositoryBytes = repositorySize(repository)
	aggregate.Coverage.Subsystems = len(chunks)
	aggregate.Coverage.SourceBatchesStarted = len(sourceBatchesStarted)
	aggregate.Coverage.SourceBatchesCompleted = len(chunks)
	aggregate.Coverage.SourceBatchesTotal = len(chunks)
	aggregate.Result = summaries[0]
	aggregate.Result.Scope = "."
	if options.TargetedBatches {
		completion := fmt.Sprintf("All %d structurally selected source batch(es) were reviewed within the per-request provider boundary and then globally synthesized.", len(chunks))
		if usedValidatedSourceFallback {
			completion = "The single structurally selected source batch validated; optional synthesis did not complete, so the validated source result was retained without cross-batch reconciliation."
		}
		aggregate.Notes = append([]string{
			completion,
			"Batch boundaries are a context-management mechanism, not declared AI-system boundaries; files outside the local structural candidate set were not model-reviewed.",
			"Repository model analysis is advisory; deterministic findings remain available for comparison.",
		}, aggregate.Notes...)
	} else {
		completion := fmt.Sprintf("The repository exceeded the one-request context budget and was analyzed as %d subsystem slice(s) followed by global synthesis.", len(chunks))
		if usedValidatedSourceFallback {
			completion = "The forced single subsystem validated; optional synthesis did not complete, so the validated source result was retained without cross-subsystem reconciliation."
		}
		aggregate.Notes = append([]string{
			completion,
			"Subsystem boundaries are a context-management mechanism, not declared AI-system boundaries.",
			"Repository-wide model analysis is advisory; deterministic findings and the bounded evidence investigation remain available for comparison.",
		}, aggregate.Notes...)
	}
	return aggregate, nil
}

type repositorySummaryGroup struct {
	summaries       []providers.RepositorySectionResult
	maxOutputTokens int
}

type repositoryChunk struct {
	id              string
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
	id     string
	scope  string
	result providers.RepositoryAnalysisResult
	err    error
}

type repositorySynthesisCall struct {
	index           int
	scope           string
	group           repositorySummaryGroup
	request         providers.RepositoryAnalysisRequest
	inputBytes      int64
	estimatedTokens int
	maxOutputTokens int
}

type repositorySynthesisResponse struct {
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
		CompactSource: options.TargetedBatches,
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
		results[0] = repositorySourceBatchResponse{id: repositoryChunkID(calls[0].chunk), scope: calls[0].chunk.scope, result: result, err: err}
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
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(calls))
	for index := range calls {
		go func(index int) {
			defer waitGroup.Done()
			result, err := reviewRepositoryWithRetry(ctx, reviewer, calls[index].request, parallelOptions)
			results[index] = repositorySourceBatchResponse{id: repositoryChunkID(calls[index].chunk), scope: calls[index].chunk.scope, result: result, err: err}
		}(index)
	}
	waitGroup.Wait()
	return results
}

// recoverRepositorySourceWaveOutputs retries every output-exhausted member of
// one completed source wave together. The original wave already paid the cost
// of discovering which batches need more response space; recovering those
// batches one at a time would turn an otherwise parallel review into several
// serial model generations. Capacity admission and accounting remain explicit.
func recoverRepositorySourceWaveOutputs(
	ctx context.Context,
	reviewer Reviewer,
	initialCalls []repositorySourceBatchCall,
	initialResponses []repositorySourceBatchResponse,
	chunks []repositoryChunk,
	prefetched map[string]repositorySourceBatchResponse,
	observedLimits providers.RateLimitSnapshot,
	adaptiveTokenLimit int,
	unknownWaveLimit int,
	options Options,
) (providers.RateLimitSnapshot, int, error) {
	pending := make([]repositorySourceBatchCall, 0, len(initialCalls))
	for index, response := range initialResponses {
		incomplete, ok := providers.AsRemoteIncompleteError(response.err)
		if !ok || incomplete.Reason != "max_output_tokens" {
			continue
		}
		call := initialCalls[index]
		effectiveTokenLimit := smallerPositive(adaptiveTokenLimit, incomplete.TokenLimit)
		recoveryOutput := repositoryRecoveryOutputTokens(call.maxOutputTokens)
		if recoveryOutput <= call.maxOutputTokens || !outputFitsTokenLimit(incomplete.InputTokens, recoveryOutput, effectiveTokenLimit) {
			continue
		}
		adaptiveTokenLimit = effectiveTokenLimit
		call.chunk.maxOutputTokens = recoveryOutput
		chunks[call.index].maxOutputTokens = recoveryOutput
		call.maxOutputTokens = recoveryOutput
		call.request.MaxOutputTokens = recoveryOutput
		call.request.OutputRecovery = true
		call.estimatedTokens = estimatedRepositoryRequestTokens(call.request)
		pending = append(pending, call)
	}
	for len(pending) > 0 {
		waveLimit, wait := sourceBatchWaveLimit(observedLimits, pending[0].estimatedTokens, len(pending), unknownWaveLimit)
		if waveLimit == 0 {
			if wait <= 0 {
				wait = minimumRateLimitCooldown
			}
			if wait > maxRateLimitTotalWait {
				return observedLimits, adaptiveTokenLimit, fmt.Errorf("one provider-capacity reset wait exceeds the %s automatic per-window wait budget", maxRateLimitTotalWait)
			}
			if err := progress(options, Progress{Stage: "batch-capacity-wait", Scope: pending[0].chunk.scope, Wait: wait, OriginalWait: wait, Detail: "waiting for provider capacity before concurrent source-output recovery"}); err != nil {
				return observedLimits, adaptiveTokenLimit, err
			}
			if err := waitForConfiguredRateLimit(ctx, options, wait); err != nil {
				return observedLimits, adaptiveTokenLimit, err
			}
			observedLimits = replenishRateLimitSnapshot(observedLimits, wait)
			continue
		}

		recoveryWave := make([]repositorySourceBatchCall, 0, waveLimit)
		estimatedTokens := 0
		for _, call := range pending {
			if len(recoveryWave) >= waveLimit {
				break
			}
			if observedLimits.TokensKnown && len(recoveryWave) > 0 && estimatedTokens+call.estimatedTokens > observedLimits.RemainingTokens {
				break
			}
			recoveryWave = append(recoveryWave, call)
			estimatedTokens += call.estimatedTokens
		}
		if len(recoveryWave) == 0 {
			recoveryWave = append(recoveryWave, pending[0])
			estimatedTokens = pending[0].estimatedTokens
		}
		if len(recoveryWave) > 1 {
			detail := fmt.Sprintf("recovering %d output-exhausted source batches concurrently", len(recoveryWave))
			if observedLimits.RequestsKnown && observedLimits.TokensKnown {
				detail = fmt.Sprintf("observed request and token capacity allow %d concurrent source-output recoveries", len(recoveryWave))
			}
			if err := progress(options, Progress{Stage: "targeted-batch-concurrency", Completed: len(recoveryWave), Total: len(pending), Scope: recoveryWave[0].chunk.scope, Detail: detail}); err != nil {
				return observedLimits, adaptiveTokenLimit, err
			}
		}
		for _, call := range recoveryWave {
			if err := progress(options, Progress{
				Stage: "adaptive-output-retry", Completed: call.index, Total: len(chunks), Scope: call.chunk.scope, InputBytes: call.inputBytes,
				Detail: fmt.Sprintf("increase output allowance to %d tokens after the previous response exhausted its output space", call.maxOutputTokens),
			}); err != nil {
				return observedLimits, adaptiveTokenLimit, err
			}
			startStage := "subsystem"
			startCompleted := call.index
			if options.TargetedBatches {
				startStage = "targeted-batch-start"
				startCompleted = call.index + 1
			}
			if err := progress(options, Progress{Stage: startStage, Completed: startCompleted, Total: len(chunks), Scope: call.chunk.scope, InputBytes: call.inputBytes}); err != nil {
				return observedLimits, adaptiveTokenLimit, err
			}
		}

		observedLimits = reserveRateLimitSnapshot(observedLimits, len(recoveryWave), estimatedTokens)
		recovered := runRepositorySourceBatchWave(ctx, reviewer, recoveryWave, options)
		for index, response := range recovered {
			call := recoveryWave[index]
			original := prefetched[response.id]
			prefetched[response.id] = mergeRepositorySourceRecovery(original, response)
			extraRequests := max(0, response.result.Coverage.ProviderRequests-1)
			observedLimits = reserveRateLimitSnapshot(observedLimits, extraRequests, extraRequests*call.estimatedTokens)
			observedLimits = conservativeRateLimitSnapshot(observedLimits, response.result.RateLimits)
		}
		pending = pending[len(recoveryWave):]
	}
	return observedLimits, adaptiveTokenLimit, nil
}

func mergeRepositorySourceRecovery(original, recovery repositorySourceBatchResponse) repositorySourceBatchResponse {
	result := recovery.result
	result.Coverage.FilesSubmitted += original.result.Coverage.FilesSubmitted
	result.Coverage.BytesSubmitted += original.result.Coverage.BytesSubmitted
	result.Coverage.ProviderRequests += original.result.Coverage.ProviderRequests
	addUsage(&result.Usage, original.result.Usage)
	result.RequestDiagnostics = append(append([]providers.RepositoryRequestDiagnostic(nil), original.result.RequestDiagnostics...), result.RequestDiagnostics...)
	result.RateLimits = latestRateLimitSnapshot(original.result.RateLimits, result.RateLimits)
	if result.Provider == providers.None {
		result.Provider = original.result.Provider
	}
	if strings.TrimSpace(result.Model) == "" {
		result.Model = original.result.Model
	}
	return repositorySourceBatchResponse{id: recovery.id, scope: recovery.scope, result: result, err: recovery.err}
}

func prepareRepositorySynthesisCall(level, index int, group repositorySummaryGroup, repository discovery.Repository, files []providers.RepositorySourceFile, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, confirmedUses []providers.RepositoryConfirmedAIUse, adaptiveTokenLimit int, options Options) repositorySynthesisCall {
	scope := fmt.Sprintf("synthesis-level-%d-part-%d", level, index+1)
	maxOutputTokens := group.maxOutputTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
	}
	request := providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisSynthesis, Scope: scope,
		RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
		SubsystemSummaries: group.summaries, MaxOutputTokens: maxOutputTokens, CompactSynthesis: true,
	}
	inputBytes := summaryBytes(group.summaries)
	if encoded, err := json.Marshal(request); err == nil {
		inputBytes = int64(len(encoded))
	}
	return repositorySynthesisCall{
		index: index, scope: scope, group: group, request: request, inputBytes: inputBytes,
		estimatedTokens: int(inputBytes)/charactersPerToken + maxOutputTokens + sourceBatchTokenOverhead,
		maxOutputTokens: maxOutputTokens,
	}
}

func runRepositorySynthesisWave(ctx context.Context, reviewer Reviewer, calls []repositorySynthesisCall, options Options) []repositorySynthesisResponse {
	results := make([]repositorySynthesisResponse, len(calls))
	if len(calls) == 1 {
		result, err := reviewRepositoryWithRetry(ctx, reviewer, calls[0].request, options)
		results[0] = repositorySynthesisResponse{result: result, err: err}
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
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(calls))
	for index := range calls {
		go func(index int) {
			defer waitGroup.Done()
			result, err := reviewRepositoryWithRetry(ctx, reviewer, calls[index].request, parallelOptions)
			results[index] = repositorySynthesisResponse{result: result, err: err}
		}(index)
	}
	waitGroup.Wait()
	return results
}

func mergeRepositorySynthesisAccounting(target *providers.RepositoryAnalysisResult, result providers.RepositoryAnalysisResult) {
	if target == nil {
		return
	}
	target.Coverage.ProviderRequests += result.Coverage.ProviderRequests
	target.Coverage.CitationsChecked += result.Coverage.CitationsChecked
	addUsage(&target.Usage, result.Usage)
	target.RequestDiagnostics = append(target.RequestDiagnostics, result.RequestDiagnostics...)
}

func drainRepositorySynthesisPrefetch(target *providers.RepositoryAnalysisResult, pending map[int]repositorySynthesisResponse) {
	for key, response := range pending {
		mergeRepositorySynthesisAccounting(target, response.result)
		delete(pending, key)
	}
}

// shiftRepositorySynthesisPrefetch preserves already-paid, validated sibling
// responses when the current group is replaced by smaller groups. Their source
// summaries are unchanged; only their queue indices move to the right.
func shiftRepositorySynthesisPrefetch(pending map[int]repositorySynthesisResponse, afterIndex, delta int) {
	if delta <= 0 || len(pending) == 0 {
		return
	}
	moved := make(map[int]repositorySynthesisResponse)
	for index, response := range pending {
		if index <= afterIndex {
			continue
		}
		delete(pending, index)
		moved[index+delta] = response
	}
	for index, response := range moved {
		pending[index] = response
	}
}

func sourceBatchWaveLimit(snapshot providers.RateLimitSnapshot, estimatedTokens, pending, unknownLimit int) (int, time.Duration) {
	if pending <= 0 {
		return 0, 0
	}
	if !snapshot.Available() {
		if unknownLimit < 1 {
			unknownLimit = 1
		}
		return min(pending, min(maxConcurrentSourceBatches, unknownLimit)), 0
	}
	limit := pending
	if limit > maxConcurrentSourceBatches {
		limit = maxConcurrentSourceBatches
	}
	// A provider may disclose only RPM or only TPM. The missing limiter can be
	// the tighter one, so a partial snapshot caps concurrency at one. The known
	// dimension is still authoritative when it says capacity is exhausted.
	if !snapshot.RequestsKnown || !snapshot.TokensKnown {
		limit = min(limit, 1)
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
	cautiousReset := false
	if snapshot.RequestsKnown {
		if snapshot.ResetRequests > 0 && waited >= snapshot.ResetRequests {
			snapshot.RemainingRequests = snapshot.LimitRequests
		} else if snapshot.RemainingRequests <= 0 && snapshot.ResetRequests <= 0 {
			// Some providers expose an exhausted counter without a reset time.
			// A short wait is not evidence that the entire window replenished, so
			// admit one calibration request instead of a full concurrent wave.
			snapshot.RemainingRequests = 1
			cautiousReset = true
		}
	}
	if snapshot.TokensKnown {
		if snapshot.ResetTokens > 0 && waited >= snapshot.ResetTokens {
			snapshot.RemainingTokens = snapshot.LimitTokens
		} else if snapshot.ResetTokens <= 0 && snapshot.RemainingTokens < snapshot.LimitTokens {
			// The provider disclosed a token window but no portable reset time.
			// After the caller's conservative cooldown, admit one calibration
			// request instead of looping forever on a positive-but-insufficient
			// stale remainder.
			snapshot.RemainingTokens = snapshot.LimitTokens
			cautiousReset = true
		}
	}
	if cautiousReset && snapshot.RequestsKnown && snapshot.RemainingRequests > 1 {
		snapshot.RemainingRequests = 1
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

// latestRateLimitSnapshot replaces only the dimensions that a newer response
// actually disclosed. Providers frequently return just RPM or just TPM on a
// retry; replacing the whole snapshot would silently forget the other active
// limiter.
func latestRateLimitSnapshot(current, next providers.RateLimitSnapshot) providers.RateLimitSnapshot {
	result := current
	if next.RequestsKnown {
		result.RequestsKnown = true
		result.LimitRequests = next.LimitRequests
		result.RemainingRequests = next.RemainingRequests
		result.ResetRequests = next.ResetRequests
	}
	if next.TokensKnown {
		result.TokensKnown = true
		result.LimitTokens = next.LimitTokens
		result.RemainingTokens = next.RemainingTokens
		result.ResetTokens = next.ResetTokens
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

func emptyRepositoryGroupingResult(scope string) providers.RepositorySectionResult {
	return providers.RepositorySectionResult{
		Scope: scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
		UnresolvedQuestions: []string{}, EvidenceGaps: []providers.RepositoryEvidenceGap{}, ResolvedEvidenceGaps: []providers.RepositoryResolvedEvidenceGap{},
	}
}

func repositoryGroupingObservationCount(summaries []providers.RepositorySectionResult) int {
	count := 0
	for _, summary := range summaries {
		for _, use := range summary.AIUses {
			count += len(use.MemberObservationIDs)
		}
	}
	return count
}

// compactRepositoryGroupingSummaries removes result fields that synthesis no
// longer has to repeat. Short checked citation summaries and positive fact
// values remain as grouping clues; their complete validated records stay in
// sourceEvidenceSummaries and never depend on the grouping response.
func compactRepositoryGroupingSummaries(summaries []providers.RepositorySectionResult) []providers.RepositorySectionResult {
	result := make([]providers.RepositorySectionResult, 0, len(summaries))
	for _, summary := range summaries {
		compact := emptyRepositoryGroupingResult(summary.Scope)
		candidateIDs := make(map[string]struct{}, len(summary.AIUses))
		for _, use := range summary.AIUses {
			candidateIDs[use.ID] = struct{}{}
			copyUse := providers.RepositoryAIUse{
				ID: use.ID, Name: use.Name, Purpose: use.Purpose, Lifecycle: use.Lifecycle, Confidence: use.Confidence,
				MemberObservationIDs: append([]string(nil), use.MemberObservationIDs...), Evidence: compactRepositoryCitations(use.Evidence, 2),
				UnresolvedQuestions: []string{},
			}
			compact.AIUses = append(compact.AIUses, copyUse)
		}
		for _, factSet := range summary.AIUseFacts {
			if _, candidate := candidateIDs[factSet.AIUseID]; !candidate {
				continue
			}
			copySet := providers.RepositoryAIUseFactSet{AIUseID: factSet.AIUseID, Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}
			for _, fact := range factSet.Facts {
				copySet.Facts = append(copySet.Facts, providers.RepositoryAIUseFact{
					Field: fact.Field, Values: append([]string(nil), fact.Values...), Confidence: fact.Confidence,
					Rationale: "", Evidence: []providers.RepositoryCitation{},
				})
			}
			compact.AIUseFacts = append(compact.AIUseFacts, copySet)
		}
		gapIDs := make(map[string]struct{})
		for _, gap := range append(repositoryEvidenceGaps(summary), summary.EvidenceGaps...) {
			if _, duplicate := gapIDs[gap.ID]; duplicate {
				continue
			}
			gapIDs[gap.ID] = struct{}{}
			compact.EvidenceGaps = append(compact.EvidenceGaps, gap)
		}
		compact.ResolvedEvidenceGaps = append([]providers.RepositoryResolvedEvidenceGap(nil), summary.ResolvedEvidenceGaps...)
		result = append(result, compact)
	}
	return result
}

const (
	repositoryGapUseQuestion        = "use-question"
	repositoryGapFactQuestion       = "fact-question"
	repositoryGapObjectiveMissing   = "objective-missing"
	repositoryGapObjectiveQuestion  = "objective-question"
	repositoryGapRepositoryQuestion = "repository-question"
)

func repositoryEvidenceGaps(summary providers.RepositorySectionResult) []providers.RepositoryEvidenceGap {
	useMembers := make(map[string][]string, len(summary.AIUses))
	allMembers := make([]string, 0)
	citationMembers := make(map[string][]string)
	for _, use := range summary.AIUses {
		members := append([]string(nil), use.MemberObservationIDs...)
		sort.Strings(members)
		useMembers[use.ID] = members
		allMembers = append(allMembers, members...)
		for _, citation := range use.Evidence {
			key := filepath.ToSlash(citation.Path) + "\x00" + fmt.Sprint(citation.Line)
			citationMembers[key] = append(citationMembers[key], members...)
		}
	}
	allMembers = uniqueRepositoryStrings(allMembers, 0)
	result := make([]providers.RepositoryEvidenceGap, 0)
	add := func(kind, text string, origins []string) {
		text = strings.TrimSpace(text)
		origins = uniqueRepositoryStrings(append([]string(nil), origins...), 0)
		sort.Strings(origins)
		if text == "" || len(origins) == 0 {
			return
		}
		seed := summary.Scope + "\x00" + kind + "\x00" + text + "\x00" + strings.Join(origins, "\x00")
		digest := sha256.Sum256([]byte(seed))
		result = append(result, providers.RepositoryEvidenceGap{
			ID: fmt.Sprintf("gap-%x", digest[:12]), Kind: kind, Text: text, OriginObservationIDs: origins,
		})
	}
	for _, use := range summary.AIUses {
		for _, question := range use.UnresolvedQuestions {
			add(repositoryGapUseQuestion, question, useMembers[use.ID])
		}
	}
	for _, factSet := range summary.AIUseFacts {
		for _, question := range factSet.UnresolvedQuestions {
			add(repositoryGapFactQuestion, question, useMembers[factSet.AIUseID])
		}
	}
	for _, observation := range summary.ObjectiveObservations {
		origins := append([]string(nil), useMembers[observation.AIUseID]...)
		if len(origins) == 0 {
			for _, citation := range append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...) {
				key := filepath.ToSlash(citation.Path) + "\x00" + fmt.Sprint(citation.Line)
				origins = append(origins, citationMembers[key]...)
			}
		}
		if len(origins) == 0 {
			origins = allMembers
		}
		for _, missing := range observation.MissingEvidence {
			add(repositoryGapObjectiveMissing, missing, origins)
		}
		for _, question := range observation.UnresolvedQuestions {
			add(repositoryGapObjectiveQuestion, question, origins)
		}
	}
	for _, question := range summary.UnresolvedQuestions {
		add(repositoryGapRepositoryQuestion, question, allMembers)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// attachRepositoryGroupingHints carries a small amount of semantic context to
// a later grouping level without asking the model to reproduce the full source
// result. It is never used as the final evidence record.
func attachRepositoryGroupingHints(grouped providers.RepositorySectionResult, inputs []providers.RepositorySectionResult) providers.RepositorySectionResult {
	byObservation := make(map[string]providers.RepositoryAIUse)
	factsByObservation := make(map[string]providers.RepositoryAIUseFactSet)
	for _, summary := range inputs {
		sets := make(map[string]providers.RepositoryAIUseFactSet, len(summary.AIUseFacts))
		for _, set := range summary.AIUseFacts {
			sets[set.AIUseID] = set
		}
		for _, use := range summary.AIUses {
			for _, observationID := range use.MemberObservationIDs {
				byObservation[observationID] = use
				if set, exists := sets[use.ID]; exists {
					factsByObservation[observationID] = set
				}
			}
		}
	}
	grouped.AIUseFacts = []providers.RepositoryAIUseFactSet{}
	grouped.ObjectiveObservations = []providers.RepositoryObjectiveObservation{}
	grouped.UnmappedObservations = []providers.RepositoryUnmappedObservation{}
	grouped.UnresolvedQuestions = []string{}
	resolved := make(map[string]struct{})
	for _, input := range inputs {
		for _, resolution := range input.ResolvedEvidenceGaps {
			if _, duplicate := resolved[resolution.GapID]; duplicate {
				continue
			}
			resolved[resolution.GapID] = struct{}{}
			grouped.ResolvedEvidenceGaps = append(grouped.ResolvedEvidenceGaps, resolution)
		}
	}
	for _, resolution := range grouped.ResolvedEvidenceGaps {
		resolved[resolution.GapID] = struct{}{}
	}
	for _, input := range inputs {
		for _, gap := range input.EvidenceGaps {
			if _, answered := resolved[gap.ID]; !answered {
				grouped.EvidenceGaps = append(grouped.EvidenceGaps, gap)
			}
		}
	}
	for index := range grouped.AIUses {
		use := &grouped.AIUses[index]
		var citations []providers.RepositoryCitation
		factSets := make([]providers.RepositoryAIUseFactSet, 0, len(use.MemberObservationIDs))
		for _, observationID := range use.MemberObservationIDs {
			if source, exists := byObservation[observationID]; exists {
				citations = append(citations, source.Evidence...)
			}
			if set, exists := factsByObservation[observationID]; exists {
				factSets = append(factSets, set)
			}
		}
		use.Evidence = compactRepositoryCitations(citations, 3)
		use.UnresolvedQuestions = []string{}
		merged, _ := mergeRepositoryFactSets(use.ID, factSets, false)
		grouped.AIUseFacts = append(grouped.AIUseFacts, merged)
	}
	return grouped
}

func hydrateRepositoryGroupingResult(grouped providers.RepositorySectionResult, sources []providers.RepositorySectionResult, confirmedUses []providers.RepositoryConfirmedAIUse) (providers.RepositorySectionResult, error) {
	knownGaps := make(map[string]providers.RepositoryEvidenceGap)
	for _, source := range sources {
		for _, gap := range repositoryEvidenceGaps(source) {
			knownGaps[gap.ID] = gap
		}
	}
	resolvedGapIDs := make(map[string]struct{}, len(grouped.ResolvedEvidenceGaps))
	resolutions := make([]providers.RepositoryResolvedEvidenceGap, 0, len(grouped.ResolvedEvidenceGaps))
	for _, resolution := range grouped.ResolvedEvidenceGaps {
		gap, exists := knownGaps[resolution.GapID]
		if !exists {
			return providers.RepositorySectionResult{}, fmt.Errorf("repository grouping resolved unknown evidence gap %q", resolution.GapID)
		}
		if _, duplicate := resolvedGapIDs[resolution.GapID]; duplicate {
			continue
		}
		resolvedGapIDs[resolution.GapID] = struct{}{}
		resolution.Kind = gap.Kind
		resolution.OriginalText = gap.Text
		resolutions = append(resolutions, resolution)
	}
	filteredSources := make([]providers.RepositorySectionResult, 0, len(sources))
	for _, source := range sources {
		filteredSources = append(filteredSources, filterResolvedRepositoryEvidenceGaps(source, resolvedGapIDs))
	}
	sources = filteredSources
	byObservation := make(map[string]providers.RepositoryAIUse)
	factsByObservation := make(map[string]providers.RepositoryAIUseFactSet)
	confirmed := make(map[string]struct{}, len(confirmedUses))
	for _, use := range confirmedUses {
		confirmed[use.ID] = struct{}{}
	}
	confirmedFactSets := make(map[string][]providers.RepositoryAIUseFactSet, len(confirmedUses))
	for _, summary := range sources {
		sets := make(map[string]providers.RepositoryAIUseFactSet, len(summary.AIUseFacts))
		for _, set := range summary.AIUseFacts {
			sets[set.AIUseID] = set
			if _, exists := confirmed[set.AIUseID]; exists {
				confirmedFactSets[set.AIUseID] = append(confirmedFactSets[set.AIUseID], set)
			}
		}
		for _, use := range summary.AIUses {
			set, hasFacts := sets[use.ID]
			for _, observationID := range use.MemberObservationIDs {
				if _, duplicate := byObservation[observationID]; duplicate {
					return providers.RepositorySectionResult{}, fmt.Errorf("validated source evidence repeats observation %q", observationID)
				}
				byObservation[observationID] = use
				if hasFacts {
					factsByObservation[observationID] = set
				}
			}
		}
	}

	result := emptyRepositoryGroupingResult(grouped.Scope)
	result.ResolvedEvidenceGaps = resolutions
	usedObservations := make(map[string]struct{}, len(byObservation))
	for _, groupedUse := range grouped.AIUses {
		use := groupedUse
		use.Evidence = []providers.RepositoryCitation{}
		use.UnresolvedQuestions = []string{}
		factSets := make([]providers.RepositoryAIUseFactSet, 0, len(use.MemberObservationIDs))
		for _, observationID := range use.MemberObservationIDs {
			source, exists := byObservation[observationID]
			if !exists {
				return providers.RepositorySectionResult{}, fmt.Errorf("repository grouping references unknown validated observation %q", observationID)
			}
			if _, duplicate := usedObservations[observationID]; duplicate {
				return providers.RepositorySectionResult{}, fmt.Errorf("repository grouping repeats validated observation %q", observationID)
			}
			usedObservations[observationID] = struct{}{}
			use.Evidence = append(use.Evidence, source.Evidence...)
			use.UnresolvedQuestions = append(use.UnresolvedQuestions, source.UnresolvedQuestions...)
			if set, exists := factsByObservation[observationID]; exists {
				factSets = append(factSets, set)
			}
		}
		use.Evidence = compactRepositoryCitations(use.Evidence, 0)
		use.UnresolvedQuestions = uniqueRepositoryStrings(use.UnresolvedQuestions, 100)
		if len(use.Evidence) == 0 {
			return providers.RepositorySectionResult{}, fmt.Errorf("repository grouping use %q has no validated source evidence", use.ID)
		}
		result.AIUses = append(result.AIUses, use)
		mergedFacts, err := mergeRepositoryFactSets(use.ID, factSets, true)
		if err != nil {
			return providers.RepositorySectionResult{}, err
		}
		result.AIUseFacts = append(result.AIUseFacts, mergedFacts)
	}
	if len(usedObservations) != len(byObservation) {
		return providers.RepositorySectionResult{}, fmt.Errorf("repository grouping retained %d of %d validated observations", len(usedObservations), len(byObservation))
	}
	confirmedIDs := make([]string, 0, len(confirmedFactSets))
	for id := range confirmedFactSets {
		confirmedIDs = append(confirmedIDs, id)
	}
	sort.Strings(confirmedIDs)
	for _, id := range confirmedIDs {
		mergedFacts, err := mergeRepositoryFactSets(id, confirmedFactSets[id], true)
		if err != nil {
			return providers.RepositorySectionResult{}, err
		}
		result.AIUseFacts = append(result.AIUseFacts, mergedFacts)
	}

	objectiveByKey := make(map[string]int)
	unmappedSeen := make(map[string]struct{})
	for _, summary := range sources {
		for _, observation := range summary.ObjectiveObservations {
			key := observation.AIUseID + "\x00" + observation.ObjectiveID + "\x00" + observation.SystemID
			if existingIndex, exists := objectiveByKey[key]; exists {
				result.ObjectiveObservations[existingIndex] = mergeRepositoryObjectiveObservations(result.ObjectiveObservations[existingIndex], observation)
				continue
			}
			observation.TechnicalVerdict = observation.DerivedTechnicalVerdict()
			objectiveByKey[key] = len(result.ObjectiveObservations)
			result.ObjectiveObservations = append(result.ObjectiveObservations, observation)
		}
		for _, observation := range summary.UnmappedObservations {
			identity := repositoryUnmappedIdentity(observation)
			if _, duplicate := unmappedSeen[identity]; duplicate {
				continue
			}
			if len(result.UnmappedObservations) >= 100 {
				return providers.RepositorySectionResult{}, errors.New("validated source evidence contains more than 100 distinct unmapped observations")
			}
			unmappedSeen[identity] = struct{}{}
			result.UnmappedObservations = append(result.UnmappedObservations, observation)
		}
		result.UnresolvedQuestions = append(result.UnresolvedQuestions, summary.UnresolvedQuestions...)
	}
	result.UnresolvedQuestions = uniqueRepositoryStrings(result.UnresolvedQuestions, 100)
	return result, nil
}

func filterResolvedRepositoryEvidenceGaps(summary providers.RepositorySectionResult, resolved map[string]struct{}) providers.RepositorySectionResult {
	if len(resolved) == 0 {
		return summary
	}
	isResolved := func(kind, text string, origins []string) bool {
		copySummary := emptyRepositoryGroupingResult(summary.Scope)
		copySummary.AIUses = append([]providers.RepositoryAIUse(nil), summary.AIUses...)
		switch kind {
		case repositoryGapUseQuestion:
			copySummary.AIUses = []providers.RepositoryAIUse{{ID: "gap", MemberObservationIDs: origins, UnresolvedQuestions: []string{text}}}
		case repositoryGapFactQuestion:
			copySummary.AIUses = []providers.RepositoryAIUse{{ID: "gap", MemberObservationIDs: origins}}
			copySummary.AIUseFacts = []providers.RepositoryAIUseFactSet{{AIUseID: "gap", Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{text}}}
		default:
			return false
		}
		for _, gap := range repositoryEvidenceGaps(copySummary) {
			if _, ok := resolved[gap.ID]; ok {
				return true
			}
		}
		return false
	}
	members := make(map[string][]string, len(summary.AIUses))
	for _, use := range summary.AIUses {
		members[use.ID] = append([]string(nil), use.MemberObservationIDs...)
	}
	for index := range summary.AIUses {
		questions := summary.AIUses[index].UnresolvedQuestions[:0]
		for _, question := range summary.AIUses[index].UnresolvedQuestions {
			if !isResolved(repositoryGapUseQuestion, question, members[summary.AIUses[index].ID]) {
				questions = append(questions, question)
			}
		}
		summary.AIUses[index].UnresolvedQuestions = questions
	}
	for index := range summary.AIUseFacts {
		questions := summary.AIUseFacts[index].UnresolvedQuestions[:0]
		for _, question := range summary.AIUseFacts[index].UnresolvedQuestions {
			if !isResolved(repositoryGapFactQuestion, question, members[summary.AIUseFacts[index].AIUseID]) {
				questions = append(questions, question)
			}
		}
		summary.AIUseFacts[index].UnresolvedQuestions = questions
	}
	allGaps := repositoryEvidenceGaps(summary)
	type gapCounts struct{ total, resolved int }
	resolvedText := make(map[string]gapCounts)
	for _, gap := range allGaps {
		key := gap.Kind + "\x00" + gap.Text
		counts := resolvedText[key]
		counts.total++
		if _, ok := resolved[gap.ID]; ok {
			counts.resolved++
		}
		resolvedText[key] = counts
	}
	fullyResolved := func(kind, text string) bool {
		counts := resolvedText[kind+"\x00"+text]
		return counts.total > 0 && counts.total == counts.resolved
	}
	for index := range summary.ObjectiveObservations {
		missing := summary.ObjectiveObservations[index].MissingEvidence[:0]
		for _, value := range summary.ObjectiveObservations[index].MissingEvidence {
			if !fullyResolved(repositoryGapObjectiveMissing, value) {
				missing = append(missing, value)
			}
		}
		summary.ObjectiveObservations[index].MissingEvidence = missing
		questions := summary.ObjectiveObservations[index].UnresolvedQuestions[:0]
		for _, value := range summary.ObjectiveObservations[index].UnresolvedQuestions {
			if !fullyResolved(repositoryGapObjectiveQuestion, value) {
				questions = append(questions, value)
			}
		}
		summary.ObjectiveObservations[index].UnresolvedQuestions = questions
	}
	questions := summary.UnresolvedQuestions[:0]
	for _, value := range summary.UnresolvedQuestions {
		if !fullyResolved(repositoryGapRepositoryQuestion, value) {
			questions = append(questions, value)
		}
	}
	summary.UnresolvedQuestions = questions
	return summary
}

func mergeRepositoryFactSets(useID string, sets []providers.RepositoryAIUseFactSet, retainEvidence bool) (providers.RepositoryAIUseFactSet, error) {
	result := providers.RepositoryAIUseFactSet{AIUseID: useID, Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}
	byField := make(map[profile.CodeFactField]int)
	for _, set := range sets {
		result.UnresolvedQuestions = append(result.UnresolvedQuestions, set.UnresolvedQuestions...)
		for _, fact := range set.Facts {
			index, exists := byField[fact.Field]
			if !exists {
				copyFact := fact
				copyFact.Values = append([]string(nil), fact.Values...)
				copyFact.Evidence = append([]providers.RepositoryCitation(nil), fact.Evidence...)
				if !retainEvidence {
					copyFact.Rationale = ""
					copyFact.Evidence = []providers.RepositoryCitation{}
				}
				byField[fact.Field] = len(result.Facts)
				result.Facts = append(result.Facts, copyFact)
				continue
			}
			merged := &result.Facts[index]
			values := uniqueRepositoryStrings(append(merged.Values, fact.Values...), 0)
			// The eight-value bound belongs to each model response, not to the
			// final deterministic union of several validated source batches. The
			// assembled report is ordinary typed JSON and can retain every checked
			// value without asking the synthesis model to repeat or truncate it.
			if !retainEvidence && len(values) > 8 {
				values = values[:8]
			}
			merged.Values = values
			merged.Confidence = lowerRepositoryConfidence(merged.Confidence, fact.Confidence)
			if retainEvidence {
				merged.Evidence = compactRepositoryCitations(append(merged.Evidence, fact.Evidence...), 0)
			}
		}
	}
	result.UnresolvedQuestions = uniqueRepositoryStrings(result.UnresolvedQuestions, 100)
	return result, nil
}

func mergeRepositoryObjectiveObservations(left, right providers.RepositoryObjectiveObservation) providers.RepositoryObjectiveObservation {
	if left.Strength != right.Strength {
		left.Strength = providers.StrengthUncertain
		left.Confidence = "low"
		left.Rationale = "Validated source batches returned differing code-level assessments; the combined result remains uncertain."
	} else {
		left.Confidence = lowerRepositoryConfidence(left.Confidence, right.Confidence)
	}
	left.SupportingEvidence = compactRepositoryCitations(append(left.SupportingEvidence, right.SupportingEvidence...), 0)
	left.ContradictoryEvidence = compactRepositoryCitations(append(left.ContradictoryEvidence, right.ContradictoryEvidence...), 0)
	left.MissingEvidence = uniqueRepositoryStrings(append(left.MissingEvidence, right.MissingEvidence...), 100)
	left.UnresolvedQuestions = uniqueRepositoryStrings(append(left.UnresolvedQuestions, right.UnresolvedQuestions...), 100)
	left.TechnicalVerdict = left.DerivedTechnicalVerdict()
	return left
}

func compactRepositoryCitations(values []providers.RepositoryCitation, limit int) []providers.RepositoryCitation {
	result := make([]providers.RepositoryCitation, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key := filepath.ToSlash(value.Path) + "\x00" + fmt.Sprint(value.Line)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func uniqueRepositoryStrings(values []string, limit int) []string {
	result := make([]string, 0, min(len(values), limit))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func lowerRepositoryConfidence(left, right string) string {
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if rank[right] < rank[left] {
		return right
	}
	return left
}

func repositoryUnmappedIdentity(value providers.RepositoryUnmappedObservation) string {
	parts := []string{value.Summary, value.Reason, value.Confidence, value.SuggestedReview}
	for _, citation := range value.Evidence {
		parts = append(parts, filepath.ToSlash(citation.Path), fmt.Sprint(citation.Line))
	}
	return strings.Join(parts, "\x00")
}

func repositoryRequestPhase(request providers.RepositoryAnalysisRequest) string {
	if request.CompactSynthesis || request.Mode == providers.RepositoryAnalysisSynthesis {
		return "synthesis"
	}
	if request.CompactSource || request.Mode == providers.RepositoryAnalysisSubsystem {
		return "source"
	}
	if request.Mode == providers.RepositoryAnalysisFull {
		return "full"
	}
	return "targeted"
}

func repositoryRequestOutcome(err error) (string, string) {
	if err == nil {
		return "completed", ""
	}
	if _, ok := providers.AsRepositoryValidationError(err); ok {
		return "retryable-error", "structured-validation"
	}
	if value, ok := providers.AsRemoteRateLimitError(err); ok {
		switch {
		case value.RequestTooLarge:
			return "retryable-error", "request-too-large"
		case value.Permanent:
			return "failed", "permanent-quota"
		default:
			return "retryable-error", "rate-limit"
		}
	}
	if _, ok := providers.AsRemoteTransientError(err); ok {
		return "retryable-error", "transient-provider"
	}
	if value, ok := providers.AsRemoteIncompleteError(err); ok {
		reason := strings.TrimSpace(value.Reason)
		if reason == "" {
			reason = "incomplete-output"
		}
		return "retryable-error", reason
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "failed", "cancelled"
	}
	return "failed", "provider-error"
}

func requestDiagnosticDetail(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return ":" + reason
}

func repositoryRequestDiagnosticBytes(request providers.RepositoryAnalysisRequest) int64 {
	encoded, err := json.Marshal(request)
	if err == nil {
		return int64(len(encoded))
	}
	if request.Mode == providers.RepositoryAnalysisSynthesis {
		return summaryBytes(request.SubsystemSummaries)
	}
	return requestContextBytes(request.Files, request.Graph)
}

func inferredCandidateID(sortedObservationIDs []string) string {
	digest := sha256.Sum256([]byte(strings.Join(sortedObservationIDs, "\x00")))
	return fmt.Sprintf("inferred-use-%x", digest[:16])
}

func reviewRepositoryWithRetry(ctx context.Context, reviewer Reviewer, request providers.RepositoryAnalysisRequest, options Options) (providers.RepositoryAnalysisResult, error) {
	totalWait := time.Duration(0)
	var attemptedUsage providers.Usage
	var diagnostics []providers.RepositoryRequestDiagnostic
	attemptedFiles := 0
	var attemptedSourceBytes int64
	providerRequests := 0
	validationRepairs := 0
	transientRetries := 0
	var latestLimits providers.RateLimitSnapshot
	for attempt := 0; ; attempt++ {
		gateHeld := false
		if attempt > 0 && options.retryGate != nil {
			select {
			case options.retryGate <- struct{}{}:
				gateHeld = true
			case <-ctx.Done():
				return providers.RepositoryAnalysisResult{
					Provider: options.Provider, Model: options.Model,
					Coverage: providers.RepositoryCoverage{FilesSubmitted: attemptedFiles, BytesSubmitted: attemptedSourceBytes, ProviderRequests: providerRequests},
					Usage:    attemptedUsage, RequestDiagnostics: diagnostics, RateLimits: latestLimits,
				}, ctx.Err()
			}
		}
		if !options.requestBudget.reserve() {
			if gateHeld {
				<-options.retryGate
			}
			return providers.RepositoryAnalysisResult{
				Provider: options.Provider, Model: options.Model,
				Coverage: providers.RepositoryCoverage{FilesSubmitted: attemptedFiles, BytesSubmitted: attemptedSourceBytes, ProviderRequests: providerRequests},
				Usage:    attemptedUsage, RequestDiagnostics: diagnostics, RateLimits: latestLimits,
			}, fmt.Errorf("repository review reached the safety ceiling of %d provider requests", MaxProviderRequestsPerRun)
		}
		attemptedFiles += len(request.Files)
		attemptedSourceBytes += repositorySourceContentBytes(request.Files)
		providerRequests++
		started := time.Now()
		result, err := reviewer.ReviewRepository(ctx, request)
		duration := time.Since(started)
		if gateHeld {
			<-options.retryGate
		}
		if _, representation := providers.AsRepositoryRepresentationError(err); representation {
			// The provider adapter rejected an already validated synthesis shape
			// before making a network call. Undo the optimistic attempt/transfer
			// reservation so splitting remains truthful and does not consume the
			// paid-request safety ceiling.
			options.requestBudget.releaseCount(1)
			attemptedFiles -= len(request.Files)
			attemptedSourceBytes -= repositorySourceContentBytes(request.Files)
			providerRequests--
			result.Usage = attemptedUsage
			result.Coverage.FilesSubmitted = attemptedFiles
			result.Coverage.BytesSubmitted = attemptedSourceBytes
			result.Coverage.ProviderRequests = providerRequests
			result.RequestDiagnostics = append([]providers.RepositoryRequestDiagnostic(nil), diagnostics...)
			return result, err
		}
		addUsage(&attemptedUsage, result.Usage)
		if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && usageIsZero(result.Usage) {
			addUsage(&attemptedUsage, usageFromIncomplete(incomplete))
		}
		result.Usage = attemptedUsage
		result.Coverage.FilesSubmitted = attemptedFiles
		result.Coverage.BytesSubmitted = attemptedSourceBytes
		result.Coverage.ProviderRequests = providerRequests
		outcome, retryReason := repositoryRequestOutcome(err)
		diagnostics = append(diagnostics, providers.RepositoryRequestDiagnostic{
			Phase: repositoryRequestPhase(request), Scope: request.Scope, Attempt: providerRequests,
			DurationNS: duration.Nanoseconds(), Outcome: outcome, RetryReason: retryReason,
			InputFiles: len(request.Files), InputBytes: repositoryRequestDiagnosticBytes(request),
		})
		result.RequestDiagnostics = append([]providers.RepositoryRequestDiagnostic(nil), diagnostics...)
		if result.RateLimits.Available() {
			latestLimits = latestRateLimitSnapshot(latestLimits, result.RateLimits)
			result.RateLimits = latestLimits
		} else if latestLimits.Available() {
			result.RateLimits = latestLimits
		}
		if progressErr := progress(options, Progress{
			Stage: "provider-request-complete", Completed: providerRequests, Scope: request.Scope,
			Duration: duration, Detail: outcome + requestDiagnosticDetail(retryReason),
		}); progressErr != nil {
			return result, progressErr
		}
		if err == nil {
			return result, nil
		}
		if validationErr, ok := providers.AsRepositoryValidationError(err); ok {
			if validationRepairs >= maxValidationRepairRetries {
				return result, fmt.Errorf("structured repository response remained invalid after %d corrective regeneration attempt(s): %w", validationRepairs, err)
			}
			validationRepairs++
			request.ValidationFeedback = validationErr.Diagnostic
			if progressErr := progress(options, Progress{
				Stage: "validation-repair", Completed: validationRepairs, Total: maxValidationRepairRetries,
				Scope: request.Scope, Detail: validationErr.Diagnostic,
			}); progressErr != nil {
				return result, progressErr
			}
			continue
		}
		delay, retryDetail, retryable := repositoryRetryPolicy(err, transientRetries)
		if !retryable {
			return result, err
		}
		if transientRetries >= maxTransientRetryAttempts {
			return result, fmt.Errorf("provider remained unavailable after %d bounded retry cycle(s): %w", transientRetries, err)
		}
		transientRetries++
		// Production waits add a small random spread so independently started
		// scans do not all resume on the same millisecond. Injected test waits
		// stay deterministic.
		if options.Wait == nil {
			delay = jitteredRetryDelay(delay)
		}
		if totalWait+delay > maxRateLimitTotalWait {
			return result, fmt.Errorf("provider retry wait exceeded the %s automatic budget after %d retry cycle(s): %w", maxRateLimitTotalWait, transientRetries, err)
		}
		totalWait += delay
		if err := progress(options, Progress{
			Stage: "rate-limit-wait", Completed: transientRetries,
			Scope: request.Scope, Wait: delay, OriginalWait: delay, Detail: retryDetail,
		}); err != nil {
			return result, err
		}
		if options.Wait != nil {
			if err := options.Wait(ctx, delay); err != nil {
				return result, err
			}
			latestLimits = replenishRateLimitSnapshot(latestLimits, delay)
			continue
		}
		if err := waitForRateLimit(ctx, delay, func(remaining time.Duration) error {
			return progress(options, Progress{
				Stage: "rate-limit-wait", Completed: transientRetries,
				Scope: request.Scope, Wait: remaining, OriginalWait: delay, Detail: retryDetail,
			})
		}); err != nil {
			return result, err
		}
		latestLimits = replenishRateLimitSnapshot(latestLimits, delay)
		if err := progress(options, Progress{
			Stage: "rate-limit-resume", Completed: transientRetries,
			Scope: request.Scope, OriginalWait: delay, Detail: retryDetail,
		}); err != nil {
			return result, err
		}
	}
}

func repositoryRetryPolicy(err error, priorRetries int) (time.Duration, string, bool) {
	if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok {
		if rateLimit.RequestTooLarge || rateLimit.Permanent {
			return 0, "", false
		}
		delay := rateLimit.RetryAfter
		if observedReset := exhaustedCapacityReset(rateLimit.RateLimits); observedReset > delay {
			delay = observedReset
		}
		if delay < minimumRateLimitCooldown {
			delay = exponentialRetryDelay(priorRetries)
		}
		return delay, "temporary provider rate limit", true
	}
	if transient, ok := providers.AsRemoteTransientError(err); ok {
		delay := transient.RetryAfter
		if observedReset := exhaustedCapacityReset(transient.RateLimits); observedReset > delay {
			delay = observedReset
		}
		if delay < minimumRateLimitCooldown {
			delay = exponentialRetryDelay(priorRetries)
		}
		detail := "temporary provider transport failure"
		if transient.StatusCode > 0 {
			detail = fmt.Sprintf("temporary provider HTTP %d", transient.StatusCode)
		}
		return delay, detail, true
	}
	return 0, "", false
}

func exhaustedCapacityReset(snapshot providers.RateLimitSnapshot) time.Duration {
	var wait time.Duration
	if snapshot.RequestsKnown && snapshot.RemainingRequests <= 0 {
		wait = snapshot.ResetRequests
	}
	if snapshot.TokensKnown && snapshot.RemainingTokens <= 0 && snapshot.ResetTokens > wait {
		wait = snapshot.ResetTokens
	}
	return wait
}

func exponentialRetryDelay(priorRetries int) time.Duration {
	shift := min(max(priorRetries, 0), 6)
	return minimumRateLimitCooldown * time.Duration(1<<shift)
}

func jitteredRetryDelay(delay time.Duration) time.Duration {
	if delay < minimumRateLimitCooldown {
		delay = minimumRateLimitCooldown
	}
	window := delay / 5
	if window <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int63n(int64(window)+1))
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
	if provider == providers.Ollama {
		return 8192
	}
	// Hosted models vary in their maximum structured-output allowance. Start
	// with the broadly portable 4K contract used by targeted review and model
	// qualification, then grow only after a typed output-exhaustion response.
	if tokenLimit <= 0 {
		return targetedRemoteOutputTokens
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

func outputWithinTokenLimit(inputTokens, requested, tokenLimit int) int {
	if tokenLimit <= 0 || inputTokens+requested <= tokenLimit {
		return requested
	}
	available := tokenLimit - inputTokens
	if available < minimumAdaptiveOutput {
		return minimumAdaptiveOutput
	}
	return available
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
	parentID := repositoryChunkID(chunk)
	if len(chunk.files) > 1 {
		split := balancedFileSplit(chunk.files)
		if split <= 0 || split >= len(chunk.files) {
			return nil, false
		}
		return []repositoryChunk{
			{id: parentID + "/part-1", scope: chunk.scope + " (adaptive part 1)", files: append([]providers.RepositorySourceFile(nil), chunk.files[:split]...), maxOutputTokens: maxOutputTokens},
			{id: parentID + "/part-2", scope: chunk.scope + " (adaptive part 2)", files: append([]providers.RepositorySourceFile(nil), chunk.files[split:]...), maxOutputTokens: maxOutputTokens},
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
		{id: parentID + "/segment-1", scope: chunk.scope + " (adaptive segment 1)", files: []providers.RepositorySourceFile{left}, maxOutputTokens: maxOutputTokens},
		{id: parentID + "/segment-2", scope: chunk.scope + " (adaptive segment 2)", files: []providers.RepositorySourceFile{right}, maxOutputTokens: maxOutputTokens},
	}, true
}

// repositoryChunkID is an orchestration identity, not a model-facing label.
// Human-readable scopes are intentionally not identities: a generated scope
// such as "foo (part 1)" can also be a real top-level directory name.
func repositoryChunkID(chunk repositoryChunk) string {
	if value := strings.TrimSpace(chunk.id); value != "" {
		return value
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(chunk.scope))
	for _, file := range chunk.files {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(filepath.ToSlash(file.Path)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(fmt.Sprintf("%d", file.ContentStartLine)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.Content))
	}
	return fmt.Sprintf("source-%x", hash.Sum(nil))
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
	if len(group.summaries) < 2 {
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

// fragmentRepositorySection splits one validated summary along its existing
// semantic identities. It never invents or merges an AI use: candidate facts
// and objective observations stay with their exact candidate ID, while
// confirmed-use facts, unassigned objectives, unmapped observations, and
// unresolved questions become independent synthesis atoms. This lets a large
// summary be reduced without truncating model evidence.
func fragmentRepositorySection(value providers.RepositorySectionResult) ([]providers.RepositorySectionResult, bool) {
	newFragment := func() providers.RepositorySectionResult {
		return providers.RepositorySectionResult{
			Scope:  value.Scope,
			AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
			UnmappedObservations:  []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		}
	}
	factUsed := make([]bool, len(value.AIUseFacts))
	objectiveUsed := make([]bool, len(value.ObjectiveObservations))
	fragments := make([]providers.RepositorySectionResult, 0, len(value.AIUses)+len(value.AIUseFacts)+len(value.ObjectiveObservations)+2)
	for _, use := range value.AIUses {
		fragment := newFragment()
		fragment.AIUses = append(fragment.AIUses, use)
		for index, factSet := range value.AIUseFacts {
			if factSet.AIUseID == use.ID {
				fragment.AIUseFacts = append(fragment.AIUseFacts, factSet)
				factUsed[index] = true
			}
		}
		for index, observation := range value.ObjectiveObservations {
			if observation.AIUseID == use.ID {
				fragment.ObjectiveObservations = append(fragment.ObjectiveObservations, observation)
				objectiveUsed[index] = true
			}
		}
		fragments = append(fragments, fragment)
	}
	for factIndex, factSet := range value.AIUseFacts {
		if factUsed[factIndex] {
			continue
		}
		fragment := newFragment()
		fragment.AIUseFacts = append(fragment.AIUseFacts, factSet)
		for index, observation := range value.ObjectiveObservations {
			if !objectiveUsed[index] && observation.AIUseID != "" && observation.AIUseID == factSet.AIUseID {
				fragment.ObjectiveObservations = append(fragment.ObjectiveObservations, observation)
				objectiveUsed[index] = true
			}
		}
		fragments = append(fragments, fragment)
	}
	for index, observation := range value.ObjectiveObservations {
		if objectiveUsed[index] {
			continue
		}
		fragment := newFragment()
		fragment.ObjectiveObservations = append(fragment.ObjectiveObservations, observation)
		fragments = append(fragments, fragment)
	}
	for _, observation := range value.UnmappedObservations {
		fragment := newFragment()
		fragment.UnmappedObservations = append(fragment.UnmappedObservations, observation)
		fragments = append(fragments, fragment)
	}
	if len(value.UnresolvedQuestions) > 0 {
		fragment := newFragment()
		fragment.UnresolvedQuestions = append(fragment.UnresolvedQuestions, value.UnresolvedQuestions...)
		fragments = append(fragments, fragment)
	}
	if len(fragments) < 2 {
		return nil, false
	}
	return fragments, true
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
	for index := range chunks {
		chunks[index].id = fmt.Sprintf("source-%06d", index+1)
	}
	return chunks, nil
}

// partitionTargetedRepository keeps structurally connected evidence together
// when it fits. These are request-context bundles, not inferred AI-use
// boundaries: the model remains responsible for semantic grouping after every
// source response has passed local validation.
func partitionTargetedRepository(repository discovery.Repository, graph codegraph.Graph, files []providers.RepositorySourceFile, confirmedUses []providers.RepositoryConfirmedAIUse, budget int64, separateFiles bool) ([]repositoryChunk, error) {
	if len(files) == 0 {
		return nil, nil
	}
	parent := make(map[string]string, len(files))
	order := make(map[string]int, len(files))
	selected := make(map[string]struct{}, len(files))
	for index, file := range files {
		path := filepath.ToSlash(file.Path)
		if _, exists := selected[path]; exists {
			continue
		}
		selected[path] = struct{}{}
		parent[path] = path
		order[path] = index
	}
	var find func(string) string
	find = func(path string) string {
		root := parent[path]
		if root != path {
			root = find(root)
			parent[path] = root
		}
		return root
	}
	union := func(left, right string) {
		left = filepath.ToSlash(left)
		right = filepath.ToSlash(right)
		if _, ok := selected[left]; !ok {
			return
		}
		if _, ok := selected[right]; !ok {
			return
		}
		leftRoot, rightRoot := find(left), find(right)
		if leftRoot == rightRoot {
			return
		}
		if order[leftRoot] <= order[rightRoot] {
			parent[rightRoot] = leftRoot
			return
		}
		parent[leftRoot] = rightRoot
	}

	if !separateFiles {
		symbolPaths := make(map[string]string, len(graph.Symbols))
		for _, symbol := range graph.Symbols {
			path := filepath.ToSlash(symbol.Path)
			if _, ok := selected[path]; ok {
				symbolPaths[symbol.ID] = path
			}
		}
		for _, edge := range graph.Edges {
			if !edge.Resolved {
				continue
			}
			from, fromOK := symbolPaths[edge.From]
			to, toOK := symbolPaths[edge.To]
			if fromOK && toOK && from != to {
				union(from, to)
			}
		}

		repositoryFiles := make(map[string]discovery.File, len(repository.Files))
		for _, file := range repository.Files {
			repositoryFiles[filepath.ToSlash(file.Path)] = file
		}
		for _, repositoryImport := range graph.Imports {
			importer := filepath.ToSlash(repositoryImport.Path)
			if _, ok := selected[importer]; !ok {
				continue
			}
			for _, imported := range targetedImportedPaths(importer, repositoryImport.ImportedPath, repositoryFiles) {
				union(importer, imported)
			}
		}
		for _, confirmed := range confirmedUses {
			definition := aiuse.Use{Paths: append([]string(nil), confirmed.Paths...)}
			anchor := ""
			for _, file := range files {
				path := filepath.ToSlash(file.Path)
				if !aiuse.UseMatchesPath(definition, path) {
					continue
				}
				if anchor == "" {
					anchor = path
					continue
				}
				union(anchor, path)
			}
		}
	}

	type component struct {
		first int
		files []providers.RepositorySourceFile
	}
	byRoot := make(map[string]*component, len(files))
	for index, file := range files {
		path := filepath.ToSlash(file.Path)
		root := find(path)
		value := byRoot[root]
		if value == nil {
			value = &component{first: index}
			byRoot[root] = value
		}
		value.files = append(value.files, file)
	}
	components := make([]*component, 0, len(byRoot))
	for _, value := range byRoot {
		components = append(components, value)
	}
	sort.Slice(components, func(left, right int) bool { return components[left].first < components[right].first })

	var chunks []repositoryChunk
	current := repositoryChunk{scope: "evidence bundle"}
	var currentSize int64
	flush := func() {
		if len(current.files) == 0 {
			return
		}
		chunks = append(chunks, current)
		current = repositoryChunk{scope: "evidence bundle"}
		currentSize = 0
	}
	for _, value := range components {
		segments := make([]providers.RepositorySourceFile, 0, len(value.files))
		var componentSize int64
		for _, file := range value.files {
			fileSegments, segmentErr := repositoryFileSegments(file, budget)
			if segmentErr != nil {
				return nil, segmentErr
			}
			for _, segment := range fileSegments {
				segments = append(segments, segment)
				componentSize += int64(len(segment.Content) + len(segment.Path) + 100)
			}
		}
		if componentSize <= budget {
			if len(current.files) > 0 && currentSize+componentSize > budget {
				flush()
			}
			current.files = append(current.files, segments...)
			currentSize += componentSize
			if separateFiles {
				flush()
			}
			continue
		}
		flush()
		for _, segment := range segments {
			segmentSize := int64(len(segment.Content) + len(segment.Path) + 100)
			if len(current.files) > 0 && currentSize+segmentSize > budget {
				flush()
			}
			current.files = append(current.files, segment)
			currentSize += segmentSize
		}
		flush()
	}
	flush()
	for index := range chunks {
		chunks[index].id = fmt.Sprintf("source-%06d", index+1)
		if len(chunks) > 1 {
			chunks[index].scope = fmt.Sprintf("evidence bundle (part %d)", index+1)
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
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, duplicate := seen[value.Path]; duplicate {
			continue
		}
		seen[value.Path] = struct{}{}
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

func estimatedRepositoryRequestTokens(request providers.RepositoryAnalysisRequest) int {
	encoded, _ := json.Marshal(request)
	return len(encoded)/charactersPerToken + request.MaxOutputTokens + sourceBatchTokenOverhead
}

func waitForInitialRepositoryCapacity(ctx context.Context, options Options, estimatedTokens int, scope string) (providers.RateLimitSnapshot, bool, error) {
	snapshot := options.InitialRateLimits
	if snapshot.TokensKnown && snapshot.LimitTokens > 0 && estimatedTokens > snapshot.LimitTokens {
		return snapshot, true, nil
	}
	for snapshot.Available() {
		limit, wait := sourceBatchWaveLimit(snapshot, estimatedTokens, 1, 1)
		if limit > 0 {
			return snapshot, false, nil
		}
		if wait <= 0 {
			wait = minimumRateLimitCooldown
		}
		if wait > maxRateLimitTotalWait {
			return snapshot, false, fmt.Errorf("one provider-capacity reset wait exceeds the %s automatic per-window wait budget", maxRateLimitTotalWait)
		}
		if err := progress(options, Progress{
			Stage: "batch-capacity-wait", Scope: scope, Wait: wait, OriginalWait: wait,
			Detail: "waiting for live provider capacity before the first repository source transfer",
		}); err != nil {
			return snapshot, false, err
		}
		if err := waitForConfiguredRateLimit(ctx, options, wait); err != nil {
			return snapshot, false, err
		}
		snapshot = replenishRateLimitSnapshot(snapshot, wait)
	}
	return snapshot, false, nil
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
	indexed := make(map[string]struct{}, len(wanted))
	for _, file := range files {
		if _, exists := wanted[file.Path]; exists {
			if _, duplicate := indexed[file.Path]; duplicate {
				continue
			}
			indexed[file.Path] = struct{}{}
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
