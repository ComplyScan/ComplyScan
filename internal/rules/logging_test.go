package rules

import (
	"context"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

func TestPromptLoggingRule(t *testing.T) {
	repo := repositoryWithFile("service.py", discovery.KindSource, `
logging.info("request received")
logging.info("prompt=%s", user_prompt)
logger.info("result", response)
`)
	findings, err := (PromptLoggingRule{}).Run(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2: %#v", len(findings), findings)
	}
	if findings[0].StartLine != 3 || findings[0].Confidence != "medium" {
		t.Errorf("unexpected prompt finding: %#v", findings[0])
	}
	if findings[1].Confidence != "low" {
		t.Errorf("unexpected response confidence: %#v", findings[1])
	}
}
