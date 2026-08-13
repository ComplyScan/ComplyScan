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
	"github.com/ComplyScan/ComplyScan/internal/policy"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/repositoryanalysis"
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
		Args:          cobra.NoArgs,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if _, err := os.Stat(config.FileName); err == nil {
			return runDefaultSubcommand(cmd, newScanCommand(stdout, build))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", config.FileName, err)
		}
		if _, err := fmt.Fprint(stdout, "Welcome to ComplyScan\n\nNo configuration was found. ComplyScan will inspect this repository before asking setup questions.\n\n"); err != nil {
			return err
		}
		return runDefaultSubcommand(cmd, newSetupCommand(stdout, build))
	}
	root.AddCommand(newScanCommand(stdout, build))
	root.AddCommand(newInventoryCommand(stdout, build))
	root.AddCommand(newGenerateCommand(stdout, build))
	root.AddCommand(newBaselineCommand(stdout))
	root.AddCommand(newInitCommand(stdout))
	root.AddCommand(newSetupCommand(stdout, build))
	root.AddCommand(newProfileCommand(stdout))
	root.AddCommand(newOwnershipCommand(stdout))
	root.AddCommand(newFrameworkCommand(stdout))
	root.AddCommand(newDoctorCommand(stdout, build))
	root.AddCommand(newVerifyCommand(stdout))
	root.AddCommand(newVersionCommand(stdout, build))
	return root
}

func runDefaultSubcommand(parent, command *cobra.Command) error {
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetIn(parent.InOrStdin())
	command.SetOut(parent.OutOrStdout())
	command.SetErr(parent.ErrOrStderr())
	command.SetContext(parent.Context())
	return command.RunE(command, nil)
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
	return newScanCommandWithDiscovery(stdout, build, nil)
}

type scanDiscoverySeed struct {
	Target    string
	Discovery discovery.Result
}

