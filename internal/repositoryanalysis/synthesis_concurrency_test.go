package repositoryanalysis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type capacityAwareSynthesisReviewer struct {
	mu sync.Mutex

	limits        providers.RateLimitSnapshot
	failScope     string
	semanticInput bool

	providerCalls  int
	filesSent      int
	bytesSent      int64
	totalUsage     providers.Usage
	sourceCalls    int
	activeSources  int
	maximumSources int
	sourcePaths    map[string]int

	levelOneCalls       int
	activeLevelOne      int
	maximumLevelOne     int
	partOneActive       bool
	partOneOverlapped   bool
	levelOneCompletions []string

	releasePartOne     chan struct{}
	releasePartOneOnce sync.Once
	partOneStarted     chan struct{}
	partOneStartedOnce sync.Once
}

type synthesisConcurrencySnapshot struct {
	providerCalls       int
	filesSent           int
	bytesSent           int64
	totalUsage          providers.Usage
	sourceCalls         int
	maximumSources      int
	sourcePaths         map[string]int
	levelOneCalls       int
	maximumLevelOne     int
	partOneOverlapped   bool
	levelOneCompletions []string
}

func newCapacityAwareSynthesisReviewer(limits providers.RateLimitSnapshot) *capacityAwareSynthesisReviewer {
	return &capacityAwareSynthesisReviewer{
		limits:         limits,
		sourcePaths:    make(map[string]int),
		releasePartOne: make(chan struct{}),
		partOneStarted: make(chan struct{}),
	}
}

func (reviewer *capacityAwareSynthesisReviewer) ReviewRepository(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	usage := providers.Usage{PromptTokens: 13, CompletionTokens: 3, ReasoningTokens: 1, TotalDurationNS: 7}
	level, part, isSynthesisLevel := synthesisLevelAndPart(request.Scope)
	isLevelOne := request.Mode == providers.RepositoryAnalysisSynthesis && isSynthesisLevel && level == 1
	isSource := request.Mode != providers.RepositoryAnalysisSynthesis

	waitForPartOne := false
	reviewer.mu.Lock()
	reviewer.providerCalls++
	reviewer.filesSent += len(request.Files)
	reviewer.bytesSent += repositorySourceContentBytes(request.Files)
	addUsage(&reviewer.totalUsage, usage)
	if isSource {
		reviewer.sourceCalls++
		reviewer.activeSources++
		for _, file := range request.Files {
			reviewer.sourcePaths[file.Path]++
		}
		if reviewer.activeSources > reviewer.maximumSources {
			reviewer.maximumSources = reviewer.activeSources
		}
	}
	if isLevelOne {
		reviewer.levelOneCalls++
		reviewer.activeLevelOne++
		if reviewer.activeLevelOne > reviewer.maximumLevelOne {
			reviewer.maximumLevelOne = reviewer.activeLevelOne
		}
		if part == 1 {
			reviewer.partOneActive = true
			if reviewer.activeLevelOne > 1 {
				reviewer.partOneOverlapped = true
			}
			reviewer.partOneStartedOnce.Do(func() { close(reviewer.partOneStarted) })
		} else if reviewer.partOneActive {
			reviewer.partOneOverlapped = true
			reviewer.releasePartOneOnce.Do(func() { close(reviewer.releasePartOne) })
		} else {
			waitForPartOne = true
		}
	}
	reviewer.mu.Unlock()
	if isSource {
		// Keep calls alive long enough for a concurrent wave to overlap
		// deterministically. This is also the serial Ollama regression's
		// race-safe concurrency probe.
		time.Sleep(20 * time.Millisecond)
		reviewer.mu.Lock()
		reviewer.activeSources--
		reviewer.mu.Unlock()
	}
	if waitForPartOne {
		select {
		case <-reviewer.partOneStarted:
		case <-time.After(120 * time.Millisecond):
		}
		reviewer.releasePartOneOnce.Do(func() { close(reviewer.releasePartOne) })
	}

	// Hold part one briefly so a capacity-aware wave can start another group.
	// The timeout keeps the intentionally sequential control cases bounded.
	if isLevelOne && part == 1 {
		select {
		case <-reviewer.releasePartOne:
		case <-time.After(120 * time.Millisecond):
		}
		// Ensure a later part completes first, making result-order assertions
		// independent from provider completion order.
		time.Sleep(20 * time.Millisecond)
	}

	if isLevelOne {
		reviewer.mu.Lock()
		reviewer.activeLevelOne--
		if part == 1 {
			reviewer.partOneActive = false
		}
		reviewer.levelOneCompletions = append(reviewer.levelOneCompletions, request.Scope)
		reviewer.mu.Unlock()
	}

	section := providers.RepositorySectionResult{
		Scope: request.Scope, AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}
	switch {
	case request.Mode != providers.RepositoryAnalysisSynthesis:
		// Several concise observations per source result keep this fixture large
		// enough to require independent compact-grouping requests. Bulky
		// unresolved prose is intentionally irrelevant to synthesis now.
		if len(request.Files) > 0 {
			file := request.Files[0]
			for index := 0; index < 12; index++ {
				id := fmt.Sprintf("provider-local-%d", index)
				section.AIUses = append(section.AIUses, providers.RepositoryAIUse{
					ID: id, Name: fmt.Sprintf("Bounded workflow observation %d", index+1), Purpose: strings.Repeat("Detailed bounded purpose ", 14), Lifecycle: "development", Confidence: "medium",
					Evidence: []providers.RepositoryCitation{{Path: file.Path, Line: file.ContentStartLine, Summary: strings.Repeat("Checked model invocation context ", 10)}},
				})
				section.AIUseFacts = append(section.AIUseFacts, providers.RepositoryAIUseFactSet{AIUseID: id, Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}})
			}
		}
	default:
		members := make([]string, 0)
		for _, summary := range request.SubsystemSummaries {
			for _, use := range summary.AIUses {
				members = append(members, use.MemberObservationIDs...)
			}
		}
		if len(members) > 0 {
			section.AIUses = []providers.RepositoryAIUse{{
				ID: "temporary-group", Name: "Grouped workflow", Purpose: "Connected bounded observations", Lifecycle: "development", Confidence: "medium", MemberObservationIDs: members,
			}}
		}
	}

	result := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test", Result: section, Usage: usage, RateLimits: reviewer.limits,
		Coverage: providers.RepositoryCoverage{
			Mode: request.Mode, FilesSubmitted: len(request.Files), BytesSubmitted: repositorySourceContentBytes(request.Files), CitationsChecked: len(section.AIUses),
		},
	}
	if request.Scope == reviewer.failScope {
		return result, errors.New("simulated concurrent synthesis failure")
	}
	return result, nil
}

