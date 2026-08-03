package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/baseline"
	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/framework"
	"github.com/1eonardodawinki/ComplyScan/internal/governance"
	"github.com/1eonardodawinki/ComplyScan/internal/inventory"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
	"github.com/1eonardodawinki/ComplyScan/internal/providers"
	"github.com/1eonardodawinki/ComplyScan/internal/report"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
	"github.com/1eonardodawinki/ComplyScan/internal/scanner"
	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

type exitError struct {
	code int
}

func (e *exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func Execute(args []string, stdout, stderr io.Writer, build BuildInfo) int {
	return executeWithInput(args, os.Stdin, stdout, stderr, build)
}

func executeWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer, build BuildInfo) int {
	command := newRootCommand(stdout, stderr, build)
	command.SetIn(stdin)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		var status *exitError
		if errors.As(err, &status) {
			return status.code
		}
		_, _ = fmt.Fprintln(stderr, "Error:", err)
		return 2
	}
	return 0
}

func newRootCommand(stdout, stderr io.Writer, build BuildInfo) *cobra.Command {
	root := &cobra.Command{
		Use:           "complyscan",
		Short:         "Scan repositories for potential AI compliance engineering risks",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newScanCommand(stdout, build))
	root.AddCommand(newInventoryCommand(stdout, build))
	root.AddCommand(newGenerateCommand(stdout, build))
	root.AddCommand(newBaselineCommand(stdout))
	root.AddCommand(newInitCommand(stdout))
	root.AddCommand(newProfileCommand(stdout))
	root.AddCommand(newFrameworkCommand(stdout))
	root.AddCommand(newVersionCommand(stdout, build))
	return root
}

func newGenerateCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	command := &cobra.Command{
		Use:   "generate",
		Short: "Generate reviewable AI governance document scaffolds",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newGenerateDocumentCommand(stdout, build, "ai-system", governance.DefaultAISystemPath, governance.AISystem))
	command.AddCommand(newGenerateDocumentCommand(stdout, build, "risk-assessment", governance.DefaultRiskAssessmentPath, governance.RiskAssessment))
	return command
}

type documentGenerator func(inventory.Report, time.Time) string

func newGenerateDocumentCommand(stdout io.Writer, build BuildInfo, name, defaultOutput string, generate documentGenerator) *cobra.Command {
	var (
		outputPath         string
		configPath         string
		force              bool
		additionalExcludes []string
		trackedOnly        bool
	)
	command := &cobra.Command{
		Use:   name + " [path]",
		Short: "Generate " + strings.ReplaceAll(name, "-", " ") + " documentation",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			cfg, _, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			destination := resolveTargetPath(target, outputPath)
			discovered, err := discovery.Discover(cmd.Context(), target, discovery.Options{
				Exclude:                   withGeneratedReportExclusion(append(append([]string(nil), cfg.Scan.Exclude...), additionalExcludes...)),
				MaxFiles:                  cfg.Scan.MaxFiles,
				MaxTotalBytes:             cfg.Scan.MaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
				OnProgress:                terminalProgress(stdout),
			})
			if err != nil {
				return fmt.Errorf("inventory %q: %w", target, err)
			}
			reportValue := inventory.NewReport(target, build.Version, inventory.Analyze(discovered.Repository), discovered.Warnings)
			if err := governance.Write(destination, generate(reportValue, time.Now()), force); err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Created %s from %d detected component(s); human review is required.\n", destination, reportValue.Summary.Components)
			return err
		},
	}
	command.Flags().StringVarP(&outputPath, "output", "o", defaultOutput, "output file (relative to the target)")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing document")
	command.Flags().StringArrayVar(&additionalExcludes, "exclude", nil, "exclude a path or directory name (repeatable)")
	command.Flags().BoolVar(&trackedOnly, "tracked-only", false, "inspect only files tracked by Git")
	return command
}

func newInventoryCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var (
		format                    string
		configPath                string
		additionalExcludes        []string
		trackedOnly               bool
		includeNestedRepositories bool
		maxFiles                  int
		maxTotalBytes             int64
	)
	command := &cobra.Command{
		Use:   "inventory [path]",
		Short: "Inventory detected AI providers and frameworks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			outputFormat := strings.ToLower(strings.TrimSpace(format))
			if outputFormat != "terminal" && outputFormat != "json" {
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
			cfg, _, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("max-files") && maxFiles <= 0 {
				return errors.New("--max-files must be greater than zero")
			}
			if cmd.Flags().Changed("max-total-bytes") && maxTotalBytes <= 0 {
				return errors.New("--max-total-bytes must be greater than zero")
			}
			effectiveMaxFiles := cfg.Scan.MaxFiles
			if cmd.Flags().Changed("max-files") {
				effectiveMaxFiles = maxFiles
			}
			effectiveMaxTotalBytes := cfg.Scan.MaxTotalBytes
			if cmd.Flags().Changed("max-total-bytes") {
				effectiveMaxTotalBytes = maxTotalBytes
			}
			discoveryOptions := discovery.Options{
				Exclude:                   withGeneratedReportExclusion(append(append([]string(nil), cfg.Scan.Exclude...), additionalExcludes...)),
				MaxFiles:                  effectiveMaxFiles,
				MaxTotalBytes:             effectiveMaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
			}
			if outputFormat == "terminal" {
				if _, err := fmt.Fprintf(stdout, "ComplyScan inventorying %s...\n\n", target); err != nil {
					return fmt.Errorf("write terminal inventory: %w", err)
				}
				discoveryOptions.OnProgress = terminalProgress(stdout)
			}
			discovered, err := discovery.Discover(cmd.Context(), target, discoveryOptions)
			if err != nil {
				return fmt.Errorf("inventory %q: %w", target, err)
			}
			reportValue := inventory.NewReport(target, build.Version, inventory.Analyze(discovered.Repository), discovered.Warnings)
			if outputFormat == "json" {
				return inventory.WriteJSON(stdout, reportValue)
			}
			if err := inventory.WriteTerminal(stdout, reportValue); err != nil {
				return fmt.Errorf("write terminal inventory: %w", err)
			}
			return nil
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().StringArrayVar(&additionalExcludes, "exclude", nil, "exclude a path or directory name (repeatable)")
	command.Flags().BoolVar(&trackedOnly, "tracked-only", false, "inventory only files tracked by Git")
	command.Flags().BoolVar(&includeNestedRepositories, "include-nested-repositories", false, "inventory inside nested Git repositories")
	command.Flags().IntVar(&maxFiles, "max-files", 0, "maximum number of text files to read")
	command.Flags().Int64Var(&maxTotalBytes, "max-total-bytes", 0, "maximum total bytes of text content to read")
	return command
}

func newScanCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var (
		format                    string
		minimum                   string
		configPath                string
		noColor                   bool
		additionalExcludes        []string
		trackedOnly               bool
		includeNestedRepositories bool
		maxFiles                  int
		maxTotalBytes             int64
		baselinePath              string
		noBaseline                bool
		changedSince              string
		reviewProvider            string
		ollamaModel               string
		ollamaEndpoint            string
		reportDirectory           string
		noReport                  bool
	)
	command := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a repository (defaults to the current directory)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			outputFormat := strings.ToLower(strings.TrimSpace(format))
			if outputFormat != "terminal" && outputFormat != "json" && outputFormat != "sarif" {
				return fmt.Errorf("invalid format %q (want terminal, json, or sarif)", format)
			}
			minimumSeverity, err := rules.ParseSeverity(minimum)
			if err != nil {
				return fmt.Errorf("--severity: %w", err)
			}
			cfg, _, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("review") {
				cfg.AI.Provider = strings.ToLower(strings.TrimSpace(reviewProvider))
			}
			if cmd.Flags().Changed("ollama-model") {
				cfg.AI.Ollama.Model = strings.TrimSpace(ollamaModel)
			}
			if cmd.Flags().Changed("ollama-endpoint") {
				cfg.AI.Ollama.Endpoint = strings.TrimSpace(ollamaEndpoint)
			}
			if (cmd.Flags().Changed("ollama-model") || cmd.Flags().Changed("ollama-endpoint")) && cfg.AI.Provider != "ollama" {
				return errors.New("--ollama-model and --ollama-endpoint require --review ollama or ai.provider: ollama")
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("validate review configuration: %w", err)
			}
			if cmd.Flags().Changed("max-files") && maxFiles <= 0 {
				return errors.New("--max-files must be greater than zero")
			}
			if cmd.Flags().Changed("max-total-bytes") && maxTotalBytes <= 0 {
				return errors.New("--max-total-bytes must be greater than zero")
			}
			if noBaseline && cmd.Flags().Changed("baseline") {
				return errors.New("--baseline and --no-baseline cannot be used together")
			}
			if noReport && cmd.Flags().Changed("report-dir") {
				return errors.New("--report-dir and --no-report cannot be used together")
			}
			resolvedReportDirectory := ""
			if !noReport {
				resolvedReportDirectory, err = resolveReportDirectory(target, reportDirectory)
				if err != nil {
					return err
				}
			}

			configuredBaseline := cfg.Baseline
			baselineRequired := cmd.Flags().Changed("baseline")
			if baselineRequired {
				configuredBaseline = baselinePath
			}
			var accepted baseline.File
			baselineLoaded := false
			if !noBaseline && configuredBaseline != "" {
				resolvedBaseline := resolveTargetPath(target, configuredBaseline)
				accepted, err = baseline.Load(resolvedBaseline)
				if err == nil {
					baselineLoaded = true
				} else if baselineRequired || !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}

			terminalOptions := report.TerminalOptions{Color: !noColor && supportsColor(stdout)}
			effectiveMaxFiles := cfg.Scan.MaxFiles
			if cmd.Flags().Changed("max-files") {
				effectiveMaxFiles = maxFiles
			}
			effectiveMaxTotalBytes := cfg.Scan.MaxTotalBytes
			if cmd.Flags().Changed("max-total-bytes") {
				effectiveMaxTotalBytes = maxTotalBytes
			}
			excludes := withGeneratedReportExclusion(append(append([]string(nil), cfg.Scan.Exclude...), additionalExcludes...))
			if resolvedReportDirectory != "" {
				if exclusion := targetExclusion(target, resolvedReportDirectory); exclusion != "" {
					excludes = append(excludes, exclusion)
				}
			}
			if !noBaseline && configuredBaseline != "" {
				if exclusion := targetExclusion(target, configuredBaseline); exclusion != "" {
					excludes = append(excludes, exclusion)
				}
			}
			scanOptions := scanner.Options{
				Exclude:                   excludes,
				MaxFiles:                  effectiveMaxFiles,
				MaxTotalBytes:             effectiveMaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
				ChangedSince:              changedSince,
				RuleEnabled:               cfg.RuleEnabled,
				Suppress: func(finding rules.Finding) bool {
					return cfg.FindingSuppressed(finding) || baselineLoaded && accepted.Contains(finding.Fingerprint)
				},
			}
			if outputFormat == "terminal" {
				scope := ""
				if changedSince != "" {
					scope = fmt.Sprintf(" (files changed since %s; governance remains repository-wide)", changedSince)
				}
				if _, err := fmt.Fprintf(stdout, "ComplyScan scanning %s%s...\n\n", target, scope); err != nil {
					return fmt.Errorf("write terminal report: %w", err)
				}
				scanOptions.OnFinding = func(finding rules.Finding) error {
					if rules.SeverityRank(finding.Severity) < rules.SeverityRank(minimumSeverity) {
						return nil
					}
					return report.WriteTerminalFinding(stdout, finding, terminalOptions)
				}
				scanOptions.OnProgress = terminalProgress(stdout)
			}

			result, err := scanner.New().Scan(cmd.Context(), target, scanOptions)
			if err != nil {
				return fmt.Errorf("scan %q: %w", target, err)
			}
			visible := report.FilterByMinimum(result.Findings, minimumSeverity)
			findingsScope := "full-repository"
			if changedSince != "" {
				findingsScope = "changed-files"
			}
			reportValue := report.NewWithMetadata(
				target,
				report.Tool{Name: "ComplyScan", Version: build.Version, Commit: build.Commit, BuiltAt: build.BuildDate},
				report.ScanScope{
					Findings: findingsScope, TechnicalEvidence: "full-repository", ChangedSince: changedSince,
					TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
					IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				},
				time.Now(),
				visible,
				result.Warnings,
				result.Suppressed,
			)
			if len(cfg.Systems) > 0 {
				assessment := profile.AssessEUAIAct(cfg.Systems)
				reportValue.Applicability = &assessment
			}
			pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
			if err != nil {
				return err
			}
			technicalEvidence := framework.Evaluate(pack, cfg.Systems, result.FullRepository)
			technicalEvidence.Target = target
			technicalEvidence.Warnings = append([]string(nil), result.Warnings...)
			reportValue.TechnicalEvidence = &technicalEvidence
			if cfg.AI.Provider == "ollama" {
				if outputFormat == "terminal" {
					if _, err := fmt.Fprintf(stdout, "Ollama advisory review requested for %d finding(s) with %s...\n\n", len(visible), cfg.AI.Ollama.Model); err != nil {
						return fmt.Errorf("write terminal report: %w", err)
					}
				}
				review, err := reviewWithOllama(cmd.Context(), cfg.AI.Ollama, target, visible)
				if err != nil {
					return err
				}
				reportValue.Review = &review
			}
			var artifacts report.Artifacts
			if resolvedReportDirectory != "" {
				artifacts, err = report.WriteArtifacts(resolvedReportDirectory, reportValue)
				if err != nil {
					return fmt.Errorf("save scan reports: %w", err)
				}
			}
			if outputFormat == "json" {
				if err := report.WriteJSON(stdout, reportValue); err != nil {
					return err
				}
			} else if outputFormat == "sarif" {
				if err := report.WriteSARIF(stdout, reportValue); err != nil {
					return err
				}
			} else {
				if err := report.WriteTerminalCompletion(stdout, reportValue); err != nil {
					return fmt.Errorf("write terminal report: %w", err)
				}
				if artifacts.Markdown != "" {
					if _, err := fmt.Fprintf(stdout, "\nReports saved:\n  Human-readable: %s\n  Evidence bundle: %s\n", artifacts.Markdown, artifacts.JSON); err != nil {
						return fmt.Errorf("write report paths: %w", err)
					}
				}
			}
			if report.MeetsThreshold(result.Findings, cfg.FailOn) {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal, json, or sarif")
	command.Flags().StringVar(&minimum, "severity", "info", "minimum severity to include in output")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors")
	command.Flags().StringArrayVar(&additionalExcludes, "exclude", nil, "exclude a path or directory name (repeatable)")
	command.Flags().BoolVar(&trackedOnly, "tracked-only", false, "scan only files tracked by Git")
	command.Flags().BoolVar(&includeNestedRepositories, "include-nested-repositories", false, "scan inside nested Git repositories")
	command.Flags().IntVar(&maxFiles, "max-files", 0, "maximum number of text files to read")
	command.Flags().Int64Var(&maxTotalBytes, "max-total-bytes", 0, "maximum total bytes of text content to read")
	command.Flags().StringVar(&baselinePath, "baseline", "", "baseline file (relative to the scan target)")
	command.Flags().BoolVar(&noBaseline, "no-baseline", false, "do not apply a configured baseline")
	command.Flags().StringVar(&changedSince, "changed-since", "", "scan code files changed since a Git reference; governance checks remain repository-wide")
	command.Flags().StringVar(&reviewProvider, "review", "", "advisory review provider: none or ollama (defaults to configuration)")
	command.Flags().StringVar(&ollamaModel, "ollama-model", "", "Ollama model name (overrides ai.ollama.model)")
	command.Flags().StringVar(&ollamaEndpoint, "ollama-endpoint", "", "local Ollama base URL (overrides ai.ollama.endpoint)")
	command.Flags().StringVar(&reportDirectory, "report-dir", report.DefaultDirectory, "directory for latest.md and latest.json (relative to the scan target)")
	command.Flags().BoolVar(&noReport, "no-report", false, "do not save local Markdown and JSON reports")
	return command
}

func reviewWithOllama(ctx context.Context, settings config.OllamaConfig, target string, findings []rules.Finding) (providers.ReviewResult, error) {
	reviewer, err := providers.NewOllama(providers.OllamaOptions{
		Endpoint: settings.Endpoint, Model: settings.Model,
		Timeout: time.Duration(settings.TimeoutSeconds) * time.Second, MaxFindings: settings.MaxFindings,
	})
	if err != nil {
		return providers.ReviewResult{}, err
	}
	reviewContext, cancel := context.WithTimeout(ctx, time.Duration(settings.TimeoutSeconds)*time.Second)
	defer cancel()
	result, err := reviewer.Review(reviewContext, providers.ReviewRequest{
		RepositoryRoot: target, Findings: findings,
	})
	if err != nil {
		return providers.ReviewResult{}, fmt.Errorf("Ollama advisory review: %w", err)
	}
	return result, nil
}

func newBaselineCommand(stdout io.Writer) *cobra.Command {
	var configPath, outputPath string
	command := &cobra.Command{
		Use:   "baseline [path]",
		Short: "Record the repository's current findings as a baseline",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			cfg, _, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			destination := outputPath
			if destination == "" {
				destination = cfg.Baseline
			}
			if destination == "" {
				destination = baseline.FileName
			}
			resolvedDestination := resolveTargetPath(target, destination)
			excludes := withGeneratedReportExclusion(append([]string(nil), cfg.Scan.Exclude...))
			if exclusion := targetExclusion(target, destination); exclusion != "" {
				excludes = append(excludes, exclusion)
			}
			result, err := scanner.New().Scan(cmd.Context(), target, scanner.Options{
				Exclude:                   excludes,
				MaxFiles:                  cfg.Scan.MaxFiles,
				MaxTotalBytes:             cfg.Scan.MaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly,
				RuleEnabled:               cfg.RuleEnabled,
				Suppress:                  cfg.FindingSuppressed,
				OnProgress:                terminalProgress(stdout),
			})
			if err != nil {
				return fmt.Errorf("scan %q: %w", target, err)
			}
			if err := baseline.Write(resolvedDestination, result.Findings); err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Wrote %d current findings to %s\n", len(result.Findings), resolvedDestination)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().StringVarP(&outputPath, "output", "o", "", "baseline output file (relative to the scan target)")
	return command
}

func terminalProgress(stdout io.Writer) discovery.ProgressHandler {
	return func(progress discovery.Progress) error {
		if progress.Done {
			_, err := fmt.Fprintf(stdout, "Discovery complete: %d files, %s read\n\n", progress.Stats.FilesRead, formatByteCount(progress.Stats.BytesRead))
			return err
		}
		_, err := fmt.Fprintf(stdout, "Discovery progress: %d files, %s read\n", progress.Stats.FilesRead, formatByteCount(progress.Stats.BytesRead))
		return err
	}
}

func resolveTargetPath(target, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(target, filepath.FromSlash(value)))
}

func resolveReportDirectory(target, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("--report-dir must not be empty")
	}
	if filepath.IsAbs(value) {
		return "", errors.New("--report-dir must be relative to the scan target")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("--report-dir must stay inside the scan target")
	}
	targetPath, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve scan target for reports: %w", err)
	}
	destination, err := filepath.Abs(filepath.Join(targetPath, clean))
	if err != nil {
		return "", fmt.Errorf("resolve report directory: %w", err)
	}
	relative, err := filepath.Rel(targetPath, destination)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("--report-dir must stay inside the scan target")
	}
	return destination, nil
}

func withGeneratedReportExclusion(excludes []string) []string {
	for _, exclusion := range excludes {
		if filepath.ToSlash(filepath.Clean(filepath.FromSlash(exclusion))) == report.DefaultDirectory {
			return excludes
		}
	}
	return append(excludes, report.DefaultDirectory)
}

func targetExclusion(target, value string) string {
	targetPath, err := filepath.Abs(target)
	if err != nil {
		return ""
	}
	resolvedPath, err := filepath.Abs(resolveTargetPath(target, value))
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(targetPath, resolvedPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(relative)
}

func formatByteCount(value int64) string {
	const mebibyte = 1 << 20
	if value < mebibyte {
		return fmt.Sprintf("%.1f KiB", float64(value)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/mebibyte)
}

func newVersionCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build metadata",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(stdout, "ComplyScan %s\ncommit: %s\nbuilt: %s\n", build.Version, build.Commit, build.BuildDate)
			return err
		},
	}
}

func supportsColor(writer io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// ConfigPathForTarget is kept small and exported only for black-box CLI tests.
func ConfigPathForTarget(target string) string {
	return filepath.Join(target, config.FileName)
}
