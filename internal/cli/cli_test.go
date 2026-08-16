// complyscan:ignore-technical-evidence -- this file embeds synthetic technical-objective fixtures.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/reviewconsent"
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

func TestScanCommandReusesGuidedSetupDiscovery(t *testing.T) {
	target := t.TempDir()
	seed := &scanDiscoverySeed{Target: target, Discovery: discovery.Result{Repository: discovery.Repository{
		Root:  target,
		Files: []discovery.File{{Path: "app.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nclient = OpenAI()\n")}},
	}}}
	var stdout, stderr bytes.Buffer
	command := newScanCommandWithDiscovery(&stdout, testBuild, seed)
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetErr(&stderr)
	command.SetArgs([]string{"--no-report", target})
	if err := command.Execute(); err != nil {
		t.Fatalf("scan error = %v; stderr=%q\n%s", err, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Reusing repository discovery from guided setup") || !strings.Contains(stdout.String(), "AI-DISC-001") {
		t.Fatalf("seeded scan output:\n%s", stdout.String())
	}
}

func TestScanExcludesActiveConfigButKeepsGitHubWorkflowYAML(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workflow := "endpoint: https://api." + "openai.com/v1/responses\n"
	if err := os.WriteFile(filepath.Join(target, ".github", "workflows", "complyscan.yml"), []byte(workflow), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if strings.Contains(output, "Ollama") {
		t.Fatalf("active config's Ollama endpoint entered scan evidence:\n%s", output)
	}
	if !strings.Contains(output, "AI provider or framework detected: OpenAI") || !strings.Contains(output, ".github/workflows/complyscan.yml") {
		t.Fatalf("GitHub workflow YAML was excluded with the active config:\n%s", output)
	}
}

func TestScanExcludesCustomActiveConfigPath(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(target, "private", "review.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Write(configPath, config.Default(), false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--config", configPath, target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "AI-DISC-001") || strings.Contains(stdout.String(), "Ollama") {
		t.Fatalf("custom active config entered scan evidence:\n%s", stdout.String())
	}
}

func TestPlainScanPreservesLegacyExplicitReviewBoundary(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_TEST_MISSING_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_TEST_MISSING_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("local scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "automatic AI review is not enabled") || !strings.Contains(stdout.String(), "--provider openai") {
		t.Fatalf("legacy configuration did not receive an actionable migration note:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "model compatibility") || strings.Contains(stdout.String()+stderr.String(), "COMPLYSCAN_TEST_MISSING_KEY") {
		t.Fatalf("local scan used configured AI:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"review", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("explicit review code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Checking model compatibility before repository review") || !strings.Contains(stdout.String(), "COMPLYSCAN_TEST_MISSING_KEY is not set") {
		t.Fatalf("explicit review did not report unavailable configured AI:\n%s", stdout.String())
	}
}

func TestLegacyDeepFlagExplicitlyActivatesSavedProvider(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_DEEP_EXPLICIT_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_DEEP_EXPLICIT_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--deep", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("deep scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "Checking model compatibility before repository review") || !strings.Contains(output, "COMPLYSCAN_DEEP_EXPLICIT_KEY is not set") {
		t.Fatalf("explicit deep scan did not activate the saved provider:\n%s", output)
	}
	if strings.Contains(output, "automatic AI review is not enabled") {
		t.Fatalf("explicit deep scan was incorrectly deferred:\n%s", output)
	}
}

func TestScanUsesConfiguredAIOnlyAfterPersistedOptIn(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.ReviewOnScan = true
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_OPTED_IN_MISSING_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_OPTED_IN_MISSING_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	grantTestAutomaticReview(t, target, cfg)

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "Checking model compatibility before repository review") || !strings.Contains(output, "COMPLYSCAN_OPTED_IN_MISSING_KEY is not set") {
		t.Fatalf("persisted opt-in did not activate configured AI:\n%s", output)
	}
	if strings.Contains(output, "automatic AI review is not enabled") {
		t.Fatalf("opted-in configuration received a legacy migration note:\n%s", output)
	}
}

func TestDeterministicOnlyOverridesPersistedAIOptIn(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.ReviewOnScan = true
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_DETERMINISTIC_ONLY_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_DETERMINISTIC_ONLY_KEY", "secret-that-must-not-be-used")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	grantTestAutomaticReview(t, target, cfg)

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--deterministic-only", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("deterministic scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, "model compatibility") || strings.Contains(output, "AI reasoning") || strings.Contains(output, "automatic AI review is not enabled") {
		t.Fatalf("deterministic-only scan entered the configured AI path:\n%s", output)
	}
}

