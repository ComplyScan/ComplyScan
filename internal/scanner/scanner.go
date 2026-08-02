package scanner

import (
	"context"
	"fmt"
	"sort"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

type Options struct {
	Exclude                   []string
	MaxFiles                  int
	MaxTotalBytes             int64
	IncludeNestedRepositories bool
	TrackedOnly               bool
	ChangedSince              string
	RuleEnabled               func(id string) bool
	Suppress                  func(rules.Finding) bool
	OnFinding                 rules.FindingEmitter
	OnProgress                discovery.ProgressHandler
}

type Result struct {
	Repository     discovery.Repository
	FullRepository discovery.Repository
	Findings       []rules.Finding
	Warnings       []string
	Suppressed     int
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
	discovered, err := discovery.Discover(ctx, target, discovery.Options{
		Exclude:                   options.Exclude,
		MaxFiles:                  options.MaxFiles,
		MaxTotalBytes:             options.MaxTotalBytes,
		IncludeNestedRepositories: options.IncludeNestedRepositories,
		TrackedOnly:               options.TrackedOnly,
		OnProgress:                options.OnProgress,
	})
	if err != nil {
		return Result{}, err
	}

	fullRepository := discovered.Repository
	scopedRepository := fullRepository
	if options.ChangedSince != "" {
		changed, err := discovery.ChangedPaths(ctx, target, options.ChangedSince)
		if err != nil {
			return Result{}, err
		}
		scopedRepository = filterRepository(fullRepository, changed)
	}
	result := Result{Repository: scopedRepository, FullRepository: fullRepository, Warnings: discovered.Warnings}
	recordFinding := func(finding rules.Finding) error {
		finding.Fingerprint = rules.ComputeFingerprint(finding)
		if options.Suppress != nil && options.Suppress(finding) {
			result.Suppressed++
			return nil
		}
		result.Findings = append(result.Findings, finding)
		if options.OnFinding != nil {
			return options.OnFinding(finding)
		}
		return nil
	}
	scopedContext := rules.WithRepositoryAnalysis(ctx, scopedRepository)
	fullContext := scopedContext
	if options.ChangedSince != "" {
		fullContext = rules.WithRepositoryAnalysis(ctx, fullRepository)
	}
	for _, rule := range e.rules {
		if options.RuleEnabled != nil && !options.RuleEnabled(rule.ID()) {
			continue
		}
		ruleRepository := scopedRepository
		ruleContext := scopedContext
		if repositoryWide, ok := rule.(rules.RepositoryWideRule); ok && repositoryWide.RepositoryWide() {
			ruleRepository = fullRepository
			ruleContext = fullContext
		}
		if streamingRule, ok := rule.(rules.StreamingRule); ok && options.OnFinding != nil {
			err := streamingRule.RunStreaming(ruleContext, ruleRepository, func(finding rules.Finding) error {
				return recordFinding(finding)
			})
			if err != nil {
				return Result{}, fmt.Errorf("run rule %s: %w", rule.ID(), err)
			}
			continue
		}

		findings, err := rule.Run(ruleContext, ruleRepository)
		if err != nil {
			return Result{}, fmt.Errorf("run rule %s: %w", rule.ID(), err)
		}
		for _, finding := range findings {
			if err := recordFinding(finding); err != nil {
				return Result{}, fmt.Errorf("report finding from rule %s: %w", rule.ID(), err)
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

func filterRepository(repo discovery.Repository, paths map[string]struct{}) discovery.Repository {
	filtered := discovery.Repository{Root: repo.Root, Files: make([]discovery.File, 0, len(paths))}
	for _, file := range repo.Files {
		if _, ok := paths[file.Path]; ok {
			filtered.Files = append(filtered.Files, file)
		}
	}
	return filtered
}
