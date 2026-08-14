package repositoryanalysis

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type recordingReviewer struct {
	requests []providers.RepositoryAnalysisRequest
}

type repositoryFollowUpReviewer struct {
	recordingReviewer
}

type repositoryOutputRecoveryReviewer struct {
	recordingReviewer
	incomplete bool
}

func (reviewer *repositoryFollowUpReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	result, err := reviewer.recordingReviewer.ReviewRepository(ctx, request)
	if err == nil && request.AllowFollowUp {
		result.FollowUpPlan = providers.TechnicalSearchPlan{
			Needed: true, Reason: "Approval handling could change the oversight conclusion.",
			Queries: []providers.TechnicalSearchQuery{{Text: "approve_response", PathHint: "review", Reason: "Find the approval implementation."}},
		}
	}
	return result, err
}

func (reviewer *repositoryOutputRecoveryReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	if !reviewer.incomplete {
		reviewer.incomplete = true
		return providers.RepositoryAnalysisResult{}, &providers.RemoteIncompleteError{
			Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens",
			InputTokens: 4200, OutputTokens: 4096, ReasoningTokens: 3000, TokenLimit: 10_000,
		}
	}
	reviewer.recordingReviewer.requests = reviewer.requests[:len(reviewer.requests)-1]
	return reviewer.recordingReviewer.ReviewRepository(ctx, request)
}

type adaptiveReviewer struct {
	recordingReviewer
	maxSourceBytes      int64
	temporary429        bool
	temporary429Count   int
	minimumOutputTokens int
}

func (reviewer *adaptiveReviewer) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.requests = append(reviewer.requests, request)
	if reviewer.temporary429 || reviewer.temporary429Count > 0 {
		reviewer.temporary429 = false
		if reviewer.temporary429Count > 0 {
			reviewer.temporary429Count--
		}
		return providers.RepositoryAnalysisResult{}, &providers.RemoteRateLimitError{
			Provider: "OpenAI", Message: "Rate limit reached. Try again.", RetryAfter: 2 * time.Second,
		}
	}
	if request.Mode == providers.RepositoryAnalysisSubsystem && sourceFileBytes(request.Files) > reviewer.maxSourceBytes {
		return providers.RepositoryAnalysisResult{}, &providers.RemoteRateLimitError{
			Provider: "OpenAI", Message: "Request too large.", LimitTokens: 10_000,
			RequestedTokens: 21_769, RequestTooLarge: true,
		}
	}
	if request.Mode == providers.RepositoryAnalysisSubsystem && reviewer.minimumOutputTokens > 0 && request.MaxOutputTokens < reviewer.minimumOutputTokens {
		return providers.RepositoryAnalysisResult{}, &providers.RemoteIncompleteError{
			Provider: "OpenAI", Status: "incomplete", Reason: "max_output_tokens",
			InputTokens: 7_000, OutputTokens: request.MaxOutputTokens,
		}
	}
	// Avoid appending the successful request twice when delegating.
	reviewer.recordingReviewer.requests = reviewer.requests[:len(reviewer.requests)-1]
	return reviewer.recordingReviewer.ReviewRepository(ctx, request)
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
	}}, nil, Options{Mode: ModeDeep, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000})
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

