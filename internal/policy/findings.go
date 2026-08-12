// Package policy turns sufficiently confirmed technical mappings into
// reviewable engineering findings without making a legal compliance verdict.
package policy

import (
	"fmt"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const TechnicalGapRuleID = "AI-CTRL-001"

// TechnicalGapFindings returns blocking technical-evidence gaps only for
// profiles whose factual context has been confirmed by a named human. Unknown
// or provisional applicability remains visible in reconciliation but cannot
// change the process exit code.
func TechnicalGapFindings(frameworkID, configPath string, systems []profile.System, mapping reconciliation.Report) []rules.Finding {
	reviewed := make(map[string]bool, len(systems))
	for _, system := range systems {
		reviewed[system.ID] = system.ProfileReview.Status == profile.ReviewConfirmed
	}
	findings := make([]rules.Finding, 0)
	for _, system := range mapping.Systems {
		if !reviewed[system.SystemID] {
			continue
		}
		for _, objective := range system.Objectives {
			if objective.Requirement != reconciliation.RequirementLikelyRequired || objective.Mapping != reconciliation.MappingRequirementWithoutEvidence {
				continue
			}
			finding := rules.Finding{
				RuleID:      TechnicalGapRuleID,
				Title:       "Likely required technical control has no detected implementation evidence",
				Severity:    rules.SeverityHigh,
				Category:    "technical-compliance-gap",
				Message:     fmt.Sprintf("%s maps %s (%s) as likely required for %s, but the bounded repository scan did not detect configured implementation evidence.", frameworkID, objective.Title, objective.SourceReference, system.SystemName),
				Path:        configPath,
				Evidence:    fmt.Sprintf("framework=%s system=%s objective=%s mapping=%s", frameworkID, system.SystemID, objective.ObjectiveID, objective.Mapping),
				Remediation: "Confirm the applicability decision and repository coverage, then implement or verify the technical control. If the gap is knowingly accepted, baseline or suppress its fingerprint with a documented reason.",
				Confidence:  "medium",
				Scope:       rules.ScopeConfiguration,
			}
			finding.Fingerprint = rules.ComputeFingerprint(finding)
			findings = append(findings, finding)
		}
	}
	return findings
}
