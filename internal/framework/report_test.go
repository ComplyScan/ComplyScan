package framework

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
)

func TestFrameworkReportsShowPackVersionGapsAndLimitations(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	report := Evaluate(pack, []profile.System{candidateProviderSystem()}, discovery.Repository{})
	var terminal bytes.Buffer
	if err := WriteAssessmentTerminal(&terminal, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"@ 0.1.0", "MISSING", "Article 9", "Control summary: 7 missing", "Coverage limitation", "not legal compliance"} {
		if !strings.Contains(terminal.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, terminal.String())
		}
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	var decoded AssessmentReport
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.Pack.Version != "0.1.0" {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
}

func TestPackListDoesNotOmitCoverageBoundary(t *testing.T) {
	listings, err := ListBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WritePackListTerminal(&output, listings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), EUAIActHighRiskProviderPackID) || !strings.Contains(output.String(), "Limitation:") {
		t.Fatalf("unexpected listing:\n%s", output.String())
	}
}
