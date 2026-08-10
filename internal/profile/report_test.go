package profile

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAssessmentReportsRemainExplicitlyProvisional(t *testing.T) {
	report := AssessEUAIAct([]System{NewDraftSystem("example", "Example")})
	var terminal bytes.Buffer
	if err := WriteTerminal(&terminal, report); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Automated scope: needs-context", "Technical mapping readiness: incomplete", "Human decision: needs-review", "No result is a legal determination"} {
		if !strings.Contains(terminal.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, terminal.String())
		}
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	var decoded AssessmentReport
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || len(decoded.Systems) != 1 {
		t.Fatalf("decoded=%#v error=%v", decoded, err)
	}
}
