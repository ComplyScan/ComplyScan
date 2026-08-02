package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type AIUsageRule struct{}

func (AIUsageRule) ID() string { return "AI-DISC-001" }

func (rule AIUsageRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	return collectFindings(func(emit FindingEmitter) error {
		return rule.RunStreaming(ctx, repo, emit)
	})
}

func (AIUsageRule) RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error {
	groups := make(map[string][]aiMatch)
	var order []string
	for _, match := range detectAIUsage(ctx, repo) {
		if _, ok := groups[match.Name]; !ok {
			order = append(order, match.Name)
		}
		groups[match.Name] = append(groups[match.Name], match)
	}

	for _, name := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		matches := groups[name]
		representative := matches[0]
		locations := representativeLocations(matches, 3)
		message := fmt.Sprintf("ComplyScan detected %d technical signal(s) for %s across %d file(s). Confirm how this component is used and include it in the system inventory.", len(matches), name, distinctFileCount(matches))
		if examples := locationSummary(locations); examples != "" {
			message += " Representative locations: " + examples + "."
		}
		if err := emit(Finding{
			RuleID: "AI-DISC-001", Title: fmt.Sprintf("AI provider or framework detected: %s", name),
			Severity: SeverityInfo, Category: "ai-inventory",
			Message:     message,
			Path:        representative.Path,
			StartLine:   representative.Line,
			EndLine:     representative.Line,
			Evidence:    representative.Evidence,
			Remediation: "Review the detected component and document its purpose, data flows, model ownership, and operational controls.",
			Confidence:  representative.Confidence,
			Occurrences: len(matches),
			Locations:   locations,
		}); err != nil {
			return err
		}
	}
	return nil
}

func distinctFileCount(matches []aiMatch) int {
	paths := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		paths[match.Path] = struct{}{}
	}
	return len(paths)
}

func representativeLocations(matches []aiMatch, limit int) []Location {
	if len(matches) < limit {
		limit = len(matches)
	}
	locations := make([]Location, 0, limit)
	for _, match := range matches[:limit] {
		locations = append(locations, Location{
			Path: match.Path, StartLine: match.Line, EndLine: match.Line, Evidence: match.Evidence,
		})
	}
	return locations
}

func locationSummary(locations []Location) string {
	values := make([]string, 0, len(locations))
	for _, location := range locations {
		value := location.Path
		if location.StartLine > 0 {
			value += fmt.Sprintf(":%d", location.StartLine)
		}
		values = append(values, value)
	}
	return strings.Join(values, ", ")
}
