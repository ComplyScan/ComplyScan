package profiledraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const BenchmarkSchemaVersion = 1

type Drafter interface {
	DraftProfile(context.Context, providers.ProfileDraftRequest) (providers.ProfileDraftResult, error)
}

type BenchmarkProgress struct {
	CaseID     string
	Index      int
	Total      int
	Done       bool
	DurationMS int64
	Err        error
}

type BenchmarkProgressHandler func(BenchmarkProgress)

type BenchmarkManifest struct {
	SchemaVersion   int                 `json:"schema_version"`
	Description     string              `json:"description"`
	EvaluatedFields []string            `json:"evaluated_fields"`
	Acceptance      BenchmarkAcceptance `json:"acceptance"`
	Cases           []BenchmarkCase     `json:"cases"`
}

type BenchmarkAcceptance struct {
	MinimumPrecision            float64 `json:"minimum_precision"`
	MinimumRecall               float64 `json:"minimum_recall"`
	MaximumForbiddenClaims      int     `json:"maximum_forbidden_claims"`
	MaximumUngroundedReferences int     `json:"maximum_ungrounded_references"`
	MaximumCaseSeconds          int     `json:"maximum_case_seconds"`
}

type BenchmarkCase struct {
	ID              string           `json:"id"`
	Repository      string           `json:"repository"`
	Expected        []BenchmarkClaim `json:"expected"`
	ForbiddenFields []string         `json:"forbidden_fields"`
}

type BenchmarkClaim struct {
	Field         string   `json:"field"`
	Value         string   `json:"value"`
	EvidenceAnyOf []string `json:"evidence_any_of,omitempty"`
}

type BenchmarkReport struct {
	SchemaVersion int                   `json:"schema_version"`
	PromptVersion int                   `json:"prompt_version"`
	Model         string                `json:"model"`
	GeneratedAt   string                `json:"generated_at"`
	Manifest      string                `json:"manifest"`
	Acceptance    BenchmarkAcceptance   `json:"acceptance"`
	Summary       BenchmarkSummary      `json:"summary"`
	Cases         []BenchmarkCaseResult `json:"cases"`
	Passed        bool                  `json:"passed"`
}

type BenchmarkSummary struct {
	Cases                 int     `json:"cases"`
	Completed             int     `json:"completed"`
	TruePositives         int     `json:"true_positives"`
	FalsePositives        int     `json:"false_positives"`
	FalseNegatives        int     `json:"false_negatives"`
	Precision             float64 `json:"precision"`
	Recall                float64 `json:"recall"`
	ForbiddenClaims       int     `json:"forbidden_claims"`
	UngroundedReferences  int     `json:"ungrounded_references"`
	PromptTokens          int     `json:"prompt_tokens,omitempty"`
	CompletionTokens      int     `json:"completion_tokens,omitempty"`
	TotalDurationMS       int64   `json:"total_duration_ms"`
	SlowestCaseDurationMS int64   `json:"slowest_case_duration_ms"`
}

type BenchmarkCaseResult struct {
	ID                   string                        `json:"id"`
	Repository           string                        `json:"repository"`
	Contexts             int                           `json:"contexts"`
	Suggestions          []providers.ProfileSuggestion `json:"suggestions"`
	TruePositives        int                           `json:"true_positives"`
	FalsePositives       int                           `json:"false_positives"`
	FalseNegatives       int                           `json:"false_negatives"`
	ForbiddenClaims      int                           `json:"forbidden_claims"`
	UngroundedReferences int                           `json:"ungrounded_references"`
	MissingClaims        []BenchmarkClaim              `json:"missing_claims,omitempty"`
	UnexpectedClaims     []BenchmarkClaim              `json:"unexpected_claims,omitempty"`
	DurationMS           int64                         `json:"duration_ms"`
	PromptTokens         int                           `json:"prompt_tokens,omitempty"`
	CompletionTokens     int                           `json:"completion_tokens,omitempty"`
	Error                string                        `json:"error,omitempty"`
	Passed               bool                          `json:"passed"`
}

