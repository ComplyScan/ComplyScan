package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/usemapping"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

func TestSeverityFilteringAndThreshold(t *testing.T) {
	findings := []rules.Finding{
		{RuleID: "INFO", Severity: rules.SeverityInfo},
		{RuleID: "MED", Severity: rules.SeverityMedium},
		{RuleID: "HIGH", Severity: rules.SeverityHigh},
	}
	filtered := FilterByMinimum(findings, rules.SeverityMedium)
	if len(filtered) != 2 || filtered[0].RuleID != "MED" || filtered[1].RuleID != "HIGH" {
		t.Fatalf("unexpected filtered findings: %#v", filtered)
	}
	if !MeetsThreshold(findings, rules.SeverityHigh) {
		t.Fatal("high finding should meet high threshold")
	}
	if MeetsThreshold(findings, rules.SeverityCritical) {
		t.Fatal("findings should not meet critical threshold")
	}
}

func TestWriteJSON(t *testing.T) {
	value := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "0.1.0", Commit: "abc123", BuiltAt: "today"}, ScanScope{
		Findings: "changed-files", TechnicalEvidence: "full-repository", ChangedSince: "main", TrackedOnly: true,
	}, time.Date(2026, 8, 3, 10, 30, 0, 0, time.FixedZone("test", 2*60*60)), []rules.Finding{{
		RuleID: "AI-DOC-001", Title: "Missing docs", Severity: rules.SeverityMedium,
	}}, nil, 2)
	value.AIUseInventory = &aiuse.Snapshot{
		Summary: aiuse.SnapshotSummary{Confirmed: 1},
		Confirmed: []aiuse.ObservedUse{{
			Use: aiuse.Use{ID: "assistant", Name: "Assistant"},
			RepositoryFacts: &aiuse.FactReview{
				Status: aiuse.FactReviewModelReviewed, ModelCoverage: aiuse.FactCoverageFullRepository,
				Facts: []aiuse.Fact{{
					Field: profile.CodeFactAIActivities, Values: []string{"inference"}, Confidence: "high",
					Source: aiuse.FactSourceModel, Coverage: aiuse.FactCoverageFullRepository, Strength: aiuse.FactStrengthModelReasoned,
					Rationale: "The handler invokes a model.", Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12, Summary: "Model invocation"}},
				}},
			},
		}},
	}
	value.AIUseMappings = &usemapping.Report{SchemaVersion: 1, Summary: usemapping.Summary{
		Uses: 1, FrameworkSystemContexts: 1, WithInScopeCodeEvidence: 1, ObjectivesWithEvidenceOutsideUse: 1,
		LikelyRequiredWithoutInScopeEvidence: 1, RecommendedWithoutInScopeEvidence: 1,
	}, Uses: []usemapping.UseResult{{UseID: "assistant", UseName: "Assistant"}}}
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{Coverage: providers.RepositoryCoverage{
		Mode: providers.RepositoryAnalysisTargeted, Subsystems: 2, SourceBatchesCompleted: 2, SourceBatchesTotal: 2,
	}, Result: providers.RepositorySectionResult{
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
			ObjectiveID: "pack/human-review", AIUseID: "assistant", SystemID: "assistant",
			Strength: providers.StrengthPartial, Confidence: "high", Rationale: "A review path is present but incomplete.",
		}},
	}}
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 11 || decoded.Tool.Name != "ComplyScan" || decoded.Tool.Version != "0.1.0" || decoded.Tool.Commit != "abc123" {
		t.Fatalf("unexpected tool: %#v", decoded.Tool)
	}
	if decoded.RepositoryAnalysisRun != RepositoryAnalysisCompleted {
		t.Fatalf("repository analysis run = %q", decoded.RepositoryAnalysisRun)
	}
	if decoded.AIUseInventory == nil || decoded.AIUseInventory.Summary.Confirmed != 1 {
		t.Fatalf("AI-use inventory was not serialized: %#v", decoded.AIUseInventory)
	}
	if len(decoded.AIUseInventory.Confirmed) != 1 || decoded.AIUseInventory.Confirmed[0].RepositoryFacts == nil ||
		len(decoded.AIUseInventory.Confirmed[0].RepositoryFacts.Facts) != 1 ||
		decoded.AIUseInventory.Confirmed[0].RepositoryFacts.Facts[0].Field != profile.CodeFactAIActivities {
		t.Fatalf("per-use repository facts were not serialized: %#v", decoded.AIUseInventory.Confirmed)
	}
	if decoded.AIUseMappings == nil || decoded.AIUseMappings.SchemaVersion != 1 || decoded.AIUseMappings.Summary.Uses != 1 {
		t.Fatalf("AI-use mappings were not serialized: %#v", decoded.AIUseMappings)
	}
	if decoded.RepositoryAnalysis == nil || len(decoded.RepositoryAnalysis.Result.ObjectiveObservations) != 1 ||
		decoded.RepositoryAnalysis.Result.ObjectiveObservations[0].AIUseID != "assistant" {
		t.Fatalf("use-scoped repository observation was not serialized: %#v", decoded.RepositoryAnalysis)
	}
	if decoded.RepositoryAnalysis.Coverage.SourceBatchesCompleted != 2 || decoded.RepositoryAnalysis.Coverage.SourceBatchesTotal != 2 {
		t.Fatalf("schema-version 11 batch coverage was not serialized: %#v", decoded.RepositoryAnalysis.Coverage)
	}
	if !strings.Contains(output.String(), `"ai_use_id": "assistant"`) {
		t.Fatalf("schema-version 11 AI-use attribution is missing:\n%s", output.String())
	}
	if !strings.Contains(output.String(), `"framework_system_contexts": 1`) ||
		!strings.Contains(output.String(), `"with_in_scope_code_evidence": 1`) ||
		!strings.Contains(output.String(), `"objectives_with_evidence_outside_use": 1`) ||
		!strings.Contains(output.String(), `"likely_required_without_in_scope_code_evidence": 1`) ||
		!strings.Contains(output.String(), `"recommended_without_in_scope_code_evidence": 1`) ||
		strings.Contains(output.String(), `"required_without_code_evidence"`) {
		t.Fatalf("per-use summary schema is ambiguous:\n%s", output.String())
	}
	if !strings.HasPrefix(decoded.Scan.ID, "scan-") || decoded.Scan.CreatedAt != "2026-08-03T08:30:00Z" || decoded.Scan.Scope.Findings != "changed-files" || decoded.Scan.Scope.TechnicalEvidence != "full-repository" {
		t.Fatalf("unexpected scan metadata: %#v", decoded.Scan)
	}
	if decoded.Summary.Total != 1 || decoded.Summary.Medium != 1 {
		t.Fatalf("unexpected summary: %#v", decoded.Summary)
	}
	if decoded.Findings == nil || len(decoded.Findings) != 1 {
		t.Fatalf("unexpected findings: %#v", decoded.Findings)
	}
	if decoded.Suppressed != 2 {
		t.Fatalf("suppressed = %d", decoded.Suppressed)
	}
}

