package technicalreview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type Reviewer interface {
	ReviewTechnical(context.Context, providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error)
}

type SearchPlanner interface {
	PlanTechnicalSearch(context.Context, providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error)
}

type FollowUpRetriever func(providers.TechnicalCandidate, providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int)

type Progress struct {
	Stage     string
	Current   int
	Total     int
	Candidate providers.TechnicalCandidate
	Cached    bool
	Attempt   int
	Wait      time.Duration
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
	ProgressStageCandidate     = "candidate"
	ProgressStageRateLimitWait = "rate-limit-wait"
	minimumRateLimitCooldown   = 60 * time.Second
	maxRateLimitTotalWait      = 10 * time.Minute
)

// Run applies source-context-free cache reuse around one-candidate model requests.
// Cache failures are reported as notes and never prevent the requested review.
func Run(ctx context.Context, reviewer Reviewer, request providers.TechnicalReviewRequest, options Options) (providers.TechnicalReviewResult, error) {
	base, err := reviewer.ReviewTechnical(ctx, providers.TechnicalReviewRequest{})
	if err != nil {
		return providers.TechnicalReviewResult{}, err
	}
	base.InputCandidates = len(request.Candidates)
	if len(request.Candidates) == 0 {
		return base, nil
	}
	base.Observations = []providers.TechnicalObservation{}
	base.Reviewed = 0
	base.Notes = withoutNoCandidatesNote(base.Notes)
	selected := request.Candidates
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
			return providers.TechnicalReviewResult{}, err
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
				return providers.TechnicalReviewResult{}, err
			}
		}
		if hit {
			cacheHits++
			base.Observations = append(base.Observations, observation)
			continue
		}
		baseCandidate := candidate
		plan := providers.TechnicalSearchPlan{Queries: []providers.TechnicalSearchQuery{}}
		followUpExcerpts := 0
		if planner, ok := reviewer.(SearchPlanner); ok && options.RetrieveFollowUp != nil && needsModelDirectedFollowUp(baseCandidate) {
			var plannerUsage providers.Usage
			plan, plannerUsage, err = planTechnicalSearchWithRetry(ctx, planner, baseCandidate, index+1, len(selected), options)
			if err != nil {
				if ctx.Err() != nil {
					return base, ctx.Err()
				}
				base.Notes = append(base.Notes, candidateFailureNote(candidate, "follow-up planning", err))
				continue
			}
			base.Usage.PromptTokens += plannerUsage.PromptTokens
			base.Usage.CompletionTokens += plannerUsage.CompletionTokens
			base.Usage.TotalDurationNS += plannerUsage.TotalDurationNS
			if strings.HasPrefix(plan.Reason, "Follow-up skipped") {
				base.Notes = append(base.Notes, plan.Reason)
			}
			if plan.Needed {
				candidate, followUpExcerpts = options.RetrieveFollowUp(baseCandidate, plan)
			}
		}
		partial, err := reviewTechnicalWithRetry(ctx, reviewer, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}}, candidate, index+1, len(selected), options)
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
		base.Usage.PromptTokens += partial.Usage.PromptTokens
		base.Usage.CompletionTokens += partial.Usage.CompletionTokens
		base.Usage.TotalDurationNS += partial.Usage.TotalDurationNS
		base.Notes = appendUnique(base.Notes, partial.Notes...)
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
	return base, nil
}

func reviewTechnicalWithRetry(ctx context.Context, reviewer Reviewer, request providers.TechnicalReviewRequest, candidate providers.TechnicalCandidate, current, total int, options Options) (providers.TechnicalReviewResult, error) {
	var result providers.TechnicalReviewResult
	err := retryRateLimited(ctx, candidate, current, total, options, func() error {
		var err error
		result, err = reviewer.ReviewTechnical(ctx, request)
		return err
	})
	return result, err
}

func planTechnicalSearchWithRetry(ctx context.Context, planner SearchPlanner, candidate providers.TechnicalCandidate, current, total int, options Options) (providers.TechnicalSearchPlan, providers.Usage, error) {
	var plan providers.TechnicalSearchPlan
	var usage providers.Usage
	err := retryRateLimited(ctx, candidate, current, total, options, func() error {
		var err error
		plan, usage, err = planner.PlanTechnicalSearch(ctx, candidate)
		return err
	})
	return plan, usage, err
}

func retryRateLimited(ctx context.Context, candidate providers.TechnicalCandidate, current, total int, options Options, operation func() error) error {
	totalWait := time.Duration(0)
	for attempt := 0; ; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}
		rateLimit, ok := providers.AsRemoteRateLimitError(err)
		if !ok || rateLimit.RequestTooLarge {
			return err
		}
		delay := rateLimit.RetryAfter
		if delay < minimumRateLimitCooldown {
			delay = minimumRateLimitCooldown
		}
		if totalWait+delay > maxRateLimitTotalWait {
			return fmt.Errorf("provider rate limit exceeded the %s automatic wait budget after %d retry cycle(s): %w", maxRateLimitTotalWait, attempt, err)
		}
		totalWait += delay
		if options.OnProgress != nil {
			if progressErr := options.OnProgress(Progress{
				Stage: ProgressStageRateLimitWait, Current: current, Total: total, Candidate: candidate,
				Attempt: attempt + 1, Wait: delay,
			}); progressErr != nil {
				return progressErr
			}
		}
		wait := options.Wait
		if wait == nil {
			wait = waitForRateLimit
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
}

func waitForRateLimit(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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
