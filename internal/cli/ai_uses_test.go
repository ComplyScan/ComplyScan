package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/report"
)

func TestAIUsesSetupConfirmsSuggestionWithoutPersistingModelID(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support assistant"))
	suggestion := aiUseTestSuggestion("volatile-model-id", "Support answer generation", "internal/support/chat.go")
	writeAIUseTestReport(t, target, suggestion)

	input := strings.NewReader("1\nCustomer support assistant\nDrafts answers for support agents\ninternal/support/chat.go,cmd/support/main.go\ny\nAlex Reviewer\n")
	var stdout, stderr bytes.Buffer
	code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild)
	if code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	manifestPath := filepath.Join(target, aiuse.DefaultPath)
	manifest, exists, err := aiuse.LoadOptional(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || len(manifest.Uses) != 1 {
		t.Fatalf("manifest exists=%t uses=%#v", exists, manifest.Uses)
	}
	use := manifest.Uses[0]
	if use.ID != "customer-support-assistant" || use.Name != "Customer support assistant" || use.Description != "Drafts answers for support agents" {
		t.Fatalf("confirmed use = %#v", use)
	}
	if strings.Join(use.SystemIDs, ",") != "support" || strings.Join(use.Paths, ",") != "cmd/support/main.go,internal/support/chat.go" {
		t.Fatalf("confirmed association = systems %#v, paths %#v", use.SystemIDs, use.Paths)
	}
	if use.Review.Status != profile.ReviewConfirmed || use.Review.ReviewedBy != "Alex Reviewer" || use.Review.ReviewedAt == "" {
		t.Fatalf("review = %#v", use.Review)
	}
	if !reflect.DeepEqual(use.SuggestionFingerprints, []string{aiuse.SuggestionFingerprint(suggestion)}) {
		t.Fatalf("linked suggestion fingerprints = %#v", use.SuggestionFingerprints)
	}
	encoded, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), suggestion.ID) {
		t.Fatalf("raw model ID entered durable manifest:\n%s", encoded)
	}
	if !strings.Contains(stdout.String(), "confirm as a new AI use — create a stable, developer-owned record") || strings.Contains(stdout.String(), "confirm-new") {
		t.Fatalf("AI-use decision choices were not developer-friendly:\n%s", stdout.String())
	}
}

