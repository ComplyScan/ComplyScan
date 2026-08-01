package baseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestWriteAndLoadDeterministicSourceFreeBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	findings := []rules.Finding{
		{Fingerprint: strings.Repeat("b", 64), RuleID: "AI-LOG-001", Path: "b.py", Title: "Logged prompt", Evidence: "private source"},
		{Fingerprint: strings.Repeat("a", 64), RuleID: "AI-DOC-001", Title: "Missing docs"},
	}
	if err := Write(path, findings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private source") {
		t.Fatal("baseline contains source evidence")
	}
	if strings.Index(string(data), strings.Repeat("a", 64)) > strings.Index(string(data), strings.Repeat("b", 64)) {
		t.Fatal("baseline entries are not sorted")
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Findings) != 2 || !loaded.Contains(strings.Repeat("b", 64)) {
		t.Fatalf("unexpected baseline: %#v", loaded)
	}
}

func TestLoadRejectsInvalidBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(`{"version":2,"findings":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("got error %v", err)
	}
}
