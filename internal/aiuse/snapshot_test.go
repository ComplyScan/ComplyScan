package aiuse

import (
	"reflect"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

func TestUseMatchesPath(t *testing.T) {
	use := testUse("chat", profile.ReviewDraft, StatusActive, "internal/chat/**", "cmd/worker/main.go")
	tests := []struct {
		path string
		want bool
	}{
		{path: "internal/chat/service.go", want: true},
		{path: "./internal/chat/service.go", want: true},
		{path: `internal\chat\service.go`, want: true},
		{path: "cmd/worker/main.go", want: true},
		{path: "cmd/worker/other.go", want: false},
		{path: "internal/chatter/service.go", want: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := UseMatchesPath(use, test.path); got != test.want {
				t.Fatalf("UseMatchesPath(%q) = %v, want %v", test.path, got, test.want)
			}
		})
	}
}

func TestMatchSuggestionExactAndAmbiguous(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("chat", profile.ReviewConfirmed, StatusActive, "services/chat/**"),
		testUse("shared-chat", profile.ReviewDraft, StatusActive, "services/chat/**", "shared/ai/**"),
	}

	ambiguous := providers.RepositoryAIUse{Evidence: []providers.RepositoryCitation{{Path: "services/chat/client.go", Line: 12}}}
	if got := MatchSuggestion(manifest, ambiguous); !reflect.DeepEqual(got, []string{"chat", "shared-chat"}) {
		t.Fatalf("ambiguous MatchSuggestion() = %v", got)
	}
	exact := providers.RepositoryAIUse{Evidence: []providers.RepositoryCitation{
		{Path: "services/chat/client.go", Line: 12},
		{Path: "shared/ai/prompt.go", Line: 4},
	}}
	if got := MatchSuggestion(manifest, exact); !reflect.DeepEqual(got, []string{"shared-chat"}) {
		t.Fatalf("exact MatchSuggestion() = %v", got)
	}
	outside := providers.RepositoryAIUse{Evidence: []providers.RepositoryCitation{{Path: "services/ranking/model.go", Line: 8}}}
	if got := MatchSuggestion(manifest, outside); len(got) != 0 {
		t.Fatalf("outside MatchSuggestion() = %v, want none", got)
	}
	if got := MatchSuggestion(manifest, providers.RepositoryAIUse{}); len(got) != 0 {
		t.Fatalf("empty-evidence MatchSuggestion() = %v, want none", got)
	}
}

