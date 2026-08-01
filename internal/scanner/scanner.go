package scanner

import (
	"context"
	"fmt"
	"sort"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

type Options struct {
	Exclude     []string
	RuleEnabled func(id string) bool
	OnFinding   rules.FindingEmitter
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
		if streamingRule, ok := rule.(rules.StreamingRule); ok && options.OnFinding != nil {
			err := streamingRule.RunStreaming(ctx, discovered.Repository, func(finding rules.Finding) error {
				result.Findings = append(result.Findings, finding)
				return options.OnFinding(finding)
			})
			if err != nil {
				return Result{}, fmt.Errorf("run rule %s: %w", rule.ID(), err)
			}
			continue
		}

		findings, err := rule.Run(ctx, discovered.Repository)
		if err != nil {
			return Result{}, fmt.Errorf("run rule %s: %w", rule.ID(), err)
		}
		for _, finding := range findings {
			result.Findings = append(result.Findings, finding)
			if options.OnFinding != nil {
				if err := options.OnFinding(finding); err != nil {
					return Result{}, fmt.Errorf("report finding from rule %s: %w", rule.ID(), err)
				}
			}
		}
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
