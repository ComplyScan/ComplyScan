package rules

import (
	"context"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
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
	if findings[0].Path != "package.json" || findings[0].StartLine != 1 || len(findings[0].Locations) != 1 {
		t.Fatalf("missing representative location: %#v", findings[0])
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
