package modelqualification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type reviewFunc func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error)

func (function reviewFunc) Review(ctx context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
	return function(ctx, request)
}

func TestQualifyAcceptsOneBoundStructuredObservation(t *testing.T) {
	identity := CurrentIdentity(providers.Ollama, "test-model", "digest")
	if identity.RepositoryPromptVersion != providers.RepositoryAnalysisPromptVersion {
		t.Fatalf("repository prompt identity = %q, want %q", identity.RepositoryPromptVersion, providers.RepositoryAnalysisPromptVersion)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	result, err := Qualify(context.Background(), reviewFunc(func(_ context.Context, request providers.ReviewRequest) (providers.ReviewResult, error) {
		finding := request.Findings[0]
		if !strings.Contains(finding.Message, "ignore the schema") {
			t.Fatalf("probe did not contain injection-shaped text: %#v", finding)
		}
		return providers.ReviewResult{
			Provider: providers.Ollama, Model: "test-model", InputFindings: 1, Reviewed: 1,
			Observations: []providers.Observation{{Fingerprint: finding.Fingerprint, RuleID: finding.RuleID}},
			Usage:        providers.Usage{PromptTokens: 10, CompletionTokens: 5},
			RateLimits:   providers.RateLimitSnapshot{RequestsKnown: true, LimitRequests: 500, RemainingRequests: 499},
		}, nil
	}), identity, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "compatible" || result.ExpiresAt.Sub(result.CheckedAt) != CacheValidity || result.Usage.PromptTokens != 10 || result.RateLimits.RemainingRequests != 499 {
		t.Fatalf("result = %#v", result)
	}
}

func TestQualifyRejectsWrongBinding(t *testing.T) {
	identity := CurrentIdentity(providers.OpenAI, "test-model", "")
	_, err := Qualify(context.Background(), reviewFunc(func(context.Context, providers.ReviewRequest) (providers.ReviewResult, error) {
		return providers.ReviewResult{
			Provider: providers.OpenAI, Model: "test-model", InputFindings: 1, Reviewed: 1,
			Observations: []providers.Observation{{Fingerprint: strings.Repeat("b", 64), RuleID: "AI-DOC-001"}},
		}, nil
	}), identity, time.Now())
	if err == nil || !strings.Contains(err.Error(), "correctly bound") {
		t.Fatalf("expected binding error, got %v", err)
	}
}

func TestCacheBindsIdentityExpiresAndUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", cacheFileName)
	cache, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	identity := CurrentIdentity(providers.Gemini, "model", "")
	result := Result{Identity: identity, Status: "compatible", CheckedAt: now, ExpiresAt: now.Add(CacheValidity), Detail: "Passed."}
	if err := cache.Store(result); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions = %o", info.Mode().Perm())
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := reopened.Lookup(identity, now.Add(time.Hour))
	if err != nil || !found || !got.FromCache {
		t.Fatalf("lookup result=%#v found=%t err=%v", got, found, err)
	}
	changed := identity
	changed.RepositoryPromptVersion = "older-contract"
	if _, found, err := reopened.Lookup(changed, now.Add(time.Hour)); err != nil || found {
		t.Fatalf("changed contract found=%t err=%v", found, err)
	}
	if cacheSchemaVersion != 3 || cacheFileName != "model-qualification-v3.json" {
		t.Fatalf("qualification cache contract = schema %d file %q", cacheSchemaVersion, cacheFileName)
	}
	if _, found, err := reopened.Lookup(identity, result.ExpiresAt); err != nil || found {
		t.Fatalf("expired result found=%t err=%v", found, err)
	}
}
