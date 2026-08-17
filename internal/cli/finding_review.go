package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const (
	maximumFindingReviewAttempts  = 4
	initialFindingReviewRetryWait = 500 * time.Millisecond
	maximumFindingReviewRetryWait = 8 * time.Second
	maximumFindingProviderWait    = 10 * time.Minute
)

type findingReviewer interface {
	Review(context.Context, providers.ReviewRequest) (providers.ReviewResult, error)
}

type findingReviewRetryPolicy struct {
	MaximumAttempts int
	InitialWait     time.Duration
	MaximumWait     time.Duration
	Wait            func(context.Context, time.Duration) error
}

func reviewFindingsWithRetry(ctx context.Context, reviewer findingReviewer, request providers.ReviewRequest, policy findingReviewRetryPolicy) (providers.ReviewResult, error) {
	if reviewer == nil {
		return providers.ReviewResult{}, errors.New("finding review requires a reviewer")
	}
	aggregate := providers.ReviewResult{InputFindings: len(request.Findings), Observations: []providers.Observation{}}
	queue := []providers.ReviewRequest{request}
	for len(queue) > 0 {
		batch := queue[0]
		queue = queue[1:]
		result, err := reviewFindingBatchWithRetry(ctx, reviewer, batch, policy)
		mergeFindingReviewAccounting(&aggregate, result)
		aggregate.Notes = appendUniqueFindingNotes(aggregate.Notes, result.Notes...)
		if err != nil {
			if incomplete, ok := providers.AsRemoteIncompleteError(err); ok && incomplete.Reason == "max_output_tokens" && len(batch.Findings) > 1 {
				midpoint := len(batch.Findings) / 2
				left := batch
				left.Findings = append([]rules.Finding(nil), batch.Findings[:midpoint]...)
				right := batch
				right.Findings = append([]rules.Finding(nil), batch.Findings[midpoint:]...)
				queue = append([]providers.ReviewRequest{left, right}, queue...)
				aggregate.Notes = appendUniqueFindingNotes(aggregate.Notes, fmt.Sprintf("Finding output reached its limit for %d record(s); ComplyScan continued with two smaller bound batches.", len(batch.Findings)))
				continue
			}
			aggregate.InputFindings = len(request.Findings)
			aggregate.Reviewed = len(aggregate.Observations)
			appendFindingReviewAccountingNote(&aggregate)
			return aggregate, err
		}
		if aggregate.Provider == providers.None || aggregate.Provider == "" {
			aggregate.Provider = result.Provider
		}
		if aggregate.Model == "" {
			aggregate.Model = result.Model
		}
		aggregate.Observations = append(aggregate.Observations, result.Observations...)
	}
	aggregate.InputFindings = len(request.Findings)
	aggregate.Reviewed = len(aggregate.Observations)
	appendFindingReviewAccountingNote(&aggregate)
	return aggregate, nil
}

func reviewFindingBatchWithRetry(ctx context.Context, reviewer findingReviewer, request providers.ReviewRequest, policy findingReviewRetryPolicy) (providers.ReviewResult, error) {
	if reviewer == nil {
		return providers.ReviewResult{}, errors.New("finding review requires a reviewer")
	}
	if policy.MaximumAttempts <= 0 {
		policy.MaximumAttempts = 1
	}
	if policy.InitialWait < 0 {
		policy.InitialWait = 0
	}
	if policy.MaximumWait < policy.InitialWait {
		policy.MaximumWait = policy.InitialWait
	}
	if policy.Wait == nil {
		policy.Wait = waitForFindingReviewRetry
	}

	aggregate := providers.ReviewResult{InputFindings: len(request.Findings), Observations: []providers.Observation{}}
	for attempt := 1; attempt <= policy.MaximumAttempts; attempt++ {
		result, err := reviewer.Review(ctx, request)
		// Review currently maps one non-empty finding submission to exactly one
		// provider call. Count the locally initiated attempt even when the
		// provider failed before returning response accounting.
		if len(request.Findings) > 0 && result.ProviderRequests == 0 {
			result.ProviderRequests = 1
		}
		mergeFindingReviewAccounting(&aggregate, result)
		if err == nil {
			result.Usage = aggregate.Usage
			result.ProviderRequests = aggregate.ProviderRequests
			result.RateLimits = aggregate.RateLimits
			return result, nil
		}
		if !retryableFindingReviewError(err) || attempt == policy.MaximumAttempts {
			failed := failedFindingReviewResult(aggregate, result)
			return failed, fmt.Errorf("finding review failed after %d provider request(s): %w", aggregate.ProviderRequests, err)
		}
		if contextErr := ctx.Err(); contextErr != nil {
			failed := failedFindingReviewResult(aggregate, result)
			return failed, fmt.Errorf("finding review stopped after %d provider request(s): %w", aggregate.ProviderRequests, contextErr)
		}
		if _, invalid := providers.AsStructuredOutputValidationError(err); invalid {
			continue
		}
		delay := findingReviewRetryDelay(policy, attempt, err)
		if delay > maximumFindingProviderWait {
			failed := failedFindingReviewResult(aggregate, result)
			return failed, fmt.Errorf("finding review provider requested a retry wait beyond the %s automatic per-window budget after %d provider request(s): %w", maximumFindingProviderWait, aggregate.ProviderRequests, err)
		}
		if waitErr := policy.Wait(ctx, delay); waitErr != nil {
			failed := failedFindingReviewResult(aggregate, result)
			return failed, fmt.Errorf("finding review stopped after %d provider request(s) while waiting to retry: %w", aggregate.ProviderRequests, waitErr)
		}
	}
	return aggregate, errors.New("finding review exhausted its retry policy")
}

