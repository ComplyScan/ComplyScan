package profiledraft

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type draftFunc func(context.Context, providers.ProfileDraftRequest) (providers.ProfileDraftResult, error)

func (function draftFunc) DraftProfile(ctx context.Context, request providers.ProfileDraftRequest) (providers.ProfileDraftResult, error) {
	return function(ctx, request)
}

func TestRunBenchmarkPassesGroundedLabelledDrafts(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "profile-draft-evaluation", "manifest.json")
	manifest, err := LoadBenchmarkManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	byRepository := make(map[string]BenchmarkCase, len(manifest.Cases))
	for _, benchmarkCase := range manifest.Cases {
		byRepository[filepath.Base(benchmarkCase.Repository)] = benchmarkCase
	}
	drafter := draftFunc(func(_ context.Context, request providers.ProfileDraftRequest) (providers.ProfileDraftResult, error) {
		benchmarkCase := byRepository[request.RepositoryName]
		suggestions := suggestionsForClaims(benchmarkCase.Expected)
		return providers.ProfileDraftResult{
			Model: "fixture", Suggestions: suggestions,
			Usage: providers.Usage{PromptTokens: 100, CompletionTokens: 20},
		}, nil
	})

	report, err := RunBenchmark(context.Background(), manifestPath, manifest, "fixture", drafter)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("expected benchmark to pass: %+v", report.Summary)
	}
	if report.Summary.Precision != 1 || report.Summary.Recall != 1 {
		t.Fatalf("unexpected metrics: %+v", report.Summary)
	}
	if report.Summary.Completed != len(manifest.Cases) {
		t.Fatalf("completed %d cases, want %d", report.Summary.Completed, len(manifest.Cases))
	}
	if report.Summary.PromptTokens != 400 || report.Summary.CompletionTokens != 80 {
		t.Fatalf("unexpected token totals: %+v", report.Summary)
	}
	var output bytes.Buffer
	if err := WriteBenchmarkSummary(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "PASS profile-draft benchmark") {
		t.Fatalf("unexpected summary:\n%s", output.String())
	}
}

func TestRunBenchmarkRejectsForbiddenAndUngroundedDrafts(t *testing.T) {
	manifestPath := filepath.Join("..", "..", "testdata", "profile-draft-evaluation", "manifest.json")
	manifest, err := LoadBenchmarkManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Cases = []BenchmarkCase{manifest.Cases[len(manifest.Cases)-1]}
	drafter := draftFunc(func(context.Context, providers.ProfileDraftRequest) (providers.ProfileDraftResult, error) {
		return providers.ProfileDraftResult{Suggestions: []providers.ProfileSuggestion{{
			Field: "ai-activities", Values: []string{"inference"}, Confidence: "high",
			Rationale: "Invented from documentation.",
			Evidence:  []providers.ProfileEvidence{{Path: "missing.py", Line: 400, Summary: "Not real."}},
		}}}, nil
	})

	report, err := RunBenchmark(context.Background(), manifestPath, manifest, "unsafe", drafter)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatal("expected unsafe benchmark result to fail")
	}
	if report.Summary.ForbiddenClaims != 1 || report.Summary.UngroundedReferences != 1 || report.Summary.FalsePositives != 1 {
		t.Fatalf("unexpected unsafe metrics: %+v", report.Summary)
	}
}

func TestLoadBenchmarkManifestRejectsEscapingRepository(t *testing.T) {
	directory := t.TempDir()
	manifest := BenchmarkManifest{
		SchemaVersion:   BenchmarkSchemaVersion,
		EvaluatedFields: []string{"ai-activities"},
		Acceptance:      BenchmarkAcceptance{MinimumPrecision: 1, MinimumRecall: 1, MaximumCaseSeconds: 1},
		Cases:           []BenchmarkCase{{ID: "escape", Repository: "../"}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBenchmarkManifest(path); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected escaping repository error, got %v", err)
	}
}

func suggestionsForClaims(claims []BenchmarkClaim) []providers.ProfileSuggestion {
	byField := make(map[string]*providers.ProfileSuggestion)
	order := make([]string, 0)
	for _, claim := range claims {
		suggestion := byField[claim.Field]
		if suggestion == nil {
			path := claim.EvidenceAnyOf[0]
			suggestion = &providers.ProfileSuggestion{
				Field: claim.Field, Confidence: "high", Rationale: "Labelled fixture evidence.",
				Evidence: []providers.ProfileEvidence{{Path: path, Line: 1, Summary: "Labelled fixture evidence."}},
			}
			byField[claim.Field] = suggestion
			order = append(order, claim.Field)
		}
		suggestion.Values = append(suggestion.Values, claim.Value)
	}
	result := make([]providers.ProfileSuggestion, 0, len(order))
	for _, field := range order {
		result = append(result, *byField[field])
	}
	return result
}
