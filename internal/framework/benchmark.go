package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

const TechnicalBenchmarkSchemaVersion = 1

// BenchmarkManifest is a versioned set of human-labelled technical-evidence
// expectations. Repository paths are resolved relative to the manifest.
type BenchmarkManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Name          string              `json:"name"`
	PackID        string              `json:"pack_id"`
	PackVersion   string              `json:"pack_version"`
	Thresholds    BenchmarkThresholds `json:"thresholds"`
	Cases         []BenchmarkCase     `json:"cases"`
}

type BenchmarkThresholds struct {
	CandidatePrecision        float64 `json:"candidate_precision"`
	CandidateRecall           float64 `json:"candidate_recall"`
	AnchorAccuracy            float64 `json:"anchor_accuracy"`
	ReachabilityAccuracy      float64 `json:"reachability_accuracy"`
	RelationshipRecall        float64 `json:"relationship_recall"`
	LanguageCoverage          float64 `json:"language_coverage"`
	MaxForbiddenRelationships int     `json:"max_forbidden_relationships"`
}

type BenchmarkCase struct {
	ID                 string               `json:"id"`
	Path               string               `json:"path"`
	Languages          []codegraph.Language `json:"languages"`
	ExpectedCandidates []BenchmarkCandidate `json:"expected_candidates"`
}

type BenchmarkCandidate struct {
	ObjectiveID            string                  `json:"objective_id"`
	Path                   string                  `json:"path"`
	Anchor                 string                  `json:"anchor,omitempty"`
	Reachability           codegraph.Reachability  `json:"reachability,omitempty"`
	Relationships          []BenchmarkRelationship `json:"relationships,omitempty"`
	ForbiddenRelationships []BenchmarkRelationship `json:"forbidden_relationships,omitempty"`
}

type BenchmarkRelationship struct {
	Kind     codegraph.EdgeKind `json:"kind"`
	From     string             `json:"from,omitempty"`
	To       string             `json:"to,omitempty"`
	Label    string             `json:"label,omitempty"`
	Resolved *bool              `json:"resolved,omitempty"`
}

type BenchmarkReport struct {
	SchemaVersion int                   `json:"schema_version"`
	ManifestName  string                `json:"manifest_name"`
	Pack          PackReference         `json:"pack"`
	Thresholds    BenchmarkThresholds   `json:"thresholds"`
	Metrics       BenchmarkMetrics      `json:"metrics"`
	Cases         []BenchmarkCaseResult `json:"cases"`
	Passed        bool                  `json:"passed"`
	Failures      []string              `json:"failures,omitempty"`
}

type BenchmarkMetrics struct {
	TruePositiveCandidates      int     `json:"true_positive_candidates"`
	FalsePositiveCandidates     int     `json:"false_positive_candidates"`
	FalseNegativeCandidates     int     `json:"false_negative_candidates"`
	CandidatePrecision          float64 `json:"candidate_precision"`
	CandidateRecall             float64 `json:"candidate_recall"`
	CorrectAnchors              int     `json:"correct_anchors"`
	ExpectedAnchors             int     `json:"expected_anchors"`
	AnchorAccuracy              float64 `json:"anchor_accuracy"`
	CorrectReachability         int     `json:"correct_reachability"`
	ExpectedReachability        int     `json:"expected_reachability"`
	ReachabilityAccuracy        float64 `json:"reachability_accuracy"`
	MatchedRelationships        int     `json:"matched_relationships"`
	ExpectedRelationships       int     `json:"expected_relationships"`
	RelationshipRecall          float64 `json:"relationship_recall"`
	ForbiddenRelationshipsFound int     `json:"forbidden_relationships_found"`
	DetectedLanguages           int     `json:"detected_languages"`
	ExpectedLanguages           int     `json:"expected_languages"`
	LanguageCoverage            float64 `json:"language_coverage"`
}

type BenchmarkCaseResult struct {
	ID                      string   `json:"id"`
	Path                    string   `json:"path"`
	ExpectedCandidates      int      `json:"expected_candidates"`
	ActualCandidates        int      `json:"actual_candidates"`
	FalsePositiveCandidates int      `json:"false_positive_candidates"`
	FalseNegativeCandidates int      `json:"false_negative_candidates"`
	Failures                []string `json:"failures,omitempty"`
}

