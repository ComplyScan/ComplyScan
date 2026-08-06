package report

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

type ScanMetadata struct {
	ID        string    `json:"id"`
	CreatedAt string    `json:"created_at"`
	Scope     ScanScope `json:"scope"`
}

type ScanScope struct {
	Findings                  string `json:"findings"`
	TechnicalEvidence         string `json:"technical_evidence"`
	AIInventory               string `json:"ai_inventory"`
	Reconciliation            string `json:"reconciliation"`
	ChangedSince              string `json:"changed_since,omitempty"`
	TrackedOnly               bool   `json:"tracked_only"`
	IncludeNestedRepositories bool   `json:"include_nested_repositories"`
}

type Summary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

type Report struct {
	SchemaVersion     int                                `json:"schema_version"`
	Tool              Tool                               `json:"tool"`
	Scan              ScanMetadata                       `json:"scan"`
	Target            string                             `json:"target"`
	Summary           Summary                            `json:"summary"`
	Findings          []rules.Finding                    `json:"findings"`
	Warnings          []string                           `json:"warnings,omitempty"`
	Suppressed        int                                `json:"suppressed"`
	Applicability     *profile.AssessmentReport          `json:"applicability,omitempty"`
	TechnicalEvidence *framework.TechnicalEvidenceReport `json:"technical_evidence,omitempty"`
	AIInventory       *inventory.Report                  `json:"ai_inventory,omitempty"`
	Reconciliation    *reconciliation.Report             `json:"reconciliation,omitempty"`
	Review            *providers.ReviewResult            `json:"review,omitempty"`
	TechnicalReview   *providers.TechnicalReviewResult   `json:"evidence_investigation,omitempty"`
}

type TerminalOptions struct {
	Color bool
}

func New(target, version string, findings []rules.Finding, warnings []string, suppressed int) Report {
	return NewWithMetadata(
		target,
		Tool{Name: "ComplyScan", Version: version},
		ScanScope{Findings: "full-repository", TechnicalEvidence: "full-repository", AIInventory: "full-repository", Reconciliation: "full-repository"},
		time.Now(),
		findings,
		warnings,
		suppressed,
	)
}

func NewWithMetadata(target string, tool Tool, scope ScanScope, createdAt time.Time, findings []rules.Finding, warnings []string, suppressed int) Report {
	if findings == nil {
		findings = []rules.Finding{}
	}
	if scope.AIInventory == "" {
		scope.AIInventory = "full-repository"
	}
	if scope.Reconciliation == "" {
		scope.Reconciliation = "full-repository"
	}
	created := createdAt.UTC().Format(time.RFC3339Nano)
	identifier := sha256.Sum256([]byte(strings.Join([]string{target, tool.Version, tool.Commit, created}, "\x00")))
	return Report{
		SchemaVersion: 3,
		Tool:          tool,
		Scan: ScanMetadata{
			ID: "scan-" + fmt.Sprintf("%x", identifier[:12]), CreatedAt: created, Scope: scope,
		},
		Target:  target,
		Summary: Summarize(findings), Findings: findings, Warnings: warnings, Suppressed: suppressed,
	}
}

func Summarize(findings []rules.Finding) Summary {
	summary := Summary{Total: len(findings)}
	for _, finding := range findings {
		switch finding.Severity {
		case rules.SeverityCritical:
			summary.Critical++
		case rules.SeverityHigh:
			summary.High++
		case rules.SeverityMedium:
			summary.Medium++
		case rules.SeverityLow:
			summary.Low++
		case rules.SeverityInfo:
			summary.Info++
		}
	}
	return summary
}