func LoadBenchmarkManifest(path string) (BenchmarkManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BenchmarkManifest{}, fmt.Errorf("read profile-draft benchmark manifest: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest BenchmarkManifest
	if err := decoder.Decode(&manifest); err != nil {
		return BenchmarkManifest{}, fmt.Errorf("decode profile-draft benchmark manifest: %w", err)
	}
	if err := validateBenchmarkManifest(path, manifest); err != nil {
		return BenchmarkManifest{}, err
	}
	return manifest, nil
}

func RunBenchmark(ctx context.Context, manifestPath string, manifest BenchmarkManifest, model string, drafter Drafter) (BenchmarkReport, error) {
	return RunBenchmarkWithProgress(ctx, manifestPath, manifest, model, drafter, nil)
}

func RunBenchmarkWithProgress(ctx context.Context, manifestPath string, manifest BenchmarkManifest, model string, drafter Drafter, onProgress BenchmarkProgressHandler) (BenchmarkReport, error) {
	if drafter == nil {
		return BenchmarkReport{}, errors.New("profile-draft benchmark requires a drafter")
	}
	manifestAbsolute, err := filepath.Abs(manifestPath)
	if err != nil {
		return BenchmarkReport{}, err
	}
	base := filepath.Dir(manifestAbsolute)
	report := BenchmarkReport{
		SchemaVersion: BenchmarkSchemaVersion, PromptVersion: providers.ProfileDraftPromptVersion,
		Model: model, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Manifest: filepath.ToSlash(manifestPath),
		Acceptance: manifest.Acceptance, Cases: make([]BenchmarkCaseResult, 0, len(manifest.Cases)),
	}
	evaluated := stringSet(manifest.EvaluatedFields)
	for index, benchmarkCase := range manifest.Cases {
		if onProgress != nil {
			onProgress(BenchmarkProgress{CaseID: benchmarkCase.ID, Index: index + 1, Total: len(manifest.Cases)})
		}
		caseResult, runErr := runBenchmarkCase(ctx, base, benchmarkCase, evaluated, manifest.Acceptance.MaximumCaseSeconds, drafter)
		if runErr != nil {
			caseResult.Error = runErr.Error()
			caseResult.Passed = false
		}
		report.Cases = append(report.Cases, caseResult)
		if onProgress != nil {
			onProgress(BenchmarkProgress{
				CaseID: benchmarkCase.ID, Index: index + 1, Total: len(manifest.Cases), Done: true,
				DurationMS: caseResult.DurationMS, Err: runErr,
			})
		}
	}
	report.Summary = summarizeBenchmark(report.Cases)
	report.Passed = report.Summary.Completed == len(manifest.Cases) &&
		report.Summary.Precision >= manifest.Acceptance.MinimumPrecision &&
		report.Summary.Recall >= manifest.Acceptance.MinimumRecall &&
		report.Summary.ForbiddenClaims <= manifest.Acceptance.MaximumForbiddenClaims &&
		report.Summary.UngroundedReferences <= manifest.Acceptance.MaximumUngroundedReferences &&
		report.Summary.SlowestCaseDurationMS <= int64(manifest.Acceptance.MaximumCaseSeconds)*1000
	return report, nil
}

func runBenchmarkCase(ctx context.Context, base string, benchmarkCase BenchmarkCase, evaluated map[string]struct{}, maximumSeconds int, drafter Drafter) (BenchmarkCaseResult, error) {
	result := BenchmarkCaseResult{ID: benchmarkCase.ID, Repository: benchmarkCase.Repository, Suggestions: []providers.ProfileSuggestion{}}
	repositoryPath, err := resolveBenchmarkRepository(base, benchmarkCase.Repository)
	if err != nil {
		return result, err
	}
	discovered, err := discovery.Discover(ctx, repositoryPath, discovery.Options{})
	if err != nil {
		return result, err
	}
	inventoryReport := inventory.NewReport(repositoryPath, "profile-draft-benchmark", inventory.Analyze(discovered.Repository), discovered.Warnings)
	request := BuildRequest(repositoryPath, discovered.Repository, Languages(discovered.Repository), inventoryReport)
	deterministic := DeterministicSuggestions(inventoryReport)
	result.Contexts = len(request.Contexts)
	caseContext, cancel := context.WithTimeout(ctx, time.Duration(maximumSeconds)*time.Second)
	started := time.Now()
	draft, err := drafter.DraftProfile(caseContext, request)
	result.DurationMS = time.Since(started).Milliseconds()
	cancel()
	if err != nil {
		return result, err
	}
	result.Suggestions = SuggestionSlice(MergeSuggestions(deterministic, draft.Suggestions))
	result.PromptTokens = draft.Usage.PromptTokens
	result.CompletionTokens = draft.Usage.CompletionTokens
	evaluateBenchmarkCase(&result, benchmarkCase, evaluated, discovered.Repository)
	result.Passed = result.ForbiddenClaims == 0 && result.UngroundedReferences == 0 && result.DurationMS <= int64(maximumSeconds)*1000
	return result, nil
}

func evaluateBenchmarkCase(result *BenchmarkCaseResult, benchmarkCase BenchmarkCase, evaluated map[string]struct{}, repository discovery.Repository) {
	expected := make(map[string]BenchmarkClaim, len(benchmarkCase.Expected))
	for _, claim := range benchmarkCase.Expected {
		expected[claimKey(claim.Field, claim.Value)] = claim
	}
	forbiddenFields := stringSet(benchmarkCase.ForbiddenFields)
	files := make(map[string]int, len(repository.Files))
	for _, file := range repository.Files {
		files[file.Path] = lineCount(file.Content)
	}
	actual := make(map[string]providers.ProfileSuggestion)
	for _, suggestion := range result.Suggestions {
		for _, evidence := range suggestion.Evidence {
			lines, exists := files[evidence.Path]
			if !exists || evidence.Line < 0 || evidence.Line > lines {
				result.UngroundedReferences++
			}
		}
		for _, value := range suggestion.Values {
			key := claimKey(suggestion.Field, value)
			actual[key] = suggestion
			if _, forbidden := forbiddenFields[suggestion.Field]; forbidden {
				result.ForbiddenClaims++
			}
		}
	}
	for key, claim := range expected {
		suggestion, exists := actual[key]
		if !exists {
			result.FalseNegatives++
			result.MissingClaims = append(result.MissingClaims, claim)
			continue
		}
		if len(claim.EvidenceAnyOf) > 0 && !suggestionCitesAny(suggestion, claim.EvidenceAnyOf) {
			result.UngroundedReferences++
		}
		result.TruePositives++
	}
	for key, suggestion := range actual {
		if _, inScope := evaluated[suggestion.Field]; !inScope {
			continue
		}
		if _, exists := expected[key]; exists {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		result.FalsePositives++
		result.UnexpectedClaims = append(result.UnexpectedClaims, BenchmarkClaim{Field: parts[0], Value: parts[1]})
	}
	sortClaims(result.MissingClaims)
	sortClaims(result.UnexpectedClaims)
}

func summarizeBenchmark(cases []BenchmarkCaseResult) BenchmarkSummary {
	summary := BenchmarkSummary{Cases: len(cases)}
	for _, result := range cases {
		if result.Error == "" {
			summary.Completed++
		}
		summary.TruePositives += result.TruePositives
		summary.FalsePositives += result.FalsePositives
		summary.FalseNegatives += result.FalseNegatives
		summary.ForbiddenClaims += result.ForbiddenClaims
		summary.UngroundedReferences += result.UngroundedReferences
		summary.PromptTokens += result.PromptTokens
		summary.CompletionTokens += result.CompletionTokens
		summary.TotalDurationMS += result.DurationMS
		if result.DurationMS > summary.SlowestCaseDurationMS {
			summary.SlowestCaseDurationMS = result.DurationMS
		}
	}
	if denominator := summary.TruePositives + summary.FalsePositives; denominator > 0 {
		summary.Precision = float64(summary.TruePositives) / float64(denominator)
	} else if summary.FalseNegatives == 0 {
		summary.Precision = 1
	}
	if denominator := summary.TruePositives + summary.FalseNegatives; denominator > 0 {
		summary.Recall = float64(summary.TruePositives) / float64(denominator)
	} else {
		summary.Recall = 1
	}
	return summary
}

func WriteBenchmarkSummary(output io.Writer, report BenchmarkReport) error {
	status := "FAIL"
	if report.Passed {
		status = "PASS"
	}
	if _, err := fmt.Fprintf(output, "%s profile-draft benchmark for %s\n", status, report.Model); err != nil {
		return err
	}
	for _, result := range report.Cases {
		caseStatus := "PASS"
		if !result.Passed {
			caseStatus = "FAIL"
		}
		if result.Error != "" {
			if _, err := fmt.Fprintf(output, "  %s %s: %s (%d ms)\n", caseStatus, result.ID, result.Error, result.DurationMS); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(output, "  %s %s: TP=%d FP=%d FN=%d forbidden=%d ungrounded=%d contexts=%d duration=%d ms\n",
			caseStatus, result.ID, result.TruePositives, result.FalsePositives, result.FalseNegatives,
			result.ForbiddenClaims, result.UngroundedReferences, result.Contexts, result.DurationMS); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output,
		"Summary: precision=%.1f%% recall=%.1f%% forbidden=%d ungrounded=%d tokens=%d+%d total=%d ms slowest=%d ms\n",
		report.Summary.Precision*100, report.Summary.Recall*100, report.Summary.ForbiddenClaims,
		report.Summary.UngroundedReferences, report.Summary.PromptTokens, report.Summary.CompletionTokens,
		report.Summary.TotalDurationMS, report.Summary.SlowestCaseDurationMS)
	return err
}

func validateBenchmarkManifest(path string, manifest BenchmarkManifest) error {
	if manifest.SchemaVersion != BenchmarkSchemaVersion {
		return fmt.Errorf("profile-draft benchmark schema_version is %d, want %d", manifest.SchemaVersion, BenchmarkSchemaVersion)
	}
	if len(manifest.Cases) == 0 || len(manifest.EvaluatedFields) == 0 {
		return errors.New("profile-draft benchmark requires cases and evaluated_fields")
	}
	if manifest.Acceptance.MinimumPrecision < 0 || manifest.Acceptance.MinimumPrecision > 1 || manifest.Acceptance.MinimumRecall < 0 || manifest.Acceptance.MinimumRecall > 1 {
		return errors.New("profile-draft benchmark precision and recall thresholds must be between 0 and 1")
	}
	if manifest.Acceptance.MaximumForbiddenClaims < 0 || manifest.Acceptance.MaximumUngroundedReferences < 0 || manifest.Acceptance.MaximumCaseSeconds <= 0 {
		return errors.New("profile-draft benchmark limits must be non-negative and maximum_case_seconds must be positive")
	}
	evaluated := stringSet(manifest.EvaluatedFields)
	seenCases := make(map[string]struct{}, len(manifest.Cases))
	for _, benchmarkCase := range manifest.Cases {
		if strings.TrimSpace(benchmarkCase.ID) == "" {
			return errors.New("profile-draft benchmark case ID must not be empty")
		}
		if _, duplicate := seenCases[benchmarkCase.ID]; duplicate {
			return fmt.Errorf("profile-draft benchmark case ID %q is duplicated", benchmarkCase.ID)
		}
		seenCases[benchmarkCase.ID] = struct{}{}
		if _, err := resolveBenchmarkRepository(filepath.Dir(path), benchmarkCase.Repository); err != nil {
			return fmt.Errorf("profile-draft benchmark case %q: %w", benchmarkCase.ID, err)
		}
		seenClaims := map[string]struct{}{}
		for _, claim := range benchmarkCase.Expected {
			if _, exists := evaluated[claim.Field]; !exists {
				return fmt.Errorf("profile-draft benchmark case %q evaluates undeclared field %q", benchmarkCase.ID, claim.Field)
			}
			if strings.TrimSpace(claim.Value) == "" {
				return fmt.Errorf("profile-draft benchmark case %q has an empty expected value", benchmarkCase.ID)
			}
			key := claimKey(claim.Field, claim.Value)
			if _, duplicate := seenClaims[key]; duplicate {
				return fmt.Errorf("profile-draft benchmark case %q duplicates expected claim %s=%s", benchmarkCase.ID, claim.Field, claim.Value)
			}
			seenClaims[key] = struct{}{}
		}
	}
	return nil
}

func resolveBenchmarkRepository(base, value string) (string, error) {
	if filepath.IsAbs(value) || strings.TrimSpace(value) == "" {
		return "", errors.New("repository must be a non-empty relative path")
	}
	baseAbsolute, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(baseAbsolute, value))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(baseAbsolute, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("repository escapes the benchmark directory")
	}
	resolvedBase, err := filepath.EvalSymlinks(baseAbsolute)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark directory: %w", err)
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect repository: %w", err)
	}
	resolvedRelative, err := filepath.Rel(resolvedBase, resolvedCandidate)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return "", errors.New("repository symlink escapes the benchmark directory")
	}
	info, err := os.Stat(resolvedCandidate)
	if err != nil {
		return "", fmt.Errorf("inspect repository: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("repository is not a directory")
	}
	return resolvedCandidate, nil
}

func suggestionCitesAny(suggestion providers.ProfileSuggestion, wanted []string) bool {
	allowed := stringSet(wanted)
	for _, evidence := range suggestion.Evidence {
		if _, exists := allowed[evidence.Path]; exists {
			return true
		}
	}
	return false
}

func lineCount(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	return strings.Count(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") + 1
}

func claimKey(field, value string) string { return field + "\x00" + value }

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortClaims(values []BenchmarkClaim) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Field != values[j].Field {
			return values[i].Field < values[j].Field
		}
		return values[i].Value < values[j].Value
	})
}