func TestAIUsesSetupRequiresExplicitSystemChoiceWhenSeveralExist(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target,
		profile.NewDraftSystem("ranking", "Ranking"),
		profile.NewDraftSystem("support", "Support"),
	)
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Shared inference", "internal/shared/model.go"))

	input := strings.NewReader("1\n\n\n\ny\n1,2\nSam Reviewer\n")
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	manifest, _, err := aiuse.LoadOptional(filepath.Join(target, aiuse.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(manifest.Uses[0].SystemIDs, ","); got != "ranking,support" {
		t.Fatalf("system IDs = %q", got)
	}
	if !strings.Contains(stdout.String(), "ranking — Ranking") || !strings.Contains(stdout.String(), "support — Support") {
		t.Fatalf("configured systems were not explained:\n%s", stdout.String())
	}
}

func TestAIUsesSetupAllowsNoSystemAssociation(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Shared model gateway", "internal/ai/gateway.go"))

	input := strings.NewReader("1\n\n\n\nn\nCasey Reviewer\n")
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	manifest, _, err := aiuse.LoadOptional(filepath.Join(target, aiuse.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Uses) != 1 || len(manifest.Uses[0].SystemIDs) != 0 {
		t.Fatalf("optional association was persisted unexpectedly: %#v", manifest.Uses)
	}
	if !strings.Contains(stdout.String(), "will remain unassociated") {
		t.Fatalf("unassociated choice was not explained:\n%s", stdout.String())
	}
}

func TestAIUsesSetupWorksWithoutDeclaredSystems(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target)
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Repository helper", "internal/ai/helper.go"))

	input := strings.NewReader("1\n\n\n\nDana Reviewer\n")
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	manifest, _, err := aiuse.LoadOptional(filepath.Join(target, aiuse.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Uses) != 1 || len(manifest.Uses[0].SystemIDs) != 0 {
		t.Fatalf("unexpected system association: %#v", manifest.Uses)
	}
	if !strings.Contains(stdout.String(), "No declared system is configured") {
		t.Fatalf("missing unassociated-system explanation:\n%s", stdout.String())
	}
}

func TestAIUsesSetupMergesOnlyExplicitPathsAndRefreshesReview(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	manifestPath := filepath.Join(target, aiuse.DefaultPath)
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{{
		ID: "support-chat", Name: "Support chat", Description: "Existing use", SystemIDs: []string{"support"},
		Paths: []string{"internal/support/**"}, Status: aiuse.StatusActive,
		Review: profile.ProfileReview{Status: profile.ReviewDraft},
	}}
	if err := aiuse.Write(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	writeAIUseTestReport(t, target, aiUseTestSuggestion("different-model-id", "New chat integration", "cmd/support/main.go"))

	input := strings.NewReader("2\n1\nMorgan Reviewer\n")
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	loaded, _, err := aiuse.LoadOptional(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	use := loaded.Uses[0]
	if strings.Join(use.Paths, ",") != "cmd/support/main.go,internal/support/**" {
		t.Fatalf("merged paths = %#v", use.Paths)
	}
	if use.Name != "Support chat" || use.Description != "Existing use" || use.Status != aiuse.StatusActive {
		t.Fatalf("merge changed durable identity or status: %#v", use)
	}
	if use.Review.Status != profile.ReviewConfirmed || use.Review.ReviewedBy != "Morgan Reviewer" {
		t.Fatalf("review was not refreshed: %#v", use.Review)
	}
	if !reflect.DeepEqual(use.SuggestionFingerprints, []string{aiuse.SuggestionFingerprint(aiUseTestSuggestion("different-model-id", "New chat integration", "cmd/support/main.go"))}) {
		t.Fatalf("merge did not retain its explicit suggestion link: %#v", use.SuggestionFingerprints)
	}
	if !strings.Contains(stdout.String(), "support-chat — Support chat — Existing use") {
		t.Fatalf("saved-use choice did not explain the durable target:\n%s", stdout.String())
	}
}

func TestAIUsesSetupDismissesByFingerprintAndDoesNotPromptAgain(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	suggestion := aiUseTestSuggestion("first-model-id", "Test-only integration", "internal/test/model.go")
	suggestion.Evidence[0].Summary = "Calls\x1b[31m the configured model"
	writeAIUseTestReport(t, target, suggestion)

	var stdout, stderr bytes.Buffer
	input := strings.NewReader("2\nOnly test scaffolding, not a product AI use\n")
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	manifestPath := filepath.Join(target, aiuse.DefaultPath)
	manifest, _, err := aiuse.LoadOptional(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Dismissals) != 1 || manifest.Dismissals[0].Fingerprint != aiuse.SuggestionFingerprint(suggestion) {
		t.Fatalf("dismissals = %#v", manifest.Dismissals)
	}
	if strings.Contains(manifest.Dismissals[0].Fingerprint, suggestion.ID) {
		t.Fatal("dismissal fingerprint unexpectedly contains model-authored ID")
	}
	if strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("unsafe model text reached the dismissal prompt: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("dismissed rerun code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "No new AI-use suggestions need a decision") {
		t.Fatalf("dismissed suggestion was not skipped:\n%s", stdout.String())
	}
}

func TestAIUsesSetupDecideLaterDoesNotCreateOrChangeManifest(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Support", "support.go"))

	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, strings.NewReader("3\n"), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "No AI-use decisions were saved") {
		t.Fatalf("missing decide-later summary:\n%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(target, aiuse.DefaultPath)); !os.IsNotExist(err) {
		t.Fatalf("decide-later wrote a manifest: %v", err)
	}
}

func TestAIUsesSetupSanitizesUntrustedSuggestionText(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	suggestion := aiUseTestSuggestion("draft-1", "Support\x1b[2J\u202eassistant", "support.go")
	suggestion.Purpose = "Line one\nLine two"
	suggestion.Evidence[0].Summary = "Calls\x08 model"
	suggestion.UnresolvedQuestions = []string{"Enabled\x1b[31m in production?"}
	writeAIUseTestReport(t, target, suggestion)

	var stdout, stderr bytes.Buffer
	input := strings.NewReader("1\n\n\n\nn\nTaylor Reviewer\n")
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, input, &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if strings.ContainsAny(stdout.String(), "\x1b\x08") || strings.Contains(stdout.String(), "\u202e") {
		t.Fatalf("unsafe terminal control survived rendering: %q", stdout.String())
	}
	for _, expected := range []string{"Support [2J assistant", "Line one Line two", "Calls model", "Enabled [31m in production?"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("sanitized output missing %q:\n%s", expected, stdout.String())
		}
	}
	manifest, _, err := aiuse.LoadOptional(filepath.Join(target, aiuse.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Uses) != 1 || !reflect.DeepEqual(manifest.Uses[0].SuggestionFingerprints, []string{aiuse.SuggestionFingerprint(suggestion)}) {
		t.Fatalf("raw suggestion identity was not retained safely: %#v", manifest.Uses)
	}

	stdout.Reset()
	stderr.Reset()
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("unchanged rerun code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "No new AI-use suggestions need a decision") {
		t.Fatalf("unchanged sanitized suggestion was prompted again:\n%s", stdout.String())
	}
}

func TestAIUsesSetupRejectsUnsafeEvidencePath(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Support", "support.go\x1b[2J"))

	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "unsafe terminal control") || strings.Contains(stdout.String(), "\x1b") {
		t.Fatalf("unsafe path was not rejected safely; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestAIUsesSetupRequiresTerminalUnlessExplicitlyInteractive(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Support", "support.go"))
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 2 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "requires a terminal") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(target, aiuse.DefaultPath)); !os.IsNotExist(err) {
		t.Fatalf("non-interactive setup wrote a manifest: %v", err)
	}
}

func TestAIUsesSetupWarnsWhenSuggestionsComeFromChangedScope(t *testing.T) {
	target := t.TempDir()
	writeAIUseTestConfig(t, target, profile.NewDraftSystem("support", "Support"))
	writeAIUseTestReport(t, target, aiUseTestSuggestion("draft-1", "Support", "support.go"))
	reportPath := filepath.Join(target, report.DefaultDirectory, "latest.json")
	value, err := loadAIUseReport(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	value.Scan.Scope.AIReview = "changed-plus-connected"
	writeRawAIUseReport(t, reportPath, value)

	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--interactive", target}, strings.NewReader("3\n"), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q\n%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "reviewed changed and connected code only") || !strings.Contains(stdout.String(), "will not retire or delete") {
		t.Fatalf("changed-scope limitation was not explained:\n%s", stdout.String())
	}
}

func TestAIUsesShowSupportsTerminalAndJSON(t *testing.T) {
	target := t.TempDir()
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{{
		ID: "ranking", Name: "Ranking", Description: "Ranks candidates", SystemIDs: []string{"ranking"}, Paths: []string{"ranking/**"},
		Status: aiuse.StatusActive, Review: profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "Reviewer", ReviewedAt: "2026-08-15"},
	}}
	if err := aiuse.Write(filepath.Join(target, aiuse.DefaultPath), manifest); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		format string
		want   string
	}{{format: "terminal", want: "Ranking (ranking)"}, {format: "json", want: `"reviewed_by": "Reviewer"`}} {
		var stdout, stderr bytes.Buffer
		if code := executeWithInput([]string{"ai-uses", "show", "--format", test.format, target}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
			t.Fatalf("%s exit code = %d; stderr=%q", test.format, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("%s output:\n%s", test.format, stdout.String())
		}
	}
}

func TestAIUsesHelpStatesTheModelAndComplianceBoundaries(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := executeWithInput([]string{"ai-uses", "setup", "--help"}, strings.NewReader(""), &stdout, &stderr, testBuild); code != 0 {
		t.Fatalf("exit code = %d; stderr=%q", code, stderr.String())
	}
	for _, expected := range []string{"makes no model request", "only after explicit", "not a compliance conclusion"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("setup help missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestLoadAIUseReportUpgradesCompletedSchemaSixAnalysis(t *testing.T) {
	target := t.TempDir()
	path := filepath.Join(target, "legacy.json")
	value := report.New(target, "legacy", nil, nil, 0)
	value.SchemaVersion = 6
	value.RepositoryAnalysisRun = ""
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{
		Scope: ".", AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
		UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}}
	writeRawAIUseReport(t, path, value)

	loaded, err := loadAIUseReport(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RepositoryAnalysisRun != report.RepositoryAnalysisCompleted {
		t.Fatalf("legacy repository analysis status = %q", loaded.RepositoryAnalysisRun)
	}
}

func TestLoadAIUseReportRejectsUnsupportedSchemaAndLifecycleMismatch(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   report.Report
		wantErr string
	}{
		{name: "future schema", value: report.Report{SchemaVersion: 8, RepositoryAnalysisRun: report.RepositoryAnalysisCompleted, RepositoryAnalysis: &providers.RepositoryAnalysisResult{}}, wantErr: "unsupported schema version 8"},
		{name: "missing completed result", value: report.Report{SchemaVersion: 7, RepositoryAnalysisRun: report.RepositoryAnalysisCompleted}, wantErr: "has no result"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.json")
			writeRawAIUseReport(t, path, test.value)
			if _, err := loadAIUseReport(path); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("loadAIUseReport() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func writeAIUseTestConfig(t *testing.T, target string, systems ...profile.System) {
	t.Helper()
	cfg := config.Default()
	cfg.Systems = systems
	if err := config.Write(filepath.Join(target, config.FileName), cfg, false); err != nil {
		t.Fatal(err)
	}
}

func writeAIUseTestReport(t *testing.T, target string, suggestions ...providers.RepositoryAIUse) {
	t.Helper()
	directory := filepath.Join(target, report.DefaultDirectory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	value := report.New(target, "test", nil, nil, 0)
	value.RepositoryAnalysisRun = report.RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{
		Scope: ".", AIUses: suggestions, ObjectiveObservations: []providers.RepositoryObjectiveObservation{},
		UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
	}}
	file, err := os.Create(filepath.Join(directory, "latest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := report.WriteJSON(file, value); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeRawAIUseReport(t *testing.T, path string, value report.Report) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := report.WriteJSON(file, value); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func aiUseTestSuggestion(id, name, path string) providers.RepositoryAIUse {
	return providers.RepositoryAIUse{
		ID: id, Name: name, Purpose: "Uses an AI model in a product workflow", Lifecycle: "production", Confidence: "high",
		Evidence:            []providers.RepositoryCitation{{Path: path, Line: 12, Summary: "Calls the configured model"}},
		UnresolvedQuestions: []string{"Is this enabled in production?"},
	}
}
