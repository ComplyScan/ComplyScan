package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/repositoryanalysis"
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

func TestSetupStepTitleShowsPositionAndPreservesPlainFallback(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{output: &output, step: &setupStepProgress{}}
	if err := setupStepTitle(prompt, 2, 5, "Repository inspection", true); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\nStep 2 of 5 — Repository inspection\n" {
		t.Fatalf("step heading = %q", output.String())
	}
	prompt, err := prompt.startQuestionGroup("Repository questions", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if label := prompt.nextQuestionLabel("Repository type"); label != "Step 2 of 5 · Question 1 of 2 — Repository type" {
		t.Fatalf("question breadcrumb = %q", label)
	}
}

func TestRunSetupPromptStepsReturnsAndPreservesEarlierAnswers(t *testing.T) {
	var calls []string
	first := "draft"
	secondCalls := 0
	prompt := promptSession{guidance: &questionGuidance{}, questions: &questionProgress{total: 2}}
	err := runSetupPromptSteps(prompt, false,
		func(step promptSession) error {
			calls = append(calls, "first:"+first)
			first = "confirmed"
			return nil
		},
		func(step promptSession) error {
			secondCalls++
			calls = append(calls, "second")
			if secondCalls == 1 {
				return errPromptBack
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first:draft", "second", "first:confirmed", "second"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunSetupPromptStepsAlwaysEnablesBackNavigation(t *testing.T) {
	calls := 0
	err := runSetupPromptSteps(promptSession{}, false, func(step promptSession) error {
		calls++
		if !step.backAvailable {
			t.Fatal("back navigation was not enabled on the first question")
		}
		if calls == 1 {
			return errPromptBack
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("first question calls = %d, want 2", calls)
	}
}

func TestBackControlsAreAddedOnlyWhenNavigationIsAvailable(t *testing.T) {
	var selectedOptions []terminalChoice
	prompt := promptSession{
		guidance:      &questionGuidance{},
		backAvailable: true,
		selectOne: func(_ string, _ string, options []terminalChoice) (string, error) {
			selectedOptions = append([]terminalChoice(nil), options...)
			return backChoiceValue, nil
		},
	}
	_, err := promptChoice(prompt, "Example", "one", "one", "two")
	if !errors.Is(err, errPromptBack) {
		t.Fatalf("error = %v, want back signal", err)
	}
	if !containsTerminalChoice(selectedOptions, backChoiceValue) {
		t.Fatalf("back option missing from %#v", selectedOptions)
	}
	for _, option := range visibleTerminalChoices(selectedOptions) {
		if option.Value == backChoiceValue || strings.Contains(option.Label, "Back") {
			t.Fatalf("back navigation marker became a visible option: %#v", selectedOptions)
		}
	}

	textPrompt := promptSession{
		reader:        bufio.NewReader(strings.NewReader("back\n")),
		output:        io.Discard,
		guidance:      &questionGuidance{},
		backAvailable: true,
	}
	if _, err := textPrompt.text("Name", "draft"); !errors.Is(err, errPromptBack) {
		t.Fatalf("text back error = %v", err)
	}
}

func TestPromptOllamaModelListsInstalledModelsAndAcceptsCustomTag(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("1\n")), output: &output}
	model, err := promptOllamaModel(prompt, defaultSetupModel, []ollamaInstalledModel{{tag: "codestral:22b", sizeGB: 12.4}})
	if err != nil {
		t.Fatal(err)
	}
	if model != "codestral:22b" {
		t.Fatalf("model = %q", model)
	}
	for _, expected := range []string{
		"Installed · codestral:22b", "Suggested download · qwen3.5:9b",
		"qwen3.5:9b", "onboarding benchmark recorded", "qwen3:8b", "technical-review baseline",
		"Ollama catalogue · Choose from common local models", "Exact tag · Enter any other Ollama model tag",
		"6.6 GB", "5.2 GB", "12.4 GB",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("picker output missing %q:\n%s", expected, output.String())
		}
	}

	output.Reset()
	prompt = promptSession{reader: bufio.NewReader(strings.NewReader("4\nmy-model:latest\n")), output: &output}
	model, err = promptOllamaModel(prompt, defaultSetupModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if model != "my-model:latest" {
		t.Fatalf("custom model = %q", model)
	}
}

func TestPromptAnalysisProviderGroupsHostedProviders(t *testing.T) {
	var calls int
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &bytes.Buffer{},
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			calls++
			switch calls {
			case 1:
				if label != "Optional AI assistance" || defaultValue != "Cloud AI review — configured scans using your API key" || len(options) != 3 || !strings.Contains(options[1].Label, "Experimental local AI assistance") || !strings.Contains(options[2].Label, "do not reason about safeguards") {
					t.Fatalf("analysis selector: label=%q default=%q options=%#v", label, defaultValue, options)
				}
				return options[0].Value, nil
			case 2:
				if label != "Hosted provider" || defaultValue != "anthropic" || len(options) != 4 || options[0].Value != "openai" || options[1].Value != "anthropic" || options[2].Value != "gemini" || options[3].Value != backChoiceValue {
					t.Fatalf("provider selector: label=%q default=%q options=%#v", label, defaultValue, options)
				}
				return "gemini", nil
			default:
				t.Fatalf("unexpected selector call %d", calls)
				return "", nil
			}
		},
	}
	provider, err := promptAnalysisProvider(prompt, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "gemini" || calls != 2 {
		t.Fatalf("provider=%q calls=%d", provider, calls)
	}
}

func TestStandardCloudShortlistExcludesExperimentalProvidersAndModels(t *testing.T) {
	profiles := standardHostedProviderProfiles()
	if len(profiles) != 3 {
		t.Fatalf("standard provider count = %d, want 3: %#v", len(profiles), profiles)
	}
	wantModels := map[string][]string{
		"openai":    {"gpt-5.6-sol", "gpt-5.6-terra"},
		"anthropic": {"claude-opus-5", "claude-sonnet-5"},
		"gemini":    {"gemini-3.5-flash", "gemini-3.6-flash"},
	}
	for _, profile := range profiles {
		models := remoteModelOptions(profile.ID)
		if strings.Join(models, ",") != strings.Join(wantModels[profile.ID], ",") {
			t.Errorf("%s shortlist = %#v", profile.ID, models)
		}
		for _, model := range profile.Models {
			if model.DraftValidated || model.CodeValidated || !strings.Contains(hostedModelStatus(model), "live quality gates pending") {
				t.Errorf("unearned validation state for %s/%s: %#v", profile.ID, model.ID, model)
			}
		}
	}
	for _, experimental := range []string{"xai", "mistral", "groq", "openrouter", customCompatibleProvider} {
		profile, exists := hostedProviderProfileFor(experimental)
		if !exists || profile.StandardSetup {
			t.Errorf("experimental provider %q entered standard setup: %#v", experimental, profile)
		}
	}
}

func TestPromptAnalysisProviderCanReturnFromHostedProvider(t *testing.T) {
	var calls int
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &bytes.Buffer{}, guidance: &questionGuidance{},
		selectOne: func(label, _ string, options []terminalChoice) (string, error) {
			calls++
			switch calls {
			case 1:
				return options[0].Value, nil // Cloud AI review.
			case 2:
				if label != "Hosted provider" || !containsTerminalChoice(options, backChoiceValue) {
					t.Fatalf("hosted selector has no back control: %#v", options)
				}
				return backChoiceValue, nil
			case 3:
				return options[2].Value, nil // Fast technical analysis.
			default:
				t.Fatalf("unexpected selector call %d", calls)
				return "", nil
			}
		},
	}
	provider, err := promptAnalysisProvider(prompt, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "none" || calls != 3 {
		t.Fatalf("provider = %q, calls = %d", provider, calls)
	}
}

func TestBasicSystemQuestionnaireMovesBackAndKeepsAnswer(t *testing.T) {
	input := strings.Join([]string{
		"",     // Keep system name.
		"",     // Keep intended purpose.
		"1",    // EU operating region.
		"back", // Return from organisation role.
		"",     // Keep the previously selected EU region.
		"1",    // Provider role.
		"1",    // Advisory impact.
		"1",    // Development lifecycle.
		"1",    // Required oversight.
	}, "\n") + "\n"
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader(input)), output: &output,
		guidance: &questionGuidance{}, step: &setupStepProgress{current: 4, total: 5},
	}
	system, err := collectBasicSystemProfile(prompt, ".", time.Now(), setupRepositorySummary{}, setupProfileDraft{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(system.OperatingRegions) != 1 || system.OperatingRegions[0] != profile.RegionEU {
		t.Fatalf("operating regions = %#v", system.OperatingRegions)
	}
	if strings.Count(output.String(), "Operating regions") < 2 {
		t.Fatalf("operating-region question was not revisited:\n%s", output.String())
	}
}

func TestBasicSystemQuestionnaireRequiresExplicitSelectorAnswersWhenRerun(t *testing.T) {
	existing := profile.NewDraftSystem("assistant", "Support Assistant")
	existing.IntendedPurpose = "Draft support replies for human review."
	existing.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	existing.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	existing.DecisionImpact = profile.ImpactAdvisory
	existing.LifecycleStage = profile.LifecycleProduction
	existing.HumanOversight = profile.OversightRequired
	var singleCalls, multipleCalls int
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("\n\n")), output: io.Discard,
		guidance: &questionGuidance{}, step: &setupStepProgress{current: 3, total: 5},
		selectOne: func(_ string, defaultValue string, _ []terminalChoice) (string, error) {
			singleCalls++
			if defaultValue != requiredAnswerChoiceValue {
				t.Fatalf("selector %d reused saved default %q", singleCalls, defaultValue)
			}
			return []string{
				string(profile.ImpactAdvisory), string(profile.LifecycleProduction), string(profile.OversightRequired),
			}[singleCalls-1], nil
		},
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			multipleCalls++
			if len(defaults) != 0 {
				t.Fatalf("multi-selector reused saved defaults: %#v", defaults)
			}
			if multipleCalls == 1 {
				return []string{string(profile.RegionEU)}, nil
			}
			if label != "Step 3 of 5 · Question 4 of 7 — Organisation roles" || strings.Join(exclusive, ",") != string(profile.RoleUnknown) {
				t.Fatalf("organisation selector label=%q exclusive=%#v", label, exclusive)
			}
			wantLabels := []string{
				"Provider — we build, brand, or supply the AI system",
				"Deployer — we professionally use an AI system supplied by someone else",
				"Importer — we bring a non-EU provider’s AI system into the EU market",
				"Distributor — we make another provider’s AI system available in the EU",
				"Product manufacturer — we supply the AI system with a product under our name or brand",
				"Unknown — our organisation’s role has not been confirmed",
			}
			if len(options) < len(wantLabels) {
				t.Fatalf("organisation-role options = %#v", options)
			}
			for index, want := range wantLabels {
				if options[index].Label != want {
					t.Errorf("organisation-role option %d = %q, want %q", index, options[index].Label, want)
				}
			}
			return []string{string(profile.RoleImporter), string(profile.RoleDistributor)}, nil
		},
	}
	updated, err := collectBasicSystemProfile(prompt, ".", time.Now(), setupRepositorySummary{}, newSetupProfileDraft(), &existing)
	if err != nil {
		t.Fatal(err)
	}
	if singleCalls != 3 || multipleCalls != 2 || updated.Name != existing.Name || updated.IntendedPurpose != existing.IntendedPurpose ||
		len(updated.OperatingRegions) != 1 || updated.OperatingRegions[0] != profile.RegionEU ||
		len(updated.OrganizationRoles) != 2 || updated.OrganizationRoles[0] != profile.RoleImporter || updated.OrganizationRoles[1] != profile.RoleDistributor ||
		updated.DecisionImpact != profile.ImpactAdvisory || updated.LifecycleStage != profile.LifecycleProduction ||
		updated.HumanOversight != profile.OversightRequired {
		t.Fatalf("rerun changed existing answers: %#v", updated)
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
	model, err := promptOllamaModel(prompt, defaultSetupModel, []ollamaInstalledModel{{tag: "codestral:22b", sizeGB: 12.4}})
	if err != nil {
		t.Fatal(err)
	}
	if model != "account-model:latest" {
		t.Fatalf("model = %q", model)
	}
	if len(choices) != 5 || choices[0].Value != "codestral:22b" || !strings.HasPrefix(choices[0].Label, "Installed · ") ||
		choices[1].Value != defaultSetupModel || !strings.HasPrefix(choices[1].Label, "Suggested download · ") ||
		choices[len(choices)-2].Value != catalogueModelChoice || !strings.HasPrefix(choices[len(choices)-2].Label, "Ollama catalogue · ") ||
		choices[len(choices)-1].Value != customModelChoice || !strings.HasPrefix(choices[len(choices)-1].Label, "Exact tag · ") {
		t.Fatalf("terminal choices = %#v", choices)
	}
	for _, choice := range choices {
		if choice.Value != customModelChoice && choice.Value != catalogueModelChoice && (!strings.Contains(choice.Label, "GB") || (!strings.HasPrefix(choice.Label, "Installed · ") && !strings.HasPrefix(choice.Label, "Suggested download · "))) {
			t.Errorf("model choice does not show a GB size: %#v", choice)
		}
	}
	for _, expected := range []string{"codestral:22b", defaultSetupModel, "qwen3:8b"} {
		found := false
		for _, choice := range choices {
			if choice.Value == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("terminal choices missing %q: %#v", expected, choices)
		}
	}
	if !strings.Contains(output.String(), "Custom Ollama model tag") || strings.Contains(output.String(), "1)") {
		t.Fatalf("custom-model output = %q", output.String())
	}
}

