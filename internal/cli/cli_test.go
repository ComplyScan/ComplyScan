package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
