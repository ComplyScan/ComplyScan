package aiuse

import (
	"reflect"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestBuildSnapshotDerivesFullRepositoryFactsPerUseWithoutProvider(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("chat", profile.ReviewConfirmed, StatusActive, "services/chat/**"),
		testUse("ranking", profile.ReviewDraft, StatusActive, "services/ranking/**"),
	}
	technical := inventory.Report{Components: []inventory.Component{
		{
			Name: "Zeta AI", Kind: inventory.KindProvider, Confidence: "high",
			Locations: []inventory.Location{
				{Path: "services/chat/client.go", Line: 8, Scope: inventory.ScopeRuntime, Evidence: "runtime import zeta"},
				{Path: "services/chat/client.go", Line: 8, Scope: inventory.ScopeRuntime, Evidence: "runtime import zeta"},
			},
		},
		{
			Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high",
			Locations: []inventory.Location{{Path: "services/chat/client.go", Line: 3, Scope: inventory.ScopeRuntime, Evidence: "import openai"}},
		},
		{
			Name: "Anthropic", Kind: inventory.KindProvider, Confidence: "high",
			Locations: []inventory.Location{{Path: "services/ranking/settings.yml", Line: 2, Scope: inventory.ScopeConfig, Evidence: "provider setting"}},
		},
	}}

	snapshot := BuildSnapshotWithRepository(manifest, technical, discovery.Repository{}, nil, true)
	chat := snapshot.Confirmed[0]
	if chat.RepositoryFacts == nil || chat.RepositoryFacts.Status != FactReviewDeterministicOnly ||
		chat.RepositoryFacts.DeterministicCoverage != FactCoverageFullRepository || chat.RepositoryFacts.ModelCoverage != "" {
		t.Fatalf("chat facts = %#v", chat.RepositoryFacts)
	}
	if activity := factForField(chat.RepositoryFacts, profile.CodeFactAIActivities); activity != nil {
		t.Fatalf("provider reference was incorrectly promoted to an inference activity: %#v", activity)
	}
	if got := providerNames(chat.RepositoryFacts.ModelProviders); !reflect.DeepEqual(got, []string{"OpenAI", "Zeta AI"}) {
		t.Fatalf("model providers = %v", got)
	}
	if len(chat.RoleCandidates) != 1 || chat.RoleCandidates[0].Role != TechnicalRoleDeployer || chat.RoleCandidates[0].Status != "possible" ||
		len(chat.RoleCandidates[0].MissingOrganizationFacts) == 0 {
		t.Fatalf("deployer candidate = %#v", chat.RoleCandidates)
	}
	if ranking := snapshot.Draft[0]; ranking.RepositoryFacts != nil || len(ranking.RoleCandidates) != 0 {
		t.Fatalf("configuration-only signal became runtime fact: %#v", ranking)
	}
	if len(snapshot.OrganizationUnknowns) != len(fixedOrganizationUnknowns) || len(snapshot.OrganizationUnknowns) == 0 {
		t.Fatalf("organisation unknowns = %#v", snapshot.OrganizationUnknowns)
	}
}

