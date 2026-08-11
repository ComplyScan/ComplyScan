package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestWriteSectionTitleUsesBoldOnlyWhenEnabled(t *testing.T) {
	var output bytes.Buffer
	if err := writeSectionTitle(&output, "Analysis and privacy mode", true, false); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\x1b[1mAnalysis and privacy mode\x1b[0m\n" {
		t.Fatalf("bold heading = %q", output.String())
	}

	output.Reset()
	if err := writeSectionTitle(&output, "Analysis and privacy mode", false, true); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\nAnalysis and privacy mode\n" {
		t.Fatalf("plain heading = %q", output.String())
	}
}

func TestPromptOllamaModelListsInstalledModelsAndAcceptsCustomTag(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("4\n")), output: &output}
	model, err := promptOllamaModel(prompt, defaultSetupModel, []string{"codestral:22b"})
	if err != nil {
		t.Fatal(err)
	}
	if model != "codestral:22b" {
		t.Fatalf("model = %q", model)
	}
	for _, expected := range []string{"qwen3.5:9b", "onboarding benchmark recorded", "qwen3:8b", "technical-review baseline", "qwen3-coder:30b", "codestral:22b", "compatibility checked automatically"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("picker output missing %q:\n%s", expected, output.String())
		}
	}

	output.Reset()
	prompt = promptSession{reader: bufio.NewReader(strings.NewReader("my-model:latest\n")), output: &output}
	model, err = promptOllamaModel(prompt, defaultSetupModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if model != "my-model:latest" {
		t.Fatalf("custom model = %q", model)
	}
}

func TestPromptOllamaModelUsesTerminalSelectorAndCustomEntry(t *testing.T) {
	var output bytes.Buffer
	var choices []terminalChoice
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("account-model:latest\n")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Ollama model" || defaultValue != defaultSetupModel {
				t.Fatalf("selector arguments: label=%q default=%q", label, defaultValue)
			}
			choices = append([]terminalChoice(nil), options...)
			return customModelChoice, nil
		},
	}
	model, err := promptOllamaModel(prompt, defaultSetupModel, []string{"codestral:22b"})
	if err != nil {
		t.Fatal(err)
	}
	if model != "account-model:latest" {
		t.Fatalf("model = %q", model)
	}
	if len(choices) < 2 || choices[len(choices)-1].Value != customModelChoice || !strings.Contains(choices[len(choices)-1].Label, "custom") {
		t.Fatalf("terminal choices = %#v", choices)
	}
	if !strings.Contains(output.String(), "Custom Ollama model tag") || strings.Contains(output.String(), "1)") {
		t.Fatalf("custom-model output = %q", output.String())
	}
}

func TestOllamaInstalledModelsParsesListOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ollama")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'NAME ID SIZE MODIFIED\\nqwen3:8b abc 5GB now\\nQWEN3:8B duplicate 5GB now\\ncodestral:22b def 12GB now\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	models := ollamaInstalledModels(context.Background(), executable)
	if strings.Join(models, ",") != "qwen3:8b,codestral:22b" {
		t.Fatalf("models = %#v", models)
	}
}

func TestInteractiveSetupCreatesProfileAndSelectsLocalReview(t *testing.T) {
	target := t.TempDir()
	input := strings.NewReader(strings.Repeat("\n", 20))
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"setup", "--interactive", "--skip-ollama-install", "--skip-model-pull", "--skip-scan", target}, input, &stdout, &stderr, testBuild)
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
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != defaultSetupModel {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
	for _, expected := range []string{"ComplyScan setup", "Repository inspected", "Quick system setup", "Local model setup", "Saved", "Next: complyscan scan"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, stdout.String())
		}
	}
	for _, expected := range []string{
		"Select Lifecycle stage (1-5)",
		"Operating regions numbers (comma-separated)",
		"Select organisation role (1-4)",
		"advisory — AI suggests or drafts",
		"remaining conditionally relevant facts",
		"EU AI Act technical mapping is recommended",
		"Local AI-assisted analysis — Ollama keeps context on this machine",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("guided setup output missing explanation %q:\n%s", expected, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "comma-separated: eu-ai-act-technical-evidence") {
		t.Errorf("guided setup exposed internal framework IDs:\n%s", stdout.String())
	}
	ignored, err := os.ReadFile(filepath.Join(target, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignored), reportGitIgnoreEntry) {
		t.Fatalf("generated reports are not ignored:\n%s", ignored)
	}
}

