package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reviewcontext"
)

const semanticBenchmarkSchemaVersion = 1

type semanticBenchmarkConfig struct {
	SchemaVersion  int                          `json:"schema_version"`
	Model          string                       `json:"model"`
	BatchSize      int                          `json:"batch_size"`
	TimeoutSeconds int                          `json:"timeout_seconds"`
	Rejected       []providers.EvidenceStrength `json:"rejected_strengths"`
	Thresholds     semanticBenchmarkThresholds  `json:"thresholds"`
}

type semanticBenchmarkThresholds struct {
	CandidatePrecision  float64 `json:"candidate_precision"`
	CandidateRecall     float64 `json:"candidate_recall"`
	NegativeSpecificity float64 `json:"negative_specificity"`
	ReviewCoverage      float64 `json:"review_coverage"`
}

type semanticBenchmarkReport struct {
	SchemaVersion int                         `json:"schema_version"`
	Model         string                      `json:"model"`
	Policy        semanticBenchmarkPolicy     `json:"policy"`
	Thresholds    semanticBenchmarkThresholds `json:"thresholds"`
	Metrics       semanticBenchmarkMetrics    `json:"metrics"`
	Cases         []semanticBenchmarkCase     `json:"cases"`
	Passed        bool                        `json:"passed"`
	Failures      []string                    `json:"failures,omitempty"`
}

type semanticBenchmarkPolicy struct {
	RejectedStrengths  []providers.EvidenceStrength `json:"rejected_strengths"`
	MissingObservation string                       `json:"missing_observation"`
}

type semanticBenchmarkMetrics struct {
	InputCandidates         int                                `json:"input_candidates"`
	ReviewedCandidates      int                                `json:"reviewed_candidates"`
	TruePositiveCandidates  int                                `json:"true_positive_candidates"`
	FalsePositiveCandidates int                                `json:"false_positive_candidates"`
	TrueNegativeCandidates  int                                `json:"true_negative_candidates"`
	FalseNegativeCandidates int                                `json:"false_negative_candidates"`
	CandidatePrecision      float64                            `json:"candidate_precision"`
	CandidateRecall         float64                            `json:"candidate_recall"`
	NegativeSpecificity     float64                            `json:"negative_specificity"`
	ReviewCoverage          float64                            `json:"review_coverage"`
	Strengths               map[providers.EvidenceStrength]int `json:"strengths"`
	Usage                   providers.Usage                    `json:"usage,omitempty"`
}

type semanticBenchmarkCase struct {
	ID                      string                      `json:"id"`
	ExpectedCandidates      int                         `json:"expected_candidates"`
	InputCandidates         int                         `json:"input_candidates"`
	FalsePositiveCandidates int                         `json:"false_positive_candidates"`
	FalseNegativeCandidates int                         `json:"false_negative_candidates"`
	Decisions               []semanticBenchmarkDecision `json:"decisions"`
	Failures                []string                    `json:"failures,omitempty"`
}

type semanticBenchmarkDecision struct {
	ObjectiveID         string                     `json:"objective_id"`
	Path                string                     `json:"path"`
	EvidenceFingerprint string                     `json:"evidence_fingerprint,omitempty"`
	ExpectedCandidate   bool                       `json:"expected_candidate"`
	Reviewed            bool                       `json:"reviewed"`
	Strength            providers.EvidenceStrength `json:"strength,omitempty"`
	ModelStrength       providers.EvidenceStrength `json:"model_strength,omitempty"`
	Confidence          string                     `json:"confidence,omitempty"`
	Rationale           string                     `json:"rationale,omitempty"`
	Accepted            bool                       `json:"accepted"`
	Correct             bool                       `json:"correct"`
}

type externalStudyReport struct {
	Deterministic framework.BenchmarkReport `json:"deterministic"`
	Semantic      *semanticBenchmarkReport  `json:"semantic,omitempty"`
}

type technicalReviewer interface {
	ReviewTechnical(context.Context, providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error)
}