func (reviewer *capacityAwareSynthesisReviewer) snapshot() synthesisConcurrencySnapshot {
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	sourcePaths := make(map[string]int, len(reviewer.sourcePaths))
	for path, count := range reviewer.sourcePaths {
		sourcePaths[path] = count
	}
	return synthesisConcurrencySnapshot{
		providerCalls: reviewer.providerCalls, filesSent: reviewer.filesSent, bytesSent: reviewer.bytesSent, totalUsage: reviewer.totalUsage,
		sourceCalls: reviewer.sourceCalls, maximumSources: reviewer.maximumSources, sourcePaths: sourcePaths,
		levelOneCalls: reviewer.levelOneCalls, maximumLevelOne: reviewer.maximumLevelOne, partOneOverlapped: reviewer.partOneOverlapped,
		levelOneCompletions: append([]string(nil), reviewer.levelOneCompletions...),
	}
}

func TestRunTargetedKeepsUnknownCapacityOllamaSourceAndSynthesisSerialWithExactAccounting(t *testing.T) {
	reviewer := newCapacityAwareSynthesisReviewer(providers.RateLimitSnapshot{})
	repository, expectedPaths := exhaustiveCandidateRepository(20)
	result, err := Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.Ollama, Model: "test", MaxInputTokens: 8_000,
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot := reviewer.snapshot()
	if snapshot.sourceCalls < 3 {
		t.Fatalf("fixture produced %d source batch(es), want at least three to exercise unknown-capacity slow start", snapshot.sourceCalls)
	}
	if snapshot.levelOneCalls < 3 {
		t.Fatalf("fixture produced %d first-level synthesis group(s), want at least three to exercise unknown-capacity slow start", snapshot.levelOneCalls)
	}
	if snapshot.maximumSources != 1 {
		t.Fatalf("maximum concurrent Ollama source calls = %d, want 1 without provider capacity metadata", snapshot.maximumSources)
	}
	if snapshot.maximumLevelOne != 1 {
		t.Fatalf("maximum concurrent Ollama synthesis calls = %d, want 1 without provider capacity metadata", snapshot.maximumLevelOne)
	}
	for _, path := range expectedPaths {
		if snapshot.sourcePaths[path] != 1 {
			t.Errorf("selected source path %q reached Ollama %d time(s), want exactly once", path, snapshot.sourcePaths[path])
		}
	}
	if len(snapshot.sourcePaths) != len(expectedPaths) {
		t.Fatalf("distinct source transfers = %d, want the %d selected files exactly once: %#v", len(snapshot.sourcePaths), len(expectedPaths), snapshot.sourcePaths)
	}
	if result.Coverage.ProviderRequests != snapshot.providerCalls {
		t.Fatalf("provider request accounting = %d, want every attempted call %d", result.Coverage.ProviderRequests, snapshot.providerCalls)
	}
	if result.Coverage.FilesSubmitted != snapshot.filesSent || result.Coverage.BytesSubmitted != snapshot.bytesSent {
		t.Fatalf("source transfer accounting = %d files/%d bytes, want exact reviewer totals %d/%d", result.Coverage.FilesSubmitted, result.Coverage.BytesSubmitted, snapshot.filesSent, snapshot.bytesSent)
	}
	if !reflect.DeepEqual(result.Usage, snapshot.totalUsage) {
		t.Fatalf("usage accounting = %#v, want every provider response %#v", result.Usage, snapshot.totalUsage)
	}
	if result.Coverage.SourceBatchesStarted != snapshot.sourceCalls || result.Coverage.SourceBatchesCompleted != snapshot.sourceCalls || result.Coverage.SourceBatchesTotal != snapshot.sourceCalls {
		t.Fatalf("source batch coverage = %#v, want %d/%d/%d", result.Coverage, snapshot.sourceCalls, snapshot.sourceCalls, snapshot.sourceCalls)
	}
}