// LoadBenchmarkManifest parses a strict benchmark manifest.
func LoadBenchmarkManifest(path string) (BenchmarkManifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return BenchmarkManifest{}, fmt.Errorf("open technical benchmark manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest BenchmarkManifest
	if err := decoder.Decode(&manifest); err != nil {
		return BenchmarkManifest{}, fmt.Errorf("parse technical benchmark manifest: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return BenchmarkManifest{}, err
	}
	if err := manifest.Validate(); err != nil {
		return BenchmarkManifest{}, fmt.Errorf("validate technical benchmark manifest: %w", err)
	}
	return manifest, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse technical benchmark manifest: multiple JSON values")
		}
		return fmt.Errorf("parse technical benchmark manifest: %w", err)
	}
	return nil
}

func (manifest BenchmarkManifest) Validate() error {
	if manifest.SchemaVersion != TechnicalBenchmarkSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.Name) == "" || strings.TrimSpace(manifest.PackID) == "" || strings.TrimSpace(manifest.PackVersion) == "" {
		return fmt.Errorf("name, pack_id, and pack_version must not be empty")
	}
	for name, value := range map[string]float64{
		"candidate_precision":   manifest.Thresholds.CandidatePrecision,
		"candidate_recall":      manifest.Thresholds.CandidateRecall,
		"anchor_accuracy":       manifest.Thresholds.AnchorAccuracy,
		"reachability_accuracy": manifest.Thresholds.ReachabilityAccuracy,
		"relationship_recall":   manifest.Thresholds.RelationshipRecall,
		"language_coverage":     manifest.Thresholds.LanguageCoverage,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("threshold %s must be between 0 and 1", name)
		}
	}
	if manifest.Thresholds.MaxForbiddenRelationships < 0 {
		return fmt.Errorf("threshold max_forbidden_relationships must not be negative")
	}
	if len(manifest.Cases) == 0 {
		return fmt.Errorf("cases must not be empty")
	}
	caseIDs := make(map[string]struct{}, len(manifest.Cases))
	for caseIndex, benchmarkCase := range manifest.Cases {
		if strings.TrimSpace(benchmarkCase.ID) == "" {
			return fmt.Errorf("cases[%d].id must not be empty", caseIndex)
		}
		if _, exists := caseIDs[benchmarkCase.ID]; exists {
			return fmt.Errorf("cases[%d].id %q is duplicated", caseIndex, benchmarkCase.ID)
		}
		caseIDs[benchmarkCase.ID] = struct{}{}
		if err := validateBenchmarkPath(benchmarkCase.Path); err != nil {
			return fmt.Errorf("cases[%d].path: %w", caseIndex, err)
		}
		if len(benchmarkCase.Languages) == 0 {
			return fmt.Errorf("cases[%d].languages must not be empty", caseIndex)
		}
		languages := make(map[codegraph.Language]struct{}, len(benchmarkCase.Languages))
		for languageIndex, language := range benchmarkCase.Languages {
			if !supportedBenchmarkLanguage(language) {
				return fmt.Errorf("cases[%d].languages[%d] %q is not supported", caseIndex, languageIndex, language)
			}
			if _, exists := languages[language]; exists {
				return fmt.Errorf("cases[%d].languages[%d] %q is duplicated", caseIndex, languageIndex, language)
			}
			languages[language] = struct{}{}
		}
		candidateKeys := make(map[string]struct{}, len(benchmarkCase.ExpectedCandidates))
		for candidateIndex, candidate := range benchmarkCase.ExpectedCandidates {
			if strings.TrimSpace(candidate.ObjectiveID) == "" {
				return fmt.Errorf("cases[%d].expected_candidates[%d].objective_id must not be empty", caseIndex, candidateIndex)
			}
			if err := validateBenchmarkPath(candidate.Path); err != nil {
				return fmt.Errorf("cases[%d].expected_candidates[%d].path: %w", caseIndex, candidateIndex, err)
			}
			key := benchmarkCandidateKey(candidate.ObjectiveID, candidate.Path)
			if _, exists := candidateKeys[key]; exists {
				return fmt.Errorf("cases[%d] contains duplicate candidate %s", caseIndex, key)
			}
			candidateKeys[key] = struct{}{}
			if candidate.Reachability != "" && candidate.Anchor == "" {
				return fmt.Errorf("cases[%d].expected_candidates[%d] cannot declare reachability without an anchor", caseIndex, candidateIndex)
			}
			if candidate.Reachability != "" && !supportedBenchmarkReachability(candidate.Reachability) {
				return fmt.Errorf("cases[%d].expected_candidates[%d].reachability %q is not supported", caseIndex, candidateIndex, candidate.Reachability)
			}
			for relationshipIndex, relationship := range append(append([]BenchmarkRelationship(nil), candidate.Relationships...), candidate.ForbiddenRelationships...) {
				if !supportedBenchmarkEdgeKind(relationship.Kind) {
					return fmt.Errorf("cases[%d].expected_candidates[%d].relationships[%d].kind %q is not supported", caseIndex, candidateIndex, relationshipIndex, relationship.Kind)
				}
			}
		}
	}
	return nil
}

func validateBenchmarkPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.Contains(path, "\\") {
		return fmt.Errorf("must use slash-separated paths")
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("must be a relative path within the benchmark directory")
	}
	return nil
}

func supportedBenchmarkLanguage(language codegraph.Language) bool {
	switch language {
	case codegraph.LanguageGo, codegraph.LanguagePython, codegraph.LanguageJavaScript, codegraph.LanguageTypeScript:
		return true
	default:
		return false
	}
}

func supportedBenchmarkReachability(reachability codegraph.Reachability) bool {
	switch reachability {
	case codegraph.ReachableProduction, codegraph.ReachableExported, codegraph.ReachableTestOnly, codegraph.ReachableUnknown:
		return true
	default:
		return false
	}
}

func supportedBenchmarkEdgeKind(kind codegraph.EdgeKind) bool {
	switch kind {
	case codegraph.EdgeCall, codegraph.EdgeRoute, codegraph.EdgeTest, codegraph.EdgeAuthorization, codegraph.EdgePersistence, codegraph.EdgeLogging, codegraph.EdgeConfiguration:
		return true
	default:
		return false
	}
}

// RunBenchmark evaluates every labelled repository against a built-in pack.
func RunBenchmark(ctx context.Context, manifestPath string, manifest BenchmarkManifest, pack Pack) (BenchmarkReport, error) {
	return RunBenchmarkInWorkspace(ctx, filepath.Dir(manifestPath), manifest, pack)
}

// RunBenchmarkInWorkspace evaluates a manifest whose case paths live under an
// explicit workspace. It allows a source-free manifest to be checked against
// separately downloaded repositories without writing those repositories into
// the manifest directory.
func RunBenchmarkInWorkspace(ctx context.Context, workspace string, manifest BenchmarkManifest, pack Pack) (BenchmarkReport, error) {
	if err := manifest.Validate(); err != nil {
		return BenchmarkReport{}, fmt.Errorf("validate technical benchmark manifest: %w", err)
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return BenchmarkReport{}, fmt.Errorf("resolve technical benchmark workspace: %w", err)
	}
	if manifest.PackID != pack.ID || manifest.PackVersion != pack.Version {
		return BenchmarkReport{}, fmt.Errorf("benchmark requires pack %s@%s, got %s@%s", manifest.PackID, manifest.PackVersion, pack.ID, pack.Version)
	}
	objectiveIDs := make(map[string]struct{}, len(pack.Objectives))
	for _, objective := range pack.Objectives {
		objectiveIDs[objective.ID] = struct{}{}
	}
	report := BenchmarkReport{
		SchemaVersion: TechnicalBenchmarkSchemaVersion,
		ManifestName:  manifest.Name,
		Pack:          PackReference{ID: pack.ID, Name: pack.Name, Version: pack.Version, Released: pack.Released, Digest: pack.Digest},
		Thresholds:    manifest.Thresholds,
		Cases:         make([]BenchmarkCaseResult, 0, len(manifest.Cases)),
	}
	for _, benchmarkCase := range manifest.Cases {
		for _, expected := range benchmarkCase.ExpectedCandidates {
			if _, ok := objectiveIDs[expected.ObjectiveID]; !ok {
				return BenchmarkReport{}, fmt.Errorf("case %s references unknown objective %s", benchmarkCase.ID, expected.ObjectiveID)
			}
		}
		discovered, err := discovery.Discover(ctx, filepath.Join(workspace, filepath.FromSlash(benchmarkCase.Path)), discovery.Options{})
		if err != nil {
			return BenchmarkReport{}, fmt.Errorf("discover benchmark case %s: %w", benchmarkCase.ID, err)
		}
		caseResult := evaluateBenchmarkCase(benchmarkCase, Evaluate(pack, nil, discovered.Repository), &report.Metrics)
		report.Cases = append(report.Cases, caseResult)
	}
	report.Metrics.finish()
	report.applyThresholds()
	report.Passed = len(report.Failures) == 0
	return report, nil
}