func FilterByMinimum(findings []rules.Finding, minimum rules.Severity) []rules.Finding {
	filtered := make([]rules.Finding, 0, len(findings))
	for _, finding := range findings {
		if rules.SeverityRank(finding.Severity) >= rules.SeverityRank(minimum) {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func MeetsThreshold(findings []rules.Finding, threshold rules.Severity) bool {
	for _, finding := range findings {
		if rules.SeverityRank(finding.Severity) >= rules.SeverityRank(threshold) {
			return true
		}
	}
	return false
}

func WriteJSON(w io.Writer, report Report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	return nil
}

func WriteTerminal(w io.Writer, report Report, options TerminalOptions) error {
	if _, err := fmt.Fprintf(w, "ComplyScan found %d potential %s\n\n", report.Summary.Total, issueWord(report.Summary.Total)); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if err := WriteTerminalFinding(w, finding, options); err != nil {
			return err
		}
	}
	if report.Applicability != nil {
		if err := profile.WriteTerminal(w, *report.Applicability); err != nil {
			return err
		}
	}
	if report.AIInventory != nil {
		if err := writeAIInventoryTerminal(w, *report.AIInventory); err != nil {
			return err
		}
	}
	if report.Reconciliation != nil {
		if err := writeReconciliationTerminal(w, *report.Reconciliation); err != nil {
			return err
		}
	}
	if report.TechnicalEvidence != nil {
		if err := framework.WriteTechnicalEvidenceTerminal(w, *report.TechnicalEvidence); err != nil {
			return err
		}
	}
	if report.Review != nil {
		if err := WriteTerminalReview(w, *report.Review); err != nil {
			return err
		}
	}
	if report.TechnicalReview != nil {
		if err := WriteTerminalTechnicalReview(w, *report.TechnicalReview); err != nil {
			return err
		}
	}
	return writeTerminalSummary(w, report)
}

// WriteTerminalFinding renders one finding immediately for streaming scans.
func WriteTerminalFinding(w io.Writer, finding rules.Finding, options TerminalOptions) error {
	label := severityLabel(finding.Severity)
	if options.Color {
		label = colorize(finding.Severity, label)
	}
	if _, err := fmt.Fprintf(w, "%-5s  %s  %s\n", label, finding.RuleID, finding.Title); err != nil {
		return err
	}
	if finding.Path != "" {
		location := finding.Path
		if finding.StartLine > 0 {
			location += ":" + strconv.Itoa(finding.StartLine)
		}
		if _, err := fmt.Fprintf(w, "       %s\n", location); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "       %s\n", finding.Message); err != nil {
		return err
	}
	if finding.Evidence != "" {
		if _, err := fmt.Fprintf(w, "       Evidence: %s\n", finding.Evidence); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// WriteTerminalCompletion closes a streaming report with final counts.
func WriteTerminalCompletion(w io.Writer, report Report) error {
	if report.Applicability != nil {
		if err := profile.WriteTerminal(w, *report.Applicability); err != nil {
			return err
		}
	}
	if report.AIInventory != nil {
		if err := writeAIInventoryTerminal(w, *report.AIInventory); err != nil {
			return err
		}
	}
	if report.Reconciliation != nil {
		if err := writeReconciliationTerminal(w, *report.Reconciliation); err != nil {
			return err
		}
	}
	if report.TechnicalEvidence != nil {
		if err := framework.WriteTechnicalEvidenceTerminal(w, *report.TechnicalEvidence); err != nil {
			return err
		}
	}
	if report.Review != nil {
		if err := WriteTerminalReview(w, *report.Review); err != nil {
			return err
		}
	}
	if report.TechnicalReview != nil {
		if err := WriteTerminalTechnicalReview(w, *report.TechnicalReview); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Scan complete: %d potential %s\n", report.Summary.Total, issueWord(report.Summary.Total)); err != nil {
		return err
	}
	return writeTerminalSummary(w, report)
}

// WriteTerminalReview renders advisory observations separately from findings.
func WriteTerminalReview(w io.Writer, review providers.ReviewResult) error {
	if _, err := fmt.Fprintf(w, "Ollama advisory review (%s): %d of %d finding(s) reviewed\n", review.Model, review.Reviewed, review.InputFindings); err != nil {
		return err
	}
	for _, observation := range review.Observations {
		if _, err := fmt.Fprintf(w, "REVIEW  %-13s %-6s %s\n", observation.Verdict, strings.ToUpper(observation.Confidence), observation.RuleID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        %s\n", observation.Rationale); err != nil {
			return err
		}
		if observation.SuggestedAction != "" {
			if _, err := fmt.Fprintf(w, "        Suggested: %s\n", observation.SuggestedAction); err != nil {
				return err
			}
		}
	}
	for _, note := range review.Notes {
		if _, err := fmt.Fprintf(w, "Review note: %s\n", note); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// WriteTerminalTechnicalReview renders bounded evidence investigations without
// merging them into deterministic evidence or legal applicability status.
func WriteTerminalTechnicalReview(w io.Writer, review providers.TechnicalReviewResult) error {
	if _, err := fmt.Fprintf(w, "Ollama technical evidence investigation (%s): %d of %d target(s) reviewed\n", review.Model, review.Reviewed, review.InputCandidates); err != nil {
		return err
	}
	for _, observation := range review.Observations {
		if _, err := fmt.Fprintf(w, "EVIDENCE %-24s %-23s %s\n", observation.Conclusion, observation.Assurance, observation.ObjectiveID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        Strength: %s; confidence: %s; evidence: %s\n        %s\n", observation.Strength, strings.ToUpper(observation.Confidence), observation.EvidenceFingerprint, observation.Rationale); err != nil {
			return err
		}
		for _, claim := range observation.SupportingEvidence {
			if _, err := fmt.Fprintf(w, "        Supports: %s — %s\n", locationText(claim.Path, claim.Line), claim.Summary); err != nil {
				return err
			}
		}
		for _, claim := range observation.ContradictoryEvidence {
			if _, err := fmt.Fprintf(w, "        Contradicts: %s — %s\n", locationText(claim.Path, claim.Line), claim.Summary); err != nil {
				return err
			}
		}
		for _, missing := range observation.MissingEvidence {
			if _, err := fmt.Fprintf(w, "        Missing: %s\n", missing); err != nil {
				return err
			}
		}
		if observation.GuardrailNote != "" {
			if _, err := fmt.Fprintf(w, "        Guardrail: %s (model returned %s)\n", observation.GuardrailNote, observation.ModelStrength); err != nil {
				return err
			}
		}
		for _, question := range observation.UnresolvedQuestions {
			if _, err := fmt.Fprintf(w, "        Unresolved: %s\n", question); err != nil {
				return err
			}
		}
		if observation.SuggestedReview != "" {
			if _, err := fmt.Fprintf(w, "        Suggested: %s\n", observation.SuggestedReview); err != nil {
				return err
			}
		}
	}
	for _, note := range review.Notes {
		if _, err := fmt.Fprintf(w, "Evidence investigation note: %s\n", note); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeTerminalSummary(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Summary: %s\n", summaryText(report.Summary)); err != nil {
		return err
	}
	if report.Suppressed > 0 {
		if _, err := fmt.Fprintf(w, "Suppressed: %d accepted or baselined %s\n", report.Suppressed, issueWord(report.Suppressed)); err != nil {
			return err
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(w, "Warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func issueWord(count int) string {
	if count == 1 {
		return "issue"
	}
	return "issues"
}

func severityLabel(severity rules.Severity) string {
	switch severity {
	case rules.SeverityCritical:
		return "CRIT"
	case rules.SeverityHigh:
		return "HIGH"
	case rules.SeverityMedium:
		return "MED"
	case rules.SeverityLow:
		return "LOW"
	default:
		return "INFO"
	}
}

func colorize(severity rules.Severity, value string) string {
	code := "36"
	switch severity {
	case rules.SeverityCritical:
		code = "35"
	case rules.SeverityHigh:
		code = "31"
	case rules.SeverityMedium:
		code = "33"
	case rules.SeverityLow:
		code = "34"
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func summaryText(summary Summary) string {
	parts := make([]string, 0, 5)
	appendCount := func(count int, name string) {
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, name))
		}
	}
	appendCount(summary.Critical, "critical")
	appendCount(summary.High, "high")
	appendCount(summary.Medium, "medium")
	appendCount(summary.Low, "low")
	appendCount(summary.Info, "info")
	if len(parts) == 0 {
		return "no findings"
	}
	return strings.Join(parts, ", ")
}