func TestWriteJSONDefaultsSchemaSevenRepositoryAnalysisLifecycle(t *testing.T) {
	value := Report{SchemaVersion: 7}
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RepositoryAnalysisRun != RepositoryAnalysisNotRequested {
		t.Fatalf("repository analysis run = %q", decoded.RepositoryAnalysisRun)
	}
}

func TestIncompleteRepositoryAnalysisWithPartialCoverageIsNotReportedAsCompleted(t *testing.T) {
	value := New(".", "test", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisIncomplete
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 20, FilesSubmitted: 4, Subsystems: 1,
			SourceBatchesCompleted: 1, SourceBatchesTotal: 3,
		},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
		},
	}
	if label := developerAnalysisSummaryLabel(value); !strings.Contains(label, "incomplete") {
		t.Fatalf("analysis summary = %q, want incomplete", label)
	}
	if label := developerRepositoryAnalysisLabel(value); !strings.Contains(label, "incomplete after 1 of 3 bounded code batch") {
		t.Fatalf("repository analysis label = %q", label)
	}
	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	var detailed bytes.Buffer
	if err := WriteDetailedMarkdown(&detailed, value); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"concise": concise.String(), "detailed": detailed.String()} {
		lowerOutput := strings.ToLower(output)
		if !strings.Contains(lowerOutput, "1 of 3 bounded source batch") || !strings.Contains(lowerOutput, "no unsynthesized model conclusions") {
			t.Fatalf("%s incomplete report lacks truthful partial coverage:\n%s", name, output)
		}
		if strings.Contains(output, "No AI implementation was identified") || strings.Contains(output, "did not suggest a specific AI use") {
			t.Fatalf("%s incomplete report reads as a completed negative:\n%s", name, output)
		}
	}
}