func newScanCommandWithDiscovery(stdout io.Writer, build BuildInfo, seed *scanDiscoverySeed) *cobra.Command {
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
		remoteProviderName        string
		remoteBaseURL             string
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
		quickScan                 bool
		deepScan                  bool
		verbose                   bool
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
			if quickScan && deepScan {
				return errors.New("--quick and --deep cannot be used together")
			}
			// A normal scan uses the analysis provider saved by `complyscan setup`.
			// The legacy --quick flag remains as a hidden compatibility escape hatch
			// for existing automation that explicitly requested model-free analysis.
			if quickScan {
				cfg.AI.Provider = "none"
			}
			if deepScan && cfg.AI.Provider == "none" && !cmd.Flags().Changed("review") {
				return errors.New("--deep requires an AI review provider; run complyscan setup or pass --review")
			}
			previousReviewProvider := cfg.AI.Provider
			if cmd.Flags().Changed("review") {
				cfg.AI.Provider = strings.ToLower(strings.TrimSpace(reviewProvider))
				if isRemoteReviewProvider(cfg.AI.Provider) && previousReviewProvider != cfg.AI.Provider {
					profile, _ := hostedProviderProfileFor(cfg.AI.Provider)
					cfg.AI.Remote = config.RemoteConfig{
						ProviderName: profile.Label, BaseURL: profile.BaseURL,
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
			if cmd.Flags().Changed("provider-name") {
				cfg.AI.Remote.ProviderName = strings.TrimSpace(remoteProviderName)
			}
			if cmd.Flags().Changed("base-url") {
				cfg.AI.Remote.BaseURL = strings.TrimSpace(remoteBaseURL)
			}
			if (cmd.Flags().Changed("ollama-model") || cmd.Flags().Changed("ollama-endpoint")) && cfg.AI.Provider != "ollama" {
				return errors.New("--ollama-model and --ollama-endpoint require --review ollama or ai.provider: ollama")
			}
			if (cmd.Flags().Changed("model") || cmd.Flags().Changed("api-key-env") || cmd.Flags().Changed("provider-name") || cmd.Flags().Changed("base-url")) && !isRemoteReviewProvider(cfg.AI.Provider) {
				return errors.New("--model, --api-key-env, --provider-name, and --base-url require a hosted review provider")
			}
			if (cmd.Flags().Changed("provider-name") || cmd.Flags().Changed("base-url")) && !isOpenAICompatibleProvider(cfg.AI.Provider) {
				return errors.New("--provider-name and --base-url require an OpenAI-compatible review provider")
			}
			if refreshReview && cfg.AI.Provider == "none" {
				return errors.New("--refresh-review requires an enabled advisory review provider")
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("validate review configuration: %w", err)
			}
			if cfg.AI.Provider != "none" {
				if _, _, _, _, _, err := configuredReviewer(cfg.AI); err != nil {
					return fmt.Errorf("configure %s review: %w", reviewProviderLabel(cfg.AI.Provider), err)
				}
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

			engine := scanner.New()
			var result scanner.Result
			if seed != nil && sameScanTarget(seed.Target, target) {
				if outputFormat == "terminal" {
					if _, err := fmt.Fprintln(stdout, "Reusing repository discovery from guided setup."); err != nil {
						return fmt.Errorf("write discovery reuse status: %w", err)
					}
				}
				result, err = engine.ScanDiscovered(cmd.Context(), target, seed.Discovery, scanOptions)
			} else {
				result, err = engine.Scan(cmd.Context(), target, scanOptions)
			}
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
			aiInventory := inventory.NewReport(target, build.Version, inventory.Analyze(result.FullRepository), result.Warnings)
			reportValue.AIInventory = &aiInventory
			gateFindings := append([]rules.Finding(nil), result.Findings...)
			frameworkResults := make([]report.FrameworkResult, 0, len(cfg.Frameworks))
			for _, packID := range cfg.Frameworks {
				pack, loadErr := framework.LoadBuiltin(packID)
				if loadErr != nil {
					return loadErr
				}
				technicalEvidence := framework.Evaluate(pack, cfg.Systems, result.FullRepository)
				technicalEvidence.Target = target
				technicalEvidence.Warnings = append(technicalEvidence.Warnings, result.Warnings...)
				if err := reconciliation.ValidateCoverage(technicalEvidence); err != nil {
					return err
				}
				var assessment profile.AssessmentReport
				var assessmentReference *profile.AssessmentReport
				if pack.Coverage.Framework == profile.FrameworkEUAIAct {
					assessment = profile.AssessEUAIAct(cfg.Systems)
					if len(cfg.Systems) > 0 {
						assessmentReference = &assessment
					}
				}
				evidenceMapping := reconciliation.Build(cfg.Systems, assessment, technicalEvidence, aiInventory, cfg.Ownership)
				frameworkResults = append(frameworkResults, report.FrameworkResult{
					ID: pack.Coverage.Framework, Name: pack.Name, Nature: pack.Coverage.Nature,
					Applicability: assessmentReference, TechnicalEvidence: technicalEvidence, Reconciliation: evidenceMapping,
				})
			}
			reportValue.Frameworks = frameworkResults
			if cfg.RuleEnabled(policy.TechnicalGapRuleID) {
				for _, frameworkResult := range frameworkResults {
					for _, finding := range policy.TechnicalGapFindings(frameworkResult.ID, config.FileName, cfg.Systems, frameworkResult.Reconciliation) {
						if cfg.FindingSuppressed(finding) || baselineLoaded && accepted.Contains(finding.Fingerprint) {
							reportValue.Suppressed++
							continue
						}
						gateFindings = append(gateFindings, finding)
						if rules.SeverityRank(finding.Severity) < rules.SeverityRank(minimumSeverity) {
							continue
						}
						reportValue.Findings = append(reportValue.Findings, finding)
						if outputFormat == "terminal" {
							if err := report.WriteTerminalFinding(stdout, finding, terminalOptions); err != nil {
								return fmt.Errorf("write technical gap finding: %w", err)
							}
						}
					}
				}
				reportValue.Summary = report.Summarize(reportValue.Findings)
			}
			verificationResults := []verification.Report{}
			if len(verificationPlans) > 0 {
				verificationPlans, err = validateVerificationPlans(verificationPlans, combinedFrameworkEvidence(frameworkResults), cfg.Systems)
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
				for index := range frameworkResults {
					reconciliation.AttachExecutionVerifications(&frameworkResults[index].Reconciliation, verificationResults)
				}
			}
			reportValue.Frameworks = frameworkResults
			syncLegacyFrameworkFields(&reportValue)
			var artifacts report.Artifacts
			if resolvedReportDirectory != "" && cfg.AI.Provider != "none" {
				artifacts, err = report.WriteArtifacts(resolvedReportDirectory, reportValue)
				if err != nil {
					return fmt.Errorf("save preliminary scan reports: %w", err)
				}
				if outputFormat == "terminal" {
					if _, err := fmt.Fprintf(stdout, "Preliminary report saved before AI review:\n  %s\n  %s\n\n", artifacts.Markdown, artifacts.JSON); err != nil {
						return fmt.Errorf("write preliminary report paths: %w", err)
					}
				}
			}
			if cfg.AI.Provider != "none" {
				candidateCount := 0
				investigationRequests := make([]providers.TechnicalReviewRequest, len(frameworkResults))
				for index := range frameworkResults {
					investigationRequests[index] = reviewcontext.BuildInvestigations(frameworkResults[index].TechnicalEvidence, result.FullRepository, frameworkResults[index].Reconciliation)
					investigationRequests[index] = reviewcontext.AttachVerifications(investigationRequests[index], verificationResults)
					candidateCount += len(investigationRequests[index].Candidates)
				}
				progressWriter := io.Writer(stdout)
				if outputFormat != "terminal" {
					progressWriter = cmd.ErrOrStderr()
				}
				modelQualified := true
				repositoryAnalysisRequested := cfg.AI.RepositoryAnalysis.Mode != "bounded-only" && len(result.FullRepository.Files) > 0
				if len(visible) > 0 || candidateCount > 0 || repositoryAnalysisRequested {
					if _, err := fmt.Fprintf(progressWriter, "Checking model compatibility before repository review...\n"); err != nil {
						return fmt.Errorf("write model qualification progress: %w", err)
					}
					activity := startConfiguredLLMActivity(progressWriter, cfg.AI, "check compatibility", "Compatibility response received", "Compatibility request failed")
					outcome, qualificationErr := qualifyConfiguredModel(cmd.Context(), cfg.AI, false)
					activity.Finish(qualificationErr)
					if qualificationErr != nil {
						modelQualified = false
						warning := fmt.Sprintf("%s review was incomplete because model qualification failed: %v. Deterministic findings and evidence remain available.", reviewProviderLabel(cfg.AI.Provider), qualificationErr)
						reportValue.Warnings = append(reportValue.Warnings, warning)
						if _, err := fmt.Fprintln(progressWriter, "Warning:", warning); err != nil {
							return fmt.Errorf("write model qualification warning: %w", err)
						}
					} else {
						source := "live"
						if outcome.Result.FromCache {
							source = "cached"
						}
						if _, err := fmt.Fprintf(progressWriter, "Model compatibility: compatible (%s check; not a quality or legal approval).\n\n", source); err != nil {
							return fmt.Errorf("write model qualification result: %w", err)
						}
						if outcome.CacheWarning != nil {
							reportValue.Warnings = append(reportValue.Warnings, "Model compatibility passed but its private cache could not be updated: "+outcome.CacheWarning.Error())
						}
					}
				}
				if modelQualified && outputFormat == "terminal" {
					if _, err := fmt.Fprintf(stdout, "%s advisory review requested for repository-wide reasoning, %d finding(s), and %d bounded technical evidence target(s) with %s...\n\n", reviewProviderLabel(cfg.AI.Provider), len(visible), candidateCount, configuredReviewModel(cfg.AI)); err != nil {
						return fmt.Errorf("write terminal report: %w", err)
					}
					if isRemoteReviewProvider(cfg.AI.Provider) {
						disclosure := "Remote review sends redacted relevant repository text for whole-repository reasoning, plus bounded finding and evidence records, to the selected provider; usage may incur cost."
						if !repositoryAnalysisRequested {
							disclosure = "Remote review sends only bounded, redacted finding and source-context records to the selected provider; usage may incur cost."
						}
						if _, err := fmt.Fprintln(stdout, disclosure); err != nil {
							return fmt.Errorf("write remote review disclosure: %w", err)
						}
					}
				}
				if modelQualified && repositoryAnalysisRequested {
					repositoryReviewStarted := time.Now()
					if _, err := fmt.Fprintf(progressWriter, "Starting repository-wide AI reasoning with %s %q...\n", reviewProviderLabel(cfg.AI.Provider), configuredReviewModel(cfg.AI)); err != nil {
						return fmt.Errorf("write repository analysis progress: %w", err)
					}
					repositoryReview, reviewErr := reviewRepositoryWithProvider(
						cmd.Context(), cfg.AI, result.FullRepository, frameworkEvidenceReports(frameworkResults), cfg.Systems, progressWriter,
					)
					if reviewErr != nil {
						warning := fmt.Sprintf("%s repository-wide analysis was incomplete after %s: %v. Deterministic findings and bounded evidence review remain available.", reviewProviderLabel(cfg.AI.Provider), formatElapsed(time.Since(repositoryReviewStarted)), reviewErr)
						reportValue.Warnings = append(reportValue.Warnings, warning)
						if _, err := fmt.Fprintln(progressWriter, "Warning:", warning); err != nil {
							return fmt.Errorf("write repository analysis warning: %w", err)
						}
					} else {
						reportValue.RepositoryAnalysis = &repositoryReview
						if _, err := fmt.Fprintf(progressWriter, "Repository-wide AI reasoning completed in %s using %s context.\n", formatElapsed(time.Since(repositoryReviewStarted)), repositoryReview.Coverage.Mode); err != nil {
							return fmt.Errorf("write repository analysis completion: %w", err)
						}
						if resolvedReportDirectory != "" {
							artifacts, err = report.WriteArtifacts(resolvedReportDirectory, reportValue)
							if err != nil {
								return fmt.Errorf("checkpoint repository analysis reports: %w", err)
							}
						}
					}
				}
				for index := range frameworkResults {
					if !modelQualified {
						break
					}
					var technicalReview providers.TechnicalReviewResult
					if index == 0 {
						findingReviewStarted := time.Now()
						if len(visible) > 0 {
							if _, err := fmt.Fprintf(progressWriter, "Finding review: %d item(s) with %s %q...\n", len(visible), reviewProviderLabel(cfg.AI.Provider), configuredReviewModel(cfg.AI)); err != nil {
								return fmt.Errorf("write finding review progress: %w", err)
							}
						}
						review, reviewErr := reviewFindingsWithProvider(cmd.Context(), progressWriter, cfg.AI, target, visible)
						if reviewErr != nil {
							warning := fmt.Sprintf("%s finding review was incomplete after %s: %v. Deterministic findings remain unchanged.", reviewProviderLabel(cfg.AI.Provider), formatElapsed(time.Since(findingReviewStarted)), reviewErr)
							reportValue.Warnings = append(reportValue.Warnings, warning)
							if _, err := fmt.Fprintln(progressWriter, "Warning:", warning); err != nil {
								return fmt.Errorf("write review warning: %w", err)
							}
						} else {
							reportValue.Review = &review
							if len(visible) > 0 {
								if _, err := fmt.Fprintf(progressWriter, "Finding review completed in %s.\n", formatElapsed(time.Since(findingReviewStarted))); err != nil {
									return fmt.Errorf("write finding review completion: %w", err)
								}
							}
						}
					}
					technicalReviewStarted := time.Now()
					if len(investigationRequests[index].Candidates) > 0 {
						if _, err := fmt.Fprintf(progressWriter, "Starting %s evidence investigation with up to %d target(s)...\n", frameworkResults[index].Name, len(investigationRequests[index].Candidates)); err != nil {
							return fmt.Errorf("write technical review start: %w", err)
						}
					}
					technicalReview, reviewErr := reviewTechnicalWithProvider(
						cmd.Context(), cfg.AI, frameworkResults[index].TechnicalEvidence, investigationRequests[index], result.FullRepository,
						refreshReview, progressWriter, technicalReviewProgress(progressWriter, cfg.AI.Provider, technicalReviewStarted, time.Now),
					)
					if reviewErr != nil {
						warning := fmt.Sprintf("%s technical evidence review for %s was incomplete after %s: %v. Deterministic evidence remains available.", reviewProviderLabel(cfg.AI.Provider), frameworkResults[index].Name, formatElapsed(time.Since(technicalReviewStarted)), reviewErr)
						reportValue.Warnings = append(reportValue.Warnings, warning)
						if _, err := fmt.Fprintln(progressWriter, "Warning:", warning); err != nil {
							return fmt.Errorf("write technical review warning: %w", err)
						}
						continue
					}
					if len(investigationRequests[index].Candidates) > 0 {
						if _, err := fmt.Fprintf(progressWriter, "%s evidence investigation completed in %s.\n", frameworkResults[index].Name, formatElapsed(time.Since(technicalReviewStarted))); err != nil {
							return fmt.Errorf("write technical review completion: %w", err)
						}
					}
					frameworkResults[index].TechnicalReview = &technicalReview
					reconciliation.AttachTechnicalInvestigations(&frameworkResults[index].Reconciliation, technicalReview)
					if frameworkResults[index].TechnicalEvidence.Pack.ID == framework.EUAIActTechnicalEvidencePackID {
						reportValue.TechnicalReview = &technicalReview
					}
					reportValue.Frameworks = frameworkResults
					syncLegacyFrameworkFields(&reportValue)
					if resolvedReportDirectory != "" {
						artifacts, err = report.WriteArtifacts(resolvedReportDirectory, reportValue)
						if err != nil {
							return fmt.Errorf("checkpoint AI review reports: %w", err)
						}
					}
				}
			}
			reportValue.Frameworks = frameworkResults
			syncLegacyFrameworkFields(&reportValue)
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
				completionWriter := report.WriteTerminalConciseCompletion
				if verbose {
					completionWriter = report.WriteTerminalCompletion
				}
				if err := completionWriter(stdout, reportValue); err != nil {
					return fmt.Errorf("write terminal report: %w", err)
				}
				if artifacts.Markdown != "" {
					if _, err := fmt.Fprintf(stdout, "\nReports saved:\n  Human-readable: %s\n  Evidence bundle: %s\n", artifacts.Markdown, artifacts.JSON); err != nil {
						return fmt.Errorf("write report paths: %w", err)
					}
				}
			}
			if report.MeetsThreshold(gateFindings, cfg.FailOn) {
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
	command.Flags().StringVar(&reviewProvider, "review", "", "advisory review provider: none, ollama, openai, anthropic, gemini, xai, groq, mistral, openrouter, or openai-compatible")
	command.Flags().StringVar(&ollamaModel, "ollama-model", "", "Ollama model name (overrides ai.ollama.model)")
	command.Flags().StringVar(&ollamaEndpoint, "ollama-endpoint", "", "local Ollama base URL (overrides ai.ollama.endpoint)")
	command.Flags().StringVar(&remoteModel, "model", "", "remote-provider model name (overrides ai.remote.model)")
	command.Flags().StringVar(&remoteAPIKeyEnv, "api-key-env", "", "environment-variable name containing the remote-provider API key")
	command.Flags().StringVar(&remoteProviderName, "provider-name", "", "display name for a custom OpenAI-compatible provider")
	command.Flags().StringVar(&remoteBaseURL, "base-url", "", "HTTPS API base URL for an OpenAI-compatible provider")
	command.Flags().StringVar(&reportDirectory, "report-dir", report.DefaultDirectory, "directory for latest.md and latest.json (relative to the scan target)")
	command.Flags().BoolVar(&noReport, "no-report", false, "do not save local Markdown and JSON reports")
	command.Flags().BoolVar(&refreshReview, "refresh-review", false, "ignore cached technical observations and run the configured provider again")
	command.Flags().BoolVar(&quickScan, "quick", false, "run deterministic discovery and checks without AI review")
	command.Flags().BoolVar(&deepScan, "deep", false, "require the configured AI review provider for a deep scan")
	_ = command.Flags().MarkHidden("quick")
	_ = command.Flags().MarkHidden("deep")
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "print full framework, evidence, and advisory-review details in the terminal")
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

func sameScanTarget(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftPath) == filepath.Clean(rightPath)
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

func combinedFrameworkEvidence(results []report.FrameworkResult) framework.TechnicalEvidenceReport {
	combined := framework.TechnicalEvidenceReport{Objectives: []framework.ObjectiveAssessment{}}
	for _, result := range results {
		combined.Objectives = append(combined.Objectives, result.TechnicalEvidence.Objectives...)
	}
	return combined
}

func syncLegacyFrameworkFields(value *report.Report) {
	value.Applicability = nil
	value.TechnicalEvidence = nil
	value.Reconciliation = nil
	value.TechnicalReview = nil
	for index := range value.Frameworks {
		result := &value.Frameworks[index]
		if result.TechnicalEvidence.Pack.ID != framework.EUAIActTechnicalEvidencePackID {
			continue
		}
		value.Applicability = result.Applicability
		value.TechnicalEvidence = &result.TechnicalEvidence
		value.Reconciliation = &result.Reconciliation
		value.TechnicalReview = result.TechnicalReview
		return
	}
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

func reviewFindingsWithProvider(
	ctx context.Context,
	activityOutput io.Writer,
	settings config.AIConfig,
	target string,
	findings []rules.Finding,
) (providers.ReviewResult, error) {
	reviewer, timeout, _, _, _, err := configuredReviewer(settings)
	if err != nil {
		return providers.ReviewResult{}, err
	}
	findingContext, cancelFindings := context.WithTimeout(ctx, timeout)
	defer cancelFindings()
	activity := startConfiguredLLMActivity(activityOutput, settings, "review findings", "Finding-review response received", "Finding-review request failed")
	findingResult, err := reviewer.Review(findingContext, providers.ReviewRequest{
		RepositoryRoot: target, Findings: findings,
	})
	activity.Finish(err)
	if err != nil {
		return providers.ReviewResult{}, fmt.Errorf("%s advisory review: %w", reviewProviderLabel(settings.Provider), err)
	}
	return findingResult, nil
}

func reviewRepositoryWithProvider(
	ctx context.Context,
	settings config.AIConfig,
	repository discovery.Repository,
	evidence []framework.TechnicalEvidenceReport,
	systems []profile.System,
	progressWriter io.Writer,
) (providers.RepositoryAnalysisResult, error) {
	reviewer, _, _, model, kind, err := configuredReviewer(settings)
	if err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	mode := repositoryanalysis.Mode(settings.RepositoryAnalysis.Mode)
	if mode == "bounded-only" {
		return providers.RepositoryAnalysisResult{}, errors.New("repository-wide analysis is disabled by configuration")
	}
	return repositoryanalysis.Run(ctx, reviewer, repository, evidence, systems, repositoryanalysis.Options{
		Mode: mode, MaxInputTokens: settings.RepositoryAnalysis.MaxInputTokens, Provider: kind, Model: model,
		OnProgress: func(progress repositoryanalysis.Progress) error {
			if progress.Completed == 0 {
				return nil
			}
			switch progress.Stage {
			case "full-repository":
				_, err := fmt.Fprintf(progressWriter, "Full repository context analyzed (%d byte(s)).\n", progress.InputBytes)
				return err
			case "subsystem":
				_, err := fmt.Fprintf(progressWriter, "Subsystem %d/%d analyzed: %s\n", progress.Completed, progress.Total, progress.Scope)
				return err
			case "synthesis":
				_, err := fmt.Fprintf(progressWriter, "Synthesis batch %d/%d completed: %s\n", progress.Completed, progress.Total, progress.Scope)
				return err
			default:
				return nil
			}
		},
	})
}

func frameworkEvidenceReports(results []report.FrameworkResult) []framework.TechnicalEvidenceReport {
	evidence := make([]framework.TechnicalEvidenceReport, len(results))
	for index := range results {
		evidence[index] = results[index].TechnicalEvidence
	}
	return evidence
}

func reviewTechnicalWithProvider(
	ctx context.Context,
	settings config.AIConfig,
	evidence framework.TechnicalEvidenceReport,
	investigationRequest providers.TechnicalReviewRequest,
	repository discovery.Repository,
	refresh bool,
	activityOutput io.Writer,
	onProgress func(technicalreview.Progress) error,
) (providers.TechnicalReviewResult, error) {
	reviewer, _, maxFindings, model, kind, err := configuredReviewer(settings)
	if err != nil {
		return providers.TechnicalReviewResult{}, err
	}
	return runTechnicalReview(ctx, reviewer, settings, evidence, investigationRequest, repository, refresh, maxFindings, model, kind, activityOutput, onProgress)
}

func runTechnicalReview(
	ctx context.Context,
	reviewer *providers.OllamaProvider,
	settings config.AIConfig,
	evidence framework.TechnicalEvidenceReport,
	investigationRequest providers.TechnicalReviewRequest,
	repository discovery.Repository,
	refresh bool,
	maxFindings int,
	model string,
	kind providers.Kind,
	activityOutput io.Writer,
	onProgress func(technicalreview.Progress) error,
) (providers.TechnicalReviewResult, error) {
	var cache *technicalreview.Cache
	var err error
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
	activeReviewer := &technicalActivityReviewer{reviewer: reviewer, output: activityOutput, settings: settings}
	technicalResult, err := technicalreview.Run(ctx, activeReviewer, investigationRequest, technicalreview.Options{
		Identity: technicalreview.Identity{
			Provider: kind, Model: model, PromptVersion: providers.TechnicalReviewPromptVersion,
			PackID: evidence.Pack.ID, PackVersion: evidence.Pack.Version, PackDigest: evidence.Pack.Digest,
		},
		Cache: cache, Refresh: refresh, MaxCandidates: maxFindings, MaxPerObjective: 2, OnProgress: onProgress,
		RetrieveFollowUp: func(candidate providers.TechnicalCandidate, plan providers.TechnicalSearchPlan) (providers.TechnicalCandidate, int) {
			return reviewcontext.ApplyFollowUp(candidate, plan, repository)
		},
	})
	if err != nil {
		return providers.TechnicalReviewResult{}, fmt.Errorf("%s technical evidence investigation: %w", reviewProviderLabel(settings.Provider), err)
	}
	if cacheUnavailable {
		technicalResult.Notes = append(technicalResult.Notes, "The local technical review cache was unavailable; review continued without cache reuse.")
	}
	return technicalResult, nil
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
		APIKey: key, BaseURL: settings.Remote.BaseURL, Model: settings.Remote.Model,
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
	case providers.XAI, providers.Groq, providers.Mistral, providers.OpenRouter, providers.Compatible:
		reviewer, err = providers.NewOpenAICompatible(kind, remoteProviderName(settings), options)
	default:
		return nil, 0, 0, "", providers.None, fmt.Errorf("unsupported review provider %q", settings.Provider)
	}
	return reviewer, options.Timeout, options.MaxFindings, options.Model, kind, err
}

func isRemoteReviewProvider(value string) bool {
	_, exists := hostedProviderProfileFor(value)
	return exists
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
		if profile, exists := hostedProviderProfileFor(value); exists {
			return profile.Label
		}
		return value
	}
}

func remoteProviderName(settings config.AIConfig) string {
	if strings.TrimSpace(settings.Remote.ProviderName) != "" {
		return settings.Remote.ProviderName
	}
	return reviewProviderLabel(settings.Provider)
}

func technicalReviewProgress(output io.Writer, provider string, started time.Time, now func() time.Time) func(technicalreview.Progress) error {
	return func(progress technicalreview.Progress) error {
		status := "reviewing with " + reviewProviderLabel(provider)
		if progress.Cached {
			status = "using cached observation"
		}
		scope := "repository-wide"
		if progress.Candidate.SystemID != "" {
			scope = fmt.Sprintf("system %s, %d owned file(s)", progress.Candidate.SystemID, progress.Candidate.RepositoryFiles)
		}
		_, err := fmt.Fprintf(output, "Evidence investigation %d/%d [elapsed %s]: %s — %s [%s] (%s)\n", progress.Current, progress.Total, formatElapsed(now().Sub(started)), progress.Candidate.ObjectiveID, progress.Candidate.Path, scope, status)
		return err
	}
}

func formatElapsed(value time.Duration) string {
	if value < time.Second {
		return "<1s"
	}
	return value.Round(time.Second).String()
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
			allFindings := append([]rules.Finding(nil), result.Findings...)
			if cfg.RuleEnabled(policy.TechnicalGapRuleID) {
				aiInventory := inventory.NewReport(target, "", inventory.Analyze(result.FullRepository), result.Warnings)
				for _, packID := range cfg.Frameworks {
					pack, loadErr := framework.LoadBuiltin(packID)
					if loadErr != nil {
						return loadErr
					}
					evidence := framework.Evaluate(pack, cfg.Systems, result.FullRepository)
					if err := reconciliation.ValidateCoverage(evidence); err != nil {
						return err
					}
					assessment := profile.AssessmentReport{}
					if pack.Coverage.Framework == profile.FrameworkEUAIAct {
						assessment = profile.AssessEUAIAct(cfg.Systems)
					}
					mapping := reconciliation.Build(cfg.Systems, assessment, evidence, aiInventory, cfg.Ownership)
					for _, finding := range policy.TechnicalGapFindings(pack.Coverage.Framework, config.FileName, cfg.Systems, mapping) {
						if !cfg.FindingSuppressed(finding) {
							allFindings = append(allFindings, finding)
						}
					}
				}
			}
			if err := baseline.Write(resolvedDestination, allFindings); err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Wrote %d current findings and confirmed technical gaps to %s\n", len(allFindings), resolvedDestination)
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
