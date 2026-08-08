package main

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

func TestValidateOllamaResultAcceptsExpectedLiveAndHardNegativeStrengths(t *testing.T) {
	value := validationReport(providers.StrengthPartial, providers.StrengthNotSupported)
	lines, err := validateOllamaResult(value, "qwen3:8b")
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "production-routed") || !strings.Contains(joined, "test-only") {
		t.Fatalf("unexpected validation summary:\n%s", joined)
	}
}

func TestValidateOllamaResultRejectsStrongTestOnlyCandidate(t *testing.T) {
	value := validationReport(providers.StrengthStrong, providers.StrengthStrong)
	_, err := validateOllamaResult(value, "qwen3:8b")
	if err == nil || !strings.Contains(err.Error(), "test-only candidate strength") {
		t.Fatalf("got error %v", err)
	}
}

func TestValidateOllamaResultRejectsHiddenGuardrailCorrection(t *testing.T) {
	value := validationReport(providers.StrengthStrong, providers.StrengthWeak)
	value.Frameworks[0].TechnicalReview.Observations[1].ModelStrength = providers.StrengthPartial
	value.Frameworks[0].TechnicalReview.Observations[1].GuardrailNote = "Test-only cap applied."
	_, err := validateOllamaResult(value, "qwen3:8b")
	if err == nil || !strings.Contains(err.Error(), "required a deterministic guardrail") {
		t.Fatalf("got error %v", err)
	}
}

func validationReport(production, testOnly providers.EvidenceStrength) report.Report {
	const (
		productionFingerprint = "production-fingerprint"
		testFingerprint       = "test-fingerprint"
	)
	value := report.Report{Frameworks: make([]report.FrameworkResult, 0, len(validationTargets))}
	for _, target := range validationTargets {
		value.Frameworks = append(value.Frameworks, report.FrameworkResult{
			ID: target.FrameworkID,
			TechnicalEvidence: framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
				ID: target.ObjectiveID,
				Matches: []framework.EvidenceMatch{
					{Fingerprint: productionFingerprint, Context: contextWithReachability(codegraph.ReachableProduction)},
					{Fingerprint: testFingerprint, Context: contextWithReachability(codegraph.ReachableTestOnly)},
				},
			}}},
			TechnicalReview: &providers.TechnicalReviewResult{
				Model: "qwen3:8b",
				Observations: []providers.TechnicalObservation{
					{ObjectiveID: target.ObjectiveID, EvidenceFingerprint: productionFingerprint, Strength: production},
					{ObjectiveID: target.ObjectiveID, EvidenceFingerprint: testFingerprint, Strength: testOnly},
				},
			},
		})
	}
	return value
}

func contextWithReachability(value codegraph.Reachability) codegraph.ContextPackage {
	return codegraph.ContextPackage{Anchor: &codegraph.SymbolReference{Reachability: value}}
}
