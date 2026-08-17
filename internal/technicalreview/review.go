package technicalreview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type Reviewer interface {
	ReviewTechnical(context.Context, providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error)
}

type SearchPlanner interface {
	PlanTechnicalSearch(context.Context, providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error)
}

type FollowUpRetriever func(providers.TechnicalCandidate, providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int)

type Progress struct {
	Stage        string
	Current      int
	Total        int
	Candidate    providers.TechnicalCandidate
	Cached       bool
	Attempt      int
	Wait         time.Duration
	OriginalWait time.Duration
}

type Options struct {
	Identity         Identity
	Cache            *Cache
	Refresh          bool
	MaxCandidates    int
	MaxPerObjective  int
	OnProgress       func(Progress) error
	RetrieveFollowUp FollowUpRetriever
	Wait             func(context.Context, time.Duration) error
}

const (
	ProgressStageCandidate        = "candidate"
	ProgressStageRateLimitWait    = "rate-limit-wait"
	ProgressStageRateLimitResume  = "rate-limit-resume"
	ProgressStageOutputRecovery   = "output-recovery"
	ProgressStageValidationRepair = "validation-repair"
	minimumRateLimitCooldown      = 60 * time.Second
	minimumTransientCooldown      = time.Second
	maxRateLimitTotalWait         = 10 * time.Minute
	maxTechnicalProviderAttempts  = 4
	maximumTechnicalOutputTokens  = 16_384
)

type technicalOutputRecoveryError struct {
	cause error
}

type technicalValidationRepairError struct {
	cause error
}

func (value *technicalValidationRepairError) Error() string { return value.cause.Error() }
func (value *technicalValidationRepairError) Unwrap() error { return value.cause }

func (value *technicalOutputRecoveryError) Error() string { return value.cause.Error() }
func (value *technicalOutputRecoveryError) Unwrap() error { return value.cause }

