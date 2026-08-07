// complyscan:ignore-technical-evidence -- this file embeds synthetic technical-objective fixtures.
package framework

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestTechnicalEvidenceReportsShowVersionCandidatesAndLimitations(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "review/override.go", Kind: discovery.KindSource,
		Content: []byte("package review\nfunc OverrideDecision(output string) {}\n"),
	}}}
	report := Evaluate(pack, nil, repository)
	var terminal bytes.Buffer
	if err := WriteTechnicalEvidenceTerminal(&terminal, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"@ 0.1.3", "CANDIDATE EVIDENCE", "Article 14", "Applicability: framework scope high-risk-system", "Technical summary:", "Coverage limitation", "not a legal compliance conclusion"} {
		if !strings.Contains(terminal.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, terminal.String())
		}
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	var decoded TechnicalEvidenceReport
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || decoded.Pack.Version != "0.1.3" {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
}

func TestPackListDoesNotOmitCodeBoundary(t *testing.T) {
	listings, err := ListBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := WritePackListTerminal(&output, listings); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), EUAIActTechnicalEvidencePackID) || !strings.Contains(output.String(), "technical objectives") || !strings.Contains(output.String(), "Limitation:") {
		t.Fatalf("unexpected listing:\n%s", output.String())
	}
}
