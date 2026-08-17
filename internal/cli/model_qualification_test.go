package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/modelqualification"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type qualificationReviewFunc func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error)

func (function qualificationReviewFunc) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return function(ctx, request)
}

type qualificationRepositoryReviewFunc func(context.Context, providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error)

type qualificationReviewerStub struct {
	review     qualificationReviewFunc
	repository qualificationRepositoryReviewFunc
}

func (reviewer qualificationReviewerStub) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return reviewer.review(ctx, request)
}

func (reviewer qualificationReviewerStub) ReviewRepository(ctx context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
	return reviewer.repository(ctx, request)
}

func TestFinishSetupModelQualificationUsesAutomaticCompatibleResult(t *testing.T) {
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		return modelQualificationOutcome{Result: modelqualification.Result{
			Status: "compatible", FromCache: true, ExpiresAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		}}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "ollama"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, true)
	if err != nil || !ready {
		t.Fatalf("ready=%t err=%v output=%s", ready, err, output.String())
	}
	for _, expected := range []string{"bounded synthetic finding and repository compatibility requests", "at most 4 provider requests", "Model status: compatible", "cached check", "not model accuracy or legal correctness"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestFinishSetupModelQualificationFallsBackDeterministically(t *testing.T) {
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		return modelQualificationOutcome{Result: modelqualification.Result{
			ProviderRequests: 2,
			Usage:            providers.Usage{PromptTokens: 9, CompletionTokens: 4, ReasoningTokens: 1},
		}}, errors.New("schema unsupported")
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "ollama"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, true)
	if err != nil || ready {
		t.Fatalf("ready=%t err=%v output=%s", ready, err, output.String())
	}
	if !strings.Contains(output.String(), "Deterministic setup will continue") || !strings.Contains(output.String(), "2 provider request(s), 9 input / 4 output / 1 reasoning token(s)") {
		t.Fatalf("fallback output:\n%s", output.String())
	}
}

