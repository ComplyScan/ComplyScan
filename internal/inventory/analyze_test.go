// complyscan:ignore-ai-signals -- these are synthetic detector fixtures.
package inventory

import (
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

func TestAnalyzeFindsTypedSignals(t *testing.T) {
	repo := discovery.Repository{Files: []discovery.File{
		{Path: "requirements.txt", Kind: discovery.KindManifest, Content: []byte("openai==1.2.3\n")},
		{Path: "src/chat.py", Kind: discovery.KindSource, Content: []byte("from anthropic import Anthropic\n")},
		{Path: "src/gemini.ts", Kind: discovery.KindSource, Content: []byte("const endpoint = 'https://generativelanguage.googleapis.com/v1/models'\n")},
		{Path: "tests/config.py", Kind: discovery.KindSource, Content: []byte("key = os.getenv('MISTRAL_API_KEY')\n")},
	}}
	signals := Analyze(repo)
	if len(signals) != 4 {
		t.Fatalf("got %d signals, want 4: %#v", len(signals), signals)
	}
	wants := map[string]EvidenceType{
		"OpenAI": EvidenceDependency, "Anthropic": EvidenceImport,
		"Google Gemini": EvidenceEndpoint, "Mistral": EvidenceEnvironment,
	}
	for _, signal := range signals {
		if signal.EvidenceType != wants[signal.Name] {
			t.Errorf("unexpected signal: %#v", signal)
		}
		if signal.Name == "Mistral" && signal.Scope != ScopeTest {
			t.Errorf("test signal scope = %q", signal.Scope)
		}
	}
}

func TestAnalyzeRejectsPlainNamesAndSignatureFiles(t *testing.T) {
	repo := discovery.Repository{Files: []discovery.File{
		{Path: "main.go", Kind: discovery.KindSource, Content: []byte("var providers = []string{\"OpenAI\", \"Anthropic\", \"Gemini\"}\n")},
		{Path: "signatures.go", Kind: discovery.KindSource, Content: []byte("// " + IgnoreSignalsMarker + "\nconst endpoint = \"https://api.openai.com\"\n")},
	}}
	if signals := Analyze(repo); len(signals) != 0 {
		t.Fatalf("plain names or signature file produced signals: %#v", signals)
	}
}

func TestAnalyzeParsesNodeAndGoDependencies(t *testing.T) {
	repo := discovery.Repository{Files: []discovery.File{
		{Path: "package.json", Kind: discovery.KindManifest, Content: []byte(`{"dependencies":{"@anthropic-ai/sdk":"^1.0.0"},"devDependencies":{"ai":"2.0.0"}}`)},
		{Path: "go.mod", Kind: discovery.KindManifest, Content: []byte("module example.com/app\nrequire github.com/openai/openai-go v1.0.0\n")},
	}}
	signals := Analyze(repo)
	if len(signals) != 3 {
		t.Fatalf("got %d signals, want 3: %#v", len(signals), signals)
	}
}

func TestDependencyDeclarationRequiresWholePackageName(t *testing.T) {
	if _, ok := dependencyDeclaration("langchain-community==1.0", "langchain"); ok {
		t.Fatal("prefix-only package match was accepted")
	}
	version, ok := dependencyDeclaration(`langchain = "^1.0"`, "langchain")
	if !ok || version != "^1.0" {
		t.Fatalf("valid declaration = %q, %v", version, ok)
	}
}
