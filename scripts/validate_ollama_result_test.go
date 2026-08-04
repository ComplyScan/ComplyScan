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

func validationReport(production, testOnly providers.EvidenceStrength) report.Report {
	const (
		productionFingerprint = "production-fingerprint"
		testFingerprint       = "test-fingerprint"
	)
	return report.Report{
		TechnicalEvidence: &framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
			ID: validationObjective,
			Matches: []framework.EvidenceMatch{
				{Fingerprint: productionFingerprint, Context: contextWithReachability(codegraph.ReachableProduction)},
				{Fingerprint: testFingerprint, Context: contextWithReachability(codegraph.ReachableTestOnly)},
			},
		}}},
		TechnicalReview: &providers.TechnicalReviewResult{
			Model: "qwen3:8b",
			Observations: []providers.TechnicalObservation{
				{ObjectiveID: validationObjective, EvidenceFingerprint: productionFingerprint, Strength: production},
				{ObjectiveID: validationObjective, EvidenceFingerprint: testFingerprint, Strength: testOnly},
			},
		},
	}
}

func contextWithReachability(value codegraph.Reachability) codegraph.ContextPackage {
	return codegraph.ContextPackage{Anchor: &codegraph.SymbolReference{Reachability: value}}
}
