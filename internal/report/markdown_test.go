// complyscan:ignore-technical-evidence -- this file embeds synthetic technical-objective fixtures.
package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestWriteMarkdownKeepsReviewerDiagnosticsOutOfDeveloperSummary(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.Review = &providers.ReviewResult{Provider: providers.OpenAI, Model: "test-model"}
	value.TechnicalReview = &providers.TechnicalReviewResult{Provider: providers.OpenAI, Model: "test-model"}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "openai / test-model") || strings.Contains(output.String(), "AI advisory review") {
		t.Fatalf("developer summary included model diagnostics:\n%s", output.String())
	}
}

func TestDetailedMarkdownNamesConfiguredReviewProviders(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.Review = &providers.ReviewResult{Provider: providers.Gemini, Model: "gemini-test"}
	value.TechnicalReview = &providers.TechnicalReviewResult{Provider: providers.OpenRouter, Model: "router-test"}
	var output bytes.Buffer
	if err := WriteDetailedMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"## Google Gemini advisory review", "## OpenRouter technical evidence investigation"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("detailed Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "## Ollama advisory review") || strings.Contains(output.String(), "## Ollama technical evidence investigation") {
		t.Fatalf("detailed Markdown mislabeled a hosted provider as Ollama:\n%s", output.String())
	}
}

func TestBoundedOnlyQualificationFailureDoesNotClaimRepositoryReviewRan(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisNotRequested
	value.Warnings = []string{"AI review was incomplete because model qualification failed: temporary provider failure"}
	if developerRepositoryAnalysisIncomplete(value) {
		t.Fatal("bounded-only qualification failure was misclassified as an incomplete repository analysis")
	}
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "AI code review incomplete") || strings.Contains(output.String(), "repository model review") {
		t.Fatalf("bounded-only report falsely claimed a repository review ran:\n%s", output.String())
	}
}

func TestWriteDetailedMarkdownRendersScannerEvidenceTrace(t *testing.T) {
	pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
	if err != nil {
		t.Fatal(err)
	}
	evidence := framework.Evaluate(pack, nil, discovery.Repository{Files: []discovery.File{{
		Path: "review/override.go", Kind: discovery.KindSource,
		Content: []byte("package review\nfunc OverrideDecision(output string) {}\n"),
	}}})
	evidence.Target = "."
	value := NewWithMetadata(
		".", Tool{Name: "ComplyScan", Version: "0.2.0-dev", Commit: "abc123"},
		ScanScope{Findings: "full-repository", TechnicalEvidence: "full-repository"},
		time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		[]rules.Finding{{
			Fingerprint: "fingerprint", RuleID: "AI-LOG-001", Title: "Review *logging*", Severity: rules.SeverityHigh,
			Message: "Prompt-like data reaches logging.", Path: "app.go", StartLine: 10,
			Evidence: "logger.Info(response)", Remediation: "Review the data flow.", Confidence: "high",
		}}, nil, 0,
	)
	value.TechnicalEvidence = &evidence
	aiInventory := inventory.NewReport(".", "0.2.0-dev", []inventory.Signal{{
		Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceImport,
		Scope: inventory.ScopeRuntime, Confidence: "high", Path: "client.go", Line: 2, Evidence: "import openai",
	}}, nil)
	value.AIInventory = &aiInventory
	mapping := reconciliation.Build(nil, profile.AssessEUAIAct(nil), evidence, aiInventory, nil)
	value.Reconciliation = &mapping
	value.TechnicalReview = &providers.TechnicalReviewResult{
		Provider: providers.Ollama, Model: "gemma3", InputCandidates: 1, Reviewed: 1,
		Observations: []providers.TechnicalObservation{{
			SystemID: "ranking", SystemName: "Ranking", OwnershipScope: "explicit", RepositoryFiles: 42,
			ObjectiveID: "eu-aia-14-override-intervention", EvidenceFingerprint: strings.Repeat("b", 64),
			EvidenceStatus: "candidate-evidence", InvestigationMode: "candidate-validation",
			Strength: providers.StrengthWeak, ModelStrength: providers.StrengthPartial,
			Conclusion: providers.ConclusionPartial, Assurance: providers.AssuranceSignalDetected,
			Confidence: "high", Rationale: "Only an exported candidate was found.", RuntimeVerificationRequired: true, LegalReviewRequired: true,
			GuardrailNote: "Test-only anchors cannot provide partial evidence.",
		}},
	}
	var output bytes.Buffer
	if err := WriteDetailedMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"# ComplyScan report",
		"## Result at a glance",
		"AI-related components detected: **1**",
		"Technical objectives with candidate evidence: **1**",
		"AI review: **Performed — advisory AI evidence review included**",
		"Legal applicability: **Not assessed by this technical report**",
		"## AI-related components",
		"Runtime-source integrations: **1** — OpenAI",
		"## Technical checklist",
		"| Article 14 | Human override or intervention mechanism | **Candidate evidence** |",
		"## Recommended next actions",
		"<summary><strong>Show detailed scanner evidence</strong></summary>",
		"scan-",
		"### Deterministic rule findings",
		"**1 finding:**",
		"Review \\*logging\\*",
		"### Technical evidence: EU AI Act technical code evidence",
		"## Independently observed AI components",
		"## Requirement-to-evidence reconciliation",
		"Path ownership configured: no",
		"unassigned -&gt; no system",
		"no-declared-system",
		"### Repository analysis",
		"exported-entry-candidate",
		"Unresolved:",
		"Candidate evidence detected — Human override or intervention mechanism",
		"Applicability conditions: framework scope high-risk-system",
		"`review/override.go:2`",
		"No evidence detected",
		"### Coverage boundary",
		"## Ollama technical evidence investigation",
		"System: Ranking (`ranking`)",
		"Repository files in scope: 42",
		"Assurance level: signal-detected",
		"Only an exported candidate was found.",
		"Original model strength: partial.",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if summaryIndex, detailIndex := strings.Index(output.String(), "## Result at a glance"), strings.Index(output.String(), "## Detailed scanner evidence"); summaryIndex < 0 || detailIndex < 0 || summaryIndex > detailIndex {
		t.Fatalf("human summary does not precede scanner detail:\n%s", output.String())
	}
}

