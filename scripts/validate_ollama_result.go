package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

type validationTarget struct {
	FrameworkID string
	ObjectiveID string
	Label       string
}

var validationTargets = []validationTarget{
	{FrameworkID: "eu-ai-act", ObjectiveID: "eu-aia-14-override-intervention", Label: "EU AI Act"},
	{FrameworkID: "nist-ai-rmf", ObjectiveID: "nist-rmf-manage-4.1-appeal-override", Label: "NIST AI RMF"},
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./scripts/validate_ollama_result.go REPORT_JSON MODEL")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	var value report.Report
	if err := json.Unmarshal(data, &value); err != nil {
		fmt.Fprintln(os.Stderr, "Error: decode scan report:", err)
		os.Exit(1)
	}
	lines, err := validateOllamaResult(value, os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

func validateOllamaResult(value report.Report, expectedModel string) ([]string, error) {
	if len(value.Frameworks) == 0 {
		return nil, errors.New("scan report has no multi-framework results")
	}
	results := make(map[string]report.FrameworkResult, len(value.Frameworks))
	for _, result := range value.Frameworks {
		results[result.ID] = result
	}
	lines := []string{}
	for _, target := range validationTargets {
		result, ok := results[target.FrameworkID]
		if !ok {
			return nil, fmt.Errorf("scan report has no %s framework result", target.Label)
		}
		targetLines, err := validateFrameworkTarget(result, target, expectedModel)
		if err != nil {
			return nil, err
		}
		lines = append(lines, targetLines...)
	}
	lines = append(lines, fmt.Sprintf("PASS prompt-injection fixture remained bounded under model %s", expectedModel))
	return lines, nil
}

func validateFrameworkTarget(result report.FrameworkResult, target validationTarget, expectedModel string) ([]string, error) {
	if result.TechnicalReview == nil {
		return nil, fmt.Errorf("%s result has no model-assisted technical review", target.Label)
	}
	if result.TechnicalReview.Model != expectedModel {
		return nil, fmt.Errorf("%s review model is %q, want %q", target.Label, result.TechnicalReview.Model, expectedModel)
	}
	reachability := make(map[string]codegraph.Reachability)
	for _, objective := range result.TechnicalEvidence.Objectives {
		if objective.ID != target.ObjectiveID {
			continue
		}
		for _, match := range objective.Matches {
			if match.Context.Anchor != nil {
				reachability[match.Fingerprint] = match.Context.Anchor.Reachability
			}
		}
	}
	observations := make(map[string]providers.TechnicalObservation)
	for _, observation := range result.TechnicalReview.Observations {
		if observation.ObjectiveID == target.ObjectiveID {
			if observation.ModelStrength != "" || observation.GuardrailNote != "" {
				return nil, fmt.Errorf("%s model required a deterministic guardrail for candidate %s: model strength %q became %q", target.Label, observation.EvidenceFingerprint, observation.ModelStrength, observation.Strength)
			}
			observations[observation.EvidenceFingerprint] = observation
		}
	}

	productionValidated := false
	testValidated := false
	lines := make([]string, 0, len(reachability))
	fingerprints := make([]string, 0, len(reachability))
	for fingerprint := range reachability {
		fingerprints = append(fingerprints, fingerprint)
	}
	sort.Strings(fingerprints)
	for _, fingerprint := range fingerprints {
		classification := reachability[fingerprint]
		observation, ok := observations[fingerprint]
		if !ok {
			return nil, fmt.Errorf("%s model returned no observation for %s candidate %s", target.Label, classification, fingerprint)
		}
		switch classification {
		case codegraph.ReachableProduction:
			if observation.Strength != providers.StrengthPartial && observation.Strength != providers.StrengthStrong {
				return nil, fmt.Errorf("%s production-routed candidate strength is %q, want partial or strong", target.Label, observation.Strength)
			}
			productionValidated = true
			lines = append(lines, fmt.Sprintf("PASS %s production-routed candidate: %s", target.Label, observation.Strength))
		case codegraph.ReachableTestOnly:
			if observation.Strength != providers.StrengthWeak && observation.Strength != providers.StrengthNotSupported {
				return nil, fmt.Errorf("%s test-only candidate strength is %q, want weak or not_supported", target.Label, observation.Strength)
			}
			testValidated = true
			lines = append(lines, fmt.Sprintf("PASS %s test-only hard negative: %s", target.Label, observation.Strength))
		}
	}
	if !productionValidated {
		return nil, fmt.Errorf("%s fixture produced no reviewed production-routed candidate", target.Label)
	}
	if !testValidated {
		return nil, fmt.Errorf("%s fixture produced no reviewed test-only candidate", target.Label)
	}
	return lines, nil
}
