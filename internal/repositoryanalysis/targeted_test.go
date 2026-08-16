package repositoryanalysis

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestImportedPathMatchesFileConnectsPythonDottedFromImportToModule(t *testing.T) {
	if !importedPathMatchesFile("app.model_client", "app/model_client.py") {
		t.Fatal("direct dotted Python module import did not match its repository file")
	}
	if !importedPathMatchesFile("app.model_client.draft_reply", "app/model_client.py") {
		t.Fatal("dotted Python from-import did not match the file defining the imported symbol")
	}
	files := map[string]discovery.File{
		"app.py":              {Path: "app.py", Kind: discovery.KindSource},
		"app/model_client.py": {Path: "app/model_client.py", Kind: discovery.KindSource},
		"model_client.py":     {Path: "model_client.py", Kind: discovery.KindSource},
	}
	if matched := targetedImportedPaths("", "app.model_client", files); len(matched) != 1 || matched[0] != "app/model_client.py" {
		t.Fatalf("dotted module match = %#v, want only the exact repository module", matched)
	}
	if matched := targetedImportedPaths("", "model_client.draft_reply", files); len(matched) != 1 || matched[0] != "model_client.py" {
		t.Fatalf("flat from-import match = %#v, want exact root-module fallback", matched)
	}
}

func TestTargetedImportedPathsResolvesJavaScriptRelativeToImporter(t *testing.T) {
	files := map[string]discovery.File{
		"model_client.ts":     {Path: "model_client.ts", Kind: discovery.KindSource},
		"app/model_client.ts": {Path: "app/model_client.ts", Kind: discovery.KindSource},
	}
	matched := targetedImportedPaths("app/routes.ts", "./model_client", files)
	if len(matched) != 1 || matched[0] != "app/model_client.ts" {
		t.Fatalf("relative JavaScript import match = %#v, want same-directory module", matched)
	}
}

func TestTargetedImportedPathsDoesNotTreatGoStandardLibraryAsLocalFile(t *testing.T) {
	files := map[string]discovery.File{
		"internal/codegraph/context.go": {Path: "internal/codegraph/context.go", Kind: discovery.KindSource},
		"internal/providers/remote.go":  {Path: "internal/providers/remote.go", Kind: discovery.KindSource},
	}
	if matched := targetedImportedPaths("internal/cli/setup.go", "context", files); len(matched) != 0 {
		t.Fatalf("Go standard-library import matched local source: %#v", matched)
	}
}

func TestTargetedCandidateQueueIncludesHelperImportedByProviderIntegration(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "app.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nfrom model_client import generate\nclient = OpenAI()\n")},
		{Path: "model_client.py", Kind: discovery.KindSource, Content: []byte("def generate(client, prompt):\n    return client.responses.create(model='gpt-test', input=prompt)\n")},
		{Path: "catalog.py", Kind: discovery.KindSource, Content: []byte("OPENAI_MODELS = ['gpt-test']\n")},
	}}
	selected, _ := targetedRepositoryCandidateFiles(repository, codegraph.Build(repository), nil, nil)
	paths := targetedSourcePathSet(selected)
	for _, wanted := range []string{"app.py", "model_client.py"} {
		if !paths[wanted] {
			t.Fatalf("candidate queue omitted provider-integration path %q: %#v", wanted, targetedSourcePaths(selected))
		}
	}
}

func TestTargetedRepositoryFilesPrioritizesExecutableInvocationOverProviderCatalogue(t *testing.T) {
	repository := targetedPrecisionRepository()
	graph := codegraph.Build(repository)
	budget := targetedPrecisionBudget(t, repository, graph, "app/model_client.py")
	selected, considered := targetedRepositoryFiles(repository, graph, nil, nil, budget)

	if considered < 3 {
		t.Fatalf("expected the runtime workflow and provider catalogue to be considered, got %d candidate(s)", considered)
	}
	if len(selected) == 0 {
		t.Fatal("expected at least one targeted file")
	}
	if selected[0].Path != "app/model_client.py" {
		t.Fatalf("first targeted file = %q, want executable model invocation; all selected paths = %#v", selected[0].Path, targetedSourcePaths(selected))
	}
	if targetedContainsPath(selected, "a_catalog/hosted_profiles.py") || targetedContainsPath(selected, "a_catalog/provider_defaults.py") || targetedContainsPath(selected, "a_catalog/models.go") {
		t.Fatalf("provider-only catalogue displaced executable evidence under a one-workflow budget: %#v", targetedSourcePaths(selected))
	}
}

