// Package providers defines the extension boundary for optional AI-assisted
// review. Provider output is advisory and remains separate from deterministic
// findings and exit-code evaluation.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type Kind string

const (
	None        Kind = "none"
	Ollama      Kind = "ollama"
	OpenAI      Kind = "openai"
	Anthropic   Kind = "anthropic"
	Gemini      Kind = "gemini"
	XAI         Kind = "xai"
	Groq        Kind = "groq"
	Mistral     Kind = "mistral"
	OpenRouter  Kind = "openrouter"
	Compatible  Kind = "openai-compatible"
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
	ReasoningTokens  int   `json:"reasoning_tokens,omitempty"`
	TotalDurationNS  int64 `json:"total_duration_ns,omitempty"`
}

// RateLimitSnapshot is the provider's effective rolling request/token
// allowance as observed on one API response. It is runtime scheduling data,
// not part of the persisted evidence contract. A zero limit means the provider
// did not disclose that dimension.
type RateLimitSnapshot struct {
	RequestsKnown     bool
	LimitRequests     int
	RemainingRequests int
	ResetRequests     time.Duration
	TokensKnown       bool
	LimitTokens       int
	RemainingTokens   int
	ResetTokens       time.Duration
}

func (value RateLimitSnapshot) Available() bool {
	return value.RequestsKnown || value.TokensKnown
}

type ReviewResult struct {
	Provider      Kind          `json:"provider"`
	Model         string        `json:"model"`
	InputFindings int           `json:"input_findings"`
	Reviewed      int           `json:"reviewed"`
	Observations  []Observation `json:"observations"`
	Notes         []string      `json:"notes,omitempty"`
	Usage         Usage         `json:"usage,omitempty"`
	// ProviderRequests is live-run accounting. Reports retain the count in a
	// human-readable note so this runtime field does not change the evidence
	// schema contract.
	ProviderRequests int               `json:"-"`
	RateLimits       RateLimitSnapshot `json:"-"`
}

// Provider reviews deterministic findings in an explicitly enabled layer.
type Provider interface {
	Review(ctx context.Context, request ReviewRequest) (ReviewResult, error)
}

// TechnicalReviewRequest is intentionally separate from deterministic finding
// review. Each candidate remains bound to an existing objective and evidence
// fingerprint.
type TechnicalReviewRequest struct {
	Candidates      []TechnicalCandidate
	MaxOutputTokens int `json:"-"`
}

type TechnicalCandidate struct {
	SystemID            string                   `json:"system_id,omitempty"`
	SystemName          string                   `json:"system_name,omitempty"`
	OwnershipScope      string                   `json:"ownership_scope,omitempty"`
	RepositoryFiles     int                      `json:"repository_files,omitempty"`
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
	AllowedPaths        []string                 `json:"-"`
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
type RepositoryTechnicalVerdict string

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

	RepositoryVerdictImplemented     RepositoryTechnicalVerdict = "implemented-in-reviewed-code"
	RepositoryVerdictPartial         RepositoryTechnicalVerdict = "partially-implemented-in-reviewed-code"
	RepositoryVerdictNotImplemented  RepositoryTechnicalVerdict = "not-implemented-in-reviewed-code"
	RepositoryVerdictCannotDetermine RepositoryTechnicalVerdict = "cannot-determine-from-reviewed-code"
)

type TechnicalEvidenceClaim struct {
	Path    string `json:"path"`
	Line    int    `json:"line,omitempty"`
	Summary string `json:"summary"`
}

type TechnicalObservation struct {
	SystemID                    string                   `json:"system_id,omitempty"`
	SystemName                  string                   `json:"system_name,omitempty"`
	OwnershipScope              string                   `json:"ownership_scope,omitempty"`
	RepositoryFiles             int                      `json:"repository_files,omitempty"`
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
	// ProviderRequests is live-run accounting only. A report note retains the
	// aggregate without changing the persisted technical-evidence schema.
	ProviderRequests int `json:"-"`
}

// RepositoryAnalysisMode records which repository context strategy was
// available to the model. It distinguishes targeted evidence from explicit
// full-repository, hierarchical, and older bounded-candidate review.
type RepositoryAnalysisMode string

