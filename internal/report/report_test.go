package report

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
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
		Mode: providers.RepositoryAnalysisTargeted, GroupingStatus: providers.RepositoryGroupingComplete,
		Subsystems: 2, SourceBatchesStarted: 2, SourceBatchesCompleted: 2, SourceBatchesTotal: 2, ProviderRequests: 3,
	}, RequestDiagnostics: []providers.RepositoryRequestDiagnostic{
		{Phase: "source", Scope: "evidence bundle (part 1)", Attempt: 1, DurationNS: int64(2 * time.Second), Outcome: "completed", InputFiles: 3, InputBytes: 2048, InputTokens: 120, OutputTokens: 30, ReasoningTokens: 8},
		{Phase: "source", Scope: "evidence bundle (part 2)", Attempt: 1, DurationNS: int64(3 * time.Second), Outcome: "retryable-error", RetryReason: "rate-limit", InputFiles: 2, InputBytes: 1024, InputTokens: 90, OutputTokens: 10},
	}, Result: providers.RepositorySectionResult{
		AIUses: []providers.RepositoryAIUse{{
			ID: "inferred-use-example", Name: "Inferred assistant", Purpose: "Draft replies", Confidence: "medium",
			Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12, Summary: "Model invocation"}}, MemberObservationIDs: []string{"observation-a", "observation-b"},
		}},
		AIUseFacts: []providers.RepositoryAIUseFactSet{{AIUseID: "inferred-use-example", Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}},
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
			ObjectiveID: "pack/human-review", AIUseID: "assistant", SystemID: "assistant",
			Strength: providers.StrengthPartial, Confidence: "high", Rationale: "A review path is present but incomplete.",
		}},
		ResolvedEvidenceGaps: []providers.RepositoryResolvedEvidenceGap{{
			GapID: "gap-review-call", Kind: "objective-missing", OriginalText: "The provider call was outside one source batch.",
			ResolvingObservationIDs: []string{"observation-b"}, Evidence: []providers.RepositoryCitation{{Path: "assistant.go", Line: 12, Summary: "The connected provider call."}},
			Reason: "Another validated member contains the provider call.",
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
	if decoded.SchemaVersion != 17 || decoded.Tool.Name != "ComplyScan" || decoded.Tool.Version != "0.1.0" || decoded.Tool.Commit != "abc123" {
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
	if len(decoded.RepositoryAnalysis.Result.AIUses) != 1 || !reflect.DeepEqual(decoded.RepositoryAnalysis.Result.AIUses[0].MemberObservationIDs, []string{"observation-a", "observation-b"}) {
		t.Fatalf("schema-version 14 inferred-use observation membership was not serialized: %#v", decoded.RepositoryAnalysis.Result.AIUses)
	}
	if len(decoded.RepositoryAnalysis.Result.ResolvedEvidenceGaps) != 1 || decoded.RepositoryAnalysis.Result.ResolvedEvidenceGaps[0].GapID != "gap-review-call" {
		t.Fatalf("schema-version 14 cross-batch resolution audit was not serialized: %#v", decoded.RepositoryAnalysis.Result.ResolvedEvidenceGaps)
	}
	if decoded.RepositoryAnalysis.Coverage.SourceBatchesStarted != 2 || decoded.RepositoryAnalysis.Coverage.SourceBatchesCompleted != 2 || decoded.RepositoryAnalysis.Coverage.SourceBatchesTotal != 2 || decoded.RepositoryAnalysis.Coverage.ProviderRequests != 3 || decoded.RepositoryAnalysis.Coverage.GroupingStatus != providers.RepositoryGroupingComplete {
		t.Fatalf("schema-version 16 batch/grouping coverage was not serialized: %#v", decoded.RepositoryAnalysis.Coverage)
	}
	if len(decoded.RepositoryAnalysis.RequestDiagnostics) != 2 || decoded.RepositoryAnalysis.RequestDiagnostics[1].RetryReason != "rate-limit" || decoded.RepositoryAnalysis.RequestDiagnostics[0].DurationNS != int64(2*time.Second) || decoded.RepositoryAnalysis.RequestDiagnostics[0].InputTokens != 120 || decoded.RepositoryAnalysis.RequestDiagnostics[0].OutputTokens != 30 || decoded.RepositoryAnalysis.RequestDiagnostics[0].ReasoningTokens != 8 {
		t.Fatalf("schema-version 16 request diagnostics were not serialized: %#v", decoded.RepositoryAnalysis.RequestDiagnostics)
	}
	if !strings.Contains(output.String(), `"source_batches_started": 2`) || !strings.Contains(output.String(), `"provider_requests": 3`) || !strings.Contains(output.String(), `"request_diagnostics"`) || !strings.Contains(output.String(), `"input_tokens": 120`) || !strings.Contains(output.String(), `"reasoning_tokens": 8`) {
		t.Fatalf("schema-version 16 request/start diagnostics are missing from JSON:\n%s", output.String())
	}
	if !strings.Contains(output.String(), `"ai_use_id": "assistant"`) {
		t.Fatalf("schema-version 16 AI-use attribution is missing:\n%s", output.String())
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

func TestDeveloperObjectiveNextStepUsesDeveloperActionsInsteadOfBatchJargon(t *testing.T) {
	tests := []struct {
		missing string
		want    string
	}{
		{
			missing: "Only test-side evidence is submitted; validation implementation is outside this batch.",
			want:    "Complete or test the missing part, then rerun the scan. Missing: The reviewed evidence only shows tests; validation implementation is not established by the reviewed code",
		},
		{
			missing: "No retry or fallback behavior is shown in this submitted flow.",
			want:    "Add retry and fallback handling to the production AI call path. Then rerun the scan",
		},
		{
			missing: "No submitted code shows benchmark execution or pass/fail evaluation.",
			want:    "Run the benchmark in CI or another executable workflow and enforce its pass/fail result. Then rerun the scan",
		},
		{
			missing: "Verification of retry exhaustion and terminal fallback behavior.",
			want:    "Add a test showing what happens after all retries fail. Then rerun the scan",
		},
	}
	for index, testCase := range tests {
		observation := providers.RepositoryObjectiveObservation{
			Strength: providers.StrengthPartial, Confidence: "medium", MissingEvidence: []string{testCase.missing},
			SupportingEvidence: []providers.RepositoryCitation{{Path: "safeguard.go", Line: 10, Summary: "Partial implementation."}},
		}
		if index == 2 {
			observation.Strength = providers.StrengthUncertain
			observation.Confidence = "low"
		}
		if got := developerObjectiveNextStep(observation, "latest.json"); got != testCase.want {
			t.Fatalf("developer action = %q, want %q", got, testCase.want)
		}
	}
	retryObservation := providers.RepositoryObjectiveObservation{
		Strength: providers.StrengthUncertain, Confidence: "low",
		MissingEvidence: []string{"No test evidence in this batch verifies retry outcomes."},
	}
	if got, want := developerObjectiveNextStep(retryObservation, "latest.json"), "Add a test that verifies retry outcomes. Then rerun the scan"; got != want {
		t.Fatalf("retry action = %q, want %q", got, want)
	}
	retryCompletion := providers.RepositoryObjectiveObservation{
		Strength: providers.StrengthPartial, Confidence: "medium",
		MissingEvidence: []string{
			"Verification of retry exhaustion and final fallback outcome.",
			"Tests demonstrating retry bounds and terminal failure behavior.",
		},
	}
	if got, want := developerObjectiveNextStep(retryCompletion, "latest.json"), "Add tests proving the retry limit, behavior after retries are exhausted, and the final fallback result. Then rerun the scan"; got != want {
		t.Fatalf("complete retry action = %q, want %q", got, want)
	}
	if got := developerObservationFollowUp(retryCompletion); got != "Add tests proving the retry limit, behavior after retries are exhausted, and the final fallback result" {
		t.Fatalf("retry evidence follow-up = %q", got)
	}
	conflicting := providers.RepositoryObjectiveObservation{
		Strength: providers.StrengthUncertain, Confidence: "low",
		Rationale: "Validated source batches returned differing code-level assessments; the combined result remains uncertain.",
	}
	if got, want := developerObjectiveNextStep(conflicting, "latest.json"), "Review the cited paths and identify the end-to-end test or enforcement path that establishes the expected behavior, then rerun the scan"; got != want {
		t.Fatalf("conflicting-evidence action = %q, want %q", got, want)
	}
	if got := developerPlainLanguage(conflicting.Rationale); strings.Contains(got, "source batches") || !strings.Contains(got, "cited code supports conflicting conclusions") {
		t.Fatalf("conflicting-evidence rationale was not developer-facing: %q", got)
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
			SourceBatchesStarted: 3, SourceBatchesCompleted: 1, SourceBatchesTotal: 3, ProviderRequests: 4,
		},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{},
		},
	}
	if label := developerAnalysisSummaryLabel(value); !strings.Contains(label, "incomplete") {
		t.Fatalf("analysis summary = %q, want incomplete", label)
	}
	if label := developerRepositoryAnalysisLabel(value); !strings.Contains(label, "3 code batch(es) started and 1 of 3 were reviewed successfully") {
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
		batchCoverage := strings.Contains(lowerOutput, "3 distinct source batch") || strings.Contains(lowerOutput, "3 code batch")
		if !batchCoverage || !strings.Contains(lowerOutput, "1 of 3") || !(strings.Contains(lowerOutput, "no unsynthesized model conclusions") || strings.Contains(lowerOutput, "no unchecked model conclusions")) {
			t.Fatalf("%s incomplete report lacks truthful partial coverage:\n%s", name, output)
		}
		if strings.Contains(output, "No AI implementation was identified") || strings.Contains(output, "did not suggest a specific AI use") {
			t.Fatalf("%s incomplete report reads as a completed negative:\n%s", name, output)
		}
	}
}

func TestCompletedSourceReviewReportsIncompleteGroupingWithoutDiscardingObservations(t *testing.T) {
	value := New(".", "test", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Coverage: providers.RepositoryCoverage{
			Mode: providers.RepositoryAnalysisTargeted, GroupingStatus: providers.RepositoryGroupingIncomplete,
			RepositoryFiles: 20, FilesSubmitted: 2, ProviderRequests: 3,
			SourceBatchesStarted: 2, SourceBatchesCompleted: 2, SourceBatchesTotal: 2, Subsystems: 2,
		},
		Result: providers.RepositorySectionResult{
			Scope: ".", AIUses: []providers.RepositoryAIUse{{
				ID: "inferred-use-one", Name: "Repository review observation", Purpose: "Send bounded evidence to a model", Confidence: "high",
				Evidence: []providers.RepositoryCitation{{Path: "review.go", Line: 20, Summary: "The model request is sent."}}, MemberObservationIDs: []string{"observation-one"},
			}},
			AIUseFacts:            []providers.RepositoryAIUseFactSet{{AIUseID: "inferred-use-one", Facts: []providers.RepositoryAIUseFact{}, UnresolvedQuestions: []string{}}},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
		},
	}
	if developerRepositoryAnalysisIncomplete(value) {
		t.Fatal("completed source evidence was incorrectly classified as an incomplete repository review")
	}
	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	var detailed bytes.Buffer
	if err := WriteDetailedMarkdown(&detailed, value); err != nil {
		t.Fatal(err)
	}
	var terminal bytes.Buffer
	if err := WriteTerminalRepositoryAnalysis(&terminal, *value.RepositoryAnalysis); err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{"concise": concise.String(), "detailed": detailed.String(), "terminal": terminal.String()} {
		lower := strings.ToLower(output)
		observationBoundary := strings.Contains(lower, "reviewed observation") || strings.Contains(lower, "validated technical observation")
		if !strings.Contains(lower, "group") || !observationBoundary || !(strings.Contains(lower, "remain") || strings.Contains(lower, "retained separately")) {
			t.Fatalf("%s output omitted the evidence-complete/grouping-incomplete boundary:\n%s", name, output)
		}
		if strings.Contains(lower, "no unsynthesized model conclusions were retained") {
			t.Fatalf("%s output falsely says validated observations were discarded:\n%s", name, output)
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
	if !strings.Contains(output.String(), `"schema_version": 17`) || !strings.Contains(output.String(), `"repository_analysis_run": "not-requested"`) || !strings.Contains(output.String(), `"evidence_investigation"`) || !strings.Contains(output.String(), `"system_id": "ranking"`) || !strings.Contains(output.String(), `"repository_files": 42`) || strings.Contains(output.String(), `"technical_review"`) {
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
		"## AI functionality found", "Confirmed scope", "Confirmed generation", "runtime/\\*\\*",
		"Optional draft", "Draft ranking", "ranking/\\*\\*",
		"Inferred from code — AI workflow", "Suggested assistant", "assistant.go:12",
		"Provider and configuration references retained in `latest.json`", "1 underlying code reference(s)",
		"## Needs product or compliance input",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("concise Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "AI features found") {
		t.Fatalf("concise Markdown retained misleading feature count:\n%s", output.String())
	}
	for _, unwanted := range []string{
		"draft AI-use record still needs confirmation", "AI-use suggestion needs a developer decision", "Run `complyscan ai-uses setup`",
		"What code indicates for Confirmed generation", "Possible roles indicated by the repository", "Organisation context that repository code cannot establish",
	} {
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
	if !strings.Contains(output.String(), "Confirmed scope") || !strings.Contains(output.String(), "Assistant") {
		t.Fatalf("confirmed use was omitted from the compact report:\n%s", output.String())
	}
	if strings.Contains(output.String(), "positive per-use facts") || strings.Contains(output.String(), "fact is false") {
		t.Fatalf("dashboard-level empty fact detail leaked into the compact report:\n%s", output.String())
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
	if !strings.Contains(output.String(), "**No developer actions. No direct code risks found.**") {
		t.Fatalf("optional AI-use organization changed the scan outcome:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Review performed: **local code checks complete; no relevant AI code selected for model review**") {
		t.Fatalf("empty completed review was not described honestly:\n%s", output.String())
	}
	if strings.Contains(output.String(), "| **Review** |") {
		t.Fatalf("optional AI-use organization produced a required action:\n%s", output.String())
	}
}

func TestDeveloperReportSeparatesTechnicalFollowUpFromLegalApplicability(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Result: providers.RepositorySectionResult{
			AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{
				{ObjectiveID: "pack/security", Strength: providers.StrengthPartial, Confidence: "high", Rationale: "Input sanitization exists.", SupportingEvidence: []providers.RepositoryCitation{{Path: "security.go", Line: 12}}, MissingEvidence: []string{"Adversarial-input tests"}},
				{ObjectiveID: "pack/robustness", Strength: providers.StrengthPartial, Confidence: "high", Rationale: "Provider errors are detected.", SupportingEvidence: []providers.RepositoryCitation{{Path: "remote.go", Line: 20}}, MissingEvidence: []string{"Retry-exhaustion test", "Fallback outcome"}},
				{ObjectiveID: "pack/thresholds", Strength: providers.StrengthPartial, Confidence: "medium", Rationale: "Thresholds are configured.", SupportingEvidence: []providers.RepositoryCitation{{Path: "benchmark.go", Line: 30}}, MissingEvidence: []string{"Executable threshold enforcement"}},
				{ObjectiveID: "pack/safe-stop", Strength: providers.StrengthUncertain, Confidence: "medium", Rationale: "The active-provider gate was not included.", MissingEvidence: []string{"Production safe-stop path"}},
			},
			UnmappedObservations: []providers.RepositoryUnmappedObservation{{
				Summary: "Generic provider helper", SuggestedReview: "Review the helper.", Evidence: []providers.RepositoryCitation{{Path: "helper.go", Line: 8}},
			}},
			UnresolvedQuestions: []string{},
		},
	}
	value.AIUseInventory = &aiuse.Snapshot{
		Summary: aiuse.SnapshotSummary{Suggested: 3},
		Suggested: []aiuse.Suggestion{
			{Name: "Repository technical review", Purpose: "Review repository evidence.", Evidence: []providers.RepositoryCitation{{Path: "review.go", Line: 10}}},
			{Name: "Remote model inference adapters", Purpose: "Send requests to model providers.", Evidence: []providers.RepositoryCitation{{Path: "remote.go", Line: 5}}},
			{Name: "Repository benchmark evaluation", Purpose: "Evaluate analysis quality.", Evidence: []providers.RepositoryCitation{{Path: "benchmark.go", Line: 30}}},
		},
	}

	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		"**4 developer actions. No direct code risks found.**",
		"## Needs product or compliance input",
		"Missing: Adversarial-input tests",
		"Add tests proving the retry limit, behavior after retries are exhausted, and the final fallback result",
		"**Repository technical review** — Inferred from code — AI workflow",
		"**Remote model inference adapters** — Inferred from code — Supporting infrastructure",
		"**Repository benchmark evaluation** — Inferred from code — Evaluation/test tooling",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("developer report missing %q:\n%s", expected, text)
		}
	}
	if count := strings.Count(text, "- **Do:**"); count != maxDeveloperActions {
		t.Fatalf("top action count = %d, want %d:\n%s", count, maxDeveloperActions, text)
	}
	if strings.Contains(text, "Review the helper") {
		t.Fatalf("unmapped infrastructure signal displaced concrete safeguard work:\n%s", text)
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
		"## Developer actions", "Support answer generation",
		"Human review gate", "The code review found only a partial implementation inside this AI use", "apps/support/review.go:12",
		"Unlinked classifier", "Safe stop",
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
	if strings.Count(markdown.String(), "Repository-level, not assigned to one confirmed AI use: Safe stop") != 1 {
		t.Fatalf("per-use decisions were duplicated in the generic evidence section:\n%s", markdown.String())
	}
	var terminal bytes.Buffer
	if err := WriteTerminalConciseCompletion(&terminal, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(terminal.String(), "Per-use safeguard detail (optional scope refinement): 2 confirmed AI use(s), 2 framework/system context(s)") ||
		!strings.Contains(terminal.String(), "Analysis: local code checks and AI code review completed") ||
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
	started, err := writeDeveloperFrameworkAssessmentMarkdown(&output, developerReportView{hasPerUseMappings: true})
	if err != nil {
		t.Fatal(err)
	}
	if started || output.Len() != 0 {
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
	if result := developerUseCodeResult(objective); !strings.Contains(result, "demonstrated by the AI code review") {
		t.Fatalf("bounded investigation result = %q", result)
	}
	objective.Investigation.Conclusion = providers.ConclusionNotFoundAfterInvestigation
	action, include = developerUseObjectiveAction(use, association, objective)
	if !include || action.priority != "High" || !strings.Contains(action.why, "No implementation found") {
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
		Coverage:           providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 20, FilesSubmitted: 2, CitationsChecked: 1, ProviderRequests: 3},
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
	if !strings.Contains(terminal.String(), "Repository AI code analysis") || !strings.Contains(terminal.String(), "2 code-excerpt transfer(s) from 20 discovered file(s), 3 provider request(s)") || !strings.Contains(terminal.String(), "Follow-up: 1 bounded excerpt(s)") || !strings.Contains(terminal.String(), "Recovery: the initial output limit was reached") || !strings.Contains(terminal.String(), "Tokens: 200 input, 100 output (40 reasoning)") || !strings.Contains(terminal.String(), "main.go:12") {
		t.Fatalf("repository analysis missing from terminal:\n%s", terminal.String())
	}
	var markdown bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markdown.String(), "## AI functionality found") || !strings.Contains(markdown.String(), "Summary generation") || !strings.Contains(markdown.String(), "not a legal compliance decision") {
		t.Fatalf("repository analysis boundary missing from Markdown:\n%s", markdown.String())
	}
	var detailed bytes.Buffer
	if err := WriteDetailedMarkdown(&detailed, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detailed.String(), "source-content byte(s), across 3 provider request(s)") {
		t.Fatalf("detailed report presents source bytes as total external-transfer bytes:\n%s", detailed.String())
	}
}

func TestCurrentRunCompatibilityAccountingStaysOutOfConciseReport(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Notes:    []string{"Provider request and token totals include the live source-free model compatibility request that supplied this run's initial capacity snapshot."},
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisTargeted, FilesSubmitted: 1, ProviderRequests: 2},
		Usage:    providers.Usage{PromptTokens: 120, CompletionTokens: 30},
		Result:   providers.RepositorySectionResult{AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
	}
	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(concise.String(), "Current-run compatibility accounting") || strings.Contains(concise.String(), "source-free model compatibility request") {
		t.Fatalf("concise report exposed provider accounting that belongs in JSON/dashboard data:\n%s", concise.String())
	}
	if !strings.Contains(concise.String(), "Full evidence and dashboard data: `latest.json`") {
		t.Fatalf("concise report did not point to the complete accounting data:\n%s", concise.String())
	}
}

func TestConciseRepositoryCoverageStaysDecisionFocused(t *testing.T) {
	newReport := func() Report {
		value := New(".", "dev", nil, nil, 0)
		value.RepositoryAnalysisRun = RepositoryAnalysisCompleted
		value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
			Provider: providers.Gemini, Model: "gemini-test",
			Coverage: providers.RepositoryCoverage{
				Mode: providers.RepositoryAnalysisTargeted, RepositoryFiles: 20, FilesSubmitted: 13,
				SourceBatchesStarted: 13, SourceBatchesCompleted: 13, SourceBatchesTotal: 13, ProviderRequests: 17,
			},
			Result: providers.RepositorySectionResult{
				AIUses: []providers.RepositoryAIUse{}, AIUseFacts: []providers.RepositoryAIUseFactSet{},
				ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{},
			},
		}
		return value
	}

	t.Run("fresh review", func(t *testing.T) {
		value := newReport()
		var output bytes.Buffer
		if err := WriteMarkdown(&output, value); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "**Assessment coverage:** 20 files evaluated; AI code review completed") {
			t.Fatalf("fresh concise report omitted the assessment boundary:\n%s", output.String())
		}
		for _, internalDetail := range []string{"AI review provider and model", "AI review activity", "17 model call", "13 code batch"} {
			if strings.Contains(output.String(), internalDetail) {
				t.Errorf("fresh concise report exposed internal coverage detail %q:\n%s", internalDetail, output.String())
			}
		}
	})

	t.Run("cached review", func(t *testing.T) {
		value := newReport()
		value.RepositoryAnalysis.CacheHit = true
		value.RepositoryAnalysis.Notes = []string{
			"This scan made 2 live source-free model compatibility request(s) before reusing the repository-analysis cache (100 input, 20 output, 3 reasoning token(s)). It sent no repository source; those compatibility costs are separate from the cached repository-layer coverage and usage totals.",
		}
		var output bytes.Buffer
		if err := WriteMarkdown(&output, value); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "AI code review completed using relevant code selected by ComplyScan (reused private cache)") {
			t.Fatalf("cached concise report omitted the result source boundary:\n%s", output.String())
		}
		for _, internalDetail := range []string{"AI review activity", "Current-run compatibility accounting", "17 model call", "13 code batch", "100 input"} {
			if strings.Contains(output.String(), internalDetail) {
				t.Errorf("cached concise report exposed internal coverage detail %q:\n%s", internalDetail, output.String())
			}
		}
	})
}

func TestChangedReviewCoverageExplainsModelBoundary(t *testing.T) {
	value := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "dev"}, ScanScope{
		Findings: "changed-files", TechnicalEvidence: "full-repository", AIInventory: "full-repository", Reconciliation: "full-repository",
		ChangedSince: "main", AIReview: string(providers.RepositoryReviewScopeChanged), AIReviewFiles: 3, AIReviewChangedFiles: 1, AIReviewConnectedFiles: 2,
	}, time.Now(), nil, nil, 0)
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model", CacheHit: true,
		Notes: []string{"This scan made 1 live source-free model compatibility request(s) before reusing the repository-analysis cache (100 input, 20 output, 3 reasoning token(s)). It sent no repository source; those compatibility costs are separate from the cached repository-layer coverage and usage totals."},
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
	for _, expected := range []string{"reused matching private cache", "Cached reviewed context: 2 original code-excerpt transfer(s); current run transferred no source", "1 changed eligible + 2 connected file(s)", "full 50-file repository governance remained local"} {
		if !strings.Contains(terminal.String(), expected) {
			t.Errorf("terminal changed scope missing %q:\n%s", expected, terminal.String())
		}
	}

	var markdown bytes.Buffer
	if err := WriteDetailedMarkdown(&markdown, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"AI review scope: changed-plus-connected", "matching private cache entry", "1 changed eligible file(s) plus 2 connected file(s)", "Changed-code review boundary", "Current-run compatibility accounting", "100 input, 20 output, 3 reasoning"} {
		if !strings.Contains(markdown.String(), expected) {
			t.Errorf("Markdown changed scope missing %q:\n%s", expected, markdown.String())
		}
	}
	var concise bytes.Buffer
	if err := WriteMarkdown(&concise, value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(concise.String(), "completed for 1 changed and 2 connected code file(s) (reused private cache)") {
		t.Fatalf("concise report omitted the changed-code review boundary:\n%s", concise.String())
	}
	if strings.Contains(concise.String(), "Current-run compatibility accounting") || strings.Contains(concise.String(), "100 input, 20 output, 3 reasoning") {
		t.Fatalf("concise report exposed detailed provider accounting:\n%s", concise.String())
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
	for _, expected := range []string{"no relevant AI code selected for model review", "not run — ComplyScan found no relevant AI code to send for review", "sent no repository code to a model", "does not prove that the repository contains no AI activity"} {
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
	if !strings.Contains(markdown.String(), "**Runtime verification:** 1 passed, 0 failed.") {
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

func TestTerminalCompletionCallsAnIncompleteAIReviewAnIncompleteScan(t *testing.T) {
	value := New(".", "0.2.0", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisIncomplete
	value.Warnings = []string{"OpenAI repository analysis was incomplete: provider unavailable."}
	for name, writer := range map[string]func(io.Writer, Report) error{
		"verbose": WriteTerminalCompletion,
		"concise": WriteTerminalConciseCompletion,
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			if err := writer(&output, value); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "Scan incomplete: 0 potential issues") ||
				strings.Contains(output.String(), "Scan complete:") {
				t.Fatalf("incomplete AI review was presented as a completed scan:\n%s", output.String())
			}
		})
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
	for _, expected := range []string{"Scan complete: 1 potential issue", "Analysis: local code checks and focused AI safeguard review completed", "AI inventory: 3 component", "AI-use organization (optional): 1 confirmed, 2 draft, 0 retired; 3 model-suggested; 4 other AI-related code signal", "Applicability context: Example — incomplete", "unresolved fact(s); requirement mapping is provisional", "Code safeguards checked: 7 total", "Requirement screening: 5 likely required", "3/4 technical target", "Use --verbose"} {
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

func TestTerminalCompletionNamesConfiguredReviewProviders(t *testing.T) {
	value := New(".", "0.2.0", nil, nil, 0)
	value.Review = &providers.ReviewResult{Provider: providers.OpenAI, Model: "gpt-4.1", InputFindings: 1, Reviewed: 1}
	value.TechnicalReview = &providers.TechnicalReviewResult{Provider: providers.Anthropic, Model: "claude-sonnet", InputCandidates: 1, Reviewed: 1}
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"OpenAI advisory review (gpt-4.1)", "Anthropic technical evidence investigation (claude-sonnet)"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("terminal output missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Ollama advisory review") || strings.Contains(output.String(), "Ollama technical evidence investigation") {
		t.Fatalf("terminal output mislabeled a hosted provider as Ollama:\n%s", output.String())
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
