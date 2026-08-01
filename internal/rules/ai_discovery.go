package rules

import (
	"context"
	"fmt"

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
	return visitAIUsage(repo, func(match aiMatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return emit(Finding{
			RuleID: "AI-DISC-001", Title: fmt.Sprintf("AI provider or framework detected: %s", match.Name),
			Severity: SeverityInfo, Category: "ai-inventory",
			Message:     fmt.Sprintf("ComplyScan detected a likely reference to %s. Confirm how this component is used and include it in the system inventory.", match.Name),
			Path:        match.Path,
			StartLine:   match.Line,
			EndLine:     match.Line,
			Evidence:    match.Evidence,
			Remediation: "Review the detected component and document its purpose, data flows, model ownership, and operational controls.",
			Confidence:  "high",
		})
	})
}
