package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

// Severity describes the potential engineering impact of a finding.
type Severity string
type FindingScope string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

const (
	ScopeProduction    FindingScope = "production"
	ScopeTest          FindingScope = "test"
	ScopeDocumentation FindingScope = "documentation-example"
	ScopeConfiguration FindingScope = "configuration"
	ScopeUnknown       FindingScope = "unknown"
)

// Finding is a single piece of technical evidence requiring review.
type Finding struct {
	Fingerprint string       `json:"fingerprint"`
	RuleID      string       `json:"rule_id"`
	Title       string       `json:"title"`
	Severity    Severity     `json:"severity"`
	Category    string       `json:"category"`
	Message     string       `json:"message"`
	Path        string       `json:"path,omitempty"`
	StartLine   int          `json:"start_line,omitempty"`
	EndLine     int          `json:"end_line,omitempty"`
	Evidence    string       `json:"evidence,omitempty"`
	Remediation string       `json:"remediation"`
	Confidence  string       `json:"confidence"`
	Scope       FindingScope `json:"scope,omitempty"`
	Occurrences int          `json:"occurrences,omitempty"`
	Locations   []Location   `json:"locations,omitempty"`
}

// ComputeFingerprint returns a stable identity for a finding. Line numbers are
// deliberately excluded so a finding remains baselined when nearby code moves.
func ComputeFingerprint(finding Finding) string {
	path := filepath.ToSlash(filepath.Clean(filepath.FromSlash(finding.Path)))
	if path == "." {
		path = ""
	}
	path = strings.TrimPrefix(path, "./")
	canonical := strings.Join([]string{
		finding.RuleID,
		path,
		finding.Title,
		strings.Join(strings.Fields(finding.Evidence), " "),
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}

// Location is a representative source location for an aggregated finding.
type Location struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
}

// Rule is an independently executable deterministic repository check.
type Rule interface {
	ID() string
	Run(ctx context.Context, repo discovery.Repository) ([]Finding, error)
}

// FindingEmitter receives a finding as soon as a streaming rule discovers it.
type FindingEmitter func(Finding) error

// StreamingRule is an optional extension for rules that can emit findings
// incrementally. Rule remains the stable extension interface for compatibility.
type StreamingRule interface {
	Rule
	RunStreaming(ctx context.Context, repo discovery.Repository, emit FindingEmitter) error
}

// RepositoryWideRule marks governance checks that must retain full-repository
// context even when a scan is limited to files changed since a Git reference.
type RepositoryWideRule interface {
	Rule
	RepositoryWide() bool
}

func collectFindings(run func(FindingEmitter) error) ([]Finding, error) {
	var findings []Finding
	err := run(func(finding Finding) error {
		findings = append(findings, finding)
		return nil
	})
	return findings, err
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
