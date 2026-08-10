package scanner

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/rules"
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
	fileKinds := make(map[string]discovery.FileKind, len(fullRepository.Files))
	for _, file := range fullRepository.Files {
		fileKinds[file.Path] = file.Kind
	}
	recordFinding := func(finding rules.Finding) error {
		finding.Scope = findingScope(finding.Path, fileKinds[finding.Path])
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

func findingScope(path string, kind discovery.FileKind) rules.FindingScope {
	lower := "/" + strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") || strings.Contains(lower, "/__tests__/") ||
		strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return rules.ScopeTest
	}
	if kind == discovery.KindDocumentation || kind == discovery.KindReadme || strings.Contains(lower, "/docs/") || strings.Contains(lower, "/examples/") {
		return rules.ScopeDocumentation
	}
	switch kind {
	case discovery.KindManifest, discovery.KindDockerfile, discovery.KindGitHubAction, discovery.KindCI, discovery.KindTerraform, discovery.KindEnvTemplate, discovery.KindConfig:
		return rules.ScopeConfiguration
	case discovery.KindSource:
		return rules.ScopeProduction
	default:
		return rules.ScopeUnknown
	}
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
