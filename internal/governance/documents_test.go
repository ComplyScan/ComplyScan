package governance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/inventory"
)

func TestDocumentsIncludeInventoryAndReviewGuardrails(t *testing.T) {
	report := inventory.NewReport(".", "0.2.0", []inventory.Signal{{
		Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceDependency,
		Scope: inventory.ScopeConfig, Confidence: "high", Path: "requirements.txt", Line: 1,
		Package: "openai", Version: "1.2.3", Evidence: "dependency openai 1.2.3",
	}}, []string{"one file could not be read"})
	date := time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC)
	for name, document := range map[string]string{
		"ai-system": AISystem(report, date), "risk-assessment": RiskAssessment(report, date),
	} {
		t.Run(name, func(t *testing.T) {
			for _, want := range []string{"Generated: 2026-08-02", "OpenAI", "requirements.txt:1", "human review required", "one file could not be read", "TODO"} {
				if !strings.Contains(document, want) {
					t.Errorf("document missing %q:\n%s", want, document)
				}
			}
		})
	}
}

func TestWriteProtectsExistingDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docs", "AI_SYSTEM.md")
	if err := Write(path, "first", false); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, "second", false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected protected-file error, got %v", err)
	}
	if err := Write(path, "second", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "second" {
		t.Fatalf("file contents = %q", data)
	}
}
