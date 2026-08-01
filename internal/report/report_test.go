package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestSeverityFilteringAndThreshold(t *testing.T) {
	findings := []rules.Finding{
		{RuleID: "INFO", Severity: rules.SeverityInfo},
		{RuleID: "MED", Severity: rules.SeverityMedium},
		{RuleID: "HIGH", Severity: rules.SeverityHigh},
	}
	filtered := FilterByMinimum(findings, rules.SeverityMedium)
	if len(filtered) != 2 || filtered[0].RuleID != "MED" || filtered[1].RuleID != "HIGH" {
		t.Fatalf("unexpected filtered findings: %#v", filtered)
	}
	if !MeetsThreshold(findings, rules.SeverityHigh) {
		t.Fatal("high finding should meet high threshold")
	}
	if MeetsThreshold(findings, rules.SeverityCritical) {
		t.Fatal("findings should not meet critical threshold")
	}
}

func TestWriteJSON(t *testing.T) {
	value := New(".", "0.1.0", []rules.Finding{{
		RuleID: "AI-DOC-001", Title: "Missing docs", Severity: rules.SeverityMedium,
	}}, nil, 2)
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Tool.Name != "ComplyScan" || decoded.Tool.Version != "0.1.0" {
		t.Fatalf("unexpected tool: %#v", decoded.Tool)
	}
	if decoded.Summary.Total != 1 || decoded.Summary.Medium != 1 {
		t.Fatalf("unexpected summary: %#v", decoded.Summary)
	}
	if decoded.Findings == nil || len(decoded.Findings) != 1 {
		t.Fatalf("unexpected findings: %#v", decoded.Findings)
	}
	if decoded.Suppressed != 2 {
		t.Fatalf("suppressed = %d", decoded.Suppressed)
	}
}

func TestWriteTerminalDoesNotAddColorWhenDisabled(t *testing.T) {
	value := New(".", "0.1.0", []rules.Finding{{
		RuleID: "AI-LOG-001", Title: "Logged prompt", Severity: rules.SeverityHigh,
		Path: "app.py", StartLine: 10, Message: "Review logging.",
	}}, nil, 1)
	var output bytes.Buffer
	if err := WriteTerminal(&output, value, TerminalOptions{Color: false}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatal("unexpected ANSI color sequence")
	}
	for _, want := range []string{"ComplyScan found 1 potential issue", "HIGH", "app.py:10", "Summary: 1 high", "Suppressed: 1 accepted or baselined issue"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestWriteTerminalCompletion(t *testing.T) {
	value := New(".", "0.1.0", []rules.Finding{{Severity: rules.SeverityMedium}}, nil, 0)
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Scan complete: 1 potential issue", "Summary: 1 medium"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output.String())
		}
	}
}