func TestRunAutoSelectsStructuralAIEvidenceInsteadOfWholeRepository(t *testing.T) {
	reviewer := &recordingReviewer{}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "app.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nclient = OpenAI()\ndef generate(prompt):\n    return client.responses.create(model='gpt-test', input=prompt)\n")},
		{Path: "service.py", Kind: discovery.KindSource, Content: []byte("from app import generate\ndef handler(value):\n    return generate(value)\n")},
		{Path: "docs/providers.md", Kind: discovery.KindDocumentation, Content: []byte("Example endpoint: https://api.openai.com/v1/responses\n")},
		{Path: "unrelated.go", Kind: discovery.KindSource, Content: []byte("package unrelated\nfunc Add(a, b int) int { return a + b }\n")},
	}}
	result, err := Run(context.Background(), reviewer, repository, []framework.TechnicalEvidenceReport{{
		Pack: framework.PackReference{ID: "pack"}, Objectives: []framework.ObjectiveAssessment{{ID: "LOG", Title: "Log AI events"}, {ID: "ROBUST", Title: "Handle failures"}},
	}}, nil, Options{Mode: ModeAuto, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 1 || reviewer.requests[0].Mode != providers.RepositoryAnalysisTargeted {
		t.Fatalf("expected one targeted request, got %#v", reviewer.requests)
	}
	paths := map[string]bool{}
	for _, file := range reviewer.requests[0].Files {
		paths[file.Path] = true
	}
	if !paths["app.py"] || !paths["service.py"] || paths["docs/providers.md"] || paths["unrelated.go"] {
		t.Fatalf("targeted paths = %#v", paths)
	}
	if len(reviewer.requests[0].Objectives) != 2 || result.Coverage.Mode != providers.RepositoryAnalysisTargeted {
		t.Fatalf("targeted objectives or coverage missing: request=%#v result=%#v", reviewer.requests[0], result)
	}
}

func TestRunTargetedAllowsOneBoundedFollowUp(t *testing.T) {
	reviewer := &repositoryFollowUpReviewer{}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "app.py", Kind: discovery.KindSource, Content: []byte("from openai import OpenAI\nclient = OpenAI()\ndef generate(prompt):\n    return client.responses.create(model='gpt-test', input=prompt)\n")},
		{Path: "review/approval.py", Kind: discovery.KindSource, Content: []byte("def approve_response(value):\n    return ask_human(value)\n")},
		{Path: "other.py", Kind: discovery.KindSource, Content: []byte("def unrelated():\n    return 1\n")},
	}}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 2 || !reviewer.requests[0].AllowFollowUp || reviewer.requests[1].AllowFollowUp {
		t.Fatalf("follow-up request sequence = %#v", reviewer.requests)
	}
	paths := map[string]bool{}
	for _, file := range reviewer.requests[1].Files {
		paths[file.Path] = true
	}
	if !paths["app.py"] || !paths["review/approval.py"] || paths["other.py"] {
		t.Fatalf("final targeted paths = %#v", paths)
	}
	if !result.FollowUpRequested || result.FollowUpExcerpts != 1 || len(result.FollowUpQueries) != 1 || result.Usage.PromptTokens != 20 {
		t.Fatalf("follow-up metadata or usage = %#v", result)
	}
}

