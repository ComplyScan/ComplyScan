package cli

import (
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

func TestFindingsWithinReviewRepositoryExcludesUnrelatedAndPathlessRecords(t *testing.T) {
	repository := discovery.Repository{Files: []discovery.File{
		{Path: "changed.go", Kind: discovery.KindSource},
		{Path: "connected.go", Kind: discovery.KindSource},
	}}
	findings := []rules.Finding{
		{RuleID: "changed", Path: "changed.go"},
		{RuleID: "connected", Path: "connected.go"},
		{RuleID: "unrelated", Path: "unrelated.go"},
		{RuleID: "repository-summary"},
	}

	filtered := findingsWithinReviewRepository(findings, repository)
	if len(filtered) != 2 || filtered[0].RuleID != "changed" || filtered[1].RuleID != "connected" {
		t.Fatalf("finding review crossed the changed-code boundary: %#v", filtered)
	}
}