const (
	RepositoryAnalysisTargeted    RepositoryAnalysisMode = "targeted-evidence"
	RepositoryAnalysisFull        RepositoryAnalysisMode = "full-repository"
	RepositoryAnalysisSubsystem   RepositoryAnalysisMode = "subsystem"
	RepositoryAnalysisSynthesis   RepositoryAnalysisMode = "hierarchical-synthesis"
	RepositoryAnalysisBoundedOnly RepositoryAnalysisMode = "bounded-fallback"
)

// RepositorySourceFile is a redacted file or excerpt supplied to repository
// analysis. Paths and line counts are retained so citations can be verified
// deterministically after the model responds.
type RepositorySourceFile struct {
	// BlockID is a request-local identity assigned by ComplyScan. Compact source
	// responses cite this identity instead of repeating repository paths, which
	// removes path spelling and cross-field candidate-ID bookkeeping from the
	// model contract. It is never a durable repository or AI-use identity.
	BlockID          string `json:"block_id,omitempty"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	LineCount        int    `json:"line_count"`
	ContentStartLine int    `json:"content_start_line,omitempty"`
	Content          string `json:"content"`
}

type RepositoryFileReference struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	LineCount int    `json:"line_count"`
}

type RepositoryObjective struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	SourceReference string `json:"source_reference"`
	Description     string `json:"description"`
	Verification    string `json:"verification"`
}

type RepositorySystemContext struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Paths          []string `json:"paths,omitempty"`
	DeclaredFacts  []string `json:"declared_facts,omitempty"`
	MissingContext []string `json:"missing_context,omitempty"`
}

// RepositoryConfirmedAIUse is human-owned project context supplied to an
// explicit repository review. Paths describe the durable repository scope;
// SubmittedFiles is the smaller, trusted set actually present in this model
// request. Objectives contain only checks selected for this use and context.
type RepositoryConfirmedAIUse struct {
	ID             string                            `json:"id"`
	Name           string                            `json:"name"`
	Description    string                            `json:"description"`
	Paths          []string                          `json:"paths"`
	SystemIDs      []string                          `json:"system_ids"`
	SubmittedFiles []string                          `json:"submitted_files"`
	Objectives     []RepositoryAIUseObjectiveContext `json:"objectives"`
}

type RepositoryAIUseObjectiveContext struct {
	ObjectiveID string `json:"objective_id"`
	SystemID    string `json:"system_id,omitempty"`
	Requirement string `json:"requirement_status"`
}

type RepositoryGraphContext struct {
	Languages              []string                      `json:"languages,omitempty"`
	IndexedSourceFiles     int                           `json:"indexed_source_files"`
	UnsupportedSourceFiles []string                      `json:"unsupported_source_files,omitempty"`
	Imports                []RepositoryGraphImport       `json:"imports,omitempty"`
	Symbols                []RepositoryGraphSymbol       `json:"symbols,omitempty"`
	Relationships          []RepositoryGraphRelationship `json:"relationships,omitempty"`
}

type RepositoryGraphImport struct {
	Path         string `json:"path"`
	ImportedPath string `json:"imported_path"`
}

type RepositoryGraphSymbol struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	StartLine    int    `json:"start_line"`
	EndLine      int    `json:"end_line"`
	Reachability string `json:"reachability"`
}

type RepositoryGraphRelationship struct {
	Kind     string `json:"kind"`
	From     string `json:"from"`
	To       string `json:"to"`
	Label    string `json:"label,omitempty"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Resolved bool   `json:"resolved"`
}

// RepositoryAnalysisRequest carries either repository source or trusted,
// structured subsystem summaries. Synthesis requests also include the full
// file index so every returned citation can still be checked against the
// discovered repository.
type RepositoryAnalysisRequest struct {
	Mode               RepositoryAnalysisMode `json:"mode"`
	Scope              string                 `json:"scope"`
	RepositoryFiles    int                    `json:"repository_files"`
	RepositoryBytes    int64                  `json:"repository_bytes"`
	MaxOutputTokens    int                    `json:"-"`
	ValidationFeedback string                 `json:"-"`
	// CompactSynthesis asks the model to decide observation membership only.
	// Validated source facts and citations are reattached locally after the
	// grouping response, so they do not have to be repeated by the model.
	CompactSynthesis bool `json:"-"`
	// CompactSource marks one independently analyzed source batch. These calls
	// extract only decision-relevant evidence atoms; global grouping and the
	// complete evidence assembly happen after every batch validates.
	CompactSource      bool                       `json:"-"`
	AllowFollowUp      bool                       `json:"allow_follow_up,omitempty"`
	OutputRecovery     bool                       `json:"output_recovery,omitempty"`
	Files              []RepositorySourceFile     `json:"files,omitempty"`
	FileIndex          []RepositoryFileReference  `json:"file_index,omitempty"`
	Objectives         []RepositoryObjective      `json:"objectives"`
	Systems            []RepositorySystemContext  `json:"systems,omitempty"`
	ConfirmedAIUses    []RepositoryConfirmedAIUse `json:"confirmed_ai_uses,omitempty"`
	Graph              RepositoryGraphContext     `json:"repository_graph,omitempty"`
	SubsystemSummaries []RepositorySectionResult  `json:"subsystem_summaries,omitempty"`
}

