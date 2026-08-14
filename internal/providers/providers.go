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
	Mode               RepositoryAnalysisMode    `json:"mode"`
	Scope              string                    `json:"scope"`
	RepositoryFiles    int                       `json:"repository_files"`
	RepositoryBytes    int64                     `json:"repository_bytes"`
	MaxOutputTokens    int                       `json:"-"`
	AllowFollowUp      bool                      `json:"allow_follow_up,omitempty"`
	OutputRecovery     bool                      `json:"output_recovery,omitempty"`
	Files              []RepositorySourceFile    `json:"files,omitempty"`
	FileIndex          []RepositoryFileReference `json:"file_index,omitempty"`
	Objectives         []RepositoryObjective     `json:"objectives"`
	Systems            []RepositorySystemContext `json:"systems,omitempty"`
	Graph              RepositoryGraphContext    `json:"repository_graph,omitempty"`
	SubsystemSummaries []RepositorySectionResult `json:"subsystem_summaries,omitempty"`
}

type RepositoryCitation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Summary string `json:"summary"`
}

// RepositoryAIUse is a model-discovered implementation or integration. It is
// an advisory inventory candidate, not a legal classification.
type RepositoryAIUse struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	Purpose             string               `json:"purpose"`
	Lifecycle           string               `json:"lifecycle"`
	Confidence          string               `json:"confidence"`
	Evidence            []RepositoryCitation `json:"evidence"`
	UnresolvedQuestions []string             `json:"unresolved_questions,omitempty"`
}

type RepositoryObjectiveObservation struct {
	ObjectiveID           string               `json:"objective_id"`
	SystemID              string               `json:"system_id,omitempty"`
	Strength              EvidenceStrength     `json:"strength"`
	Confidence            string               `json:"confidence"`
	Rationale             string               `json:"rationale"`
	SupportingEvidence    []RepositoryCitation `json:"supporting_evidence"`
	ContradictoryEvidence []RepositoryCitation `json:"contradictory_evidence"`
	MissingEvidence       []string             `json:"missing_evidence,omitempty"`
	UnresolvedQuestions   []string             `json:"unresolved_questions,omitempty"`
}

type RepositoryUnmappedObservation struct {
	Summary         string               `json:"summary"`
	Reason          string               `json:"reason"`
	Confidence      string               `json:"confidence"`
	Evidence        []RepositoryCitation `json:"evidence"`
	SuggestedReview string               `json:"suggested_review,omitempty"`
}

// RepositorySectionResult is returned for a complete repository, one
// subsystem, or the synthesis of multiple subsystems.
type RepositorySectionResult struct {
	Scope                 string                           `json:"scope"`
	AIUses                []RepositoryAIUse                `json:"ai_uses"`
	ObjectiveObservations []RepositoryObjectiveObservation `json:"objective_observations"`
	UnmappedObservations  []RepositoryUnmappedObservation  `json:"unmapped_observations"`
	UnresolvedQuestions   []string                         `json:"unresolved_questions"`
}

type RepositoryCoverage struct {
	Mode             RepositoryAnalysisMode `json:"mode"`
	RepositoryFiles  int                    `json:"repository_files"`
	RepositoryBytes  int64                  `json:"repository_bytes"`
	FilesSubmitted   int                    `json:"files_submitted"`
	BytesSubmitted   int64                  `json:"bytes_submitted"`
	Subsystems       int                    `json:"subsystems,omitempty"`
	CitationsChecked int                    `json:"citations_checked"`
}

type RepositoryAnalysisResult struct {
	Provider           Kind                    `json:"provider"`
	Model              string                  `json:"model"`
	Coverage           RepositoryCoverage      `json:"coverage"`
	Result             RepositorySectionResult `json:"result"`
	Notes              []string                `json:"notes,omitempty"`
	Usage              Usage                   `json:"usage,omitempty"`
	FollowUpRequested  bool                    `json:"follow_up_requested,omitempty"`
	FollowUpQueries    []string                `json:"follow_up_queries,omitempty"`
	FollowUpExcerpts   int                     `json:"follow_up_excerpts,omitempty"`
	OutputRecoveryUsed bool                    `json:"output_recovery_used,omitempty"`
	FollowUpPlan       TechnicalSearchPlan     `json:"-"`
}