func TestScanBuildsAIUseInventoryWithoutProvider(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestAIUseManifest(t, target, aiuse.Use{
		ID: "answer-generation", Name: "Answer generation", Description: "Generate answers with a hosted model.",
		Paths: []string{"app.py"}, Status: aiuse.StatusActive, Review: profile.ProfileReview{Status: profile.ReviewDraft},
	})

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RepositoryAnalysis != nil || decoded.RepositoryAnalysisRun != report.RepositoryAnalysisNotRequested {
		t.Fatalf("plain scan unexpectedly ran repository analysis: %#v", decoded.RepositoryAnalysis)
	}
	if decoded.AIUseInventory == nil || decoded.AIUseInventory.ChangedScope || decoded.AIUseInventory.Summary.Draft != 1 || decoded.AIUseInventory.Summary.UngroupedSignals != 0 {
		t.Fatalf("unexpected local AI-use inventory: %#v", decoded.AIUseInventory)
	}
	if got := decoded.AIUseInventory.Draft[0].Observation; got != aiuse.ObservationTechnicalSignal {
		t.Fatalf("draft observation = %q", got)
	}
}

func TestScanExcludesAIUseManifestFromRepositoryEvidence(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestAIUseManifest(t, target, aiuse.Use{
		ID: "declared-use", Name: "Declared use", Description: "Calls https://api.openai.com for generated text.",
		Paths: []string{"main.go"}, Status: aiuse.StatusActive,
		Review: profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Product owner", ReviewedAt: "2026-08-15"},
	})

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AIInventory == nil || decoded.AIInventory.Summary.Components != 0 || decoded.AIInventory.Summary.Signals != 0 {
		t.Fatalf("manifest content entered deterministic/model repository evidence: %#v", decoded.AIInventory)
	}
	if decoded.AIUseInventory == nil || decoded.AIUseInventory.Summary.Confirmed != 1 {
		t.Fatalf("excluded manifest was not loaded as user-owned state: %#v", decoded.AIUseInventory)
	}
}

func TestScanRejectsInvalidAIUseManifest(t *testing.T) {
	target := t.TempDir()
	manifestPath := filepath.Join(target, aiuse.DefaultPath)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("version: 1\nuses:\n  - id: INVALID ID\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("scan code=%d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "validate AI-use manifest") {
		t.Fatalf("invalid manifest error was not actionable: %s", stderr.String())
	}
}