// RepositoryValidationError identifies a metered provider response that was
// successfully received but rejected by ComplyScan's local structured-output
// and evidence-boundary checks. Diagnostic is safe to send back as untrusted
// corrective feedback; the rejected raw provider response is never retained.
type RepositoryValidationError struct {
	Diagnostic string
	cause      error
}

func (value *RepositoryValidationError) Error() string {
	return value.Diagnostic
}

func (value *RepositoryValidationError) Unwrap() error {
	return value.cause
}

// AsRepositoryValidationError lets orchestration retry a rejected response
// without parsing user-facing error text or weakening validation.
func AsRepositoryValidationError(err error) (*RepositoryValidationError, bool) {
	var value *RepositoryValidationError
	if !errors.As(err, &value) {
		return nil, false
	}
	return value, true
}

// RepositoryRepresentationError identifies already validated synthesis input
// that cannot fit into one locally enforceable structured result without
// losing trusted identity or evidence semantics. It occurs before a provider
// transfer, so orchestration can split the synthesis group or retain the
// validated child results without treating it as a malformed model response.
type RepositoryRepresentationError struct {
	Diagnostic string
	cause      error
}

func (value *RepositoryRepresentationError) Error() string {
	return value.Diagnostic
}

func (value *RepositoryRepresentationError) Unwrap() error {
	return value.cause
}

// AsRepositoryRepresentationError lets orchestration select an
// identity-preserving split or fallback without parsing diagnostic text.
func AsRepositoryRepresentationError(err error) (*RepositoryRepresentationError, bool) {
	var value *RepositoryRepresentationError
	if !errors.As(err, &value) {
		return nil, false
	}
	return value, true
}

// StructuredOutputValidationError marks a metered model response that could
// not be decoded or bound to the trusted finding/technical input. Callers may
// request a bounded full replacement; they must never retain partial output.
type StructuredOutputValidationError struct {
	Diagnostic string
	cause      error
}

func (value *StructuredOutputValidationError) Error() string { return value.Diagnostic }
func (value *StructuredOutputValidationError) Unwrap() error { return value.cause }

func AsStructuredOutputValidationError(err error) (*StructuredOutputValidationError, bool) {
	var value *StructuredOutputValidationError
	if !errors.As(err, &value) {
		return nil, false
	}
	return value, true
}

type RepositoryCitation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Summary string `json:"summary"`
}

// RepositoryEvidenceReference is the compact source-stage citation contract.
// The provider returns only a trusted request-local block identity and an
// original source line. ComplyScan resolves it to RepositoryCitation locally
// before the existing evidence and report validators run.
type RepositoryEvidenceReference struct {
	BlockID string `json:"block_id"`
	Line    int    `json:"line"`
	Summary string `json:"summary"`
}

// RepositoryEvidenceFact is a positive fact nested directly under the source
// observation that supports it. It deliberately has no model-authored AI-use
// ID; local orchestration assigns observation identity after validation.
type RepositoryEvidenceFact struct {
	Field      profile.CodeFactField         `json:"field"`
	Values     []string                      `json:"values"`
	Confidence string                        `json:"confidence"`
	Rationale  string                        `json:"rationale"`
	Evidence   []RepositoryEvidenceReference `json:"evidence"`
}

