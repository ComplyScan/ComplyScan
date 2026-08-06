// Package ownership maps repository-relative evidence paths to declared AI
// systems. It deliberately keeps repository layout separate from legal and
// operational profile facts.
package ownership

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

type Status string

const (
	StatusAssigned    Status = "assigned"
	StatusShared      Status = "shared"
	StatusConflicting Status = "conflicting"
	StatusUnassigned  Status = "unassigned"
)

// Rule explicitly assigns matching repository paths to one or more declared
// systems. Multiple systems in one rule are an intentional shared ownership
// declaration; separate overlapping rules with different owner sets conflict.
type Rule struct {
	Paths   []string `yaml:"paths" json:"paths"`
	Systems []string `yaml:"systems" json:"systems"`
}

type Resolution struct {
	Path        string   `json:"path"`
	Status      Status   `json:"status"`
	Systems     []string `json:"systems"`
	RuleIndexes []int    `json:"rule_indexes,omitempty"`
}

type matcher interface {
	MatchesPath(string) bool
}

type compiledRule struct {
	systems  []string
	matchers []matcher
}

type Resolver struct {
	rules []compiledRule
}

func Validate(rules []Rule, declaredSystems []string) error {
	if len(rules) > 100 {
		return errors.New("ownership must not exceed 100 rules")
	}
	declared := make(map[string]struct{}, len(declaredSystems))
	for _, system := range declaredSystems {
		declared[system] = struct{}{}
	}
	for index, rule := range rules {
		prefix := fmt.Sprintf("ownership[%d]", index)
		if len(rule.Paths) == 0 || len(rule.Paths) > 50 {
			return fmt.Errorf("%s.paths must contain between 1 and 50 patterns", prefix)
		}
		if len(rule.Systems) == 0 || len(rule.Systems) > 20 {
			return fmt.Errorf("%s.systems must contain between 1 and 20 system IDs", prefix)
		}
		seenPaths := make(map[string]struct{}, len(rule.Paths))
		for _, pattern := range rule.Paths {
			if err := validatePattern(pattern); err != nil {
				return fmt.Errorf("%s.paths: %w", prefix, err)
			}
			if _, duplicate := seenPaths[pattern]; duplicate {
				return fmt.Errorf("%s.paths contains duplicate pattern %q", prefix, pattern)
			}
			seenPaths[pattern] = struct{}{}
		}
		seenSystems := make(map[string]struct{}, len(rule.Systems))
		for _, system := range rule.Systems {
			if _, exists := declared[system]; !exists {
				return fmt.Errorf("%s.systems references undeclared system %q", prefix, system)
			}
			if _, duplicate := seenSystems[system]; duplicate {
				return fmt.Errorf("%s.systems contains duplicate system %q", prefix, system)
			}
			seenSystems[system] = struct{}{}
		}
	}
	return nil
}

func validatePattern(pattern string) error {
	if pattern == "" || strings.TrimSpace(pattern) != pattern || len([]rune(pattern)) > 500 {
		return fmt.Errorf("pattern %q must be non-empty, trimmed, and at most 500 characters", pattern)
	}
	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "!") {
		return fmt.Errorf("pattern %q must be a positive repository-relative pattern", pattern)
	}
	if strings.Contains(pattern, "\\") || strings.ContainsAny(pattern, "\x00\r\n") {
		return fmt.Errorf("pattern %q must use forward slashes and contain no control characters", pattern)
	}
	for _, part := range strings.Split(strings.TrimSuffix(pattern, "/"), "/") {
		if part == "." || part == ".." || part == "" {
			return fmt.Errorf("pattern %q contains an unsafe path segment", pattern)
		}
	}
	return nil
}

func New(rules []Rule) Resolver {
	compiled := make([]compiledRule, 0, len(rules))
	for _, rule := range rules {
		entry := compiledRule{systems: sortedUnique(rule.Systems), matchers: make([]matcher, 0, len(rule.Paths))}
		for _, pattern := range rule.Paths {
			entry.matchers = append(entry.matchers, ignore.CompileIgnoreLines(pattern))
		}
		compiled = append(compiled, entry)
	}
	return Resolver{rules: compiled}
}

func (resolver Resolver) Configured() bool {
	return len(resolver.rules) > 0
}

func (resolver Resolver) Resolve(path string) Resolution {
	normalized := strings.TrimPrefix(filepath.ToSlash(path), "./")
	result := Resolution{Path: normalized, Status: StatusUnassigned, Systems: []string{}, RuleIndexes: []int{}}
	var wantedKey string
	owners := make(map[string]struct{})
	for index, rule := range resolver.rules {
		matched := false
		for _, candidate := range rule.matchers {
			if candidate.MatchesPath(normalized) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		result.RuleIndexes = append(result.RuleIndexes, index)
		key := strings.Join(rule.systems, "\x00")
		if wantedKey == "" {
			wantedKey = key
		} else if wantedKey != key {
			result.Status = StatusConflicting
		}
		for _, system := range rule.systems {
			owners[system] = struct{}{}
		}
	}
	if len(result.RuleIndexes) == 0 {
		return result
	}
	result.Systems = make([]string, 0, len(owners))
	for system := range owners {
		result.Systems = append(result.Systems, system)
	}
	sort.Strings(result.Systems)
	if result.Status == StatusConflicting {
		return result
	}
	result.Status = StatusAssigned
	if len(result.Systems) > 1 {
		result.Status = StatusShared
	}
	return result
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
