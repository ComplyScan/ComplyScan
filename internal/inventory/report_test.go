package inventory

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewReportAggregatesComponents(t *testing.T) {
	report := NewReport(".", "0.2.0", []Signal{
		{Name: "OpenAI", Kind: KindProvider, EvidenceType: EvidenceDependency, Scope: ScopeConfig, Confidence: "high", Path: "requirements.txt", Line: 1, Package: "openai", Version: "1.2.3", Evidence: "dependency openai 1.2.3"},
		{Name: "OpenAI", Kind: KindProvider, EvidenceType: EvidenceImport, Scope: ScopeRuntime, Confidence: "high", Path: "app.py", Line: 1, Package: "openai", Evidence: "import openai"},
		{Name: "LangChain", Kind: KindFramework, EvidenceType: EvidenceImport, Scope: ScopeTest, Confidence: "high", Path: "tests/test_chain.py", Line: 2, Package: "langchain", Evidence: "import langchain"},
	}, []string{"partial read"})
	if report.SchemaVersion != 1 || report.Summary.Components != 2 || report.Summary.Signals != 3 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if report.Summary.Providers != 1 || report.Summary.Frameworks != 1 || report.Summary.RuntimeSignals != 1 || report.Summary.TestSignals != 1 || report.Summary.ConfigurationSignals != 1 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	if report.Components[1].Name != "OpenAI" || report.Components[1].Occurrences != 2 {
		t.Fatalf("unexpected component aggregation: %#v", report.Components)
	}
	if len(report.Components[1].Packages) != 1 || report.Components[1].Packages[0].Version != "1.2.3" || report.Components[1].Scopes[0] != ScopeRuntime {
		t.Fatalf("unexpected component details: %#v", report.Components[1])
	}
}

func TestInventoryReportWriters(t *testing.T) {
	report := NewReport(".", "0.2.0", []Signal{{
		Name: "OpenAI", Kind: KindProvider, EvidenceType: EvidenceImport, Scope: ScopeRuntime,
		Confidence: "high", Path: "app.py", Line: 1, Package: "openai", Evidence: "import openai",
	}}, nil)
	var terminal bytes.Buffer
	if err := WriteTerminal(&terminal, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "PROVIDER  OpenAI") || !strings.Contains(terminal.String(), "app.py:1") {
		t.Fatalf("unexpected terminal report:\n%s", terminal.String())
	}

	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Components[0].Name != "OpenAI" {
		t.Fatalf("unexpected JSON report: %#v", decoded)
	}
}
