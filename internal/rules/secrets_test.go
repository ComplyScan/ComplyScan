package rules

import (
	"context"
	"strings"
	"testing"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

func TestHardcodedSecretRuleRedactsAndIgnoresEnvironmentReferences(t *testing.T) {
	secret := "sk-" + "proj-" + "1234567890abcdefghijklmnop"
	repo := repositoryWithFile("app.py", discovery.KindSource, `
client = OpenAI(api_key="`+secret+`")
safe = os.getenv("OPENAI_API_KEY")
also_safe = process.env.OPENAI_API_KEY
`)
	findings, err := (HardcodedSecretRule{}).Run(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1: %#v", len(findings), findings)
	}
	if strings.Contains(findings[0].Evidence, secret) {
		t.Fatal("evidence exposed complete secret")
	}
	if !strings.Contains(findings[0].Evidence, "sk-proj-****mnop") {
		t.Fatalf("unexpected redaction: %q", findings[0].Evidence)
	}
}

func TestRedactSecret(t *testing.T) {
	secret := "sk-ant-" + "api03-" + "abcdefghijklmnopqrstuv"
	if got := RedactSecret(secret); got != "sk-ant-api03-****stuv" {
		t.Fatalf("got %q", got)
	}
	if got := RedactSecret("short"); got != "****" {
		t.Fatalf("got %q", got)
	}
}
