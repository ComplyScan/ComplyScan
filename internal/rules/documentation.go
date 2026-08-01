package rules

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

type MissingDocumentationRule struct{}

func (MissingDocumentationRule) ID() string { return "AI-DOC-001" }

func (MissingDocumentationRule) Run(ctx context.Context, repo discovery.Repository) ([]Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(detectAIUsage(repo)) == 0 || hasAIDocumentation(repo) {
		return nil, nil
	}
	return []Finding{{
		RuleID: "AI-DOC-001", Title: "AI-system documentation not found",
		Severity: SeverityMedium, Category: "governance-evidence",
		Message:     "AI-related technical usage was detected, but no model card or AI-system documentation was found in the repository.",
		Remediation: "Add a model card or AI-system document covering purpose, capabilities, limitations, data, ownership, evaluation, and operational controls.",
		Confidence:  "medium",
	}}, nil
}

func (rule MissingDocumentationRule) RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error {
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

func hasAIDocumentation(repo discovery.Repository) bool {
	for _, file := range repo.Files {
		path := strings.ToLower(filepath.ToSlash(file.Path))
		base := strings.ToLower(filepath.Base(path))
		if file.Kind == discovery.KindModelCard || file.Kind == discovery.KindAIGovernance {
			return true
		}
		if base == "model_card.md" || base == "model-card.md" || base == "ai_system.md" || base == "ai-system.md" {
			return true
		}
		if strings.HasPrefix(path, "docs/ai/") {
			return true
		}
	}
	return false
}