func TestPromptOllamaModelGroupsInstalledModelsBeforeDownloads(t *testing.T) {
	var choices []terminalChoice
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &bytes.Buffer{},
		selectOne: func(_ string, defaultValue string, options []terminalChoice) (string, error) {
			choices = append([]terminalChoice(nil), options...)
			return defaultValue, nil
		},
	}
	installed := []ollamaInstalledModel{{tag: defaultSetupModel, sizeGB: 6.6}}
	model, err := promptOllamaModel(prompt, defaultSetupModel, installed)
	if err != nil {
		t.Fatal(err)
	}
	if model != defaultSetupModel {
		t.Fatalf("model = %q", model)
	}
	if len(choices) != 4 || choices[0].Value != defaultSetupModel || choices[1].Value != "qwen3:8b" {
		t.Fatalf("choices = %#v", choices)
	}
	if !strings.HasPrefix(choices[0].Label, "Installed · ") || !strings.HasPrefix(choices[1].Label, "Suggested download · ") || choices[2].Value != catalogueModelChoice {
		t.Fatalf("model categories = %#v", choices[:3])
	}
	if choices[len(choices)-1].Value != customModelChoice {
		t.Fatalf("custom choice is not last: %#v", choices)
	}
}

func TestCommonOllamaModelsAreCuratedExactLocalTags(t *testing.T) {
	expected := []string{
		"qwen3.5:9b", "qwen3.5:4b", "llama3.2:3b", "gemma3:4b",
		"qwen2.5-coder:7b", "qwen3-coder:30b", "deepseek-coder-v2:16b", "codestral:22b",
		"gemma3:12b", "mistral:7b", "deepseek-r1:8b", "phi4:14b", "gpt-oss:20b", "qwen3.5:27b",
	}
	models := commonOllamaModels()
	if len(models) != len(expected) {
		t.Fatalf("models = %#v", models)
	}
	seen := make(map[string]bool, len(models))
	for index, model := range models {
		if model.tag != expected[index] || !strings.Contains(model.tag, ":") || model.downloadSizeGB <= 0 || model.category == "" || isOllamaCloudTag(model.tag) || seen[model.tag] {
			t.Fatalf("curated model %d = %#v", index, model)
		}
		seen[model.tag] = true
	}
}

