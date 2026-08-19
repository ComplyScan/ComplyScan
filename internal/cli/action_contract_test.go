package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGitHubActionRunsUnifiedSafeScanWithAutomaticScope(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Inputs map[string]struct {
			Description string `yaml:"description"`
			Default     string `yaml:"default"`
		} `yaml:"inputs"`
		Runs struct {
			Steps []struct {
				ID  string `yaml:"id"`
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}
	if action.Inputs["scope"].Default != "auto" || !strings.Contains(action.Inputs["scope"].Description, "file-local findings") || !strings.Contains(action.Inputs["scope"].Description, "repository-wide governance gates") {
		t.Fatalf("scope input does not default to automatic PR/full selection: %#v", action.Inputs["scope"])
	}
	aiReview := action.Inputs["ai-review"]
	if aiReview.Default != "none" || !strings.Contains(aiReview.Description, "safe deterministic default") || !strings.Contains(aiReview.Description, "multiple bounded requests") || !strings.Contains(aiReview.Description, "variable runtime and provider cost") || !strings.Contains(aiReview.Description, "trusts the provider destination") {
		t.Fatalf("ai-review input does not preserve explicit consent and configured trust boundaries: %#v", aiReview)
	}
	if legacy := action.Inputs["review"]; legacy.Default != "" || !strings.Contains(strings.ToLower(legacy.Description), "deprecated alias") {
		t.Fatalf("legacy review input is not a bounded compatibility alias: %#v", legacy)
	}
	for _, supportedInput := range []string{"ollama-model", "ollama-endpoint", "model", "api-key-env", "provider-name", "base-url"} {
		input, exists := action.Inputs[supportedInput]
		if !exists || strings.Contains(strings.ToLower(input.Description), "deprecated") {
			t.Fatalf("Action one-run input %q is missing or incorrectly deprecated: %#v", supportedInput, input)
		}
	}
	var command string
	for _, step := range action.Runs.Steps {
		if step.ID == "scan" {
			command = step.Run
			break
		}
	}
	for _, expected := range []string{
		"arguments=(scan",
		"INPUT_AI_REVIEW",
		"COMPLYSCAN_EVENT_NAME",
		"COMPLYSCAN_PR_BASE_SHA",
		`auto|pull-request|full`,
		`git -C "$target" rev-parse --verify --quiet`,
		`actions/checkout with fetch-depth: 0`,
		`arguments+=(--changed-since "$changed_since")`,
		"tr '[:upper:]' '[:lower:]'",
		`ai-review and its deprecated review alias cannot both be set`,
		`arguments+=(--deterministic-only)`,
		`arguments+=(--deep)`,
		`arguments+=(--provider "$ai_review_value")`,
		"effective_api_key_env=OPENAI_API_KEY",
		"effective_api_key_env=ANTHROPIC_API_KEY",
		"effective_api_key_env=GEMINI_API_KEY",
		`requires workflow input api-key-env so repository configuration cannot choose a credential`,
		"effective_base_url=https://api.x.ai/v1",
		"effective_base_url=https://api.groq.com/openai/v1",
		"effective_base_url=https://api.mistral.ai/v1",
		"effective_base_url=https://openrouter.ai/api/v1",
		`openai-compatible requires workflow inputs api-key-env, base-url, and provider-name`,
		`arguments+=(--api-key-env "$effective_api_key_env")`,
		"arguments+=(--require-ai-review)",
		"arguments+=(--refresh-review)",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("Action scan step is missing %q:\n%s", expected, command)
		}
	}
	for _, unexpected := range []string{"command_name=review", `arguments=(review`, "using a full-repository scan"} {
		if strings.Contains(command, unexpected) {
			t.Fatalf("Action scan step still contains one-run AI override %q:\n%s", unexpected, command)
		}
	}
	conflictMessage := strings.Index(command, "ai-review: none cannot be combined")
	if conflictMessage < 0 {
		t.Fatalf("Action scan step has no deterministic-only override validation:\n%s", command)
	}
	conflictStart := strings.LastIndex(command[:conflictMessage], "if [[")
	if conflictStart < 0 || strings.Contains(command[conflictStart:conflictMessage], "INPUT_REQUIRE_AI_REVIEW") {
		t.Fatalf("require-ai-review is incorrectly treated as AI processing consent:\n%s", command)
	}
	syntax := exec.Command("bash", "-n")
	syntax.Stdin = strings.NewReader(command)
	if output, err := syntax.CombinedOutput(); err != nil {
		t.Fatalf("Action scan step is not valid Bash: %v\n%s", err, output)
	}
}

