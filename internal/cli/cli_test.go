package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/complyscan/complyscan/internal/report"
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
