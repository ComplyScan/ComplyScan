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
	if strings.Contains(string(encoded), "IGNORE ALL INSTRUCTIONS") {
		t.Fatalf("serialized Python graph leaked source: %s", encoded)
	}
}

func TestBuildIndexesPythonFrameworkRelationships(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "app/api.py", Kind: discovery.KindSource, Content: []byte(`import os
from fastapi import Depends, FastAPI
from app.auth import authorize_reviewer

app = FastAPI()

@app.post("/override", dependencies=[Depends(authorize_reviewer)])
def override_decision():
    feature = os.getenv("OVERRIDE_ENABLED")
    session.commit()
    logger.info("override stored")
    return feature
`),
		},
		{
			Path: "app/auth.py", Kind: discovery.KindSource, Content: []byte(`def authorize_reviewer():
    return True
`),
		},
		{
			Path: "app/flask_api.py", Kind: discovery.KindSource, Content: []byte(`@bp.route("/review", methods=["POST", "PATCH"])
@permission_required("review.change")
def review_decision():
    repository.save()
`),
		},
		{
			Path: "app/urls.py", Kind: discovery.KindSource, Content: []byte(`from django.urls import path
from app.views import approve_decision

urlpatterns = [path("approve/", approve_decision)]
`),
		},
		{
			Path: "app/views.py", Kind: discovery.KindSource, Content: []byte(`def approve_decision():
    audit.record_event()
`),
		},
	}}

	graph := Build(repository)
	assertEdge(t, graph, EdgeRoute, "framework-route:POST /override", "override_decision", "POST /override")
	assertEdge(t, graph, EdgeAuthorization, "override_decision", "authorize_reviewer", "Depends(authorize_reviewer)")
	assertEdge(t, graph, EdgeConfiguration, "override_decision", "config:OVERRIDE_ENABLED", "OVERRIDE_ENABLED")
	assertEdge(t, graph, EdgePersistence, "override_decision", "session.commit", "session.commit")
	assertEdge(t, graph, EdgeLogging, "override_decision", "logger.info", "logger.info")
	assertEdge(t, graph, EdgeRoute, "framework-route:POST /review", "review_decision", "POST /review")
	assertEdge(t, graph, EdgeRoute, "framework-route:PATCH /review", "review_decision", "PATCH /review")
	assertEdge(t, graph, EdgeAuthorization, "review_decision", "permission_required", "permission_required")
	assertEdge(t, graph, EdgePersistence, "review_decision", "repository.save", "repository.save")
	assertEdge(t, graph, EdgeRoute, "django-route:ANY /approve/", "approve_decision", "ANY /approve/")
	assertEdge(t, graph, EdgeLogging, "approve_decision", "audit.record_event", "audit.record_event")
	assertReachability(t, graph, "override_decision", ReachableProduction)
	assertReachability(t, graph, "review_decision", ReachableProduction)
	assertReachability(t, graph, "approve_decision", ReachableProduction)
	for _, symbol := range graph.Symbols {
		if symbol.Name == "override_decision" && !strings.Contains(graph.SourceForSymbol(symbol.ID, 4_000), `@app.post("/override"`) {
			t.Fatalf("Python model context omitted route decorators: %q", graph.SourceForSymbol(symbol.ID, 4_000))
		}
	}
}

func TestPythonCommentsCannotCreateFrameworkRelationships(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{{
		Path: "app.py", Kind: discovery.KindSource, Content: []byte(`def worker():
    # os.environ["FAKE_FLAG"]
    message = 'path("fake/", fake_handler)'
    return message
`),
	}}})
	if len(graph.Edges) != 0 {
		t.Fatalf("comment or string created Python relationships: %#v", graph.Edges)
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