// RepositoryEvidenceObjective is one code-level objective judgment nested
// under a source observation. AIUseID is absent by design for model-discovered
// observations; confirmed operator-owned uses have their own result records.
type RepositoryEvidenceObjective struct {
	ObjectiveID           string                        `json:"objective_id"`
	SystemID              string                        `json:"system_id,omitempty"`
	Strength              EvidenceStrength              `json:"strength"`
	Confidence            string                        `json:"confidence"`
	Rationale             string                        `json:"rationale"`
	SupportingEvidence    []RepositoryEvidenceReference `json:"supporting_evidence"`
	ContradictoryEvidence []RepositoryEvidenceReference `json:"contradictory_evidence"`
	MissingEvidence       []string                      `json:"missing_evidence,omitempty"`
	UnresolvedQuestions   []string                      `json:"unresolved_questions,omitempty"`
}

// RepositoryEvidenceObservation is the atomic compact-source result. The
// model describes one implementation observation and its evidence; it does not
// invent a join key or coordinate a separate fact-set record.
type RepositoryEvidenceObservation struct {
	Name                string                        `json:"name"`
	Purpose             string                        `json:"purpose"`
	Lifecycle           string                        `json:"lifecycle"`
	Confidence          string                        `json:"confidence"`
	Evidence            []RepositoryEvidenceReference `json:"evidence"`
	Facts               []RepositoryEvidenceFact      `json:"facts"`
	UnresolvedQuestions []string                      `json:"unresolved_questions,omitempty"`
}

// RepositoryConfirmedEvidenceResult carries source-stage records for a
// human-owned stable AI-use ID. Stable confirmed IDs are the only AI-use IDs
// the source model may repeat.
type RepositoryConfirmedEvidenceResult struct {
	AIUseID               string                        `json:"ai_use_id"`
	Facts                 []RepositoryEvidenceFact      `json:"facts"`
	ObjectiveObservations []RepositoryEvidenceObjective `json:"objective_observations"`
	UnresolvedQuestions   []string                      `json:"unresolved_questions,omitempty"`
}

type RepositoryEvidenceUnmappedObservation struct {
	Summary         string                        `json:"summary"`
	Reason          string                        `json:"reason"`
	Confidence      string                        `json:"confidence"`
	Evidence        []RepositoryEvidenceReference `json:"evidence"`
	SuggestedReview string                        `json:"suggested_review,omitempty"`
}

// RepositorySourceObservationResult is returned only by compact source-stage
// requests. It is converted locally into RepositorySectionResult before any
// orchestration, synthesis, cache, report, or dashboard consumer sees it.
type RepositorySourceObservationResult struct {
	Scope                 string                                  `json:"scope"`
	Observations          []RepositoryEvidenceObservation         `json:"observations"`
	ConfirmedAIUses       []RepositoryConfirmedEvidenceResult     `json:"confirmed_ai_uses"`
	ObjectiveObservations []RepositoryEvidenceObjective           `json:"objective_observations"`
	UnmappedObservations  []RepositoryEvidenceUnmappedObservation `json:"unmapped_observations"`
	UnresolvedQuestions   []string                                `json:"unresolved_questions"`
}

// RepositoryAIUseFactSet binds positive, repository-evident profile facts to
// one exact model-discovered candidate ID or operator-confirmed AI-use ID.
// These facts remain advisory drafts and never establish legal applicability,
// organisation role, geographic operation, or actual production use.
type RepositoryAIUseFactSet struct {
	AIUseID             string                `json:"ai_use_id"`
	Facts               []RepositoryAIUseFact `json:"facts"`
	UnresolvedQuestions []string              `json:"unresolved_questions,omitempty"`
}

// RepositoryAIUseFact is one bounded profile field with one or more directly
// supported values. Every fact must retain checked repository citations.
type RepositoryAIUseFact struct {
	Field      profile.CodeFactField `json:"field"`
	Values     []string              `json:"values"`
	Confidence string                `json:"confidence"`
	Rationale  string                `json:"rationale"`
	Evidence   []RepositoryCitation  `json:"evidence"`
}

// RepositoryAIUse is a model-discovered implementation or integration. It is
// an advisory inventory candidate, not a legal classification.
type RepositoryAIUse struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Purpose    string               `json:"purpose"`
	Lifecycle  string               `json:"lifecycle"`
	Confidence string               `json:"confidence"`
	Evidence   []RepositoryCitation `json:"evidence"`
	// MemberObservationIDs contains trusted, scan-local evidence-observation
	// identities after synthesis. The model proposes membership, but local
	// orchestration replaces its temporary group ID with an ID derived from
	// this exact membership. Confirmed human-owned uses continue to use their
	// separately supplied stable IDs.
	MemberObservationIDs []string `json:"member_observation_ids,omitempty"`
	UnresolvedQuestions  []string `json:"unresolved_questions,omitempty"`
}

