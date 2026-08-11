package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	nethtml "golang.org/x/net/html"
)

func TestLiveOllamaCatalogueSearchAndVariants(t *testing.T) {
	if os.Getenv("COMPLYSCAN_LIVE_OLLAMA_CATALOGUE") != "1" {
		t.Skip("set COMPLYSCAN_LIVE_OLLAMA_CATALOGUE=1 to query ollama.com")
	}
	catalogue := newOllamaWebCatalogue()
	models, err := catalogue.Search(context.Background(), "qwen3.5")
	if err != nil {
		t.Fatal(err)
	}
	var selected ollamaCatalogueModel
	for _, model := range models {
		if model.Name == "qwen3.5" {
			selected = model
			break
		}
	}
	if selected.Name == "" {
		t.Fatalf("official qwen3.5 model missing from results: %#v", models)
	}
	variants, err := catalogue.Variants(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, variant := range variants {
		if variant.Tag == "qwen3.5:9b" && variant.SizeGB > 0 {
			found = true
		}
		if isOllamaCloudTag(variant.Tag) {
			t.Fatalf("cloud-only variant leaked into local catalogue: %#v", variant)
		}
	}
	if !found {
		t.Fatalf("qwen3.5:9b with a reported size missing from variants: %#v", variants)
	}
}

func TestParseOllamaCatalogueModelsSupportsOfficialAndCommunityEntries(t *testing.T) {
	document, err := nethtml.Parse(strings.NewReader(`
<html><body>
  <a href="/library/qwen3.5" class="group w-full">
    <h2><span>qwen3.5</span></h2>
    <p class="max-w-lg break-words">A capable &amp; compact coding model.</p>
  </a>
  <a href="/frob/qwen3.5" class="group w-full">
    <h2><span>frob/qwen3.5</span></h2>
    <p class="max-w-lg">Community quants.</p>
  </a>
  <a href="https://example.com/unsafe" class="group w-full"><p class="max-w-lg">Ignore.</p></a>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	models := parseOllamaCatalogueModels(document)
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].Name != "qwen3.5" || models[0].Path != "/library/qwen3.5" || models[0].Description != "A capable & compact coding model." {
		t.Fatalf("official model = %#v", models[0])
	}
	if models[1].Name != "frob/qwen3.5" || models[1].Path != "/frob/qwen3.5" {
		t.Fatalf("community model = %#v", models[1])
	}
}

func TestParseOllamaCatalogueVariantsIncludesSizesAndExcludesCloud(t *testing.T) {
	document, err := nethtml.Parse(strings.NewReader(`
<html><body>
  <a href="/library/qwen3.5:latest" class="sm:hidden group"><p class="flex text-neutral-500">6.6GB · 256K context window · Text</p></a>
  <a href="/library/qwen3.5:9b" class="sm:hidden group"><p class="flex text-neutral-500">6.6 GB · 256K context window · Text</p></a>
  <a href="/library/qwen3.5:397b-cloud" class="sm:hidden group"><p class="flex text-neutral-500">Medium Usage · Cloud</p></a>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	variants := parseOllamaCatalogueVariants(document, ollamaCatalogueModel{Name: "qwen3.5", Path: "/library/qwen3.5"})
	if len(variants) != 2 {
		t.Fatalf("variants = %#v", variants)
	}
	if variants[0].Tag != "qwen3.5:latest" || variants[0].SizeGB != 6.6 || variants[1].Tag != "qwen3.5:9b" || variants[1].SizeGB != 6.6 {
		t.Fatalf("variants = %#v", variants)
	}
}

func TestParseOllamaCatalogueCommunityVariantKeepsNamespace(t *testing.T) {
	document, err := nethtml.Parse(strings.NewReader(`<a href="/frob/qwen3.5:122b" class="sm:hidden"><p class="text-neutral-500">102GB · 256K context window</p></a>`))
	if err != nil {
		t.Fatal(err)
	}
	variants := parseOllamaCatalogueVariants(document, ollamaCatalogueModel{Name: "frob/qwen3.5", Path: "/frob/qwen3.5"})
	if len(variants) != 1 || variants[0].Tag != "frob/qwen3.5:122b" || variants[0].SizeGB != 102 {
		t.Fatalf("variants = %#v", variants)
	}
}

type fakeOllamaCatalogue struct {
	models   []ollamaCatalogueModel
	variants []ollamaCatalogueVariant
	err      error
	query    string
}

func (catalogue *fakeOllamaCatalogue) Search(_ context.Context, query string) ([]ollamaCatalogueModel, error) {
	catalogue.query = query
	return catalogue.models, catalogue.err
}

func (catalogue *fakeOllamaCatalogue) Variants(_ context.Context, _ ollamaCatalogueModel) ([]ollamaCatalogueVariant, error) {
	return catalogue.variants, catalogue.err
}

func TestPromptOllamaModelSearchesCatalogueAndSelectsVariant(t *testing.T) {
	catalogue := &fakeOllamaCatalogue{
		models: []ollamaCatalogueModel{{Name: "qwen3.5", Path: "/library/qwen3.5", Description: "Coding and reasoning model."}},
		variants: []ollamaCatalogueVariant{
			{Tag: "qwen3.5:4b", Detail: "3.4GB · 256K context window", SizeGB: 3.4},
			{Tag: "qwen3.5:9b", Detail: "6.6GB · 256K context window", SizeGB: 6.6},
		},
	}
	var output bytes.Buffer
	selectCall := 0
	prompt := promptSession{
		reader: bufio.NewReader(strings.NewReader("qwen\n")), output: &output,
		selectOne: func(label, _ string, options []terminalChoice) (string, error) {
			selectCall++
			switch selectCall {
			case 1:
				if label != "Ollama model" || options[len(options)-2].Value != catalogueModelChoice {
					t.Fatalf("main selector: label=%q options=%#v", label, options)
				}
				return catalogueModelChoice, nil
			case 2:
				if label != "Ollama catalogue results" || !strings.Contains(options[0].Label, "qwen3.5") {
					t.Fatalf("catalogue selector: label=%q options=%#v", label, options)
				}
				return "/library/qwen3.5", nil
			case 3:
				if label != "qwen3.5 variants" || !strings.Contains(options[1].Label, "6.6 GB") {
					t.Fatalf("variant selector: label=%q options=%#v", label, options)
				}
				return "qwen3.5:9b", nil
			default:
				t.Fatalf("unexpected selector call %d", selectCall)
				return "", nil
			}
		},
	}
	model, err := promptOllamaModelWithCatalogue(prompt, defaultSetupModel, nil, catalogue)
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen3.5:9b" || catalogue.query != "qwen" || selectCall != 3 {
		t.Fatalf("model=%q query=%q calls=%d", model, catalogue.query, selectCall)
	}
	if !strings.Contains(output.String(), "only your search words") || !strings.Contains(output.String(), "Repository content is never sent") {
		t.Fatalf("privacy disclosure missing:\n%s", output.String())
	}
}

func TestPromptOllamaCatalogueFallsBackToExactTagWhenOffline(t *testing.T) {
	catalogue := &fakeOllamaCatalogue{err: errors.New("offline")}
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("code\nmy-local-model:7b\n")), output: &output}
	model, err := promptOllamaCatalogue(prompt, catalogue)
	if err != nil {
		t.Fatal(err)
	}
	if model != "my-local-model:7b" || !strings.Contains(output.String(), "Catalogue search is unavailable") || !strings.Contains(output.String(), "Enter an exact Ollama tag instead") {
		t.Fatalf("model=%q output=%s", model, output.String())
	}
}

func TestPromptCustomOllamaModelRejectsCloudOnlyTags(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("qwen3.5:cloud\nqwen3.5:9b\n")), output: &output}
	model, err := promptCustomOllamaModel(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if model != "qwen3.5:9b" || !strings.Contains(output.String(), "unavailable in local privacy mode") {
		t.Fatalf("model=%q output=%s", model, output.String())
	}
}