func TestWriteJSONUsesCurrentEvidenceInvestigationContract(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.TechnicalReview = &providers.TechnicalReviewResult{
		Provider: providers.Ollama, Model: "qwen3:8b", InputCandidates: 1, Reviewed: 1,
		Observations: []providers.TechnicalObservation{{
			SystemID: "ranking", OwnershipScope: "explicit", RepositoryFiles: 42,
			ObjectiveID: "objective", EvidenceFingerprint: strings.Repeat("a", 64),
			Conclusion: providers.ConclusionNotFoundAfterInvestigation, Assurance: providers.AssuranceInvestigationNoEvidence,
			Strength: providers.StrengthNotSupported, Confidence: "medium", Rationale: "No evidence in the bounded search.",
		}},
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"schema_version": 11`) || !strings.Contains(output.String(), `"repository_analysis_run": "not-requested"`) || !strings.Contains(output.String(), `"evidence_investigation"`) || !strings.Contains(output.String(), `"system_id": "ranking"`) || !strings.Contains(output.String(), `"repository_files": 42`) || strings.Contains(output.String(), `"technical_review"`) {
		t.Fatalf("unexpected current-schema investigation JSON:\n%s", output.String())
	}
}

func TestConciseMarkdownSeparatesSavedSuggestedAndUngroupedAIUses(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.AIUseInventory = &aiuse.Snapshot{
		Summary: aiuse.SnapshotSummary{Confirmed: 1, Draft: 1, Retired: 1, Suggested: 1, UngroupedSignals: 1},
		Confirmed: []aiuse.ObservedUse{{
			Use: aiuse.Use{
				ID: "confirmed", Name: "Confirmed generation", Description: "Generates summaries.", Paths: []string{"runtime/**"},
			},
			RepositoryFacts: &aiuse.FactReview{
				Status: aiuse.FactReviewModelReviewed, DeterministicCoverage: aiuse.FactCoverageFullRepository, ModelCoverage: aiuse.FactCoverageChangedAndConnected,
				ModelProviders: []aiuse.ModelProviderObservation{{
					Name: "OpenAI", Confidence: "high", Source: aiuse.FactSourceDeterministic, Coverage: aiuse.FactCoverageFullRepository,
					Strength: aiuse.FactStrengthDirectSignal, Evidence: []providers.RepositoryCitation{{Path: "runtime/client.go", Line: 4, Summary: "OpenAI client"}},
				}},
				Facts: []aiuse.Fact{{
					Field: profile.CodeFactHumanOversight, Values: []string{"required"}, Confidence: "high",
					Source: aiuse.FactSourceModel, Coverage: aiuse.FactCoverageChangedAndConnected, Strength: aiuse.FactStrengthModelReasoned,
					Rationale: "The route calls an approval gate.", Evidence: []providers.RepositoryCitation{{Path: "runtime/review.go", Line: 15, Summary: "Approval gate"}},
				}},
				UnresolvedQuestions: []string{"Whether every route uses the approval gate"},
			},
			RoleCandidates: []aiuse.RoleCandidate{{
				Role: aiuse.TechnicalRoleDeployer, Status: aiuse.RoleCandidatePossible, Confidence: "medium",
				Source: aiuse.FactSourceDeterministic, Coverage: aiuse.FactCoverageFullRepository, Strength: aiuse.FactStrengthDirectSignal,
				Rationale:                "Runtime code connects to a third-party model provider.",
				MissingOrganizationFacts: []string{"Whether the organisation actually operates this system"},
			}},
		}},
		Draft: []aiuse.ObservedUse{{Use: aiuse.Use{
			ID: "draft", Name: "Draft ranking", Description: "Ranks candidates.", Paths: []string{"ranking/**"},
		}}},
		Retired: []aiuse.ObservedUse{{Use: aiuse.Use{
			ID: "retired", Name: "Retired classifier", Description: "Previously classified documents.", Paths: []string{"legacy/**"},
		}, Observation: aiuse.ObservationTechnicalSignal}},
		Suggested: []aiuse.Suggestion{{
			Name: "Suggested assistant", Purpose: "May answer questions.", Confidence: "medium",
			Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12}},
		}},
		UngroupedSignals: []aiuse.SignalLocation{{
			Component: "OpenAI", Kind: inventory.KindProvider, Path: "unowned.py", Line: 4, Scope: inventory.ScopeRuntime,
		}},
		OrganizationUnknowns: []string{"Actual operating regions cannot be established from repository code."},
	}

	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Organising code into confirmed AI uses is optional", "you can act on this report without doing it",
		"#### Confirmed AI-use scopes", "Confirmed generation", "#### Draft AI-use scopes (optional)", "Draft ranking",
		"#### Retired AI uses", "Retired classifier", "Current signals match retired AI use", "Matching local technical signal found", "No matching signal was observed in this scan",
		"#### Likely AI uses found by the code review", "Suggested assistant", "**Other AI-related code signals (1):** OpenAI at unowned.py:4",
		"##### What code indicates for Confirmed generation", "Model provider integration", "Human control", "required (high confidence)",
		"The route calls an approval gate.",
		"Possible roles indicated by the repository", "Deployer", "Organisation context that repository code cannot establish",
		"These unknowns are report context only. They do not create a setup task or block the code scan.",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("concise Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "AI features found") {
		t.Fatalf("concise Markdown retained misleading feature count:\n%s", output.String())
	}
	for _, unwanted := range []string{"draft AI-use record still needs confirmation", "AI-use suggestion needs a developer decision", "Run `complyscan ai-uses setup`"} {
		if strings.Contains(output.String(), unwanted) {
			t.Fatalf("optional AI-use enrichment was presented as required work %q:\n%s", unwanted, output.String())
		}
	}
}

func TestConciseMarkdownExplainsEmptyPerUseFactReview(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.AIUseInventory = &aiuse.Snapshot{
		Summary: aiuse.SnapshotSummary{Confirmed: 1},
		Confirmed: []aiuse.ObservedUse{{
			Use: aiuse.Use{ID: "assistant", Name: "Assistant", Paths: []string{"assistant/**"}},
			RepositoryFacts: &aiuse.FactReview{
				Status: aiuse.FactReviewModelReviewed, ModelCoverage: aiuse.FactCoverageChangedAndConnected, Facts: []aiuse.Fact{},
			},
		}},
	}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "The reviewed code supported no positive per-use facts") ||
		!strings.Contains(output.String(), "does not establish that a fact is false") {
		t.Fatalf("empty fact review was not explained honestly:\n%s", output.String())
	}
}

func TestConciseMarkdownOptionalAIUseGroupingDoesNotChangeCleanOutcome(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted},
		Result: providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{{
			ID: "suggested", Name: "Suggested assistant", Purpose: "Drafts replies.",
			Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12}},
		}}},
	}
	value.AIUseInventory = &aiuse.Snapshot{
		Summary: aiuse.SnapshotSummary{Draft: 1, Suggested: 1, UngroupedSignals: 1},
		Draft: []aiuse.ObservedUse{{Use: aiuse.Use{
			ID: "draft", Name: "Draft assistant", Paths: []string{"assistant/**"},
		}}},
		Suggested: []aiuse.Suggestion{{
			Name: "Suggested assistant", Purpose: "Drafts replies.", Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12}},
		}},
		UngroupedSignals: []aiuse.SignalLocation{{Component: "OpenAI", Path: "client.go", Line: 4}},
	}

	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "**No urgent code problems found**") {
		t.Fatalf("optional AI-use organization changed the scan outcome:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "AI-reviewed safeguards: **no code-level decisions returned**") {
		t.Fatalf("empty completed review was not described honestly:\n%s", output.String())
	}
	if strings.Contains(output.String(), "| **Review** |") {
		t.Fatalf("optional AI-use organization produced a required action:\n%s", output.String())
	}
}

func TestConciseMarkdownDoesNotPresentUnchangedUseAsReviewed(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.AIUseInventory = &aiuse.Snapshot{
		ChangedScope: true,
		Summary:      aiuse.SnapshotSummary{Confirmed: 1},
		Confirmed: []aiuse.ObservedUse{{Use: aiuse.Use{
			ID: "unchanged", Name: "Unchanged assistant", Description: "Lives outside this pull request.", Paths: []string{"stable/**"},
		}, Observation: aiuse.ObservationNotReviewed}},
	}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Not reviewed in this change-focused run") {
		t.Fatalf("changed-scope limitation missing:\n%s", output.String())
	}
}

func TestConciseMarkdownAndTerminalMapRequirementsPerConfirmedAIUse(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.AIUseMappings = &usemapping.Report{
		SchemaVersion: 1,
		Summary:       usemapping.Summary{Uses: 2, FrameworkSystemContexts: 2, LikelyRequired: 1, Unresolved: 1, WithInScopeCodeEvidence: 1, LikelyRequiredWithoutInScopeEvidence: 1},
		Uses: []usemapping.UseResult{
			{
				UseID: "support-chat", UseName: "Support answer generation", Description: "Drafts support replies.", Paths: []string{"apps/support/**"},
				Summary: usemapping.Summary{Uses: 1, FrameworkSystemContexts: 1, LikelyRequired: 1, WithInScopeCodeEvidence: 1},
				Frameworks: []usemapping.FrameworkResult{{ID: "eu-ai-act", Name: "EU AI Act", Contexts: []usemapping.ContextResult{{
					Association: usemapping.Association{Status: usemapping.AssociationConfigured, SystemID: "support", SystemName: "Support"},
					Objectives: []usemapping.ObjectiveResult{{
						ObjectiveResult: reconciliation.ObjectiveResult{
							ObjectiveID: "eu-aia-14-human-review-gate", Title: "Human review gate", SourceReference: "Article 14",
							Requirement: reconciliation.RequirementLikelyRequired, Evidence: framework.ObjectiveCandidate,
							Mapping:            reconciliation.MappingRequirementWithEvidence,
							EvidenceReferences: []reconciliation.EvidenceReference{{Path: "apps/support/review.go", Line: 12}},
						},
						AIReview: &usemapping.CodeReview{
							RepositoryObjectiveID: "eu-ai-act-pack/eu-aia-14-human-review-gate", SystemID: "support",
							Verdict: providers.RepositoryVerdictPartial, Strength: providers.StrengthPartial, Confidence: "high",
							SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/review.go", Line: 12}}, Attribution: usemapping.ReviewAttributionMatchingCitations,
						},
					}},
				}}}},
			},
			{
				UseID: "unlinked", UseName: "Unlinked classifier", Paths: []string{"classifier/**"},
				Summary: usemapping.Summary{Uses: 1, FrameworkSystemContexts: 1, Unresolved: 1, UnassociatedUses: 1},
				Frameworks: []usemapping.FrameworkResult{{ID: "eu-ai-act", Name: "EU AI Act", Contexts: []usemapping.ContextResult{{
					Association: usemapping.Association{Status: usemapping.AssociationNone, Message: "No system association."},
					Objectives: []usemapping.ObjectiveResult{{ObjectiveResult: reconciliation.ObjectiveResult{
						ObjectiveID: "eu-aia-15-safe-stop", Title: "Safe stop", SourceReference: "Article 15",
						Requirement: reconciliation.RequirementUnresolved, Evidence: framework.ObjectiveNotDetected, Mapping: reconciliation.MappingApplicabilityUnresolved,
					}}},
				}}}},
			},
		},
	}
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{
		{
			ObjectiveID: "eu-ai-act-pack/eu-aia-14-human-review-gate", SystemID: "support", Strength: providers.StrengthPartial, Confidence: "high",
			Rationale: "The gate covers only one path.", SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/review.go", Line: 12}},
			UnresolvedQuestions: []string{"Is human review enforced?"},
		},
		{
			ObjectiveID: "eu-ai-act-pack/eu-aia-15-safe-stop", Strength: providers.StrengthStrong, Confidence: "high",
			Rationale: "The repository-level implementation is visible but overlaps use scopes.", SupportingEvidence: []providers.RepositoryCitation{{Path: "shared/stop.go", Line: 7}},
		},
	}}}
	value.AIUseMappings.Uses[0].Frameworks[0].Contexts[0].Objectives[0].AIReview.UnresolvedQuestions = []string{"Is human review enforced?"}

	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"### Requirements and code evidence by confirmed AI use", "Support answer generation", "Support (support)",
		"Human review gate", "Likely required from declared context", "Partially implemented in the reviewed code", "apps/support/review.go:12",
		"Unlinked classifier", "No configured system association", "Cannot determine from saved context",
		"AI use has no configured system context", "Which configured system contains the AI use Unlinked classifier?",
		"Repository-level, not assigned to one confirmed AI use: Safe stop", "shared/stop.go:7",
		"Support answer generation: Is human review enforced?",
	} {
		if !strings.Contains(markdown.String(), expected) {
			t.Errorf("per-use Markdown missing %q:\n%s", expected, markdown.String())
		}
	}
	if strings.Count(markdown.String(), "Is human review enforced?") != 1 {
		t.Fatalf("covered repository question was duplicated:\n%s", markdown.String())
	}
	if strings.Contains(markdown.String(), "| **Review** | Human review gate |") {
		t.Fatalf("covered repository observation produced a duplicate generic action:\n%s", markdown.String())
	}
	if strings.Contains(markdown.String(), "### Code-level safeguard decisions") || !strings.Contains(markdown.String(), "### Other code-level evidence") {
		t.Fatalf("per-use decisions were duplicated in the generic evidence section:\n%s", markdown.String())
	}
	var terminal bytes.Buffer
	if err := WriteTerminalConciseCompletion(&terminal, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "Per-use safeguard detail (optional scope refinement): 2 confirmed AI use(s), 2 framework/system context(s)") ||
		!strings.Contains(terminal.String(), "Analysis: deterministic checks + completed AI code review") ||
		!strings.Contains(terminal.String(), "Support answer generation: 1 likely-required check(s)") ||
		!strings.Contains(terminal.String(), "Unlinked classifier: 0 likely-required check(s)") ||
		!strings.Contains(terminal.String(), "no system association (context needed)") {
		t.Fatalf("per-use terminal summary missing:\n%s", terminal.String())
	}
}

func TestDeveloperQuestionsKeepDirectUseObservationsSeparate(t *testing.T) {
	value := Report{
		AIUseMappings: &usemapping.Report{Uses: []usemapping.UseResult{{
			UseID: "summarization", UseName: "Summarization",
			Frameworks: []usemapping.FrameworkResult{{Contexts: []usemapping.ContextResult{{Objectives: []usemapping.ObjectiveResult{{
				AIReview: &usemapping.CodeReview{
					RepositoryObjectiveID: "pack/review", SystemID: "assistant", Attribution: usemapping.ReviewAttributionExplicitUse,
					UnresolvedQuestions: []string{"Can operators bypass the gate?"},
				},
			}}}}}},
		}}},
		RepositoryAnalysis: &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{
			{ObjectiveID: "pack/review", AIUseID: "summarization", SystemID: "assistant", UnresolvedQuestions: []string{"Can operators bypass the gate?"}},
			{ObjectiveID: "pack/review", AIUseID: "classification", SystemID: "assistant", UnresolvedQuestions: []string{"Does classification use the same gate?"}},
		}}},
	}
	questions, _ := developerQuestions(value)
	joined := strings.Join(questions, "\n")
	if strings.Count(joined, "Can operators bypass the gate?") != 1 || !strings.Contains(joined, "Summarization: Can operators bypass the gate?") {
		t.Fatalf("covered use question was not deduplicated correctly: %q", joined)
	}
	if !strings.Contains(joined, "Does classification use the same gate?") {
		t.Fatalf("different direct-use observation was hidden: %q", joined)
	}
}

func TestVoluntaryFrameworkRecommendationDoesNotRequireSystemAssociation(t *testing.T) {
	context := usemapping.ContextResult{
		Association: usemapping.Association{Status: usemapping.AssociationNone},
		Objectives: []usemapping.ObjectiveResult{{ObjectiveResult: reconciliation.ObjectiveResult{
			ObjectiveID: "practice", Requirement: reconciliation.RequirementRecommended,
			Mapping: reconciliation.MappingRecommendedWithoutEvidence,
		}}},
	}
	if developerUseContextNeedsAssociation(context) {
		t.Fatal("framework-wide voluntary recommendation incorrectly required a system association")
	}
}

func TestPerUseReportOmitsEmptyDuplicateEvidenceSection(t *testing.T) {
	var output bytes.Buffer
	if err := writeDeveloperEvidenceMarkdown(&output, developerReportView{hasPerUseMappings: true}); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("empty duplicate evidence section = %q", output.String())
	}
}

func TestPerUseActionsRespectApplicabilityBeforeAIImplementationVerdict(t *testing.T) {
	use := usemapping.UseResult{UseID: "assistant", UseName: "Assistant"}
	association := usemapping.Association{Status: usemapping.AssociationConfigured, SystemID: "assistant", SystemName: "Assistant"}
	objective := usemapping.ObjectiveResult{
		ObjectiveResult: reconciliation.ObjectiveResult{
			ObjectiveID: "control", Title: "Human review gate",
			Requirement: reconciliation.RequirementNotCurrentlyIndicated,
			Mapping:     reconciliation.MappingNotCurrentlyIndicated,
		},
		AIReview: &usemapping.CodeReview{Verdict: providers.RepositoryVerdictNotImplemented},
	}
	if action, include := developerUseObjectiveAction(use, association, objective); include {
		t.Fatalf("non-indicated safeguard became an implementation action: %#v", action)
	}

	objective.Requirement = reconciliation.RequirementRecommended
	objective.Mapping = reconciliation.MappingRecommendedWithEvidence
	action, include := developerUseObjectiveAction(use, association, objective)
	if !include || action.priority != "Review" || !strings.Contains(action.why, "voluntary framework") {
		t.Fatalf("recommended safeguard action = %#v, %v", action, include)
	}

	objective.Requirement = reconciliation.RequirementLikelyRequired
	action, include = developerUseObjectiveAction(use, association, objective)
	if !include || action.priority != "High" {
		t.Fatalf("likely-required safeguard action = %#v, %v", action, include)
	}

	objective.Mapping = reconciliation.MappingUnableToEvaluate
	objective.AIReview.Verdict = providers.RepositoryVerdictImplemented
	if action, include = developerUseObjectiveAction(use, association, objective); include {
		t.Fatalf("implemented use-scoped review was overridden by deterministic coverage warning: %#v", action)
	}
	objective.AIReview = nil
	if action, include = developerUseObjectiveAction(use, association, objective); !include || action.priority != "Review" {
		t.Fatalf("unreviewed unsupported code did not remain visible: %#v, %v", action, include)
	}

	objective.Mapping = reconciliation.MappingRequirementWithoutEvidence
	objective.EvidenceOutsideUse = []usemapping.EvidenceLocation{{Path: "shared/review.go", Line: 9}}
	action, include = developerUseObjectiveAction(use, association, objective)
	if !include || !strings.Contains(action.why, "elsewhere in the repository") || !strings.Contains(action.next, "add that path") || !strings.Contains(action.evidence, "Outside saved paths: shared/review.go:9") {
		t.Fatalf("outside-scope safeguard action = %#v, %v", action, include)
	}

	objective.EvidenceOutsideUse = nil
	objective.Mapping = reconciliation.MappingRequirementWithEvidence
	objective.Investigation = &reconciliation.ObjectiveInvestigation{Conclusion: providers.ConclusionSubstantiated, Assurance: providers.AssuranceAISubstantiated}
	if action, include = developerUseObjectiveAction(use, association, objective); include {
		t.Fatalf("substantiated bounded investigation became an action: %#v", action)
	}
	if result := developerUseCodeResult(objective); !strings.Contains(result, "substantiated by the bounded AI evidence review") {
		t.Fatalf("bounded investigation result = %q", result)
	}
	objective.Investigation.Conclusion = providers.ConclusionNotFoundAfterInvestigation
	action, include = developerUseObjectiveAction(use, association, objective)
	if !include || action.priority != "High" || !strings.Contains(action.why, "No implementation was found") {
		t.Fatalf("negative bounded investigation action = %#v, %v", action, include)
	}

	value := Report{AIUseMappings: &usemapping.Report{Uses: []usemapping.UseResult{{
		UseID: "assistant", Frameworks: []usemapping.FrameworkResult{{ID: "eu-ai-act", Contexts: []usemapping.ContextResult{{Objectives: []usemapping.ObjectiveResult{objective}}}}},
	}}}}
	implemented, partial, notImplemented, unclear := developerTechnicalVerdictCounts(value)
	if implemented != 0 || partial != 0 || notImplemented != 1 || unclear != 0 {
		t.Fatalf("bounded verdict counts = %d/%d/%d/%d", implemented, partial, notImplemented, unclear)
	}
}

func TestRepositoryAnalysisIsRenderedAsAdvisoryEvidence(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Coverage:           providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 20, FilesSubmitted: 2, CitationsChecked: 1},
		FollowUpRequested:  true,
		FollowUpExcerpts:   1,
		OutputRecoveryUsed: true,
		Usage:              providers.Usage{PromptTokens: 200, CompletionTokens: 100, ReasoningTokens: 40},
		Result: providers.RepositorySectionResult{Scope: ".", AIUses: []providers.RepositoryAIUse{{
			ID: "summaries", Name: "Summary generation", Purpose: "Generate summaries", Confidence: "high",
			Evidence: []providers.RepositoryCitation{{Path: "main.go", Line: 12, Summary: "Runtime model call"}},
		}}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
	}
	var terminal bytes.Buffer
	if err := WriteTerminalCompletion(&terminal, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "Repository AI code analysis") || !strings.Contains(terminal.String(), "2 file-excerpt submission(s) from 20 discovered file(s)") || !strings.Contains(terminal.String(), "Follow-up: 1 bounded excerpt(s)") || !strings.Contains(terminal.String(), "Recovery: the initial output limit was reached") || !strings.Contains(terminal.String(), "Tokens: 200 input, 100 output (40 reasoning)") || !strings.Contains(terminal.String(), "main.go:12") {
		t.Fatalf("repository analysis missing from terminal:\n%s", terminal.String())
	}
	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "## 2. What ComplyScan found") || !strings.Contains(markdown.String(), "Summary generation") || !strings.Contains(markdown.String(), "cannot decide on its own whether your product complies with a law") {
		t.Fatalf("repository analysis boundary missing from Markdown:\n%s", markdown.String())
	}
	var detailed bytes.Buffer
	if err := WriteDetailedMarkdown(&detailed, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detailed.String(), "source-content byte(s)") {
		t.Fatalf("detailed report presents source bytes as total external-transfer bytes:\n%s", detailed.String())
	}
}

func TestChangedReviewCoverageExplainsModelBoundary(t *testing.T) {
	value := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "dev"}, ScanScope{
		Findings: "changed-files", TechnicalEvidence: "full-repository", AIInventory: "full-repository", Reconciliation: "full-repository",
		ChangedSince: "main", AIReview: string(providers.RepositoryReviewScopeChanged), AIReviewFiles: 3, AIReviewChangedFiles: 1, AIReviewConnectedFiles: 2,
	}, time.Now(), nil, nil, 0)
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model", CacheHit: true,
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, ReviewScope: providers.RepositoryReviewScopeChanged,
			RepositoryFiles: 50, ScopeFiles: 3, ChangedFiles: 1, ConnectedFiles: 2, FilesSubmitted: 2,
		},
		Result: providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
	}

	var terminal bytes.Buffer
	if err := WriteTerminalRepositoryAnalysis(&terminal, *value.RepositoryAnalysis); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"reused matching private cache", "Cached reviewed context: 2 original file-excerpt submission(s); current run transferred no source", "1 changed eligible + 2 connected file(s)", "full 50-file repository governance remained local"} {
		if !strings.Contains(terminal.String(), expected) {
			t.Errorf("terminal changed scope missing %q:\n%s", expected, terminal.String())
		}
	}

	var markdown bytes.Buffer
	if err := WriteDetailedMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"AI review scope: changed-plus-connected", "matching private cache entry", "1 changed eligible file(s) plus 2 connected file(s)", "Changed-code review boundary"} {
		if !strings.Contains(markdown.String(), expected) {
			t.Errorf("Markdown changed scope missing %q:\n%s", expected, markdown.String())
		}
	}
	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(concise.String(), "reused private cache") {
		t.Fatalf("concise report omitted repository cache reuse:\n%s", concise.String())
	}
}

func TestNoStructuralCandidateDoesNotReadAsModelDerivedZeroUse(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 20},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
			UnresolvedQuestions: []string{"No eligible structural candidate was selected."},
		},
	}

	var terminal bytes.Buffer
	if err := WriteTerminalCompletion(&terminal, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "no eligible structural candidate; no source was sent") {
		t.Fatalf("terminal no-candidate boundary is unclear:\n%s", terminal.String())
	}

	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"no structural AI code candidate selected", "no source review run", "no model-derived zero-use conclusion was made"} {
		if !strings.Contains(concise.String(), expected) {
			t.Errorf("concise no-candidate report missing %q:\n%s", expected, concise.String())
		}
	}
	if strings.Contains(concise.String(), "did not suggest a specific AI use") {
		t.Fatalf("no-candidate report falsely describes a completed model review:\n%s", concise.String())
	}

	var detailed bytes.Buffer
	if err := WriteDetailedMarkdown(&detailed, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detailed.String(), "No repository-analysis model pass was run") || strings.Contains(detailed.String(), "No AI implementation was identified by this model pass") {
		t.Fatalf("detailed no-candidate report implies a model-derived zero-use result:\n%s", detailed.String())
	}
}

func TestExecutionVerificationIsRenderedWithoutComplianceClaim(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.ExecutionVerifications = []verification.Report{{
		RecipeID: "go-tests", Status: verification.StatusPassed, Runtime: "docker", Image: "golang:local",
		Command: []string{"go", "test", "./..."}, Objectives: []string{"objective"},
		ExitCode: 0, DurationMS: 123, OutputDigest: strings.Repeat("d", 64), Output: "ok",
		Boundary: "Passing does not establish compliance.",
	}}
	var terminal bytes.Buffer
	if err := WriteTerminalCompletion(&terminal, value); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Isolated execution verification go-tests: PASSED", "Passing does not establish compliance."} {
		if !strings.Contains(terminal.String(), want) {
			t.Errorf("terminal output missing %q:\n%s", want, terminal.String())
		}
	}
	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "Execution check: go-tests") || !strings.Contains(markdown.String(), "Passing does not establish compliance") || !strings.Contains(markdown.String(), "Isolated execution checks: **1 passed, 0 failed**") {
		t.Fatalf("verification missing from Markdown:\n%s", markdown.String())
	}
	var jsonOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"execution_verification"`) || !strings.Contains(jsonOutput.String(), `"status": "passed"`) {
		t.Fatalf("verification missing from JSON:\n%s", jsonOutput.String())
	}
}