func TestBuildSnapshotAttachesModelFactsOnlyByExactIDAndMarksChangedCoverage(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("chat", profile.ReviewConfirmed, StatusActive, "services/chat/**"),
		testUse("ranking", profile.ReviewConfirmed, StatusActive, "services/ranking/**"),
	}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{
		AIUses: []providers.RepositoryAIUse{{
			ID: "new-use", Name: "New use", Purpose: "Generate summaries", Confidence: "medium",
			Evidence: []providers.RepositoryCitation{{Path: "services/new/client.go", Line: 4, Summary: "Model call"}},
		}},
		AIUseFacts: []providers.RepositoryAIUseFactSet{
			{
				AIUseID: "chat", UnresolvedQuestions: []string{"Is the gate used in every route?"},
				Facts: []providers.RepositoryAIUseFact{{
					Field: profile.CodeFactHumanOversight, Values: []string{"required"}, Confidence: "high",
					Rationale: "The reviewed route calls an approval gate.",
					Evidence:  []providers.RepositoryCitation{{Path: "services/chat/review.go", Line: 12, Summary: "Approval gate"}},
				}},
			},
			{
				AIUseID: "new-use", Facts: []providers.RepositoryAIUseFact{{
					Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "medium",
					Rationale: "The candidate calls a model.",
					Evidence:  []providers.RepositoryCitation{{Path: "services/new/client.go", Line: 4, Summary: "Model call"}},
				}},
			},
			{
				AIUseID: "unknown-use", Facts: []providers.RepositoryAIUseFact{{
					Field: profile.CodeFactAIActivities, Values: []string{"training"}, Confidence: "high", Rationale: "Unknown use",
					Evidence: []providers.RepositoryCitation{{Path: "unknown.py", Line: 1, Summary: "Training"}},
				}},
			},
		},
	}}

	snapshot := BuildSnapshotWithRepository(manifest, inventory.Report{}, discovery.Repository{}, analysis, true)
	chat := snapshot.Confirmed[0]
	if chat.Use.ID != "chat" || chat.RepositoryFacts == nil || chat.RepositoryFacts.Status != FactReviewModelReviewed ||
		chat.RepositoryFacts.ModelCoverage != FactCoverageChangedAndConnected || chat.RepositoryFacts.DeterministicCoverage != "" {
		t.Fatalf("changed model facts = %#v", chat.RepositoryFacts)
	}
	fact := factForField(chat.RepositoryFacts, profile.CodeFactHumanOversight)
	if fact == nil || fact.Source != FactSourceModel || fact.Coverage != FactCoverageChangedAndConnected ||
		fact.Strength != FactStrengthModelReasoned || len(fact.Evidence) != 1 {
		t.Fatalf("model fact = %#v", fact)
	}
	if !reflect.DeepEqual(chat.RepositoryFacts.UnresolvedQuestions, []string{"Is the gate used in every route?"}) {
		t.Fatalf("fact questions = %#v", chat.RepositoryFacts.UnresolvedQuestions)
	}
	if ranking := snapshot.Confirmed[1]; ranking.Use.ID != "ranking" || ranking.RepositoryFacts != nil {
		t.Fatalf("fact leaked to sibling use: %#v", ranking)
	}
	if len(snapshot.Suggested) != 1 || snapshot.Suggested[0].RepositoryFacts == nil ||
		snapshot.Suggested[0].RepositoryFacts.ModelCoverage != FactCoverageChangedAndConnected ||
		factForField(snapshot.Suggested[0].RepositoryFacts, profile.CodeFactAIActivities) == nil {
		t.Fatalf("candidate facts = %#v", snapshot.Suggested)
	}
}

func TestPackagedThirdPartyAIProductProducesMultiplePossibleRoles(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("assistant", profile.ReviewConfirmed, StatusActive, "internal/assistant/**", ".github/workflows/**", "cmd/assistant/**")}
	technical := inventory.Report{Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high",
		Locations: []inventory.Location{{Path: "internal/assistant/client.go", Line: 9, Scope: inventory.ScopeRuntime, Evidence: "OpenAI runtime client"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: ".github/workflows/release.yml", Kind: discovery.KindGitHubAction,
			Content: []byte("name: Release\nsteps:\n  - uses: goreleaser/goreleaser-action@v7\n"),
		},
		{Path: "cmd/assistant/main.go", Kind: discovery.KindSource, Content: []byte("package main\nfunc main() {}\n")},
	}}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUseFacts: []providers.RepositoryAIUseFactSet{{
		AIUseID: "assistant", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high", Rationale: "The command invokes the provider client.",
			Evidence: []providers.RepositoryCitation{{Path: "internal/assistant/client.go", Line: 9, Summary: "Model invocation"}},
		}},
	}}}}

	snapshot := BuildSnapshotWithRepository(manifest, technical, repository, analysis, false)
	use := snapshot.Confirmed[0]
	deployment := factForField(use.RepositoryFacts, profile.CodeFactDeploymentModels)
	if deployment == nil || !reflect.DeepEqual(deployment.Values, []string{"local-cli"}) || deployment.Source != FactSourceDeterministic {
		t.Fatalf("local CLI fact = %#v", deployment)
	}
	wantRoles := []TechnicalRole{TechnicalRoleDeployer, TechnicalRoleDownstreamProvider, TechnicalRoleProvider}
	if got := roleNames(use.RoleCandidates); !reflect.DeepEqual(got, wantRoles) {
		t.Fatalf("roles = %v, want %v", got, wantRoles)
	}
	for _, candidate := range use.RoleCandidates {
		if candidate.Status != "possible" || candidate.Condition == "" || len(candidate.MissingOrganizationFacts) == 0 || len(candidate.Evidence) == 0 {
			t.Fatalf("role candidate lost its boundary: %#v", candidate)
		}
	}
}