// Run applies source-context-free cache reuse around one-candidate model requests.
// Cache failures are reported as notes and never prevent the requested review.
func Run(ctx context.Context, reviewer Reviewer, request providers.TechnicalReviewRequest, options Options) (providers.TechnicalReviewResult, error) {
	base, err := reviewer.ReviewTechnical(ctx, providers.TechnicalReviewRequest{})
	if err != nil {
		return base, err
	}
	base.InputCandidates = len(request.Candidates)
	if len(request.Candidates) == 0 {
		return base, nil
	}
	base.Observations = []providers.TechnicalObservation{}
	base.Reviewed = 0
	base.Notes = withoutNoCandidatesNote(base.Notes)
	selected := redactTechnicalCandidates(request.Candidates)
	if options.MaxCandidates > 0 && len(selected) > options.MaxCandidates {
		selected = selected[:options.MaxCandidates]
		base.Notes = append(base.Notes, fmt.Sprintf("Technical evidence investigation was limited to the first %d of %d targets.", len(selected), len(request.Candidates)))
	}
	if options.MaxPerObjective > 0 {
		var omitted int
		selected, omitted = limitCandidatesPerObjective(selected, options.MaxPerObjective)
		if omitted > 0 {
			base.Notes = append(base.Notes, fmt.Sprintf("AI review used representative evidence: %d repetitive candidate(s) were omitted after retaining up to %d candidate(s) per system and technical objective. All deterministic evidence remains in the report.", omitted, options.MaxPerObjective))
		}
	}
	cacheEnabled := options.Cache != nil
	cacheHits := 0
	for index, candidate := range selected {
		if err := ctx.Err(); err != nil {
			return base, err
		}
		observation, hit, lookupErr := providers.TechnicalObservation{}, false, error(nil)
		if cacheEnabled && !options.Refresh {
			observation, hit, lookupErr = options.Cache.Lookup(options.Identity, candidate)
			if lookupErr != nil {
				base.Notes = append(base.Notes, "Technical review cache was disabled after an invalid entry: "+lookupErr.Error())
				cacheEnabled = false
				hit = false
			}
		}
		if options.OnProgress != nil {
			if err := options.OnProgress(Progress{Stage: ProgressStageCandidate, Current: index + 1, Total: len(selected), Candidate: candidate, Cached: hit}); err != nil {
				return base, err
			}
		}
		if hit {
			cacheHits++
			base.Observations = append(base.Observations, observation)
			base.Reviewed = len(base.Observations)
			continue
		}
		baseCandidate := candidate
		plan := providers.TechnicalSearchPlan{Queries: []providers.TechnicalSearchQuery{}}
		followUpExcerpts := 0
		if planner, ok := reviewer.(SearchPlanner); ok && options.RetrieveFollowUp != nil && needsModelDirectedFollowUp(baseCandidate) {
			var plannerUsage providers.Usage
			var plannerRequests int
			plan, plannerUsage, plannerRequests, err = planTechnicalSearchWithRetry(ctx, planner, baseCandidate, index+1, len(selected), options)
			addUsage(&base.Usage, plannerUsage)
			base.ProviderRequests += plannerRequests
			if err != nil {
				if ctx.Err() != nil {
					return base, ctx.Err()
				}
				base.Notes = append(base.Notes, candidateFailureNote(candidate, "follow-up planning", err))
				continue
			}
			if strings.HasPrefix(plan.Reason, "Follow-up skipped") {
				base.Notes = append(base.Notes, plan.Reason)
			}
			if plan.Needed {
				candidate, followUpExcerpts = options.RetrieveFollowUp(baseCandidate, plan)
				candidate = redactTechnicalCandidate(candidate)
			}
		}
		partial, err := reviewTechnicalWithRetry(ctx, reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, candidate, index+1, len(selected), options)
		addUsage(&base.Usage, partial.Usage)
		base.ProviderRequests += partial.ProviderRequests
		base.Notes = appendUnique(base.Notes, partial.Notes...)
		if err != nil {
			if ctx.Err() != nil {
				return base, ctx.Err()
			}
			base.Notes = append(base.Notes, candidateFailureNote(candidate, "model review", err))
			continue
		}
		if len(partial.Observations) != 1 || partial.Observations[0].SystemID != candidate.SystemID || partial.Observations[0].ObjectiveID != candidate.ObjectiveID || partial.Observations[0].EvidenceFingerprint != candidate.EvidenceFingerprint {
			base.Notes = append(base.Notes, candidateFailureNote(candidate, "model review", errors.New("technical reviewer did not return exactly one correctly bound observation")))
			continue
		}
		observation = partial.Observations[0]
		observation.FollowUpRequested = plan.Needed
		observation.FollowUpQueries = searchQueryLabels(plan.Queries)
		observation.FollowUpExcerpts = followUpExcerpts
		base.Observations = append(base.Observations, observation)
		base.Reviewed = len(base.Observations)
		if cacheEnabled {
			if err := options.Cache.Store(options.Identity, baseCandidate, observation); err != nil {
				base.Notes = append(base.Notes, "Technical review cache could not be updated and was disabled: "+err.Error())
				cacheEnabled = false
			}
		}
	}
	base.Reviewed = len(base.Observations)
	if cacheHits > 0 {
		base.Notes = append(base.Notes, fmt.Sprintf("Reused %d of %d technical observation(s) from the local source-free review cache.", cacheHits, len(selected)))
	}
	if base.ProviderRequests > 0 {
		base.Notes = append(base.Notes, fmt.Sprintf("Technical evidence investigation usage aggregates %d provider request attempt(s) made during this run.", base.ProviderRequests))
	}
	return base, nil
}

// redactTechnicalCandidates establishes the privacy boundary before any
// reviewer, optional model search planner, progress callback, or cache sees
// repository source. The copy prevents redaction from mutating the caller's
// evidence bundle, while retaining exact paths and line metadata for citation
// validation.
func redactTechnicalCandidates(candidates []providers.TechnicalCandidate) []providers.TechnicalCandidate {
	result := make([]providers.TechnicalCandidate, len(candidates))
	for index, candidate := range candidates {
		result[index] = redactTechnicalCandidate(candidate)
	}
	return result
}

func redactTechnicalCandidate(candidate providers.TechnicalCandidate) providers.TechnicalCandidate {
	candidate.SourceContexts = append([]providers.TechnicalSourceContext(nil), candidate.SourceContexts...)
	for index := range candidate.SourceContexts {
		candidate.SourceContexts[index].Source = rules.RedactSecrets(candidate.SourceContexts[index].Source)
	}
	return candidate
}

