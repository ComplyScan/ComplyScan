// Package providers defines the extension boundary for optional AI-assisted
// review. Provider output is advisory and remains separate from deterministic
// findings and exit-code evaluation.
package providers

import (
	"context"

	"github.com/ComplyScan/ComplyScan/internal/rules"
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
	EvidenceStatus      string                   `json:"evidence_status,omitempty"`
	InvestigationMode   string                   `json:"investigation_mode,omitempty"`
	RepositoryDigest    string                   `json:"repository_digest,omitempty"`
	EvidenceFingerprint string                   `json:"evidence_fingerprint,omitempty"`
	Path                string                   `json:"path"`
	StartLine           int                      `json:"start_line,omitempty"`
	Anchor              string                   `json:"anchor,omitempty"`
	Reachability        string                   `json:"reachability,omitempty"`
	Imports             []string                 `json:"imports,omitempty"`
	Relationships       []TechnicalRelationship  `json:"relationships,omitempty"`
	UnresolvedQuestions []string                 `json:"unresolved_questions,omitempty"`
	SearchTerms         []string                 `json:"search_terms,omitempty"`
	EligibleFileKinds   []string                 `json:"eligible_file_kinds,omitempty"`
	SearchCoverage      TechnicalSearchCoverage  `json:"search_coverage,omitempty"`
	SourceContexts      []TechnicalSourceContext `json:"source_contexts"`
}

type TechnicalSearchCoverage struct {
	EligibleFiles int `json:"eligible_files"`
	MatchingFiles int `json:"matching_files"`
	Excerpts      int `json:"excerpts"`
}

type TechnicalSearchQuery struct {
	Text     string `json:"text"`
	PathHint string `json:"path_hint,omitempty"`
	Reason   string `json:"reason"`
}

type TechnicalSearchPlan struct {
	Needed  bool                   `json:"needed"`
	Queries []TechnicalSearchQuery `json:"queries"`
	Reason  string                 `json:"reason"`
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
type TechnicalConclusion string
type AssuranceLevel string

const (
	StrengthStrong       EvidenceStrength = "strong"
	StrengthPartial      EvidenceStrength = "partial"
	StrengthWeak         EvidenceStrength = "weak"
	StrengthUncertain    EvidenceStrength = "uncertain"
	StrengthNotSupported EvidenceStrength = "not_supported"

	ConclusionSubstantiated              TechnicalConclusion = "technically-substantiated"
	ConclusionPartial                    TechnicalConclusion = "partially-substantiated"
	ConclusionTestOnly                   TechnicalConclusion = "test-only-evidence"
	ConclusionUnreachable                TechnicalConclusion = "unreachable-evidence"
	ConclusionNotSubstantiated           TechnicalConclusion = "not-substantiated"
	ConclusionNotFoundAfterInvestigation TechnicalConclusion = "not-found-after-investigation"
	ConclusionCannotDetermine            TechnicalConclusion = "cannot-determine"

	AssuranceSignalDetected          AssuranceLevel = "signal-detected"
	AssuranceAISubstantiated         AssuranceLevel = "ai-substantiated"
	AssuranceStructurallyVerified    AssuranceLevel = "structurally-verified"
	AssuranceTestEvidenceObserved    AssuranceLevel = "test-evidence-observed"
	AssuranceInvestigationNoEvidence AssuranceLevel = "investigation-no-evidence"
	AssuranceUnableToDetermine       AssuranceLevel = "unable-to-determine"
)

type TechnicalEvidenceClaim struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Summary string `json:"summary"`
}

type TechnicalObservation struct {
	ObjectiveID                 string                   `json:"objective_id"`
	EvidenceFingerprint         string                   `json:"evidence_fingerprint"`
	EvidenceStatus              string                   `json:"evidence_status,omitempty"`
	InvestigationMode           string                   `json:"investigation_mode,omitempty"`
	Strength                    EvidenceStrength         `json:"strength"`
	ModelStrength               EvidenceStrength         `json:"model_strength,omitempty"`
	Conclusion                  TechnicalConclusion      `json:"conclusion"`
	Assurance                   AssuranceLevel           `json:"assurance_level"`
	Confidence                  string                   `json:"confidence"`
	Rationale                   string                   `json:"rationale"`
	SupportingEvidence          []TechnicalEvidenceClaim `json:"supporting_evidence"`
	ContradictoryEvidence       []TechnicalEvidenceClaim `json:"contradictory_evidence"`
	MissingEvidence             []string                 `json:"missing_evidence,omitempty"`
	FollowUpRequested           bool                     `json:"follow_up_requested"`
	FollowUpQueries             []string                 `json:"follow_up_queries,omitempty"`
	FollowUpExcerpts            int                      `json:"follow_up_excerpts,omitempty"`
	UnresolvedQuestions         []string                 `json:"unresolved_questions,omitempty"`
	SuggestedReview             string                   `json:"suggested_review,omitempty"`
	GuardrailNote               string                   `json:"guardrail_note,omitempty"`
	RuntimeVerificationRequired bool                     `json:"runtime_verification_required"`
	LegalReviewRequired         bool                     `json:"legal_review_required"`
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