func TestWriteTerminalDoesNotAddColorWhenDisabled(t *testing.T) {
	value := New(".", "0.1.0", []rules.Finding{{
		RuleID: "AI-LOG-001", Title: "Logged prompt", Severity: rules.SeverityHigh,
		Path: "app.py", StartLine: 10, Message: "Review logging.",
	}}, nil, 1)
	var output bytes.Buffer
	if err := WriteTerminal(&output, value, TerminalOptions{Color: false}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatal("unexpected ANSI color sequence")
	}
	for _, want := range []string{"ComplyScan found 1 potential issue", "HIGH", "app.py:10", "Summary: 1 high", "Suppressed: 1 accepted or baselined issue"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestTerminalRendersSelectedFrameworkResultsWithoutLegacyDuplicates(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.Frameworks = []FrameworkResult{{
		ID: "nist-ai-rmf", Name: "NIST AI RMF technical code evidence", Nature: framework.NatureVoluntaryFramework,
		TechnicalEvidence: framework.TechnicalEvidenceReport{
			Pack:   framework.PackReference{ID: framework.NISTAIRMFTechnicalEvidencePackID, Name: "NIST AI RMF technical code evidence", Version: "0.1.0"},
			Source: framework.Source{Reference: "NIST AI 100-1", URL: "https://doi.org/10.6028/NIST.AI.100-1"},
		},
		Reconciliation: reconciliation.Report{MappingVersion: "0.1.0", Summary: reconciliation.Summary{Recommended: 2}},
	}}
	var output bytes.Buffer
	if err := WriteTerminal(&output, value, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Framework: NIST AI RMF", "voluntary-framework", "2 recommended", "Technical evidence: NIST AI RMF"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestWriteTerminalRendersEvidenceOwnership(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.Reconciliation = &reconciliation.Report{
		MappingVersion: "test",
		Ownership: reconciliation.OwnershipReport{
			Configured: true,
			Rules:      []ownership.Rule{{Paths: []string{"shared/**"}, Systems: []string{"ranking", "support"}}},
		},
		Summary: reconciliation.Summary{SharedReferences: 1, ConflictingReferences: 1, UnmappedEvidence: 1},
		Systems: []reconciliation.SystemResult{{
			SystemID: "ranking", SystemName: "Ranking",
			Objectives: []reconciliation.ObjectiveResult{{
				ObjectiveID: "objective", Title: "Human review", SourceReference: "Article 14",
				Mapping: reconciliation.MappingRequirementWithEvidence,
				EvidenceReferences: []reconciliation.EvidenceReference{{
					Path: "shared/review.go", Line: 7, Ownership: ownership.StatusShared, Systems: []string{"ranking", "support"},
				}},
				Investigation: &reconciliation.ObjectiveInvestigation{SystemID: "ranking", OwnershipScope: "explicit", RepositoryFiles: 42},
			}},
		}},
		Unmapped: []reconciliation.UnmappedEvidence{{
			Kind: reconciliation.UnmappedTechnicalObjective, Title: "Logging", Reason: reconciliation.Reason{Code: "conflicting-path-ownership"},
			References: []reconciliation.EvidenceReference{{Path: "overlap/log.go", Line: 4, Ownership: ownership.StatusConflicting, Systems: []string{"ranking", "support"}}},
		}},
	}
	var output bytes.Buffer
	if err := WriteTerminal(&output, value, TerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Path ownership: configured (1 rule(s))",
		"shared/review.go:7 [shared -> ranking, support]",
		"Investigation scope: explicit ownership for system ranking across 42 repository file(s)",
		"Repository evidence with unresolved ownership: 1",
		"overlap/log.go:4 [conflicting -> ranking, support]",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestWriteTerminalCompletion(t *testing.T) {
	value := New(".", "0.1.0", []rules.Finding{{Severity: rules.SeverityMedium}}, nil, 0)
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Scan complete: 1 potential issue", "Summary: 1 medium"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output.String())
		}
	}
}

func TestWriteTerminalConciseCompletionSummarizesWithoutEvidenceDump(t *testing.T) {
	value := New(".", "0.1.5-dev", []rules.Finding{{Severity: rules.SeverityMedium}}, nil, 0)
	value.AIInventory = &inventory.Report{Summary: inventory.Summary{Components: 3, Signals: 12}}
	value.AIUseInventory = &aiuse.Snapshot{Summary: aiuse.SnapshotSummary{Confirmed: 1, Draft: 2, Suggested: 3, UngroupedSignals: 4}}
	value.Frameworks = []FrameworkResult{{
		Applicability: func() *profile.AssessmentReport {
			assessment := profile.AssessEUAIAct([]profile.System{profile.NewDraftSystem("example", "Example")})
			return &assessment
		}(),
		TechnicalEvidence: framework.TechnicalEvidenceReport{Summary: framework.ObjectiveSummary{Total: 7, CandidateEvidence: 3, NotDetected: 4}},
		Reconciliation:    reconciliation.Report{Summary: reconciliation.Summary{LikelyRequired: 5, RequirementWithEvidence: 3, RequirementWithoutEvidence: 2, Unresolved: 1}},
		TechnicalReview:   &providers.TechnicalReviewResult{InputCandidates: 4, Reviewed: 3},
	}}
	var output bytes.Buffer
	if err := WriteTerminalConciseCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Scan complete: 1 potential issue", "Analysis: deterministic checks + bounded AI safeguard review", "AI inventory: 3 component", "AI-use organization (optional): 1 confirmed, 2 draft, 0 retired; 3 model-suggested; 4 other AI-related code signal", "Applicability context: Example — incomplete", "unresolved fact(s); requirement mapping is provisional", "Code safeguards checked: 7 total", "Requirement screening: 5 likely required", "3/4 technical target", "Use --verbose"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("concise completion missing %q:\n%s", expected, output.String())
		}
	}
	for _, unwanted := range []string{"Candidate location:", "Pack digest:", "EVIDENCE "} {
		if strings.Contains(output.String(), unwanted) {
			t.Errorf("concise completion included detailed evidence %q:\n%s", unwanted, output.String())
		}
	}
}