func reviewTechnicalWithRetry(ctx context.Context, reviewer Reviewer, request providers.TechnicalReviewRequest, candidate providers.TechnicalCandidate, current, total int, options Options) (providers.TechnicalReviewResult, error) {
	var result providers.TechnicalReviewResult
	var cumulative providers.Usage
	var attemptNotes []string
	providerRequests := 0
	err := retryTemporary(ctx, candidate, current, total, options, func() error {
		providerRequests++
		attempt, err := reviewer.ReviewTechnical(ctx, request)
		addUsage(&cumulative, attempt.Usage)
		attemptNotes = appendUnique(attemptNotes, attempt.Notes...)
		result = attempt
		if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && incomplete.Reason == "max_output_tokens" {
			next := request.MaxOutputTokens * 2
			if next <= 0 {
				if attempt.Provider == providers.Ollama {
					next = 2400
				} else {
					next = 8192
				}
			}
			if next > maximumTechnicalOutputTokens {
				next = maximumTechnicalOutputTokens
			}
			if next > request.MaxOutputTokens {
				request.MaxOutputTokens = next
				return &technicalOutputRecoveryError{cause: err}
			}
		}
		if _, invalid := providers.AsStructuredOutputValidationError(err); invalid {
			return &technicalValidationRepairError{cause: err}
		}
		return err
	})
	result.Usage = cumulative
	result.ProviderRequests = providerRequests
	result.Notes = appendUnique(result.Notes, attemptNotes...)
	return result, err
}

func planTechnicalSearchWithRetry(ctx context.Context, planner SearchPlanner, candidate providers.TechnicalCandidate, current, total int, options Options) (providers.TechnicalSearchPlan, providers.Usage, int, error) {
	var plan providers.TechnicalSearchPlan
	var cumulative providers.Usage
	providerRequests := 0
	err := retryTemporary(ctx, candidate, current, total, options, func() error {
		providerRequests++
		attemptPlan, usage, err := planner.PlanTechnicalSearch(ctx, candidate)
		addUsage(&cumulative, usage)
		plan = attemptPlan
		return err
	})
	return plan, cumulative, providerRequests, err
}

