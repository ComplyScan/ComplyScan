package framework

type Pack struct {
	SchemaVersion int                  `yaml:"schema-version" json:"schema_version"`
	ID            string               `yaml:"id" json:"id"`
	Name          string               `yaml:"name" json:"name"`
	Version       string               `yaml:"version" json:"version"`
	Released      string               `yaml:"released" json:"released"`
	Digest        string               `yaml:"-" json:"digest"`
	Source        Source               `yaml:"source" json:"source"`
	Coverage      Coverage             `yaml:"coverage" json:"coverage"`
	Objectives    []TechnicalObjective `yaml:"objectives" json:"objectives"`
}

type Source struct {
	Title     string `yaml:"title" json:"title"`
	Reference string `yaml:"reference" json:"reference"`
	URL       string `yaml:"url" json:"url"`
	Edition   string `yaml:"edition" json:"edition"`
}

type Coverage struct {
	Framework    string   `yaml:"framework" json:"framework"`
	EvidenceType string   `yaml:"evidence-type" json:"evidence_type"`
	Provisions   []string `yaml:"provisions" json:"provisions"`
	Limitations  []string `yaml:"limitations" json:"limitations"`
}

type TechnicalObjective struct {
	ID                string                 `yaml:"id" json:"id"`
	Title             string                 `yaml:"title" json:"title"`
	SourceReference   string                 `yaml:"source-reference" json:"source_reference"`
	Description       string                 `yaml:"description" json:"description"`
	ApplicabilityNote string                 `yaml:"applicability-note,omitempty" json:"applicability_note,omitempty"`
	Applicability     ObjectiveApplicability `yaml:"applicability" json:"applicability"`
	FileKinds         []string               `yaml:"file-kinds" json:"file_kinds"`
	PathKeywords      []string               `yaml:"path-keywords,omitempty" json:"path_keywords,omitempty"`
	KeywordGroups     [][]string             `yaml:"keyword-groups" json:"keyword_groups"`
	Verification      string                 `yaml:"verification" json:"verification"`
}

type ObjectiveApplicability struct {
	LegalScope          string   `yaml:"legal-scope" json:"legal_scope"`
	ActivitiesAnyOf     []string `yaml:"activities-any-of" json:"activities_any_of"`
	ExternalUseRequired bool     `yaml:"external-use-required" json:"external_use_required"`
}

const (
	ApplicabilityHighRiskSystem         = "high-risk-system"
	ApplicabilityTransparencyObligation = "transparency-obligation"
)
