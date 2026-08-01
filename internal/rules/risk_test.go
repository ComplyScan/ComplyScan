package rules

import (
	"context"
	"testing"

	"github.com/complyscan/complyscan/internal/discovery"
)

func TestMissingRiskClassificationRule(t *testing.T) {
	aiFile := discovery.File{Path: "package.json", Kind: discovery.KindManifest, Content: []byte(`{"openai":"1.0"}`)}
	rule := MissingRiskClassificationRule{}

	findings, err := rule.Run(context.Background(), discovery.Repository{Files: []discovery.File{aiFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != SeverityMedium {
		t.Fatalf("unexpected findings: %#v", findings)
	}

	documented := discovery.Repository{Files: []discovery.File{
		aiFile,
		{Path: "docs/ai-risk.md", Kind: discovery.KindRisk, Content: []byte("reviewed")},
	}}
	findings, err = rule.Run(context.Background(), documented)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("risk-assessed repository produced findings: %#v", findings)
	}
}
