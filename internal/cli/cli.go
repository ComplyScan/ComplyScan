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

	"github.com/ComplyScan/ComplyScan/internal/baseline"
	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/governance"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/reviewcontext"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/scanner"
	"github.com/ComplyScan/ComplyScan/internal/technicalreview"
	"github.com/ComplyScan/ComplyScan/internal/verification"
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
	root.AddCommand(newSetupCommand(stdout, build))
	root.AddCommand(newProfileCommand(stdout))
	root.AddCommand(newFrameworkCommand(stdout))
	root.AddCommand(newDoctorCommand(stdout, build))
	root.AddCommand(newVerifyCommand(stdout))
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
		remoteModel               string
		remoteAPIKeyEnv           string
		reportDirectory           string
		noReport                  bool
		refreshReview             bool
		verifyRuntime             string
		verifyImage               string
		verifyCommand             string
		verifyArguments           []string
		verifyObjectives          []string
		verifyTimeout             time.Duration
		verifyConfigured          bool
		verifySystems             []string
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
			previousReviewProvider := cfg.AI.Provider
			if cmd.Flags().Changed("review") {
				cfg.AI.Provider = strings.ToLower(strings.TrimSpace(reviewProvider))
				if isRemoteReviewProvider(cfg.AI.Provider) && previousReviewProvider != cfg.AI.Provider {
					cfg.AI.Remote = config.RemoteConfig{
						Model: defaultRemoteModel(cfg.AI.Provider), APIKeyEnv: defaultRemoteAPIKeyEnvironment(cfg.AI.Provider),
						TimeoutSeconds: 360, MaxFindings: 20,
					}
				}
			}
			if cmd.Flags().Changed("ollama-model") {
				cfg.AI.Ollama.Model = strings.TrimSpace(ollamaModel)
			}
			if cmd.Flags().Changed("ollama-endpoint") {
				cfg.AI.Ollama.Endpoint = strings.TrimSpace(ollamaEndpoint)
			}
			if cmd.Flags().Changed("model") {
				cfg.AI.Remote.Model = strings.TrimSpace(remoteModel)
			}
			if cmd.Flags().Changed("api-key-env") {
				cfg.AI.Remote.APIKeyEnv = strings.TrimSpace(remoteAPIKeyEnv)
			}
			if (cmd.Flags().Changed("ollama-model") || cmd.Flags().Changed("ollama-endpoint")) && cfg.AI.Provider != "ollama" {
				return errors.New("--ollama-model and --ollama-endpoint require --review ollama or ai.provider: ollama")
			}
			if (cmd.Flags().Changed("model") || cmd.Flags().Changed("api-key-env")) && !isRemoteReviewProvider(cfg.AI.Provider) {
				return errors.New("--model and --api-key-env require --review openai, anthropic, or gemini")
			}
			if refreshReview && cfg.AI.Provider == "none" {
				return errors.New("--refresh-review requires an enabled advisory review provider")
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
			adHocVerification := cmd.Flags().Changed("verify-runtime") || cmd.Flags().Changed("verify-image") || cmd.Flags().Changed("verify-command") || cmd.Flags().Changed("verify-arg") || cmd.Flags().Changed("verify-objective") || cmd.Flags().Changed("verify-system") || cmd.Flags().Changed("verify-timeout")
			if verifyConfigured && adHocVerification {
				return errors.New("--verify runs configured recipes and cannot be combined with ad-hoc --verify-* options")
			}
			if adHocVerification && (strings.TrimSpace(verifyImage) == "" || strings.TrimSpace(verifyCommand) == "" || len(verifyObjectives) == 0) {
				return errors.New("isolated verification requires --verify-image, --verify-command, and at least one --verify-objective")
			}
			verificationPlans := []verification.Options{}
			if verifyConfigured {
				if cfg.Verification == nil || len(cfg.Verification.Recipes) == 0 {
					return errors.New("--verify requires at least one verification recipe in .complyscan.yml")
				}
				verificationPlans = configuredVerificationOptions(target, cfg.Verification.Recipes)
			} else if adHocVerification {
				verificationPlans = append(verificationPlans, verification.Options{
					RecipeID: "cli-verification", Target: target, Runtime: verifyRuntime, Image: verifyImage, Command: verifyCommand,
					Arguments: verifyArguments, Objectives: verifyObjectives, Systems: verifySystems, Timeout: verifyTimeout,
				})
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
					Findings: findingsScope, TechnicalEvidence: "full-repository", AIInventory: "full-repository", Reconciliation: "full-repository", ChangedSince: changedSince,
					TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
					IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				},
				time.Now(),
				visible,
				result.Warnings,
				result.Suppressed,
			)
			assessment := profile.AssessEUAIAct(cfg.Systems)
			if len(cfg.Systems) > 0 {
				reportValue.Applicability = &assessment
			}
			pack, err := framework.LoadBuiltin(framework.EUAIActTechnicalEvidencePackID)
			if err != nil {
				return err
			}
			technicalEvidence := framework.Evaluate(pack, cfg.Systems, result.FullRepository)
			technicalEvidence.Target = target
			technicalEvidence.Warnings = append(technicalEvidence.Warnings, result.Warnings...)
			reportValue.TechnicalEvidence = &technicalEvidence
			if err := reconciliation.ValidateCoverage(technicalEvidence); err != nil {
				return err
			}
			aiInventory := inventory.NewReport(target, build.Version, inventory.Analyze(result.FullRepository), result.Warnings)
			reportValue.AIInventory = &aiInventory
			evidenceMapping := reconciliation.Build(cfg.Systems, assessment, technicalEvidence, aiInventory, cfg.Ownership)
			reportValue.Reconciliation = &evidenceMapping
			verificationResults := []verification.Report{}
			if len(verificationPlans) > 0 {
				verificationPlans, err = validateVerificationPlans(verificationPlans, technicalEvidence, cfg.Systems)
				if err != nil {
					return err
				}
				progressWriter := cmd.ErrOrStderr()
				if outputFormat == "terminal" {
					progressWriter = stdout
				}
				for index, plan := range verificationPlans {
					if _, err := fmt.Fprintf(progressWriter, "Running isolated verification %d/%d: %s in local image %s with network disabled...\n", index+1, len(verificationPlans), plan.RecipeID, plan.Image); err != nil {
						return fmt.Errorf("write verification progress: %w", err)
					}
					verificationResult, err := verification.Execute(cmd.Context(), plan)
					if err != nil {
						return fmt.Errorf("isolated verification %s: %w", plan.RecipeID, err)
					}
					verificationResults = append(verificationResults, verificationResult)
				}
				reportValue.ExecutionVerifications = verificationResults
				reconciliation.AttachExecutionVerifications(&evidenceMapping, verificationResults)
			}
			if cfg.AI.Provider != "none" {
				investigationRequest := reviewcontext.BuildInvestigations(technicalEvidence, result.FullRepository, evidenceMapping)
				if len(cfg.Systems) <= 1 {
					investigationRequest = reviewcontext.AttachVerifications(investigationRequest, verificationResults)
				}
				candidateCount := len(investigationRequest.Candidates)
				if outputFormat == "terminal" {
					if _, err := fmt.Fprintf(stdout, "%s advisory review requested for %d finding(s) and %d technical evidence investigation target(s) with %s...\n\n", reviewProviderLabel(cfg.AI.Provider), len(visible), candidateCount, configuredReviewModel(cfg.AI)); err != nil {
						return fmt.Errorf("write terminal report: %w", err)
					}
					if isRemoteReviewProvider(cfg.AI.Provider) {
						if _, err := fmt.Fprintln(stdout, "Remote review sends only bounded, redacted finding and source-context records to the selected provider; usage may incur cost."); err != nil {
							return fmt.Errorf("write remote review disclosure: %w", err)
						}
					}
				}
				progressWriter := io.Writer(stdout)
				if outputFormat != "terminal" {
					progressWriter = cmd.ErrOrStderr()
				}
				review, technicalReview, err := reviewWithProvider(
					cmd.Context(), cfg.AI, target, visible, technicalEvidence, investigationRequest, result.FullRepository,
					refreshReview, technicalReviewProgress(progressWriter),
				)
				if err != nil {
					return err
				}
				reportValue.Review = &review
				reportValue.TechnicalReview = &technicalReview
				reconciliation.AttachTechnicalInvestigations(&evidenceMapping, technicalReview)
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
	command.Flags().StringVar(&reviewProvider, "review", "", "advisory review provider: none, ollama, openai, anthropic, or gemini (defaults to configuration)")
	command.Flags().StringVar(&ollamaModel, "ollama-model", "", "Ollama model name (overrides ai.ollama.model)")
	command.Flags().StringVar(&ollamaEndpoint, "ollama-endpoint", "", "local Ollama base URL (overrides ai.ollama.endpoint)")
	command.Flags().StringVar(&remoteModel, "model", "", "remote-provider model name (overrides ai.remote.model)")
	command.Flags().StringVar(&remoteAPIKeyEnv, "api-key-env", "", "environment-variable name containing the remote-provider API key")
	command.Flags().StringVar(&reportDirectory, "report-dir", report.DefaultDirectory, "directory for latest.md and latest.json (relative to the scan target)")
	command.Flags().BoolVar(&noReport, "no-report", false, "do not save local Markdown and JSON reports")
	command.Flags().BoolVar(&refreshReview, "refresh-review", false, "ignore cached technical observations and run the configured provider again")
	command.Flags().BoolVar(&verifyConfigured, "verify", false, "run verification recipes from .complyscan.yml in isolated containers")
	command.Flags().StringVar(&verifyRuntime, "verify-runtime", "docker", "local container runtime for opt-in execution: docker or podman")
	command.Flags().StringVar(&verifyImage, "verify-image", "", "preloaded local container image for opt-in execution")
	command.Flags().StringVar(&verifyCommand, "verify-command", "", "test executable to run without a shell inside the container")
	command.Flags().StringArrayVar(&verifyArguments, "verify-arg", nil, "argument for the isolated test command (repeatable)")
	command.Flags().StringArrayVar(&verifyObjectives, "verify-objective", nil, "technical objective the test supports (repeatable)")
	command.Flags().StringArrayVar(&verifySystems, "verify-system", nil, "configured system the test supports (repeatable; required when multiple systems exist)")
	command.Flags().DurationVar(&verifyTimeout, "verify-timeout", 5*time.Minute, "timeout for opt-in isolated execution (maximum 30m)")
	return command
}