func synthesisLevelAndPart(scope string) (int, int, bool) {
	const prefix = "synthesis-level-"
	if !strings.HasPrefix(scope, prefix) {
		return 0, 0, false
	}
	remainder := strings.TrimPrefix(scope, prefix)
	pieces := strings.Split(remainder, "-part-")
	if len(pieces) != 2 {
		return 0, 0, false
	}
	level, levelErr := strconv.Atoi(pieces[0])
	part, partErr := strconv.Atoi(pieces[1])
	return level, part, levelErr == nil && partErr == nil
}

func completeSynthesisCapacity() providers.RateLimitSnapshot {
	return providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
		TokensKnown: true, LimitTokens: 50_000_000, RemainingTokens: 49_000_000, ResetTokens: time.Minute,
	}
}

func runSynthesisConcurrencyFixture(t *testing.T, reviewer Reviewer, limits providers.RateLimitSnapshot) (providers.RepositoryAnalysisResult, error) {
	t.Helper()
	repository, _ := exhaustiveCandidateRepository(20)
	return Run(context.Background(), reviewer, repository, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", MaxInputTokens: 8_000,
		InitialRateLimits: limits,
	})
}

func TestRunTargetedUsesCompleteProviderCapacityForConcurrentSynthesisGroupsInDeterministicOrder(t *testing.T) {
	limits := completeSynthesisCapacity()
	reviewer := newCapacityAwareSynthesisReviewer(limits)
	result, err := runSynthesisConcurrencyFixture(t, reviewer, limits)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reviewer.snapshot()
	if snapshot.levelOneCalls < 2 {
		t.Fatalf("fixture produced %d first-level synthesis group(s), want at least two", snapshot.levelOneCalls)
	}
	if snapshot.maximumLevelOne < 2 || !snapshot.partOneOverlapped {
		t.Fatalf("maximum concurrent first-level synthesis calls = %d, part one overlapped = %t; complete RPM+TPM capacity should start independent groups together", snapshot.maximumLevelOne, snapshot.partOneOverlapped)
	}
	if len(snapshot.levelOneCompletions) < 2 || snapshot.levelOneCompletions[0] == "synthesis-level-1-part-1" {
		t.Fatalf("test did not force out-of-order synthesis completion: %v", snapshot.levelOneCompletions)
	}
	if len(result.Result.AIUses) != 1 || len(result.Result.AIUses[0].MemberObservationIDs) != 12*snapshot.sourceCalls {
		t.Fatalf("final compact grouping did not retain deterministic observation membership: %#v", result.Result.AIUses)
	}
}

