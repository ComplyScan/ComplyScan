package main

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestSemanticScoringRejectsOnlyConfiguredStrength(t *testing.T) {
	configuration := semanticBenchmarkConfig{Rejected: []providers.EvidenceStrength{providers.StrengthNotSupported}}
	benchmarkCase := framework.BenchmarkCase{
		ID:                 "case",
		ExpectedCandidates: []framework.BenchmarkCandidate{{ObjectiveID: "objective", Path: "true.go"}},
	}
	candidates := []providers.TechnicalCandidate{
		{ObjectiveID: "objective", Path: "true.go", EvidenceFingerprint: "true"},
		{ObjectiveID: "objective", Path: "false.go", EvidenceFingerprint: "false"},
	}
	observations := map[string]providers.TechnicalObservation{
		"true":  {EvidenceFingerprint: "true", Strength: providers.StrengthWeak, Confidence: "high"},
		"false": {EvidenceFingerprint: "false", Strength: providers.StrengthNotSupported, Confidence: "high"},
	}
	metrics := semanticBenchmarkMetrics{Strengths: make(map[providers.EvidenceStrength]int)}
	result := scoreSemanticCase(benchmarkCase, candidates, observations, configuration, &metrics)
	metrics.finish()
	if result.FalsePositiveCandidates != 0 || result.FalseNegativeCandidates != 0 {
		t.Fatalf("unexpected case result: %#v", result)
	}
	if metrics.TruePositiveCandidates != 1 || metrics.TrueNegativeCandidates != 1 || metrics.CandidatePrecision != 1 || metrics.CandidateRecall != 1 || metrics.NegativeSpecificity != 1 || metrics.ReviewCoverage != 1 {
		t.Fatalf("unexpected semantic metrics: %#v", metrics)
	}
}

func TestMissingSemanticObservationRetainsCandidateAndFailsCoverage(t *testing.T) {
	configuration := semanticBenchmarkConfig{Rejected: []providers.EvidenceStrength{providers.StrengthNotSupported}}
	benchmarkCase := framework.BenchmarkCase{ID: "case", ExpectedCandidates: []framework.BenchmarkCandidate{{ObjectiveID: "objective", Path: "candidate.go"}}}
	candidates := []providers.TechnicalCandidate{{ObjectiveID: "objective", Path: "candidate.go", EvidenceFingerprint: "candidate"}}
	metrics := semanticBenchmarkMetrics{Strengths: make(map[providers.EvidenceStrength]int)}
	result := scoreSemanticCase(benchmarkCase, candidates, nil, configuration, &metrics)
	metrics.finish()
	if metrics.TruePositiveCandidates != 1 || metrics.ReviewCoverage != 0 || len(result.Failures) == 0 || !result.Decisions[0].Accepted {
		t.Fatalf("missing observation was not handled conservatively: result=%#v metrics=%#v", result, metrics)
	}
}

func TestSemanticCandidatePathFilterIsExact(t *testing.T) {
	candidates := []providers.TechnicalCandidate{{Path: "site/blog.ts"}, {Path: "site/blog.tsx"}}
	filtered := filterTechnicalCandidates(candidates, "site/blog.ts")
	if len(filtered) != 1 || filtered[0].Path != "site/blog.ts" {
		t.Fatalf("unexpected exact path filter result: %#v", filtered)
	}
}