func TestWriteMarkdownExplainsTestOnlyComponentsAndQuickScan(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	aiInventory := inventory.NewReport(".", "dev", []inventory.Signal{{
		Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceImport,
		Scope: inventory.ScopeTest, Confidence: "high", Path: "client_test.go", Line: 2,
	}}, nil)
	value.AIInventory = &aiInventory
	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Scan status: **deterministic checks only**",
		"Repository AI review: **not run — deterministic checks only**",
		"This scan used deterministic checks only",
		"Additional provider or configuration references: **OpenAI**",
		"not treated as separate deployed AI functions",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Detailed scanner evidence") || strings.Count(output.String(), "\n") > 80 {
		t.Fatalf("concise Markdown included scanner trace or became too long:\n%s", output.String())
	}
}

func TestWriteMarkdownDistinguishesIncompleteRepositoryReview(t *testing.T) {
	value := New(".", "dev", nil, []string{"OpenAI repository-wide analysis was incomplete: request too large."}, 0)
	value.RepositoryAnalysisRun = RepositoryAnalysisIncomplete

	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"deterministic scan completed, but the AI code review did not finish",
		"Scan status: **deterministic checks complete; AI code review incomplete**",
		"Repository AI review: **incomplete; deterministic results are available**",
		"Scan incomplete or uncertain",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Repository AI review: **not run") {
		t.Fatalf("incomplete review was presented as deterministic-only:\n%s", output.String())
	}
}

