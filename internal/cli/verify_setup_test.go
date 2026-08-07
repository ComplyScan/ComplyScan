package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestDetectTestCommandsFindsProjectNativeRunners(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "go.mod", Kind: discovery.KindManifest, Content: []byte("module example.com/test")},
		{Path: "control/control_test.go", Kind: discovery.KindSource, Content: []byte("package control")},
		{Path: "package.json", Kind: discovery.KindManifest, Content: []byte(`{"scripts":{"test":"vitest run"}}`)},
		{Path: "pnpm-lock.yaml", Kind: discovery.KindManifest},
		{Path: "tests/test_control.py", Kind: discovery.KindSource},
		{Path: "Makefile", Kind: discovery.KindOtherText, Content: []byte("test:\n\tgo test ./...")},
	}}
	commands := detectTestCommands(repository)
	if len(commands) != 4 {
		t.Fatalf("detected commands = %#v", commands)
	}
	byID := make(map[string]detectedTestCommand, len(commands))
	for _, command := range commands {
		byID[command.ID] = command
	}
	if byID["go"].Command != "go" || byID["javascript"].Command != "pnpm" || byID["python"].Args[1] != "pytest" || byID["make"].Command != "make" {
		t.Fatalf("unexpected detected commands: %#v", byID)
	}
}

func TestVerifySetupSavesInertConfirmedRecipe(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "go.mod"), []byte("module example.com/project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "control_test.go"), []byte("package project\nfunc TestControl() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	system := cliCandidateProviderSystem()
	system.AIActivities = []profile.AIActivity{profile.ActivityInference, profile.ActivityAutomatedDecision}
	cfg.Systems = []profile.System{system}
	path := filepath.Join(target, config.FileName)
	if err := config.Write(path, cfg, false); err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader("\n\n1\n\nproject-tests:local\n\n\n\n")
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"verify", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	updated, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Verification == nil || len(updated.Verification.Recipes) != 1 {
		t.Fatalf("recipe was not saved: %#v", updated.Verification)
	}
	recipe := updated.Verification.Recipes[0]
	if recipe.Command != "go" || len(recipe.Arguments) != 2 || recipe.Image != "project-tests:local" || len(recipe.Objectives) != 1 || len(recipe.Systems) != 1 || recipe.Systems[0] != system.ID {
		t.Fatalf("unexpected recipe: %#v", recipe)
	}
	for _, expected := range []string{
		"does not run tests", "Developer meaning:", "Current repository signal:", "must already exist locally",
		"Nothing was executed", "complyscan scan", "--verify",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("wizard output missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestVerifySetupRejectsNonInteractiveInputAndMissingProfiles(t *testing.T) {
	target := t.TempDir()
	if err := config.Write(filepath.Join(target, config.FileName), config.Default(), false); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"verify", "setup", target}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 2 || !strings.Contains(stderr.String(), "needs a configured system profile") {
		t.Fatalf("missing-profile code=%d stderr=%q", code, stderr.String())
	}

	cfg := config.Default()
	cfg.Systems = []profile.System{cliCandidateProviderSystem()}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, true); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	code = executeWithInput([]string{"verify", "setup", target}, strings.NewReader(""), &stdout, &stderr, testBuild)
	if code != 2 || !strings.Contains(stderr.String(), "requires a terminal") {
		t.Fatalf("non-interactive code=%d stderr=%q", code, stderr.String())
	}
}

func TestVerificationSetupOffersObjectivesFromEverySelectedFramework(t *testing.T) {
	cfg := config.Default()
	cfg.Frameworks = []string{framework.EUAIActTechnicalEvidencePackID, framework.NISTAIRMFTechnicalEvidencePackID}
	system := cliCandidateProviderSystem()
	system.AIActivities = []profile.AIActivity{profile.ActivityInference, profile.ActivityAutomatedDecision}
	cfg.Systems = []profile.System{system}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "review.go", Kind: discovery.KindSource,
		Content: []byte("package review\nfunc humanReview() { approveDecision() }\n"),
	}}}
	components := inventory.NewReport(".", "setup", inventory.Analyze(repository), nil)
	choices, err := verificationObjectivesForFrameworks(cfg, repository, components, system.ID)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, choice := range choices {
		seen[choice.Framework] = true
	}
	if !seen["EU AI Act technical code evidence"] || !seen["NIST AI RMF technical code evidence"] {
		t.Fatalf("framework choices = %#v", seen)
	}
}

func TestParseObjectiveSelectionRequiresDisplayedNumbers(t *testing.T) {
	choices := []verificationObjectiveChoice{{}, {}, {}}
	selected, err := parseObjectiveSelection("3, 1, 3", choices)
	if err != nil || len(selected) != 2 {
		t.Fatalf("selection=%#v error=%v", selected, err)
	}
	if _, err := parseObjectiveSelection("4", choices); err == nil {
		t.Fatal("out-of-range objective selection was accepted")
	}
}