func TestTerminalCompletionSeparatesAdvisoryReview(t *testing.T) {
	value := New(".", "0.2.0", nil, nil, 0)
	value.Review = &providers.ReviewResult{
		Provider: providers.Ollama, Model: "gemma3", InputFindings: 1, Reviewed: 1,
		Observations: []providers.Observation{{
			RuleID: "AI-LOG-001", Verdict: providers.VerdictUncertain,
			Confidence: "low", Rationale: "More context is required.", SuggestedAction: "Review manually.",
		}},
	}
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Ollama advisory review (gemma3)", "REVIEW", "More context is required", "Scan complete"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestTerminalCompletionSeparatesTechnicalObjectiveReview(t *testing.T) {
	value := New(".", "0.2.0-dev", nil, nil, 0)
	value.TechnicalReview = &providers.TechnicalReviewResult{
		Provider: providers.Ollama, Model: "gemma3", InputCandidates: 1, Reviewed: 1,
		Observations: []providers.TechnicalObservation{{
			SystemID: "ranking", SystemName: "Ranking", OwnershipScope: "explicit", RepositoryFiles: 42,
			ObjectiveID: "eu-aia-14-override-intervention", EvidenceFingerprint: strings.Repeat("a", 64),
			EvidenceStatus: "candidate-evidence", InvestigationMode: "candidate-validation",
			Strength: providers.StrengthWeak, ModelStrength: providers.StrengthPartial,
			Conclusion: providers.ConclusionTestOnly, Assurance: providers.AssuranceTestEvidenceObserved, Confidence: "medium",
			Rationale:           "The handler is live but its authorization is unresolved.",
			UnresolvedQuestions: []string{"Which role can invoke it?"}, SuggestedReview: "Trace middleware.",
			GuardrailNote: "Test-only anchors cannot provide partial evidence.",
		}},
	}
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Ollama technical evidence investigation", "EVIDENCE", "test-only-evidence", "test-evidence-observed", "eu-aia-14-override-intervention", "explicit ownership for Ranking (ranking), 42 repository file(s)", "Which role can invoke it?", "Guardrail:", "model returned partial", "Scan complete"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestTerminalCompletionShowsApplicabilitySeparatelyFromFindings(t *testing.T) {
	value := New(".", "0.2.0-dev", nil, nil, 0)
	assessment := profile.AssessEUAIAct([]profile.System{profile.NewDraftSystem("example", "Example")})
	value.Applicability = &assessment
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"EU AI Act applicability profile", "Automated scope: needs-context", "Scan complete: 0 potential issues"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestTerminalCompletionShowsTechnicalEvidenceSeparatelyFromFindings(t *testing.T) {
	pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	system := profile.NewDraftSystem("candidate", "Candidate")
	system.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	system.OperatingRegions = []profile.OperatingRegion{profile.RegionEU}
	system.UseCaseDomains = []profile.UseCaseDomain{profile.DomainEmployment}
	assessment := framework.Evaluate(pack, []profile.System{system}, discovery.Repository{})
	value := New(".", "0.2.0-dev", nil, nil, 0)
	value.TechnicalEvidence = &assessment
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Technical evidence", "NOT DETECTED", "Technical summary:", "Scan complete: 0 potential issues"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestTerminalCompletionShowsInventoryAndReconciliation(t *testing.T) {
	value := New(".", "0.2.0-dev", nil, nil, 0)
	aiInventory := inventory.Report{Summary: inventory.Summary{Components: 1, Signals: 1}, Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high",
		Locations: []inventory.Location{{Path: "client.go", Line: 4}},
	}}}
	value.AIInventory = &aiInventory
	mapping := reconciliation.Report{
		MappingVersion: "0.1.2",
		Summary:        reconciliation.Summary{Systems: 1, LikelyRequired: 1, RequirementWithoutEvidence: 1},
		Systems: []reconciliation.SystemResult{{SystemID: "assistant", SystemName: "Assistant", Objectives: []reconciliation.ObjectiveResult{{
			ObjectiveID: "eu-aia-14-human-review-gate", Title: "Human review gate", SourceReference: "Article 14",
			Mapping: reconciliation.MappingRequirementWithoutEvidence,
			Reasons: []reconciliation.Reason{{Code: "candidate-evidence-not-detected", Message: "The bounded scan did not detect evidence."}},
		}}}},
	}
	value.Reconciliation = &mapping
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"AI component inventory", "OpenAI", "Requirement/evidence reconciliation", "NOT FOUND", "candidate-evidence-not-detected"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
}