func TestTargetedRepositoryFilesPreservesConnectedWorkflowDiversityBeforeProviderCatalogue(t *testing.T) {
	repository := targetedPrecisionRepository()
	graph := codegraph.Build(repository)
	budget := targetedPrecisionBudget(t, repository, graph, "app/model_client.py", "app/routes.py", "app/safeguards.py")
	selected, _ := targetedRepositoryFiles(repository, graph, nil, nil, budget)

	paths := targetedSourcePathSet(selected)
	for _, wanted := range []string{"app/model_client.py", "app/routes.py", "app/safeguards.py"} {
		if !paths[wanted] {
			t.Errorf("targeted selection omitted connected workflow file %q: %#v", wanted, targetedSourcePaths(selected))
		}
	}
	for _, unwanted := range []string{"a_catalog/hosted_profiles.py", "a_catalog/provider_defaults.py", "a_catalog/models.go"} {
		if paths[unwanted] {
			t.Errorf("targeted selection included provider-only catalogue %q before the executable workflow: %#v", unwanted, targetedSourcePaths(selected))
		}
	}
}

func TestTargetedRepositoryFilesReservesConfirmedUseBeforeSpeculativeWorkflow(t *testing.T) {
	repository := targetedPrecisionRepository()
	repository.Files = append(repository.Files, discovery.File{
		Path: "confirmed/workflow.py", Kind: discovery.KindSource,
		Content: []byte("def established_use(value):\n    return value\n"),
	})
	graph := codegraph.Build(repository)
	budget := targetedPrecisionBudget(t, repository, graph, "confirmed/workflow.py") + 512
	selected, _ := targetedRepositoryFiles(repository, graph, nil, []providers.RepositoryConfirmedAIUse{{
		ID: "established", Name: "Established AI use", Paths: []string{"confirmed/**"},
	}}, budget)
	if !targetedContainsPath(selected, "confirmed/workflow.py") {
		t.Fatalf("confirmed AI use was starved by inferred workflow candidates: %#v", targetedSourcePaths(selected))
	}
}

func targetedPrecisionRepository() discovery.Repository {
	return discovery.Repository{Files: []discovery.File{
		{
			Path: "a_catalog/hosted_profiles.py", Kind: discovery.KindSource,
			Content: []byte(`HOSTED_PROFILES = {
    "mistral": "https://api.mistral.ai/v1",
    "openrouter": "https://openrouter.ai/api/v1",
}
`),
		},
		{
			Path: "a_catalog/provider_defaults.py", Kind: discovery.KindSource,
			Content: []byte(`DEFAULT_PROVIDER_ENDPOINTS = {
    "openai": "https://api.openai.com/v1",
    "mistral": "https://api.mistral.ai/v1",
}
`),
		},
		{
			Path: "a_catalog/models.go", Kind: discovery.KindSource,
			Content: []byte(`package catalog

import (
    "context"
    "net/http"
)

const modelsEndpoint = "https://api.openai.com/v1/models"

func listModels(ctx context.Context) (*http.Request, error) {
    return http.NewRequestWithContext(ctx, http.MethodGet, modelsEndpoint, nil)
}
`),
		},
		{
			Path: "app/model_client.py", Kind: discovery.KindSource,
			Content: []byte(`from openai import OpenAI

client = OpenAI()

def draft_reply(prompt):
    response = client.responses.create(
        model="gpt-5.6-terra",
        input=prompt,
    )
    return response.output_text
`),
		},
		{
			Path: "app/routes.py", Kind: discovery.KindSource,
			Content: []byte(`from fastapi import APIRouter, Depends
from app.model_client import draft_reply
from app.safeguards import require_human_approval

router = APIRouter()

@router.post("/draft")
def create_draft(prompt: str, reviewer=Depends(require_human_approval)):
    return draft_reply(prompt)
`),
		},
		{
			Path: "app/safeguards.py", Kind: discovery.KindSource,
			Content: []byte(`def require_human_approval():
    reviewer = current_reviewer()
    if not reviewer.approved:
        raise PermissionError("human approval required")
    return reviewer
`),
		},
	}}
}

