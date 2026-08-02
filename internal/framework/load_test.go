package framework

import (
	"strings"
	"testing"
)

func TestBuiltinEUAIActPackIsVersionedAndCoversArticlesNineToFifteen(t *testing.T) {
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Version != "0.1.0" || pack.Source.Reference != "Regulation (EU) 2024/1689" {
		t.Fatalf("unexpected pack metadata: %#v", pack)
	}
	if len(pack.Digest) != 64 {
		t.Fatalf("pack digest = %q", pack.Digest)
	}
	if len(pack.Controls) != 7 {
		t.Fatalf("controls = %d", len(pack.Controls))
	}
	for index, article := range []string{"Article 9", "Article 10", "Article 11 and Annex IV", "Article 12", "Article 13", "Article 14", "Article 15"} {
		if pack.Controls[index].SourceReference != article {
			t.Errorf("control %d source = %q, want %q", index, pack.Controls[index].SourceReference, article)
		}
	}
	if len(pack.Coverage.Limitations) == 0 || !strings.Contains(strings.Join(pack.Coverage.Limitations, " "), "not a complete") {
		t.Fatalf("coverage limitations are not explicit: %#v", pack.Coverage)
	}
}

func TestPackParserRejectsUnknownFieldsAndDuplicateControls(t *testing.T) {
	valid, err := builtins.ReadFile(builtinPaths[EUAIActHighRiskProviderPackID])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(append(valid, []byte("unknown-field: true\n")...)); err == nil {
		t.Fatal("expected unknown field error")
	}
	pack, err := LoadBuiltin(EUAIActHighRiskProviderPackID)
	if err != nil {
		t.Fatal(err)
	}
	pack.Controls = append(pack.Controls, pack.Controls[0])
	if err := pack.Validate(); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("got error %v", err)
	}
}
