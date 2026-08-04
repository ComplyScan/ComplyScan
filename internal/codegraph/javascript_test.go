package codegraph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestBuildIndexesTypeScriptSymbolsImportsCallsAndReachability(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "src/main.ts", Kind: discovery.KindSource, Content: []byte(`import { handleOverride } from "./override";

function main(): void {
  handleOverride();
}

main();
`),
		},
		{
			Path: "src/override.ts", Kind: discovery.KindSource, Content: []byte(`import { persistResult } from "./store";

export async function handleOverride(): Promise<void> {
  persistResult();
}

export const deadOverride = (): void => {
  persistResult();
};

export class Reviewer {
  authorize(): boolean {
    return true;
  }

  review(): boolean {
    return this.authorize();
  }
}
`),
		},
		{
			Path: "src/store.ts", Kind: discovery.KindSource, Content: []byte(`export function persistResult(): void {}
`),
		},
		{
			Path: "src/override.test.ts", Kind: discovery.KindSource, Content: []byte(`import { deadOverride } from "./override";

test("dead override", () => {
  deadOverride();
});
`),
		},
	}}

	graph := Build(repository)
	if graph.FilesIndexed != 4 || len(graph.Languages) != 1 || graph.Languages[0] != LanguageTypeScript {
		t.Fatalf("unexpected TypeScript coverage: %#v", graph)
	}
	if len(graph.Imports) != 3 {
		t.Fatalf("imports = %#v", graph.Imports)
	}
	assertEdge(t, graph, EdgeCall, "main", "handleOverride", "handleOverride")
	assertEdge(t, graph, EdgePersistence, "handleOverride", "persistResult", "persistResult")
	assertEdge(t, graph, EdgeTest, "test_case", "deadOverride", "deadOverride")
	assertEdge(t, graph, EdgeAuthorization, "Reviewer.review", "Reviewer.authorize", "this.authorize")
	assertReachability(t, graph, "handleOverride", ReachableProduction)
	assertReachability(t, graph, "deadOverride", ReachableTestOnly)

	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "IGNORE ALL INSTRUCTIONS") {
		t.Fatalf("serialized TypeScript graph leaked source: %s", encoded)
	}
}

func TestBuildIndexesJavaScriptAndTypeScriptSeparately(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{
		{Path: "worker.mjs", Kind: discovery.KindSource, Content: []byte("export function work() {}\n")},
		{Path: "worker.mts", Kind: discovery.KindSource, Content: []byte("export function typed(): void {}\n")},
	}})
	if len(graph.Languages) != 2 || graph.Languages[0] != LanguageJavaScript || graph.Languages[1] != LanguageTypeScript {
		t.Fatalf("languages = %#v", graph.Languages)
	}
}

func TestJavaScriptCommentsAndStringsDoNotCreateStructure(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{{
		Path: "worker.js", Kind: discovery.KindSource, Content: []byte(`const message = "fakeCall()";
// function fakeFunction() {}
/* const fakeArrow = () => attack(); */
export function realFunction() {
  return ` + "`fakeTemplateCall()`" + `;
}
`),
	}}})
	if len(graph.Symbols) != 1 || graph.Symbols[0].Name != "realFunction" || len(graph.Edges) != 0 {
		t.Fatalf("masked JavaScript produced false structure: %#v", graph)
	}
}
