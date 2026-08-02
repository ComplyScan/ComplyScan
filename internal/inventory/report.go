package inventory

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type PackageRef struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Location struct {
	Path         string       `json:"path"`
	Line         int          `json:"line,omitempty"`
	EvidenceType EvidenceType `json:"evidence_type"`
	Scope        Scope        `json:"scope"`
	Evidence     string       `json:"evidence"`
}

type Component struct {
	Name          string         `json:"name"`
	Kind          ComponentKind  `json:"kind"`
	Confidence    string         `json:"confidence"`
	Scopes        []Scope        `json:"scopes"`
	EvidenceTypes []EvidenceType `json:"evidence_types"`
	Packages      []PackageRef   `json:"packages"`
	Occurrences   int            `json:"occurrences"`
	Locations     []Location     `json:"locations"`
}

type Summary struct {
	Components           int `json:"components"`
	Providers            int `json:"providers"`
	Frameworks           int `json:"frameworks"`
	Signals              int `json:"signals"`
	RuntimeSignals       int `json:"runtime_signals"`
	TestSignals          int `json:"test_signals"`
	ConfigurationSignals int `json:"configuration_signals"`
}

type Report struct {
	SchemaVersion int         `json:"schema_version"`
	Tool          Tool        `json:"tool"`
	Target        string      `json:"target"`
	Summary       Summary     `json:"summary"`
	Components    []Component `json:"components"`
	Warnings      []string    `json:"warnings,omitempty"`
}

// NewReport aggregates low-level signals into a stable component inventory.
func NewReport(target, version string, signals []Signal, warnings []string) Report {
	groups := make(map[string][]Signal)
	for _, signal := range signals {
		groups[signal.Name] = append(groups[signal.Name], signal)
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)

	report := Report{
		SchemaVersion: 1,
		Tool:          Tool{Name: "ComplyScan", Version: version},
		Target:        target,
		Components:    make([]Component, 0, len(names)),
		Warnings:      warnings,
	}
	for _, name := range names {
		component := aggregateComponent(groups[name])
		report.Components = append(report.Components, component)
		report.Summary.Signals += component.Occurrences
		switch component.Kind {
		case KindProvider:
			report.Summary.Providers++
		case KindFramework:
			report.Summary.Frameworks++
		}
	}
	report.Summary.Components = len(report.Components)
	for _, signal := range signals {
		switch signal.Scope {
		case ScopeRuntime:
			report.Summary.RuntimeSignals++
		case ScopeTest:
			report.Summary.TestSignals++
		case ScopeConfig:
			report.Summary.ConfigurationSignals++
		}
	}
	return report
}

func aggregateComponent(signals []Signal) Component {
	component := Component{
		Name: signals[0].Name, Kind: signals[0].Kind, Confidence: "medium",
		Occurrences: len(signals), Locations: make([]Location, 0, len(signals)),
		Scopes: []Scope{}, EvidenceTypes: []EvidenceType{}, Packages: []PackageRef{},
	}
	scopes := make(map[Scope]struct{})
	evidenceTypes := make(map[EvidenceType]struct{})
	packages := make(map[string]PackageRef)
	for _, signal := range signals {
		if signal.Confidence == "high" {
			component.Confidence = "high"
		}
		scopes[signal.Scope] = struct{}{}
		evidenceTypes[signal.EvidenceType] = struct{}{}
		if signal.Package != "" {
			current, exists := packages[signal.Package]
			if !exists || current.Version == "" && signal.Version != "" {
				packages[signal.Package] = PackageRef{Name: signal.Package, Version: signal.Version}
			}
		}
		component.Locations = append(component.Locations, Location{
			Path: signal.Path, Line: signal.Line, EvidenceType: signal.EvidenceType,
			Scope: signal.Scope, Evidence: signal.Evidence,
		})
	}
	for scope := range scopes {
		component.Scopes = append(component.Scopes, scope)
	}
	sort.Slice(component.Scopes, func(i, j int) bool { return scopeRank(component.Scopes[i]) < scopeRank(component.Scopes[j]) })
	for evidenceType := range evidenceTypes {
		component.EvidenceTypes = append(component.EvidenceTypes, evidenceType)
	}
	sort.Slice(component.EvidenceTypes, func(i, j int) bool { return component.EvidenceTypes[i] < component.EvidenceTypes[j] })
	for _, packageRef := range packages {
		component.Packages = append(component.Packages, packageRef)
	}
	sort.Slice(component.Packages, func(i, j int) bool {
		if component.Packages[i].Name != component.Packages[j].Name {
			return component.Packages[i].Name < component.Packages[j].Name
		}
		return component.Packages[i].Version < component.Packages[j].Version
	})
	return component
}

func WriteJSON(writer io.Writer, report Report) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode inventory JSON: %w", err)
	}
	return nil
}

func WriteTerminal(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintf(writer, "Inventory complete: %d component(s), %d technical signal(s)\n\n", report.Summary.Components, report.Summary.Signals); err != nil {
		return err
	}
	for _, component := range report.Components {
		if _, err := fmt.Fprintf(writer, "%-9s %s (%s confidence)\n", strings.ToUpper(string(component.Kind)), component.Name, component.Confidence); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "          scopes: %s\n", joinScopes(component.Scopes)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "          evidence: %s\n", joinEvidenceTypes(component.EvidenceTypes)); err != nil {
			return err
		}
		if len(component.Packages) > 0 {
			if _, err := fmt.Fprintf(writer, "          packages: %s\n", joinPackages(component.Packages)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, "          locations: %s\n\n", joinLocations(component.Locations)); err != nil {
			return err
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(writer, "Warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func joinScopes(values []Scope) string {
	stringsOut := make([]string, 0, len(values))
	for _, value := range values {
		stringsOut = append(stringsOut, string(value))
	}
	return strings.Join(stringsOut, ", ")
}

func joinEvidenceTypes(values []EvidenceType) string {
	stringsOut := make([]string, 0, len(values))
	for _, value := range values {
		stringsOut = append(stringsOut, string(value))
	}
	return strings.Join(stringsOut, ", ")
}

func joinPackages(values []PackageRef) string {
	stringsOut := make([]string, 0, len(values))
	for _, value := range values {
		packageValue := value.Name
		if value.Version != "" {
			packageValue += "@" + value.Version
		}
		stringsOut = append(stringsOut, packageValue)
	}
	return strings.Join(stringsOut, ", ")
}

func joinLocations(values []Location) string {
	stringsOut := make([]string, 0, len(values))
	for _, value := range values {
		location := value.Path
		if value.Line > 0 {
			location += fmt.Sprintf(":%d", value.Line)
		}
		stringsOut = append(stringsOut, location)
	}
	return strings.Join(stringsOut, ", ")
}
