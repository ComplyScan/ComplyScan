package repositoryanalysis

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type capacityPreflightReviewer struct {
	mu sync.Mutex

	limit              int
	semantic           bool
	waited             bool
	sourceBeforeWait   bool
	requests           []providers.RepositoryAnalysisRequest
	waits              []time.Duration
	usage              providers.Usage
	providerCalls      int
	filesSubmitted     int
	bytesSubmitted     int64
	citationsSubmitted int
}

func (reviewer *capacityPreflightReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()

	if reviewer.limit > 0 && estimatedRepositoryRequestTokens(request) > reviewer.limit {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("provider received an estimated %d-token request above the known %d-token window", estimatedRepositoryRequestTokens(request), reviewer.limit)
	}
	if len(request.Files) > 0 && !reviewer.waited {
		reviewer.sourceBeforeWait = true
	}
	reviewer.requests = append(reviewer.requests, request)
	reviewer.providerCalls++
	reviewer.filesSubmitted += len(request.Files)
	reviewer.bytesSubmitted += repositorySourceContentBytes(request.Files)

	section := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if reviewer.semantic {
		if request.Mode == providers.RepositoryAnalysisSynthesis {
			for _, summary := range request.SubsystemSummaries {
				section.AIUses = append(section.AIUses, summary.AIUses...)
				section.AIUseFacts = append(section.AIUseFacts, summary.AIUseFacts...)
				section.ObjectiveObservations = append(section.ObjectiveObservations, summary.ObjectiveObservations...)
				section.UnmappedObservations = append(section.UnmappedObservations, summary.UnmappedObservations...)
				section.UnresolvedQuestions = append(section.UnresolvedQuestions, summary.UnresolvedQuestions...)
			}
		} else if len(request.Files) > 0 {
			file := request.Files[0]
			section.AIUses = append(section.AIUses, providers.RepositoryAIUse{
				ID: "batch-local-use", Name: "Bounded AI call", Purpose: "Exercise capacity preflight", Lifecycle: "development", Confidence: "medium",
				Evidence: []providers.RepositoryCitation{{Path: file.Path, Line: file.ContentStartLine, Summary: "Submitted model invocation"}},
			})
		}
	}
	citations := 0
	for _, use := range section.AIUses {
		citations += len(use.Evidence)
	}
	reviewer.citationsSubmitted += citations
	usage := providers.Usage{PromptTokens: 11, CompletionTokens: 3, ReasoningTokens: 1, TotalDurationNS: 5}
	addUsage(&reviewer.usage, usage)
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: section, Usage: usage,
		Coverage: providers.RepositoryCoverage{
			Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files), CitationsChecked: citations,
		},
	}, nil
}

func (reviewer *capacityPreflightReviewer) wait(_ context.Context, delay time.Duration) error {
	reviewer.mu.Lock()
	reviewer.waited = true
	reviewer.waits = append(reviewer.waits, delay)
	reviewer.mu.Unlock()
	return nil
}

type capacityPreflightSnapshot struct {
	requests           []providers.RepositoryAnalysisRequest
	waits              []time.Duration
	sourceBeforeWait   bool
	usage              providers.Usage
	providerCalls      int
	filesSubmitted     int
	bytesSubmitted     int64
	citationsSubmitted int
}

func (reviewer *capacityPreflightReviewer) snapshot() capacityPreflightSnapshot {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return capacityPreflightSnapshot{
		requests: append([]providers.RepositoryAnalysisRequest(nil), reviewer.requests...), waits: append([]time.Duration(nil), reviewer.waits...),
		sourceBeforeWait: reviewer.sourceBeforeWait, usage: reviewer.usage, providerCalls: reviewer.providerCalls,
		filesSubmitted: reviewer.filesSubmitted, bytesSubmitted: reviewer.bytesSubmitted, citationsSubmitted: reviewer.citationsSubmitted,
	}
}

