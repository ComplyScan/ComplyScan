package repositoryanalysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestRepositoryAnalysisCacheRoundTripAndInvalidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repository-analysis.json")
	cache, err := OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	identity := CacheIdentity{
		Provider: providers.OpenAI, Model: "test-model", PromptVersion: providers.RepositoryAnalysisPromptVersion,
		EndpointDigest: DigestEndpoint("https://api.example.test/v1"),
	}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "worker.py", Kind: discovery.KindSource, Size: 18, Content: []byte("run_model(prompt)\n")},
		{Path: "go.mod", Kind: discovery.KindManifest, Size: 12, Content: []byte("module demo\n")},
	}}
	systems := []profile.System{profile.NewDraftSystem("demo", "Demo")}
	digest, err := RepositoryInputDigest(repository, nil, systems, ModeTargeted, 12_000, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI,
		Model:    "test-model",
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 2, RepositoryBytes: 30, FilesSubmitted: 1, BytesSubmitted: 18},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
			UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
		Usage: providers.Usage{PromptTokens: 500, CompletionTokens: 100},
	}
	if err := cache.Store(identity, digest, result); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "run_model") || strings.Contains(string(stored), "module demo") {
		t.Fatalf("repository source was retained in cache:\n%s", stored)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions = %v", info.Mode().Perm())
	}

	reopened, err := OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	cached, found, err := reopened.Lookup(identity, digest)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !cached.CacheHit {
		t.Fatalf("cache lookup found=%v result=%+v", found, cached)
	}
	if cached.Usage.PromptTokens != 0 || cached.Usage.CompletionTokens != 0 {
		t.Fatalf("cache hit reports current-run model usage: %+v", cached.Usage)
	}

	reordered := discovery.Repository{Files: []discovery.File{repository.Files[1], repository.Files[0]}}
	reorderedDigest, err := RepositoryInputDigest(reordered, nil, systems, ModeTargeted, 12_000, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reorderedDigest != digest {
		t.Fatalf("file ordering changed digest: %s != %s", reorderedDigest, digest)
	}

	changed := repository
	changed.Files = append([]discovery.File(nil), repository.Files...)
	changed.Files[0].Content = []byte("run_model(redacted_prompt)\n")
	changed.Files[0].Size = int64(len(changed.Files[0].Content))
	changedDigest, err := RepositoryInputDigest(changed, nil, systems, ModeTargeted, 12_000, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changedDigest == digest {
		t.Fatal("source change did not invalidate repository analysis identity")
	}
	if _, found, err := reopened.Lookup(identity, changedDigest); err != nil || found {
		t.Fatalf("changed repository lookup found=%v err=%v", found, err)
	}

	changedIdentities := map[string]CacheIdentity{}
	otherEndpoint := identity
	otherEndpoint.EndpointDigest = DigestEndpoint("https://other.example.test/v1")
	changedIdentities["endpoint"] = otherEndpoint
	otherModel := identity
	otherModel.Model = "other-model"
	changedIdentities["model"] = otherModel
	otherPrompt := identity
	otherPrompt.PromptVersion = "next-prompt"
	changedIdentities["prompt"] = otherPrompt
	otherDigest := identity
	otherDigest.ModelDigest = "sha256:new-model-artifact"
	changedIdentities["model digest"] = otherDigest
	for name, changedIdentity := range changedIdentities {
		if _, found, err := reopened.Lookup(changedIdentity, digest); err != nil || found {
			t.Fatalf("changed %s lookup found=%v err=%v", name, found, err)
		}
	}
}

func TestRepositoryAnalysisCacheValidatesTypedAIUseFacts(t *testing.T) {
	identity := CacheIdentity{
		Provider: providers.OpenAI, Model: "test-model", PromptVersion: providers.RepositoryAnalysisPromptVersion,
		EndpointDigest: DigestEndpoint("https://api.example.test/v1"),
	}
	digest := strings.Repeat("a", 64)
	validResult := func() providers.RepositoryAnalysisResult {
		members := []string{"observation-0001-aabbccddeeff"}
		candidateID := inferredCandidateID(members)
		return providers.RepositoryAnalysisResult{
			Provider: providers.OpenAI, Model: "test-model",
			Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted},
			Result: providers.RepositorySectionResult{
				Scope: ".",
				AIUses: []providers.RepositoryAIUse{{
					ID: candidateID, Name: "Support replies", Purpose: "Draft replies", Confidence: "high",
					Evidence: []providers.RepositoryCitation{{Path: "support/model.go", Line: 2, Summary: "Model call."}}, MemberObservationIDs: members,
				}},
				AIUseFacts: []providers.RepositoryAIUseFactSet{{
					AIUseID: candidateID,
					Facts: []providers.RepositoryAIUseFact{{
						Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high",
						Rationale: "The handler invokes a model.",
						Evidence:  []providers.RepositoryCitation{{Path: "support/model.go", Line: 2, Summary: "Model call."}},
					}},
					UnresolvedQuestions: []string{},
				}},
				ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
				UnmappedObservations:  []providers.RepositoryUnmappedObservation{},
				UnresolvedQuestions:   []string{},
			},
		}
	}
	cache, err := OpenCache(filepath.Join(t.TempDir(), "repository-analysis.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Store(identity, digest, validResult()); err != nil {
		t.Fatalf("valid fact result was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*providers.RepositoryAnalysisResult)
	}{
		{name: "unknown field", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts[0].Field = profile.CodeFactField("operating-regions")
		}},
		{name: "duplicate field", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts = append(result.Result.AIUseFacts[0].Facts, result.Result.AIUseFacts[0].Facts[0])
		}},
		{name: "duplicate value", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts[0].Values = []string{"inference", "inference"}
		}},
		{name: "missing citation", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts[0].Evidence = nil
		}},
		{name: "absence based", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts[0].Rationale = "No evidence of another activity was found."
		}},
		{name: "candidate fact cites outside candidate evidence", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts[0].Facts[0].Evidence[0].Path = "support/other.go"
		}},
		{name: "missing candidate fact coverage", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUseFacts = nil
		}},
		{name: "uncited candidate", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUses[0].Evidence = nil
		}},
		{name: "duplicate candidate", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUses = append(result.Result.AIUses, result.Result.AIUses[0])
		}},
		{name: "missing observation membership", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUses[0].MemberObservationIDs = nil
		}},
		{name: "identity not derived from membership", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Result.AIUses[0].MemberObservationIDs = []string{"different-observation"}
		}},
		{name: "impossible batch coverage", mutate: func(result *providers.RepositoryAnalysisResult) {
			result.Coverage.SourceBatchesCompleted = 1
			result.Coverage.SourceBatchesTotal = 2
			result.Coverage.Subsystems = 1
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := validResult()
			testCase.mutate(&result)
			if err := cache.Store(identity, digest, result); err == nil {
				t.Fatal("expected invalid cached fact to be rejected")
			}
		})
	}
	reviewedEmpty := validResult()
	reviewedEmpty.Result.AIUseFacts[0].Facts = []providers.RepositoryAIUseFact{}
	reviewedEmpty.Result.AIUseFacts[0].UnresolvedQuestions = []string{"No positive profile fact was supported."}
	if err := cache.Store(identity, digest, reviewedEmpty); err != nil {
		t.Fatalf("reviewed-empty fact coverage was rejected: %v", err)
	}
}