func TestFastSetupUsesDeterministicDraftWithoutModel(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "requirements.txt"), []byte("openai==2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("\nDraft support replies for agents.\n4\n1\n1\n1\n1\n\n8\n")
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{
		"setup", "--interactive", "--review", "none", "--framework", framework.NISTAIRMFTechnicalEvidencePackID, "--skip-scan", target,
	}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 1 || len(cfg.Systems[0].AIActivities) != 1 || cfg.Systems[0].AIActivities[0] != profile.ActivityInference {
		t.Fatalf("systems = %#v", cfg.Systems)
	}
	if cfg.AI.Provider != "none" || !strings.Contains(stdout.String(), "Prepared 1 repository-evident setup suggestion(s) without a model") || !strings.Contains(stdout.String(), "editable draft") {
		t.Fatalf("fast setup output:\n%s", stdout.String())
	}
}

func TestRefreshSetupDiscoveryAddsAndReplacesGeneratedFiles(t *testing.T) {
	target := t.TempDir()
	configPath := filepath.Join(target, config.FileName)
	ignorePath := filepath.Join(target, ".gitignore")
	if err := os.WriteFile(configPath, []byte("frameworks: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignorePath, []byte("/.complyscan/reports/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initial := discovery.Result{
		Repository: discovery.Repository{Root: target, Files: []discovery.File{{Path: config.FileName, Kind: discovery.KindConfig, Size: 3, Content: []byte("old")}}},
		Stats:      discovery.Stats{FilesRead: 1, BytesRead: 3},
	}
	refreshed, err := refreshSetupDiscovery(initial, target,
		setupGeneratedFile{Path: configPath, Kind: discovery.KindConfig},
		setupGeneratedFile{Path: ignorePath, Kind: discovery.KindOtherText},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Repository.Files) != 2 || refreshed.Stats.FilesRead != 2 || refreshed.Repository.Files[0].Path != config.FileName || string(refreshed.Repository.Files[0].Content) != "frameworks: []\n" {
		t.Fatalf("refreshed = %#v", refreshed)
	}
}

func TestRelevantEUContextAsksHighRiskFollowUpQuestions(t *testing.T) {
	system := basicApplicabilityTestSystem()
	input := strings.NewReader("4\n1\n3\nrecruiters\njob applicants\n1\n2\n2\nProduct owner\n")
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(input), output: &output}
	if err := collectRelevantEUApplicabilityContext(prompt, &system, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), newSetupProfileDraft()); err != nil {
		t.Fatal(err)
	}
	assessment := profile.AssessEUAIAct([]profile.System{system}).Systems[0]
	if assessment.MappingReadiness != profile.MappingHumanReviewed || len(assessment.MissingContext) != 0 {
		t.Fatalf("assessment = %#v", assessment)
	}
	for _, expected := range []string{"? Users", "? Potentially affected groups", "Select Processes personal data", "Applicability readiness gate: human-reviewed"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestRelevantEUContextSkipsIrrelevantPeopleAndDataQuestions(t *testing.T) {
	system := basicApplicabilityTestSystem()
	system.DecisionImpact = profile.ImpactLow
	input := strings.NewReader("10\n1\n6\n\n")
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(input), output: &output}
	if err := collectRelevantEUApplicabilityContext(prompt, &system, time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), newSetupProfileDraft()); err != nil {
		t.Fatal(err)
	}
	assessment := profile.AssessEUAIAct([]profile.System{system}).Systems[0]
	if assessment.MappingReadiness != profile.MappingFactuallyReady || len(assessment.MissingContext) != 0 {
		t.Fatalf("assessment = %#v", assessment)
	}
	for _, unexpected := range []string{"? Users", "? Potentially affected groups", "Select Processes personal data"} {
		if strings.Contains(output.String(), unexpected) {
			t.Errorf("output unexpectedly contains %q:\n%s", unexpected, output.String())
		}
	}
	if !strings.Contains(output.String(), "Applicability readiness gate: factually-ready") {
		t.Errorf("readiness gate missing:\n%s", output.String())
	}
}