func TestExhaustedTargetedCompactCapacityWaitsThenUsesCheapestSingleRequest(t *testing.T) {
	repository := singleTargetedRepository()
	reviewer := &capacityPreflightReviewer{semantic: true}
	reset := 2 * time.Minute
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 1, RemainingRequests: 0, ResetRequests: reset,
			TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 1_000_000, ResetTokens: reset,
		},
		Wait: reviewer.wait,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reviewer.snapshot()
	if snapshot.sourceBeforeWait {
		t.Fatal("repository source reached the provider before the exhausted RPM window reset")
	}
	if !reflect.DeepEqual(snapshot.waits, []time.Duration{reset}) {
		t.Fatalf("capacity waits = %v, want one %s wait before source", snapshot.waits, reset)
	}
	if len(snapshot.requests) != 1 || len(snapshot.requests[0].Files) == 0 || snapshot.requests[0].Scope != "." || !snapshot.requests[0].AllowFollowUp || snapshot.requests[0].Mode != providers.RepositoryAnalysisTargeted {
		t.Fatalf("exhausted capacity did not preserve the cheapest compact request after waiting: %#v", snapshot.requests)
	}
	if result.Coverage.SourceBatchesStarted != 0 || result.Coverage.SourceBatchesCompleted != 0 || result.Coverage.SourceBatchesTotal != 0 {
		t.Fatalf("compact source coverage = %#v, want no hierarchical batches", result.Coverage)
	}
	assertCapacityPreflightAccounting(t, result, snapshot)
	if len(result.Result.AIUses) != 1 || len(result.Result.AIUses[0].Evidence) != 1 || result.Result.AIUses[0].Evidence[0].Path != "app.py" {
		t.Fatalf("validated compact source citation was not retained: %#v", result.Result)
	}
}

func TestBroadModesDoNotTransferAnUnadmittedFullRepositoryPackage(t *testing.T) {
	repository := singleTargetedRepository()
	files := repositoryFiles(repository)
	graph := codegraph.Build(repository)
	fullRequest := providers.RepositoryAnalysisRequest{
		Mode: providers.RepositoryAnalysisFull, Scope: ".", RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
		MaxOutputTokens: repositoryOutputTokens(providers.OpenAI, 0), Files: files, Graph: repositoryGraphContext(graph, files),
	}
	fullEnvelope := estimatedRepositoryRequestTokens(fullRequest) - fullRequest.MaxOutputTokens
	tokenLimit := fullEnvelope + 2*minimumAdaptiveOutput
	if tokenLimit >= estimatedRepositoryRequestTokens(fullRequest) {
		t.Fatalf("fixture token limit %d does not reject the %d-token full package", tokenLimit, estimatedRepositoryRequestTokens(fullRequest))
	}

	tests := []struct {
		name   string
		limits providers.RateLimitSnapshot
	}{
		{
			name: "exhausted RPM",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 2, RemainingRequests: 0, ResetRequests: time.Minute,
				TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 1_000_000, ResetTokens: time.Minute,
			},
		},
		{
			name: "full package exceeds known token window",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 10, RemainingRequests: 10, ResetRequests: time.Minute,
				TokensKnown: true, LimitTokens: tokenLimit, RemainingTokens: tokenLimit, ResetTokens: time.Minute,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fullReviewer := &capacityPreflightReviewer{semantic: true}
			fullResult, err := Run(context.Background(), fullReviewer, repository, nil, nil, Options{
				Mode: ModeFull, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000, InitialRateLimits: test.limits,
				Wait: fullReviewer.wait,
			})
			fullSnapshot := fullReviewer.snapshot()
			if test.limits.RemainingRequests == 0 {
				if err != nil {
					t.Fatalf("explicit full mode did not resume after a healthy capacity reset: %v", err)
				}
				if len(fullSnapshot.requests) != 1 || fullSnapshot.requests[0].Mode != providers.RepositoryAnalysisFull || len(fullSnapshot.waits) != 1 || fullSnapshot.sourceBeforeWait {
					t.Fatalf("explicit full mode did not wait and then make exactly one admitted broad request: %#v", fullSnapshot)
				}
				assertCapacityPreflightAccounting(t, fullResult, fullSnapshot)
			} else {
				if err == nil || !strings.Contains(err.Error(), "cannot admit the full repository request") {
					t.Fatalf("explicit full mode error = %v, want a no-transfer intrinsic-capacity error", err)
				}
				if len(fullSnapshot.requests) != 0 || fullSnapshot.filesSubmitted != 0 || fullSnapshot.bytesSubmitted != 0 {
					t.Fatalf("explicit full mode leaked source after failed preflight: %#v", fullSnapshot)
				}
			}

			deepReviewer := &capacityPreflightReviewer{limit: test.limits.LimitTokens, semantic: true}
			result, err := Run(context.Background(), deepReviewer, repository, nil, nil, Options{
				Mode: ModeDeep, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000, InitialRateLimits: test.limits,
				Wait: deepReviewer.wait,
			})
			if err != nil {
				t.Fatal(err)
			}
			snapshot := deepReviewer.snapshot()
			if test.limits.RemainingRequests == 0 {
				if len(snapshot.requests) != 1 || snapshot.requests[0].Mode != providers.RepositoryAnalysisFull || len(snapshot.waits) != 1 || snapshot.sourceBeforeWait {
					t.Fatalf("broad auto mode did not wait and then send one admitted full request: %#v", snapshot)
				}
			} else {
				if len(snapshot.requests) == 0 {
					t.Fatal("broad auto mode did not continue with a bounded source batch")
				}
				for _, request := range snapshot.requests {
					if request.Mode == providers.RepositoryAnalysisFull {
						t.Fatalf("broad auto mode transferred the intrinsically unadmitted full package: %#v", request)
					}
				}
			}
			assertCapacityPreflightAccounting(t, result, snapshot)
		})
	}
}

