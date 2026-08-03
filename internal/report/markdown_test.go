// complyscan:ignore-technical-evidence -- this file embeds synthetic technical-objective fixtures.
package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/framework"
	"github.com/1eonardodawinki/ComplyScan/internal/providers"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestWriteMarkdownRendersHumanTechnicalEvidenceReport(t *testing.T) {
	pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := framework.Evaluate(pack, nil, discovery.Repository{Files: []discovery.File{{
		Path: "review/override.go", Kind: discovery.KindSource,
		Content: []byte("package review\nfunc OverrideDecision(output string) {}\n"),
	}}})
	evidence.Target = "."
	value := NewWithMetadata(
		".", Tool{Name: "ComplyScan", Version: "0.2.0-dev", Commit: "abc123"},
		ScanScope{Findings: "full-repository", TechnicalEvidence: "full-repository"},
		time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		[]rules.Finding{{
			Fingerprint: "fingerprint", RuleID: "AI-LOG-001", Title: "Review *logging*", Severity: rules.SeverityHigh,
			Message: "Prompt-like data reaches logging.", Path: "app.go", StartLine: 10,
			Evidence: "logger.Info(response)", Remediation: "Review the data flow.", Confidence: "high",
		}}, nil, 0,
	)
	value.TechnicalEvidence = &evidence
	value.TechnicalReview = &providers.TechnicalReviewResult{
		Provider: providers.Ollama, Model: "gemma3", InputCandidates: 1, Reviewed: 1,
		Observations: []providers.TechnicalObservation{{
			ObjectiveID: "eu-aia-14-override-intervention", EvidenceFingerprint: strings.Repeat("b", 64),
			Strength: providers.StrengthWeak, Confidence: "high", Rationale: "Only an exported candidate was found.",
		}},
	}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# ComplyScan technical evidence report",
		"Technical evidence only",
		"scan-",
		"## Rule findings",
		"**1 finding:**",
		"Review \\*logging\\*",
		"## EU AI Act technical evidence",
		"### Repository analysis",
		"exported-entry-candidate",
		"Unresolved:",
		"Candidate evidence detected — Human override or intervention mechanism",
		"`review/override.go:2`",
		"No evidence detected",
		"## Coverage boundary",
		"## Ollama technical-objective review",
		"Only an exported candidate was found.",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
}

func TestMarkdownTextRemovesLineBreaksAndEscapesMarkup(t *testing.T) {
	if got := markdownText("hello\n<script>[x] *y*"); got != "hello &lt;script&gt;\\[x\\] \\*y\\*" {
		t.Fatalf("markdownText() = %q", got)
	}
	if got := inlineCode("a`b"); strings.Contains(got, "`a`b`") {
		t.Fatalf("inlineCode did not protect backtick: %q", got)
	}
}
