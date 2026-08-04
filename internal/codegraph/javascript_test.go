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

func TestBuildIndexesJavaScriptFrameworkRelationships(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "src/api.ts", Kind: discovery.KindSource, Content: []byte(`import { requireReviewer } from "./auth";

export async function handleOverride(): Promise<void> {
  const enabled = process.env.OVERRIDE_ENABLED;
  await prisma.decision.update({});
  logger.info("override stored");
}

app.post("/override", requireReviewer, handleOverride);
fastify.route({ method: "DELETE", url: "/override/:id", preHandler: requireReviewer, handler: handleOverride });

router.patch("/inline", requireReviewer, async (_request, response) => {
  await repository.save();
  audit.recordEvent();
  response.send();
});
`),
		},
		{
			Path: "src/auth.ts", Kind: discovery.KindSource, Content: []byte(`export function requireReviewer(): boolean {
  return true;
}
`),
		},
		{
			Path: "src/controller.ts", Kind: discovery.KindSource, Content: []byte(`@Controller("decisions")
export class DecisionController {
  @Post("override")
  @UseGuards(ReviewerGuard)
  overrideDecision(): void {
    repository.save();
    logger.info("override");
  }
}
`),
		},
		{
			Path: "src/app/api/review/route.ts", Kind: discovery.KindSource, Content: []byte(`export async function POST(): Promise<void> {
  await audit.recordEvent();
}
`),
		},
	}}

	graph := Build(repository)
	assertEdge(t, graph, EdgeRoute, "framework-route:POST /override", "handleOverride", "POST /override")
	assertEdge(t, graph, EdgeRoute, "framework-route:DELETE /override/:id", "handleOverride", "DELETE /override/:id")
	assertEdge(t, graph, EdgeAuthorization, "handleOverride", "requireReviewer", "requireReviewer")
	assertEdge(t, graph, EdgeConfiguration, "handleOverride", "config:OVERRIDE_ENABLED", "OVERRIDE_ENABLED")
	assertEdge(t, graph, EdgePersistence, "handleOverride", "prisma.decision.update", "prisma.decision.update")
	assertEdge(t, graph, EdgeLogging, "handleOverride", "logger.info", "logger.info")
	assertEdge(t, graph, EdgeRoute, "framework-route:PATCH /inline", "route_handler", "PATCH /inline")
	assertEdge(t, graph, EdgePersistence, "route_handler", "repository.save", "repository.save")
	assertEdge(t, graph, EdgeLogging, "route_handler", "audit.recordEvent", "audit.recordEvent")
	assertEdge(t, graph, EdgeRoute, "framework-route:POST /decisions/override", "overrideDecision", "POST /decisions/override")
	assertEdge(t, graph, EdgeAuthorization, "overrideDecision", "ReviewerGuard", "ReviewerGuard")
	assertEdge(t, graph, EdgeRoute, "framework-route:POST /api/review", "POST", "POST /api/review")
	assertReachability(t, graph, "handleOverride", ReachableProduction)
	assertReachability(t, graph, "overrideDecision", ReachableProduction)
}

func TestJavaScriptCommentsCannotCreateFrameworkRelationships(t *testing.T) {
	graph := Build(discovery.Repository{Files: []discovery.File{{
		Path: "app.js", Kind: discovery.KindSource, Content: []byte(`export function worker() {
  // app.post("/fake", requireAuth, fakeHandler);
  const message = 'process.env.FAKE_FLAG logger.info()';
  return message;
}
`),
	}}})
	if len(graph.Edges) != 0 {
		t.Fatalf("comment or string created JavaScript relationships: %#v", graph.Edges)
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
