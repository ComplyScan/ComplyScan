package reconciliation

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

func TestAttachExecutionVerificationsPreservesMappingAndScopesSystems(t *testing.T) {
	report := Report{Systems: []SystemResult{
		{SystemID: "one", Objectives: []ObjectiveResult{{ObjectiveID: "objective", Mapping: MappingRequirementWithoutEvidence}}},
		{SystemID: "two", Objectives: []ObjectiveResult{{ObjectiveID: "objective", Mapping: MappingRequirementWithoutEvidence}}},
	}}
	AttachExecutionVerifications(&report, []verification.Report{
		{RecipeID: "passing", Status: verification.StatusPassed, Objectives: []string{"objective"}, Systems: []string{"one"}},
		{RecipeID: "failing", Status: verification.StatusFailed, Objectives: []string{"objective"}, Systems: []string{"one"}},
	})
	first := report.Systems[0].Objectives[0]
	if first.Mapping != MappingRequirementWithoutEvidence || first.Verification == nil || first.Verification.Runs != 2 || first.Verification.Passed != 1 || first.Verification.Failed != 1 || first.Verification.Assurance != providers.AssuranceTestEvidenceObserved {
		t.Fatalf("unexpected first objective: %#v", first)
	}
	if report.Systems[1].Objectives[0].Verification != nil || report.Summary.TestEvidenceObserved != 1 {
		t.Fatalf("verification escaped system scope: %#v", report)
	}
}