func TestRunTargetedUsesSecondAndFinalCallForCompactOutputRecovery(t *testing.T) {
	reviewer := &repositoryOutputRecoveryReviewer{}
	var progressEvents []Progress
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "app.py", Kind: discovery.KindSource,
		Content: []byte("from openai import OpenAI\nclient = OpenAI()\ndef generate(prompt):\n    return client.responses.create(model='gpt-test', input=prompt)\n"),
	}}}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		OnProgress: func(value Progress) error {
			progressEvents = append(progressEvents, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reviewer.requests) != 2 || !reviewer.requests[0].AllowFollowUp || reviewer.requests[0].OutputRecovery || reviewer.requests[1].AllowFollowUp || !reviewer.requests[1].OutputRecovery {
		t.Fatalf("output recovery request sequence = %#v", reviewer.requests)
	}
	if reviewer.requests[0].MaxOutputTokens != 4096 || reviewer.requests[1].MaxOutputTokens != 5800 {
		t.Fatalf("output allowances = %d, %d", reviewer.requests[0].MaxOutputTokens, reviewer.requests[1].MaxOutputTokens)
	}
	if !result.OutputRecoveryUsed || result.FollowUpRequested || result.Usage.PromptTokens != 4210 || result.Usage.CompletionTokens != 4098 || result.Usage.ReasoningTokens != 3000 {
		t.Fatalf("output recovery result = %#v", result)
	}
	recoveryEvents := 0
	for _, event := range progressEvents {
		if event.Stage == "targeted-output-recovery" {
			recoveryEvents++
		}
	}
	if recoveryEvents != 2 {
		t.Fatalf("output recovery progress events = %#v", progressEvents)
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

func TestRunAdaptivelySplitsOversizedSubsystemAndLargeFile(t *testing.T) {
	reviewer := &adaptiveReviewer{maxSourceBytes: 700}
	var progressEvents []Progress
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "docs/guide.md", Kind: discovery.KindDocumentation, Content: []byte(strings.Repeat("documented line\n", 160)),
	}}}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		OnProgress: func(value Progress) error {
			progressEvents = append(progressEvents, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.Subsystems < 2 {
		t.Fatalf("oversized file was not analyzed as smaller segments: %#v", result.Coverage)
	}
	foundOriginalOffset, foundReducedOutput, foundSplitProgress := false, false, false
	for _, request := range reviewer.requests {
		for _, file := range request.Files {
			if file.ContentStartLine > 1 {
				foundOriginalOffset = true
			}
		}
		if request.Mode == providers.RepositoryAnalysisSubsystem && request.MaxOutputTokens == 2_000 {
			foundReducedOutput = true
		}
	}
	for _, value := range progressEvents {
		if value.Stage == "adaptive-split" && strings.Contains(value.Detail, "provider limit 10000 tokens") {
			foundSplitProgress = true
		}
	}
	if !foundOriginalOffset || !foundReducedOutput || !foundSplitProgress {
		t.Fatalf("adaptive behavior missing: offset=%t output=%t progress=%t requests=%#v", foundOriginalOffset, foundReducedOutput, foundSplitProgress, reviewer.requests)
	}
}

func TestRunRecoversWhenSubsystemExhaustsOutputTokens(t *testing.T) {
	reviewer := &adaptiveReviewer{maxSourceBytes: 700, minimumOutputTokens: 4_096}
	var progressEvents []Progress
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "docs/guide.md", Kind: discovery.KindDocumentation, Content: []byte(strings.Repeat("documented line\n", 160)),
	}}}
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		OnProgress: func(value Progress) error {
			progressEvents = append(progressEvents, value)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	foundIncompleteRequest, foundRecoveredRequest, foundOutputSplit := false, false, false
	for _, request := range reviewer.requests {
		if request.Mode != providers.RepositoryAnalysisSubsystem {
			continue
		}
		if request.MaxOutputTokens == 2_000 {
			foundIncompleteRequest = true
		}
		if request.MaxOutputTokens >= 4_096 {
			foundRecoveredRequest = true
		}
	}
	for _, value := range progressEvents {
		if value.Stage == "adaptive-output-split" {
			foundOutputSplit = true
		}
	}
	if result.Coverage.Subsystems < 2 || !foundIncompleteRequest || !foundRecoveredRequest || !foundOutputSplit {
		t.Fatalf("output recovery missing: coverage=%#v incomplete=%t recovered=%t split=%t requests=%#v", result.Coverage, foundIncompleteRequest, foundRecoveredRequest, foundOutputSplit, reviewer.requests)
	}
}

