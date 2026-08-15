package cli

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGitHubActionRequiresExplicitAIReview(t *testing.T) {
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
	if action.Inputs["review"].Default != "" || !strings.Contains(action.Inputs["review"].Description, "deterministic scan") {
		t.Fatalf("review input does not preserve the deterministic default: %#v", action.Inputs["review"])
	}
	var command string
	for _, step := range action.Runs.Steps {
		if step.ID == "scan" {
			command = step.Run
			break
		}
	}
	for _, expected := range []string{
		"command_name=scan",
		"command_name=review",
		"tr '[:upper:]' '[:lower:]'",
		"arguments+=(--provider \"$INPUT_REVIEW\")",
		"arguments+=(--require-ai-review)",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("Action scan step is missing %q:\n%s", expected, command)
		}
	}
	if strings.Contains(command, "${INPUT_REVIEW,,}") {
		t.Fatalf("Action scan step uses Bash 4-only lowercase expansion:\n%s", command)
	}
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
