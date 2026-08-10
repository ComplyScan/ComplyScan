// complyscan:ignore-technical-evidence -- this file embeds synthetic technical-objective fixtures.
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/technicalreview"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

var testBuild = BuildInfo{Version: "0.1.0", Commit: "test", BuildDate: "today"}

func TestScanExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "no threshold findings", target: "non-ai-repository", want: 0},
		{name: "high threshold finding", target: "vulnerable-python-ai-app", want: 1},
		{name: "scan error", target: "does-not-exist", want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			target := filepath.Join("..", "..", "testdata", test.target)
			if got := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); got != test.want {
				t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", got, test.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestScanModesControlConfiguredAIReview(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	cfg.AI.Ollama.Endpoint = "http://127.0.0.1:1"
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--quick", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("quick scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("default scan unexpectedly invoked configured AI: code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}

	cfg.AI.Provider = "none"
	if err := config.Write(filepath.Join(target, config.FileName), cfg, true); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--deep", "--no-report", target}, &stdout, &stderr, testBuild); code != 2 || !strings.Contains(stderr.String(), "--deep requires an AI review provider") {
		t.Fatalf("deep scan code=%d stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--quick", "--deep", "--no-report", target}, &stdout, &stderr, testBuild); code != 2 || !strings.Contains(stderr.String(), "cannot be used together") {
		t.Fatalf("conflicting scan modes code=%d stderr=%q", code, stderr.String())
	}
}

func TestDeepScanSavesPreliminaryReportAndSurvivesProviderFailure(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("import openai\nlogger.info(model_output)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	cfg.AI.Ollama.Endpoint = "http://127.0.0.1:1"
	cfg.Systems = []profile.System{profile.NewDraftSystem("example", "Example")}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--deep", "--no-color", target}, &stdout, &stderr, testBuild)
	if code != 0 && code != 1 {
		t.Fatalf("deep scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Preliminary report saved before AI review") || !strings.Contains(stdout.String(), "review was incomplete") {
		t.Fatalf("deep scan did not preserve and explain partial results:\n%s", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(target, report.DefaultDirectory, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded report.Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Findings) == 0 || !strings.Contains(strings.Join(decoded.Warnings, "\n"), "review was incomplete") {
		t.Fatalf("saved report lost deterministic or failure information: %#v", decoded)
	}
}

func TestRootCommandScansConfiguredCurrentRepository(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(current) })

	var stdout, stderr bytes.Buffer
	if code := executeWithInput(nil, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("default command code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "ComplyScan scanning .") || !strings.Contains(stdout.String(), "Reports saved:") {
		t.Fatalf("default command did not scan configured repository:\n%s", stdout.String())
	}
}

func TestValidateVerificationPlansRejectsUnknownAndDuplicateIDs(t *testing.T) {
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{ID: "known"}}}
	base := verification.Options{RecipeID: "tests", Objectives: []string{"known"}}
	if _, err := validateVerificationPlans([]verification.Options{base}, evidence, nil); err != nil {
		t.Fatal(err)
	}
	unknown := base
	unknown.Objectives = []string{"unknown"}
	if _, err := validateVerificationPlans([]verification.Options{unknown}, evidence, nil); err == nil || !strings.Contains(err.Error(), "unknown objective") {
		t.Fatalf("unexpected unknown-objective error: %v", err)
	}
	duplicate := base
	duplicate.Objectives = []string{"known", "known"}
	if _, err := validateVerificationPlans([]verification.Options{duplicate}, evidence, nil); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected duplicate-objective error: %v", err)
	}
}

func TestValidateVerificationPlansInfersOneSystemButRequiresMultipleSystemOwnership(t *testing.T) {
	evidence := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{ID: "objective"}}}
	plan := verification.Options{RecipeID: "tests", Objectives: []string{"objective"}}
	validated, err := validateVerificationPlans([]verification.Options{plan}, evidence, []profile.System{{ID: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated[0].Systems) != 1 || validated[0].Systems[0] != "one" {
		t.Fatalf("single system was not inferred: %#v", validated)
	}
	_, err = validateVerificationPlans([]verification.Options{plan}, evidence, []profile.System{{ID: "one"}, {ID: "two"}})
	if err == nil || !strings.Contains(err.Error(), "must declare systems") {
		t.Fatalf("unexpected multi-system error: %v", err)
	}
}

func TestScanJSONOutputAndSeverityFilter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--no-report", "--format", "json", "--severity", "high", target}, &stdout, &stderr, testBuild)
	if code != 1 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Summary.High == 0 || decoded.Summary.Medium != 0 || decoded.Summary.Info != 0 {
		t.Fatalf("severity filter not reflected in summary: %#v", decoded.Summary)
	}
	if decoded.SchemaVersion != 5 || decoded.Tool.Commit != "test" || decoded.Scan.ID == "" || decoded.Scan.Scope.Findings != "full-repository" || decoded.Scan.Scope.TechnicalEvidence != "full-repository" {
		t.Fatalf("missing evidence-bundle metadata: %#v", decoded)
	}
}

func TestScanSARIFOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--no-report", "--format", "sarif", "--severity", "high", target}, &stdout, &stderr, testBuild)
	if code != 1 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid SARIF JSON: %v\n%s", err, stdout.String())
	}
	if decoded["version"] != "2.1.0" || !strings.Contains(stdout.String(), "complyscanFingerprint/v1") {
		t.Fatalf("unexpected SARIF output:\n%s", stdout.String())
	}
}

func TestTerminalScanStreamsFindingsBeforeCompletion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--no-report", "--no-color", "--severity", "high", target}, &stdout, &stderr, testBuild)
	if code != 1 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	scanningAt := strings.Index(output, "ComplyScan scanning")
	findingAt := strings.Index(output, "AI-LOG-001")
	completeAt := strings.Index(output, "Scan complete:")
	if scanningAt < 0 || findingAt < 0 || completeAt < 0 {
		t.Fatalf("streaming output is incomplete:\n%s", output)
	}
	if !(scanningAt < findingAt && findingAt < completeAt) {
		t.Fatalf("unexpected streaming output order:\n%s", output)
	}
	if strings.Contains(output, "AI-DISC-001") {
		t.Fatalf("severity filter was not applied to streamed output:\n%s", output)
	}
}

func TestScanSupportsAdditionalExcludes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--no-report", "--no-color", "--exclude", "app.py", target}, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "AI-LOG-001") || strings.Contains(stdout.String(), "AI-SEC-001") {
		t.Fatalf("excluded file produced a finding:\n%s", stdout.String())
	}
}

func TestScanRejectsInvalidBudgets(t *testing.T) {
	for _, args := range [][]string{{"scan", "--max-files", "0"}, {"scan", "--max-total-bytes", "0"}} {
		var stdout, stderr bytes.Buffer
		if code := Execute(args, &stdout, &stderr, testBuild); code != 2 {
			t.Fatalf("Execute(%v) code = %d, want 2", args, code)
		}
	}
}

func TestScanAutomaticallySavesHumanAndMachineReports(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "override.go"), []byte("package review\nfunc OverrideDecision(output string) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for id := range cfg.Rules {
		cfg.Rules[id] = config.RuleConfig{Enabled: false}
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-color", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	reportDirectory := filepath.Join(target, ".complyscan", "reports")
	markdownPath := filepath.Join(reportDirectory, "latest.md")
	jsonPath := filepath.Join(reportDirectory, "latest.json")
	for _, path := range []string{markdownPath, jsonPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("report %q was not written: %v", path, err)
		}
	}
	if !strings.Contains(stdout.String(), "Reports saved:") || !strings.Contains(stdout.String(), markdownPath) {
		t.Fatalf("terminal did not identify saved reports:\n%s", stdout.String())
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded report.Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TechnicalEvidence == nil || decoded.TechnicalEvidence.Summary.CandidateEvidence == 0 {
		t.Fatalf("saved bundle lacks technical evidence: %#v", decoded.TechnicalEvidence)
	}
	for _, objective := range decoded.TechnicalEvidence.Objectives {
		for _, match := range objective.Matches {
			if strings.HasPrefix(match.Path, ".complyscan/reports/") {
				t.Fatalf("generated report was scanned as evidence: %#v", match)
			}
		}
	}
}

func TestScanReportFlagsAreSafeAndOptional(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("no-report exit code = %d; stderr=%q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, ".complyscan")); !os.IsNotExist(err) {
		t.Fatalf("--no-report created report directory: %v", err)
	}
	for _, arguments := range [][]string{
		{"scan", "--no-report", "--report-dir", "reports", target},
		{"scan", "--report-dir", "../outside", target},
		{"scan", "--report-dir", filepath.Join(string(filepath.Separator), "tmp", "reports"), target},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := Execute(arguments, &stdout, &stderr, testBuild); code != 2 {
			t.Fatalf("Execute(%v) code=%d stderr=%q", arguments, code, stderr.String())
		}
	}
}

func TestScanChangedSinceRequiresGitRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--no-report", "--changed-since", "main", t.TempDir()}, &stdout, &stderr, testBuild)
	if code != 2 || !strings.Contains(stderr.String(), "locate Git repository") {
		t.Fatalf("exit code = %d; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "governance remains repository-wide") {
		t.Fatalf("changed-scan scope was not explained:\n%s", stdout.String())
	}
}

func TestScanCanEnableOllamaWithoutCallingItForClearRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "non-ai-repository")
	code := Execute([]string{"scan", "--no-report", "--format", "json", "--review", "ollama", "--ollama-model", "test-model", target}, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Review == nil || decoded.Review.Provider != providers.Ollama || decoded.Review.Model != "test-model" || decoded.Review.InputFindings != 0 {
		t.Fatalf("unexpected advisory review: %#v", decoded.Review)
	}
	if decoded.TechnicalReview == nil || decoded.TechnicalReview.Provider != providers.Ollama || decoded.TechnicalReview.InputCandidates != 0 {
		t.Fatalf("unexpected technical review: %#v", decoded.TechnicalReview)
	}
	if decoded.Summary.Total != 0 {
		t.Fatalf("model review changed deterministic summary: %#v", decoded.Summary)
	}
}

func TestBuildTechnicalReviewRequestIncludesConnectedSourceWithoutChangingEvidence(t *testing.T) {
	pack := framework.Pack{Objectives: []framework.TechnicalObjective{{
		ID: "eu-aia-14-override-intervention", Title: "Override", SourceReference: "Article 14",
		Description: "An authorised person can override a decision.", FileKinds: []string{"source"},
		PathKeywords: []string{"override"}, KeywordGroups: [][]string{{"override"}, {"decision"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "override/main.go", Kind: discovery.KindSource,
		Content: []byte(`package main
func main() { overrideDecision() }
func overrideDecision() { authorizeReviewer() }
func authorizeReviewer() {}
`),
	}}}
	evidence := framework.Evaluate(pack, nil, repository)
	request := buildTechnicalReviewRequest(evidence, repository)
	if len(request.Candidates) != 1 {
		t.Fatalf("unexpected candidates: %#v", request)
	}
	candidate := request.Candidates[0]
	if candidate.EvidenceFingerprint != evidence.Objectives[0].Matches[0].Fingerprint || candidate.Anchor != "main.overrideDecision" || candidate.Reachability != "production-reachable" {
		t.Fatalf("candidate lost evidence binding: %#v", candidate)
	}
	if len(candidate.SourceContexts) < 2 {
		t.Fatalf("connected source context missing: %#v", candidate.SourceContexts)
	}
	joined := ""
	for _, source := range candidate.SourceContexts {
		joined += source.Source
	}
	if !strings.Contains(joined, "overrideDecision") || !strings.Contains(joined, "authorizeReviewer") {
		t.Fatalf("connected functions missing from source context: %s", joined)
	}
}

func TestBuildTechnicalReviewRequestIncludesConnectedPythonSource(t *testing.T) {
	pack := framework.Pack{Objectives: []framework.TechnicalObjective{{
		ID: "eu-aia-14-override-intervention", Title: "Override", SourceReference: "Article 14",
		Description: "An authorised person can override a decision.", FileKinds: []string{"source"},
		PathKeywords: []string{"override"}, KeywordGroups: [][]string{{"override"}, {"decision"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "override/api.py", Kind: discovery.KindSource,
			Content: []byte(`from fastapi import Depends
from override.auth import authorize_reviewer

@app.post("/override")
def override_decision(reviewer=Depends(authorize_reviewer)):
    update_decision()
`),
		},
		{
			Path: "override/auth.py", Kind: discovery.KindSource,
			Content: []byte(`def authorize_reviewer():
    return True
`),
		},
	}}
	evidence := framework.Evaluate(pack, nil, repository)
	request := buildTechnicalReviewRequest(evidence, repository)
	if len(request.Candidates) != 1 {
		t.Fatalf("unexpected Python candidates: %#v", request)
	}
	candidate := request.Candidates[0]
	if candidate.Anchor != "override.api.override_decision" || candidate.Reachability != "production-reachable" || len(candidate.SourceContexts) < 2 {
		t.Fatalf("Python candidate lost connected context: %#v", candidate)
	}
	joined := ""
	for _, source := range candidate.SourceContexts {
		joined += source.Source
	}
	if !strings.Contains(joined, `@app.post("/override")`) || !strings.Contains(joined, "authorize_reviewer") {
		t.Fatalf("Python route or authorization source missing: %s", joined)
	}
}

func TestBuildTechnicalReviewRequestIncludesConnectedTypeScriptSource(t *testing.T) {
	pack := framework.Pack{Objectives: []framework.TechnicalObjective{{
		ID: "eu-aia-14-override-intervention", Title: "Override", SourceReference: "Article 14",
		Description: "An authorised person can override a decision.", FileKinds: []string{"source"},
		PathKeywords: []string{"override"}, KeywordGroups: [][]string{{"override"}, {"decision"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "override/api.ts", Kind: discovery.KindSource,
			Content: []byte(`import { requireReviewer } from "./auth";

export function overrideDecision(): void {
  persistResult();
}

app.post("/override", requireReviewer, overrideDecision);
`),
		},
		{
			Path: "override/auth.ts", Kind: discovery.KindSource,
			Content: []byte(`export function requireReviewer(): boolean {
  return true;
}
`),
		},
	}}
	evidence := framework.Evaluate(pack, nil, repository)
	request := buildTechnicalReviewRequest(evidence, repository)
	if len(request.Candidates) != 1 {
		t.Fatalf("unexpected TypeScript candidates: %#v", request)
	}
	candidate := request.Candidates[0]
	if candidate.Anchor != "override.api.overrideDecision" || candidate.Reachability != "production-reachable" || len(candidate.SourceContexts) < 2 {
		t.Fatalf("TypeScript candidate lost connected context: %#v", candidate)
	}
	joined := ""
	for _, source := range candidate.SourceContexts {
		joined += source.Source
	}
	if !strings.Contains(joined, `app.post("/override"`) || !strings.Contains(joined, "requireReviewer") {
		t.Fatalf("TypeScript route or authorization source missing: %s", joined)
	}
}

func TestScanPreservesGraphCoverageWarnings(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "broken.go"), []byte("package broken\nfunc"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded.TechnicalEvidence == nil || len(decoded.TechnicalEvidence.Warnings) == 0 || !strings.Contains(decoded.TechnicalEvidence.Warnings[0], "broken.go") {
		t.Fatalf("graph parse warning was lost: %#v", decoded.TechnicalEvidence)
	}
}

func TestScanRejectsUnsafeOrInactiveOllamaOverrides(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	for _, arguments := range [][]string{
		{"scan", "--review", "openai"},
		{"scan", "--ollama-model", "gemma3"},
		{"scan", "--model", "gpt-test"},
		{"scan", "--api-key-env", "OPENAI_API_KEY"},
		{"scan", "--refresh-review"},
		{"scan", "--review", "ollama", "--ollama-endpoint", "https://example.com"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(arguments, &stdout, &stderr, testBuild); code != 2 {
			t.Fatalf("Execute(%v) code = %d, want 2; stderr=%q", arguments, code, stderr.String())
		}
	}
}

func TestConfiguredRemoteReviewerReadsOnlyNamedEnvironmentVariable(t *testing.T) {
	settings := config.Default().AI
	settings.Provider = "openai"
	settings.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_TEST_OPENAI_KEY", TimeoutSeconds: 30, MaxFindings: 3,
	}
	t.Setenv("COMPLYSCAN_TEST_OPENAI_KEY", "secret-in-process-only")
	provider, timeout, maximum, model, kind, err := configuredReviewer(settings)
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || timeout != 30*time.Second || maximum != 3 || model != "gpt-test" || kind != providers.OpenAI {
		t.Fatalf("configured reviewer = %#v, %s, %d, %q, %q", provider, timeout, maximum, model, kind)
	}

	settings.Remote.APIKeyEnv = "COMPLYSCAN_TEST_MISSING_KEY"
	_, _, _, _, _, err = configuredReviewer(settings)
	if err == nil || !strings.Contains(err.Error(), "COMPLYSCAN_TEST_MISSING_KEY is not set") {
		t.Fatalf("missing-key error = %v", err)
	}
}

func TestTechnicalReviewProgressDistinguishesModelAndCache(t *testing.T) {
	var output bytes.Buffer
	progress := technicalReviewProgress(&output)
	candidate := providers.TechnicalCandidate{SystemID: "ranking", RepositoryFiles: 42, ObjectiveID: "eu-aia-10-bias-evaluation", Path: "evaluation.go"}
	if err := progress(technicalreview.Progress{Current: 1, Total: 2, Candidate: candidate}); err != nil {
		t.Fatal(err)
	}
	if err := progress(technicalreview.Progress{Current: 2, Total: 2, Candidate: candidate, Cached: true}); err != nil {
		t.Fatal(err)
	}
	if value := output.String(); !strings.Contains(value, "1/2") || !strings.Contains(value, "reviewing with Ollama") || !strings.Contains(value, "2/2") || !strings.Contains(value, "using cached observation") || !strings.Contains(value, "system ranking, 42 owned file(s)") {
		t.Fatalf("unexpected progress output:\n%s", value)
	}
}

func TestScanAppliesReasonedSuppressions(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".complyscan.yml")
	content := `version: 1
fail-on: high
ai:
  provider: none
suppressions:
  - rule: AI-LOG-001
    reason: covered by fixture controls
  - rule: AI-SEC-001
    reason: unmistakably synthetic test credential
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--no-report", "--no-color", "--config", configPath, target}, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Suppressed:") || strings.Contains(stdout.String(), "AI-LOG-001") || strings.Contains(stdout.String(), "AI-SEC-001") {
		t.Fatalf("unexpected suppression output:\n%s", stdout.String())
	}
}

func TestBaselineAcceptsCurrentFindingsButNotNewOnes(t *testing.T) {
	target := t.TempDir()
	appPath := filepath.Join(target, "app.py")
	if err := os.WriteFile(appPath, []byte("import openai\nimport logging\nlogging.info(prompt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"baseline", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("baseline exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	baselinePath := filepath.Join(target, ".complyscan-baseline.json")
	if _, err := os.Stat(baselinePath); err != nil {
		t.Fatalf("baseline was not written: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("baselined scan exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Suppressed:") {
		t.Fatalf("baseline was not applied:\n%s", stdout.String())
	}

	if err := os.WriteFile(appPath, []byte("import openai\nimport logging\nlogging.info(prompt)\nlogging.info(response)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("changed scan exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "response") {
		t.Fatalf("new finding was not reported:\n%s", stdout.String())
	}
}

func TestVersionCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"version"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d: %s", code, stderr.String())
	}
	for _, want := range []string{"ComplyScan 0.1.0", "commit: test", "built: today"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("version output missing %q: %s", want, stdout.String())
		}
	}
}

func TestDoctorReportsOfflineRepositoryReadiness(t *testing.T) {
	target := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	for _, expected := range []string{
		"[PASS] version: ComplyScan 0.1.0",
		"[WARN] config: no .complyscan.yml found",
		"[PASS] reports: writable",
		"ollama executable",
		"[SKIP] ollama service: AI review is disabled",
		"Doctor found no blocking issues.",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("doctor output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestDoctorReviewProbeRequiresConfiguredProvider(t *testing.T) {
	target := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] review compatibility: no advisory review provider is configured") {
		t.Fatalf("probe output:\n%s", stdout.String())
	}
}

func TestDoctorChecksConfiguredOllamaModel(t *testing.T) {
	target := t.TempDir()
	useDoctorHTTPTransport(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/tags" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		return doctorHTTPResponse(http.StatusOK, `{"models":[{"name":"qwen3.5:9b","model":"qwen3.5:9b"}]}`), nil
	})

	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	commandDirectory := t.TempDir()
	ollamaPath := filepath.Join(commandDirectory, "ollama")
	if err := os.WriteFile(ollamaPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commandDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	for _, expected := range []string{"[PASS] ollama executable:", "[PASS] ollama service:", "[PASS] ollama model: qwen3.5:9b"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("doctor output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestDoctorFailsWhenConfiguredOllamaModelIsMissing(t *testing.T) {
	target := t.TempDir()
	useDoctorHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		return doctorHTTPResponse(http.StatusOK, `{"models":[]}`), nil
	})

	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	commandDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(commandDirectory, "ollama"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", commandDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] ollama model: qwen3.5:9b is not installed") {
		t.Fatalf("missing model was not reported:\n%s", stdout.String())
	}
}

func TestDoctorChecksRemoteCredentialWithoutPrintingValue(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.AI.Provider = "anthropic"
	cfg.AI.Remote = config.RemoteConfig{Model: "claude-sonnet-5", APIKeyEnv: "COMPLYSCAN_TEST_ANTHROPIC_KEY", TimeoutSeconds: 60, MaxFindings: 10}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	secret := "credential-must-stay-hidden"
	t.Setenv("COMPLYSCAN_TEST_ANTHROPIC_KEY", secret)
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] remote credential: COMPLYSCAN_TEST_ANTHROPIC_KEY is set (value hidden)") ||
		!strings.Contains(stdout.String(), "[PASS] remote model: claude-sonnet-5 via Anthropic") || strings.Contains(stdout.String(), secret) {
		t.Fatalf("remote doctor output:\n%s", stdout.String())
	}

	t.Setenv("COMPLYSCAN_TEST_ANTHROPIC_KEY", "")
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("missing credential exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] remote credential: COMPLYSCAN_TEST_ANTHROPIC_KEY is not set") {
		t.Fatalf("missing credential output:\n%s", stdout.String())
	}
}

func TestDoctorReportsInvalidConfiguration(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, config.FileName), []byte("version: 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] config:") || !strings.Contains(stdout.String(), "unsupported version 99") {
		t.Fatalf("invalid config was not reported:\n%s", stdout.String())
	}
}

type doctorRoundTripFunc func(*http.Request) (*http.Response, error)

func (function doctorRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func useDoctorHTTPTransport(t *testing.T, function doctorRoundTripFunc) {
	t.Helper()
	previous := doctorHTTPClient
	doctorHTTPClient = &http.Client{Transport: function}
	t.Cleanup(func() { doctorHTTPClient = previous })
}

func doctorHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestInventoryCommandWritesStructuredJSON(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "requirements.txt"), []byte("openai==1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"inventory", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded inventory.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid inventory JSON: %v\n%s", err, stdout.String())
	}
	if decoded.SchemaVersion != 1 || decoded.Summary.Components != 1 || decoded.Summary.Signals != 2 {
		t.Fatalf("unexpected inventory: %#v", decoded)
	}
	if decoded.Components[0].Name != "OpenAI" || len(decoded.Components[0].Locations) != 2 {
		t.Fatalf("unexpected component: %#v", decoded.Components)
	}
}

func TestInventoryCommandWritesTerminalOutput(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.js"), []byte("import Anthropic from '@anthropic-ai/sdk';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"inventory", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Inventory complete: 1 component") || !strings.Contains(stdout.String(), "Anthropic") {
		t.Fatalf("unexpected inventory output:\n%s", stdout.String())
	}
}

func TestGenerateCommandsCreateReviewableDocuments(t *testing.T) {
	for _, testCase := range []struct {
		command string
		path    string
		heading string
	}{
		{command: "ai-system", path: filepath.Join("docs", "AI_SYSTEM.md"), heading: "# AI system record"},
		{command: "risk-assessment", path: filepath.Join("docs", "risk-assessment.md"), heading: "# AI risk assessment"},
	} {
		t.Run(testCase.command, func(t *testing.T) {
			target := t.TempDir()
			if err := os.WriteFile(filepath.Join(target, "requirements.txt"), []byte("openai==1.2.3\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			args := []string{"generate", testCase.command, target}
			if code := Execute(args, &stdout, &stderr, testBuild); code != 0 {
				t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
			}
			data, err := os.ReadFile(filepath.Join(target, testCase.path))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), testCase.heading) || !strings.Contains(string(data), "OpenAI") || !strings.Contains(string(data), "human review required") {
				t.Fatalf("unexpected generated document:\n%s", data)
			}
			stdout.Reset()
			stderr.Reset()
			if code := Execute(args, &stdout, &stderr, testBuild); code != 2 || !strings.Contains(stderr.String(), "--force") {
				t.Fatalf("overwrite exit code = %d; stderr=%q", code, stderr.String())
			}
		})
	}
}

func TestTargetExclusionHandlesAbsolutePaths(t *testing.T) {
	target := t.TempDir()
	inside := filepath.Join(target, "state", ".complyscan-baseline.json")
	if got := targetExclusion(target, inside); got != "state/.complyscan-baseline.json" {
		t.Fatalf("targetExclusion() = %q", got)
	}
	if got := targetExclusion(target, filepath.Join(t.TempDir(), "baseline.json")); got != "" {
		t.Fatalf("outside exclusion = %q", got)
	}
}

func TestInitCommandProtectsExistingConfig(t *testing.T) {
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"init"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("first init exit code = %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"init"}, &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("second init exit code = %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"init", "--force"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("forced init exit code = %d: %s", code, stderr.String())
	}
}

func TestInteractiveInitCollectsAttributedSystemContext(t *testing.T) {
	target := t.TempDir()
	input := strings.Join([]string{
		"", "", "Candidate ranking", "Rank job applications for recruiter review.", "development", "provider", "eu,uk", "employment",
		"recruiters", "job applicants", "advisory", "required", "inference,automated-decision", "yes", "unknown", "no", "private-customer,api",
		"A. Reviewer", "applicable", "The system is offered to EU customers by its provider.", "",
	}, "\n") + "\n"
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"init", "--interactive", target}, strings.NewReader(input), &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 1 {
		t.Fatalf("systems = %#v", cfg.Systems)
	}
	system := cfg.Systems[0]
	if system.Name != "Candidate ranking" || system.UseCaseDomains[0] != profile.DomainEmployment || system.Data.PersonalData != profile.TriYes || len(system.AIActivities) != 2 {
		t.Fatalf("unexpected system profile: %#v", system)
	}
	if system.ProfileReview.Status != profile.ReviewConfirmed || system.ProfileReview.ReviewedBy != "A. Reviewer" {
		t.Fatalf("unexpected profile review: %#v", system.ProfileReview)
	}
	if len(system.Applicability) != 1 || system.Applicability[0].Status != profile.ApplicabilityApplicable || system.Applicability[0].Rationale == "" {
		t.Fatalf("unexpected applicability: %#v", system.Applicability)
	}
	for _, expected := range []string{"System applicability setup", "Human EU AI Act applicability decision", "1 system profile"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestNonInteractiveInitCreatesConfigWithoutInventingContext(t *testing.T) {
	target := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"init", "--non-interactive", target}, strings.NewReader("ignored"), &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 0 || !strings.Contains(stdout.String(), "no system profile was collected") {
		t.Fatalf("systems=%#v output=%q", cfg.Systems, stdout.String())
	}
	ignoreData, err := os.ReadFile(filepath.Join(target, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignoreData), reportGitIgnoreEntry) {
		t.Fatalf("generated reports are not ignored:\n%s", ignoreData)
	}
}

func TestInitPreservesExistingGitIgnoreWithoutDuplicatingReportEntry(t *testing.T) {
	target := t.TempDir()
	ignorePath := filepath.Join(target, ".gitignore")
	if err := os.WriteFile(ignorePath, []byte("node_modules/\n/.complyscan/reports/\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"init", "--non-interactive", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("init code=%d stderr=%q", code, stderr.String())
	}
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), ".complyscan/reports") != 1 || !strings.Contains(string(data), "node_modules/") {
		t.Fatalf("unexpected .gitignore:\n%s", data)
	}
	info, err := os.Stat(ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf(".gitignore permissions = %o", info.Mode().Perm())
	}
}

func TestInitRejectsConflictingInteractionFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"init", "--interactive", "--non-interactive"}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
}

func TestProfileSetupAddsContextToExistingConfig(t *testing.T) {
	target := t.TempDir()
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	input := strings.NewReader(strings.Repeat("\n", 17))
	code := executeWithInput([]string{"profile", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 1 || cfg.Systems[0].ProfileReview.Status != profile.ReviewDraft {
		t.Fatalf("unexpected systems: %#v", cfg.Systems)
	}
	if !strings.Contains(stdout.String(), "Provisional screening") || !strings.Contains(stdout.String(), "Added system profile") {
		t.Fatalf("unexpected output:\n%s", stdout.String())
	}
}

func TestProfileShowWritesStructuredApplicabilityJSON(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.Systems = []profile.System{profile.NewDraftSystem("example", "Example")}
	for id := range cfg.Rules {
		cfg.Rules[id] = config.RuleConfig{Enabled: false}
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"profile", "show", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded profile.AssessmentReport
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Systems) != 1 || decoded.Systems[0].AutomatedScope != profile.ScopeNeedsContext {
		t.Fatalf("unexpected assessment: %#v", decoded)
	}
}

func TestScanJSONIncludesApplicabilityWithoutChangingFindings(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Systems = []profile.System{profile.NewDraftSystem("example", "Example")}
	for id := range cfg.Rules {
		cfg.Rules[id] = config.RuleConfig{Enabled: false}
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Applicability == nil || len(decoded.Applicability.Systems) != 1 {
		t.Fatalf("missing applicability: %#v", decoded.Applicability)
	}
	if decoded.TechnicalEvidence == nil || len(decoded.TechnicalEvidence.Systems) != 1 || decoded.TechnicalEvidence.Summary.Total == 0 {
		t.Fatalf("missing technical evidence: %#v", decoded.TechnicalEvidence)
	}
	if decoded.AIInventory == nil || decoded.Reconciliation == nil || len(decoded.Reconciliation.Systems) != 1 {
		t.Fatalf("missing inventory or reconciliation: inventory=%#v reconciliation=%#v", decoded.AIInventory, decoded.Reconciliation)
	}
	if decoded.Summary.Total != 0 {
		t.Fatalf("applicability changed findings: %#v", decoded.Summary)
	}
	if len(decoded.Frameworks) != 1 || decoded.Frameworks[0].ID != profile.FrameworkEUAIAct {
		t.Fatalf("default framework result missing: %#v", decoded.Frameworks)
	}
}

func TestScanMapsSharedEvidenceAcrossEUAndNISTFrameworks(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "override.go"), []byte("package review\nfunc OverrideDecision(output string) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Frameworks = []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID}
	system := profile.NewDraftSystem("assistant", "Assistant")
	system.AIActivities = []profile.AIActivity{profile.ActivityInference}
	cfg.Systems = []profile.System{system}
	for id := range cfg.Rules {
		cfg.Rules[id] = config.RuleConfig{Enabled: false}
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 5 || len(decoded.Frameworks) != 2 {
		t.Fatalf("multi-framework contract missing: %#v", decoded.Frameworks)
	}
	var fingerprints []string
	for _, result := range decoded.Frameworks {
		for _, objective := range result.TechnicalEvidence.Objectives {
			if objective.ControlID == "human-override" && len(objective.Matches) == 1 {
				fingerprints = append(fingerprints, objective.Matches[0].Fingerprint)
			}
		}
	}
	if len(fingerprints) != 2 || fingerprints[0] != fingerprints[1] {
		t.Fatalf("shared control evidence was not reused: %#v", fingerprints)
	}
	if decoded.Frameworks[1].Reconciliation.Summary.Recommended == 0 || decoded.Frameworks[1].Reconciliation.Summary.LikelyRequired != 0 {
		t.Fatalf("NIST mapping was not kept voluntary: %#v", decoded.Frameworks[1].Reconciliation.Summary)
	}
}

func TestFrameworkListWritesVersionedCoverageJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"framework", "list", "--format", "json"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var listings []framework.PackListing
	if err := json.Unmarshal(stdout.Bytes(), &listings); err != nil {
		t.Fatal(err)
	}
	if len(listings) != 2 || listings[0].Pack.Version != "0.1.3" || listings[1].Pack.ID != framework.NISTAIRMFTechnicalEvidencePackID || listings[1].Coverage.Nature != framework.NatureVoluntaryFramework || len(listings[0].Coverage.Limitations) == 0 {
		t.Fatalf("unexpected listings: %#v", listings)
	}
}

func TestFrameworkAssessMapsFullRepositoryEvidence(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "override.go"), []byte("package review\nfunc OverrideDecision(output string) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Systems = []profile.System{cliCandidateProviderSystem()}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"framework", "assess", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded framework.TechnicalEvidenceReport
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Pack.ID != framework.EUAIActTechnicalEvidencePackID || len(decoded.Systems) != 1 {
		t.Fatalf("unexpected assessment: %#v", decoded)
	}
	found := false
	for _, objective := range decoded.Objectives {
		if objective.ID == "eu-aia-14-override-intervention" && objective.Status == framework.ObjectiveCandidate {
			found = true
		}
	}
	if !found {
		t.Fatalf("Article 14 override evidence was not mapped: %#v", decoded.Objectives)
	}
}

func TestFrameworkAssessWorksWithoutProfileAndRejectsUnknownPack(t *testing.T) {
	target := t.TempDir()
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"framework", "assess", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("profile-free assessment code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	arguments := []string{"framework", "assess", "--pack", "unknown", "--config", filepath.Join(target, config.FileName), target}
	if code := Execute(arguments, &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("Execute(%v) code=%d stderr=%q", arguments, code, stderr.String())
	}
}

func cliCandidateProviderSystem() profile.System {
	system := profile.NewDraftSystem("candidate-ranking", "Candidate ranking")
	system.IntendedPurpose = "Rank job applications for recruiter review."
	system.LifecycleStage = profile.LifecycleDevelopment
	system.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.Users = []string{"recruiters"}
	system.AffectedGroups = []string{"job applicants"}
	system.DecisionImpact = profile.ImpactAdvisory
	system.HumanOversight = profile.OversightRequired
	system.Data = profile.DataProfile{PersonalData: profile.TriYes, SpecialCategoryData: profile.TriNo, ChildrenData: profile.TriNo}
	system.DeploymentModels = []profile.DeploymentModel{profile.DeploymentPrivateCustomer}
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "A. Reviewer", ReviewedAt: "2026-08-02"}
	return system
}