func TestGitHubActionUsesMacOS26CompatibleGoToolchain(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				Uses string `yaml:"uses"`
				With struct {
					GoVersion     string `yaml:"go-version"`
					GoVersionFile string `yaml:"go-version-file"`
				} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}

	for _, step := range action.Runs.Steps {
		if step.Name != "Set up Go" {
			continue
		}
		if step.Uses != "actions/setup-go@v6" {
			t.Fatalf("Go setup action = %q, want actions/setup-go@v6", step.Uses)
		}
		if step.With.GoVersion != "1.26.x" {
			t.Fatalf("Action build Go version = %q, want macOS 26-compatible 1.26.x", step.With.GoVersion)
		}
		if step.With.GoVersionFile != "" {
			t.Fatalf("Action build regressed to the module minimum via go-version-file: %q", step.With.GoVersionFile)
		}
		return
	}
	t.Fatal("Action has no Set up Go step")
}

func TestGitHubActionUploadsGeneratedSARIFBeforeEnforcingCompletion(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				ID   string `yaml:"id"`
				If   string `yaml:"if"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}

	var scanCommand, uploadCondition, completionCommand string
	uploadIndex, completionIndex := -1, -1
	for index, step := range action.Runs.Steps {
		switch {
		case step.ID == "scan":
			scanCommand = step.Run
		case step.Name == "Upload SARIF":
			uploadCondition = step.If
			uploadIndex = index
		case step.Name == "Enforce scan completion":
			completionCommand = step.Run
			completionIndex = index
		}
	}
	for _, expected := range []string{
		"sarif-ready=true",
		"sarif-ready=false",
	} {
		if !strings.Contains(scanCommand, expected) {
			t.Fatalf("Action scan step is missing %q:\n%s", expected, scanCommand)
		}
	}
	if strings.Contains(scanCommand, "exit \"$status\"") {
		t.Fatalf("Action exits before generated SARIF can be uploaded:\n%s", scanCommand)
	}
	if !strings.Contains(uploadCondition, "steps.scan.outputs.sarif-ready == 'true'") {
		t.Fatalf("SARIF upload is not gated on generated output: %q", uploadCondition)
	}
	if uploadIndex < 0 || completionIndex < 0 || uploadIndex >= completionIndex {
		t.Fatalf("SARIF upload must precede completion enforcement: upload=%d completion=%d", uploadIndex, completionIndex)
	}
	if !strings.Contains(completionCommand, "exit \"$COMPLYSCAN_EXIT_CODE\"") {
		t.Fatalf("completion step does not preserve the ComplyScan exit code:\n%s", completionCommand)
	}
}

func TestGitHubActionPublishesConciseReportAndExposesLocalBundles(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Inputs map[string]struct {
			Description string `yaml:"description"`
			Default     string `yaml:"default"`
		} `yaml:"inputs"`
		Outputs map[string]struct {
			Description string `yaml:"description"`
			Value       string `yaml:"value"`
		} `yaml:"outputs"`
		Runs struct {
			Steps []struct {
				Name string `yaml:"name"`
				ID   string `yaml:"id"`
				If   string `yaml:"if"`
				Run  string `yaml:"run"`
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatalf("parse action.yml: %v", err)
	}

	publish := action.Inputs["publish-summary"]
	if publish.Default != "true" || !strings.Contains(publish.Description, "GitHub job summary") || !strings.Contains(publish.Description, "raw JSON") {
		t.Fatalf("publish-summary input does not explain its default and privacy boundary: %#v", publish)
	}
	for name, expected := range map[string]string{
		"markdown-report": "steps.scan.outputs.markdown-report",
		"json-report":     "steps.scan.outputs.json-report",
	} {
		output, exists := action.Outputs[name]
		if !exists || !strings.Contains(output.Value, expected) || !strings.Contains(output.Description, "Absolute path") {
			t.Fatalf("Action report output %q is incomplete: %#v", name, output)
		}
	}

	var scanCommand, summaryCondition, summaryCommand string
	summaryIndex, completionIndex := -1, -1
	for index, step := range action.Runs.Steps {
		switch {
		case step.ID == "scan":
			scanCommand = step.Run
		case step.Name == "Publish concise report summary":
			summaryCondition = step.If
			summaryCommand = step.Run
			summaryIndex = index
		case step.Name == "Enforce scan completion":
			completionIndex = index
		}
		if strings.Contains(step.Uses, "upload-artifact") {
			t.Fatalf("Action must not upload report artifacts automatically: %q", step.Uses)
		}
	}
	for _, expected := range []string{
		`markdown_report="$target/.complyscan/reports/latest.md"`,
		`json_report="$target/.complyscan/reports/latest.json"`,
		`touch "$report_start_marker"`,
		`"$markdown_report" -nt "$report_start_marker"`,
		`"$json_report" -nt "$report_start_marker"`,
		"markdown-report-ready=true",
		"json-report-ready=true",
	} {
		if !strings.Contains(scanCommand, expected) {
			t.Fatalf("Action scan step is missing report output %q:\n%s", expected, scanCommand)
		}
	}
	if !strings.Contains(summaryCondition, "inputs.publish-summary == 'true'") || !strings.Contains(summaryCondition, "markdown-report-ready == 'true'") {
		t.Fatalf("job-summary publication is not opt-out and report-gated: %q", summaryCondition)
	}
	if !strings.Contains(summaryCommand, `cat "$COMPLYSCAN_MARKDOWN_REPORT" >> "$GITHUB_STEP_SUMMARY"`) || strings.Contains(summaryCommand, "JSON") {
		t.Fatalf("job summary must append only the concise Markdown report:\n%s", summaryCommand)
	}
	for _, expected := range []string{"Pull-request scope", "changed and locally connected code", "Repository-wide governance checks"} {
		if !strings.Contains(summaryCommand, expected) {
			t.Fatalf("job summary does not explain PR-versus-repository scope; missing %q:\n%s", expected, summaryCommand)
		}
	}
	if summaryIndex < 0 || completionIndex < 0 || summaryIndex >= completionIndex {
		t.Fatalf("report summary must publish before completion enforcement: summary=%d completion=%d", summaryIndex, completionIndex)
	}
}

func TestSelfScanWorkflowLetsActionChoosePullRequestScope(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/complyscan.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	if !strings.Contains(workflow, "fetch-depth: 0") {
		t.Fatalf("self-scan checkout must retain the pull-request base commit:\n%s", workflow)
	}
	if strings.Contains(workflow, "changed-since:") {
		t.Fatalf("self-scan workflow bypasses the Action's automatic scope selection:\n%s", workflow)
	}
	for _, expected := range []string{
		"upload-results:",
		"github.event_name == 'push'",
		"github.event.pull_request.head.repo.full_name == github.repository",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("self-scan workflow does not keep fork PR scanning separate from SARIF upload; missing %q:\n%s", expected, workflow)
		}
	}
}

func TestManualAIReviewWorkflowUsesAnExplicitTrustedBoundary(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/ai-review.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, expected := range []string{
		"workflow_dispatch:",
		"github.ref == 'refs/heads/main'",
		"environment: ai-review",
		"ref: main",
		"OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}",
		"scope: full",
		"ai-review: openai",
		"require-ai-review: \"true\"",
		"upload-results: \"false\"",
		"fail-on-findings: \"false\"",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("manual AI-review workflow is missing trusted boundary %q:\n%s", expected, workflow)
		}
	}
	for _, unexpected := range []string{"pull_request:", "pull_request_target:", "push:"} {
		if strings.Contains(workflow, unexpected) {
			t.Fatalf("manual source-bearing AI review must not run automatically via %q:\n%s", unexpected, workflow)
		}
	}
}

func TestGitHubActionRefusesAutomaticPullRequestTargetScope(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		`"$COMPLYSCAN_EVENT_NAME" == "pull_request_target"`,
		`"$scope_value" == "auto" || "$scope_value" == "pull-request" || -n "$INPUT_CHANGED_SINCE"`,
		"default checkout is the base branch and cannot assess PR changes",
		"Use a pull_request workflow for change scanning",
		"consciously choose scope: full for a base-only scan",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("pull_request_target refusal is missing %q:\n%s", expected, text)
		}
	}
	if !strings.Contains(text, `"$scope_value" == "auto" && "$COMPLYSCAN_EVENT_NAME" == "pull_request"`) {
		t.Fatalf("automatic change scope is not restricted to pull_request events:\n%s", text)
	}
	if strings.Contains(text, `"$scope_value" == "auto" && "$COMPLYSCAN_EVENT_NAME" == "pull_request_target"`) {
		t.Fatalf("pull_request_target still enters automatic base derivation and can false-clear:\n%s", text)
	}
}
