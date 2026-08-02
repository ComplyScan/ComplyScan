package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/framework"
	"github.com/1eonardodawinki/ComplyScan/internal/inventory"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
	"github.com/1eonardodawinki/ComplyScan/internal/providers"
	"github.com/1eonardodawinki/ComplyScan/internal/report"
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
			if got := Execute([]string{"scan", "--no-color", target}, &stdout, &stderr, testBuild); got != test.want {
				t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", got, test.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestScanJSONOutputAndSeverityFilter(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--format", "json", "--severity", "high", target}, &stdout, &stderr, testBuild)
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
}

func TestScanSARIFOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	code := Execute([]string{"scan", "--format", "sarif", "--severity", "high", target}, &stdout, &stderr, testBuild)
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
	code := Execute([]string{"scan", "--no-color", "--severity", "high", target}, &stdout, &stderr, testBuild)
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
	code := Execute([]string{"scan", "--no-color", "--exclude", "app.py", target}, &stdout, &stderr, testBuild)
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

func TestScanChangedSinceRequiresGitRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute([]string{"scan", "--changed-since", "main", t.TempDir()}, &stdout, &stderr, testBuild)
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
	code := Execute([]string{"scan", "--format", "json", "--review", "ollama", "--ollama-model", "test-model", target}, &stdout, &stderr, testBuild)
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
	if decoded.Summary.Total != 0 {
		t.Fatalf("model review changed deterministic summary: %#v", decoded.Summary)
	}
}

func TestScanRejectsUnsafeOrInactiveOllamaOverrides(t *testing.T) {
	for _, arguments := range [][]string{
		{"scan", "--review", "openai"},
		{"scan", "--ollama-model", "gemma3"},
		{"scan", "--review", "ollama", "--ollama-endpoint", "https://example.com"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Execute(arguments, &stdout, &stderr, testBuild); code != 2 {
			t.Fatalf("Execute(%v) code = %d, want 2; stderr=%q", arguments, code, stderr.String())
		}
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
	code := Execute([]string{"scan", "--no-color", "--config", configPath, target}, &stdout, &stderr, testBuild)
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
	if code := Execute([]string{"scan", "--no-color", target}, &stdout, &stderr, testBuild); code != 0 {
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
	if code := Execute([]string{"scan", "--no-color", target}, &stdout, &stderr, testBuild); code != 1 {
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
		"", "Candidate ranking", "Rank job applications for recruiter review.", "development", "provider", "eu,uk", "employment",
		"recruiters", "job applicants", "advisory", "required", "yes", "unknown", "no", "private-customer,api",
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
	if system.Name != "Candidate ranking" || system.UseCaseDomains[0] != profile.DomainEmployment || system.Data.PersonalData != profile.TriYes {
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
	if code := Execute([]string{"scan", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded report.Report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Applicability == nil || len(decoded.Applicability.Systems) != 1 {
		t.Fatalf("missing applicability: %#v", decoded.Applicability)
	}
	if decoded.FrameworkAssessment == nil || len(decoded.FrameworkAssessment.Systems) != 1 || decoded.FrameworkAssessment.Systems[0].Activation != framework.ActivationNeedsReview {
		t.Fatalf("missing framework assessment: %#v", decoded.FrameworkAssessment)
	}
	if decoded.Summary.Total != 0 {
		t.Fatalf("applicability changed findings: %#v", decoded.Summary)
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
	if len(listings) != 1 || listings[0].Pack.Version != "0.1.0" || len(listings[0].Coverage.Limitations) == 0 {
		t.Fatalf("unexpected listings: %#v", listings)
	}
}

func TestFrameworkAssessMapsFullRepositoryEvidence(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "docs", "risk-assessment.md"), []byte("Risk assessment and mitigation controls cover intended purpose, foreseeable misuse, fundamental rights, testing, and post-market monitoring.\n"), 0o644); err != nil {
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
	var decoded framework.AssessmentReport
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Pack.ID != framework.EUAIActHighRiskProviderPackID || len(decoded.Systems) != 1 || decoded.Systems[0].Activation != framework.ActivationCandidate {
		t.Fatalf("unexpected assessment: %#v", decoded)
	}
	if decoded.Systems[0].Controls[0].Status != framework.ControlEvidenceFound {
		t.Fatalf("Article 9 was not mapped: %#v", decoded.Systems[0].Controls[0])
	}
}

func TestFrameworkAssessRequiresProfileAndKnownPack(t *testing.T) {
	target := t.TempDir()
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"framework", "assess", target},
		{"framework", "assess", "--pack", "unknown", "--config", filepath.Join(target, config.FileName), target},
	} {
		if arguments[2] == "--pack" {
			cfg, err := config.Load(filepath.Join(target, config.FileName))
			if err != nil {
				t.Fatal(err)
			}
			cfg.Systems = []profile.System{cliCandidateProviderSystem()}
			if err := config.Write(filepath.Join(target, config.FileName), cfg, true); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		if code := Execute(arguments, &stdout, &stderr, testBuild); code != 2 {
			t.Fatalf("Execute(%v) code=%d stderr=%q", arguments, code, stderr.String())
		}
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
