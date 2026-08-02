// Package inventory extracts structured AI provider and framework signals from
// repositories without making legal or compliance classifications.
package inventory

type ComponentKind string

const (
	KindProvider  ComponentKind = "provider"
	KindFramework ComponentKind = "framework"
)

type EvidenceType string

const (
	EvidenceDependency  EvidenceType = "dependency"
	EvidenceImport      EvidenceType = "import"
	EvidenceEndpoint    EvidenceType = "endpoint"
	EvidenceEnvironment EvidenceType = "environment"
)

type Scope string

const (
	ScopeRuntime Scope = "runtime"
	ScopeTest    Scope = "test"
	ScopeConfig  Scope = "configuration"
)

// Signal is one typed piece of technical evidence for an AI component.
type Signal struct {
	Name         string        `json:"name"`
	Kind         ComponentKind `json:"kind"`
	EvidenceType EvidenceType  `json:"evidence_type"`
	Scope        Scope         `json:"scope"`
	Confidence   string        `json:"confidence"`
	Path         string        `json:"path"`
	Line         int           `json:"line,omitempty"`
	Package      string        `json:"package,omitempty"`
	Version      string        `json:"version,omitempty"`
	Evidence     string        `json:"evidence"`
}
