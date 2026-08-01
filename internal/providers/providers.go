// Package providers defines the extension boundary for optional AI-assisted review.
// The v0.1 scanner does not instantiate providers and makes no network requests.
package providers

import (
	"context"

	"github.com/complyscan/complyscan/internal/rules"
)

type Kind string

const (
	None        Kind = "none"
	Ollama      Kind = "ollama"
	OpenAI      Kind = "openai"
	Anthropic   Kind = "anthropic"
	Gemini      Kind = "gemini"
	ComplyCloud Kind = "complyscan-cloud"
)

type ReviewRequest struct {
	RepositoryRoot string
	Findings       []rules.Finding
}

type ReviewResult struct {
	Findings []rules.Finding
	Notes    []string
}

// Provider can enrich deterministic findings in a future, explicitly enabled layer.
type Provider interface {
	Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
