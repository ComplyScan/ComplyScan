package reviewcontext

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

func TestAttachVerificationsAddsOnlyMatchingBoundedObjectiveEvidence(t *testing.T) {
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{
		{SystemID: "system", ObjectiveID: "matching", SourceContexts: []providers.TechnicalSourceContext{}},
		{SystemID: "other-system", ObjectiveID: "matching", SourceContexts: []providers.TechnicalSourceContext{}},
		{ObjectiveID: "other", SourceContexts: []providers.TechnicalSourceContext{}},
	}}
	request = AttachVerifications(request, []verification.Report{{
		RecipeID: "tests", Status: verification.StatusPassed, Objectives: []string{"matching"}, Systems: []string{"system"},
		Command: []string{"go", "test", "./..."}, OutputDigest: strings.Repeat("a", 64), Output: strings.Repeat("x", 8_000),
	}})
	if len(request.Candidates[0].SourceContexts) != 1 || request.Candidates[0].SourceContexts[0].Role != "isolated-verification-result" {
		t.Fatalf("matching verification was not attached: %#v", request.Candidates[0])
	}
	if len([]rune(request.Candidates[0].SourceContexts[0].Source)) > maxVerificationContextChars {
		t.Fatal("verification context exceeded its bound")
	}
	if len(request.Candidates[1].SourceContexts) != 0 || len(request.Candidates[2].SourceContexts) != 0 {
		t.Fatalf("verification escaped objective or system scope: %#v", request.Candidates)
	}
}

func TestAttachVerificationsRedactsRecipeMetadataAtContextBoundary(t *testing.T) {
	secret := "sk-proj-" + strings.Repeat("v", 24)
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{{
		ObjectiveID: "matching", SourceContexts: []providers.TechnicalSourceContext{},
	}}}
	request = AttachVerifications(request, []verification.Report{{
		RecipeID: "tests", Status: verification.StatusPassed, Objectives: []string{"matching"},
		Command: []string{"tool", "--token", secret}, OutputDigest: strings.Repeat("a", 64),
	}})
	if len(request.Candidates[0].SourceContexts) != 1 {
		t.Fatalf("verification context missing: %#v", request.Candidates[0])
	}
	source := request.Candidates[0].SourceContexts[0].Source
	if strings.Contains(source, secret) || !strings.Contains(source, "sk-proj-****vvvv") {
		t.Fatalf("verification metadata was not redacted: %q", source)
	}
}
