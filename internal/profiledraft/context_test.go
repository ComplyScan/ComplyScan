package profiledraft

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
)

func TestDeterministicSuggestionsUseRuntimeEvidence(t *testing.T) {
	report := inventory.NewReport(".", "test", []inventory.Signal{{
		Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceImport,
		Scope: inventory.ScopeRuntime, Confidence: "high", Path: "agent.go", Line: 12, Evidence: "import openai",
	}}, nil)
	suggestion, exists := DeterministicSuggestions(report)["ai-activities"]
	if !exists || strings.Join(suggestion.Values, ",") != "inference" || suggestion.Evidence[0].Path != "agent.go" {
		t.Fatalf("suggestion = %#v; exists=%t", suggestion, exists)
	}
}

func TestBuildRequestPrioritizesRelevantBoundedFiles(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "README.md", Kind: discovery.KindReadme, Content: []byte("# Support assistant\nDrafts replies for support agents.")},
		{Path: "agent.go", Kind: discovery.KindSource, Content: []byte("package agent\n\nfunc run() { callModel() }")},
		{Path: "unrelated.go", Kind: discovery.KindSource, Content: []byte("package unrelated")},
	}}
	report := inventory.NewReport(".", "test", []inventory.Signal{{
		Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceImport,
		Scope: inventory.ScopeRuntime, Confidence: "high", Path: "agent.go", Line: 3, Evidence: "import openai",
	}}, nil)
	request := BuildRequest("/tmp/support-assistant", repository, Languages(repository), report)
	if request.RepositoryName != "support-assistant" || len(request.Contexts) != 2 || strings.Join(request.Languages, ",") != "Go" {
		t.Fatalf("request = %#v", request)
	}
	paths := request.Contexts[0].Path + "," + request.Contexts[1].Path
	if !strings.Contains(paths, "README.md") || !strings.Contains(paths, "agent.go") || strings.Contains(paths, "unrelated.go") {
		t.Fatalf("context paths = %q", paths)
	}
	if !strings.Contains(request.Contexts[0].Source+request.Contexts[1].Source, "3: func run") {
		t.Fatalf("source contexts = %#v", request.Contexts)
	}
}
