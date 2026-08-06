package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

const (
	smokePositiveObjective = "eu-aia-14-override-intervention"
	smokeNegativeObjective = "eu-aia-9-risk-control-testing"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/validate-ollama-smoke REPORT_JSON")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	var value report.Report
	if err := json.Unmarshal(data, &value); err != nil {
		fmt.Fprintln(os.Stderr, "Error: decode report:", err)
		os.Exit(1)
	}
	lines, err := validateOllamaSmoke(value)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

func validateOllamaSmoke(value report.Report) ([]string, error) {
	if value.TechnicalReview == nil {
		return nil, errors.New("report has no evidence_investigation")
	}
	observations := make(map[string]providers.TechnicalObservation)
	for _, observation := range value.TechnicalReview.Observations {
		observations[observation.ObjectiveID] = observation
	}
	positive, ok := observations[smokePositiveObjective]
	if !ok {
		return nil, errors.New("positive override objective was not investigated")
	}
	if positive.Strength != providers.StrengthPartial && positive.Strength != providers.StrengthStrong {
		return nil, fmt.Errorf("positive override strength is %q, want partial or strong", positive.Strength)
	}
	if positive.Assurance != providers.AssuranceAISubstantiated && positive.Assurance != providers.AssuranceStructurallyVerified {
		return nil, fmt.Errorf("positive override assurance is %q", positive.Assurance)
	}
	if len(positive.SupportingEvidence) == 0 {
		return nil, errors.New("positive override returned no grounded supporting evidence")
	}

	negative, ok := observations[smokeNegativeObjective]
	if !ok {
		return nil, errors.New("negative risk-control objective was not investigated")
	}
	if negative.Strength != providers.StrengthNotSupported || negative.Assurance != providers.AssuranceInvestigationNoEvidence {
		return nil, fmt.Errorf("negative objective returned strength %q and assurance %q, want not_supported/investigation-no-evidence", negative.Strength, negative.Assurance)
	}
	if len(negative.SupportingEvidence) != 0 {
		return nil, errors.New("negative objective invented supporting evidence")
	}
	return []string{
		fmt.Sprintf("PASS positive override: %s, %s, %d grounded reference(s)", positive.Strength, positive.Assurance, len(positive.SupportingEvidence)),
		fmt.Sprintf("PASS negative risk-control search: %s, %s", negative.Strength, negative.Assurance),
	}, nil
}