func TestPackagedModelFactCanSuggestProviderWithoutDeployer(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("trainer", profile.ReviewConfirmed, StatusActive, "training/**", "install.sh")}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "install.sh", Kind: discovery.KindSource,
		Content: []byte("#!/bin/sh\ncurl -fsSL https://example.test/releases/download/v1/tool.tar.gz\ninstall_dir=/usr/local/bin\n"),
	}}}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUseFacts: []providers.RepositoryAIUseFactSet{{
		AIUseID: "trainer", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{"training"}, Confidence: "high", Rationale: "Trainer executes training.",
			Evidence: []providers.RepositoryCitation{{Path: "training/run.py", Line: 20, Summary: "Calls train"}},
		}},
	}}}}

	snapshot := BuildSnapshotWithRepository(manifest, inventory.Report{}, repository, analysis, false)
	use := snapshot.Confirmed[0]
	if got := roleNames(use.RoleCandidates); !reflect.DeepEqual(got, []TechnicalRole{TechnicalRoleProvider}) {
		t.Fatalf("model-only packaged roles = %v", got)
	}
	if use.RoleCandidates[0].Source != FactSourceModel {
		t.Fatalf("model-only provider source = %q", use.RoleCandidates[0].Source)
	}
	if factForField(use.RepositoryFacts, profile.CodeFactDeploymentModels) == nil {
		t.Fatal("model-supported AI activity did not connect to the deterministic CLI artifact")
	}
}

func TestPackagedProviderReferenceDoesNotEstablishAIActivity(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("tool", profile.ReviewConfirmed, StatusActive, "tool/**")}
	technical := inventory.Report{Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high",
		Locations: []inventory.Location{{Path: "tool/client.go", Line: 3, Scope: inventory.ScopeRuntime, Evidence: "import openai"}},
	}}}
	repository := discovery.Repository{Files: []discovery.File{{
		Path: "tool/.goreleaser.yml", Kind: discovery.KindConfig, Content: []byte("builds:\n  - main: ./cmd/tool\n"),
	}}}

	use := BuildSnapshotWithRepository(manifest, technical, repository, nil, false).Confirmed[0]
	if activity := factForField(use.RepositoryFacts, profile.CodeFactAIActivities); activity != nil {
		t.Fatalf("provider reference became activity: %#v", activity)
	}
	if deployment := factForField(use.RepositoryFacts, profile.CodeFactDeploymentModels); deployment != nil {
		t.Fatalf("packaging plus provider reference became deployment fact: %#v", deployment)
	}
	if got := roleNames(use.RoleCandidates); !reflect.DeepEqual(got, []TechnicalRole{TechnicalRoleDeployer}) {
		t.Fatalf("unproven activity produced provider roles: %v", got)
	}
}

func TestDocumentationAndOrdinaryManifestDoNotCreateFactsOrRoles(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("glossary", profile.ReviewConfirmed, StatusActive, "**")}
	repository := discovery.Repository{Files: []discovery.File{
		{
			Path: "README.md", Kind: discovery.KindReadme,
			Content: []byte("European Union provider. Run goreleaser and npm publish. curl releases/download. production public API."),
		},
		{Path: "package.json", Kind: discovery.KindManifest, Content: []byte(`{"name":"terminology-viewer","private":true}`)},
		{Path: "LICENSE", Kind: discovery.KindOtherText, Content: []byte("provider deployer distribution")},
	}}

	snapshot := BuildSnapshotWithRepository(manifest, inventory.Report{}, repository, nil, false)
	use := snapshot.Confirmed[0]
	if use.RepositoryFacts != nil || len(use.RoleCandidates) != 0 {
		t.Fatalf("prose-only repository produced technical facts: %#v", use)
	}
}

