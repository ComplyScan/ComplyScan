package rules

import (
	"context"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

func TestMissingDocumentationRule(t *testing.T) {
	aiFile := discovery.File{Path: "requirements.txt", Kind: discovery.KindManifest, Content: []byte("openai==1.0\n")}
	rule := MissingDocumentationRule{}

	findings, err := rule.Run(context.Background(), discovery.Repository{Files: []discovery.File{aiFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Severity != SeverityMedium {
		t.Fatalf("unexpected findings: %#v", findings)
	}

	documented := discovery.Repository{Files: []discovery.File{
		aiFile,
		{Path: "docs/model-card.md", Kind: discovery.KindModelCard, Content: []byte("documented")},
	}}
	findings, err = rule.Run(context.Background(), documented)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("documented repository produced findings: %#v", findings)
	}
}