func TestPromptOllamaCatalogueShowsCommonModelsWithoutSearching(t *testing.T) {
	var calls int
	var catalogueChoices []terminalChoice
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &bytes.Buffer{},
		selectOne: func(label, _ string, options []terminalChoice) (string, error) {
			calls++
			if calls == 1 {
				if label != "Ollama model" {
					t.Fatalf("main label = %q", label)
				}
				return catalogueModelChoice, nil
			}
			if label != "Common Ollama models" {
				t.Fatalf("catalogue label = %q", label)
			}
			catalogueChoices = append([]terminalChoice(nil), options...)
			return "llama3.2:3b", nil
		},
	}
	model, err := promptOllamaModel(prompt, defaultSetupModel, []ollamaInstalledModel{{tag: "gemma3:4b", sizeGB: 3.2}})
	if err != nil {
		t.Fatal(err)
	}
	if model != "llama3.2:3b" || calls != 2 {
		t.Fatalf("model=%q calls=%d", model, calls)
	}
	for _, expected := range []string{
		"Recommended · qwen3.5:9b · 6.6 GB",
		"Small · llama3.2:3b · 2.0 GB",
		"Installed · gemma3:4b · 3.2 GB · Small",
		"Coding · qwen2.5-coder:7b · 4.7 GB",
		"Reasoning · gpt-oss:20b · 14.0 GB",
		"Large · qwen3.5:27b · 17.0 GB",
	} {
		found := false
		for _, choice := range catalogueChoices {
			if choice.Label == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("catalogue missing %q: %#v", expected, catalogueChoices)
		}
	}
	if catalogueChoices[len(catalogueChoices)-1].Value != customModelChoice {
		t.Fatalf("exact-tag option is not last: %#v", catalogueChoices)
	}
}

func TestOllamaInstalledModelsParsesListOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "ollama")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'NAME ID SIZE MODIFIED\\nqwen3:8b abc 5.2 GB now\\nQWEN3:8B duplicate 5.2 GB now\\ncodestral:22b def 12GB now\\nsmall:latest ghi 950 MB now\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	models := ollamaInstalledModels(context.Background(), executable)
	if len(models) != 3 || models[0].tag != "qwen3:8b" || models[0].sizeGB != 5.2 || models[1].tag != "codestral:22b" || models[1].sizeGB != 12 || models[2].tag != "small:latest" || math.Abs(models[2].sizeGB-0.95) > 0.0001 {
		t.Fatalf("models = %#v", models)
	}
}

func TestOllamaResourceEstimateUsesTransparentRanges(t *testing.T) {
	tests := map[string]string{
		"qwen3.5:9b":            "5–8 GB",
		"qwen2.5-coder:7b":      "4–6 GB",
		"deepseek-coder-v2:16b": "9–13 GB",
		"codestral:22b":         "12–16 GB",
		"qwen3-coder:30b":       "18–24 GB",
		"custom:latest":         "model- and quantization-dependent",
	}
	for model, expected := range tests {
		if estimate := ollamaResourceEstimate(model); !strings.Contains(estimate, expected) || !strings.Contains(estimate, "memory") {
			t.Errorf("estimate for %q = %q", model, estimate)
		}
	}
}