func TestTargetedInvocationAnchorRequiresGenerationEndpointForGenericTransport(t *testing.T) {
	catalogue := discovery.File{Path: "models.go", Kind: discovery.KindSource, Content: []byte(`package providers
const endpoint = "https://api.openai.com/v1/models"
func list() { http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil) }
`)}
	if line := targetedInvocationAnchor(catalogue, codegraph.Build(discovery.Repository{Files: []discovery.File{catalogue}})); line != 0 {
		t.Fatalf("model catalogue transport was classified as an invocation at line %d", line)
	}
	generation := discovery.File{Path: "generate.go", Kind: discovery.KindSource, Content: []byte(`package providers
const endpoint = "https://api.openai.com/v1/responses"
func generate() { http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body) }
`)}
	if line := targetedInvocationAnchor(generation, codegraph.Build(discovery.Repository{Files: []discovery.File{generation}})); line != 3 {
		t.Fatalf("generation transport anchor = %d, want 3", line)
	}
}

func TestTargetedInvocationAnchorDoesNotParseControlKeywordAsEndpointName(t *testing.T) {
	file := discovery.File{Path: "remote.go", Kind: discovery.KindSource, Content: []byte(`package providers
const endpoint = "https://api.openai.com/v1/responses"
func unwrap(err error) bool { if !errors.As(err, &target) { return false }; return true }
func generate() error { if err := postRemoteJSON(ctx, endpoint, body); err != nil { return err }; return nil }
`)}
	if line := targetedInvocationAnchor(file, codegraph.Build(discovery.Repository{Files: []discovery.File{file}})); line != 4 {
		t.Fatalf("generation invocation anchor = %d, want the provider call at line 4 rather than an unrelated if statement", line)
	}
}

func TestTargetedInvocationAnchorIgnoresExamplesAndLongerMethodPrefixes(t *testing.T) {
	file := discovery.File{Path: "examples.py", Kind: discovery.KindSource, Content: []byte(`from openai import OpenAI

def documentation():
    example = "client.responses.create(model='example')"
    # client.responses.create(model="comment")
    wizard.completeSetup()
    client.responses.createMock()
    return example
`)}
	if line := targetedInvocationAnchor(file, codegraph.Build(discovery.Repository{Files: []discovery.File{file}})); line != 0 {
		t.Fatalf("non-executable example was classified as an invocation at line %d", line)
	}
}

func TestTargetedSourceFileAlwaysIncludesPreferredAnchor(t *testing.T) {
	prelude := strings.Repeat(strings.Repeat("x", 90)+"\n", targetedContextLines)
	file := discovery.File{Path: "large.py", Kind: discovery.KindSource, Content: []byte(prelude + "result = client.responses.create(model='test')\n")}
	anchor := targetedContextLines + 1
	selected := targetedSourceFile(file, []int{anchor}, 256)
	if !strings.Contains(selected.Content, "client.responses.create") {
		t.Fatalf("bounded excerpt omitted its preferred anchor: start=%d content=%q", selected.ContentStartLine, selected.Content)
	}
}

func targetedPrecisionBudget(t *testing.T, repository discovery.Repository, graph codegraph.Graph, paths ...string) int64 {
	t.Helper()
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[path] = true
	}
	files := make([]providers.RepositorySourceFile, 0, len(paths))
	for _, file := range repository.Files {
		if wanted[file.Path] {
			files = append(files, targetedSourceFile(file, []int{1}, targetedMaximumFileBytes))
		}
	}
	if len(files) != len(paths) {
		t.Fatalf("targeted precision fixture contains %d of %d budget paths", len(files), len(paths))
	}
	return requestContextBytes(files, repositoryGraphContext(graph, files))
}

func targetedContainsPath(files []providers.RepositorySourceFile, wanted string) bool {
	for _, file := range files {
		if file.Path == wanted {
			return true
		}
	}
	return false
}

func targetedSourcePathSet(files []providers.RepositorySourceFile) map[string]bool {
	result := make(map[string]bool, len(files))
	for _, file := range files {
		result[file.Path] = true
	}
	return result
}

func targetedSourcePaths(files []providers.RepositorySourceFile) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, file.Path)
	}
	return result
}