func TestDistributionArtifactsRemainScopedToTheirAIUse(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("cli", profile.ReviewConfirmed, StatusActive, "apps/cli/**"),
		testUse("service", profile.ReviewConfirmed, StatusActive, "services/api/**"),
	}
	technical := inventory.Report{Components: []inventory.Component{
		{Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high", Locations: []inventory.Location{{Path: "apps/cli/client.go", Line: 5, Scope: inventory.ScopeRuntime, Evidence: "OpenAI client"}}},
		{Name: "Anthropic", Kind: inventory.KindProvider, Confidence: "high", Locations: []inventory.Location{{Path: "services/api/client.go", Line: 6, Scope: inventory.ScopeRuntime, Evidence: "Anthropic client"}}},
	}}
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "apps/cli/.goreleaser.yml", Kind: discovery.KindConfig, Content: []byte("builds:\n  - main: ./cmd/tool\n")},
		{Path: "apps/cli/client.go", Kind: discovery.KindSource, Content: []byte("package cli\n")},
		{Path: "services/api/client.go", Kind: discovery.KindSource, Content: []byte("package api\n")},
	}}

	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUseFacts: []providers.RepositoryAIUseFactSet{
		{AIUseID: "cli", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high", Rationale: "The CLI invokes a model.",
			Evidence: []providers.RepositoryCitation{{Path: "apps/cli/client.go", Line: 5, Summary: "Model invocation"}},
		}}},
		{AIUseID: "service", Facts: []providers.RepositoryAIUseFact{{
			Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high", Rationale: "The service invokes a model.",
			Evidence: []providers.RepositoryCitation{{Path: "services/api/client.go", Line: 6, Summary: "Model invocation"}},
		}}},
	}}}
	snapshot := BuildSnapshotWithRepository(manifest, technical, repository, analysis, false)
	if deployment := factForField(snapshot.Confirmed[0].RepositoryFacts, profile.CodeFactDeploymentModels); deployment == nil {
		t.Fatal("scoped CLI use did not retain its distribution fact")
	}
	if got := roleNames(snapshot.Confirmed[0].RoleCandidates); !reflect.DeepEqual(got, []TechnicalRole{TechnicalRoleDeployer, TechnicalRoleDownstreamProvider, TechnicalRoleProvider}) {
		t.Fatalf("CLI roles = %v", got)
	}
	if deployment := factForField(snapshot.Confirmed[1].RepositoryFacts, profile.CodeFactDeploymentModels); deployment != nil {
		t.Fatalf("unrelated service inherited CLI distribution: %#v", deployment)
	}
	if got := roleNames(snapshot.Confirmed[1].RoleCandidates); !reflect.DeepEqual(got, []TechnicalRole{TechnicalRoleDeployer}) {
		t.Fatalf("unrelated service roles = %v", got)
	}
}

func TestCandidateIDCollisionCannotAttachFactsToDurableUse(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("shared-id", profile.ReviewDraft, StatusActive, "saved/**")}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{
		AIUses: []providers.RepositoryAIUse{{
			ID: "shared-id", Name: "Different candidate", Purpose: "Runs a different model", Confidence: "high",
			Evidence: []providers.RepositoryCitation{{Path: "candidate/model.go", Line: 4, Summary: "Model call"}},
		}},
		AIUseFacts: []providers.RepositoryAIUseFactSet{{
			AIUseID: "shared-id", Facts: []providers.RepositoryAIUseFact{{
				Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high", Rationale: "Candidate calls a model.",
				Evidence: []providers.RepositoryCitation{{Path: "candidate/model.go", Line: 4, Summary: "Model call"}},
			}},
		}},
	}}

	snapshot := BuildSnapshotWithRepository(manifest, inventory.Report{}, discovery.Repository{}, analysis, false)
	if snapshot.Draft[0].RepositoryFacts != nil || snapshot.Draft[0].Observation == ObservationModelReviewed {
		t.Fatalf("candidate facts contaminated durable use: %#v", snapshot.Draft[0])
	}
	if len(snapshot.Suggested) != 1 || snapshot.Suggested[0].RepositoryFacts == nil {
		t.Fatalf("candidate facts were not retained with candidate: %#v", snapshot.Suggested)
	}
}

func factForField(review *FactReview, field profile.CodeFactField) *Fact {
	if review == nil {
		return nil
	}
	for index := range review.Facts {
		if review.Facts[index].Field == field {
			return &review.Facts[index]
		}
	}
	return nil
}

func providerNames(values []ModelProviderObservation) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Name)
	}
	return result
}

func roleNames(values []RoleCandidate) []TechnicalRole {
	result := make([]TechnicalRole, 0, len(values))
	for _, value := range values {
		result = append(result, value.Role)
	}
	return result
}
