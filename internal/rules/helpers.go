package rules

import (
	"context"
	"strings"
	"sync"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
)

type aiMatch = inventory.Signal

type analysisContextKey struct{}

type repositoryAnalysis struct {
	repo    discovery.Repository
	once    sync.Once
	aiUsage []aiMatch
}

// WithRepositoryAnalysis installs a lazily computed, scan-wide analysis cache.
// Rules outside this package remain compatible with an ordinary context.
func WithRepositoryAnalysis(ctx context.Context, repo discovery.Repository) context.Context {
	return context.WithValue(ctx, analysisContextKey{}, &repositoryAnalysis{repo: repo})
}

func detectAIUsage(ctx context.Context, repo discovery.Repository) []aiMatch {
	if analysis, ok := ctx.Value(analysisContextKey{}).(*repositoryAnalysis); ok {
		analysis.once.Do(func() {
			analysis.aiUsage = collectAIUsage(analysis.repo)
		})
		return analysis.aiUsage
	}
	return collectAIUsage(repo)
}

func collectAIUsage(repo discovery.Repository) []aiMatch {
	return inventory.Analyze(repo)
}

func lines(content []byte) []string {
	return strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
}

func sanitizeEvidence(value string, limit int) string {
	value = strings.TrimSpace(RedactSecrets(value))
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return value
}
