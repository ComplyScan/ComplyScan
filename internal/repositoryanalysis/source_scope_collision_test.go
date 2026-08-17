package repositoryanalysis

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type sourceScopeCollisionReviewer struct {
	mu sync.Mutex

	limits         providers.RateLimitSnapshot
	requests       []providers.RepositoryAnalysisRequest
	totalUsage     providers.Usage
	activeSources  int
	maximumSources int
	sourceCalls    int

	initialSourceWave     chan struct{}
	initialSourceWaveOnce sync.Once
}

type sourceScopeCollisionSnapshot struct {
	requests       []providers.RepositoryAnalysisRequest
	totalUsage     providers.Usage
	maximumSources int
}

func (reviewer *sourceScopeCollisionReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	usage := providers.Usage{PromptTokens: 17, CompletionTokens: 4, ReasoningTokens: 1, TotalDurationNS: 9}
	isSource := request.Mode != providers.RepositoryAnalysisSynthesis

	reviewer.mu.Lock()
	reviewer.requests = append(reviewer.requests, request)
	addUsage(&reviewer.totalUsage, usage)
	if isSource {
		reviewer.sourceCalls++
		reviewer.activeSources++
		if reviewer.activeSources > reviewer.maximumSources {
			reviewer.maximumSources = reviewer.activeSources
		}
		if reviewer.sourceCalls == 3 {
			reviewer.initialSourceWaveOnce.Do(func() { close(reviewer.initialSourceWave) })
		}
	}
	reviewer.mu.Unlock()

	if isSource {
		select {
		case <-reviewer.initialSourceWave:
		case <-time.After(150 * time.Millisecond):
		}
		reviewer.mu.Lock()
		reviewer.activeSources--
		reviewer.mu.Unlock()
	}

	section := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	if isSource {
		paths := make([]string, len(request.Files))
		for index := range request.Files {
			paths[index] = request.Files[index].Path
		}
		section.UnresolvedQuestions = []string{"retained:" + strings.Join(paths, ",")}
	} else {
		for _, summary := range request.SubsystemSummaries {
			section.UnresolvedQuestions = append(section.UnresolvedQuestions, summary.UnresolvedQuestions...)
		}
	}
	return providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: section, Usage: usage, RateLimits: reviewer.limits,
		Coverage: providers.RepositoryCoverage{
			Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files),
		},
	}, nil
}

func (reviewer *sourceScopeCollisionReviewer) snapshot() sourceScopeCollisionSnapshot {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	return sourceScopeCollisionSnapshot{
		requests:   append([]providers.RepositoryAnalysisRequest(nil), reviewer.requests...),
		totalUsage: reviewer.totalUsage, maximumSources: reviewer.maximumSources,
	}
}

func TestConcurrentSourcePrefetchUsesStableIdentityWhenDisplayScopesCollide(t *testing.T) {
	padding := func(label string, lines int) []byte {
		var content strings.Builder
		fmt.Fprintf(&content, "package ai\n// %s\n", label)
		for index := 0; index < lines; index++ {
			fmt.Fprintf(&content, "// bounded evidence %s %03d\n", label, index)
		}
		return []byte(content.String())
	}
	files := []discovery.File{
		{Path: "foo/a.go", Kind: discovery.KindSource, Content: padding("foo-a", 150)},
		{Path: "foo/b.go", Kind: discovery.KindSource, Content: padding("foo-b", 150)},
		{Path: "foo (part 1)/real.go", Kind: discovery.KindSource, Content: padding("literal-scope", 20)},
	}
	for index := range files {
		files[index].Size = int64(len(files[index].Content))
	}
	repository := discovery.Repository{Root: ".", Files: files}
	preparedFiles := repositoryFiles(repository)
	chunks, err := partitionRepository(preparedFiles, 8_000*80/100)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 || chunks[0].scope != "foo (part 1)" || chunks[2].scope != "foo (part 1)" {
		t.Fatalf("collision fixture scopes = %#v, want generated and literal foo (part 1)", chunks)
	}

	limits := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
		TokensKnown: true, LimitTokens: 100_000_000, RemainingTokens: 99_000_000, ResetTokens: time.Minute,
	}
	reviewer := &sourceScopeCollisionReviewer{limits: limits, initialSourceWave: make(chan struct{})}
	result, err := runHierarchical(context.Background(), reviewer, repository, codegraph.Graph{}, preparedFiles, nil, nil, nil, 8_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true, InitialRateLimits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reviewer.snapshot()
	if snapshot.maximumSources < 2 {
		t.Fatalf("maximum concurrent source calls = %d, want collision exercised in one concurrent wave", snapshot.maximumSources)
	}

	wantPaths := []string{"foo (part 1)/real.go", "foo/a.go", "foo/b.go"}
	submitted := make(map[string]int, len(wantPaths))
	providerRequests := 0
	filesSubmitted := 0
	var bytesSubmitted int64
	for _, request := range snapshot.requests {
		providerRequests++
		filesSubmitted += len(request.Files)
		bytesSubmitted += repositorySourceContentBytes(request.Files)
		for _, file := range request.Files {
			submitted[file.Path]++
		}
	}
	for _, path := range wantPaths {
		if submitted[path] != 1 {
			t.Errorf("source path %q reached the reviewer %d time(s), want exactly once", path, submitted[path])
		}
	}

	retained := append([]string(nil), result.Result.UnresolvedQuestions...)
	sort.Strings(retained)
	wantRetained := []string{
		"retained:foo (part 1)/real.go",
		"retained:foo/a.go",
		"retained:foo/b.go",
	}
	if !reflect.DeepEqual(retained, wantRetained) {
		t.Fatalf("retained source summaries = %v, want every colliding chunk exactly once %v", retained, wantRetained)
	}
	if result.Coverage.ProviderRequests != providerRequests || result.Coverage.FilesSubmitted != filesSubmitted || result.Coverage.BytesSubmitted != bytesSubmitted {
		t.Fatalf("attempt accounting = requests/files/bytes %d/%d/%d, actual calls %d/%d/%d", result.Coverage.ProviderRequests, result.Coverage.FilesSubmitted, result.Coverage.BytesSubmitted, providerRequests, filesSubmitted, bytesSubmitted)
	}
	if !reflect.DeepEqual(result.Usage, snapshot.totalUsage) {
		t.Fatalf("usage accounting = %#v, want every provider response %#v", result.Usage, snapshot.totalUsage)
	}
	if result.Coverage.SourceBatchesStarted != len(chunks) || result.Coverage.SourceBatchesCompleted != len(chunks) || result.Coverage.SourceBatchesTotal != len(chunks) {
		t.Fatalf("distinct source batch coverage = %#v, want %d/%d/%d", result.Coverage, len(chunks), len(chunks), len(chunks))
	}
}