func TestRepositoryRecoveryOutputTokens(t *testing.T) {
	tests := map[int]int{2_000: 4_096, 4_096: 8_192, 8_192: 8_192}
	for input, want := range tests {
		if got := repositoryRecoveryOutputTokens(input); got != want {
			t.Fatalf("repositoryRecoveryOutputTokens(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestRunWaitsAndRetriesTemporaryRateLimit(t *testing.T) {
	reviewer := &adaptiveReviewer{maxSourceBytes: 10_000, temporary429: true}
	var waits []time.Duration
	result, err := Run(context.Background(), reviewer, discovery.Repository{Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\n"),
	}}}, nil, nil, Options{
		Mode: ModeDeep, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(waits) != 1 || waits[0] != time.Minute || len(reviewer.requests) != 2 || result.Coverage.Mode != providers.RepositoryAnalysisFull {
		t.Fatalf("unexpected retry: waits=%v requests=%d result=%#v", waits, len(reviewer.requests), result)
	}
}

func TestRunRepeatsFullCooldownUntilTemporaryRateLimitClears(t *testing.T) {
	reviewer := &adaptiveReviewer{maxSourceBytes: 10_000, temporary429Count: 3}
	var waits []time.Duration
	result, err := Run(context.Background(), reviewer, discovery.Repository{Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte("package main\n"),
	}}}, nil, nil, Options{
		Mode: ModeDeep, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		Wait: func(_ context.Context, delay time.Duration) error {
			waits = append(waits, delay)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(waits) != 3 || len(reviewer.requests) != 4 || result.Coverage.Mode != providers.RepositoryAnalysisFull {
		t.Fatalf("unexpected repeated retry: waits=%v requests=%d result=%#v", waits, len(reviewer.requests), result)
	}
	for _, wait := range waits {
		if wait != time.Minute {
			t.Fatalf("wait = %s, want one minute", wait)
		}
	}
}

func TestRunRedactsRepositorySourceBeforeReviewer(t *testing.T) {
	reviewer := &recordingReviewer{}
	secret := "sk-" + "proj-" + "abcdefghijklmnopqrstuvwxyz" + "123456"
	redacted := "sk-proj-" + "****3456"
	_, err := Run(context.Background(), reviewer, discovery.Repository{Files: []discovery.File{{
		Path: "main.go", Kind: discovery.KindSource, Content: []byte(fmt.Sprintf("var key = %q\n", secret)),
	}}}, nil, nil, Options{Mode: ModeFull, Provider: providers.OpenAI, MaxInputTokens: 8_000})
	if err != nil {
		t.Fatal(err)
	}
	content := reviewer.requests[0].Files[0].Content
	if strings.Contains(content, secret) || !strings.Contains(content, redacted) {
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

func TestValidateSystemAttributionRequiresOwnedCitations(t *testing.T) {
	systems := []profile.System{{ID: "assistant"}, {ID: "ranking"}}
	rules := []ownership.Rule{
		{Paths: []string{"apps/assistant/**"}, Systems: []string{"assistant"}},
		{Paths: []string{"apps/ranking/**"}, Systems: []string{"ranking"}},
	}
	result := providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "pack/objective", SystemID: "assistant",
		SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/ranking/model.go", Line: 1}},
	}}}
	if err := validateSystemAttribution(result, systems, rules); err == nil || !strings.Contains(err.Error(), "path ownership") {
		t.Fatalf("expected cross-system attribution rejection, got %v", err)
	}
	result.ObjectiveObservations[0].SupportingEvidence[0].Path = "apps/assistant/model.go"
	if err := validateSystemAttribution(result, systems, rules); err != nil {
		t.Fatalf("owned citation should validate: %v", err)
	}
}

func TestValidateSystemAttributionLeavesMultiSystemEvidenceUnresolvedWithoutOwnership(t *testing.T) {
	result := providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "pack/objective", SystemID: "assistant",
		SupportingEvidence: []providers.RepositoryCitation{{Path: "main.go", Line: 1}},
	}}}
	err := validateSystemAttribution(result, []profile.System{{ID: "assistant"}, {ID: "ranking"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "without configured path ownership") {
		t.Fatalf("expected unresolved multi-system attribution, got %v", err)
	}
}

func TestValidateSystemAttributionAllowsNoEvidenceForSoleSystem(t *testing.T) {
	result := providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "pack/objective", SystemID: "assistant", Strength: providers.StrengthNotSupported,
	}}}
	rules := []ownership.Rule{{Paths: []string{"apps/assistant/**"}, Systems: []string{"assistant"}}}
	if err := validateSystemAttribution(result, []profile.System{{ID: "assistant"}}, rules); err != nil {
		t.Fatalf("no-evidence observation should remain attached to the sole system: %v", err)
	}
}
