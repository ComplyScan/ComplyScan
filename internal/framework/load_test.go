package framework

import (
	"strings"
	"testing"
)

func TestBuiltinEUAIActPackContainsCodeObjectivesOnly(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Version != "0.1.3" || pack.Source.Reference != "Regulation (EU) 2024/1689" || pack.Coverage.EvidenceType != "code" || pack.Coverage.Nature != NatureLegislation {
		t.Fatalf("unexpected pack metadata: %#v", pack)
	}
	if len(pack.Digest) != 64 || len(pack.Objectives) < 10 {
		t.Fatalf("digest=%q objectives=%d", pack.Digest, len(pack.Objectives))
	}
	for _, objective := range pack.Objectives {
		if objective.ControlID == "" || objective.Applicability.Scope == "" {
			t.Fatalf("objective %q has no inspectable applicability condition", objective.ID)
		}
		for _, kind := range objective.FileKinds {
			if _, supported := supportedFileKinds[kind]; !supported {
				t.Fatalf("objective %q uses non-code evidence kind %q", objective.ID, kind)
			}
		}
	}
	if len(pack.Coverage.Limitations) == 0 || !strings.Contains(strings.Join(pack.Coverage.Limitations, " "), "dashboard") {
		t.Fatalf("code/dashboard boundary is not explicit: %#v", pack.Coverage)
	}
}

func TestBuiltinNISTAIRMFPackIsVoluntaryAndCodeOnly(t *testing.T) {
	pack, err := LoadBuiltin(NISTAIRMFTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Version != "0.1.0" || pack.Source.Reference != "NIST AI 100-1" || pack.Coverage.Nature != NatureVoluntaryFramework || pack.Coverage.EvidenceType != "code" {
		t.Fatalf("unexpected NIST pack metadata: %#v", pack)
	}
	if len(pack.Objectives) < 10 || len(pack.Digest) != 64 {
		t.Fatalf("digest=%q objectives=%d", pack.Digest, len(pack.Objectives))
	}
	for _, objective := range pack.Objectives {
		if objective.ControlID == "" || objective.Applicability.Scope != ApplicabilitySelectedFramework {
			t.Fatalf("objective %q lacks selected-framework mapping: %#v", objective.ID, objective)
		}
	}
	if !strings.Contains(strings.Join(pack.Coverage.Limitations, " "), "voluntary") {
		t.Fatalf("voluntary boundary is not explicit: %#v", pack.Coverage)
	}
}

func TestBuiltinObjectivesHaveAdjacentPlainLanguageComments(t *testing.T) {
	for _, id := range []string{EUAIActTechnicalEvidencePackID, NISTAIRMFTechnicalEvidencePackID} {
		data, err := builtins.ReadFile(builtinPaths[id])
		if err != nil {
			t.Fatal(err)
		}
		pack, err := LoadBuiltin(id)
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		for _, objective := range pack.Objectives {
			marker := "  - id: " + objective.ID
			position := strings.Index(content, marker)
			if position < 0 {
				t.Fatalf("objective %q is missing from the embedded YAML", objective.ID)
			}
			before := strings.TrimSpace(content[:position])
			lines := strings.Split(before, "\n")
			if len(lines) == 0 || !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "# ") {
				t.Errorf("objective %q needs a plain-language comment immediately above it", objective.ID)
			}
		}
	}
}

func TestPackParserRejectsUnknownFieldsDuplicateObjectivesAndDocuments(t *testing.T) {
	valid, err := builtins.ReadFile(builtinPaths[EUAIActTechnicalEvidencePackID])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(append(valid, []byte("unknown-field: true\n")...)); err == nil {
		t.Fatal("expected unknown field error")
	}
	pack, err := LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	pack.Objectives = append(pack.Objectives, pack.Objectives[0])
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("got error %v", err)
	}
	pack.Objectives = pack.Objectives[:1]
	pack.Objectives[0].FileKinds = []string{"documentation"}
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "not a code evidence kind") {
		t.Fatalf("got error %v", err)
	}
	pack, err = LoadBuiltin(EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	pack.Objectives[0].Applicability.Scope = ""
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("got error %v", err)
	}
	pack.Objectives[0].Applicability = ObjectiveApplicability{Scope: ApplicabilityTransparencyObligation}
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "activities-any-of") {
		t.Fatalf("got error %v", err)
	}
	pack.Objectives[0].Applicability = ObjectiveApplicability{Scope: ApplicabilityHighRiskSystem, ActivitiesAnyOf: []string{"unknown"}}
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("got error %v", err)
	}
}
