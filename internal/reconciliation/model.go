// Package reconciliation combines declared applicability context with
// independently discovered repository evidence without turning either into a
// legal or compliance conclusion.
package reconciliation

import (
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type RequirementStatus string
type MappingStatus string
type UnmappedEvidenceKind string

const (
	RequirementLikelyRequired        RequirementStatus = "likely-required"
	RequirementContextDependent      RequirementStatus = "context-dependent"
	RequirementNotCurrentlyIndicated RequirementStatus = "not-currently-indicated"
	RequirementUnresolved            RequirementStatus = "unresolved"

	MappingRequirementWithEvidence    MappingStatus = "requirement-with-candidate-evidence"
	MappingRequirementWithoutEvidence MappingStatus = "requirement-without-detected-evidence"
	MappingEvidenceUnclear            MappingStatus = "evidence-with-unclear-applicability"
	MappingEvidenceMismatch           MappingStatus = "evidence-configuration-mismatch"
	MappingApplicabilityUnresolved    MappingStatus = "applicability-unresolved"
	MappingNotCurrentlyIndicated      MappingStatus = "not-currently-indicated"
	MappingUnableToEvaluate           MappingStatus = "unable-to-evaluate"
	MappingAssociated                 MappingStatus = "associated-by-single-system-inference"
	MappingUnassigned                 MappingStatus = "unassigned"

	UnmappedTechnicalObjective UnmappedEvidenceKind = "technical-objective"
	UnmappedAIComponent        UnmappedEvidenceKind = "ai-component"
)

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type EvidenceReference struct {
	Fingerprint string `json:"fingerprint,omitempty"`
	Path        string `json:"path"`
	Line        int    `json:"line,omitempty"`
	Kind        string `json:"kind,omitempty"`
}

type ObjectiveResult struct {
	ObjectiveID        string                    `json:"objective_id"`
	Title              string                    `json:"title"`
	SourceReference    string                    `json:"source_reference"`
	Requirement        RequirementStatus         `json:"requirement_status"`
	Evidence           framework.ObjectiveStatus `json:"evidence_status"`
	Mapping            MappingStatus             `json:"mapping_status"`
	Reasons            []Reason                  `json:"reasons"`
	EvidenceReferences []EvidenceReference       `json:"evidence_references"`
	Investigation      *ObjectiveInvestigation   `json:"evidence_investigation,omitempty"`
	Verification       *ObjectiveVerification    `json:"execution_verification,omitempty"`
}

type ObjectiveVerification struct {
	Assurance providers.AssuranceLevel `json:"assurance_level"`
	Runs      int                      `json:"runs"`
	Passed    int                      `json:"passed"`
	Failed    int                      `json:"failed"`
	Recipes   []string                 `json:"recipes"`
	Boundary  string                   `json:"boundary"`
}

type ObjectiveInvestigation struct {
	Conclusion                  providers.TechnicalConclusion `json:"conclusion"`
	Assurance                   providers.AssuranceLevel      `json:"assurance_level"`
	Confidence                  string                        `json:"confidence"`
	Observations                int                           `json:"observations"`
	SupportingEvidence          int                           `json:"supporting_evidence"`
	ContradictoryEvidence       int                           `json:"contradictory_evidence"`
	RuntimeVerificationRequired bool                          `json:"runtime_verification_required"`
	LegalReviewRequired         bool                          `json:"legal_review_required"`
}

type ComponentResult struct {
	Name       string                  `json:"name"`
	Kind       inventory.ComponentKind `json:"kind"`
	Confidence string                  `json:"confidence"`
	Mapping    MappingStatus           `json:"mapping_status"`
	Reasons    []Reason                `json:"reasons"`
	Locations  []EvidenceReference     `json:"locations"`
}

type SystemResult struct {
	SystemID           string            `json:"system_id"`
	SystemName         string            `json:"system_name"`
	Objectives         []ObjectiveResult `json:"objectives"`
	ObservedComponents []ComponentResult `json:"observed_components"`
}

type UnmappedEvidence struct {
	Kind       UnmappedEvidenceKind `json:"kind"`
	ID         string               `json:"id"`
	Title      string               `json:"title"`
	Reason     Reason               `json:"reason"`
	References []EvidenceReference  `json:"references"`
}

type Summary struct {
	Systems                    int `json:"systems"`
	LikelyRequired             int `json:"likely_required"`
	RequirementWithEvidence    int `json:"requirement_with_candidate_evidence"`
	RequirementWithoutEvidence int `json:"requirement_without_detected_evidence"`
	EvidenceMismatches         int `json:"evidence_configuration_mismatches"`
	Unresolved                 int `json:"unresolved"`
	UnmappedEvidence           int `json:"unmapped_evidence"`
	AISubstantiated            int `json:"ai_substantiated"`
	StructurallyVerified       int `json:"structurally_verified"`
	InvestigationNoEvidence    int `json:"investigation_no_evidence"`
	InvestigationUnresolved    int `json:"investigation_unresolved"`
	TestEvidenceObserved       int `json:"test_evidence_observed"`
}

type Report struct {
	SchemaVersion  int                `json:"schema_version"`
	MappingVersion string             `json:"mapping_version"`
	Systems        []SystemResult     `json:"systems"`
	Unmapped       []UnmappedEvidence `json:"unmapped_evidence"`
	Summary        Summary            `json:"summary"`
	Notes          []string           `json:"notes"`
}
