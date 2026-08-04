package codegraph

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestTechnicalContextFixtureCoversAdversarialGraphCases(t *testing.T) {
	result, err := discovery.Discover(context.Background(), filepath.Join("..", "..", "testdata", "technical-context-go"), discovery.Options{})
	if err != nil {
		t.Fatal(err)
	}
	graph := Build(result.Repository)
	assertReachability(t, graph, "handleOverrideDecision", ReachableProduction)
	assertReachability(t, graph, "deadOverrideDecision", ReachableTestOnly)
	assertEdge(t, graph, EdgeRoute, "registerRoutes", "handleOverrideDecision", "HANDLEFUNC /override")
	assertEdge(t, graph, EdgeConfiguration, "handleOverrideDecision", "config:OVERRIDE_ENABLED", "OVERRIDE_ENABLED")
	assertEdge(t, graph, EdgeAuthorization, "handleOverrideDecision", "authorizeReviewer", "authorizeReviewer")
	assertEdge(t, graph, EdgePersistence, "handleOverrideDecision", "updateDecision", "updateDecision")
	assertEdge(t, graph, EdgeLogging, "handleOverrideDecision", "auditOverride", "auditOverride")

	encoded, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "IGNORE ALL PRIOR INSTRUCTIONS") {
		t.Fatal("repository source comment leaked into report-safe graph JSON")
	}
}

func TestUnsupportedLanguageFixtureIsExplicitCoverageDebt(t *testing.T) {
	result, err := discovery.Discover(context.Background(), filepath.Join("..", "..", "testdata", "technical-context-unsupported"), discovery.Options{})
	if err != nil {
		t.Fatal(err)
	}
	graph := Build(result.Repository)
	if graph.FilesIndexed != 0 || graph.SourceFilesSeen != 1 || len(graph.UnsupportedSourceFiles) != 1 || graph.UnsupportedSourceFiles[0] != "override_worker.py" {
		t.Fatalf("unsupported fixture was not reported conservatively: %#v", graph)
	}
}