func loadSemanticBenchmarkConfig(path string) (semanticBenchmarkConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return semanticBenchmarkConfig{}, fmt.Errorf("open semantic benchmark config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var configuration semanticBenchmarkConfig
	if err := decoder.Decode(&configuration); err != nil {
		return semanticBenchmarkConfig{}, fmt.Errorf("parse semantic benchmark config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return semanticBenchmarkConfig{}, fmt.Errorf("parse semantic benchmark config: expected one JSON value")
	}
	if err := configuration.validate(); err != nil {
		return semanticBenchmarkConfig{}, fmt.Errorf("validate semantic benchmark config: %w", err)
	}
	return configuration, nil
}

func (configuration semanticBenchmarkConfig) validate() error {
	if configuration.SchemaVersion != semanticBenchmarkSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", configuration.SchemaVersion)
	}
	if strings.TrimSpace(configuration.Model) == "" || configuration.BatchSize < 1 || configuration.BatchSize > 10 || configuration.TimeoutSeconds < 1 {
		return fmt.Errorf("model, batch_size from 1 to 10, and a positive timeout_seconds are required")
	}
	if len(configuration.Rejected) == 0 {
		return fmt.Errorf("rejected_strengths must not be empty")
	}
	seen := make(map[providers.EvidenceStrength]struct{}, len(configuration.Rejected))
	for _, strength := range configuration.Rejected {
		if !validSemanticStrength(strength) {
			return fmt.Errorf("invalid rejected strength %q", strength)
		}
		if _, exists := seen[strength]; exists {
			return fmt.Errorf("duplicate rejected strength %q", strength)
		}
		seen[strength] = struct{}{}
	}
	for name, value := range map[string]float64{
		"candidate_precision":  configuration.Thresholds.CandidatePrecision,
		"candidate_recall":     configuration.Thresholds.CandidateRecall,
		"negative_specificity": configuration.Thresholds.NegativeSpecificity,
		"review_coverage":      configuration.Thresholds.ReviewCoverage,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("threshold %s must be between 0 and 1", name)
		}
	}
	return nil
}

func validSemanticStrength(strength providers.EvidenceStrength) bool {
	switch strength {
	case providers.StrengthStrong, providers.StrengthPartial, providers.StrengthWeak, providers.StrengthUncertain, providers.StrengthNotSupported:
		return true
	default:
		return false
	}
}

func (configuration semanticBenchmarkConfig) rejects(strength providers.EvidenceStrength) bool {
	for _, rejected := range configuration.Rejected {
		if strength == rejected {
			return true
		}
	}
	return false
}

func runSemanticBenchmark(ctx context.Context, workspace string, manifest framework.BenchmarkManifest, pack framework.Pack, configuration semanticBenchmarkConfig, model, candidatePath string, reviewer technicalReviewer) (semanticBenchmarkReport, error) {
	report := semanticBenchmarkReport{
		SchemaVersion: semanticBenchmarkSchemaVersion, Model: model,
		Policy:     semanticBenchmarkPolicy{RejectedStrengths: append([]providers.EvidenceStrength(nil), configuration.Rejected...), MissingObservation: "retain-candidate"},
		Thresholds: configuration.Thresholds,
		Metrics:    semanticBenchmarkMetrics{Strengths: make(map[providers.EvidenceStrength]int)},
		Cases:      make([]semanticBenchmarkCase, 0, len(manifest.Cases)),
	}
	for _, benchmarkCase := range manifest.Cases {
		discovered, err := discovery.Discover(ctx, filepath.Join(workspace, filepath.FromSlash(benchmarkCase.Path)), discovery.Options{})
		if err != nil {
			return semanticBenchmarkReport{}, fmt.Errorf("discover semantic benchmark case %s: %w", benchmarkCase.ID, err)
		}
		evidence := framework.Evaluate(pack, nil, discovered.Repository)
		request := reviewcontext.Build(evidence, discovered.Repository)
		scopedCase := benchmarkCase
		if candidatePath != "" {
			request.Candidates = filterTechnicalCandidates(request.Candidates, candidatePath)
			scopedCase.ExpectedCandidates = filterExpectedCandidates(benchmarkCase.ExpectedCandidates, candidatePath)
		}
		observations := make(map[string]providers.TechnicalObservation, len(request.Candidates))
		for start := 0; start < len(request.Candidates); start += configuration.BatchSize {
			end := start + configuration.BatchSize
			if end > len(request.Candidates) {
				end = len(request.Candidates)
			}
			fmt.Fprintf(os.Stderr, "Reviewing %s candidates %d-%d of %d with %s...\n", benchmarkCase.ID, start+1, end, len(request.Candidates), model)
			batchContext, cancel := context.WithTimeout(ctx, time.Duration(configuration.TimeoutSeconds)*time.Second)
			result, reviewErr := reviewer.ReviewTechnical(batchContext, providers.TechnicalReviewRequest{Candidates: request.Candidates[start:end]})
			cancel()
			if reviewErr != nil {
				return semanticBenchmarkReport{}, fmt.Errorf("review semantic benchmark case %s candidates %d-%d: %w", benchmarkCase.ID, start+1, end, reviewErr)
			}
			report.Metrics.Usage.PromptTokens += result.Usage.PromptTokens
			report.Metrics.Usage.CompletionTokens += result.Usage.CompletionTokens
			report.Metrics.Usage.TotalDurationNS += result.Usage.TotalDurationNS
			for _, observation := range result.Observations {
				if _, duplicate := observations[observation.EvidenceFingerprint]; duplicate {
					return semanticBenchmarkReport{}, fmt.Errorf("semantic benchmark returned duplicate observation %s", observation.EvidenceFingerprint)
				}
				observations[observation.EvidenceFingerprint] = observation
			}
		}
		caseResult := scoreSemanticCase(scopedCase, request.Candidates, observations, configuration, &report.Metrics)
		report.Cases = append(report.Cases, caseResult)
	}
	if candidatePath != "" && report.Metrics.InputCandidates == 0 {
		return semanticBenchmarkReport{}, fmt.Errorf("semantic candidate path %q was not produced by the deterministic stage", candidatePath)
	}
	report.Metrics.finish()
	report.applyThresholds()
	report.Passed = len(report.Failures) == 0
	return report, nil
}

