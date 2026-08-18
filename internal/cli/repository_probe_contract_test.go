package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRepositoryReviewDoesNotRepeatQualificationForCapacity(t *testing.T) {
	source, err := os.ReadFile("cli.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func reviewRepositoryWithProvider(")
	end := strings.Index(text[start:], "\nfunc ")
	if start < 0 || end < 0 {
		t.Fatal("could not isolate reviewRepositoryWithProvider source")
	}
	body := text[start : start+end]
	if strings.Contains(body, "qualifyConfiguredModel(") || strings.Contains(body, "ProbeRateLimits:") {
		t.Fatal("repository review still repeats source-free compatibility contracts only to discover capacity")
	}
}