type RepositoryObjectiveObservation struct {
	ObjectiveID           string                     `json:"objective_id"`
	AIUseID               string                     `json:"ai_use_id,omitempty"`
	SystemID              string                     `json:"system_id,omitempty"`
	Strength              EvidenceStrength           `json:"strength"`
	Confidence            string                     `json:"confidence"`
	TechnicalVerdict      RepositoryTechnicalVerdict `json:"technical_verdict,omitempty"`
	Rationale             string                     `json:"rationale"`
	SupportingEvidence    []RepositoryCitation       `json:"supporting_evidence"`
	ContradictoryEvidence []RepositoryCitation       `json:"contradictory_evidence"`
	MissingEvidence       []string                   `json:"missing_evidence,omitempty"`
	UnresolvedQuestions   []string                   `json:"unresolved_questions,omitempty"`
}

// DerivedTechnicalVerdict converts a validated model observation into a
// bounded code-level decision. It does not decide legal applicability,
// deployment state, or operational effectiveness.
func (value RepositoryObjectiveObservation) DerivedTechnicalVerdict() RepositoryTechnicalVerdict {
	if value.Confidence == "low" {
		return RepositoryVerdictCannotDetermine
	}
	switch value.Strength {
	case StrengthStrong:
		if len(value.SupportingEvidence) == 0 {
			return RepositoryVerdictCannotDetermine
		}
		if value.Confidence == "high" && len(value.ContradictoryEvidence) == 0 && len(value.MissingEvidence) == 0 {
			return RepositoryVerdictImplemented
		}
		return RepositoryVerdictPartial
	case StrengthPartial:
		if len(value.SupportingEvidence) > 0 {
			return RepositoryVerdictPartial
		}
		return RepositoryVerdictCannotDetermine
	case StrengthWeak, StrengthNotSupported:
		return RepositoryVerdictNotImplemented
	default:
		return RepositoryVerdictCannotDetermine
	}
}

type RepositoryUnmappedObservation struct {
	Summary         string               `json:"summary"`
	Reason          string               `json:"reason"`
	Confidence      string               `json:"confidence"`
	Evidence        []RepositoryCitation `json:"evidence"`
	SuggestedReview string               `json:"suggested_review,omitempty"`
}

// RepositoryEvidenceGap is a scan-local question or missing-evidence claim
// emitted by one independently reviewed source batch. Compact synthesis may
// resolve it only by binding the claim to checked evidence from another
// observation in the same model-proposed technical use. It is orchestration
// input and is not retained in the completed report.
type RepositoryEvidenceGap struct {
	ID                   string   `json:"id"`
	Kind                 string   `json:"kind"`
	Text                 string   `json:"text"`
	OriginObservationIDs []string `json:"origin_observation_ids"`
}

// RepositoryResolvedEvidenceGap records a global synthesis decision that a
// batch-local gap was answered by another validated source observation. The
// model decides the semantic relationship; local validation checks the claim,
// observation membership, and every cited source location before this audit
// record can enter the report JSON.
type RepositoryResolvedEvidenceGap struct {
	GapID                   string               `json:"gap_id"`
	Kind                    string               `json:"kind,omitempty"`
	OriginalText            string               `json:"original_text,omitempty"`
	ResolvingObservationIDs []string             `json:"resolving_observation_ids"`
	Evidence                []RepositoryCitation `json:"evidence"`
	Reason                  string               `json:"reason"`
}

// RepositorySectionResult is returned for a complete repository, one
// subsystem, or the synthesis of multiple subsystems.
type RepositorySectionResult struct {
	Scope                 string                           `json:"scope"`
	AIUses                []RepositoryAIUse                `json:"ai_uses"`
	AIUseFacts            []RepositoryAIUseFactSet         `json:"ai_use_facts"`
	ObjectiveObservations []RepositoryObjectiveObservation `json:"objective_observations"`
	UnmappedObservations  []RepositoryUnmappedObservation  `json:"unmapped_observations"`
	UnresolvedQuestions   []string                         `json:"unresolved_questions"`
	EvidenceGaps          []RepositoryEvidenceGap          `json:"evidence_gaps,omitempty"`
	ResolvedEvidenceGaps  []RepositoryResolvedEvidenceGap  `json:"resolved_evidence_gaps,omitempty"`
}