func filterTechnicalCandidates(candidates []providers.TechnicalCandidate, path string) []providers.TechnicalCandidate {
	result := make([]providers.TechnicalCandidate, 0, 1)
	for _, candidate := range candidates {
		if candidate.Path == path {
			result = append(result, candidate)
		}
	}
	return result
}

func filterExpectedCandidates(candidates []framework.BenchmarkCandidate, path string) []framework.BenchmarkCandidate {
	result := make([]framework.BenchmarkCandidate, 0, 1)
	for _, candidate := range candidates {
		if candidate.Path == path {
			result = append(result, candidate)
		}
	}
	return result
}

func scoreSemanticCase(benchmarkCase framework.BenchmarkCase, candidates []providers.TechnicalCandidate, observations map[string]providers.TechnicalObservation, configuration semanticBenchmarkConfig, metrics *semanticBenchmarkMetrics) semanticBenchmarkCase {
	result := semanticBenchmarkCase{ID: benchmarkCase.ID, ExpectedCandidates: len(benchmarkCase.ExpectedCandidates), InputCandidates: len(candidates), Decisions: []semanticBenchmarkDecision{}}
	expected := make(map[string]struct{}, len(benchmarkCase.ExpectedCandidates))
	for _, candidate := range benchmarkCase.ExpectedCandidates {
		expected[semanticCandidateKey(candidate.ObjectiveID, candidate.Path)] = struct{}{}
	}
	actual := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := semanticCandidateKey(candidate.ObjectiveID, candidate.Path)
		actual[key] = struct{}{}
		_, wanted := expected[key]
		observation, reviewed := observations[candidate.EvidenceFingerprint]
		accepted := !reviewed || !configuration.rejects(observation.Strength)
		decision := semanticBenchmarkDecision{
			ObjectiveID: candidate.ObjectiveID, Path: candidate.Path, EvidenceFingerprint: candidate.EvidenceFingerprint,
			ExpectedCandidate: wanted, Reviewed: reviewed, Accepted: accepted, Correct: wanted == accepted,
		}
		if reviewed {
			decision.Strength, decision.ModelStrength, decision.Confidence = observation.Strength, observation.ModelStrength, observation.Confidence
			decision.Rationale = observation.Rationale
			metrics.ReviewedCandidates++
			metrics.Strengths[observation.Strength]++
		} else {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: missing semantic observation %s", benchmarkCase.ID, key))
		}
		metrics.InputCandidates++
		scoreSemanticDecision(wanted, accepted, metrics, &result)
		if !decision.Correct {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: semantic decision for %s accepted=%t, want %t", benchmarkCase.ID, key, accepted, wanted))
		}
		result.Decisions = append(result.Decisions, decision)
	}
	for key := range expected {
		if _, exists := actual[key]; exists {
			continue
		}
		objectiveID, path, _ := strings.Cut(key, "\x00")
		metrics.FalseNegativeCandidates++
		result.FalseNegativeCandidates++
		result.Failures = append(result.Failures, fmt.Sprintf("%s: deterministic stage omitted labelled candidate %s", benchmarkCase.ID, key))
		result.Decisions = append(result.Decisions, semanticBenchmarkDecision{ObjectiveID: objectiveID, Path: path, ExpectedCandidate: true, Correct: false})
	}
	sort.Slice(result.Decisions, func(left, right int) bool {
		if result.Decisions[left].ObjectiveID != result.Decisions[right].ObjectiveID {
			return result.Decisions[left].ObjectiveID < result.Decisions[right].ObjectiveID
		}
		return result.Decisions[left].Path < result.Decisions[right].Path
	})
	return result
}

