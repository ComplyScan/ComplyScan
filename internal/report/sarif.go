package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string                    `json:"id"`
	ShortDescription     sarifMessage              `json:"shortDescription"`
	Help                 sarifMessage              `json:"help"`
	DefaultConfiguration sarifDefaultConfiguration `json:"defaultConfiguration"`
}

type sarifDefaultConfiguration struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
	Properties          sarifProperties   `json:"properties"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine,omitempty"`
}

type sarifProperties struct {
	Category    string `json:"category,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	Remediation string `json:"remediation,omitempty"`
	Occurrences int    `json:"occurrences,omitempty"`
}

// WriteSARIF writes a SARIF 2.1.0 log suitable for GitHub code scanning.
func WriteSARIF(writer io.Writer, report Report) error {
	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           report.Tool.Name,
				Version:        report.Tool.Version,
				InformationURI: "https://github.com/1eonardodawinki/ComplyScan",
				Rules:          sarifRules(report.Findings),
			}},
			Results: sarifResults(report.Findings),
		}},
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(log); err != nil {
		return fmt.Errorf("encode SARIF report: %w", err)
	}
	return nil
}

func sarifRules(findings []rules.Finding) []sarifRule {
	byID := make(map[string]rules.Finding)
	for _, finding := range findings {
		if _, exists := byID[finding.RuleID]; !exists {
			byID[finding.RuleID] = finding
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	values := make([]sarifRule, 0, len(ids))
	for _, id := range ids {
		finding := byID[id]
		values = append(values, sarifRule{
			ID:               id,
			ShortDescription: sarifMessage{Text: finding.Title},
			Help:             sarifMessage{Text: finding.Remediation},
			DefaultConfiguration: sarifDefaultConfiguration{
				Level: sarifLevel(finding.Severity),
			},
		})
	}
	return values
}

func sarifResults(findings []rules.Finding) []sarifResult {
	values := make([]sarifResult, 0, len(findings))
	for _, finding := range findings {
		values = append(values, sarifResult{
			RuleID:    finding.RuleID,
			Level:     sarifLevel(finding.Severity),
			Message:   sarifMessage{Text: finding.Title + ": " + finding.Message},
			Locations: sarifLocations(finding),
			PartialFingerprints: map[string]string{
				"complyscanFingerprint/v1": finding.Fingerprint,
			},
			Properties: sarifProperties{
				Category: finding.Category, Confidence: finding.Confidence,
				Remediation: finding.Remediation, Occurrences: finding.Occurrences,
			},
		})
	}
	return values
}

func sarifLocations(finding rules.Finding) []sarifLocation {
	if len(finding.Locations) > 0 {
		locations := make([]sarifLocation, 0, len(finding.Locations))
		for _, location := range finding.Locations {
			locations = append(locations, newSARIFLocation(location.Path, location.StartLine, location.EndLine))
		}
		return locations
	}
	if finding.Path == "" {
		return nil
	}
	return []sarifLocation{newSARIFLocation(finding.Path, finding.StartLine, finding.EndLine)}
}

func newSARIFLocation(path string, startLine, endLine int) sarifLocation {
	location := sarifLocation{PhysicalLocation: sarifPhysicalLocation{
		ArtifactLocation: sarifArtifactLocation{URI: path},
	}}
	if startLine > 0 {
		location.PhysicalLocation.Region = &sarifRegion{StartLine: startLine, EndLine: endLine}
	}
	return location
}

func sarifLevel(severity rules.Severity) string {
	switch severity {
	case rules.SeverityCritical, rules.SeverityHigh:
		return "error"
	case rules.SeverityMedium:
		return "warning"
	default:
		return "note"
	}
}