func TestKnownTokenWindowShrinksSourceAndSynthesisBeforeProviderCalls(t *testing.T) {
	repository := discovery.Repository{Root: ".", Files: []discovery.File{
		oversizedInvocationFile("alpha/use.py", 8),
		oversizedInvocationFile("beta/use.py", 8),
	}}
	files := repositoryFiles(repository)
	graph := codegraph.Build(repository)
	baseOptions := Options{Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000}
	budget := sourceBudget(baseOptions.MaxInputTokens, nil, nil, nil)
	chunks, err := partitionRepository(files, budget*80/100)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("fixture source chunks = %d, want two independent subsystem batches", len(chunks))
	}
	prepared := prepareRepositorySourceBatch(chunks[0], repository, graph, nil, nil, nil, budget, 0, baseOptions)
	inputAndEnvelope := prepared.estimatedTokens - prepared.maxOutputTokens
	tokenLimit := inputAndEnvelope + 2*minimumAdaptiveOutput
	if tokenLimit >= prepared.estimatedTokens {
		t.Fatalf("fixture token limit %d does not require reducing the %d-token source request", tokenLimit, prepared.estimatedTokens)
	}

	reviewer := &capacityPreflightReviewer{limit: tokenLimit, semantic: true}
	var sourcePreflights, synthesisPreflights int
	result, err := runHierarchical(context.Background(), reviewer, repository, graph, files, nil, nil, nil, budget, Options{
		Mode: ModeHierarchical, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 20, RemainingRequests: 20, ResetRequests: time.Minute,
			TokensKnown: true, LimitTokens: tokenLimit, RemainingTokens: tokenLimit, ResetTokens: time.Minute,
		},
		Wait: reviewer.wait,
		OnProgress: func(value Progress) error {
			if value.Stage != "capacity-preflight-output" && value.Stage != "capacity-preflight-split" {
				return nil
			}
			if strings.HasPrefix(value.Scope, "synthesis-") || value.Scope == "repository-synthesis" {
				synthesisPreflights++
			} else {
				sourcePreflights++
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reviewer.snapshot()
	if sourcePreflights == 0 {
		t.Fatal("known token window did not reduce or split source locally before transfer")
	}
	if synthesisPreflights == 0 {
		t.Fatal("known token window did not reduce or split synthesis locally before transfer")
	}
	for _, request := range snapshot.requests {
		if estimated := estimatedRepositoryRequestTokens(request); estimated > tokenLimit {
			t.Errorf("provider request %q estimated at %d tokens exceeds preflight window %d", request.Scope, estimated, tokenLimit)
		}
	}
	if t.Failed() {
		t.Fatalf("preflight request sequence: %#v", snapshot.requests)
	}
	assertCapacityPreflightAccounting(t, result, snapshot)
	for _, use := range result.Result.AIUses {
		for _, citation := range use.Evidence {
			if citation.Path != "alpha/use.py" && citation.Path != "beta/use.py" {
				t.Fatalf("synthesis introduced a citation outside transferred source: %#v", citation)
			}
		}
	}
}

func TestHealthyLowRPMWindowsMayResetForMoreThanTenMinutesCumulatively(t *testing.T) {
	repository, expectedPaths := exhaustiveCandidateRepository(90)
	reviewer := &capacityPreflightReviewer{}
	reset := time.Minute
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: providers.RateLimitSnapshot{
			RequestsKnown: true, LimitRequests: 1, RemainingRequests: 1, ResetRequests: reset,
			TokensKnown: true, LimitTokens: 1_000_000_000, RemainingTokens: 1_000_000_000, ResetTokens: reset,
		},
		Wait: reviewer.wait,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reviewer.snapshot()
	var cumulative time.Duration
	for _, wait := range snapshot.waits {
		if wait > maxRateLimitTotalWait {
			t.Fatalf("one healthy reset wait = %s, exceeds per-window safety bound %s", wait, maxRateLimitTotalWait)
		}
		cumulative += wait
	}
	if cumulative <= maxRateLimitTotalWait {
		t.Fatalf("fixture cumulative reset wait = %s across %d window(s), want more than %s", cumulative, len(snapshot.waits), maxRateLimitTotalWait)
	}
	pathCounts := make(map[string]int, len(expectedPaths))
	for _, request := range snapshot.requests {
		for _, file := range request.Files {
			pathCounts[file.Path]++
		}
	}
	for _, path := range expectedPaths {
		if pathCounts[path] != 1 {
			t.Errorf("selected path %q reached the provider %d time(s), want exactly once", path, pathCounts[path])
		}
	}
	if len(pathCounts) != len(expectedPaths) {
		t.Fatalf("source transfer leaked outside the selected candidate set: %#v", pathCounts)
	}
	if result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal {
		t.Fatalf("low-RPM run did not finish every bounded source batch: %#v", result.Coverage)
	}
	assertCapacityPreflightAccounting(t, result, snapshot)
}

func assertCapacityPreflightAccounting(t *testing.T, result providers.RepositoryAnalysisResult, snapshot capacityPreflightSnapshot) {
	t.Helper()
	if result.Coverage.ProviderRequests != snapshot.providerCalls || result.Coverage.FilesSubmitted != snapshot.filesSubmitted || result.Coverage.BytesSubmitted != snapshot.bytesSubmitted || result.Coverage.CitationsChecked != snapshot.citationsSubmitted {
		t.Fatalf("result accounting = %#v, want exact provider totals requests/files/bytes/citations %d/%d/%d/%d", result.Coverage, snapshot.providerCalls, snapshot.filesSubmitted, snapshot.bytesSubmitted, snapshot.citationsSubmitted)
	}
	if !reflect.DeepEqual(result.Usage, snapshot.usage) {
		t.Fatalf("result usage = %#v, want every provider response %#v", result.Usage, snapshot.usage)
	}
}
