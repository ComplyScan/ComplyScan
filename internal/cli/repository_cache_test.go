package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/repositoryanalysis"
)

func TestRepositoryReviewReusesCacheWithoutProviderCredential(t *testing.T) {
	settings := config.Default().AI
	settings.Provider = "openai"
	settings.RepositoryAnalysis.Mode = "targeted"
	settings.RepositoryAnalysis.MaxInputTokens = 8_000
	settings.Remote.ProviderName = "OpenAI"
	settings.Remote.BaseURL = "https://api.openai.com/v1"
	settings.Remote.Model = "cached-model"
	settings.Remote.APIKeyEnv = "COMPLYSCAN_MISSING_CACHE_TEST_KEY"
	t.Setenv(settings.Remote.APIKeyEnv, "")

	repository := discovery.Repository{Files: []discovery.File{{
		Path: "app.py", Kind: discovery.KindSource, Size: 20, Content: []byte("from openai import OpenAI\n"),
	}}}
	systems := []profile.System{profile.NewDraftSystem("demo", "Demo")}
	digest, err := repositoryanalysis.RepositoryInputDigest(repository, nil, systems, repositoryanalysis.ModeTargeted, 8_000, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := repositoryanalysis.CacheIdentity{
		Provider: providers.OpenAI, Model: "cached-model", PromptVersion: providers.RepositoryAnalysisPromptVersion,
		EndpointDigest: repositoryanalysis.DigestEndpoint(repositoryAnalysisEndpointIdentity(settings)),
	}
	cachePath := filepath.Join(t.TempDir(), "repository-analysis.json")
	cache, err := repositoryanalysis.OpenCache(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	cachedResult := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "cached-model",
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 1, RepositoryBytes: 20, FilesSubmitted: 1, BytesSubmitted: 20},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
			UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}
	if err := cache.Store(identity, digest, cachedResult); err != nil {
		t.Fatal(err)
	}
	previousPath := repositoryAnalysisCacheDefaultPath
	repositoryAnalysisCacheDefaultPath = func() (string, error) { return cachePath, nil }
	t.Cleanup(func() { repositoryAnalysisCacheDefaultPath = previousPath })

	var output bytes.Buffer
	result, err := reviewRepositoryWithProvider(context.Background(), settings, repository, nil, systems, nil, nil, false, providers.RateLimitSnapshot{}, &output)
	if err != nil {
		t.Fatalf("cache hit unexpectedly required provider credential: %v", err)
	}
	if !result.CacheHit || !strings.Contains(output.String(), "no model request") {
		t.Fatalf("repository cache was not reported truthfully: result=%+v output=%q", result, output.String())
	}

	if _, err := reviewRepositoryWithProvider(context.Background(), settings, repository, nil, systems, nil, nil, true, providers.RateLimitSnapshot{}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), settings.Remote.APIKeyEnv) {
		t.Fatalf("refresh did not bypass the cache and require the provider: %v", err)
	}
}