func evaluateBenchmarkCase(benchmarkCase BenchmarkCase, evidence TechnicalEvidenceReport, metrics *BenchmarkMetrics) BenchmarkCaseResult {
	result := BenchmarkCaseResult{ID: benchmarkCase.ID, Path: benchmarkCase.Path, ExpectedCandidates: len(benchmarkCase.ExpectedCandidates)}
	actual := make(map[string]EvidenceMatch)
	for _, objective := range evidence.Objectives {
		for _, match := range objective.Matches {
			actual[benchmarkCandidateKey(objective.ID, match.Path)] = match
		}
	}
	result.ActualCandidates = len(actual)
	expected := make(map[string]BenchmarkCandidate, len(benchmarkCase.ExpectedCandidates))
	for _, candidate := range benchmarkCase.ExpectedCandidates {
		expected[benchmarkCandidateKey(candidate.ObjectiveID, candidate.Path)] = candidate
	}
	for key, wanted := range expected {
		match, ok := actual[key]
		if !ok {
			metrics.FalseNegativeCandidates++
			result.FalseNegativeCandidates++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: missing candidate %s", benchmarkCase.ID, key))
			continue
		}
		metrics.TruePositiveCandidates++
		compareBenchmarkContext(benchmarkCase.ID, wanted, match, metrics, &result)
	}
	for key := range actual {
		if _, ok := expected[key]; ok {
			continue
		}
		metrics.FalsePositiveCandidates++
		result.FalsePositiveCandidates++
		result.Failures = append(result.Failures, fmt.Sprintf("%s: unexpected candidate %s", benchmarkCase.ID, key))
	}
	detectedLanguages := make(map[codegraph.Language]struct{}, len(evidence.Analysis.Languages))
	for _, language := range evidence.Analysis.Languages {
		detectedLanguages[language] = struct{}{}
	}
	for _, language := range benchmarkCase.Languages {
		metrics.ExpectedLanguages++
		if _, ok := detectedLanguages[language]; ok {
			metrics.DetectedLanguages++
		} else {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: expected indexed language %s", benchmarkCase.ID, language))
		}
	}
	sort.Strings(result.Failures)
	return result
}

func compareBenchmarkContext(caseID string, wanted BenchmarkCandidate, match EvidenceMatch, metrics *BenchmarkMetrics, result *BenchmarkCaseResult) {
	if wanted.Anchor != "" {
		metrics.ExpectedAnchors++
		if match.Context.Anchor != nil && match.Context.Anchor.QualifiedName == wanted.Anchor {
			metrics.CorrectAnchors++
		} else {
			actual := "<none>"
			if match.Context.Anchor != nil {
				actual = match.Context.Anchor.QualifiedName
			}
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %s|%s anchor=%s, want %s", caseID, wanted.ObjectiveID, wanted.Path, actual, wanted.Anchor))
		}
	}
	if wanted.Reachability != "" {
		metrics.ExpectedReachability++
		if match.Context.Anchor != nil && match.Context.Anchor.Reachability == wanted.Reachability {
			metrics.CorrectReachability++
		} else {
			actual := codegraph.Reachability("")
			if match.Context.Anchor != nil {
				actual = match.Context.Anchor.Reachability
			}
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %s|%s reachability=%s, want %s", caseID, wanted.ObjectiveID, wanted.Path, actual, wanted.Reachability))
		}
	}
	for _, relationship := range wanted.Relationships {
		metrics.ExpectedRelationships++
		if benchmarkHasRelationship(match.Context.Relationships, relationship) {
			metrics.MatchedRelationships++
		} else {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %s|%s missing relationship %s", caseID, wanted.ObjectiveID, wanted.Path, formatBenchmarkRelationship(relationship)))
		}
	}
	for _, relationship := range wanted.ForbiddenRelationships {
		if benchmarkHasRelationship(match.Context.Relationships, relationship) {
			metrics.ForbiddenRelationshipsFound++
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %s|%s found forbidden relationship %s", caseID, wanted.ObjectiveID, wanted.Path, formatBenchmarkRelationship(relationship)))
		}
	}
}

func benchmarkHasRelationship(actual []codegraph.Relationship, wanted BenchmarkRelationship) bool {
	for _, relationship := range actual {
		if relationship.Kind != wanted.Kind || wanted.From != "" && relationship.From != wanted.From || wanted.To != "" && relationship.To != wanted.To || wanted.Label != "" && relationship.Label != wanted.Label {
			continue
		}
		if wanted.Resolved != nil && relationship.Resolved != *wanted.Resolved {
			continue
		}
		return true
	}
	return false
}