func TestInteractiveSetupCreatesRepositoryProfileAndSelectsExperimentalLocalReview(t *testing.T) {
	target := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		"2", "", // experimental local analysis, Ollama model
		"1", // EU technical mapping
		"",  // save configuration
	}, "\n") + "\n")
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
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != defaultSetupModel || !cfg.AI.ReviewOnScan {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
	if cfg.Systems[0].LifecycleStage != profile.LifecycleUnknown {
		t.Fatalf("lifecycle answer = %q", cfg.Systems[0].LifecycleStage)
	}
	for _, expected := range []string{"ComplyScan setup", "Step 1 of 4 — Repository inspection", "Step 2 of 4 — Optional AI assistance and privacy", "Automatic report target", "Step 3 of 4 — Framework selection", "Step 4 of 4 — Confirm, save, and scan", "Repository inspected", "No model is used in this step", "Experimental local model setup", "Save configuration only", "Saved", "Next: complyscan scan"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, stdout.String())
		}
	}
	for _, expected := range []string{"scan infers code-visible facts", "Organisation, market, contractual, and legal-applicability facts remain unknown in the report", "Experimental local AI assistance — Ollama", "no local model is currently approved"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("short setup output missing explanation %q:\n%s", expected, stdout.String())
		}
	}
	for _, unexpected := range []string{"System questionnaire", "Organisation roles", "Decision impact", "Human oversight", "AI activities", "Processes personal data", "Factual profile reviewer", "Questionnaire preparation", "Drafting setup answers"} {
		if strings.Contains(stdout.String(), unexpected) {
			t.Errorf("short setup unexpectedly included %q:\n%s", unexpected, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "comma-separated: eu-ai-act-technical-evidence") {
		t.Errorf("guided setup exposed internal framework IDs:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Setup guidance") || strings.Contains(stdout.String(), "guidance detail") {
		t.Errorf("guided setup unexpectedly asked for a global guidance mode:\n%s", stdout.String())
	}
	ignored, err := os.ReadFile(filepath.Join(target, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignored), reportGitIgnoreEntry) {
		t.Fatalf("generated reports are not ignored:\n%s", ignored)
	}
}

func TestFastSetupCreatesAutomaticReportTargetWithoutModel(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "requirements.txt"), []byte("openai==2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("\n") // save configuration
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
	if len(cfg.Systems) != 1 || len(cfg.Systems[0].AIActivities) != 1 || cfg.Systems[0].AIActivities[0] != profile.ActivityUnknown {
		t.Fatalf("systems = %#v", cfg.Systems)
	}
	if cfg.AI.Provider != "none" || cfg.AI.ReviewOnScan || !strings.Contains(stdout.String(), "scan infers code-visible facts") || strings.Contains(stdout.String(), "Drafting setup answers") || strings.Contains(stdout.String(), "System questionnaire") {
		t.Fatalf("fast setup output:\n%s", stdout.String())
	}
}

func TestTechnicalMappingChangePreservesReviewedEUDecision(t *testing.T) {
	system := profile.NewDraftSystem("example", "Example")
	system.Applicability = []profile.ApplicabilityDecision{{
		Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityApplicable,
		Rationale: "Reviewed deployment facts.", ReviewedBy: "Reviewer", ReviewedAt: "2026-08-12",
	}}
	applyFrameworksToSystem(&system, []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID})
	if len(system.Applicability) != 1 || system.Applicability[0].Status != profile.ApplicabilityApplicable {
		t.Fatalf("reviewed applicability decision was replaced: %#v", system.Applicability)
	}
	applyFrameworksToSystem(&system, []string{framework.NISTAIRMFTechnicalEvidencePackID})
	if len(system.Applicability) != 0 {
		t.Fatalf("EU applicability decision remained after EU mapping was disabled: %#v", system.Applicability)
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

func TestSetupInspectionExcludesActiveConfig(t *testing.T) {
	target := t.TempDir()
	configPath := filepath.Join(target, config.FileName)
	if err := config.Write(configPath, config.Default(), false); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(target, ".github", "workflows", "complyscan.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("name: ComplyScan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	summary, err := inspectRepositoryForSetup(context.Background(), promptSession{output: &output}, target, configPath, config.Default(), testBuild)
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool, len(summary.Discovery.Repository.Files))
	for _, file := range summary.Discovery.Repository.Files {
		paths[file.Path] = true
	}
	if paths[config.FileName] || !paths[".github/workflows/complyscan.yml"] {
		t.Fatalf("setup discovery paths = %#v", paths)
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
	for _, expected := range []string{"Technical context — 3 questions", "Conditional EU follow-up — 6 questions", "Question 1 of 6 — Users", "Question 2 of 6 — Potentially affected groups", "Question 3 of 6 — Processes personal data", "Question 6 of 6 — Factual profile reviewer", "Applicability readiness gate: human-reviewed"} {
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
	for _, unexpected := range []string{"Users (comma-separated)", "Potentially affected groups (comma-separated)", "Processes personal data"} {
		if strings.Contains(output.String(), unexpected) {
			t.Errorf("output unexpectedly contains %q:\n%s", unexpected, output.String())
		}
	}
	if !strings.Contains(output.String(), "Applicability readiness gate: factually-ready") {
		t.Errorf("readiness gate missing:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Conditional EU follow-up — 1 question") || !strings.Contains(output.String(), "Question 1 of 1 — Factual profile reviewer") {
		t.Errorf("conditional progress missing:\n%s", output.String())
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
	input := strings.NewReader("\n") // save configuration
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
	if !strings.Contains(stdout.String(), "NIST AI RMF is") || !strings.Contains(stdout.String(), "voluntary guidance") {
		t.Fatalf("framework explanation missing:\n%s", stdout.String())
	}
}

func TestPromptFrameworkSelectionUsesHumanReadableMultiSelect(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		defaults []string
		want     string
	}{
		{name: "fresh selection required", input: "\n1\n", want: "eu-ai-act-technical-evidence"},
		{name: "EU", input: "1\n", want: "eu-ai-act-technical-evidence"},
		{name: "NIST", input: "2\n", want: "nist-ai-rmf-technical-evidence"},
		{name: "both", input: "1,2\n", want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
		{name: "configured NIST default", input: "\n", defaults: []string{"nist-ai-rmf-technical-evidence"}, want: "nist-ai-rmf-technical-evidence"},
		{name: "configured both default", input: "\n", defaults: []string{"eu-ai-act-technical-evidence", "nist-ai-rmf-technical-evidence"}, want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
		{name: "invalid then both", input: "3\n1,2\n", want: "eu-ai-act-technical-evidence,nist-ai-rmf-technical-evidence"},
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
			for _, expected := range []string{"1) EU AI Act", "2) NIST AI RMF", "Technical evidence packs numbers (comma-separated)"} {
				if !strings.Contains(strings.ReplaceAll(output.String(), ", ", ","), expected) {
					t.Errorf("picker output missing %q:\n%s", expected, output.String())
				}
			}
			if strings.Contains(output.String(), "Both —") {
				t.Errorf("picker still contains a synthetic Both option:\n%s", output.String())
			}
			if test.name == "fresh selection required" && !strings.Contains(output.String(), "answer required") {
				t.Errorf("fresh selection did not require an explicit answer:\n%s", output.String())
			}
			if test.name == "invalid then both" && !strings.Contains(output.String(), "Enter one or more numbers from 1 to 2, separated by commas.") {
				t.Errorf("invalid selection did not explain valid choices:\n%s", output.String())
			}
		})
	}
}

func TestPromptFrameworkSelectionUsesTerminalCheckboxes(t *testing.T) {
	called := false
	prompt := promptSession{
		output: io.Discard,
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			called = true
			if label != "Technical evidence packs" || len(defaults) != 0 {
				t.Fatalf("selector label=%q defaults=%#v", label, defaults)
			}
			if len(options) != 2 || strings.Contains(options[0].Label, "Both") || strings.Contains(options[1].Label, "Both") || len(exclusive) != 0 {
				t.Fatalf("selector options=%#v exclusive=%#v", options, exclusive)
			}
			return []string{options[0].Value, options[1].Value}, nil
		},
	}
	selected, err := promptFrameworkSelection(prompt, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !called || strings.Join(selected, ",") != framework.EUAIActTechnicalEvidencePackID+","+framework.NISTAIRMFTechnicalEvidencePackID {
		t.Fatalf("selected=%#v called=%t", selected, called)
	}
}

func TestConfigureFrameworkSelectionDoesNotPreselectSavedMappings(t *testing.T) {
	cfg := config.Default()
	cfg.Frameworks = []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID}
	prompt := promptSession{
		output: io.Discard, backAvailable: true,
		selectMany: func(_ string, defaults []string, options []terminalChoice, _ []string) ([]string, error) {
			if len(defaults) != 0 {
				t.Fatalf("saved mappings were preselected: %#v", defaults)
			}
			if !containsTerminalChoice(options, backChoiceValue) {
				t.Fatalf("framework picker has no back control: %#v", options)
			}
			return []string{options[1].Value}, nil
		},
	}
	if err := configureFrameworkSelection(prompt, &cfg, true, nil); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Frameworks) != 1 || cfg.Frameworks[0] != framework.NISTAIRMFTechnicalEvidencePackID {
		t.Fatalf("frameworks = %#v", cfg.Frameworks)
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
			for _, expected := range []string{"1) alpha", "2) beta", "3) gamma", "Select example (1-3)", "Proposed answer", "2", "enter accept"} {
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

func TestPromptRequiredChoiceHasNoPreselectedAnswer(t *testing.T) {
	var output bytes.Buffer
	called := false
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			called = true
			if label != "Lifecycle stage" || defaultValue != requiredAnswerChoiceValue {
				t.Fatalf("selector arguments: label=%q default=%q", label, defaultValue)
			}
			if len(options) != 3 || options[0].Value != requiredAnswerChoiceValue || options[1].Value != string(profile.LifecycleDevelopment) {
				t.Fatalf("selector options = %#v", options)
			}
			return string(profile.LifecycleUnknown), nil
		},
	}
	selected, err := promptRequiredChoice(prompt, "Lifecycle stage", profile.LifecycleDevelopment, profile.LifecycleUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if !called || selected != profile.LifecycleUnknown {
		t.Fatalf("selected = %q; called=%t", selected, called)
	}
}

func TestDecisionImpactChoicesExplainTheirMeaningInline(t *testing.T) {
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: io.Discard, guidance: &questionGuidance{},
		selectOne: func(label, _ string, options []terminalChoice) (string, error) {
			if label != "Step 3 of 5 · Question 5 of 7 — Decision impact" {
				t.Fatalf("label = %q", label)
			}
			want := []string{
				"advisory — AI suggests or drafts; a person independently reviews before any action",
				"low — AI affects a limited, readily reversible workflow without materially affecting a person's access, safety, or opportunities",
				"significant — AI materially influences eligibility, ranking, access, employment, education, credit, healthcare, safety, or a similarly important outcome",
				"autonomous — AI can take or trigger a consequential action without prior meaningful human approval",
				"unknown — the real downstream use of outputs has not been established",
			}
			if len(options) != len(want)+1 {
				t.Fatalf("options = %#v", options)
			}
			for index, expected := range want {
				if options[index+1].Label != expected {
					t.Errorf("option %d label = %q, want %q", index, options[index+1].Label, expected)
				}
			}
			return string(profile.ImpactAdvisory), nil
		},
		step:      &setupStepProgress{current: 3, total: 5},
		questions: &questionProgress{current: 4, total: 7},
	}
	if err := explainSetupQuestion(prompt, "decision-impact"); err != nil {
		t.Fatal(err)
	}
	selected, err := promptRequiredChoice(prompt, "Decision impact",
		profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if selected != profile.ImpactAdvisory {
		t.Fatalf("selected = %q", selected)
	}
}

func TestLifecycleStageChoicesExplainTheirMeaningInline(t *testing.T) {
	guidance := &questionGuidance{choiceDescriptions: setupChoiceDescriptions(setupQuestionHelp["lifecycle-stage"][1:])}
	want := map[profile.LifecycleStage]string{
		profile.LifecycleDevelopment: "development — being designed or implemented; not used with real users in normal operation",
		profile.LifecycleTesting:     "testing — being validated, piloted, or used in a controlled pre-production environment",
		profile.LifecycleProduction:  "production — available or used in normal real-world operation",
		profile.LifecycleRetired:     "retired — no longer used, although records or obligations may remain",
		profile.LifecycleUnknown:     "unknown — the current stage has not been established",
	}
	for value, expected := range want {
		if actual := setupChoiceLabel(guidance, string(value)); actual != expected {
			t.Errorf("label for %q = %q, want %q", value, actual, expected)
		}
	}
}

func TestHumanOversightChoicesExplainTheirMeaningInline(t *testing.T) {
	guidance := &questionGuidance{choiceDescriptions: setupChoiceDescriptions(setupQuestionHelp["human-oversight"][1:])}
	want := map[profile.HumanOversight]string{
		profile.OversightRequired:  "required — the relevant output or action is blocked until a person reviews it",
		profile.OversightAvailable: "available — a person can monitor, override, or stop it, but review is not required for every output",
		profile.OversightLimited:   "limited — intervention exists only for some cases, users, stages, or after the action",
		profile.OversightNone:      "none — no person can effectively review, override, or stop the relevant AI behavior",
		profile.OversightUnknown:   "unknown — the operational workflow has not been confirmed",
	}
	for value, expected := range want {
		if actual := setupChoiceLabel(guidance, string(value)); actual != expected {
			t.Errorf("label for %q = %q, want %q", value, actual, expected)
		}
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
			for _, expected := range []string{"1) alpha", "2) beta", "3) gamma", "Examples numbers (comma-separated)", "Proposed answer", "2,3", "enter accept"} {
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

func TestPromptRequiredChoicesHaveNoPreselectedAnswers(t *testing.T) {
	var output bytes.Buffer
	called := false
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			called = true
			if label != "AI activities" || len(defaults) != 0 {
				t.Fatalf("selector arguments: label=%q defaults=%#v", label, defaults)
			}
			if len(options) != 2 || options[0].Value != string(profile.ActivityInference) || strings.Join(exclusive, ",") != "unknown" {
				t.Fatalf("selector options=%#v exclusive=%#v", options, exclusive)
			}
			return []string{string(profile.ActivityInference)}, nil
		},
	}
	selected, err := promptRequiredChoices(prompt, "AI activities", profile.ActivityInference, profile.ActivityUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if !called || len(selected) != 1 || selected[0] != profile.ActivityInference {
		t.Fatalf("selected = %#v; called=%t", selected, called)
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

func TestWriteSetupReviewSummaryShowsDecisionsBeforeSave(t *testing.T) {
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote.Model = "gpt-test"
	cfg.Frameworks = []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID}
	cfg.Systems = []profile.System{{ID: "checkout-ai", Name: "Checkout assistant"}}
	var output bytes.Buffer
	prompt := promptSession{output: &output}
	if err := writeSetupReviewSummary(prompt, cfg, true); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Review setup", "[READY] Optional AI assistance: OpenAI cloud — gpt-test", "EU AI Act technical evidence",
		"NIST AI RMF technical evidence", "Checkout assistant (checkout-ai)",
		"all repository evidence maps to the single report target",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("review summary missing %q:\n%s", expected, output.String())
		}
	}
}

func TestSetupStatusUsesSemanticTextFallback(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{output: &output}
	for _, test := range []struct {
		kind setupStatusKind
		want string
	}{
		{setupStatusReady, "[READY] Model is available"},
		{setupStatusReview, "[NEEDS REVIEW] Profile is still a draft"},
		{setupStatusMissing, "[NOT CONFIGURED] Ownership rules"},
	} {
		if err := prompt.status(test.kind, strings.TrimPrefix(test.want, map[setupStatusKind]string{
			setupStatusReady: "[READY] ", setupStatusReview: "[NEEDS REVIEW] ", setupStatusMissing: "[NOT CONFIGURED] ",
		}[test.kind])); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), test.want) {
			t.Fatalf("status output missing %q: %s", test.want, output.String())
		}
	}
}

func TestReviewSetupOffersOnlyFirstRunOutcomes(t *testing.T) {
	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output, backAvailable: true,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Finish setup" || defaultValue != string(setupReviewRunScan) {
				t.Fatalf("selector label=%q default=%q", label, defaultValue)
			}
			if !containsTerminalChoice(options, backChoiceValue) {
				t.Fatalf("finish menu has no back control: %#v", options)
			}
			visible := visibleTerminalChoices(options)
			if len(visible) != 2 || visible[0].Value != string(setupReviewRunScan) || visible[1].Value != string(setupReviewSaveOnly) {
				t.Fatalf("finish menu options = %#v", visible)
			}
			return string(setupReviewSaveOnly), nil
		},
	}
	mode := setupScanNone
	ready := true
	save, err := reviewSetupBeforeSave(context.Background(), prompt, &output, t.TempDir(), &cfg, setupOptions{}, setupRepositorySummary{}, &mode, &ready)
	if err != nil {
		t.Fatal(err)
	}
	if !save || mode != setupScanNone {
		t.Fatalf("save=%t mode=%q", save, mode)
	}
}

func TestReviewSetupChoosesFirstRunActionWhileConfirming(t *testing.T) {
	cfg := config.Default()
	cfg.AI.Provider = "ollama"
	var output bytes.Buffer
	prompt := promptSession{
		output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Finish setup" || defaultValue != string(setupReviewRunScan) {
				t.Fatalf("selector label=%q default=%q", label, defaultValue)
			}
			values := make([]string, len(options))
			for index, option := range options {
				values[index] = option.Value
			}
			if !strings.Contains(strings.Join(values, "\n"), string(setupReviewRunScan)) || !strings.Contains(strings.Join(values, "\n"), string(setupReviewSaveOnly)) {
				t.Fatalf("finish actions = %#v", values)
			}
			return string(setupReviewRunScan), nil
		},
	}
	mode := setupScanNone
	ready := true
	save, err := reviewSetupBeforeSave(context.Background(), prompt, &output, t.TempDir(), &cfg, setupOptions{}, setupRepositorySummary{}, &mode, &ready)
	if err != nil {
		t.Fatal(err)
	}
	if !save || mode != setupScanQuick {
		t.Fatalf("save=%t mode=%q", save, mode)
	}
	if !strings.Contains(output.String(), "first scan will run deterministic checks and the configured advisory AI review") || !strings.Contains(output.String(), "deterministic report will still finish") {
		t.Fatalf("unified first-scan behavior missing:\n%s", output.String())
	}
}

