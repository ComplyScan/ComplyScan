package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
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
	var output bytes.Buffer
	if err := WriteJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 5 || decoded.Tool.Name != "ComplyScan" || decoded.Tool.Version != "0.1.0" || decoded.Tool.Commit != "abc123" {
		t.Fatalf("unexpected tool: %#v", decoded.Tool)
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

func TestWriteJSONUsesSchemaFiveEvidenceInvestigationContract(t *testing.T) {
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
	if !strings.Contains(output.String(), `"schema_version": 5`) || !strings.Contains(output.String(), `"evidence_investigation"`) || !strings.Contains(output.String(), `"system_id": "ranking"`) || !strings.Contains(output.String(), `"repository_files": 42`) || strings.Contains(output.String(), `"technical_review"`) {
		t.Fatalf("unexpected schema-version-5 investigation JSON:\n%s", output.String())
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
	if !strings.Contains(markdown.String(), "## Isolated execution verification") || !strings.Contains(markdown.String(), "Passing does not establish compliance") {
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
	value.Frameworks = []FrameworkResult{{
		TechnicalEvidence: framework.TechnicalEvidenceReport{Summary: framework.ObjectiveSummary{Total: 7, CandidateEvidence: 3, NotDetected: 4}},
		Reconciliation:    reconciliation.Report{Summary: reconciliation.Summary{LikelyRequired: 5, RequirementWithEvidence: 3, RequirementWithoutEvidence: 2, Unresolved: 1}},
		TechnicalReview:   &providers.TechnicalReviewResult{InputCandidates: 4, Reviewed: 3},
	}}
	var output bytes.Buffer
	if err := WriteTerminalConciseCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Scan complete: 1 potential issue", "AI inventory: 3 component", "Technical objectives: 7 total", "Requirement mapping: 5 likely required", "3/4 technical target", "Use --verbose"} {
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
