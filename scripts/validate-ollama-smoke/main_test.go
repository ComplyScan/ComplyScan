package main

import (
	"path/filepath"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

func TestSmokeFixturePinsBoundedEvidenceInvestigation(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "ollama-investigation-smoke", config.FileName)
	configured, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if configured.AI.RepositoryAnalysis.Mode != "bounded-only" {
		t.Fatalf("repository analysis mode = %q, want bounded-only", configured.AI.RepositoryAnalysis.Mode)
	}
}

func TestValidateOllamaSmokeAcceptsGroundedPositiveAndNegative(t *testing.T) {
	value := report.Report{TechnicalReview: &providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{
		{ObjectiveID: smokePositiveObjective, Strength: providers.StrengthPartial, Assurance: providers.AssuranceAISubstantiated, SupportingEvidence: []providers.TechnicalEvidenceClaim{{Path: "control/override.go", Summary: "Authorised override"}}},
		{ObjectiveID: smokeNegativeObjective, Strength: providers.StrengthNotSupported, Assurance: providers.AssuranceInvestigationNoEvidence},
	}}}
	if _, err := validateOllamaSmoke(value); err != nil {
		t.Fatal(err)
	}
}

func TestValidateOllamaSmokeRejectsWeakOrInventedResults(t *testing.T) {
	value := report.Report{TechnicalReview: &providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{
		{ObjectiveID: smokePositiveObjective, Strength: providers.StrengthWeak, Assurance: providers.AssuranceSignalDetected},
		{ObjectiveID: smokeNegativeObjective, Strength: providers.StrengthPartial, Assurance: providers.AssuranceAISubstantiated, SupportingEvidence: []providers.TechnicalEvidenceClaim{{Path: "invented.go", Summary: "Invented"}}},
	}}}
	if _, err := validateOllamaSmoke(value); err == nil {
		t.Fatal("weak or invented smoke result was accepted")
	}
}