func TestEverySetupQuestionHasDeveloperGuidance(t *testing.T) {
	keys := []string{
		"resume-setup", "applicability-context", "frameworks", "system-id", "system-name", "intended-purpose", "lifecycle-stage", "organization-roles",
		"operating-regions", "use-case-domains", "users", "affected-groups", "decision-impact",
		"human-oversight", "ai-activities", "personal-data", "special-category-data", "children-data",
		"deployment-models", "profile-reviewer", "applicability-decision", "decision-rationale",
		"applicability-reviewer", "replace-profile", "review-provider", "ollama-model", "install-ollama",
		"path-ownership", "ownership-paths", "ownership-systems", "replace-ownership", "download-model",
		"remote-disclosure", "remote-provider-name", "remote-base-url", "remote-model", "api-key-env", "first-scan", "scan-mode",
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

func TestRemoteDisclosureExplainsPersistentScanConsent(t *testing.T) {
	disclosure := strings.Join(setupQuestionHelp["remote-disclosure"], " ")
	for _, expected := range []string{
		"Later cloud-assisted `complyscan scan` runs", "Setup itself does not send repository source", "one or more bounded source requests",
		"source and synthesis batches may run concurrently", fmt.Sprintf("%d provider requests", repositoryanalysis.MaxProviderRequestsPerRun),
		"variable cost", "future scans without another prompt", "--deterministic-only",
	} {
		if !strings.Contains(disclosure, expected) {
			t.Fatalf("remote disclosure is missing %q: %s", expected, disclosure)
		}
	}
}

func TestSetupGuidanceShowsNonChoiceDetailsByDefault(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("Confirmed purpose\n")),
		output: &output, guidance: &questionGuidance{},
	}
	if err := explainSetupQuestion(prompt, "intended-purpose"); err != nil {
		t.Fatal(err)
	}
	normalizedOutput := strings.Join(strings.Fields(output.String()), " ")
	for _, line := range setupQuestionHelp["intended-purpose"] {
		if !strings.Contains(normalizedOutput, strings.Join(strings.Fields(line), " ")) {
			t.Errorf("default guidance missing %q", line)
		}
	}
	value, err := prompt.text("Intended purpose", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Confirmed purpose" || strings.Contains(output.String(), "More guidance") || strings.Contains(output.String(), "? details") {
		t.Fatalf("value=%q output:\n%s", value, output.String())
	}
}

