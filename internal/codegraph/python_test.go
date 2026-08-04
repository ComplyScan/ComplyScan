package codegraph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestBuildIndexesPythonSymbolsImportsCallsAndReachability(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "app/main.py", Kind: discovery.KindSource, Content: []byte(`from app.override import handle_override

def main():
    handle_override()

if __name__ == "__main__":
    main()
`),
		},
		{
			Path: "app/override.py", Kind: discovery.KindSource, Content: []byte(`import os
from app.store import update_decision

def handle_override():
    if os.getenv("OVERRIDE_ENABLED") == "true":
        authorize_reviewer()
        update_decision()
        audit_override()

def authorize_reviewer():
    return True

def audit_override():
    pass

def dead_override():
    update_decision()

class Reviewer:
    def authorize(self):
        return True

    def review(self):
        return self.authorize()
`),
		},
		{
			Path: "app/store.py", Kind: discovery.KindSource, Content: []byte(`def update_decision():
    pass
`),
		},
		{
			Path: "tests/test_override.py", Kind: discovery.KindSource, Content: []byte(`from app.override import dead_override

def test_dead_override():
    dead_override()
`),
		},
	}}

	graph := Build(repository)
	if graph.FilesIndexed != 4 || len(graph.Languages) != 1 || graph.Languages[0] != LanguagePython {
		t.Fatalf("unexpected Python coverage: %#v", graph)
	}
	if len(graph.Imports) != 4 {
		t.Fatalf("imports = %#v", graph.Imports)
	}
	for _, repositoryImport := range graph.Imports {
		if repositoryImport.Path == "" || repositoryImport.Language != LanguagePython {
			t.Fatalf("incomplete Python import: %#v", repositoryImport)
		}
	}
	assertEdge(t, graph, EdgeCall, "main", "handle_override", "handle_override")
	assertEdge(t, graph, EdgePersistence, "handle_override", "update_decision", "update_decision")
	assertEdge(t, graph, EdgeTest, "test_dead_override", "dead_override", "dead_override")
	assertEdge(t, graph, EdgeAuthorization, "Reviewer.review", "Reviewer.authorize", "self.authorize")
	assertReachability(t, graph, "handle_override", ReachableProduction)
	assertReachability(t, graph, "dead_override", ReachableTestOnly)
	assertReachability(t, graph, "test_dead_override", ReachableTestOnly)

	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "OVERRIDE_ENABLED") {
		t.Fatalf("serialized Python graph leaked source: %s", encoded)
	}
}

func TestBuildIndexesGoAndPythonTogether(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{
		{Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\nfunc main() {}\n")},
		{Path: "worker.py", Kind: discovery.KindSource, Content: []byte("def work():\n    pass\n")},
	}})
	if got := strings.Join([]string{string(graph.Languages[0]), string(graph.Languages[1])}, ","); got != "go,python" {
		t.Fatalf("languages = %q", got)
	}
}

func TestPythonCommentsAndStringsDoNotCreateSymbolsOrCalls(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{{
		Path: "worker.py", Kind: discovery.KindSource, Content: []byte(`message = "fake_call()"
# def fake_function():
def real_function():
    """fake_doc_call()"""
    return "still_not_a_call()"
`),
	}}})
	if len(graph.Symbols) != 1 || graph.Symbols[0].Name != "real_function" || len(graph.Edges) != 0 {
		t.Fatalf("masked Python source produced false structure: %#v", graph)
	}
}