type RepositoryCoverage struct {
	Mode            RepositoryAnalysisMode   `json:"mode"`
	GroupingStatus  RepositoryGroupingStatus `json:"grouping_status,omitempty"`
	ReviewScope     RepositoryReviewScope    `json:"review_scope,omitempty"`
	RepositoryFiles int                      `json:"repository_files"`
	RepositoryBytes int64                    `json:"repository_bytes"`
	ScopeFiles      int                      `json:"scope_files,omitempty"`
	ScopeBytes      int64                    `json:"scope_bytes,omitempty"`
	ChangedFiles    int                      `json:"changed_files,omitempty"`
	ConnectedFiles  int                      `json:"connected_files,omitempty"`
	FilesSubmitted  int                      `json:"files_submitted"`
	// BytesSubmitted is source-content bytes only. It deliberately excludes
	// paths, JSON escaping, graph metadata, prompts, schemas, and synthesis.
	BytesSubmitted int64 `json:"bytes_submitted"`
	// ProviderRequests counts locally initiated provider call attempts,
	// including retries and calls that fail before a response is received.
	ProviderRequests int `json:"provider_requests,omitempty"`
	Subsystems       int `json:"subsystems,omitempty"`
	// SourceBatchesStarted counts current logical leaf batches for which at
	// least one provider call was initiated; it is not proof of receipt.
	SourceBatchesStarted   int `json:"source_batches_started,omitempty"`
	SourceBatchesCompleted int `json:"source_batches_completed,omitempty"`
	SourceBatchesTotal     int `json:"source_batches_total,omitempty"`
	CitationsChecked       int `json:"citations_checked"`
}

// RepositoryGroupingStatus separates evidence-review completion from the
// optional global organization of validated observations into inferred uses.
// A grouping failure must not erase source-level technical conclusions.
type RepositoryGroupingStatus string

const (
	RepositoryGroupingNotNeeded  RepositoryGroupingStatus = "not-needed"
	RepositoryGroupingComplete   RepositoryGroupingStatus = "complete"
	RepositoryGroupingIncomplete RepositoryGroupingStatus = "incomplete"
)

// RepositoryRequestDiagnostic records one repository-layer provider attempt.
// It deliberately excludes prompts, source content, file lists, response
// bodies, and request IDs. The report can therefore explain latency and retry
// amplification without retaining another copy of submitted code.
type RepositoryRequestDiagnostic struct {
	Phase       string `json:"phase"`
	Scope       string `json:"scope"`
	Attempt     int    `json:"attempt"`
	DurationNS  int64  `json:"duration_ns"`
	Outcome     string `json:"outcome"`
	RetryReason string `json:"retry_reason,omitempty"`
	InputFiles  int    `json:"input_files,omitempty"`
	InputBytes  int64  `json:"input_bytes,omitempty"`
}

type RepositoryReviewScope string

const (
	RepositoryReviewScopeChanged RepositoryReviewScope = "changed-plus-connected"
)

type RepositoryAnalysisResult struct {
	Provider           Kind                          `json:"provider"`
	Model              string                        `json:"model"`
	CacheHit           bool                          `json:"cache_hit,omitempty"`
	Coverage           RepositoryCoverage            `json:"coverage"`
	Result             RepositorySectionResult       `json:"result"`
	Notes              []string                      `json:"notes,omitempty"`
	Usage              Usage                         `json:"usage,omitempty"`
	RequestDiagnostics []RepositoryRequestDiagnostic `json:"request_diagnostics,omitempty"`
	RateLimits         RateLimitSnapshot             `json:"-"`
	FollowUpRequested  bool                          `json:"follow_up_requested,omitempty"`
	FollowUpQueries    []string                      `json:"follow_up_queries,omitempty"`
	FollowUpExcerpts   int                           `json:"follow_up_excerpts,omitempty"`
	OutputRecoveryUsed bool                          `json:"output_recovery_used,omitempty"`
	FollowUpPlan       TechnicalSearchPlan           `json:"-"`
}