func TestBuildSnapshotClassifiesDurableUsesAndCurrentObservations(t *testing.T) {
	chatSuggestion := providers.RepositoryAIUse{
		ID: "chat-model-id", Name: "Chat assistant", Purpose: "Answers user questions", Lifecycle: "production", Confidence: "high",
		Evidence: []providers.RepositoryCitation{{Path: "services/chat/client.go", Line: 10, Summary: "Calls the model"}},
	}
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("chat", profile.ReviewConfirmed, StatusActive, "services/chat/**"),
		testUse("ranking", profile.ReviewDraft, StatusActive, "services/ranking/**"),
		testUse("legacy", profile.ReviewConfirmed, StatusRetired, "legacy/**"),
	}
	manifest.Uses[0].SuggestionFingerprints = []string{SuggestionFingerprint(chatSuggestion)}
	dismissed := providers.RepositoryAIUse{
		ID: "dismissed-model-id", Name: "Build helper", Purpose: "Suggests build changes", Lifecycle: "development", Confidence: "medium",
		Evidence: []providers.RepositoryCitation{{Path: "tools/build/assistant.go", Line: 5, Summary: "Calls an AI API"}},
	}
	manifest.Dismissals = []Dismissal{{Fingerprint: SuggestionFingerprint(dismissed), Reason: "Not a product AI use"}}

	technical := inventory.Report{Components: []inventory.Component{
		{
			Name: "OpenAI", Kind: inventory.KindProvider,
			Locations: []inventory.Location{
				{Path: "services/ranking/model.go", Line: 30, Scope: inventory.ScopeTest},
				{Path: "unowned/provider.go", Line: 8, Scope: inventory.ScopeRuntime},
			},
		},
		{
			Name: "Anthropic", Kind: inventory.KindProvider,
			Locations: []inventory.Location{{Path: "services/chat/client.go", Line: 10, Scope: inventory.ScopeRuntime}},
		},
	}}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{
		chatSuggestion,
		{
			ID: "new-model-id", Name: "Document classifier", Purpose: "Classifies documents", Lifecycle: "testing", Confidence: "medium",
			Evidence:            []providers.RepositoryCitation{{Path: "services/documents/classifier.go", Line: 22, Summary: "Runs inference"}},
			UnresolvedQuestions: []string{"Is this deployed?"},
		},
		dismissed,
	}}}

	snapshot := BuildSnapshot(manifest, technical, analysis, false)
	if snapshot.ChangedScope {
		t.Fatal("ChangedScope = true, want false")
	}
	wantSummary := SnapshotSummary{Confirmed: 1, Draft: 1, Retired: 1, Suggested: 1, UngroupedSignals: 1}
	if snapshot.Summary != wantSummary {
		t.Fatalf("Summary = %#v, want %#v", snapshot.Summary, wantSummary)
	}
	if got := snapshot.Confirmed[0]; got.Use.ID != "chat" || got.Observation != ObservationModelReviewed || len(got.ReviewEvidence) != 1 || len(got.TechnicalSignals) != 1 {
		t.Fatalf("confirmed observation = %#v", got)
	}
	if got := snapshot.Draft[0]; got.Use.ID != "ranking" || got.Observation != ObservationTechnicalSignal || len(got.TechnicalSignals) != 1 {
		t.Fatalf("draft observation = %#v", got)
	}
	if got := snapshot.Retired[0]; got.Use.ID != "legacy" || got.Observation != ObservationNotReviewed {
		t.Fatalf("retired observation = %#v", got)
	}
	if got := snapshot.Suggested[0]; got.Name != "Document classifier" || got.Status != SuggestionNew || len(got.PossibleUseIDs) != 0 {
		t.Fatalf("suggestion = %#v", got)
	}
	if got := snapshot.UngroupedSignals[0]; got.Path != "unowned/provider.go" || got.Component != "OpenAI" {
		t.Fatalf("ungrouped signal = %#v", got)
	}
	for _, suggestion := range snapshot.Suggested {
		if suggestion.Name == dismissed.Name {
			t.Fatal("dismissed suggestion was included in the snapshot")
		}
	}
}

func TestBuildSnapshotDoesNotCollapseDistinctUsesSharingOnePath(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("support-chat", profile.ReviewConfirmed, StatusActive, "internal/ai/gateway.go")}
	original := providers.RepositoryAIUse{
		Name: "Support chat", Purpose: "Drafts support answers", Confidence: "high",
		Evidence: []providers.RepositoryCitation{{Path: "internal/ai/gateway.go", Line: 10, Summary: "Sends support prompts"}},
	}
	manifest.Uses[0].SuggestionFingerprints = []string{SuggestionFingerprint(original)}
	distinct := providers.RepositoryAIUse{
		Name: "Candidate ranking", Purpose: "Ranks job candidates", Confidence: "high",
		Evidence: []providers.RepositoryCitation{{Path: "internal/ai/gateway.go", Line: 40, Summary: "Sends ranking prompts"}},
	}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{distinct}}}

	snapshot := BuildSnapshot(manifest, inventory.Report{}, analysis, false)
	if len(snapshot.Suggested) != 1 || snapshot.Suggested[0].Status != SuggestionPossibleMatch {
		t.Fatalf("shared-path distinct suggestion was hidden: %#v", snapshot.Suggested)
	}
	if !reflect.DeepEqual(snapshot.Suggested[0].PossibleUseIDs, []string{"support-chat"}) {
		t.Fatalf("possible shared-path matches = %#v", snapshot.Suggested[0].PossibleUseIDs)
	}
	if snapshot.Confirmed[0].Observation != ObservationNotReviewed {
		t.Fatalf("distinct suggestion was linked without confirmation: %#v", snapshot.Confirmed[0])
	}
}

