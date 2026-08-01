package rules

import (
	"context"
	"fmt"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

// Severity describes the potential engineering impact of a finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Finding is a single piece of technical evidence requiring review.
type Finding struct {
	RuleID      string   `json:"rule_id"`
	Title       string   `json:"title"`
	Severity    Severity `json:"severity"`
	Category    string   `json:"category"`
	Message     string   `json:"message"`
	Path        string   `json:"path,omitempty"`
	StartLine   int      `json:"start_line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	Evidence    string   `json:"evidence,omitempty"`
	Remediation string   `json:"remediation"`
	Confidence  string   `json:"confidence"`
}

// Rule is an independently executable deterministic repository check.
type Rule interface {
	ID() string
	Run(ctx context.Context, repo discovery.Repository) ([]Finding, error)
}

// ParseSeverity validates and normalizes a severity name.
func ParseSeverity(value string) (Severity, error) {
	s := Severity(strings.ToLower(strings.TrimSpace(value)))
	switch s {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return s, nil
	default:
		return "", fmt.Errorf("invalid severity %q (want info, low, medium, high, or critical)", value)
	}
}

// SeverityRank returns the ordering used for filters and failure thresholds.
func SeverityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}