func configuredVerificationOptions(target string, recipes []config.VerificationRecipe) []verification.Options {
	result := make([]verification.Options, 0, len(recipes))
	for _, recipe := range recipes {
		timeoutSeconds := recipe.TimeoutSeconds
		if timeoutSeconds == 0 {
			timeoutSeconds = config.DefaultVerificationTimeoutSeconds
		}
		result = append(result, verification.Options{
			RecipeID: recipe.ID, Target: target, Runtime: recipe.Runtime, Image: recipe.Image, Command: recipe.Command,
			Arguments: append([]string(nil), recipe.Arguments...), Objectives: append([]string(nil), recipe.Objectives...),
			Systems: append([]string(nil), recipe.Systems...), Timeout: time.Duration(timeoutSeconds) * time.Second,
		})
	}
	return result
}

func validateVerificationPlans(plans []verification.Options, evidence framework.TechnicalEvidenceReport, systems []profile.System) ([]verification.Options, error) {
	available := make(map[string]struct{}, len(evidence.Objectives))
	for _, objective := range evidence.Objectives {
		available[objective.ID] = struct{}{}
	}
	availableSystems := make(map[string]struct{}, len(systems))
	for _, system := range systems {
		availableSystems[system.ID] = struct{}{}
	}
	seenRecipes := make(map[string]struct{}, len(plans))
	for index := range plans {
		plan := &plans[index]
		if _, duplicate := seenRecipes[plan.RecipeID]; duplicate {
			return nil, fmt.Errorf("duplicate verification recipe %q", plan.RecipeID)
		}
		seenRecipes[plan.RecipeID] = struct{}{}
		seenObjectives := make(map[string]struct{}, len(plan.Objectives))
		for objectiveIndex, objective := range plan.Objectives {
			objective = strings.TrimSpace(objective)
			if _, found := available[objective]; !found {
				return nil, fmt.Errorf("verification recipe %q has unknown objective %q; inspect available IDs with complyscan framework show", plan.RecipeID, objective)
			}
			if _, duplicate := seenObjectives[objective]; duplicate {
				return nil, fmt.Errorf("verification recipe %q has duplicate objective %q", plan.RecipeID, objective)
			}
			seenObjectives[objective] = struct{}{}
			plan.Objectives[objectiveIndex] = objective
		}
		if len(plan.Systems) == 0 && len(systems) == 1 {
			plan.Systems = []string{systems[0].ID}
		} else if len(plan.Systems) == 0 && len(systems) > 1 {
			return nil, fmt.Errorf("verification recipe %q must declare systems because this repository configures multiple systems", plan.RecipeID)
		}
		seenSystems := make(map[string]struct{}, len(plan.Systems))
		for systemIndex, system := range plan.Systems {
			system = strings.TrimSpace(system)
			if _, found := availableSystems[system]; !found {
				return nil, fmt.Errorf("verification recipe %q has unknown system %q", plan.RecipeID, system)
			}
			if _, duplicate := seenSystems[system]; duplicate {
				return nil, fmt.Errorf("verification recipe %q has duplicate system %q", plan.RecipeID, system)
			}
			seenSystems[system] = struct{}{}
			plan.Systems[systemIndex] = system
		}
	}
	return plans, nil
}