func basicApplicabilityTestSystem() profile.System {
	system := profile.NewDraftSystem("example", "Example")
	system.IntendedPurpose = "Assist developers with repository review."
	system.LifecycleStage = profile.LifecycleDevelopment
	system.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.DecisionImpact = profile.ImpactSignificant
	system.HumanOversight = profile.OversightRequired
	return system
}

func TestNISTOnlySetupSkipsEUApplicabilityDecision(t *testing.T) {
	target := t.TempDir()
	input := strings.NewReader(strings.Repeat("\n", 18))
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{
		"setup", "--interactive", "--framework", "nist-ai-rmf-technical-evidence", "--review", "none", "--skip-scan", target,
	}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Frameworks) != 1 || cfg.Frameworks[0] != "nist-ai-rmf-technical-evidence" || len(cfg.Systems) != 1 {
		t.Fatalf("unexpected NIST-only setup: %#v", cfg)
	}
	if len(cfg.Systems[0].Applicability) != 0 || strings.Contains(stdout.String(), "Human EU AI Act applicability decision") {
		t.Fatalf("NIST-only setup asked for an EU legal decision:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "NIST AI RMF is voluntary guidance") {
		t.Fatalf("framework explanation missing:\n%s", stdout.String())
	}
}

func TestPromptFrameworkSelectionUsesHumanReadableNumberedChoices(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defaults []string
		want     string
	}{
		{name: "default EU", input: "\n", want: "eu-ai-act-technical-evidence"},
		{name: "EU", input: "1\n", want: "eu-ai-act-technical-evidence"},
		{name: "NIST", input: "2\n", want: "nist-ai-rmf-technical-evidence"},
		{name: "both", input: "3\n", want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
		{name: "configured NIST default", input: "\n", defaults: []string{"nist-ai-rmf-technical-evidence"}, want: "nist-ai-rmf-technical-evidence"},
		{name: "configured both default", input: "\n", defaults: []string{"eu-ai-act-technical-evidence", "nist-ai-rmf-technical-evidence"}, want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
		{name: "invalid then both", input: "4\n3\n", want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			prompt := promptSession{reader: bufio.NewReader(strings.NewReader(test.input)), output: &output}
			selected, err := promptFrameworkSelection(prompt, test.defaults)
			if err != nil {
				t.Fatal(err)
			}
			if actual := strings.Join(selected, ","); actual != test.want {
				t.Fatalf("selection = %q, want %q", actual, test.want)
			}
			for _, expected := range []string{"1) EU AI Act", "2) NIST AI RMF", "3) Both", "Select technical evidence packs (1-3)"} {
				if !strings.Contains(strings.ReplaceAll(output.String(), ", ", ","), expected) {
					t.Errorf("picker output missing %q:\n%s", expected, output.String())
				}
			}
			if test.name == "invalid then both" && !strings.Contains(output.String(), "Enter a number from 1 to 3.") {
				t.Errorf("invalid selection did not explain valid choices:\n%s", output.String())
			}
		})
	}
}

func TestPromptChoiceUsesNumberedMenu(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		invalid bool
	}{
		{name: "number", input: "2\n", want: "beta"},
		{name: "default", input: "\n", want: "beta"},
		{name: "legacy text", input: "gamma\n", want: "gamma"},
		{name: "invalid then number", input: "9\n1\n", want: "alpha", invalid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			prompt := promptSession{reader: bufio.NewReader(strings.NewReader(test.input)), output: &output}
			selected, err := promptChoice(prompt, "example", "beta", "alpha", "beta", "gamma")
			if err != nil {
				t.Fatal(err)
			}
			if selected != test.want {
				t.Fatalf("selection = %q, want %q", selected, test.want)
			}
			for _, expected := range []string{"1) alpha", "2) beta", "3) gamma", "Select example (1-3) [2]"} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("picker output missing %q:\n%s", expected, output.String())
				}
			}
			if test.invalid && !strings.Contains(output.String(), "Enter a number from 1 to 3.") {
				t.Errorf("invalid selection did not explain numeric choices:\n%s", output.String())
			}
		})
	}
}