func TestChangedScanKeepsSavedAIUsesAndFullDeterministicSignals(t *testing.T) {
	target := t.TempDir()
	stablePath := filepath.Join(target, "stable.py")
	changedPath := filepath.Join(target, "changed.go")
	if err := os.WriteFile(stablePath, []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changedPath, []byte("package changed\nconst Version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, target, "init", "--quiet")
	runTestGit(t, target, "add", "stable.py", "changed.go")
	runTestGit(t, target, "-c", "user.name=ComplyScan Test", "-c", "user.email=test@localhost", "commit", "--quiet", "-m", "fixture baseline")
	if err := os.WriteFile(changedPath, []byte("package changed\nconst Version = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestAIUseManifest(t, target, aiuse.Use{
		ID: "stable-generation", Name: "Stable generation", Description: "Generate text in the unchanged runtime path.",
		Paths: []string{"stable.py"}, Status: aiuse.StatusActive,
		Review: profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Product owner", ReviewedAt: "2026-08-15"},
	})

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--format", "json", "--severity", "critical", "--changed-since", "HEAD", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("changed scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	snapshot := decoded.AIUseInventory
	if snapshot == nil || !snapshot.ChangedScope || snapshot.Summary.Confirmed != 1 || len(snapshot.Confirmed) != 1 {
		t.Fatalf("changed scan lost saved AI use: %#v", snapshot)
	}
	if len(snapshot.Confirmed[0].TechnicalSignals) != 1 || snapshot.Confirmed[0].TechnicalSignals[0].Path != "stable.py" {
		t.Fatalf("changed scan did not use full deterministic inventory: %#v", snapshot.Confirmed[0])
	}
	if decoded.AIUseMappings == nil || len(decoded.AIUseMappings.Uses) != 1 || decoded.AIUseMappings.Uses[0].UseID != "stable-generation" || decoded.AIUseMappings.Summary.UnassociatedUses != 1 {
		t.Fatalf("changed scan lost the per-use mapping or guessed system context: %#v", decoded.AIUseMappings)
	}
}

func writeTestAIUseManifest(t *testing.T, target string, uses ...aiuse.Use) {
	t.Helper()
	manifest := aiuse.NewManifest()
	manifest.Uses = append(manifest.Uses, uses...)
	if err := aiuse.Write(filepath.Join(target, aiuse.DefaultPath), manifest); err != nil {
		t.Fatal(err)
	}
}

func grantTestAutomaticReview(t *testing.T, target string, cfg config.Config) {
	t.Helper()
	store, err := defaultReviewConsentStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Grant(target, filepath.Join(target, config.FileName), cfg.AI); err != nil {
		t.Fatal(err)
	}
}

func runTestGit(t *testing.T, target string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", target}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}

func TestReviewRequireAIReviewFailsAfterSavingDeterministicReport(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nlogger.info(model_output)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_TEST_MISSING_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_TEST_MISSING_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"review", "--require-ai-review", "--no-color", target}, &stdout, &stderr, testBuild)
	if code != 2 {
		t.Fatalf("review code=%d, want 2; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(target, report.DefaultDirectory, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded report.Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Findings) == 0 || !strings.Contains(strings.Join(decoded.Warnings, "\n"), "COMPLYSCAN_TEST_MISSING_KEY is not set") {
		t.Fatalf("saved report lost deterministic findings or AI failure: %#v", decoded)
	}
}

func TestScanRequireAIReviewWithoutProviderWritesDeterministicReport(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nlogger.info(model_output)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--require-ai-review", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild)
	if code != 2 {
		t.Fatalf("scan code=%d, want 2; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("deterministic report was not written before strict failure: %v\n%s", err, stdout.String())
	}
	if decoded.RepositoryAnalysisRun != report.RepositoryAnalysisIncomplete || len(decoded.Findings) == 0 || !strings.Contains(strings.Join(decoded.Warnings, "\n"), "no advisory provider is configured") {
		t.Fatalf("strict missing-provider report is incomplete: %#v", decoded)
	}
	if !strings.Contains(stderr.String(), "Deterministic findings and evidence remain available") {
		t.Fatalf("strict missing-provider failure was not explained: %s", stderr.String())
	}
}

func TestDeterministicOnlyWithRequiredReviewWritesReportThenFailsPolicy(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nlogger.info(model_output)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.ReviewOnScan = true
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_DETERMINISTIC_STRICT_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_DETERMINISTIC_STRICT_KEY", "secret-that-must-not-be-read")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	grantTestAutomaticReview(t, target, cfg)

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--deterministic-only", "--require-ai-review", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild)
	if code != 2 {
		t.Fatalf("scan code=%d, want 2; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("deterministic report was not written: %v\n%s", err, stdout.String())
	}
	output := stdout.String() + stderr.String()
	if decoded.RepositoryAnalysisRun != report.RepositoryAnalysisIncomplete || strings.Contains(output, "Checking model compatibility") || strings.Contains(output, "COMPLYSCAN_DETERMINISTIC_STRICT_KEY") {
		t.Fatalf("deterministic strict scan crossed the AI boundary: %#v\n%s", decoded, output)
	}
}

func TestRequireAIReviewDoesNotGrantConsentToLegacyConfiguredProvider(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_LEGACY_REQUIRED_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_LEGACY_REQUIRED_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--require-ai-review", "--no-report", "--format", "json", target}, &stdout, &stderr, testBuild)
	if code != 2 {
		t.Fatalf("scan code=%d, want 2; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("strict deterministic report was not written: %v\n%s", err, stdout.String())
	}
	output := stdout.String() + stderr.String()
	if strings.Contains(output, "Checking model compatibility") || strings.Contains(output, "COMPLYSCAN_LEGACY_REQUIRED_KEY is not set") {
		t.Fatalf("strict policy flag was incorrectly treated as model consent:\n%s", output)
	}
	if decoded.RepositoryAnalysisRun != report.RepositoryAnalysisIncomplete || !strings.Contains(strings.Join(decoded.Warnings, "\n"), "automatic review has not been enabled") {
		t.Fatalf("strict deterministic result did not explain unavailable AI: %#v", decoded)
	}
}

func TestPlainScanDoesNotTrustRepositoryConfiguredRemoteDestination(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = customCompatibleProvider
	cfg.AI.ReviewOnScan = true
	cfg.AI.Remote = config.RemoteConfig{
		ProviderName: "Attacker-controlled gateway", BaseURL: "https://attacker.example/v1",
		Model: "exfiltrate", APIKeyEnv: "COMPLYSCAN_ATTACKER_TEST_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_ATTACKER_TEST_KEY", "existing-secret-that-must-not-be-read")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	qualificationCalls := 0
	httpRequests := 0
	previousTransport := http.DefaultTransport
	http.DefaultTransport = doctorRoundTripFunc(func(*http.Request) (*http.Response, error) {
		httpRequests++
		return nil, errors.New("network path must not be entered")
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		qualificationCalls++
		return modelQualificationOutcome{}, errors.New("model path must not be entered")
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if qualificationCalls != 0 {
		t.Fatalf("untrusted repository configuration entered the model path %d time(s)", qualificationCalls)
	}
	if httpRequests != 0 {
		t.Fatalf("untrusted repository configuration triggered %d HTTP request(s)", httpRequests)
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "this machine has not approved") || !strings.Contains(output, "--base-url") || strings.Contains(output, "existing-secret") {
		t.Fatalf("fail-closed automatic-review note is missing or leaked a credential:\n%s", output)
	}
}

func TestPlainScanSurfacesUnreadablePrivateConsent(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.ReviewOnScan = true
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_CORRUPT_CONSENT_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	store := reviewconsent.NewStore(filepath.Join(t.TempDir(), "review-consent"))
	previousStore := defaultReviewConsentStore
	defaultReviewConsentStore = func() (reviewconsent.Store, error) { return store, nil }
	t.Cleanup(func() { defaultReviewConsentStore = previousStore })
	if err := store.Grant(target, filepath.Join(target, config.FileName), cfg.AI); err != nil {
		t.Fatal(err)
	}
	records, err := os.ReadDir(store.Directory)
	if err != nil || len(records) != 1 {
		t.Fatalf("consent records = %d, %v", len(records), err)
	}
	if err := os.WriteFile(filepath.Join(store.Directory, records[0].Name()), []byte("{\"unexpected\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "private machine approval could not be verified") || !strings.Contains(output, "unknown field") || strings.Contains(output, "Checking model compatibility") {
		t.Fatalf("consent-store failure was not surfaced safely:\n%s", output)
	}
}

func TestExplicitProviderDoesNotInheritRepositoryCredentialRouting(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		ProviderName: "Untrusted repository setting", BaseURL: "https://attacker.example/v1",
		Model: "untrusted-model", APIKeyEnv: "COMPLYSCAN_UNTRUSTED_ROUTING_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_UNTRUSTED_ROUTING_KEY", "credential-that-must-not-be-read")
	t.Setenv("OPENAI_API_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--provider", "openai", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	output := stdout.String() + stderr.String()
	if !strings.Contains(output, "OPENAI_API_KEY is not set") || strings.Contains(output, "COMPLYSCAN_UNTRUSTED_ROUTING_KEY") || strings.Contains(output, "credential-that-must-not-be-read") {
		t.Fatalf("explicit provider inherited untrusted repository credential routing:\n%s", output)
	}
}

func TestReviewRequiresConfiguredProvider(t *testing.T) {
	target := t.TempDir()
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"review", "--no-report", target}, &stdout, &stderr, testBuild); code != 2 || !strings.Contains(stderr.String(), "AI review is not configured") {
		t.Fatalf("review code=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--no-report", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("local scan code=%d stderr=%q", code, stderr.String())
	}
}

func TestScanHelpPresentsOneScanWorkflow(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--help"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("scan help code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "configured advisory AI review") || !strings.Contains(stdout.String(), "--deterministic-only") || !strings.Contains(stdout.String(), "no provider is configured or AI is unavailable") {
		t.Fatalf("scan help does not explain the unified workflow and safe override:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "limit model context to changed eligible files plus up to eight connected files") || !strings.Contains(stdout.String(), "advisory-review details") {
		t.Fatalf("scan help does not describe the changed-file model boundary and review detail:\n%s", stdout.String())
	}
	for _, flag := range []string{"--provider", "--model", "--api-key-env", "--refresh-review", "--require-ai-review", "--deterministic-only"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("scan help is missing %s:\n%s", flag, stdout.String())
		}
	}
	for _, hidden := range []string{"--quick", "--deep", "--review"} {
		if strings.Contains(stdout.String(), hidden) {
			t.Fatalf("scan help exposes compatibility flag %s:\n%s", hidden, stdout.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"review", "--help"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("review help code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "send selected repository context") {
		t.Fatalf("review help does not disclose external processing:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "limit model context to changed eligible files plus up to eight connected files") || !strings.Contains(stdout.String(), "advisory-review details") {
		t.Fatalf("review help does not describe the changed-file model boundary and review detail:\n%s", stdout.String())
	}
	for _, flag := range []string{"--provider", "--model", "--api-key-env", "--refresh-review", "--require-ai-review"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Fatalf("review help is missing %s:\n%s", flag, stdout.String())
		}
	}
}

func TestRootHelpHidesReviewCompatibilityCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"--help"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("root help code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "\n  review ") {
		t.Fatalf("root help exposes the compatibility command:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\n  scan ") {
		t.Fatalf("root help does not present scan as the public workflow:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"review", "--help"}, &stdout, &stderr, testBuild); code != 0 || !strings.Contains(stdout.String(), "Compatibility command") {
		t.Fatalf("review compatibility command is not directly invokable: code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
}

func TestReviewSavesPreliminaryReportAndSurvivesProviderFailure(t *testing.T) {
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
	code := Execute([]string{"review", "--no-color", target}, &stdout, &stderr, testBuild)
	if code != 0 && code != 1 {
		t.Fatalf("review code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
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
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "gpt-test", APIKeyEnv: "COMPLYSCAN_ROOT_MISSING_KEY", TimeoutSeconds: 1, MaxFindings: 20,
	}
	t.Setenv("COMPLYSCAN_ROOT_MISSING_KEY", "")
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
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
	if !strings.Contains(stdout.String(), "automatic AI review is not enabled") {
		t.Fatalf("default command did not preserve the legacy processing boundary:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "model compatibility") || strings.Contains(stdout.String()+stderr.String(), "COMPLYSCAN_ROOT_MISSING_KEY") {
		t.Fatalf("default command used configured AI:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
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
	if decoded.SchemaVersion != 10 || decoded.Tool.Commit != "test" || decoded.Scan.ID == "" || decoded.Scan.Scope.Findings != "full-repository" || decoded.Scan.Scope.TechnicalEvidence != "full-repository" {
		t.Fatalf("missing evidence-bundle metadata: %#v", decoded)
	}
}

func TestScanMapsConfirmedAIUseToSystemRequirementsAndScopedEvidence(t *testing.T) {
	target := t.TempDir()
	source := "from openai import OpenAI\n\ndef approve_human_review_decision(request):\n    return {'status': 'approved', 'decision': request.pending_decision, 'review': 'human review approval'}\n"
	if err := os.MkdirAll(filepath.Join(target, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "review", "approval.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	system := profile.NewDraftSystem("support", "Support")
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.AIActivities = []profile.AIActivity{profile.ActivityInference}
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "reviewer", ReviewedAt: "2026-08-15"}
	cfg.Systems = []profile.System{system}
	for id := range cfg.Rules {
		cfg.Rules[id] = config.RuleConfig{Enabled: false}
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{{
		ID: "support-replies", Name: "Support reply drafting", Description: "Drafts replies for human approval.",
		SystemIDs: []string{"support"}, Paths: []string{"review/**"}, Status: aiuse.StatusActive,
		Review: profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "reviewer", ReviewedAt: "2026-08-15"},
	}}
	if err := aiuse.Write(filepath.Join(target, aiuse.DefaultPath), manifest); err != nil {
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
	if decoded.SchemaVersion != 10 || decoded.AIUseMappings == nil || len(decoded.AIUseMappings.Uses) != 1 {
		t.Fatalf("per-use mapping missing: %#v", decoded.AIUseMappings)
	}
	if decoded.AIUseInventory == nil || len(decoded.AIUseInventory.Confirmed) != 1 ||
		decoded.AIUseInventory.Confirmed[0].RepositoryFacts == nil {
		t.Fatalf("deterministic per-use facts missing: %#v", decoded.AIUseInventory)
	}
	facts := decoded.AIUseInventory.Confirmed[0].RepositoryFacts
	if len(facts.ModelProviders) != 1 || facts.ModelProviders[0].Name != "OpenAI" ||
		facts.ModelProviders[0].Source != aiuse.FactSourceDeterministic || facts.ModelProviders[0].Coverage != aiuse.FactCoverageFullRepository {
		t.Fatalf("runtime provider signal was not retained as a scoped provider observation: %#v", facts)
	}
	for _, fact := range facts.Facts {
		if fact.Field == profile.CodeFactAIActivities {
			t.Fatalf("a provider import was incorrectly promoted to an AI activity: %#v", facts)
		}
	}
	use := decoded.AIUseMappings.Uses[0]
	if use.UseID != "support-replies" || len(use.Frameworks) == 0 || len(use.Frameworks[0].Contexts) != 1 {
		t.Fatalf("use mapping = %#v", use)
	}
	context := use.Frameworks[0].Contexts[0]
	if context.Association.SystemID != "support" || context.Association.Status != "configured-system" {
		t.Fatalf("association = %#v", context.Association)
	}
	found := false
	for _, objective := range context.Objectives {
		if objective.ObjectiveID != "eu-aia-14-human-review-gate" {
			continue
		}
		found = objective.Requirement == "likely-required" && objective.Evidence == framework.ObjectiveCandidate && len(objective.EvidenceReferences) > 0 && objective.EvidenceReferences[0].Path == "review/approval.py"
	}
	if !found {
		t.Fatalf("scoped Article 14 mapping not found: %#v", context.Objectives)
	}
	if decoded.RepositoryAnalysis != nil || decoded.RepositoryAnalysisRun != report.RepositoryAnalysisNotRequested {
		t.Fatalf("plain scan unexpectedly used AI review: %#v", decoded.RepositoryAnalysis)
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
	if !strings.Contains(stdout.String(), "Reports saved:") || !strings.Contains(stdout.String(), markdownPath) || !strings.Contains(stdout.String(), "Historical evidence bundle:") {
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
	historyEntries, err := os.ReadDir(filepath.Join(reportDirectory, "history"))
	if err != nil {
		t.Fatal(err)
	}
	if len(historyEntries) != 1 || !historyEntries[0].IsDir() || strings.Contains(historyEntries[0].Name(), decoded.Scan.ID) {
		t.Fatalf("unexpected report history entries: %#v", historyEntries)
	}
	if _, err := time.Parse("2006-01-02_15-04-05Z", historyEntries[0].Name()); err != nil {
		t.Fatalf("history directory %q is not a readable UTC date and time: %v", historyEntries[0].Name(), err)
	}
	historicalJSON := filepath.Join(reportDirectory, "history", historyEntries[0].Name(), "report.json")
	historicalData, err := os.ReadFile(historicalJSON)
	if err != nil {
		t.Fatal(err)
	}
	var historical report.Report
	if err := json.Unmarshal(historicalData, &historical); err != nil {
		t.Fatal(err)
	}
	if historical.Scan.ID != decoded.Scan.ID {
		t.Fatalf("historical scan ID = %q, latest = %q", historical.Scan.ID, decoded.Scan.ID)
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

func TestReviewPreservesDeterministicResultWhenAIIsEnabledForClearRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "non-ai-repository")
	code := Execute([]string{"review", "--no-report", "--format", "json", "--provider", "ollama", "--ollama-model", "test-model", target}, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if decoded.Summary.Total != 0 {
		t.Fatalf("model review changed deterministic summary: %#v", decoded.Summary)
	}
	if len(decoded.Warnings) == 0 {
		t.Fatal("expected unavailable test model to be reported without failing the deterministic scan")
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
	target := t.TempDir()
	for _, arguments := range [][]string{
		{"scan", "--ollama-model", "gemma3", target},
		{"scan", "--model", "gpt-test", target},
		{"scan", "--api-key-env", "OPENAI_API_KEY", target},
		{"scan", "--refresh-review", target},
		{"scan", "--review", "ollama", "--ollama-endpoint", "https://example.com", target},
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

func TestConfiguredCompatibleReviewerUsesSavedProviderProfile(t *testing.T) {
	settings := config.Default().AI
	settings.Provider = "openai-compatible"
	settings.Remote = config.RemoteConfig{
		ProviderName: "Acme gateway", BaseURL: "https://models.example.com/v1", Model: "review-v2",
		APIKeyEnv: "COMPLYSCAN_TEST_GATEWAY_KEY", TimeoutSeconds: 45, MaxFindings: 4,
	}
	t.Setenv("COMPLYSCAN_TEST_GATEWAY_KEY", "secret-in-process-only")
	provider, timeout, maximum, model, kind, err := configuredReviewer(settings)
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || timeout != 45*time.Second || maximum != 4 || model != "review-v2" || kind != providers.Compatible {
		t.Fatalf("configured reviewer = %#v, %s, %d, %q, %q", provider, timeout, maximum, model, kind)
	}
}

func TestTechnicalReviewProgressDistinguishesModelAndCache(t *testing.T) {
	var output bytes.Buffer
	started := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	progress := technicalReviewProgress(&output, "anthropic", started, func() time.Time { return started.Add(12 * time.Second) })
	candidate := providers.TechnicalCandidate{SystemID: "ranking", RepositoryFiles: 42, ObjectiveID: "eu-aia-10-bias-evaluation", Path: "evaluation.go"}
	if err := progress(technicalreview.Progress{Current: 1, Total: 2, Candidate: candidate}); err != nil {
		t.Fatal(err)
	}
	if err := progress(technicalreview.Progress{Current: 2, Total: 2, Candidate: candidate, Cached: true}); err != nil {
		t.Fatal(err)
	}
	if err := progress(technicalreview.Progress{Stage: technicalreview.ProgressStageRateLimitWait, Current: 2, Total: 2, Candidate: candidate, Attempt: 1, Wait: time.Minute, OriginalWait: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if value := output.String(); !strings.Contains(value, "1/2") || !strings.Contains(value, "elapsed 12s") || !strings.Contains(value, "reviewing with Anthropic") || !strings.Contains(value, "2/2") || !strings.Contains(value, "using cached observation") || !strings.Contains(value, "system ranking, 42 owned file(s)") || !strings.Contains(value, "Rate limited · retry 1 in 1m · original wait 1m · Ctrl+C to stop") {
		t.Fatalf("unexpected progress output:\n%s", value)
	}
}

func TestRateLimitCountdownMessageShowsRemainingAndOriginalWait(t *testing.T) {
	got := rateLimitCountdownMessage(2, 44*time.Second, time.Minute)
	want := "Rate limited · retry 2 in 44s · original wait 1m · Ctrl+C to stop"
	if got != want {
		t.Fatalf("countdown message = %q, want %q", got, want)
	}
}

func TestTechnicalRateLimitCountdownUpdatesInteractiveLine(t *testing.T) {
	originalTerminal := llmActivityTerminal
	t.Cleanup(func() { llmActivityTerminal = originalTerminal })
	llmActivityTerminal = func(any) bool { return true }
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv(accessiblePromptEnvironment, "")

	var output bytes.Buffer
	progress := technicalReviewProgress(&output, "openai", time.Now(), time.Now)
	base := technicalreview.Progress{Stage: technicalreview.ProgressStageRateLimitWait, Current: 1, Total: 1, Attempt: 1, OriginalWait: time.Minute}
	base.Wait = time.Minute
	if err := progress(base); err != nil {
		t.Fatal(err)
	}
	base.Wait = 44 * time.Second
	if err := progress(base); err != nil {
		t.Fatal(err)
	}
	if err := progress(technicalreview.Progress{Stage: technicalreview.ProgressStageRateLimitResume, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	value := output.String()
	if !strings.Contains(value, "retry 1 in 1m · original wait 1m") || !strings.Contains(value, "retry 1 in 44s · original wait 1m") || !strings.Contains(value, "Cooldown complete · starting retry 1") {
		t.Fatalf("interactive countdown output = %q", value)
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

func TestScanGatesReviewedLikelyRequiredTechnicalGap(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.AI.Provider = "none"
	system := basicApplicabilityTestSystem()
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.AIActivities = []profile.AIActivity{profile.ActivityInference}
	system.DeploymentModels = []profile.DeploymentModel{profile.DeploymentAPI}
	system.Users = []string{"recruiters"}
	system.AffectedGroups = []string{"job applicants"}
	system.Data = profile.DataProfile{PersonalData: profile.TriNo, SpecialCategoryData: profile.TriNo, ChildrenData: profile.TriNo}
	system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Product owner", ReviewedAt: "2026-08-12"}
	cfg.Systems = []profile.System{system}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "AI-CTRL-001") || !strings.Contains(stdout.String(), "Likely required technical control") {
		t.Fatalf("technical gap was not reported:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"baseline", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("baseline exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("baselined scan exit code = %d, want 0; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Suppressed:") || strings.Contains(stdout.String(), "AI-CTRL-001") {
		t.Fatalf("technical gap baseline was not applied:\n%s", stdout.String())
	}
}

func TestScanDoesNotGateDraftApplicabilityContext(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.AI.Provider = "none"
	system := basicApplicabilityTestSystem()
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	system.AIActivities = []profile.AIActivity{profile.ActivityInference}
	system.DeploymentModels = []profile.DeploymentModel{profile.DeploymentAPI}
	system.Users = []string{"recruiters"}
	system.AffectedGroups = []string{"job applicants"}
	system.Data = profile.DataProfile{PersonalData: profile.TriNo, SpecialCategoryData: profile.TriNo, ChildrenData: profile.TriNo}
	cfg.Systems = []profile.System{system}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"scan", "--no-report", "--no-color", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stdout.String(), "AI-CTRL-001") {
		t.Fatalf("draft profile produced a blocking gap:\n%s", stdout.String())
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

func TestDoctorWarnsWhenOptionalOllamaModelIsMissing(t *testing.T) {
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
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[WARN] ollama model: qwen3.5:9b is not installed") ||
		!strings.Contains(stdout.String(), "deterministic scans remain available") {
		t.Fatalf("missing model was not reported:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("missing model probe exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] ollama model: qwen3.5:9b is not installed") ||
		!strings.Contains(stdout.String(), "[SKIP] review compatibility: a required review dependency is unavailable") ||
		!strings.Contains(stdout.String(), "Doctor found 1 blocking issue(s).") {
		t.Fatalf("missing model probe output:\n%s", stdout.String())
	}
}

func TestDoctorTreatsUnavailableOptionalOllamaServiceAsBlockingOnlyForProbe(t *testing.T) {
	target := t.TempDir()
	requests := 0
	useDoctorHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		requests++
		return nil, io.ErrUnexpectedEOF
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
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("ordinary doctor exit code = %d, want 0; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if requests != 1 {
		t.Fatalf("ordinary doctor service requests = %d, want 1", requests)
	}
	if !strings.Contains(stdout.String(), "[WARN] ollama service:") ||
		!strings.Contains(stdout.String(), "deterministic scans remain available") ||
		!strings.Contains(stdout.String(), "[WARN] review compatibility: cached status unavailable") ||
		!strings.Contains(stdout.String(), "Doctor found no blocking issues.") {
		t.Fatalf("ordinary doctor output:\n%s", stdout.String())
	}

	requests = 0
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("probe exit code = %d, want 1; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if requests != 1 {
		t.Fatalf("probe service requests = %d, want 1", requests)
	}
	if !strings.Contains(stdout.String(), "[FAIL] ollama service:") ||
		!strings.Contains(stdout.String(), "[SKIP] review compatibility: a required review dependency is unavailable") ||
		!strings.Contains(stdout.String(), "Doctor found 1 blocking issue(s).") {
		t.Fatalf("probe output:\n%s", stdout.String())
	}
}

func TestDoctorProbeUsesHealthyOllamaServiceWithoutExecutable(t *testing.T) {
	target := t.TempDir()
	useDoctorHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		return doctorHTTPResponse(http.StatusOK, `{"models":[{"name":"qwen3.5:9b","model":"qwen3.5:9b"}]}`), nil
	})

	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())

	qualificationCalls := 0
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(_ context.Context, _ config.AIConfig, refresh bool) (modelQualificationOutcome, error) {
		qualificationCalls++
		if !refresh {
			t.Fatal("doctor probe did not request a refreshed qualification")
		}
		return modelQualificationOutcome{}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if qualificationCalls != 1 {
		t.Fatalf("qualification calls = %d, want 1", qualificationCalls)
	}
	for _, expected := range []string{
		"[WARN] ollama executable: not found on PATH",
		"[PASS] ollama service:",
		"[PASS] ollama model: qwen3.5:9b",
		"[PASS] review compatibility:",
		"Doctor found no blocking issues.",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("doctor output missing %q:\n%s", expected, stdout.String())
		}
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
	if code := Execute([]string{"doctor", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("missing credential exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[WARN] remote credential: COMPLYSCAN_TEST_ANTHROPIC_KEY is not set") ||
		!strings.Contains(stdout.String(), "deterministic scans and matching cached reviews remain available") {
		t.Fatalf("missing credential output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 1 {
		t.Fatalf("missing credential probe exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] remote credential: COMPLYSCAN_TEST_ANTHROPIC_KEY is not set") ||
		!strings.Contains(stdout.String(), "the requested review probe cannot run") ||
		!strings.Contains(stdout.String(), "[SKIP] review compatibility: a required review dependency is unavailable") ||
		!strings.Contains(stdout.String(), "Doctor found 1 blocking issue(s).") {
		t.Fatalf("missing credential probe output:\n%s", stdout.String())
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

func TestResolvedPathExclusionHandlesRelativeScanTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "repository")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if got := resolvedPathExclusion("repository", filepath.Join("repository", config.FileName)); got != config.FileName {
		t.Fatalf("relative active config exclusion = %q", got)
	}
	if got := resolvedPathExclusion("repository", filepath.Join(parent, "outside.yml")); got != "" {
		t.Fatalf("outside active config exclusion = %q", got)
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
	if code := Execute([]string{"init", "--non-interactive"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("first init exit code = %d: %s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"init", "--non-interactive"}, &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("second init exit code = %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"init", "--force", "--non-interactive"}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("forced init exit code = %d: %s", code, stderr.String())
	}
}

func TestInteractiveInitCollectsAttributedSystemContext(t *testing.T) {
	target := t.TempDir()
	input := strings.Join([]string{
		"1", "", "Candidate ranking", "Rank job applications for recruiter review.", "development", "provider", "eu,uk", "employment",
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
	for _, expected := range []string{"Advanced system questionnaire — 17 questions", "Question 17 of 17 — Profile reviewer", "Applicability decision — 1 question", "Human EU AI Act applicability decision", "1 system profile"} {
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
	input := strings.NewReader(strings.Join([]string{
		"", "", "", // system ID, name, intended purpose
		"1", "6", "7", "13", // lifecycle, roles, regions, use case
		"", "", // users, affected groups
		"5", "5", "1", // impact, oversight, AI activities
		"3", "3", "3", "8", // data questions, deployment
		"", "1", // reviewer, applicability decision
	}, "\n") + "\n")
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
	if decoded.SchemaVersion != 10 || len(decoded.Frameworks) != 2 {
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
