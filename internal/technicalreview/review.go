package technicalreview

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	Current   int
	Total     int
	Candidate providers.TechnicalCandidate
	Cached    bool
}

type Options struct {
	Identity         Identity
	Cache            *Cache
	Refresh          bool
	MaxCandidates    int
	OnProgress       func(Progress) error
	RetrieveFollowUp FollowUpRetriever
}

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
			if err := options.OnProgress(Progress{Current: index + 1, Total: len(selected), Candidate: candidate, Cached: hit}); err != nil {
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
		if planner, ok := reviewer.(SearchPlanner); ok && options.RetrieveFollowUp != nil {
			var plannerUsage providers.Usage
			plan, plannerUsage, err = planner.PlanTechnicalSearch(ctx, baseCandidate)
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
		partial, err := reviewer.ReviewTechnical(ctx, providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{candidate}})
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