func TestTerminalQuestionShowsInlineGuidanceWithoutSeparateOption(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{},
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			selections++
			if label != "Lifecycle stage" || len(options) != 2 || !strings.Contains(options[0].Label, "being designed or implemented") || !strings.Contains(options[1].Label, "has not been established") {
				t.Fatalf("question selector label=%q options=%#v", label, options)
			}
			return options[0].Value, nil
		},
	}
	if err := explainSetupQuestion(prompt, "lifecycle-stage"); err != nil {
		t.Fatal(err)
	}
	selected, err := promptChoice(prompt, "Lifecycle stage", profile.LifecycleDevelopment, profile.LifecycleDevelopment, profile.LifecycleUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if selected != profile.LifecycleDevelopment || selections != 1 || strings.Contains(output.String(), "Further explanation") {
		t.Fatalf("selected=%q selections=%d output:\n%s", selected, selections, output.String())
	}
}

func TestTerminalMultiSelectShowsInlineGuidanceWithoutSeparateOption(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{},
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			selections++
			if label != "Operating regions" || len(options) != 2 || !strings.Contains(options[0].Label, "European Union countries") || !strings.Contains(options[1].Label, "have not been established") {
				t.Fatalf("multi-selector label=%q options=%#v", label, options)
			}
			return []string{string(profile.RegionUnknown)}, nil
		},
	}
	if err := explainSetupQuestion(prompt, "operating-regions"); err != nil {
		t.Fatal(err)
	}
	selected, err := promptChoices(prompt, "Operating regions", []profile.OperatingRegion{profile.RegionUnknown}, profile.RegionEU, profile.RegionUnknown)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0] != profile.RegionUnknown || selections != 1 || strings.Contains(output.String(), "Further explanation") {
		t.Fatalf("selected=%#v selections=%d output:\n%s", selected, selections, output.String())
	}
}

