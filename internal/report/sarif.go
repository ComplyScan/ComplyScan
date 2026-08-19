package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
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
	Category           string       `json:"category,omitempty"`
	Confidence         string       `json:"confidence,omitempty"`
	Remediation        string       `json:"remediation,omitempty"`
	Occurrences        int          `json:"occurrences,omitempty"`
	Review             *sarifReview `json:"ollamaReview,omitempty"`
	ActionID           string       `json:"actionId,omitempty"`
	ActionStatus       string       `json:"actionStatus,omitempty"`
	Frameworks         []string     `json:"frameworks,omitempty"`
	AcceptanceCriteria []string     `json:"acceptanceCriteria,omitempty"`
}

type sarifReview struct {
	Provider        providers.Kind    `json:"provider"`
	Model           string            `json:"model"`
	Verdict         providers.Verdict `json:"verdict"`
	Confidence      string            `json:"confidence"`
	Rationale       string            `json:"rationale"`
	SuggestedAction string            `json:"suggestedAction,omitempty"`
}

// WriteSARIF writes a SARIF 2.1.0 log suitable for GitHub code scanning.
func WriteSARIF(writer io.Writer, report Report) error {
	report = EnsureDeveloperActions(report)
	results, err := sarifResults(report.Findings, report.Review)
	if err != nil {
		return err
	}
	actionResults := sarifActionResults(report.DeveloperActions)
	results = append(results, actionResults...)
	rules := sarifRules(report.Findings)
	if len(actionResults) > 0 {
		rules = append(rules, sarifRule{
			ID: "COMPLYSCAN-ACTION", ShortDescription: sarifMessage{Text: "Evidence-backed developer compliance action"},
			Help:                 sarifMessage{Text: "Inspect the stable action ID in the ComplyScan evidence bundle, implement the recommended change, and verify its acceptance criteria."},
			DefaultConfiguration: sarifDefaultConfiguration{Level: "warning"},
		})
	}
	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           report.Tool.Name,
				Version:        report.Tool.Version,
				InformationURI: "https://github.com/ComplyScan/ComplyScan",
				Rules:          rules,
			}},
			Results: results,
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

func sarifActionResults(actions []DeveloperAction) []sarifResult {
	values := make([]sarifResult, 0, len(actions))
	for _, action := range actions {
		if action.Status != DeveloperActionNew && action.Status != DeveloperActionReopened {
			continue
		}
		if action.Category == "deterministic-finding" || action.HumanOnly {
			continue
		}
		locations := make([]sarifLocation, 0, len(action.Evidence))
		for _, evidence := range action.Evidence {
			if evidence.Path == "" {
				continue
			}
			locations = append(locations, newSARIFLocation(evidence.Path, evidence.StartLine, evidence.EndLine))
		}
		if len(locations) == 0 {
			continue
		}
		frameworks := make([]string, 0, len(action.Frameworks))
		for _, framework := range action.Frameworks {
			label := framework.Name
			if framework.SourceReference != "" {
				label += " — " + framework.SourceReference
			}
			frameworks = append(frameworks, label)
		}
		values = append(values, sarifResult{
			RuleID: "COMPLYSCAN-ACTION", Level: sarifActionLevel(action.Priority),
			Message: sarifMessage{Text: action.Title + ": " + action.RecommendedChange}, Locations: locations,
			PartialFingerprints: map[string]string{"complyscanActionId/v1": action.ID},
			Properties: sarifProperties{
				Category: action.Category, Remediation: action.RecommendedChange, ActionID: action.ID,
				ActionStatus: string(action.Status), Frameworks: frameworks,
				AcceptanceCriteria: append([]string(nil), action.AcceptanceCriteria...),
			},
		})
	}
	return values
}

func sarifActionLevel(priority string) string {
	switch priority {
	case "Critical", "High":
		return "error"
	case "Medium", "Review":
		return "warning"
	default:
		return "note"
	}
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

func sarifResults(findings []rules.Finding, review *providers.ReviewResult) ([]sarifResult, error) {
	reviews := make(map[string]providers.Observation)
	if review != nil {
		for _, observation := range review.Observations {
			reviews[observation.Fingerprint] = observation
		}
	}
	values := make([]sarifResult, 0, len(findings))
	for _, finding := range findings {
		locations := sarifLocations(finding)
		if len(locations) == 0 {
			return nil, fmt.Errorf("encode SARIF report: finding %s has no source location", finding.RuleID)
		}
		properties := sarifProperties{
			Category: finding.Category, Confidence: finding.Confidence,
			Remediation: finding.Remediation, Occurrences: finding.Occurrences,
		}
		if observation, ok := reviews[finding.Fingerprint]; ok && review != nil {
			properties.Review = &sarifReview{
				Provider: review.Provider, Model: review.Model, Verdict: observation.Verdict,
				Confidence: observation.Confidence, Rationale: observation.Rationale,
				SuggestedAction: observation.SuggestedAction,
			}
		}
		values = append(values, sarifResult{
			RuleID:    finding.RuleID,
			Level:     sarifLevel(finding.Severity),
			Message:   sarifMessage{Text: finding.Title + ": " + finding.Message},
			Locations: locations,
			PartialFingerprints: map[string]string{
				"complyscanFingerprint/v1": finding.Fingerprint,
			},
			Properties: properties,
		})
	}
	return values, nil
}

func sarifLocations(finding rules.Finding) []sarifLocation {
	if len(finding.Locations) > 0 {
		locations := make([]sarifLocation, 0, len(finding.Locations))
		for _, location := range finding.Locations {
			if location.Path == "" {
				continue
			}
			locations = append(locations, newSARIFLocation(location.Path, location.StartLine, location.EndLine))
		}
		if len(locations) > 0 {
			return locations
		}
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
