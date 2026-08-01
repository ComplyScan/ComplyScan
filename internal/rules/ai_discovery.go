package rules

import (
	"context"
	"fmt"

	"github.com/complyscan/complyscan/internal/discovery"
)

type AIUsageRule struct{}

func (AIUsageRule) ID() string { return "AI-DISC-001" }

func (AIUsageRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matches := detectAIUsage(repo)
	findings := make([]Finding, 0, len(matches))
	for _, match := range matches {
		findings = append(findings, Finding{
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
	}
	return findings, nil
}
