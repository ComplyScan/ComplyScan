package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
				if label != "Analysis mode" || defaultValue != "Hosted AI provider — uses your API key and sends bounded context externally" || len(options) != 3 {
					t.Fatalf("analysis selector: label=%q default=%q options=%#v", label, defaultValue, options)
				}
				return options[1].Value, nil
			case 2:
				if label != "Hosted provider" || defaultValue != "anthropic" || len(options) != 9 || options[3].Value != "xai" || options[4].Value != "mistral" || options[5].Value != "groq" || options[5].Label != "GroqCloud — fast hosted models from several model makers" || options[6].Value != "openrouter" || options[7].Value != customCompatibleProvider || options[8].Value != backChoiceValue {
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

func TestPromptAnalysisProviderCanReturnFromHostedProvider(t *testing.T) {
	var calls int
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &bytes.Buffer{}, guidance: &questionGuidance{},
		selectOne: func(label, _ string, options []terminalChoice) (string, error) {
			calls++
			switch calls {
			case 1:
				return options[1].Value, nil // Hosted AI provider.
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
	system, err := collectBasicSystemProfile(prompt, ".", time.Now(), setupRepositorySummary{}, setupProfileDraft{})
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

func TestInteractiveSetupCreatesRepositoryProfileAndSelectsLocalReview(t *testing.T) {
	target := t.TempDir()
	input := strings.NewReader(strings.Join([]string{
		"", "", // analysis mode, Ollama model
		"1", // technical mapping
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
	if cfg.AI.Provider != "ollama" || cfg.AI.Ollama.Model != defaultSetupModel {
		t.Fatalf("AI configuration = %#v", cfg.AI)
	}
	if cfg.Systems[0].LifecycleStage != profile.LifecycleUnknown {
		t.Fatalf("lifecycle answer = %q", cfg.Systems[0].LifecycleStage)
	}
	for _, expected := range []string{"ComplyScan setup", "Step 1 of 4 — Analysis, privacy, and model", "Step 2 of 4 — Technical mappings", "Step 3 of 4 — Repository inspection", "Step 4 of 4 — Review, save, and first scan", "Repository inspected", "Local model setup", "Created repository profile", "Saved", "Next: complyscan scan"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, stdout.String())
		}
	}
	for _, expected := range []string{"Business, jurisdictional, and legal-applicability facts remain unconfirmed", "Local AI — Ollama keeps repository context on this machine"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("short setup output missing explanation %q:\n%s", expected, stdout.String())
		}
	}
	for _, unexpected := range []string{"Questionnaire preparation", "System questionnaire", "Drafting setup answers"} {
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

func TestFastSetupUsesDeterministicDraftWithoutModel(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "requirements.txt"), []byte("openai==2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "app.py"), []byte("from openai import OpenAI\nclient = OpenAI()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("\n")
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
	if cfg.AI.Provider != "none" || !strings.Contains(stdout.String(), "Created repository profile") || strings.Contains(stdout.String(), "AI suggestion") || strings.Contains(stdout.String(), "Drafting setup answers") {
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
	input := strings.NewReader("\n")
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
		output: io.Discard,
		selectMany: func(_ string, defaults []string, options []terminalChoice, _ []string) ([]string, error) {
			if len(defaults) != 0 {
				t.Fatalf("saved mappings were preselected: %#v", defaults)
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

func TestWriteSetupReviewSummaryShowsDecisionsBeforeSave(t *testing.T) {
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote.Model = "gpt-test"
	cfg.Frameworks = []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID}
	cfg.Systems = []profile.System{{ID: "checkout-ai", Name: "Checkout assistant"}}
	var output bytes.Buffer
	prompt := promptSession{output: &output}
	if err := writeSetupReviewSummary(prompt, cfg, setupScanDeep, true); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Review setup", "[READY] Analysis: OpenAI cloud — gpt-test", "EU AI Act technical evidence",
		"NIST AI RMF technical evidence", "Checkout assistant (checkout-ai)",
		"single-system inference", "deep AI-assisted scan",
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

func TestReviewSetupBeforeSaveCanCancelWithoutWriting(t *testing.T) {
	cfg := config.Default()
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("")), output: &output,
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			if label != "review action" || defaultValue != string(setupReviewSave) {
				t.Fatalf("selector label=%q default=%q", label, defaultValue)
			}
			return string(setupReviewCancel), nil
		},
	}
	mode := setupScanNone
	ready := true
	save, err := reviewSetupBeforeSave(context.Background(), prompt, &output, t.TempDir(), &cfg, setupOptions{}, setupRepositorySummary{}, &mode, &ready)
	if err != nil {
		t.Fatal(err)
	}
	if save {
		t.Fatal("cancel action unexpectedly allowed configuration save")
	}
}

func TestEverySetupQuestionHasDeveloperGuidance(t *testing.T) {
	keys := []string{
		"resume-setup", "applicability-context", "frameworks", "system-id", "system-name", "intended-purpose", "lifecycle-stage", "organization-roles", "organization-role-basic",
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

func TestSetupGuidanceCanProgressFromConciseToDetailed(t *testing.T) {
	var conciseOutput bytes.Buffer
	concisePrompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("?\nConfirmed purpose\n")),
		output: &conciseOutput, guidance: &questionGuidance{},
	}
	if err := explainSetupQuestion(concisePrompt, "intended-purpose"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conciseOutput.String(), setupQuestionHelp["intended-purpose"][0]) || strings.Contains(conciseOutput.String(), "Example:") || strings.Contains(conciseOutput.String(), "Enter ? at the next prompt") {
		t.Fatalf("concise guidance did not preserve only the essential explanation:\n%s", conciseOutput.String())
	}
	value, err := concisePrompt.text("Intended purpose", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Confirmed purpose" || !strings.Contains(conciseOutput.String(), "Example:") || !strings.Contains(conciseOutput.String(), "More guidance") {
		t.Fatalf("question-specific guidance was not expanded before accepting the answer; value=%q:\n%s", value, conciseOutput.String())
	}

	var detailedOutput bytes.Buffer
	detailedPrompt := promptSession{output: &detailedOutput, alwaysDetailed: true, guidance: &questionGuidance{}}
	if err := explainSetupQuestion(detailedPrompt, "intended-purpose"); err != nil {
		t.Fatal(err)
	}
	normalizedOutput := strings.Join(strings.Fields(detailedOutput.String()), " ")
	for _, line := range setupQuestionHelp["intended-purpose"] {
		if !strings.Contains(normalizedOutput, strings.Join(strings.Fields(line), " ")) {
			t.Errorf("detailed guidance missing %q", line)
		}
	}
}

func TestTerminalQuestionCanSelectMoreGuidance(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{},
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			selections++
			if label != "Lifecycle stage" || len(options) != 3 || options[2].Value != moreGuidanceChoiceValue || options[2].Guidance == "" {
				t.Fatalf("question selector label=%q options=%#v", label, options)
			}
			if selections == 1 {
				return moreGuidanceChoiceValue, nil
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
	if selected != profile.LifecycleDevelopment || selections != 2 || !strings.Contains(output.String(), "development — being designed or implemented") {
		t.Fatalf("selected=%q selections=%d output:\n%s", selected, selections, output.String())
	}
}

func TestTerminalMultiSelectCanOpenQuestionGuidance(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{},
		selectMany: func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			selections++
			if label != "Operating regions" || options[len(options)-1].Value != moreGuidanceChoiceValue || options[len(options)-1].Guidance == "" {
				t.Fatalf("multi-selector label=%q options=%#v", label, options)
			}
			if selections == 1 {
				return []string{moreGuidanceChoiceValue}, nil
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
	if len(selected) != 1 || selected[0] != profile.RegionUnknown || selections != 2 || !strings.Contains(output.String(), "eu — one or more European Union countries") {
		t.Fatalf("selected=%#v selections=%d output:\n%s", selected, selections, output.String())
	}
}

func TestTerminalConfirmationCanOpenQuestionGuidance(t *testing.T) {
	var output bytes.Buffer
	selections := 0
	prompt := promptSession{
		output: &output, guidance: &questionGuidance{},
		selectOne: func(label, defaultValue string, options []terminalChoice) (string, error) {
			selections++
			if label != "Install Ollama" || defaultValue != "Yes" || len(options) != 3 || options[2].Value != moreGuidanceChoiceValue {
				t.Fatalf("confirmation selector label=%q default=%q options=%#v", label, defaultValue, options)
			}
			if selections == 1 {
				return moreGuidanceChoiceValue, nil
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
	if confirmed || selections != 2 || !strings.Contains(output.String(), "Choose no to keep deterministic scanning") {
		t.Fatalf("confirmed=%t selections=%d output:\n%s", confirmed, selections, output.String())
	}
}

func TestTerminalGuidanceExpandsInsideActiveSelector(t *testing.T) {
	guidance := "advisory — supports a human decision\nautonomous — acts without prior human approval"
	description := terminalGuidanceDescription("Use ↑/↓ to move and Enter to confirm.", true, guidance)
	for _, expected := range []string{"Further explanation:", "advisory — supports a human decision", "autonomous — acts without prior human approval", "Move to an answer in this menu"} {
		if !strings.Contains(description, expected) {
			t.Errorf("expanded selector guidance missing %q:\n%s", expected, description)
		}
	}
	if collapsed := terminalGuidanceDescription("instructions", false, guidance); collapsed != "instructions" {
		t.Fatalf("collapsed guidance = %q", collapsed)
	}
}

func TestTerminalGuidanceActionIsRemovedFromMultiSelection(t *testing.T) {
	values := withoutTerminalValue([]string{"eu", moreGuidanceChoiceValue, "uk"}, moreGuidanceChoiceValue)
	if strings.Join(values, ",") != "eu,uk" || !containsTerminalValue([]string{moreGuidanceChoiceValue}, moreGuidanceChoiceValue) {
		t.Fatalf("filtered values = %#v", values)
	}
}

func TestTerminalTextQuestionAdvertisesQuestionGuidance(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("?\nProduct name\n")),
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
	if value != "Product name" || !strings.Contains(output.String(), "More guidance") || strings.Contains(output.String(), "Enter ? for more guidance about this question") {
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
	if cfg.AI.Provider != customCompatibleProvider || cfg.AI.Remote.ProviderName != "Acme model gateway" || cfg.AI.Remote.BaseURL != "https://models.example.com/v1" || cfg.AI.Remote.Model != "acme-review-v2" {
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
			if label != "Hosted model" || defaultValue != "claude-sonnet-5" {
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
	if model != "claude-opus-5" || len(choices) != 3 || choices[0].Label != "Claude Opus 5 · claude-opus-5" || choices[2].Value != customModelChoice {
		t.Fatalf("model=%q choices=%#v", model, choices)
	}
	if !strings.Contains(output.String(), "Loaded 2 model(s) available to this API key") || !strings.Contains(output.String(), "Use / to filter") {
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
	if model != "gpt-5.6-terra" || !strings.Contains(output.String(), "Live model catalogue unavailable") || !strings.Contains(output.String(), "suggested models and exact-ID entry") {
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
