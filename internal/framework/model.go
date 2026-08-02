package framework

type Pack struct {
	SchemaVersion int       `yaml:"schema-version" json:"schema_version"`
	ID            string    `yaml:"id" json:"id"`
	Name          string    `yaml:"name" json:"name"`
	Version       string    `yaml:"version" json:"version"`
	Released      string    `yaml:"released" json:"released"`
	Digest        string    `yaml:"-" json:"digest"`
	Source        Source    `yaml:"source" json:"source"`
	Coverage      Coverage  `yaml:"coverage" json:"coverage"`
	Controls      []Control `yaml:"controls" json:"controls"`
}

type Source struct {
	Title     string `yaml:"title" json:"title"`
	Reference string `yaml:"reference" json:"reference"`
	URL       string `yaml:"url" json:"url"`
	Edition   string `yaml:"edition" json:"edition"`
}

type Coverage struct {
	Framework          string   `yaml:"framework" json:"framework"`
	Roles              []string `yaml:"roles" json:"roles"`
	RiskClassification string   `yaml:"risk-classification" json:"risk_classification"`
	Provisions         []string `yaml:"provisions" json:"provisions"`
	Limitations        []string `yaml:"limitations" json:"limitations"`
}

type Control struct {
	ID                   string                `yaml:"id" json:"id"`
	Title                string                `yaml:"title" json:"title"`
	SourceReference      string                `yaml:"source-reference" json:"source_reference"`
	Objective            string                `yaml:"objective" json:"objective"`
	ApplicabilityNote    string                `yaml:"applicability-note,omitempty" json:"applicability_note,omitempty"`
	EvidenceRequirements []EvidenceRequirement `yaml:"evidence" json:"evidence"`
}

type EvidenceRequirement struct {
	ID            string     `yaml:"id" json:"id"`
	Description   string     `yaml:"description" json:"description"`
	FileKinds     []string   `yaml:"file-kinds" json:"file_kinds"`
	PathKeywords  []string   `yaml:"path-keywords,omitempty" json:"path_keywords,omitempty"`
	KeywordGroups [][]string `yaml:"keyword-groups" json:"keyword_groups"`
	Verification  string     `yaml:"verification" json:"verification"`
}
