package reviewcontext

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

func TestAttachVerificationsAddsOnlyMatchingBoundedObjectiveEvidence(t *testing.T) {
	request := providers.TechnicalReviewRequest{Candidates: []providers.TechnicalCandidate{
		{ObjectiveID: "matching", SourceContexts: []providers.TechnicalSourceContext{}},
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
	if len(request.Candidates[1].SourceContexts) != 0 {
		t.Fatalf("verification escaped objective scope: %#v", request.Candidates[1])
	}
}
