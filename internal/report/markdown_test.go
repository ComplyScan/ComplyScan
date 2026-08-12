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
		"AI review: **Not performed — quick technical scan**",
		"Test-only references: **1** — OpenAI",
		"Runtime-source integration code is not proof",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("Markdown missing %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Detailed scanner evidence") || strings.Count(output.String(), "\n") > 80 {
		t.Fatalf("concise Markdown included scanner trace or became too long:\n%s", output.String())
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