func reviewWithProvider(
	ctx context.Context,
	settings config.AIConfig,
	target string,
	findings []rules.Finding,
	evidence framework.TechnicalEvidenceReport,
	investigationRequest providers.TechnicalReviewRequest,
	repository discovery.Repository,
	refresh bool,
	onProgress func(technicalreview.Progress) error,
) (providers.ReviewResult, providers.TechnicalReviewResult, error) {
	reviewer, timeout, maxFindings, model, kind, err := configuredReviewer(settings)
	if err != nil {
		return providers.ReviewResult{}, providers.TechnicalReviewResult{}, err
	}
	findingContext, cancelFindings := context.WithTimeout(ctx, timeout)
	findingResult, err := reviewer.Review(findingContext, providers.ReviewRequest{
		RepositoryRoot: target, Findings: findings,
	})
	cancelFindings()
	if err != nil {
		return providers.ReviewResult{}, providers.TechnicalReviewResult{}, fmt.Errorf("%s advisory review: %w", reviewProviderLabel(settings.Provider), err)
	}
	var cache *technicalreview.Cache
	cacheUnavailable := false
	cachePath, cachePathErr := technicalreview.DefaultPath()
	if cachePathErr == nil {
		cache, err = technicalreview.Open(cachePath)
		if err != nil {
			cacheUnavailable = true
			cache = nil
		}
	} else {
		cacheUnavailable = true
	}
	technicalResult, err := technicalreview.Run(ctx, reviewer, investigationRequest, technicalreview.Options{
		Identity: technicalreview.Identity{
			Provider: kind, Model: model, PromptVersion: providers.TechnicalReviewPromptVersion,
			PackID: evidence.Pack.ID, PackVersion: evidence.Pack.Version, PackDigest: evidence.Pack.Digest,
		},
		Cache: cache, Refresh: refresh, MaxCandidates: maxFindings, OnProgress: onProgress,
		RetrieveFollowUp: func(candidate providers.TechnicalCandidate, plan providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			return reviewcontext.ApplyFollowUp(candidate, plan, repository)
		},
	})
	if err != nil {
		return providers.ReviewResult{}, providers.TechnicalReviewResult{}, fmt.Errorf("%s technical evidence investigation: %w", reviewProviderLabel(settings.Provider), err)
	}
	if cacheUnavailable {
		technicalResult.Notes = append(technicalResult.Notes, "The local technical review cache was unavailable; review continued without cache reuse.")
	}
	return findingResult, technicalResult, nil
}