func appendUniqueFindingNotes(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(values))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func mergeFindingReviewAccounting(total *providers.ReviewResult, value providers.ReviewResult) {
	if value.Provider != providers.None && value.Provider != "" {
		total.Provider = value.Provider
	}
	if value.Model != "" {
		total.Model = value.Model
	}
	if value.InputFindings > 0 || total.InputFindings == 0 {
		total.InputFindings = value.InputFindings
	}
	total.Usage.PromptTokens += value.Usage.PromptTokens
	total.Usage.CompletionTokens += value.Usage.CompletionTokens
	total.Usage.ReasoningTokens += value.Usage.ReasoningTokens
	total.Usage.TotalDurationNS += value.Usage.TotalDurationNS
	total.ProviderRequests += value.ProviderRequests
	mergeLatestFindingRateLimits(&total.RateLimits, value.RateLimits)
}

func mergeLatestFindingRateLimits(total *providers.RateLimitSnapshot, value providers.RateLimitSnapshot) {
	if value.RequestsKnown {
		total.RequestsKnown = true
		total.LimitRequests = value.LimitRequests
		total.RemainingRequests = value.RemainingRequests
		total.ResetRequests = value.ResetRequests
	}
	if value.TokensKnown {
		total.TokensKnown = true
		total.LimitTokens = value.LimitTokens
		total.RemainingTokens = value.RemainingTokens
		total.ResetTokens = value.ResetTokens
	}
}

func failedFindingReviewResult(accounting, latest providers.ReviewResult) providers.ReviewResult {
	latest.Observations = []providers.Observation{}
	latest.Reviewed = 0
	latest.Usage = accounting.Usage
	latest.ProviderRequests = accounting.ProviderRequests
	latest.RateLimits = accounting.RateLimits
	if latest.Provider == providers.None || latest.Provider == "" {
		latest.Provider = accounting.Provider
	}
	if latest.Model == "" {
		latest.Model = accounting.Model
	}
	if latest.InputFindings == 0 {
		latest.InputFindings = accounting.InputFindings
	}
	return latest
}

func retryableFindingReviewError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok {
		return !rateLimit.Permanent && !rateLimit.RequestTooLarge
	}
	if _, invalid := providers.AsStructuredOutputValidationError(err); invalid {
		return true
	}
	_, transient := providers.AsRemoteTransientError(err)
	return transient
}

func findingReviewRetryDelay(policy findingReviewRetryPolicy, completedAttempts int, err error) time.Duration {
	if _, invalid := providers.AsStructuredOutputValidationError(err); invalid {
		return 0
	}
	delay := policy.InitialWait
	for retry := 1; retry < completedAttempts && delay < policy.MaximumWait; retry++ {
		if delay > policy.MaximumWait/2 {
			delay = policy.MaximumWait
			break
		}
		delay *= 2
	}
	if delay > policy.MaximumWait {
		delay = policy.MaximumWait
	}
	providerWait := time.Duration(0)
	if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok {
		providerWait = max(providerWait, rateLimit.RetryAfter)
		providerWait = max(providerWait, exhaustedFindingReviewReset(rateLimit.RateLimits))
	}
	if transient, ok := providers.AsRemoteTransientError(err); ok {
		providerWait = max(providerWait, transient.RetryAfter)
		providerWait = max(providerWait, exhaustedFindingReviewReset(transient.RateLimits))
	}
	return max(delay, providerWait)
}

func exhaustedFindingReviewReset(limits providers.RateLimitSnapshot) time.Duration {
	wait := time.Duration(0)
	if limits.RequestsKnown && limits.RemainingRequests <= 0 {
		wait = max(wait, limits.ResetRequests)
	}
	if limits.TokensKnown && limits.RemainingTokens <= 0 {
		wait = max(wait, limits.ResetTokens)
	}
	return wait
}

func waitForFindingReviewRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func findingReviewComplete(result providers.ReviewResult) bool {
	return result.Reviewed == result.InputFindings
}

func findingReviewRunAccounting(result providers.ReviewResult) string {
	return fmt.Sprintf("%d provider request(s), %d input / %d output / %d reasoning token(s)",
		result.ProviderRequests, result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Usage.ReasoningTokens)
}

func appendFindingReviewAccountingNote(result *providers.ReviewResult) {
	if result.ProviderRequests <= 0 {
		return
	}
	result.Notes = append(result.Notes, fmt.Sprintf(
		"Finding-review usage aggregates %d provider request attempt(s) made during this run.", result.ProviderRequests,
	))
}