func TestRunTargetedKeepsPartialAndUnknownSynthesisCapacityConservative(t *testing.T) {
	tests := []struct {
		name           string
		limits         providers.RateLimitSnapshot
		wantMaximum    int
		allowSlowStart bool
	}{
		{
			name:        "request-only capacity stays sequential",
			limits:      providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute},
			wantMaximum: 1,
		},
		{
			name:        "token-only capacity stays sequential",
			limits:      providers.RateLimitSnapshot{TokensKnown: true, LimitTokens: 50_000_000, RemainingTokens: 49_000_000, ResetTokens: time.Minute},
			wantMaximum: 1,
		},
		{
			name:        "unknown capacity begins with one-group slow start",
			wantMaximum: 2, allowSlowStart: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reviewer := newCapacityAwareSynthesisReviewer(test.limits)
			_, err := runSynthesisConcurrencyFixture(t, reviewer, test.limits)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := reviewer.snapshot()
			if snapshot.levelOneCalls < 2 {
				t.Fatalf("fixture produced %d first-level synthesis group(s), want at least two", snapshot.levelOneCalls)
			}
			if snapshot.partOneOverlapped {
				t.Fatalf("first synthesis group overlapped under incomplete capacity evidence; completions=%v", snapshot.levelOneCompletions)
			}
			if snapshot.maximumLevelOne > test.wantMaximum {
				t.Fatalf("maximum synthesis concurrency = %d, conservative ceiling = %d", snapshot.maximumLevelOne, test.wantMaximum)
			}
			if !test.allowSlowStart && snapshot.maximumLevelOne != 1 {
				t.Fatalf("partial provider capacity ran %d synthesis groups together, want sequential execution", snapshot.maximumLevelOne)
			}
		})
	}
}

func TestRunTargetedConcurrentSynthesisFailureRetainsEveryAttemptButNoUnsynthesizedSemantics(t *testing.T) {
	limits := completeSynthesisCapacity()
	reviewer := newCapacityAwareSynthesisReviewer(limits)
	reviewer.failScope = "synthesis-level-1-part-1"
	reviewer.semanticInput = true

	result, err := runSynthesisConcurrencyFixture(t, reviewer, limits)
	if err == nil || !strings.Contains(err.Error(), "simulated concurrent synthesis failure") {
		t.Fatalf("concurrent synthesis error = %v", err)
	}
	snapshot := reviewer.snapshot()
	if snapshot.levelOneCalls < 2 || snapshot.maximumLevelOne < 2 {
		t.Fatalf("first-level synthesis attempts/concurrency = %d/%d, want a failed concurrent wave with multiple attempted groups", snapshot.levelOneCalls, snapshot.maximumLevelOne)
	}
	if result.Coverage.ProviderRequests != snapshot.providerCalls {
		t.Fatalf("provider request accounting = %d, want every attempted call %d", result.Coverage.ProviderRequests, snapshot.providerCalls)
	}
	if result.Coverage.FilesSubmitted != snapshot.filesSent || result.Coverage.BytesSubmitted != snapshot.bytesSent {
		t.Fatalf("attempted source transfer accounting = %d files/%d bytes, want %d/%d", result.Coverage.FilesSubmitted, result.Coverage.BytesSubmitted, snapshot.filesSent, snapshot.bytesSent)
	}
	if !reflect.DeepEqual(result.Usage, snapshot.totalUsage) {
		t.Fatalf("attempted usage = %#v, want every concurrent response %#v", result.Usage, snapshot.totalUsage)
	}
	if len(result.Result.AIUses) != 0 || len(result.Result.AIUseFacts) != 0 || len(result.Result.ObjectiveObservations) != 0 || len(result.Result.UnmappedObservations) != 0 {
		t.Fatalf("partial failure leaked unsynthesized semantics: %#v", result.Result)
	}
	if len(result.Result.UnresolvedQuestions) == 0 || !strings.Contains(result.Result.UnresolvedQuestions[0], "synthesis") {
		t.Fatalf("partial failure did not expose incomplete synthesis status: %#v", result.Result.UnresolvedQuestions)
	}
}
