package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	Tool     Tool            `json:"tool"`
	Target   string          `json:"target"`
	Summary  Summary         `json:"summary"`
	Findings []rules.Finding `json:"findings"`
	Warnings []string        `json:"warnings,omitempty"`
}

type TerminalOptions struct {
	Color bool
}

func New(target, version string, findings []rules.Finding, warnings []string) Report {
	if findings == nil {
		findings = []rules.Finding{}
	}
	return Report{
		Tool: Tool{Name: "ComplyScan", Version: version}, Target: target,
		Summary: Summarize(findings), Findings: findings, Warnings: warnings,
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
	issueWord := "issues"
	if report.Summary.Total == 1 {
		issueWord = "issue"
	}
	if _, err := fmt.Fprintf(w, "ComplyScan found %d potential %s\n\n", report.Summary.Total, issueWord); err != nil {
		return err
	}
	for _, finding := range report.Findings {
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
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "Summary: %s\n", summaryText(report.Summary)); err != nil {
		return err
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(w, "Warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
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