func TestFinishSetupModelQualificationSkipsImplicitAutomationRequest(t *testing.T) {
	previous := qualifyConfiguredModel
	called := false
	qualifyConfiguredModel = func(context.Context, config.AIConfig, bool) (modelQualificationOutcome, error) {
		called = true
		return modelQualificationOutcome{}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	settings := config.Default().AI
	settings.Provider = "openai"
	settings.Remote.Model = "test"
	var output bytes.Buffer
	ready, err := finishSetupModelQualification(context.Background(), &output, settings, false)
	if err != nil || !ready || called {
		t.Fatalf("ready=%t called=%t err=%v", ready, called, err)
	}
	if !strings.Contains(output.String(), "--qualify-model") {
		t.Fatalf("skip output:\n%s", output.String())
	}
}

func TestConfiguredQualificationIdentityIncludesOllamaDigest(t *testing.T) {
	useDoctorHTTPTransport(t, func(*http.Request) (*http.Response, error) {
		return doctorHTTPResponse(200, `{"models":[{"name":"qwen3.5:9b","model":"qwen3.5:9b","digest":"sha256:abc"}]}`), nil
	})
	settings := config.Default().AI
	settings.Provider = "ollama"
	identity, err := configuredQualificationIdentity(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != providers.Ollama || identity.ModelDigest != "sha256:abc" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestConfiguredQualificationIdentitySeparatesCompatibleEndpoints(t *testing.T) {
	settings := config.Default().AI
	settings.Provider = "openai-compatible"
	settings.Remote = config.RemoteConfig{
		ProviderName: "Acme", BaseURL: "https://models.example.com/v1/", Model: "review-v2",
		APIKeyEnv: "ACME_API_KEY", TimeoutSeconds: 60, MaxFindings: 10,
	}
	identity, err := configuredQualificationIdentity(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != providers.Compatible || !strings.HasPrefix(identity.ModelDigest, "endpoint-sha256:") {
		t.Fatalf("identity = %#v", identity)
	}
	settings.Remote.BaseURL = "https://other.example.com/v1"
	other, err := configuredQualificationIdentity(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ModelDigest == other.ModelDigest {
		t.Fatalf("endpoint identity was reused: %#v %#v", identity, other)
	}
}

func TestRunModelQualificationRetriesAndCachesExactLiveAccounting(t *testing.T) {
	settings := config.Default().AI
	settings.Provider = "openai-compatible"
	settings.Remote = config.RemoteConfig{
		ProviderName: "Acme", BaseURL: "https://models.example.test/v1", Model: "test-model",
		APIKeyEnv: "COMPLYSCAN_QUALIFICATION_RETRY_KEY", TimeoutSeconds: 5, MaxFindings: 1,
	}
	findingCalls, repositoryCalls := 0, 0
	previousReviewer := modelQualificationReviewer
	modelQualificationReviewer = func(config.AIConfig) (modelqualification.Reviewer, time.Duration, error) {
		return qualificationReviewerStub{
			review: func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
				findingCalls++
				finding := request.Findings[0]
				result := providers.ReviewResult{
					Provider: providers.Compatible, Model: "test-model", InputFindings: 1,
					Usage: providers.Usage{PromptTokens: 10 + findingCalls, CompletionTokens: findingCalls},
				}
				if findingCalls == 1 {
					return result, &providers.RemoteTransientError{Provider: "Acme", StatusCode: http.StatusServiceUnavailable}
				}
				result.Reviewed = 1
				result.Observations = []providers.Observation{{Fingerprint: finding.Fingerprint, RuleID: finding.RuleID}}
				result.RateLimits = providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 498}
				return result, nil
			},
			repository: func(_ context.Context, request providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error) {
				repositoryCalls++
				return providers.RepositoryAnalysisResult{
					Provider: providers.Compatible, Model: "test-model",
					Coverage: providers.RepositoryCoverage{Mode: request.Mode, FilesSubmitted: len(request.Files)},
					Result: providers.RepositorySectionResult{
						AIUses: []providers.RepositoryAIUse{},
						AIUseFacts: []providers.RepositoryAIUseFactSet{{
							AIUseID: request.ConfirmedAIUses[0].ID, Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{"Runtime operation is intentionally unknown."},
						}},
						ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
						UnmappedObservations:  []providers.RepositoryUnmappedObservation{},
						UnresolvedQuestions:   []string{"Synthetic evidence cannot establish production use."},
					},
					Usage:      providers.Usage{PromptTokens: 20, CompletionTokens: 4, ReasoningTokens: 2},
					RateLimits: providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 497},
				}, nil
			},
		}, 5 * time.Second, nil
	}
	t.Cleanup(func() { modelQualificationReviewer = previousReviewer })
	previousPath := modelQualificationDefaultPath
	qualificationPath := filepath.Join(t.TempDir(), "qualification.json")
	modelQualificationDefaultPath = func() (string, error) {
		return qualificationPath, nil
	}
	t.Cleanup(func() { modelQualificationDefaultPath = previousPath })

	outcome, err := runModelQualification(context.Background(), settings, true)
	if err != nil {
		t.Fatal(err)
	}
	if findingCalls != 2 || repositoryCalls != 1 || outcome.Result.ProviderRequests != 3 || outcome.Result.Usage.PromptTokens != 43 || outcome.Result.Usage.CompletionTokens != 7 || outcome.Result.Usage.ReasoningTokens != 2 || outcome.Result.RateLimits.RemainingRequests != 497 {
		t.Fatalf("live outcome=%#v finding calls=%d repository calls=%d, want three exact attempts and cumulative accounting", outcome.Result, findingCalls, repositoryCalls)
	}
	cached, err := runModelQualification(context.Background(), settings, false)
	if err != nil {
		t.Fatal(err)
	}
	if !cached.Result.FromCache || cached.Result.ProviderRequests != 0 || findingCalls != 2 || repositoryCalls != 1 {
		t.Fatalf("cached outcome=%#v finding calls=%d repository calls=%d, want no current-run provider request", cached.Result, findingCalls, repositoryCalls)
	}
}

func TestDoctorProbeRefreshesAutomaticQualification(t *testing.T) {
	target := t.TempDir()
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote = config.RemoteConfig{
		Model: "account-model", APIKeyEnv: "COMPLYSCAN_QUALIFICATION_TEST_KEY", TimeoutSeconds: 60, MaxFindings: 1,
	}
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPLYSCAN_QUALIFICATION_TEST_KEY", "secret-value")
	previous := qualifyConfiguredModel
	qualifyConfiguredModel = func(_ context.Context, actual config.AIConfig, refresh bool) (modelQualificationOutcome, error) {
		if actual.Remote.Model != "account-model" || !refresh {
			t.Fatalf("settings=%#v refresh=%t", actual, refresh)
		}
		return modelQualificationOutcome{Result: modelqualification.Result{
			Status: "compatible", ExpiresAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC),
		}}, nil
	}
	t.Cleanup(func() { qualifyConfiguredModel = previous })
	var stdout, stderr bytes.Buffer
	if code := Execute([]string{"doctor", "--probe-review", target}, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("code=%d stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] review compatibility: compatible; checked with synthetic input and cached until 2026-09-09") {
		t.Fatalf("doctor output:\n%s", stdout.String())
	}
}
