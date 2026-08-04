package rules

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

type MissingRiskClassificationRule struct{}

func (MissingRiskClassificationRule) ID() string { return "AI-RISK-001" }

func (MissingRiskClassificationRule) RepositoryWide() bool { return true }

func (MissingRiskClassificationRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	matches := detectAIUsage(ctx, repo)
	if len(matches) == 0 || hasRiskEvidence(repo) {
		return nil, nil
	}
	representative := matches[0]
	return []Finding{{
		RuleID: "AI-RISK-001", Title: "AI risk classification not found",
		Severity: SeverityMedium, Category: "risk-classification-evidence",
		Message:     "AI-related technical usage was detected, but no repository-level AI risk-classification evidence was found. This does not establish legal non-compliance.",
		Path:        representative.Path,
		StartLine:   representative.Line,
		EndLine:     representative.Line,
		Locations:   representativeLocations(matches, 3),
		Remediation: "Add a reviewed AI risk assessment describing the system context, intended purpose, affected people, preliminary risk classification, and responsible owner.",
		Confidence:  "medium",
	}}, nil
}

func (rule MissingRiskClassificationRule) RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error {
	findings, err := rule.Run(ctx, repo)
	if err != nil {
		return err
	}
	for _, finding := range findings {
		if err := emit(finding); err != nil {
			return err
		}
	}
	return nil
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