func TestPromptChoiceUsesTerminalSelectorWhenAvailable(t *testing.T) {
	var output bytes.Buffer
	called := false
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			called = true
			if label != "example" || defaultValue != "beta" {
				t.Fatalf("selector arguments: label=%q default=%q", label, defaultValue)
			}
			if len(options) != 3 || options[0] != (terminalChoice{Label: "alpha", Value: "alpha"}) {
				t.Fatalf("selector options = %#v", options)
			}
			return "gamma", nil
		},
	}
	selected, err := promptChoice(prompt, "example", "beta", "alpha", "beta", "gamma")
	if err != nil {
		t.Fatal(err)
	}
	if !called || selected != "gamma" {
		t.Fatalf("selected = %q; called=%t", selected, called)
	}
	if output.Len() != 0 {
		t.Fatalf("numbered fallback was unexpectedly rendered: %q", output.String())
	}
}

func TestConfirmUsesTerminalSelectorWhenAvailable(t *testing.T) {
	var output bytes.Buffer
	called := false
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		confirmBool: func(label string, defaultValue bool) (bool, error) {
			called = true
			if label != "Continue setup" || !defaultValue {
				t.Fatalf("selector arguments: label=%q default=%t", label, defaultValue)
			}
			return false, nil
		},
	}
	confirmed, err := prompt.confirm("Continue setup", true)
	if err != nil {
		t.Fatal(err)
	}
	if !called || confirmed {
		t.Fatalf("confirmed = %t; called=%t", confirmed, called)
	}
	if output.Len() != 0 {
		t.Fatalf("text fallback was unexpectedly rendered: %q", output.String())
	}
}

func TestPromptChoicesUsesNumberedMultiSelect(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		invalid bool
	}{
		{name: "numbers", input: "1,3\n", want: "alpha,gamma"},
		{name: "default", input: "\n", want: "beta,gamma"},
		{name: "legacy text", input: "alpha,gamma\n", want: "alpha,gamma"},
		{name: "deduplicates", input: "3,3,1\n", want: "gamma,alpha"},
		{name: "invalid then numbers", input: "1,9\n2\n", want: "beta", invalid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			prompt := promptSession{reader: bufio.NewReader(strings.NewReader(test.input)), output: &output}
			selected, err := promptChoices(prompt, "Examples", []string{"beta", "gamma"}, "alpha", "beta", "gamma")
			if err != nil {
				t.Fatal(err)
			}
			if actual := strings.Join(selected, ","); actual != test.want {
				t.Fatalf("selection = %q, want %q", actual, test.want)
			}
			for _, expected := range []string{"1) alpha", "2) beta", "3) gamma", "Examples numbers (comma-separated) [2,3]"} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("picker output missing %q:\n%s", expected, output.String())
				}
			}
			if test.invalid && !strings.Contains(output.String(), "Enter one or more numbers from 1 to 3, separated by commas.") {
				t.Errorf("invalid selection did not explain numeric choices:\n%s", output.String())
			}
		})
	}
}

func TestPromptChoicesUsesTerminalMultiSelectWhenAvailable(t *testing.T) {
	var output bytes.Buffer
	called := false
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			called = true
			values := make([]string, len(options))
			for index, option := range options {
				values[index] = option.Value
			}
			if label != "Operating regions" || strings.Join(defaults, ",") != "unknown" || strings.Join(values, ",") != "eu,us,unknown" || strings.Join(exclusive, ",") != "unknown" {
				t.Fatalf("selector arguments: label=%q defaults=%#v options=%#v exclusive=%#v", label, defaults, options, exclusive)
			}
			return []string{"eu", "us"}, nil
		},
	}
	selected, err := promptChoices(prompt, "Operating regions", []profile.OperatingRegion{profile.RegionUnknown},
		profile.RegionEU, profile.RegionUS, profile.RegionUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(selected) != 2 || selected[0] != profile.RegionEU || selected[1] != profile.RegionUS {
		t.Fatalf("selected = %#v; called=%t", selected, called)
	}
	if output.Len() != 0 {
		t.Fatalf("numbered fallback was unexpectedly rendered: %q", output.String())
	}
}

