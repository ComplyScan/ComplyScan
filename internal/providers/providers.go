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

// TechnicalReviewRequest is intentionally separate from deterministic finding
// review. Each candidate remains bound to an existing objective and evidence
// fingerprint.
type TechnicalReviewRequest struct {
	Candidates []TechnicalCandidate
}

type TechnicalCandidate struct {
	ObjectiveID         string                   `json:"objective_id"`
	Title               string                   `json:"title"`
	SourceReference     string                   `json:"source_reference"`
	Description         string                   `json:"description"`
	EvidenceFingerprint string                   `json:"evidence_fingerprint"`
	Path                string                   `json:"path"`
	StartLine           int                      `json:"start_line,omitempty"`
	Anchor              string                   `json:"anchor,omitempty"`
	Reachability        string                   `json:"reachability,omitempty"`
	Imports             []string                 `json:"imports,omitempty"`
	Relationships       []TechnicalRelationship  `json:"relationships,omitempty"`
	UnresolvedQuestions []string                 `json:"unresolved_questions,omitempty"`
	SourceContexts      []TechnicalSourceContext `json:"source_contexts"`
}

type TechnicalRelationship struct {
	Kind     string `json:"kind"`
	From     string `json:"from"`
	To       string `json:"to"`
	Label    string `json:"label,omitempty"`
	Resolved bool   `json:"resolved"`
}

// TechnicalSourceContext is local model input only. Review results never echo
// it into ComplyScan's saved evidence bundle.
type TechnicalSourceContext struct {
	Role         string `json:"role"`
	Symbol       string `json:"symbol"`
	Path         string `json:"path"`
	StartLine    int    `json:"start_line"`
	EndLine      int    `json:"end_line"`
	Reachability string `json:"reachability"`
	Source       string `json:"source"`
}

type EvidenceStrength string

const (
	StrengthStrong       EvidenceStrength = "strong"
	StrengthPartial      EvidenceStrength = "partial"
	StrengthWeak         EvidenceStrength = "weak"
	StrengthUncertain    EvidenceStrength = "uncertain"
	StrengthNotSupported EvidenceStrength = "not_supported"
)

type TechnicalObservation struct {
	ObjectiveID         string           `json:"objective_id"`
	EvidenceFingerprint string           `json:"evidence_fingerprint"`
	Strength            EvidenceStrength `json:"strength"`
	Confidence          string           `json:"confidence"`
	Rationale           string           `json:"rationale"`
	UnresolvedQuestions []string         `json:"unresolved_questions,omitempty"`
	SuggestedReview     string           `json:"suggested_review,omitempty"`
}

type TechnicalReviewResult struct {
	Provider        Kind                   `json:"provider"`
	Model           string                 `json:"model"`
	InputCandidates int                    `json:"input_candidates"`
	Reviewed        int                    `json:"reviewed"`
	Observations    []TechnicalObservation `json:"observations"`
	Notes           []string               `json:"notes,omitempty"`
	Usage           Usage                  `json:"usage,omitempty"`
}
