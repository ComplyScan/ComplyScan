package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

func TestProfileShowNISTOnlyOmitsEUApplicabilityAssessment(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.Frameworks = []string{framework.NISTAIRMFTechnicalEvidencePackID}
	system := profile.NewDraftSystem("example", "Example")
	system.Applicability = nil
	cfg.Systems = []profile.System{system}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"profile", "show", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"Declared system profiles: 1", "NIST AI RMF technical code evidence", "voluntary framework", "Profile review: draft"} {
		if !strings.Contains(output, expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output)
		}
	}
	for _, unexpected := range []string{"EU AI Act applicability profile", "Automated scope:", "High-risk screening:"} {
		if strings.Contains(output, unexpected) {
			t.Errorf("terminal output unexpectedly contains %q:\n%s", unexpected, output)
		}
	}
}

func TestProfileShowNISTOnlyWritesFrameworkNeutralJSON(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.Frameworks = []string{framework.NISTAIRMFTechnicalEvidencePackID}
	system := profile.NewDraftSystem("example", "Example")
	// Retain a stale decision to verify a NIST-only view does not leak an EU
	// applicability assessment from an older configuration.
	cfg.Systems = []profile.System{system}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"profile", "show", "--format", "json", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	var decoded declaredProfileReport
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Frameworks) != 1 || decoded.Frameworks[0].ID != framework.NISTAIRMFTechnicalEvidencePackID || decoded.Frameworks[0].Nature != framework.NatureVoluntaryFramework {
		t.Fatalf("selected frameworks = %#v", decoded.Frameworks)
	}
	if len(decoded.Systems) != 1 || decoded.Systems[0].ID != "example" || len(decoded.Systems[0].Applicability) != 0 {
		t.Fatalf("declared systems = %#v", decoded.Systems)
	}
	if strings.Contains(stdout.String(), "EU AI Act") || strings.Contains(stdout.String(), "automated_scope") || strings.Contains(stdout.String(), "high_risk_screening") {
		t.Fatalf("NIST-only JSON contains EU assessment fields:\n%s", stdout.String())
	}
}

func TestProfileShowWithoutFrameworkSelectionKeepsLegacyEUAssessment(t *testing.T) {
	target := t.TempDir()
	content := "version: 1\nfail-on: high\nai:\n  provider: none\n"
	if err := os.WriteFile(filepath.Join(target, config.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"profile", "show", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "EU AI Act applicability profile") {
		t.Fatalf("legacy default did not preserve EU assessment:\n%s", stdout.String())
	}
}

func TestProfileShowFrameworkSelectionFallback(t *testing.T) {
	if !profileShowIncludesEUAssessment(nil) {
		t.Fatal("an absent legacy framework selection should retain EU assessment")
	}
	if profileShowIncludesEUAssessment([]string{framework.NISTAIRMFTechnicalEvidencePackID}) {
		t.Fatal("a NIST-only selection should not run EU assessment")
	}
	if !profileShowIncludesEUAssessment([]string{framework.NISTAIRMFTechnicalEvidencePackID, framework.EUAIActTechnicalEvidencePackID}) {
		t.Fatal("an explicit EU selection should run EU assessment")
	}
}