func TestTerminalConfirmationShowsGuidanceWithoutSeparateOption(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{}, backAvailable: true,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			selections++
			if label != "Install Ollama" || defaultValue != "Yes" || len(options) != 3 || options[2].Value != backChoiceValue {
				t.Fatalf("confirmation selector label=%q default=%q options=%#v", label, defaultValue, options)
			}
			return "No", nil
		},
	}
	if err := explainSetupQuestion(prompt, "install-ollama"); err != nil {
		t.Fatal(err)
	}
	confirmed, err := prompt.confirm("Install Ollama", true)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed || selections != 1 || !strings.Contains(output.String(), "Choose no to keep deterministic scanning") || strings.Contains(output.String(), "Further explanation") {
		t.Fatalf("confirmed=%t selections=%d output:\n%s", confirmed, selections, output.String())
	}
}

func TestTerminalTextQuestionShowsGuidanceByDefault(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("Product name\n")),
		output: &output, guidance: &questionGuidance{},
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			return "", errors.New("not used by text prompts")
		},
	}
	if err := explainSetupQuestion(prompt, "system-name"); err != nil {
		t.Fatal(err)
	}
	value, err := prompt.text("System name", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Product name" || !strings.Contains(output.String(), "Use the product name") || strings.Contains(output.String(), "More guidance") || strings.Contains(output.String(), "? details") {
		t.Fatalf("value=%q output:\n%s", value, output.String())
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
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != "local-test-model" || !cfg.AI.ReviewOnScan {
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
	if cfg.AI.Provider != "openai" || cfg.AI.Remote.Model != "gpt-test" || !cfg.AI.ReviewOnScan {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
	authorized, err := automaticReviewAuthorized(target, filepath.Join(target, config.FileName), cfg.AI)
	if err != nil || !authorized {
		t.Fatalf("setup did not persist matching machine-local consent: authorized=%t err=%v", authorized, err)
	}
}

func TestSetupWithNoProviderRevokesMachineLocalConsent(t *testing.T) {
	target := t.TempDir()
	t.Setenv("COMPLYSCAN_REVOKE_TEST_KEY", "test-secret-value")
	var stdout, stderr bytes.Buffer
	arguments := []string{
		"setup", "--non-interactive", "--review", "openai", "--allow-remote-review",
		"--model", "gpt-test", "--api-key-env", "COMPLYSCAN_REVOKE_TEST_KEY", "--skip-scan", target,
	}
	if code := executeWithInput(arguments, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("enable setup code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	configured, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if authorized, err := automaticReviewAuthorized(target, filepath.Join(target, config.FileName), configured.AI); err != nil || !authorized {
		t.Fatalf("authorization before revoke = %t, %v", authorized, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := executeWithInput([]string{"setup", "--non-interactive", "--review", "none", "--skip-scan", target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("disable setup code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	store, err := defaultReviewConsentStore()
	if err != nil {
		t.Fatal(err)
	}
	if authorized, err := store.Authorized(target, filepath.Join(target, config.FileName), configured.AI); err != nil || authorized {
		t.Fatalf("authorization after provider none = %t, %v", authorized, err)
	}
}

func TestNonInteractiveSetupConfiguresCustomCompatibleProvider(t *testing.T) {
	target := t.TempDir()
	t.Setenv("COMPLYSCAN_TEST_GATEWAY_KEY", "test-secret-value")
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{
		"setup", "--non-interactive", "--review", customCompatibleProvider, "--allow-remote-review",
		"--provider-name", "Acme model gateway", "--base-url", "https://models.example.com/v1",
		"--model", "acme-review-v2", "--api-key-env", "COMPLYSCAN_TEST_GATEWAY_KEY", "--skip-scan", target,
	}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	cfg, err := config.Load(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != customCompatibleProvider || cfg.AI.Remote.ProviderName != "Acme model gateway" || cfg.AI.Remote.BaseURL != "https://models.example.com/v1" || cfg.AI.Remote.Model != "acme-review-v2" || !cfg.AI.ReviewOnScan {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
	data, err := os.ReadFile(filepath.Join(target, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-secret-value") {
		t.Fatalf("credential leaked into configuration:\n%s", data)
	}
}

func TestPromptRemoteModelOffersOnlyProviderShortlist(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("1\n")), output: &output}
	model, err := promptRemoteModel(prompt, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if model != "claude-opus-5" || !strings.Contains(output.String(), "claude-sonnet-5") {
		t.Fatalf("model = %q; output:\n%s", model, output.String())
	}
	prompt = promptSession{reader: bufio.NewReader(strings.NewReader("2\n")), output: &output}
	model, err = promptRemoteModel(prompt, "gemini")
	if err != nil || model != "gemini-3.6-flash" {
		t.Fatalf("shortlisted model = %q, error = %v", model, err)
	}
}

func TestPromptRemoteModelUsesTerminalSelectorWithoutCustomEntry(t *testing.T) {
	var output bytes.Buffer
	var choices []terminalChoice
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Remote model" || defaultValue != "claude-opus-5" {
				t.Fatalf("selector arguments: label=%q default=%q", label, defaultValue)
			}
			choices = append([]terminalChoice(nil), options...)
			return "claude-sonnet-5", nil
		},
	}
	model, err := promptRemoteModel(prompt, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if model != "claude-sonnet-5" {
		t.Fatalf("model = %q", model)
	}
	if len(choices) != 2 || choices[0].Value != "claude-opus-5" || choices[1].Value != "claude-sonnet-5" || containsTerminalChoice(choices, customModelChoice) {
		t.Fatalf("terminal choices = %#v", choices)
	}
	for _, choice := range choices {
		if !strings.Contains(choice.Label, "live quality gates pending") {
			t.Errorf("benchmark status missing from %#v", choice)
		}
	}
}

func TestPromptRemoteModelCatalogueUsesAccountModelsAndSearchableSelector(t *testing.T) {
	previous := listRemoteModels
	listRemoteModels = func(_ context.Context, options providers.ModelListOptions) ([]providers.RemoteModel, error) {
		if options.Provider != providers.Anthropic || options.APIKey != "test-key" || options.Label != "Anthropic" {
			t.Fatalf("list options = %#v", options)
		}
		return []providers.RemoteModel{
			{ID: "claude-opus-5", DisplayName: "Claude Opus 5"},
			{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5"},
		}, nil
	}
	t.Cleanup(func() { listRemoteModels = previous })
	var choices []terminalChoice
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "Hosted model" || defaultValue != "claude-opus-5" {
				t.Fatalf("selector label=%q default=%q", label, defaultValue)
			}
			choices = append([]terminalChoice(nil), options...)
			return "claude-opus-5", nil
		},
	}
	model, err := promptRemoteModelCatalogue(context.Background(), prompt, "anthropic", config.RemoteConfig{ProviderName: "Anthropic"}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if model != "claude-opus-5" || len(choices) != 2 || !strings.Contains(choices[0].Label, "Claude Opus 5 · claude-opus-5") || containsTerminalChoice(choices, customModelChoice) {
		t.Fatalf("model=%q choices=%#v", model, choices)
	}
	if !strings.Contains(output.String(), "Found 2 shortlisted model(s) available to this API key") || strings.Contains(output.String(), "Use / to filter") {
		t.Fatalf("output:\n%s", output.String())
	}
}

func TestPromptRemoteModelCatalogueFallsBackWhenDiscoveryUnavailable(t *testing.T) {
	previous := listRemoteModels
	listRemoteModels = func(context.Context, providers.ModelListOptions) ([]providers.RemoteModel, error) {
		return nil, errors.New("endpoint does not provide model discovery")
	}
	t.Cleanup(func() { listRemoteModels = previous })
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, _ []terminalChoice) (string, error) {
			if label != "Remote model" {
				t.Fatalf("fallback label = %q", label)
			}
			return defaultValue, nil
		},
	}
	model, err := promptRemoteModelCatalogue(context.Background(), prompt, "openai", config.RemoteConfig{ProviderName: "OpenAI"}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.6-sol" || !strings.Contains(output.String(), "Live model catalogue unavailable") || !strings.Contains(output.String(), "ComplyScan model shortlist") {
		t.Fatalf("model=%q output:\n%s", model, output.String())
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
