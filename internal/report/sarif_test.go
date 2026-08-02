package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

func TestWriteSARIFIncludesRulesLocationsAndFingerprints(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	value := New(".", "0.2.0", []rules.Finding{{
		Fingerprint: fingerprint,
		RuleID:      "AI-LOG-001",
		Title:       "Logged prompt",
		Severity:    rules.SeverityHigh,
		Category:    "data-handling",
		Message:     "Review logging.",
		Path:        "internal/chat.go",
		StartLine:   12,
		EndLine:     12,
		Remediation: "Remove sensitive values.",
		Confidence:  "medium",
	}}, nil, 0)
	var output bytes.Buffer
	if err := WriteSARIF(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	text := output.String()
	for _, expected := range []string{`"version": "2.1.0"`, `"ruleId": "AI-LOG-001"`, `"level": "error"`, `"uri": "internal/chat.go"`, fingerprint} {
		if !strings.Contains(text, expected) {
			t.Errorf("SARIF missing %s:\n%s", expected, text)
		}
	}
}

func TestWriteSARIFRejectsLocationlessFindings(t *testing.T) {
	value := New(".", "0.2.0", []rules.Finding{{
		Fingerprint: strings.Repeat("a", 64),
		RuleID:      "TEST-001",
		Title:       "Repository finding",
		Severity:    rules.SeverityMedium,
		Message:     "No source location.",
	}}, nil, 0)
	var output bytes.Buffer
	err := WriteSARIF(&output, value)
	if err == nil || !strings.Contains(err.Error(), "TEST-001 has no source location") {
		t.Fatalf("got error %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("invalid SARIF was written: %s", output.String())
	}
}