func retryTemporary(ctx context.Context, candidate providers.TechnicalCandidate, current, total int, options Options, operation func() error) error {
	totalWait := time.Duration(0)
	for attempt := 1; attempt <= maxTechnicalProviderAttempts; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		delay, retryable := technicalRetryDelay(err, attempt)
		if !retryable {
			return err
		}
		var outputRecovery *technicalOutputRecoveryError
		var validationRepair *technicalValidationRepairError
		if attempt == maxTechnicalProviderAttempts {
			if errors.As(err, &outputRecovery) {
				return fmt.Errorf("provider output remained truncated after %d request attempts: %w", attempt, err)
			}
			if errors.As(err, &validationRepair) {
				return fmt.Errorf("provider structured output remained invalid after %d request attempts: %w", attempt, err)
			}
			return fmt.Errorf("provider remained temporarily unavailable after %d request attempts: %w", attempt, err)
		}
		if errors.As(err, &outputRecovery) {
			if options.OnProgress != nil {
				if progressErr := options.OnProgress(Progress{
					Stage: ProgressStageOutputRecovery, Current: current, Total: total, Candidate: candidate, Attempt: attempt,
				}); progressErr != nil {
					return progressErr
				}
			}
			continue
		}
		if errors.As(err, &validationRepair) {
			if options.OnProgress != nil {
				if progressErr := options.OnProgress(Progress{
					Stage: ProgressStageValidationRepair, Current: current, Total: total, Candidate: candidate, Attempt: attempt,
				}); progressErr != nil {
					return progressErr
				}
			}
			continue
		}
		if totalWait+delay > maxRateLimitTotalWait {
			return fmt.Errorf("provider retry wait exceeded the %s automatic budget after %d request attempt(s): %w", maxRateLimitTotalWait, attempt, err)
		}
		totalWait += delay
		if options.OnProgress != nil {
			if progressErr := options.OnProgress(Progress{
				Stage: ProgressStageRateLimitWait, Current: current, Total: total, Candidate: candidate,
				Attempt: attempt, Wait: delay, OriginalWait: delay,
			}); progressErr != nil {
				return progressErr
			}
		}
		if options.Wait != nil {
			if err := options.Wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if err := waitForRateLimit(ctx, delay, func(remaining time.Duration) error {
			if options.OnProgress == nil {
				return nil
			}
			return options.OnProgress(Progress{
				Stage: ProgressStageRateLimitWait, Current: current, Total: total, Candidate: candidate,
				Attempt: attempt, Wait: remaining, OriginalWait: delay,
			})
		}); err != nil {
			return err
		}
		if options.OnProgress != nil {
			if err := options.OnProgress(Progress{
				Stage: ProgressStageRateLimitResume, Current: current, Total: total, Candidate: candidate,
				Attempt: attempt, OriginalWait: delay,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func technicalRetryDelay(err error, attempt int) (time.Duration, bool) {
	var outputRecovery *technicalOutputRecoveryError
	if errors.As(err, &outputRecovery) {
		return 0, true
	}
	var validationRepair *technicalValidationRepairError
	if errors.As(err, &validationRepair) {
		return 0, true
	}
	if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok {
		if rateLimit.RequestTooLarge || rateLimit.Permanent {
			return 0, false
		}
		delay := rateLimit.RetryAfter
		if delay < minimumRateLimitCooldown {
			delay = minimumRateLimitCooldown
		}
		return delay, true
	}
	if transient, ok := providers.AsRemoteTransientError(err); ok {
		delay := transient.RetryAfter
		if delay <= 0 {
			shift := min(attempt-1, 5)
			delay = minimumTransientCooldown * time.Duration(1<<shift)
		}
		if delay < minimumTransientCooldown {
			delay = minimumTransientCooldown
		}
		return delay, true
	}
	return 0, false
}

func addUsage(target *providers.Usage, value providers.Usage) {
	target.PromptTokens += value.PromptTokens
	target.CompletionTokens += value.CompletionTokens
	target.ReasoningTokens += value.ReasoningTokens
	target.TotalDurationNS += value.TotalDurationNS
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

func limitCandidatesPerObjective(candidates []providers.TechnicalCandidate, maximum int) ([]providers.TechnicalCandidate, int) {
	if maximum <= 0 {
		return candidates, 0
	}
	counts := make(map[string]int)
	selected := make([]providers.TechnicalCandidate, 0, len(candidates))
	omitted := 0
	for _, candidate := range candidates {
		key := candidate.SystemID + "\x00" + candidate.ObjectiveID
		if counts[key] >= maximum {
			omitted++
			continue
		}
		counts[key]++
		selected = append(selected, candidate)
	}
	return selected, omitted
}

func needsModelDirectedFollowUp(candidate providers.TechnicalCandidate) bool {
	if candidate.InvestigationMode == "extended-search" {
		return candidate.SearchCoverage.Excerpts == 0
	}
	if candidate.InvestigationMode == "candidate-validation" {
		return len(candidate.SourceContexts) == 0
	}
	return false
}

func candidateFailureNote(candidate providers.TechnicalCandidate, stage string, err error) string {
	detail := strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " ")
	if len(detail) > 500 {
		detail = detail[:500] + "..."
	}
	location := candidate.Path
	if location == "" {
		location = "extended repository search"
	}
	return fmt.Sprintf("AI investigation incomplete for %s at %s during %s: %s. The target remains unresolved and review continued.", candidate.ObjectiveID, location, stage, detail)
}

func searchQueryLabels(queries []providers.TechnicalSearchQuery) []string {
	labels := make([]string, 0, len(queries))
	for _, query := range queries {
		label := query.Text
		if query.PathHint != "" {
			label += " @ " + query.PathHint
		}
		labels = append(labels, label)
	}
	return labels
}

func withoutNoCandidatesNote(notes []string) []string {
	result := make([]string, 0, len(notes))
	for _, note := range notes {
		if !strings.Contains(note, "No likely technical objectives or deterministic candidates were available") {
			result = append(result, note)
		}
	}
	return result
}

func appendUnique(existing []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, current := range existing {
			if current == value {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, value)
		}
	}
	return existing
}
