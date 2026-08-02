package rules

import (
	"context"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

func TestAIUsageRuleDetectsSupportedProvidersAndFrameworks(t *testing.T) {
	repo := repositoryWithFile("package.json", discovery.KindManifest, `{
  "dependencies": {
    "openai": "1.0.0",
    "@anthropic-ai/sdk": "1.0.0",
    "@google/generative-ai": "1.0.0",
    "@mistralai/mistralai": "1.0.0",
    "cohere-ai": "1.0.0",
    "@huggingface/inference": "1.0.0",
    "ollama": "1.0.0",
    "litellm": "1.0.0",
    "langchain": "1.0.0",
    "llamaindex": "1.0.0",
    "@ai-sdk/openai": "1.0.0",
    "@openrouter/ai-sdk-provider": "1.0.0"
  }
}`)
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

func TestAIUsageRuleStreamsFindings(t *testing.T) {
	repo := repositoryWithFile("requirements.txt", discovery.KindManifest, "openai\nanthropic\n")
	var findings []Finding
	err := (AIUsageRule{}).RunStreaming(context.Background(), repo, func(finding Finding) error {
		findings = append(findings, finding)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("streamed %d findings, want 2", len(findings))
	}
}

func TestAIUsageRuleAggregatesProviderLocations(t *testing.T) {
	repo := discovery.Repository{Files: []discovery.File{
		{Path: "first.py", Kind: discovery.KindSource, Content: []byte("import openai\n")},
		{Path: "second.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\n")},
	}}
	findings, err := (AIUsageRule{}).Run(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want one aggregated finding: %#v", len(findings), findings)
	}
	if findings[0].Occurrences != 2 || len(findings[0].Locations) != 2 {
		t.Fatalf("unexpected aggregation: %#v", findings[0])
	}
}

func repositoryWithFile(path string, kind discovery.FileKind, content string) discovery.Repository {
	return discovery.Repository{Files: []discovery.File{{Path: path, Kind: kind, Content: []byte(content)}}}
}