func formatBenchmarkRelationship(relationship BenchmarkRelationship) string {
	parts := []string{string(relationship.Kind)}
	if relationship.Label != "" {
		parts = append(parts, "label="+relationship.Label)
	}
	if relationship.From != "" {
		parts = append(parts, "from="+relationship.From)
	}
	if relationship.To != "" {
		parts = append(parts, "to="+relationship.To)
	}
	if relationship.Resolved != nil {
		parts = append(parts, fmt.Sprintf("resolved=%t", *relationship.Resolved))
	}
	return strings.Join(parts, ",")
}

func benchmarkCandidateKey(objectiveID, path string) string {
	return objectiveID + "|" + filepath.ToSlash(path)
}

func benchmarkRatio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func (metrics *BenchmarkMetrics) finish() {
	metrics.CandidatePrecision = benchmarkRatio(metrics.TruePositiveCandidates, metrics.TruePositiveCandidates+metrics.FalsePositiveCandidates)
	metrics.CandidateRecall = benchmarkRatio(metrics.TruePositiveCandidates, metrics.TruePositiveCandidates+metrics.FalseNegativeCandidates)
	metrics.AnchorAccuracy = benchmarkRatio(metrics.CorrectAnchors, metrics.ExpectedAnchors)
	metrics.ReachabilityAccuracy = benchmarkRatio(metrics.CorrectReachability, metrics.ExpectedReachability)
	metrics.RelationshipRecall = benchmarkRatio(metrics.MatchedRelationships, metrics.ExpectedRelationships)
	metrics.LanguageCoverage = benchmarkRatio(metrics.DetectedLanguages, metrics.ExpectedLanguages)
}

func (report *BenchmarkReport) applyThresholds() {
	checks := []struct {
		name string
		got  float64
		want float64
	}{
		{"candidate precision", report.Metrics.CandidatePrecision, report.Thresholds.CandidatePrecision},
		{"candidate recall", report.Metrics.CandidateRecall, report.Thresholds.CandidateRecall},
		{"anchor accuracy", report.Metrics.AnchorAccuracy, report.Thresholds.AnchorAccuracy},
		{"reachability accuracy", report.Metrics.ReachabilityAccuracy, report.Thresholds.ReachabilityAccuracy},
		{"relationship recall", report.Metrics.RelationshipRecall, report.Thresholds.RelationshipRecall},
		{"language coverage", report.Metrics.LanguageCoverage, report.Thresholds.LanguageCoverage},
	}
	for _, check := range checks {
		if check.got < check.want {
			report.Failures = append(report.Failures, fmt.Sprintf("%s %.3f is below %.3f", check.name, check.got, check.want))
		}
	}
	if report.Metrics.ForbiddenRelationshipsFound > report.Thresholds.MaxForbiddenRelationships {
		report.Failures = append(report.Failures, fmt.Sprintf("forbidden relationships %d exceeds %d", report.Metrics.ForbiddenRelationshipsFound, report.Thresholds.MaxForbiddenRelationships))
	}
	sort.Strings(report.Failures)
}

// WriteBenchmarkSummary renders a concise maintainer-facing benchmark result.
func WriteBenchmarkSummary(writer io.Writer, report BenchmarkReport) error {
	status := "PASS"
	if !report.Passed {
		status = "FAIL"
	}
	if _, err := fmt.Fprintf(writer, "%s technical evidence benchmark: %s (%s@%s)\n", status, report.ManifestName, report.Pack.ID, report.Pack.Version); err != nil {
		return err
	}
	metrics := report.Metrics
	if _, err := fmt.Fprintf(writer, "Candidates: precision %.1f%%, recall %.1f%% (tp=%d fp=%d fn=%d)\n", metrics.CandidatePrecision*100, metrics.CandidateRecall*100, metrics.TruePositiveCandidates, metrics.FalsePositiveCandidates, metrics.FalseNegativeCandidates); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Context: anchors %.1f%%, reachability %.1f%%, relationships %.1f%%, languages %.1f%%, forbidden=%d\n", metrics.AnchorAccuracy*100, metrics.ReachabilityAccuracy*100, metrics.RelationshipRecall*100, metrics.LanguageCoverage*100, metrics.ForbiddenRelationshipsFound); err != nil {
		return err
	}
	for _, failure := range report.Failures {
		if _, err := fmt.Fprintf(writer, "- %s\n", failure); err != nil {
			return err
		}
	}
	return nil
}
