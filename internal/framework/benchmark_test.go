package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTechnicalEvidenceBenchmarkCorpus(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "technical-evaluation", "manifest.json")
	manifest, err := LoadBenchmarkManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := LoadBuiltin(manifest.PackID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunBenchmark(context.Background(), manifestPath, manifest, pack)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("benchmark failed: %#v", report.Failures)
	}
	metrics := report.Metrics
	if metrics.TruePositiveCandidates != 10 || metrics.FalsePositiveCandidates != 0 || metrics.FalseNegativeCandidates != 0 {
		t.Fatalf("unexpected candidate metrics: %#v", metrics)
	}
	if metrics.CorrectAnchors != 10 || metrics.CorrectReachability != 10 || metrics.MatchedRelationships != 14 || metrics.ForbiddenRelationshipsFound != 0 {
		t.Fatalf("unexpected context metrics: %#v", metrics)
	}
	if metrics.DetectedLanguages != 5 || metrics.ExpectedLanguages != 5 {
		t.Fatalf("unexpected language coverage: %#v", metrics)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "training dataset has missing schema fields") {
		t.Fatal("benchmark report leaked repository source")
	}
}

func TestTechnicalEvidenceBenchmarkThresholdDetectsUnexpectedCandidate(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "technical-evaluation", "manifest.json")
	manifest, err := LoadBenchmarkManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Cases[0].ExpectedCandidates = manifest.Cases[0].ExpectedCandidates[1:]
	pack, err := LoadBuiltin(manifest.PackID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunBenchmark(context.Background(), manifestPath, manifest, pack)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.Metrics.FalsePositiveCandidates != 1 || report.Metrics.CandidatePrecision >= manifest.Thresholds.CandidatePrecision {
		t.Fatalf("unexpected threshold result: %#v", report)
	}
	if len(report.Cases[0].Failures) == 0 || !strings.Contains(report.Cases[0].Failures[0], "unexpected candidate") {
		t.Fatalf("missing case diagnostic: %#v", report.Cases[0])
	}
}

func TestBenchmarkManifestIsStrictAndConfined(t *testing.T) {
	manifest := BenchmarkManifest{
		SchemaVersion: TechnicalBenchmarkSchemaVersion,
		Name:          "test", PackID: "pack", PackVersion: "1.0.0",
		Cases: []BenchmarkCase{{ID: "case", Path: "../outside"}},
	}
	if err := manifest.Validate(); err == nil || !strings.Contains(err.Error(), "relative path") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}

	path := filepath.Join(t.TempDir(), "manifest.json")
	data := `{"schema_version":1,"name":"test","pack_id":"pack","pack_version":"1.0.0","thresholds":{},"cases":[],"unknown":true}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBenchmarkManifest(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict field rejection, got %v", err)
	}
}

func TestWriteBenchmarkSummary(t *testing.T) {
	report := BenchmarkReport{
		ManifestName: "test", Pack: PackReference{ID: "pack", Version: "1.0.0"}, Passed: true,
		Metrics: BenchmarkMetrics{CandidatePrecision: 1, CandidateRecall: .9, AnchorAccuracy: 1, ReachabilityAccuracy: 1, RelationshipRecall: .95, LanguageCoverage: 1},
	}
	var output strings.Builder
	if err := WriteBenchmarkSummary(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "PASS technical evidence benchmark") || !strings.Contains(output.String(), "recall 90.0%") {
		t.Fatalf("unexpected summary: %s", output.String())
	}
}
