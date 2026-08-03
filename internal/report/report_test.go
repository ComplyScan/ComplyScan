package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/framework"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
	"github.com/1eonardodawinki/ComplyScan/internal/providers"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
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
	if decoded.SchemaVersion != 1 || decoded.Tool.Name != "ComplyScan" || decoded.Tool.Version != "0.1.0" || decoded.Tool.Commit != "abc123" {
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
			ObjectiveID: "eu-aia-14-override-intervention", EvidenceFingerprint: strings.Repeat("a", 64),
			Strength: providers.StrengthPartial, Confidence: "medium",
			Rationale:           "The handler is live but its authorization is unresolved.",
			UnresolvedQuestions: []string{"Which role can invoke it?"}, SuggestedReview: "Trace middleware.",
		}},
	}
	var output bytes.Buffer
	if err := WriteTerminalCompletion(&output, value); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Ollama technical-objective review", "TECH", "eu-aia-14-override-intervention", "Which role can invoke it?", "Scan complete"} {
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
