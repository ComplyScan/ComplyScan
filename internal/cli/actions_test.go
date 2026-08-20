package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestActionsListShowAndVerifyUseStableReportContract(t *testing.T) {
	target := t.TempDir()
	value := report.New(target, "test", []rules.Finding{{
		Fingerprint: "stable", RuleID: "AI-TEST-001", Severity: rules.SeverityHigh,
		Title: "Unsafe AI path", Message: "The path needs a guard.", Remediation: "Add the guard.", Path: "app.go", StartLine: 4,
	}}, nil, 0)
	value = report.ReconcileDeveloperActionLifecycle(value, nil)
	if _, err := report.WriteArtifacts(filepath.Join(target, report.DefaultDirectory), value); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"actions", "list", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("actions list exit = %d, stderr=%s", code, stderr.String())
	}
	for _, fragment := range []string{"[new]", "finding/stable", "Unsafe AI path", "app.go:4"} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("actions list missing %q:\n%s", fragment, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"actions", "show", "finding/stable", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("actions show exit = %d, stderr=%s", code, stderr.String())
	}
	for _, fragment := range []string{"Why: The path needs a guard.", "Recommended change: Add the guard.", "Done when:"} {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("actions show missing %q:\n%s", fragment, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"actions", "verify", "--deterministic-only", "finding/stable", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("actions verify exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "Resolved finding/stable") {
		t.Fatalf("verification output = %s", stdout.String())
	}
}

func TestAgentInstructionsPrintByDefaultAndWriteOnlyExplicitly(t *testing.T) {
	target := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"agent", "instructions", "--format", "skill", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("agent instructions exit = %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "name: complyscan-actions") || !strings.Contains(stdout.String(), "complyscan actions verify") {
		t.Fatalf("skill output = %s", stdout.String())
	}
	destination := filepath.Join(target, ".agents", "skills", "complyscan-actions", "SKILL.md")
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("print-only command unexpectedly wrote %s", destination)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"agent", "instructions", "--format", "skill", "--write", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("agent instruction write exit = %d, stderr=%s", code, stderr.String())
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Treat `.complyscan/reports/latest.json` as the machine-readable source of truth") {
		t.Fatalf("written skill = %s", content)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Execute([]string{"agent", "instructions", "--format", "skill", "--write", target}, &stdout, &stderr, testBuild); code != 2 || !strings.Contains(stderr.String(), "refusing to overwrite") {
		t.Fatalf("overwrite exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
