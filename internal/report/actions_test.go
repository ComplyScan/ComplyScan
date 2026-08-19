package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestDeveloperActionLifecycleUsesStableFindingIdentity(t *testing.T) {
	finding := rules.Finding{
		Fingerprint: "stable-fingerprint", RuleID: "AI-TEST-001", Title: "Missing AI guard",
		Severity: rules.SeverityHigh, Path: "app.go", StartLine: 12, Message: "A guard is missing.", Remediation: "Add the guard.",
	}
	first := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(1, 0), []rules.Finding{finding}, nil, 0)
	first = ReconcileDeveloperActionLifecycle(first, nil)
	if len(first.DeveloperActions) != 1 {
		t.Fatalf("first actions = %#v", first.DeveloperActions)
	}
	action := first.DeveloperActions[0]
	if action.ID != "finding/stable-fingerprint" || action.Status != DeveloperActionNew || action.FirstSeenScanID != first.Scan.ID {
		t.Fatalf("first action = %#v", action)
	}
	if len(action.Evidence) != 1 || action.Evidence[0].Path != "app.go" || action.Evidence[0].StartLine != 12 {
		t.Fatalf("action evidence = %#v", action.Evidence)
	}
	if action.Title == "" || action.Why == "" || action.RecommendedChange == "" || len(action.AcceptanceCriteria) == 0 || action.Verification.Type != "rescan" || action.Verification.Command == "" {
		t.Fatalf("action is not implementation-ready = %#v", action)
	}

	second := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(2, 0), []rules.Finding{finding}, nil, 0)
	second = ReconcileDeveloperActionLifecycle(second, &first)
	if got := second.DeveloperActions[0]; got.Status != DeveloperActionOpen || got.FirstSeenScanID != first.Scan.ID || got.LastSeenScanID != second.Scan.ID {
		t.Fatalf("repeated action = %#v", got)
	}

	resolved := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(3, 0), nil, nil, 0)
	resolved = ReconcileDeveloperActionLifecycle(resolved, &second)
	if len(resolved.DeveloperActions) != 1 || resolved.DeveloperActions[0].Status != DeveloperActionResolved || resolved.DeveloperActions[0].ResolvedInScanID != resolved.Scan.ID {
		t.Fatalf("resolved actions = %#v", resolved.DeveloperActions)
	}

	reappeared := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(4, 0), []rules.Finding{finding}, nil, 0)
	reappeared = ReconcileDeveloperActionLifecycle(reappeared, &resolved)
	if reappeared.DeveloperActions[0].Status != DeveloperActionReopened {
		t.Fatalf("reappeared action = %#v", reappeared.DeveloperActions[0])
	}
}

func TestChangedScopeDoesNotFalselyResolvePriorAction(t *testing.T) {
	prior := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(1, 0), nil, nil, 0)
	prior.DeveloperActions = []DeveloperAction{{ID: "finding/prior", Status: DeveloperActionOpen, Category: "deterministic-finding", Title: "Prior action"}}
	changed := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "changed-files", ChangedSince: "main"}, time.Unix(2, 0), nil, nil, 0)
	changed = ReconcileDeveloperActionLifecycle(changed, &prior)
	if len(changed.DeveloperActions) != 1 || changed.DeveloperActions[0].Status != DeveloperActionOpen {
		t.Fatalf("changed-scope actions = %#v", changed.DeveloperActions)
	}
}

