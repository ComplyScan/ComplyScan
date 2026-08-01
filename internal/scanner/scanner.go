package scanner

import (
	"context"
	"fmt"
	"sort"

	"github.com/complyscan/complyscan/internal/discovery"
	"github.com/complyscan/complyscan/internal/rules"
)

type Options struct {
	Exclude     []string
	RuleEnabled func(id string) bool
}

type Result struct {
	Repository discovery.Repository
	Findings   []rules.Finding
	Warnings   []string
}

type Engine struct {
	rules []rules.Rule
}

func New(ruleSet ...rules.Rule) *Engine {
	if len(ruleSet) == 0 {
		ruleSet = rules.DefaultRules()
	}
	return &Engine{rules: ruleSet}
}

func (e *Engine) Scan(ctx context.Context, target string, options Options) (Result, error) {
	discovered, err := discovery.Discover(ctx, target, discovery.Options{Exclude: options.Exclude})
	if err != nil {
		return Result{}, err
	}

	result := Result{Repository: discovered.Repository, Warnings: discovered.Warnings}
	for _, rule := range e.rules {
		if options.RuleEnabled != nil && !options.RuleEnabled(rule.ID()) {
			continue
		}
		findings, err := rule.Run(ctx, discovered.Repository)
		if err != nil {
			return Result{}, fmt.Errorf("run rule %s: %w", rule.ID(), err)
		}
		result.Findings = append(result.Findings, findings...)
	}

	sort.SliceStable(result.Findings, func(i, j int) bool {
		left, right := result.Findings[i], result.Findings[j]
		if rules.SeverityRank(left.Severity) != rules.SeverityRank(right.Severity) {
			return rules.SeverityRank(left.Severity) > rules.SeverityRank(right.Severity)
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.StartLine < right.StartLine
	})
	return result, nil
}
