package rules

import (
	"context"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

func TestAIUsageRuleDetectsSupportedProvidersAndFrameworks(t *testing.T) {
	repo := repositoryWithFile("package.json", discovery.KindManifest, `
openai
anthropic
@google/generative-ai
mistralai
cohere
huggingface_hub
ollama
litellm
langchain-openai
llama_index
@ai-sdk/openai
openrouter
`)
	findings, err := (AIUsageRule{}).Run(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 12 {
		t.Fatalf("got %d findings, want 12: %#v", len(findings), findings)
	}
	for _, finding := range findings {
		if finding.Severity != SeverityInfo || finding.RuleID != "AI-DISC-001" {
			t.Errorf("unexpected finding: %#v", finding)
		}
	}
}

func TestAIUsageRuleDoesNotTreatDocumentationAsRuntimeUsage(t *testing.T) {
	repo := repositoryWithFile("README.md", discovery.KindReadme, "We considered OpenAI but do not use it.")
	findings, err := (AIUsageRule{}).Run(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("got findings %#v", findings)
	}
}

func repositoryWithFile(path string, kind discovery.FileKind, content string) discovery.Repository {
	return discovery.Repository{Files: []discovery.File{{Path: path, Kind: kind, Content: []byte(content)}}}
}