func TestWriteMarkdownPrioritizesDeveloperDecisions(t *testing.T) {
	value := New(".", "dev", []rules.Finding{{
		Fingerprint: "secret", RuleID: "AI-SEC-001", Title: "Possible secret exposure",
		Severity: rules.SeverityHigh, Message: "A credential may reach an AI provider.",
		Path: "client.go", StartLine: 18, Remediation: "Remove the credential from the request.",
	}}, nil, 0)
	value.AIInventory = func() *inventory.Report {
		report := inventory.NewReport(".", "dev", []inventory.Signal{{
			Name: "OpenAI", Kind: inventory.KindProvider, EvidenceType: inventory.EvidenceImport,
			Scope: inventory.ScopeRuntime, Path: "client.go", Line: 3, Confidence: "high",
		}}, nil)
		return &report
	}()
	assessment := profile.AssessEUAIAct([]profile.System{profile.NewDraftSystem("assistant", "Assistant")})
	evidence := framework.TechnicalEvidenceReport{
		Analysis: framework.RepositoryAnalysis{SourceFilesSeen: 2, FilesIndexed: 2},
		Objectives: []framework.ObjectiveAssessment{{
			ID: "logging", Title: "Operational logging", SourceReference: "Article 12", Status: framework.ObjectiveCandidate,
			Matches: []framework.EvidenceMatch{{Path: "audit.go", StartLine: 11}},
		}},
	}
	value.Frameworks = []FrameworkResult{{
		ID: "eu-ai-act", Name: "EU AI Act technical code evidence", Applicability: &assessment,
		TechnicalEvidence: evidence,
		Reconciliation: reconciliation.Report{Systems: []reconciliation.SystemResult{{
			SystemID: "assistant", SystemName: "Assistant", Objectives: []reconciliation.ObjectiveResult{{
				ObjectiveID: "logging", Title: "Operational logging",
				Requirement: reconciliation.RequirementLikelyRequired,
				Mapping:     reconciliation.MappingRequirementWithEvidence,
			}, {
				ObjectiveID: "oversight", Title: "Human oversight",
				Requirement: reconciliation.RequirementLikelyRequired,
				Mapping:     reconciliation.MappingRequirementWithoutEvidence,
			}},
		}}},
	}}
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{
		Provider: providers.OpenAI, Model: "test-model",
		Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisFull, RepositoryFiles: 2, FilesSubmitted: 2},
		Result: providers.RepositorySectionResult{
			AIUses: []providers.RepositoryAIUse{{
				ID: "assistant", Name: "Answer generation", Purpose: "Generate answers for users", Confidence: "high",
				Evidence: []providers.RepositoryCitation{{Path: "client.go", Line: 24}},
			}},
			ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
				ObjectiveID: "eu-ai-act/logging", Strength: providers.StrengthStrong, Confidence: "high",
				SupportingEvidence: []providers.RepositoryCitation{{Path: "audit.go", Line: 11}},
			}},
		},
	}

	var output bytes.Buffer
	if err := WriteMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"## Result", "**Action required**", "## What to do next", "Possible secret exposure", "**Do:** Remove the credential from the request", "Human oversight",
		"## What the code shows", "Implemented in the reviewed code", "audit.go:11",
		"## AI functionality found", "Answer generation",
		"## What code cannot determine", "Where will this AI feature be offered or used?",
		"## Scan coverage", "Full evidence and dashboard data: `latest.json`",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if actionIndex, findingsIndex := strings.Index(output.String(), "## What to do next"), strings.Index(output.String(), "## What the code shows"); actionIndex < 0 || findingsIndex < 0 || actionIndex > findingsIndex {
		t.Fatalf("actionable next steps did not precede supporting scan detail:\n%s", output.String())
	}
	for _, excluded := range []string{"## Technical checklist", "## AI advisory review", "No evidence detected for", "not-substantiated", "Candidate evidence", "technical objective", "Possible roles indicated", "Legal and technical details", "a person still needs to check it"} {
		if strings.Contains(output.String(), excluded) {
			t.Errorf("developer summary contains diagnostic detail %q:\n%s", excluded, output.String())
		}
	}
	if lines := strings.Count(output.String(), "\n"); lines > 75 {
		t.Fatalf("developer report has %d lines, want at most 75:\n%s", lines, output.String())
	}
}

func TestMarkdownTextRemovesLineBreaksAndEscapesMarkup(t *testing.T) {
	if got := markdownText("hello\n<script>[x] *y*"); got != "hello &lt;script&gt;\\[x\\] \\*y\\*" {
		t.Fatalf("markdownText() = %q", got)
	}
	if got := inlineCode("a`b"); strings.Contains(got, "`a`b`") {
		t.Fatalf("inlineCode did not protect backtick: %q", got)
	}
}

func TestDeveloperEvidenceUsesModelDecisionInsteadOfCandidateDisclaimer(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	value.TechnicalEvidence = &framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{{
		ID: "logging", Title: "Automatic event logging", Status: framework.ObjectiveCandidate,
		Matches: []framework.EvidenceMatch{{Path: "logging.go", StartLine: 14}},
	}}}
	value.RepositoryAnalysis = &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{
		ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
			ObjectiveID: "pack/logging", Strength: providers.StrengthNotSupported, Confidence: "high",
			Rationale: "The cited helper logs validation progress, not AI runtime events.",
		}},
	}}
	items, total := developerSupportingEvidence(value, map[string]string{"logging": "Automatic event logging"})
	if total != 1 || len(items) != 1 {
		t.Fatalf("evidence = %#v, total = %d", items, total)
	}
	if !strings.Contains(items[0].assessment, "Not implemented in the reviewed code") || !strings.Contains(items[0].assessment, "not AI runtime events") {
		t.Fatalf("assessment = %q", items[0].assessment)
	}
	if strings.Contains(items[0].assessment, "person still needs") {
		t.Fatalf("legacy disclaimer remained: %q", items[0].assessment)
	}
}

func TestMarkdownShowsApplicabilityReadinessAndUnresolvedFacts(t *testing.T) {
	value := New(".", "dev", nil, nil, 0)
	assessment := profile.AssessEUAIAct([]profile.System{profile.NewDraftSystem("example", "Example")})
	value.Frameworks = []FrameworkResult{{
		ID: "eu-ai-act", Name: "EU AI Act technical code evidence", Nature: framework.NatureLegislation,
		Applicability: &assessment,
	}}
	var output bytes.Buffer
	if err := WriteDetailedMarkdown(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"technical mapping readiness **incomplete**", "Unresolved fact: Operating regions have not been established"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
}