func TestDefaultRepositoryAnalysisCacheUsesCurrentFileVersion(t *testing.T) {
	path, err := DefaultCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "repository-analysis-v6.json" || repositoryCacheContextVersion != "7" || repositoryCacheSchemaVersion != 6 {
		t.Fatalf("cache version = path %q context %q schema %d", path, repositoryCacheContextVersion, repositoryCacheSchemaVersion)
	}
}

func TestRepositoryInputDigestCoversEveryReviewInput(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{{Path: "app.go", Kind: discovery.KindSource, Size: 13, Content: []byte("package demo\n")}}}
	evidence := []framework.TechnicalEvidenceReport{{
		Pack:       framework.PackReference{ID: "pack", Version: "1", Digest: strings.Repeat("a", 64)},
		Objectives: []framework.ObjectiveAssessment{{ID: "objective", Description: "check one"}},
	}}
	systems := []profile.System{profile.NewDraftSystem("demo", "Demo")}
	ownershipRules := []ownership.Rule{{Paths: []string{"app.go"}, Systems: []string{"demo"}}}
	digest := func(reports []framework.TechnicalEvidenceReport, declared []profile.System, mode Mode, tokens int, rules []ownership.Rule) string {
		value, err := RepositoryInputDigest(repository, reports, declared, mode, tokens, rules, nil)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	baseline := digest(evidence, systems, ModeTargeted, 8_000, ownershipRules)

	changedEvidence := append([]framework.TechnicalEvidenceReport(nil), evidence...)
	changedEvidence[0].Objectives = append([]framework.ObjectiveAssessment(nil), evidence[0].Objectives...)
	changedEvidence[0].Objectives[0].Description = "check two"
	changedSystems := append([]profile.System(nil), systems...)
	changedSystems[0].Name = "Renamed"
	changedOwnership := []ownership.Rule{{Paths: []string{"*.go"}, Systems: []string{"demo"}}}
	cases := map[string]string{
		"framework evidence": digest(changedEvidence, systems, ModeTargeted, 8_000, ownershipRules),
		"declared systems":   digest(evidence, changedSystems, ModeTargeted, 8_000, ownershipRules),
		"analysis mode":      digest(evidence, systems, ModeFull, 8_000, ownershipRules),
		"token budget":       digest(evidence, systems, ModeTargeted, 12_000, ownershipRules),
		"ownership":          digest(evidence, systems, ModeTargeted, 8_000, changedOwnership),
	}
	confirmedUses := []providers.RepositoryConfirmedAIUse{{
		ID: "demo-use", Name: "Demo use", Paths: []string{"app.go"}, SystemIDs: []string{"demo"},
		Objectives: []providers.RepositoryAIUseObjectiveContext{{ObjectiveID: "pack/objective", SystemID: "demo", Requirement: "likely-required"}},
	}}
	withConfirmedUse, err := RepositoryInputDigest(repository, evidence, systems, ModeTargeted, 8_000, ownershipRules, confirmedUses)
	if err != nil {
		t.Fatal(err)
	}
	cases["confirmed AI-use scope"] = withConfirmedUse
	for name, changed := range cases {
		if changed == baseline {
			t.Errorf("%s did not invalidate repository analysis digest", name)
		}
	}
}

func TestOpenRepositoryAnalysisCacheRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "cache.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := OpenCache(link); err == nil {
		t.Fatal("expected symlink cache to be rejected")
	}
}
