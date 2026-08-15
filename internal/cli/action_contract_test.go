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
		"arguments+=(--provider \"$INPUT_REVIEW\")",
		"arguments+=(--require-ai-review)",
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("Action scan step is missing %q:\n%s", expected, command)
		}
	}
}