func TestTerminalPromptAvailabilityRespectsAccessibleModeAndStreams(t *testing.T) {
	var input, output bytes.Buffer
	if terminalPromptAvailable(&input, &output) {
		t.Fatal("buffers must use the text fallback")
	}
	t.Setenv(accessiblePromptEnvironment, "1")
	if terminalPromptAvailable(os.Stdin, os.Stdout) {
		t.Fatal("accessible mode must use the text fallback")
	}
	t.Setenv(accessiblePromptEnvironment, "")
	t.Setenv("TERM", "dumb")
	if terminalPromptAvailable(os.Stdin, os.Stdout) {
		t.Fatal("dumb terminals must use the text fallback")
	}
}

func TestPromptSetupScanModeMakesExpensiveReviewExplicit(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		provider   string
		modelReady bool
		want       setupScanMode
	}{
		{name: "quick default", input: "\n", want: setupScanQuick},
		{name: "deep default after ready AI setup", input: "\n", provider: "ollama", modelReady: true, want: setupScanDeep},
		{name: "deep", input: "2\n", want: setupScanDeep},
		{name: "configure only", input: "3\n", want: setupScanNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			prompt := promptSession{reader: bufio.NewReader(strings.NewReader(test.input)), output: &output}
			mode, err := promptSetupScanMode(prompt, setupRepositorySummary{}, test.provider, test.modelReady)
			if err != nil {
				t.Fatal(err)
			}
			if mode != test.want {
				t.Fatalf("mode = %q, want %q", mode, test.want)
			}
			for _, expected := range []string{"1) Quick scan", "2) Deep AI review", "3) Save setup without scanning"} {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("scan-mode output missing %q:\n%s", expected, output.String())
				}
			}
			if test.want == setupScanDeep && !strings.Contains(output.String(), "may take many minutes") {
				t.Errorf("deep review did not disclose duration uncertainty:\n%s", output.String())
			}
		})
	}
}

func TestEverySetupQuestionHasDeveloperGuidance(t *testing.T) {
	keys := []string{
		"applicability-context", "frameworks", "system-id", "system-name", "intended-purpose", "lifecycle-stage", "organization-roles", "organization-role-basic",
		"operating-regions", "use-case-domains", "users", "affected-groups", "decision-impact",
		"human-oversight", "ai-activities", "personal-data", "special-category-data", "children-data",
		"deployment-models", "profile-reviewer", "applicability-decision", "decision-rationale",
		"applicability-reviewer", "replace-profile", "review-provider", "ollama-model", "install-ollama",
		"path-ownership", "ownership-paths", "ownership-systems", "replace-ownership", "download-model",
		"remote-disclosure", "remote-model", "api-key-env", "first-scan", "scan-mode",
	}
	for _, key := range keys {
		lines, exists := setupQuestionHelp[key]
		if !exists || len(lines) < 2 {
			t.Errorf("setup guidance %q is missing or too brief: %#v", key, lines)
		}
	}
	if len(setupQuestionHelp) != len(keys) {
		t.Fatalf("guidance catalog has %d entries, want %d; update the completeness test when adding a setup question", len(setupQuestionHelp), len(keys))
	}
}

func TestNonInteractiveSetupUpdatesReviewWithoutInventingProfile(t *testing.T) {
	target := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{
		"setup", "--non-interactive", "--review", "ollama", "--ollama-model", "local-test-model",
		"--skip-model-pull", "--skip-scan", target,
	}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Systems) != 0 {
		t.Fatalf("non-interactive setup invented systems: %#v", cfg.Systems)
	}
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != "local-test-model" {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
}

