package scanner

import (
	"context"
	"path/filepath"
	"testing"
)

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
		if finding.RuleID == "AI-DOC-001" || finding.RuleID == "AI-RISK-001" {
			t.Fatalf("documented fixture produced missing-evidence finding: %#v", finding)
		}
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
