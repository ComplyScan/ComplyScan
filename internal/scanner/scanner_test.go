package scanner

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

type bufferedTestRule struct{}

func (bufferedTestRule) ID() string { return "TEST-001" }

func (bufferedTestRule) Run(context.Context, discovery.Repository) ([]rules.Finding, error) {
	return []rules.Finding{{RuleID: "TEST-001", Severity: rules.SeverityInfo}}, nil
}

func TestScannerRunsOfflineRulePipeline(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "documented-ai-app")
	result, err := New().Scan(context.Background(), target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected AI inventory finding")
	}
	for _, finding := range result.Findings {
		if len(finding.Fingerprint) != 64 {
			t.Fatalf("finding has invalid fingerprint: %#v", finding)
		}
		if finding.RuleID == "AI-DOC-001" || finding.RuleID == "AI-RISK-001" {
			t.Fatalf("documented fixture produced missing-evidence finding: %#v", finding)
		}
	}
}

func TestFindingFingerprintSurvivesLineMovement(t *testing.T) {
	first := rules.Finding{RuleID: "TEST-001", Title: "Test", Path: "src/app.go", StartLine: 4, Evidence: "logger.Info(prompt)"}
	second := first
	second.StartLine = 40
	if rules.ComputeFingerprint(first) != rules.ComputeFingerprint(second) {
		t.Fatal("line movement changed fingerprint")
	}
	second.Evidence = "logger.Info(response)"
	if rules.ComputeFingerprint(first) == rules.ComputeFingerprint(second) {
		t.Fatal("different evidence produced the same fingerprint")
	}
}

func TestScannerStreamsEveryFinding(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	var streamed []rules.Finding
	result, err := New().Scan(context.Background(), target, Options{
		OnFinding: func(finding rules.Finding) error {
			streamed = append(streamed, finding)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed) == 0 {
		t.Fatal("expected findings to be streamed")
	}
	if len(streamed) != len(result.Findings) {
		t.Fatalf("streamed %d findings, final result has %d", len(streamed), len(result.Findings))
	}
}

func TestScannerStreamsFindingsFromBufferedExtensionRules(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "non-ai-repository")
	streamed := 0
	result, err := New(bufferedTestRule{}).Scan(context.Background(), target, Options{
		OnFinding: func(rules.Finding) error {
			streamed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if streamed != 1 || len(result.Findings) != 1 {
		t.Fatalf("streamed=%d findings=%d, want 1 and 1", streamed, len(result.Findings))
	}
}

func TestScannerCanDisableRule(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	result, err := New().Scan(context.Background(), target, Options{
		RuleEnabled: func(id string) bool { return id != "AI-LOG-001" },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.RuleID == "AI-LOG-001" {
			t.Fatalf("disabled rule produced finding: %#v", finding)
		}
	}
}

func TestScannerSuppressesBeforeStreaming(t *testing.T) {
	target := filepath.Join("..", "..", "testdata", "vulnerable-python-ai-app")
	streamed := 0
	result, err := New().Scan(context.Background(), target, Options{
		Suppress: func(finding rules.Finding) bool { return finding.RuleID == "AI-LOG-001" },
		OnFinding: func(rules.Finding) error {
			streamed++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Suppressed == 0 {
		t.Fatal("expected a suppressed finding")
	}
	if streamed != len(result.Findings) {
		t.Fatalf("streamed=%d findings=%d", streamed, len(result.Findings))
	}
	for _, finding := range result.Findings {
		if finding.RuleID == "AI-LOG-001" {
			t.Fatal("suppressed finding remained in result")
		}
	}
}