func TestNonInteractiveSetupConfiguresRemoteReviewWithoutSavingCredential(t *testing.T) {
	target := t.TempDir()
	secret := "sk-proj-" + strings.Repeat("x", 24)
	t.Setenv("COMPLYSCAN_TEST_REMOTE_KEY", secret)
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{
		"setup", "--non-interactive", "--review", "openai", "--allow-remote-review",
		"--model", "gpt-test", "--api-key-env", "COMPLYSCAN_TEST_REMOTE_KEY", "--skip-scan", target,
	}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) || !strings.Contains(string(data), "api-key-env: COMPLYSCAN_TEST_REMOTE_KEY") {
		t.Fatalf("unsafe remote configuration:\n%s", data)
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != "openai" || cfg.AI.Remote.Model != "gpt-test" {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
}

func TestPromptRemoteModelOffersProviderChoicesAndCustomID(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("2\n")), output: &output}
	model, err := promptRemoteModel(prompt, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if model != "claude-opus-5" || !strings.Contains(output.String(), "claude-sonnet-5") {
		t.Fatalf("model = %q; output:\n%s", model, output.String())
	}
	prompt = promptSession{reader: bufio.NewReader(strings.NewReader("account-specific-model\n")), output: &output}
	model, err = promptRemoteModel(prompt, "gemini")
	if err != nil || model != "account-specific-model" {
		t.Fatalf("custom model = %q, error = %v", model, err)
	}
}

func TestPromptRemoteModelUsesTerminalSelectorAndCustomEntry(t *testing.T) {
	var output bytes.Buffer
	var choices []terminalChoice
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("account-specific-model\n")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Remote model" || defaultValue != "claude-sonnet-5" {
				t.Fatalf("selector arguments: label=%q default=%q", label, defaultValue)
			}
			choices = append([]terminalChoice(nil), options...)
			return customModelChoice, nil
		},
	}
	model, err := promptRemoteModel(prompt, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if model != "account-specific-model" {
		t.Fatalf("model = %q", model)
	}
	if len(choices) != 4 || choices[0].Value != "claude-sonnet-5" || choices[len(choices)-1].Value != customModelChoice {
		t.Fatalf("terminal choices = %#v", choices)
	}
	if !strings.Contains(output.String(), "Custom remote model ID") || strings.Contains(output.String(), "1)") {
		t.Fatalf("custom-model output = %q", output.String())
	}
}

func TestSetupRejectsUnsafeOrConflictingAutomationFlags(t *testing.T) {
	target := t.TempDir()
	tests := [][]string{
		{"setup", "--interactive", "--non-interactive", target},
		{"setup", "--non-interactive", "--pull-model", "--skip-model-pull", target},
		{"setup", "--non-interactive", "--install-ollama", "--skip-ollama-install", target},
		{"setup", "--non-interactive", "--pull-model", "--review", "none", target},
		{"setup", "--non-interactive", "--install-ollama", "--review", "none", target},
		{"setup", "--non-interactive", "--ollama-model", "model", "--review", "none", target},
		{"setup", "--non-interactive", "--review", "openai", "--model", "gpt-test", target},
		{"setup", "--non-interactive", "--review", "ollama", "--allow-remote-review", target},
		{"setup", "--non-interactive", "--review", "none", "--api-key-env", "OPENAI_API_KEY", target},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := executeWithInput(args, strings.NewReader(""), &stdout, &stderr, testBuild); code != 2 {
			t.Errorf("%v exit code = %d; stderr=%q", args, code, stderr.String())
		}
	}
}

func TestShellQuoteProtectsCopyableRecoveryCommands(t *testing.T) {
	tests := map[string]string{
		".":                   ".",
		"qwen3:8b":            "qwen3:8b",
		"repo with spaces":    "'repo with spaces'",
		"$(touch unexpected)": "'$(touch unexpected)'",
		"model'with'quotes":   "'model'\"'\"'with'\"'\"'quotes'",
	}
	for input, expected := range tests {
		if actual := shellQuote(input); actual != expected {
			t.Errorf("shellQuote(%q) = %q, want %q", input, actual, expected)
		}
	}
}