func TestExplicitChangedBaselineResolvesOnlyActionsWhoseEvidenceChanged(t *testing.T) {
	prior := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(1, 0), nil, nil, 0)
	prior.DeveloperActions = []DeveloperAction{
		{ID: "finding/changed", Status: DeveloperActionOpen, Category: "deterministic-finding", Title: "Changed", Evidence: []DeveloperActionEvidence{{Path: "changed.go", StartLine: 3}}},
		{ID: "finding/unchanged", Status: DeveloperActionOpen, Category: "deterministic-finding", Title: "Unchanged", Evidence: []DeveloperActionEvidence{{Path: "unchanged.go", StartLine: 3}}},
	}
	current := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "changed-files", ChangedSince: "main"}, time.Unix(2, 0), nil, nil, 0)
	current = ReconcileDeveloperActionLifecycleWithOptions(current, &prior, DeveloperActionLifecycleOptions{ChangedPaths: map[string]struct{}{"changed.go": {}}})
	statuses := map[string]DeveloperActionStatus{}
	for _, action := range current.DeveloperActions {
		statuses[action.ID] = action.Status
	}
	if statuses["finding/changed"] != DeveloperActionResolved || statuses["finding/unchanged"] != DeveloperActionOpen {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestWriteJSONAndMarkdownExposeDeveloperActionContract(t *testing.T) {
	value := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "test"}, ScanScope{Findings: "full-repository"}, time.Unix(1, 0), nil, nil, 0)
	value.Scan.Repository = &RepositoryProvenance{Commit: "abcdef", Branch: "main", Dirty: true, TargetPath: ".", BaseReference: "origin/main", BaseCommit: "123456"}
	value.Scan.ConfigDigest = "config-digest"
	value.Scan.FrameworkPacks = []FrameworkPackProvenance{{ID: "eu-ai-act", Name: "EU AI Act", Version: "1", Digest: "pack-digest"}}
	value.DeveloperActions = []DeveloperAction{{
		ID: "objective/system/control", Status: DeveloperActionNew, Priority: "Review", Category: "code-control",
		Title: "Add an AI event log", Why: "No implementation was verified.", RecommendedChange: "Add structured logging.",
		AcceptanceCriteria: []string{"A cited test verifies the event."}, Evidence: []DeveloperActionEvidence{{Path: "app.go", StartLine: 8}},
		Frameworks: []DeveloperActionFramework{{ID: "eu-ai-act", Name: "EU AI Act", SourceReference: "Article 12"}},
	}}
	var jsonOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, value); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 17 || len(decoded.DeveloperActions) != 1 || decoded.DeveloperActions[0].ID != "objective/system/control" || decoded.Scan.Repository == nil || decoded.Scan.Repository.Commit != "abcdef" || decoded.Scan.ConfigDigest != "config-digest" || len(decoded.Scan.FrameworkPacks) != 1 {
		t.Fatalf("decoded report = %#v", decoded)
	}
	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"## Developer actions", "Add structured logging", "**Done when:** A cited test verifies the event.", "**Compliance context:** EU AI Act — Article 12"} {
		if !strings.Contains(markdown.String(), fragment) {
			t.Fatalf("Markdown missing %q:\n%s", fragment, markdown.String())
		}
	}
	for _, internalDetail := range []string{"**Status:**", "**Action ID:**", "objective/system/control"} {
		if strings.Contains(markdown.String(), internalDetail) {
			t.Fatalf("presentation-oriented Markdown exposed internal lifecycle detail %q:\n%s", internalDetail, markdown.String())
		}
	}
}

func TestDeveloperReportSeparatesDeveloperWorkFromComplianceHandoff(t *testing.T) {
	value := New(".", "test", nil, nil, 0)
	value.DeveloperActions = []DeveloperAction{
		{
			ID: "objective/logging", Status: DeveloperActionNew, Priority: "Review", Category: "code-control",
			Title: "Add event logging", RecommendedChange: "Add structured logging.",
			AcceptanceCriteria: []string{"A test verifies the required event."}, Evidence: []DeveloperActionEvidence{{Path: "app.go", StartLine: 8}},
		},
		{
			ID: "context/operator", Status: DeveloperActionOpen, Priority: "Review", Category: "human-context",
			Title: "Confirm the operating organisation", RecommendedChange: "Record which organisation operates this system.", HumanOnly: true,
		},
	}

	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	text := markdown.String()
	for _, fragment := range []string{
		"**1 developer action. No direct code risks found.**",
		"## Developer actions", "### 1. Add event logging",
		"## Needs product or compliance input", "**Confirm the operating organisation:** Record which organisation operates this system.",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("Markdown missing %q:\n%s", fragment, text)
		}
	}
	developerSection := text[strings.Index(text, "## Developer actions"):strings.Index(text, "## AI functionality found")]
	if strings.Contains(developerSection, "operating organisation") {
		t.Fatalf("human-only handoff was presented as developer work:\n%s", developerSection)
	}
}

func TestDeveloperActionContractRemovesPipelineJargonFromCachedObservations(t *testing.T) {
	value := New(".", "test", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted},
		Result: providers.RepositorySectionResult{
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
				ObjectiveID: "eu-aia-15-performance-thresholds",
				Strength:    providers.StrengthUncertain,
				Confidence:  "low",
				Rationale:   "Validated source batches returned differing code-level assessments; the combined result remains uncertain.",
				SupportingEvidence: []providers.RepositoryCitation{{
					Path: "benchmark.go", Line: 147,
				}},
			}},
		},
	}

	actions := CurrentDeveloperActions(value)
	if len(actions) != 1 {
		t.Fatalf("actions = %#v", actions)
	}
	action := actions[0]
	if strings.Contains(strings.ToLower(action.Why), "source batch") || !strings.Contains(action.Why, "cited code supports conflicting conclusions") {
		t.Fatalf("action why is not developer-facing: %q", action.Why)
	}
	if !strings.Contains(action.RecommendedChange, "end-to-end test or enforcement path") {
		t.Fatalf("action change is not concrete: %q", action.RecommendedChange)
	}
}

func TestSARIFIncludesNewLocatedCodeAction(t *testing.T) {
	value := New(".", "test", nil, nil, 0)
	value.DeveloperActions = []DeveloperAction{{
		ID: "objective/system/control", Status: DeveloperActionNew, Priority: "Review", Category: "code-control",
		Title: "Add an AI event log", RecommendedChange: "Add structured logging.",
		Evidence: []DeveloperActionEvidence{{Path: "app.go", StartLine: 8}},
	}}
	var output bytes.Buffer
	if err := WriteSARIF(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"id": "COMPLYSCAN-ACTION"`, `"actionId": "objective/system/control"`, `"uri": "app.go"`} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("SARIF missing %q:\n%s", fragment, output.String())
		}
	}
}
