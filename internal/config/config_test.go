package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestLoadMergesDefaultsAndParsesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content := `version: 1
scan:
  exclude:
    - generated
fail-on: medium
rules:
  AI-LOG-001:
    enabled: false
ai:
  provider: none
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FailOn != rules.SeverityMedium {
		t.Fatalf("fail-on = %q", cfg.FailOn)
	}
	if cfg.RuleEnabled("AI-LOG-001") {
		t.Fatal("AI-LOG-001 should be disabled")
	}
	if !cfg.RuleEnabled("AI-SEC-001") {
		t.Fatal("unspecified default rule should stay enabled")
	}
	if len(cfg.Scan.Exclude) != 1 || cfg.Scan.Exclude[0] != "generated" {
		t.Fatalf("exclude = %#v", cfg.Scan.Exclude)
	}
	if cfg.Scan.MaxFiles != 25_000 || cfg.Scan.MaxTotalBytes != 100<<20 {
		t.Fatalf("scan budgets = %d files, %d bytes", cfg.Scan.MaxFiles, cfg.Scan.MaxTotalBytes)
	}
}

func TestLoadRejectsInvalidSeverity(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("version: 1\nfail-on: urgent\nai:\n  provider: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "invalid severity") {
		t.Fatalf("got error %v", err)
	}
}

func TestLoadRejectsUnknownFieldsRulesAndProviders(t *testing.T) {
	tests := []string{
		"version: 1\nfail-on: high\ntyop: true\nai:\n  provider: none\n",
		"version: 1\nfail-on: high\nrules:\n  AI-TYPO-001:\n    enabled: true\nai:\n  provider: none\n",
		"version: 1\nfail-on: high\nai:\n  provider: openai\n",
	}
	for _, content := range tests {
		path := filepath.Join(t.TempDir(), FileName)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("expected invalid config error for:\n%s", content)
		}
	}
}

func TestLoadRejectsInvalidScanBudgets(t *testing.T) {
	for _, field := range []string{"max-files: 0", "max-total-bytes: 0"} {
		path := filepath.Join(t.TempDir(), FileName)
		content := "version: 1\nscan:\n  " + field + "\nfail-on: high\nai:\n  provider: none\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "must be greater than zero") {
			t.Fatalf("got error %v for %q", err, field)
		}
	}
}

func TestWriteDefaultDoesNotOverwriteWithoutForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := WriteDefault(path, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteDefault(path, false); err == nil {
		t.Fatal("expected existing-file error")
	}
	if err := WriteDefault(path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("generated config does not parse: %v", err)
	}
}
