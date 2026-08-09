package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
)

func TestPromptOllamaModelListsInstalledModelsAndAcceptsCustomTag(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("3\n")), output: &output}
	model, err := promptOllamaModel(prompt, defaultSetupModel, []string{"codestral:22b"})
	if err != nil {
		t.Fatal(err)
	}
	if model != "codestral:22b" {
		t.Fatalf("model = %q", model)
	}
	for _, expected := range []string{"qwen3:8b", "qwen3-coder:30b", "codestral:22b", "installed; compatibility not yet validated"} {
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
	for _, expected := range []string{"ComplyScan setup", "System applicability setup", "Local model setup", "Saved", "Next: complyscan scan"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, stdout.String())
		}
	}
	for _, expected := range []string{
		"1) EU AI Act",
		"2) NIST AI RMF",
		"3) Both",
		"Select Lifecycle stage (1-5)",
		"Organization roles numbers (comma-separated)",
		"A short, stable machine-readable identifier",
		"provider — your organisation develops it",
		"biometrics — identifies people",
		"advisory — AI suggests or drafts",
		"inference — sends inputs to a model",
		"private-customer — a dedicated customer deployment",
		"Most developers should keep needs-review",
		"Ollama to keep model context on this machine",
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

func TestEverySetupQuestionHasDeveloperGuidance(t *testing.T) {
	keys := []string{
		"frameworks", "system-id", "system-name", "intended-purpose", "lifecycle-stage", "organization-roles",
		"operating-regions", "use-case-domains", "users", "affected-groups", "decision-impact",
		"human-oversight", "ai-activities", "personal-data", "special-category-data", "children-data",
		"deployment-models", "profile-reviewer", "applicability-decision", "decision-rationale",
		"applicability-reviewer", "replace-profile", "review-provider", "ollama-model", "install-ollama",
		"path-ownership", "ownership-paths", "ownership-systems", "replace-ownership", "download-model",
		"remote-disclosure", "remote-model", "api-key-env", "first-scan",
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
