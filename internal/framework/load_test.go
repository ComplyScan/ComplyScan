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
	if pack.Version != "0.1.1" || pack.Source.Reference != "Regulation (EU) 2024/1689" || pack.Coverage.EvidenceType != "code" {
		t.Fatalf("unexpected pack metadata: %#v", pack)
	}
	if len(pack.Digest) != 64 || len(pack.Objectives) < 10 {
		t.Fatalf("digest=%q objectives=%d", pack.Digest, len(pack.Objectives))
	}
	for _, objective := range pack.Objectives {
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
}