func scoreSemanticDecision(wanted, accepted bool, metrics *semanticBenchmarkMetrics, result *semanticBenchmarkCase) {
	switch {
	case wanted && accepted:
		metrics.TruePositiveCandidates++
	case wanted && !accepted:
		metrics.FalseNegativeCandidates++
		result.FalseNegativeCandidates++
	case !wanted && accepted:
		metrics.FalsePositiveCandidates++
		result.FalsePositiveCandidates++
	default:
		metrics.TrueNegativeCandidates++
	}
}

func semanticCandidateKey(objectiveID, path string) string {
	return objectiveID + "\x00" + path
}

func (metrics *semanticBenchmarkMetrics) finish() {
	metrics.CandidatePrecision = ratio(metrics.TruePositiveCandidates, metrics.TruePositiveCandidates+metrics.FalsePositiveCandidates)
	metrics.CandidateRecall = ratio(metrics.TruePositiveCandidates, metrics.TruePositiveCandidates+metrics.FalseNegativeCandidates)
	metrics.NegativeSpecificity = ratio(metrics.TrueNegativeCandidates, metrics.TrueNegativeCandidates+metrics.FalsePositiveCandidates)
	metrics.ReviewCoverage = ratio(metrics.ReviewedCandidates, metrics.InputCandidates)
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func (report *semanticBenchmarkReport) applyThresholds() {
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"candidate precision", report.Metrics.CandidatePrecision, report.Thresholds.CandidatePrecision},
		{"candidate recall", report.Metrics.CandidateRecall, report.Thresholds.CandidateRecall},
		{"negative specificity", report.Metrics.NegativeSpecificity, report.Thresholds.NegativeSpecificity},
		{"review coverage", report.Metrics.ReviewCoverage, report.Thresholds.ReviewCoverage},
	}
	for _, check := range checks {
		if check.got < check.want {
			report.Failures = append(report.Failures, fmt.Sprintf("semantic %s %.3f is below %.3f", check.name, check.got, check.want))
		}
	}
}

func writeSemanticBenchmarkSummary(writer io.Writer, report semanticBenchmarkReport) error {
	status := "FAIL"
	if report.Passed {
		status = "PASS"
	}
	if _, err := fmt.Fprintf(writer, "\n%s semantic technical-evidence benchmark (%s)\n", status, report.Model); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Candidates after review: precision %.1f%%, recall %.1f%% (tp=%d fp=%d tn=%d fn=%d)\n", report.Metrics.CandidatePrecision*100, report.Metrics.CandidateRecall*100, report.Metrics.TruePositiveCandidates, report.Metrics.FalsePositiveCandidates, report.Metrics.TrueNegativeCandidates, report.Metrics.FalseNegativeCandidates); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Review: coverage %.1f%%, negative specificity %.1f%%, strengths=%v\n", report.Metrics.ReviewCoverage*100, report.Metrics.NegativeSpecificity*100, report.Metrics.Strengths); err != nil {
		return err
	}
	for _, failure := range report.Failures {
		if _, err := fmt.Fprintln(writer, "-", failure); err != nil {
			return err
		}
	}
	return nil
}
