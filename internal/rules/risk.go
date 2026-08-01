package rules

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type MissingRiskClassificationRule struct{}

func (MissingRiskClassificationRule) ID() string { return "AI-RISK-001" }

func (MissingRiskClassificationRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(detectAIUsage(repo)) == 0 || hasRiskEvidence(repo) {
		return nil, nil
	}
	return []Finding{{
		RuleID: "AI-RISK-001", Title: "AI risk classification not found",
		Severity: SeverityMedium, Category: "risk-classification-evidence",
		Message:     "AI-related technical usage was detected, but no repository-level AI risk-classification evidence was found. This does not establish legal non-compliance.",
		Remediation: "Add a reviewed AI risk assessment describing the system context, intended purpose, affected people, preliminary risk classification, and responsible owner.",
		Confidence:  "medium",
	}}, nil
}

func hasRiskEvidence(repo discovery.Repository) bool {
	accepted := map[string]struct{}{
		"ai-risk.yml": {}, "ai-risk.yaml": {}, "ai-risk.md": {}, "risk-assessment.md": {},
		"docs/ai-risk.md": {}, "docs/risk-assessment.md": {},
	}
	for _, file := range repo.Files {
		path := strings.ToLower(filepath.ToSlash(file.Path))
		if _, ok := accepted[path]; ok || file.Kind == discovery.KindRisk {
			return true
		}
	}
	return false
}
