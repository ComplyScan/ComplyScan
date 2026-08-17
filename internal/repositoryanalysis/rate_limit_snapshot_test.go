package repositoryanalysis

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type noCallRepositoryReviewer struct{}

func (noCallRepositoryReviewer) ReviewRepository(context.Context, providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	panic("repository reviewer should not be called")
}

func TestMissingResetTimeAdmitsOneCalibrationRequestWithoutAssumingFullWindow(t *testing.T) {
	got := replenishRateLimitSnapshot(providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 0,
		TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 0,
	}, time.Second)
	if got.RemainingRequests != 1 || got.RemainingTokens != got.LimitTokens {
		t.Fatalf("replenished snapshot = %#v, want one cautious request with one request's token capacity", got)
	}
	limit, _ := sourceBatchWaveLimit(got, 10_000, 20, 20)
	if limit != 1 {
		t.Fatalf("wave limit = %d, want one calibration request", limit)
	}
}

func TestMissingTokenResetWithPositiveInsufficientBalanceAdmitsCalibrationRequest(t *testing.T) {
	limits := providers.RateLimitSnapshot{
		TokensKnown: true, LimitTokens: 10_000, RemainingTokens: 100,
	}
	limit, wait := sourceBatchWaveLimit(limits, 1_000, 5, 5)
	if limit != 0 || wait != 0 {
		t.Fatalf("initial wave = (%d, %s), want conservative caller cooldown", limit, wait)
	}
	replenished := replenishRateLimitSnapshot(limits, time.Second)
	limit, _ = sourceBatchWaveLimit(replenished, 1_000, 5, 5)
	if limit != 1 || replenished.RemainingTokens != replenished.LimitTokens {
		t.Fatalf("calibration snapshot=%#v wave=%d, want one request", replenished, limit)
	}
}

func TestKnownResetTimeCanRestoreObservedWindow(t *testing.T) {
	got := replenishRateLimitSnapshot(providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 12, RemainingRequests: 0, ResetRequests: 2 * time.Second,
		TokensKnown: true, LimitTokens: 100_000, RemainingTokens: 0, ResetTokens: 2 * time.Second,
	}, 2*time.Second)
	if got.RemainingRequests != 12 || got.RemainingTokens != 100_000 {
		t.Fatalf("replenished snapshot = %#v, want observed full window", got)
	}
}

func TestKnownTokenResetRestoresPositiveButInsufficientBalance(t *testing.T) {
	limits := providers.RateLimitSnapshot{
		TokensKnown: true, LimitTokens: 100_000, RemainingTokens: 3_000, ResetTokens: 2 * time.Second,
	}
	limit, wait := sourceBatchWaveLimit(limits, 10_000, 4, 4)
	if limit != 0 || wait != 2*time.Second {
		t.Fatalf("pre-reset wave = (%d, %s), want token-capacity wait", limit, wait)
	}
	got := replenishRateLimitSnapshot(limits, wait)
	if got.RemainingTokens != got.LimitTokens {
		t.Fatalf("replenished snapshot = %#v, want full observed token window", got)
	}
}

func TestLatestRateLimitSnapshotPreservesMissingDimension(t *testing.T) {
	current := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499, ResetRequests: time.Minute,
		TokensKnown: true, LimitTokens: 1_000_000, RemainingTokens: 990_000, ResetTokens: time.Minute,
	}
	next := providers.RateLimitSnapshot{
		RequestsKnown: true, LimitRequests: 400, RemainingRequests: 398, ResetRequests: 30 * time.Second,
	}
	got := latestRateLimitSnapshot(current, next)
	if got.LimitRequests != 400 || got.RemainingRequests != 398 || got.LimitTokens != 1_000_000 || got.RemainingTokens != 990_000 {
		t.Fatalf("latest partial snapshot = %#v, want new requests with prior tokens preserved", got)
	}
}

func TestPartialCapacityStillWaitsForKnownExhaustedDimension(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		limits providers.RateLimitSnapshot
		wait   time.Duration
	}{
		{
			name: "requests only",
			limits: providers.RateLimitSnapshot{
				RequestsKnown: true, LimitRequests: 500, RemainingRequests: 0, ResetRequests: 45 * time.Second,
			},
			wait: 45 * time.Second,
		},
		{
			name: "tokens only",
			limits: providers.RateLimitSnapshot{
				TokensKnown: true, LimitTokens: 500_000, RemainingTokens: 0, ResetTokens: 30 * time.Second,
			},
			wait: 30 * time.Second,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			limit, wait := sourceBatchWaveLimit(testCase.limits, 10_000, 20, 20)
			if limit != 0 || wait != testCase.wait {
				t.Fatalf("wave = (%d, %s), want (0, %s)", limit, wait, testCase.wait)
			}
		})
	}
}

func TestRunAccountsForReusedLiveCompatibilityRequest(t *testing.T) {
	usage := providers.Usage{PromptTokens: 100, CompletionTokens: 20, ReasoningTokens: 3}
	result, err := Run(context.Background(), noCallRepositoryReviewer{}, discovery.Repository{}, nil, nil, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test",
		InitialUsage: usage, InitialProviderRequests: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.ProviderRequests != 1 || result.Usage != usage {
		t.Fatalf("initial capacity accounting = requests %d usage %#v, want one request and %#v", result.Coverage.ProviderRequests, result.Usage, usage)
	}
	if result.Coverage.FilesSubmitted != 0 {
		t.Fatalf("source submissions = %d, want zero for source-free compatibility request", result.Coverage.FilesSubmitted)
	}
}

func TestCapacityProbeRetriesParticipateInHardRequestCeiling(t *testing.T) {
	repository := discovery.Repository{Root: "."}
	for index := 0; index < 12; index++ {
		content := []byte("package ai\nfunc invoke() { model() }\n" + strings.Repeat("// bounded source context\n", 120))
		repository.Files = append(repository.Files, discovery.File{
			Path: fmt.Sprintf("scope%02d/use.go", index), Kind: discovery.KindSource, Content: content, Size: int64(len(content)),
		})
	}
	files := repositoryFiles(repository)
	usage := providers.Usage{PromptTokens: 40, CompletionTokens: 8}
	result, err := runHierarchical(context.Background(), noCallRepositoryReviewer{}, repository, codegraph.Build(repository), files, nil, nil, nil, 8_000, Options{
		Mode: ModeTargeted, Provider: providers.OpenAI, Model: "test", TargetedBatches: true,
		requestBudget: &providerRequestBudget{limit: maxCapacityProbeProviderRequests}, retryGate: make(chan struct{}, 1),
		ProbeRateLimits: func(context.Context) (CapacityProbeResult, error) {
			return CapacityProbeResult{Usage: usage, ProviderRequests: maxCapacityProbeProviderRequests}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "safety ceiling") {
		t.Fatalf("probe ceiling error = %v", err)
	}
	if result.Coverage.ProviderRequests != maxCapacityProbeProviderRequests || result.Usage != usage || result.Coverage.FilesSubmitted != 0 {
		t.Fatalf("probe-only ceiling accounting = %#v, want %d source-free requests and usage %#v", result, maxCapacityProbeProviderRequests, usage)
	}
}
