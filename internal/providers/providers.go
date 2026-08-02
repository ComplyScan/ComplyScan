// Package providers defines the extension boundary for optional AI-assisted
// review. Provider output is advisory and remains separate from deterministic
// findings and exit-code evaluation.
package providers

import (
	"context"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
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

type Verdict string

const (
	VerdictConfirmed    Verdict = "confirmed"
	VerdictUncertain    Verdict = "uncertain"
	VerdictNotSupported Verdict = "not_supported"
)

// Observation is advisory model output attached to an existing deterministic
// finding. It cannot create, suppress, or change that finding.
type Observation struct {
	Fingerprint     string  `json:"fingerprint"`
	RuleID          string  `json:"rule_id"`
	Verdict         Verdict `json:"verdict"`
	Confidence      string  `json:"confidence"`
	Rationale       string  `json:"rationale"`
	SuggestedAction string  `json:"suggested_action,omitempty"`
}

type Usage struct {
	PromptTokens     int   `json:"prompt_tokens,omitempty"`
	CompletionTokens int   `json:"completion_tokens,omitempty"`
	TotalDurationNS  int64 `json:"total_duration_ns,omitempty"`
}

type ReviewResult struct {
	Provider      Kind          `json:"provider"`
	Model         string        `json:"model"`
	InputFindings int           `json:"input_findings"`
	Reviewed      int           `json:"reviewed"`
	Observations  []Observation `json:"observations"`
	Notes         []string      `json:"notes,omitempty"`
	Usage         Usage         `json:"usage,omitempty"`
}

// Provider reviews deterministic findings in an explicitly enabled layer.
type Provider interface {
	Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}