func TestBuildSnapshotKeepsAmbiguousSuggestionForReview(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("first", profile.ReviewConfirmed, StatusActive, "shared/ai/**"),
		testUse("second", profile.ReviewDraft, StatusActive, "shared/ai/**"),
	}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{{
		ID: "model-use", Name: "Shared model", Purpose: "Shared inference", Confidence: "medium",
		Evidence: []providers.RepositoryCitation{{Path: "shared/ai/client.go", Line: 7, Summary: "Calls a model"}},
	}}}}

	snapshot := BuildSnapshot(manifest, inventory.Report{}, analysis, false)
	if len(snapshot.Suggested) != 1 {
		t.Fatalf("suggestions = %d, want 1", len(snapshot.Suggested))
	}
	if got := snapshot.Suggested[0]; got.Status != SuggestionAmbiguous || !reflect.DeepEqual(got.PossibleUseIDs, []string{"first", "second"}) {
		t.Fatalf("ambiguous suggestion = %#v", got)
	}
	if snapshot.Confirmed[0].Observation != ObservationNotReviewed || snapshot.Draft[0].Observation != ObservationNotReviewed {
		t.Fatal("ambiguous suggestion was incorrectly linked to a durable use")
	}
}

func TestChangedScopeCannotAlterDurableUseState(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{
		testUse("confirmed", profile.ReviewConfirmed, StatusActive, "confirmed/**"),
		testUse("draft", profile.ReviewDraft, StatusActive, "draft/**"),
		testUse("retired", profile.ReviewConfirmed, StatusRetired, "retired/**"),
	}

	full := BuildSnapshot(manifest, inventory.Report{}, nil, false)
	changed := BuildSnapshot(manifest, inventory.Report{}, nil, true)
	if !changed.ChangedScope {
		t.Fatal("ChangedScope = false, want true")
	}
	full.ChangedScope = true
	if !reflect.DeepEqual(changed, full) {
		t.Fatalf("changed-scope snapshot altered durable state:\nchanged = %#v\nfull = %#v", changed, full)
	}
	if changed.Summary != (SnapshotSummary{Confirmed: 1, Draft: 1, Retired: 1}) {
		t.Fatalf("changed-scope summary = %#v", changed.Summary)
	}
	for _, group := range [][]ObservedUse{changed.Confirmed, changed.Draft, changed.Retired} {
		if group[0].Observation != ObservationNotReviewed {
			t.Fatalf("absent changed-scope use observation = %q, want not-reviewed", group[0].Observation)
		}
	}
}

func TestBuildSnapshotSurfacesSignalsMatchingRetiredUse(t *testing.T) {
	manifest := NewManifest()
	manifest.Uses = []Use{testUse("legacy", profile.ReviewConfirmed, StatusRetired, "legacy/**")}
	technical := inventory.Report{Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider,
		Locations: []inventory.Location{{Path: "legacy/client.go", Line: 8, Scope: inventory.ScopeRuntime}},
	}}}

	snapshot := BuildSnapshot(manifest, technical, nil, false)
	if len(snapshot.Retired) != 1 || snapshot.Retired[0].Observation != ObservationTechnicalSignal || len(snapshot.Retired[0].TechnicalSignals) != 1 {
		t.Fatalf("retired use observation = %#v", snapshot.Retired)
	}
	if len(snapshot.UngroupedSignals) != 0 {
		t.Fatalf("retired-use signal remained ungrouped: %#v", snapshot.UngroupedSignals)
	}
}
