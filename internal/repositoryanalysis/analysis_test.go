package repositoryanalysis

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type recordingReviewer struct {
	requests []providers.RepositoryAnalysisRequest
}

func (reviewer *recordingReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	result := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
		UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if request.Mode != providers.RepositoryAnalysisSynthesis && len(request.Files) > 0 {
		file := request.Files[0]
		result.AIUses = append(result.AIUses, providers.RepositoryAIUse{
			ID: "use-" + strings.ReplaceAll(request.Scope, " ", "-"), Name: "AI use", Purpose: "Test analysis", Lifecycle: "unknown", Confidence: "low",
			Evidence: []providers.RepositoryCitation{{Path: file.Path, Line: 1, Summary: "Submitted source"}},
		})
	} else if len(request.SubsystemSummaries) > 0 {
		for _, summary := range request.SubsystemSummaries {
			result.AIUses = append(result.AIUses, summary.AIUses...)
		}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: result,
		Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files)},
		Usage:    providers.Usage{PromptTokens: 10, CompletionTokens: 2},
	}, nil
}

func TestRunUsesOneRequestWhenRepositoryFits(t *testing.T) {
	reviewer := &recordingReviewer{}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\nfunc main() {}\n")},
		{Path: "notes.txt", Kind: discovery.KindOtherText, Content: []byte("not model context")},
	}}
	result, err := Run(context.Background(), reviewer, repository, []framework.TechnicalEvidenceReport{{
		Pack: framework.PackReference{ID: "pack"}, Objectives: []framework.ObjectiveAssessment{{ID: "OBJ", Title: "Objective"}},
	}}, nil, Options{Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 1 || reviewer.requests[0].Mode != providers.RepositoryAnalysisFull {
		t.Fatalf("expected one full request, got %#v", reviewer.requests)
	}
	if len(reviewer.requests[0].Files) != 1 || reviewer.requests[0].Objectives[0].ID != "pack/OBJ" {
		t.Fatalf("unexpected prepared request: %#v", reviewer.requests[0])
	}
	if reviewer.requests[0].Graph.IndexedSourceFiles != 1 || len(reviewer.requests[0].Graph.Symbols) == 0 {
		t.Fatalf("repository graph was not supplied with full source: %#v", reviewer.requests[0].Graph)
	}
	if result.Coverage.Mode != providers.RepositoryAnalysisFull {
		t.Fatalf("unexpected coverage: %#v", result.Coverage)
	}
}

func TestRunUsesSubsystemsAndSynthesisWhenRepositoryExceedsBudget(t *testing.T) {
	reviewer := &recordingReviewer{}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "api/main.go", Kind: discovery.KindSource, Content: []byte(strings.Repeat("a", 14_000))},
		{Path: "worker/main.go", Kind: discovery.KindSource, Content: []byte(strings.Repeat("b", 14_000))},
	}}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.Anthropic, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 3 {
		t.Fatalf("expected two subsystem requests and synthesis, got %d", len(reviewer.requests))
	}
	if reviewer.requests[0].Mode != providers.RepositoryAnalysisSubsystem || reviewer.requests[1].Mode != providers.RepositoryAnalysisSubsystem || reviewer.requests[2].Mode != providers.RepositoryAnalysisSynthesis {
		t.Fatalf("unexpected request sequence: %s, %s, %s", reviewer.requests[0].Mode, reviewer.requests[1].Mode, reviewer.requests[2].Mode)
	}
	if result.Coverage.Mode != providers.RepositoryAnalysisSynthesis || result.Coverage.Subsystems != 2 || len(result.Result.AIUses) != 2 {
		t.Fatalf("unexpected hierarchical result: %#v", result)
	}
}

func TestRunRedactsRepositorySourceBeforeReviewer(t *testing.T) {
	reviewer := &recordingReviewer{}
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	_, err := Run(context.Background(), reviewer, discovery.Repository{Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte(fmt.Sprintf("var key = %q\n", secret)),
	}}}, nil, nil, Options{Provider: providers.OpenAI, MaxInputTokens: 8_000})
	if err != nil {
		t.Fatal(err)
	}
	content := reviewer.requests[0].Files[0].Content
	if strings.Contains(content, secret) || !strings.Contains(content, "sk-proj-****3456") {
		t.Fatalf("source was not redacted: %s", content)
	}
}

func TestRunFullModeReportsContextOverflow(t *testing.T) {
	reviewer := &recordingReviewer{}
	_, err := Run(context.Background(), reviewer, discovery.Repository{Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte(strings.Repeat("x", 30_000)),
	}}}, nil, nil, Options{Mode: ModeFull, Provider: providers.OpenAI, MaxInputTokens: 8_000})
	if err == nil || !strings.Contains(err.Error(), "exceeding the configured full-analysis budget") {
		t.Fatalf("expected context overflow, got %v", err)
	}
}