func configuredReviewer(settings config.AIConfig) (*providers.OllamaProvider, time.Duration, int, string, providers.Kind, error) {
	if settings.Provider == "ollama" {
		timeout := time.Duration(settings.Ollama.TimeoutSeconds) * time.Second
		reviewer, err := providers.NewOllama(providers.OllamaOptions{
			Endpoint: settings.Ollama.Endpoint, Model: settings.Ollama.Model,
			Timeout: timeout, MaxFindings: settings.Ollama.MaxFindings,
		})
		return reviewer, timeout, settings.Ollama.MaxFindings, settings.Ollama.Model, providers.Ollama, err
	}
	key, exists := os.LookupEnv(settings.Remote.APIKeyEnv)
	if !exists || strings.TrimSpace(key) == "" {
		return nil, 0, 0, "", providers.None, fmt.Errorf("%s is not set; export it before using %s review", settings.Remote.APIKeyEnv, reviewProviderLabel(settings.Provider))
	}
	options := providers.RemoteOptions{
		APIKey: key, Model: settings.Remote.Model,
		Timeout: time.Duration(settings.Remote.TimeoutSeconds) * time.Second, MaxFindings: settings.Remote.MaxFindings,
	}
	var reviewer *providers.OllamaProvider
	var err error
	kind := providers.Kind(settings.Provider)
	switch kind {
	case providers.OpenAI:
		reviewer, err = providers.NewOpenAI(options)
	case providers.Anthropic:
		reviewer, err = providers.NewAnthropic(options)
	case providers.Gemini:
		reviewer, err = providers.NewGemini(options)
	default:
		return nil, 0, 0, "", providers.None, fmt.Errorf("unsupported review provider %q", settings.Provider)
	}
	return reviewer, options.Timeout, options.MaxFindings, options.Model, kind, err
}

func isRemoteReviewProvider(value string) bool {
	return value == "openai" || value == "anthropic" || value == "gemini"
}

func configuredReviewModel(settings config.AIConfig) string {
	if settings.Provider == "ollama" {
		return settings.Ollama.Model
	}
	return settings.Remote.Model
}

func reviewProviderLabel(value string) string {
	switch value {
	case "ollama":
		return "Ollama"
	case "openai":
		return "OpenAI"
	case "anthropic":
		return "Anthropic"
	case "gemini":
		return "Gemini"
	default:
		return value
	}
}

func technicalReviewProgress(output io.Writer) func(technicalreview.Progress) error {
	return func(progress technicalreview.Progress) error {
		status := "reviewing with Ollama"
		if progress.Cached {
			status = "using cached observation"
		}
		_, err := fmt.Fprintf(output, "Evidence investigation %d/%d: %s — %s (%s)\n", progress.Current, progress.Total, progress.Candidate.ObjectiveID, progress.Candidate.Path, status)
		return err
	}
}

func buildTechnicalReviewRequest(evidence framework.TechnicalEvidenceReport, repository discovery.Repository) providers.TechnicalReviewRequest {
	return reviewcontext.Build(evidence, repository)
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
