package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/spf13/cobra"
)

const defaultSetupModel = "qwen3.5:9b"

var listRemoteModels = providers.ListModels

type setupOptions struct {
	configPath         string
	forceInteractive   bool
	nonInteractive     bool
	advanced           bool
	reviewProvider     string
	ollamaModel        string
	remoteModel        string
	remoteAPIKeyEnv    string
	remoteProviderName string
	remoteBaseURL      string
	frameworks         []string
	allowRemoteReview  bool
	pullModel          bool
	skipModelPull      bool
	qualifyModel       bool
	installOllama      bool
	skipOllamaInstall  bool
	skipScan           bool
	detailedGuidance   bool
}

func newSetupCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var options setupOptions
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Inspect the repository and configure its unified compliance scan",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.forceInteractive && options.nonInteractive {
				return errors.New("--interactive and --non-interactive cannot be used together")
			}
			if options.pullModel && options.skipModelPull {
				return errors.New("--pull-model and --skip-model-pull cannot be used together")
			}
			if options.installOllama && options.skipOllamaInstall {
				return errors.New("--install-ollama and --skip-ollama-install cannot be used together")
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			info, err := os.Stat(target)
			if err != nil {
				return fmt.Errorf("inspect setup target %q: %w", target, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("setup target %q is not a directory", target)
			}
			interactive := options.forceInteractive || (!options.nonInteractive && isInteractiveReader(cmd.InOrStdin()))
			if !interactive && !options.nonInteractive {
				return errors.New("setup requires a terminal; use --interactive when piping answers or --non-interactive for automation")
			}
			return runSetup(cmd, stdout, build, target, interactive, options)
		},
	}
	command.Flags().StringVar(&options.configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&options.forceInteractive, "interactive", false, "run the setup wizard even when input is redirected")
	command.Flags().BoolVar(&options.nonInteractive, "non-interactive", false, "create or update configuration without asking questions")
	command.Flags().BoolVar(&options.advanced, "advanced", false, "collect the complete system, data, deployment, and reviewed-applicability profile")
	command.Flags().StringVar(&options.reviewProvider, "review", "", "advisory review provider: none, ollama, openai, anthropic, gemini, xai, groq, mistral, openrouter, or openai-compatible")
	command.Flags().StringVar(&options.ollamaModel, "ollama-model", "", "Ollama model name")
	command.Flags().StringVar(&options.remoteModel, "model", "", "remote-provider model name")
	command.Flags().StringVar(&options.remoteAPIKeyEnv, "api-key-env", "", "environment-variable name containing the remote-provider API key")
	command.Flags().StringVar(&options.remoteProviderName, "provider-name", "", "display name for a custom OpenAI-compatible provider")
	command.Flags().StringVar(&options.remoteBaseURL, "base-url", "", "HTTPS API base URL for an OpenAI-compatible provider")
	command.Flags().StringSliceVar(&options.frameworks, "framework", nil, "built-in technical evidence pack to enable (repeatable)")
	command.Flags().BoolVar(&options.allowRemoteReview, "allow-remote-review", false, "confirm that bounded repository context may be sent to the selected remote provider")
	command.Flags().BoolVar(&options.pullModel, "pull-model", false, "download the configured Ollama model (requires --review ollama in non-interactive mode)")
	command.Flags().BoolVar(&options.skipModelPull, "skip-model-pull", false, "configure Ollama without offering to download the model")
	command.Flags().BoolVar(&options.qualifyModel, "qualify-model", false, "run the bounded synthetic model compatibility check (automatic in interactive setup; remote providers may charge)")
	command.Flags().BoolVar(&options.installOllama, "install-ollama", false, "install Ollama when it is missing (requires explicit use in non-interactive mode)")
	command.Flags().BoolVar(&options.skipOllamaInstall, "skip-ollama-install", false, "do not offer to install Ollama when it is missing")
	command.Flags().BoolVar(&options.skipScan, "skip-scan", false, "do not offer to run the first scan")
	command.Flags().BoolVar(&options.detailedGuidance, "detailed-guidance", false, "deprecated compatibility flag; setup explanations are always visible")
	return command
}

func runSetup(cmd *cobra.Command, stdout io.Writer, build BuildInfo, target string, interactive bool, options setupOptions) error {
	cfg, path, err := config.Resolve(target, options.configPath)
	if err != nil {
		return err
	}
	existed := path != ""
	if path == "" {
		if options.configPath != "" {
			path = options.configPath
		} else {
			path = filepath.Join(target, config.FileName)
		}
	}

	prompt := newPromptSession(cmd.InOrStdin(), stdout)
	if interactive {
		prompt.backAvailable = true
	}
	if err := prompt.sectionTitle("ComplyScan setup", false); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Repository: %s\n\n", target); err != nil {
		return err
	}

	var repositorySummary setupRepositorySummary
	modelReady := true
	reviewConfigured := false
	scanMode := setupScanNone
	resumeStage := setupDraftStage("")
	draftPath := ""
	draftSaved := false
	if interactive && terminalFile(cmd.InOrStdin()) {
		draftPath, err = defaultSetupDraftPath(target)
		if err != nil {
			_ = prompt.status(setupStatusReview, "Automatic setup recovery is unavailable: "+err.Error())
			draftPath = ""
		} else {
			stored, found, loadErr := loadSetupDraft(draftPath, target, time.Now())
			if loadErr != nil {
				_ = prompt.status(setupStatusReview, "The previous setup draft could not be used; recovery will retry at the next checkpoint: "+loadErr.Error())
			} else if found {
				resume := false
				for {
					resume, err = promptSetupDraftResume(prompt, stored)
					if !errors.Is(err, errPromptBack) {
						break
					}
				}
				if err != nil {
					return err
				}
				if resume {
					cfg = stored.Config
					modelReady = stored.ModelReady
					scanMode = stored.ScanMode
					resumeStage = stored.Stage
					draftSaved = true
				} else if removeErr := removeSetupDraft(draftPath); removeErr != nil {
					return removeErr
				}
			}
		}
	}
	if options.skipScan {
		scanMode = setupScanNone
	}
	if interactive {
		totalSteps := 5
		if err := setupStepTitle(prompt, 1, totalSteps, "Repository inspection", false); err != nil {
			return err
		}
		summary, inspectErr := inspectRepositoryForSetup(cmd.Context(), prompt, target, cfg, build)
		if inspectErr != nil {
			return inspectErr
		}
		repositorySummary = summary
		if err := setupStepTitle(prompt, 2, totalSteps, "Analysis, privacy, and model", true); err != nil {
			return err
		}
		if setupDraftStageRank(resumeStage) >= setupDraftStageRank(setupDraftAnalysis) {
			if err := prompt.status(setupStatusReady, "Resumed the saved analysis and privacy selection."); err != nil {
				return err
			}
			reviewConfigured = true
		} else {
			modelReady, err = configureSetupReview(cmd.Context(), prompt, stdout, &cfg, true, options)
			if err != nil {
				return err
			}
			reviewConfigured = true
			draftSaved = checkpointSetupDraft(prompt, draftPath, target, setupDraftAnalysis, cfg, scanMode, modelReady) || draftSaved
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
		if setupDraftStageRank(resumeStage) >= setupDraftStageRank(setupDraftContext) {
			if err := prompt.status(setupStatusReady, "Resumed the saved technical mappings and repository profile."); err != nil {
				return err
			}
		} else {
			if options.advanced {
				if err := setupStepTitle(prompt, 3, totalSteps, "Technical mappings", false); err != nil {
					return err
				}
				if err := configureFrameworkSelection(prompt, &cfg, true, options.frameworks); err != nil {
					return err
				}
				if err := setupStepTitle(prompt, 4, totalSteps, "Detailed system profile and evidence ownership", true); err != nil {
					return err
				}
				if err := collectAdvancedSetupContext(prompt, target, &cfg, summary, true); err != nil {
					return err
				}
			} else {
				if err := setupStepTitle(prompt, 3, totalSteps, "Repository-assisted system context", false); err != nil {
					return err
				}
				draft := draftProfileForSetup(cmd.Context(), stdout, target, cfg, summary, modelReady)
				if err := collectRepositoryAssistedSetupContext(prompt, target, &cfg, summary, draft); err != nil {
					return err
				}
				if err := setupStepTitle(prompt, 4, totalSteps, "Technical mappings and applicability", true); err != nil {
					return err
				}
				for {
					err = configureFrameworkSelection(prompt, &cfg, true, options.frameworks)
					if !errors.Is(err, errPromptBack) {
						if err != nil {
							return err
						}
						break
					}
					if err := setupStepTitle(prompt, 3, totalSteps, "Repository-assisted system context", true); err != nil {
						return err
					}
					if err := collectRepositoryAssistedSetupContext(prompt, target, &cfg, summary, draft); err != nil {
						return err
					}
					if err := setupStepTitle(prompt, 4, totalSteps, "Technical mappings and applicability", true); err != nil {
						return err
					}
				}
				applyFrameworksToSystems(cfg.Systems, cfg.Frameworks)
				if len(cfg.Systems) > 0 {
					index := 0
					if frameworkEnabled(cfg.Frameworks, framework.EUAIActTechnicalEvidencePackID) {
						err = collectRelevantEUApplicabilityContext(prompt, &cfg.Systems[index], time.Now(), draft)
					} else {
						err = collectNonEUTechnicalContext(prompt, &cfg.Systems[index], draft)
					}
					if err != nil {
						return err
					}
				}
			}
			draftSaved = checkpointSetupDraft(prompt, draftPath, target, setupDraftContext, cfg, scanMode, modelReady) || draftSaved
		}
	} else if !existed {
		if err := configureFrameworkSelection(prompt, &cfg, false, options.frameworks); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(stdout, "Non-interactive setup: no system profile was collected."); err != nil {
			return err
		}
	} else if len(options.frameworks) > 0 {
		if err := configureFrameworkSelection(prompt, &cfg, false, options.frameworks); err != nil {
			return err
		}
	}
	if interactive {
		if err := setupStepTitle(prompt, 5, 5, "Review, save, and first scan", true); err != nil {
			return err
		}
	}
	configureReview := !reviewConfigured && (!interactive || options.skipScan || scanMode == setupScanDeep || setupReviewExplicit(options))
	if configureReview {
		modelReady, err = configureSetupReview(cmd.Context(), prompt, stdout, &cfg, interactive, options)
		if err != nil {
			return err
		}
	}
	if interactive {
		draftSaved = checkpointSetupDraft(prompt, draftPath, target, setupDraftReview, cfg, scanMode, modelReady) || draftSaved
		save, reviewErr := reviewSetupBeforeSave(cmd.Context(), prompt, stdout, target, &cfg, options, repositorySummary, &scanMode, &modelReady)
		if reviewErr != nil {
			return reviewErr
		}
		draftSaved = checkpointSetupDraft(prompt, draftPath, target, setupDraftReview, cfg, scanMode, modelReady) || draftSaved
		if !save {
			message := "\nSetup cancelled; no configuration file was written."
			if draftSaved {
				message += " Run `complyscan setup` again to resume from the last saved checkpoint."
			}
			_, err := fmt.Fprintln(stdout, message)
			return err
		}
	}
	if err := config.Write(path, cfg, existed); err != nil {
		return err
	}
	if draftPath != "" {
		if removeErr := removeSetupDraft(draftPath); removeErr != nil {
			_ = prompt.status(setupStatusReview, "Configuration was saved, but its recovery draft could not be removed: "+removeErr.Error())
		}
	}
	if err := ensureReportGitIgnore(target); err != nil {
		return fmt.Errorf("saved %s but could not ignore generated reports: %w", path, err)
	}
	if interactive {
		repositorySummary.Discovery, err = refreshSetupDiscovery(repositorySummary.Discovery, target,
			setupGeneratedFile{Path: path, Kind: discovery.KindConfig},
			setupGeneratedFile{Path: filepath.Join(target, ".gitignore"), Kind: discovery.KindOtherText},
		)
		if err != nil {
			return fmt.Errorf("refresh setup repository snapshot: %w", err)
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	if err := prompt.status(setupStatusReady, fmt.Sprintf("Saved %s with %d system profile(s).", path, len(cfg.Systems))); err != nil {
		return err
	}

	if !interactive || options.skipScan || scanMode == setupScanNone {
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan %s\n", shellQuote(target))
		return err
	}
	return runFirstScan(cmd, stdout, build, target, scanMode, repositorySummary.Discovery)
}

func collectAdvancedSetupContext(prompt promptSession, target string, cfg *config.Config, summary setupRepositorySummary, confirmReplace bool) error {
	system, err := collectSystemProfileWithPrompt(prompt, target, time.Now(), cfg.Frameworks...)
	if err != nil {
		return err
	}
	index := systemIndex(cfg.Systems, system.ID)
	if index >= 0 {
		replace := true
		if confirmReplace {
			if err := explainSetupQuestion(prompt, "replace-profile"); err != nil {
				return err
			}
			replace, err = prompt.confirm(fmt.Sprintf("Replace existing system profile %q", system.ID), true)
			if err != nil {
				return err
			}
		}
		if replace {
			cfg.Systems[index] = system
		}
	} else {
		cfg.Systems = append(cfg.Systems, system)
	}
	return offerOwnershipSetup(prompt, cfg)
}

func collectRepositoryAssistedSetupContext(prompt promptSession, target string, cfg *config.Config, summary setupRepositorySummary, draft setupProfileDraft) error {
	var existing *profile.System
	if len(cfg.Systems) > 0 {
		existing = &cfg.Systems[0]
		if len(cfg.Systems) > 1 {
			if err := prompt.status(setupStatusReview, fmt.Sprintf("Updating the first of %d existing system profiles; use `complyscan setup --advanced` to manage multiple systems.", len(cfg.Systems))); err != nil {
				return err
			}
		}
	}
	system, err := collectBasicSystemProfile(prompt, target, time.Now(), summary, draft, existing)
	if err != nil {
		return err
	}
	index := systemIndex(cfg.Systems, system.ID)
	if index >= 0 {
		cfg.Systems[index] = system
	} else if existing != nil {
		cfg.Systems[0] = system
	} else {
		cfg.Systems = append(cfg.Systems, system)
	}
	return nil
}

type setupReviewAction string

const (
	setupReviewRunScan  setupReviewAction = "Save and run ComplyScan"
	setupReviewSaveOnly setupReviewAction = "Save without scanning"
)

func reviewSetupBeforeSave(ctx context.Context, prompt promptSession, stdout io.Writer, target string, cfg *config.Config, options setupOptions, summary setupRepositorySummary, scanMode *setupScanMode, modelReady *bool) (bool, error) {
	for {
		if err := writeSetupReviewSummary(prompt, *cfg, *modelReady); err != nil {
			return false, err
		}
		actions := make([]setupReviewAction, 0, 2)
		defaultAction := setupReviewRunScan
		if options.skipScan {
			actions = append(actions, setupReviewSaveOnly)
			defaultAction = setupReviewSaveOnly
		} else {
			actions = append(actions, setupReviewRunScan)
			actions = append(actions, setupReviewSaveOnly)
		}
		if err := explainSetupQuestion(prompt, "scan-mode"); err != nil {
			return false, err
		}
		action, err := promptChoice(prompt, "Finish setup", defaultAction, actions...)
		if errors.Is(err, errPromptBack) {
			if err := prompt.sectionTitle("Technical mappings", true); err != nil {
				return false, err
			}
			for {
				err = configureFrameworkSelection(prompt, cfg, true, nil)
				if !errors.Is(err, errPromptBack) {
					break
				}
				if err := prompt.sectionTitle("Analysis and privacy mode", true); err != nil {
					return false, err
				}
				*modelReady, err = configureSetupReview(ctx, prompt, stdout, cfg, true, setupReviewRevisitOptions(options))
				if err != nil {
					return false, err
				}
				if err := prompt.sectionTitle("Technical mappings", true); err != nil {
					return false, err
				}
			}
			if err != nil {
				return false, err
			}
			applyFrameworksToSystems(cfg.Systems, cfg.Frameworks)
			continue
		}
		if err != nil {
			return false, err
		}
		switch action {
		case setupReviewRunScan:
			*scanMode = setupScanDeep
			if cfg.AI.Provider != "none" {
				if _, err := fmt.Fprintln(stdout, "\nThe scan will use the configured AI analysis when relevant. If the model is unavailable, deterministic analysis still completes and reports the interruption."); err != nil {
					return false, err
				}
			}
			return true, nil
		case setupReviewSaveOnly:
			*scanMode = setupScanNone
			return true, nil
		}
	}
}

func setupReviewRevisitOptions(options setupOptions) setupOptions {
	options.reviewProvider = ""
	options.ollamaModel = ""
	options.remoteModel = ""
	options.remoteAPIKeyEnv = ""
	options.remoteProviderName = ""
	options.remoteBaseURL = ""
	options.allowRemoteReview = false
	options.pullModel = false
	options.qualifyModel = false
	return options
}

func writeSetupReviewSummary(prompt promptSession, cfg config.Config, modelReady bool) error {
	if err := prompt.sectionTitle("Review setup", true); err != nil {
		return err
	}
	analysis := "Fast technical analysis (no model)"
	if cfg.AI.Provider == "ollama" {
		analysis = fmt.Sprintf("Experimental local Ollama — %s", cfg.AI.Ollama.Model)
	} else if cfg.AI.Provider != "none" {
		analysis = fmt.Sprintf("%s cloud — %s", reviewProviderLabel(cfg.AI.Provider), cfg.AI.Remote.Model)
	}
	frameworks := make([]string, 0, len(cfg.Frameworks))
	for _, id := range cfg.Frameworks {
		switch id {
		case framework.EUAIActTechnicalEvidencePackID:
			frameworks = append(frameworks, "EU AI Act technical evidence")
		case framework.NISTAIRMFTechnicalEvidencePackID:
			frameworks = append(frameworks, "NIST AI RMF technical evidence")
		default:
			frameworks = append(frameworks, id)
		}
	}
	systems := "none"
	if len(cfg.Systems) > 0 {
		names := make([]string, 0, len(cfg.Systems))
		for _, system := range cfg.Systems {
			names = append(names, fmt.Sprintf("%s (%s)", system.Name, system.ID))
		}
		systems = strings.Join(names, ", ")
	}
	ownership := "single-system inference"
	if len(cfg.Systems) == 0 {
		ownership = "not configured"
	} else if len(cfg.Systems) > 1 {
		ownership = fmt.Sprintf("%d path rule(s)", len(cfg.Ownership))
	}
	analysisStatus := setupStatusReady
	if cfg.AI.Provider != "none" && !modelReady {
		analysisStatus = setupStatusReview
	}
	if err := prompt.status(analysisStatus, "Analysis: "+analysis); err != nil {
		return err
	}
	if err := prompt.status(setupStatusReady, "Technical mappings: "+strings.Join(frameworks, ", ")); err != nil {
		return err
	}
	systemStatus := setupStatusMissing
	if len(cfg.Systems) > 0 {
		systemStatus = setupStatusReady
		for _, system := range cfg.Systems {
			if system.ProfileReview.Status != profile.ReviewConfirmed {
				systemStatus = setupStatusReview
				break
			}
		}
	}
	reportTargetLabel := "Report target: "
	if len(cfg.Systems) > 1 {
		reportTargetLabel = "Report targets: "
	}
	if err := prompt.status(systemStatus, reportTargetLabel+systems); err != nil {
		return err
	}
	ownershipStatus := setupStatusReady
	if len(cfg.Systems) == 0 || (len(cfg.Systems) > 1 && len(cfg.Ownership) == 0) {
		ownershipStatus = setupStatusMissing
	}
	if ownership == "single-system inference" {
		ownership = "all repository evidence maps to the single report target"
	}
	if err := prompt.status(ownershipStatus, "Evidence association: "+ownership); err != nil {
		return err
	}
	_, err := fmt.Fprintln(prompt.output)
	return err
}

func setupStepTitle(prompt promptSession, current, total int, title string, leadingBlank bool) error {
	if prompt.step != nil {
		prompt.step.current = current
		prompt.step.total = total
	}
	return prompt.sectionTitle(fmt.Sprintf("Step %d of %d — %s", current, total, title), leadingBlank)
}

func setupReviewExplicit(options setupOptions) bool {
	return options.reviewProvider != "" || options.ollamaModel != "" || options.remoteModel != "" || options.remoteAPIKeyEnv != "" || options.remoteProviderName != "" || options.remoteBaseURL != "" ||
		options.allowRemoteReview || options.pullModel || options.skipModelPull || options.qualifyModel || options.installOllama || options.skipOllamaInstall
}

func configureSetupReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	for {
		ready, err := configureSetupReviewOnce(ctx, prompt, stdout, cfg, interactive, options)
		if errors.Is(err, errPromptBack) && interactive && strings.TrimSpace(options.reviewProvider) == "" {
			continue
		}
		return ready, err
	}
}

func configureSetupReviewOnce(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := strings.ToLower(strings.TrimSpace(options.reviewProvider))
	if provider == "" && interactive {
		if err := explainSetupQuestion(prompt, "review-provider"); err != nil {
			return false, err
		}
		var err error
		provider, err = promptAnalysisProvider(prompt, cfg.AI.Provider)
		if err != nil {
			return false, err
		}
	}
	if provider == "" {
		provider = cfg.AI.Provider
	}
	if provider != "none" && provider != "ollama" && !isRemoteReviewProvider(provider) {
		return false, fmt.Errorf("invalid review provider %q", provider)
	}
	if options.ollamaModel != "" && provider != "ollama" {
		return false, errors.New("--ollama-model requires --review ollama or an interactive Ollama selection")
	}
	if options.pullModel && provider != "ollama" {
		return false, errors.New("--pull-model requires --review ollama")
	}
	if options.installOllama && provider != "ollama" {
		return false, errors.New("--install-ollama requires --review ollama")
	}
	if options.remoteModel != "" && !isRemoteReviewProvider(provider) {
		return false, errors.New("--model requires a hosted review provider")
	}
	if options.remoteAPIKeyEnv != "" && !isRemoteReviewProvider(provider) {
		return false, errors.New("--api-key-env requires a hosted review provider")
	}
	if options.allowRemoteReview && !isRemoteReviewProvider(provider) {
		return false, errors.New("--allow-remote-review requires a hosted review provider")
	}
	if (options.remoteProviderName != "" || options.remoteBaseURL != "") && !isOpenAICompatibleProvider(provider) {
		return false, errors.New("--provider-name and --base-url require an OpenAI-compatible review provider")
	}
	if options.qualifyModel && provider == "none" {
		return false, errors.New("--qualify-model requires an advisory review provider")
	}
	cfg.AI.Provider = provider
	if provider == "none" {
		if _, err := fmt.Fprintln(stdout, "AI-assisted analysis disabled. Fast deterministic scanning remains available."); err != nil {
			return false, err
		}
		return true, nil
	}
	if isRemoteReviewProvider(provider) {
		return configureRemoteReview(ctx, prompt, stdout, cfg, interactive, options)
	}

	ollamaPath, _ := exec.LookPath("ollama")
	model := strings.TrimSpace(options.ollamaModel)
	if model == "" && interactive {
		installed := []ollamaInstalledModel{}
		if ollamaPath != "" {
			installed = ollamaInstalledModels(ctx, ollamaPath)
		}
		if err := prompt.sectionTitle("Experimental local model setup", true); err != nil {
			return false, err
		}
		if _, err := fmt.Fprintln(stdout, "  Local model review is an advanced experimental path. Small general-purpose models may miss connected code or produce unreliable questionnaire drafts; no local model is currently approved as the standard ComplyScan reviewer."); err != nil {
			return false, err
		}
		if err := explainSetupQuestion(prompt, "ollama-model"); err != nil {
			return false, err
		}
		var err error
		modelPrompt := prompt
		modelPrompt.backAvailable = true
		model, err = promptOllamaModel(modelPrompt, setupModelDefault(cfg.AI.Ollama.Model), installed)
		if err != nil {
			return false, err
		}
	}
	if model == "" {
		model = setupModelDefault(cfg.AI.Ollama.Model)
	}
	cfg.AI.Ollama.Model = model
	if interactive {
		if _, err := fmt.Fprintf(stdout, "Estimated local resources for %q: %s\n", model, ollamaResourceEstimate(model)); err != nil {
			return false, err
		}
	}

	var err error
	if ollamaPath == "" {
		shouldInstall := options.installOllama
		if interactive && !options.skipOllamaInstall && !options.installOllama {
			if _, writeErr := fmt.Fprintln(stdout, "Ollama was not found on PATH. Installation may download software, change system packages, and request system privileges."); writeErr != nil {
				return false, writeErr
			}
			if explainErr := explainSetupQuestion(prompt, "install-ollama"); explainErr != nil {
				return false, explainErr
			}
			confirmPrompt := prompt
			confirmPrompt.backAvailable = true
			shouldInstall, err = confirmPrompt.confirm("Install and start Ollama now", true)
			if err != nil {
				return false, err
			}
		}
		if shouldInstall {
			ollamaPath, err = installOllama(ctx, stdout)
			if err != nil {
				if _, writeErr := fmt.Fprintf(stdout, "Automatic Ollama installation did not complete: %v\n", err); writeErr != nil {
					return false, writeErr
				}
			}
		}
		if ollamaPath == "" {
			if _, writeErr := fmt.Fprintln(stdout, "Install Ollama from https://ollama.com/download, then rerun `complyscan setup` or pull the selected model manually."); writeErr != nil {
				return false, writeErr
			}
			return false, nil
		}
	}
	ready := ollamaModelInstalled(ctx, ollamaPath, model)
	if ready {
		if _, err := fmt.Fprintf(stdout, "Ollama model %q is already installed.\n", model); err != nil {
			return false, err
		}
		return finishSetupModelQualification(ctx, stdout, cfg.AI, interactive || options.qualifyModel)
	}
	shouldPull := options.pullModel
	if interactive && !options.skipModelPull && !options.pullModel {
		if explainErr := explainSetupQuestion(prompt, "download-model"); explainErr != nil {
			return false, explainErr
		}
		var confirmErr error
		confirmPrompt := prompt
		confirmPrompt.backAvailable = true
		shouldPull, confirmErr = confirmPrompt.confirm(fmt.Sprintf("Download Ollama model %q now", model), true)
		if confirmErr != nil {
			return false, confirmErr
		}
	}
	if !shouldPull {
		if _, err := fmt.Fprintf(stdout, "Model not downloaded. When ready, run: ollama pull %s\n", shellQuote(model)); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := fmt.Fprintf(stdout, "Downloading %s with Ollama; live progress is shown below...\n", model); err != nil {
		return false, err
	}
	pullStarted := time.Now()
	command := exec.CommandContext(ctx, ollamaPath, "pull", model)
	command.Stdout = stdout
	command.Stderr = stdout
	if err := command.Run(); err != nil {
		if _, writeErr := fmt.Fprintf(stdout, "Model download did not complete after %s: %v\nStart Ollama with `ollama serve`, then run: ollama pull %s\n", formatElapsed(time.Since(pullStarted)), err, shellQuote(model)); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	if _, err := fmt.Fprintf(stdout, "Model download completed in %s.\n", formatElapsed(time.Since(pullStarted))); err != nil {
		return false, err
	}
	return finishSetupModelQualification(ctx, stdout, cfg.AI, interactive || options.qualifyModel)
}

func promptAnalysisProvider(prompt promptSession, current string) (string, error) {
	const (
		hostedOption = "Cloud AI review — recommended; selected models using your API key"
		fastOption   = "Fast technical analysis — finds known code signals but cannot judge whether requirements are satisfied"
		localOption  = "Experimental local AI — advanced Ollama setup; quality is not assured"
	)
	defaultMode := hostedOption
	if current == "ollama" {
		defaultMode = localOption
	}
	for {
		selected, err := promptChoice(prompt, "Analysis mode", defaultMode, hostedOption, localOption, fastOption)
		if err != nil {
			return "", err
		}
		switch selected {
		case localOption:
			return "ollama", nil
		case fastOption:
			return "none", nil
		}
		hostedPrompt := prompt
		hostedPrompt.backAvailable = true
		provider, err := promptHostedProvider(hostedPrompt, current)
		if errors.Is(err, errPromptBack) {
			continue
		}
		return provider, err
	}
}

func promptHostedProvider(prompt promptSession, current string) (string, error) {
	profiles := standardHostedProviderProfiles()
	choices := make([]terminalChoice, 0, len(profiles))
	for _, profile := range profiles {
		label := profile.SetupLabel
		if label == "" {
			label = profile.Label
		}
		choices = append(choices, terminalChoice{Label: label, Value: profile.ID})
	}
	defaultProvider := current
	if !isRemoteReviewProvider(defaultProvider) {
		defaultProvider = "openai"
	}
	return chooseSetupOption(prompt, "Hosted provider", choices, defaultProvider)
}

func configureRemoteReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := cfg.AI.Provider
	providerProfile, exists := hostedProviderProfileFor(provider)
	if !exists {
		return false, fmt.Errorf("unsupported hosted review provider %q", provider)
	}
	if interactive {
		if err := prompt.sectionTitle("Hosted model setup", true); err != nil {
			return false, err
		}
		if !providerProfile.StandardSetup {
			if _, err := fmt.Fprintln(stdout, "  This provider is outside ComplyScan's standard cloud shortlist and has no maintained live quality benchmark. Continue only as an explicit experimental configuration."); err != nil {
				return false, err
			}
		}
	}
	providerName := strings.TrimSpace(options.remoteProviderName)
	baseURL := strings.TrimSpace(options.remoteBaseURL)
	if provider != customCompatibleProvider {
		providerName = providerProfile.Label
		baseURL = providerProfile.BaseURL
	}
	allowed := options.allowRemoteReview
	keyEnvironment := strings.TrimSpace(options.remoteAPIKeyEnv)
	if keyEnvironment == "" {
		keyEnvironment = defaultRemoteAPIKeyEnvironment(provider)
	}
	model := strings.TrimSpace(options.remoteModel)
	if model == "" {
		model = defaultRemoteModel(provider)
	}

	if interactive {
		steps := make([]setupPromptStep, 0, 5)
		if provider == customCompatibleProvider && options.remoteProviderName == "" {
			steps = append(steps, func(step promptSession) error {
				if err := explainSetupQuestion(step, "remote-provider-name"); err != nil {
					return err
				}
				value, err := step.text("Provider name", providerName)
				if err == nil {
					providerName = value
				}
				return err
			})
		}
		if provider == customCompatibleProvider && options.remoteBaseURL == "" {
			steps = append(steps, func(step promptSession) error {
				if err := explainSetupQuestion(step, "remote-base-url"); err != nil {
					return err
				}
				defaultURL := baseURL
				if defaultURL == "" {
					defaultURL = "https://"
				}
				value, err := step.text("API base URL", defaultURL)
				if err == nil {
					baseURL = value
				}
				return err
			})
		}
		if !options.allowRemoteReview {
			steps = append(steps, func(step promptSession) error {
				if err := explainSetupQuestion(step, "remote-disclosure"); err != nil {
					return err
				}
				candidate := config.AIConfig{Provider: provider, Remote: config.RemoteConfig{ProviderName: providerName, BaseURL: baseURL}}
				value, err := step.confirm(fmt.Sprintf("Allow bounded repository context to be sent to %s", remoteProviderName(candidate)), allowed)
				if err == nil {
					allowed = value
				}
				return err
			})
		}
		if options.remoteAPIKeyEnv == "" {
			steps = append(steps, func(step promptSession) error {
				if err := explainSetupQuestion(step, "api-key-env"); err != nil {
					return err
				}
				value, err := step.text("API key environment-variable name", keyEnvironment)
				if err == nil {
					keyEnvironment = value
				}
				return err
			})
		}
		if options.remoteModel == "" {
			steps = append(steps, func(step promptSession) error {
				if err := explainSetupQuestion(step, "remote-model"); err != nil {
					return err
				}
				key, keyAvailable := os.LookupEnv(keyEnvironment)
				key = strings.TrimSpace(key)
				var value string
				var err error
				if keyAvailable && key != "" {
					value, err = promptRemoteModelCatalogue(ctx, step, provider, config.RemoteConfig{
						ProviderName: providerName, BaseURL: baseURL, TimeoutSeconds: 360,
					}, key)
				} else {
					value, err = promptRemoteModel(step, provider)
				}
				if err == nil {
					model = value
				}
				return err
			})
		}
		if err := runSetupPromptSteps(prompt, true, steps...); err != nil {
			return false, err
		}
	}
	if !allowed {
		if !interactive {
			return false, errors.New("remote review requires --allow-remote-review in non-interactive setup")
		}
		cfg.AI.Provider = "none"
		if _, err := fmt.Fprintln(stdout, "Remote review not enabled. Deterministic local scanning remains available."); err != nil {
			return false, err
		}
		return true, nil
	}
	cfg.AI.Remote = config.RemoteConfig{
		ProviderName: providerName, BaseURL: baseURL, Model: model, APIKeyEnv: keyEnvironment, TimeoutSeconds: 360, MaxFindings: 20,
	}
	if err := cfg.AI.Remote.ValidateForProvider(provider); err != nil {
		return false, fmt.Errorf("remote review configuration: %w", err)
	}
	key, keyAvailable := os.LookupEnv(keyEnvironment)
	keyAvailable = keyAvailable && strings.TrimSpace(key) != ""
	if !keyAvailable {
		if _, err := fmt.Fprintf(stdout, "%s is not currently set. Add it to your shell or CI secret store before scanning; the key itself is never written to .complyscan.yml.\n", keyEnvironment); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := fmt.Fprintf(stdout, "%s review configured with model %q. The credential was found in %s and was not saved.\n", remoteProviderName(cfg.AI), model, keyEnvironment); err != nil {
		return false, err
	}
	return finishSetupModelQualification(ctx, stdout, cfg.AI, interactive || options.qualifyModel)
}

func promptRemoteModelCatalogue(ctx context.Context, prompt promptSession, provider string, remote config.RemoteConfig, apiKey string) (string, error) {
	kind := providers.Kind(provider)
	models, err := listRemoteModels(ctx, providers.ModelListOptions{
		Provider: kind, Label: remoteProviderName(config.AIConfig{Provider: provider, Remote: remote}),
		APIKey: apiKey, BaseURL: remote.BaseURL, Timeout: 15 * time.Second,
	})
	if err != nil {
		fallback := "Showing the ComplyScan model shortlist instead."
		if profile, exists := hostedProviderProfileFor(provider); exists && !profile.StandardSetup {
			fallback = "Showing suggested experimental models and exact-ID entry instead."
		}
		if _, writeErr := fmt.Fprintf(prompt.output, "  Live model catalogue unavailable: %v\n  %s\n", err, fallback); writeErr != nil {
			return "", writeErr
		}
		return promptRemoteModel(prompt, provider)
	}
	profile, _ := hostedProviderProfileFor(provider)
	if !profile.StandardSetup {
		return promptExperimentalRemoteModelCatalogue(prompt, provider, models)
	}
	available := make(map[string]providers.RemoteModel, len(models))
	for _, model := range models {
		available[strings.ToLower(model.ID)] = model
	}
	choices := make([]terminalChoice, 0, len(profile.Models))
	for _, candidate := range profile.Models {
		model, found := available[strings.ToLower(candidate.ID)]
		if !found {
			continue
		}
		label := candidate.ID + " · " + hostedModelStatus(candidate)
		if model.DisplayName != "" && !strings.EqualFold(model.DisplayName, model.ID) {
			label = model.DisplayName + " · " + label
		}
		choices = append(choices, terminalChoice{Label: label, Value: candidate.ID})
	}
	if len(choices) == 0 {
		if _, err := fmt.Fprintln(prompt.output, "  None of ComplyScan's shortlisted models were returned for this API key. Showing the exact supported IDs so you can confirm account access or choose another provider."); err != nil {
			return "", err
		}
		return promptRemoteModel(prompt, provider)
	}
	defaultModel := choices[0].Value
	if _, err := fmt.Fprintf(prompt.output, "  Found %d shortlisted model(s) available to this API key. Only the maintained shortlist is shown.\n", len(choices)); err != nil {
		return "", err
	}
	selected, err := chooseSetupOption(prompt, "Hosted model", choices, defaultModel)
	if err != nil {
		return "", err
	}
	return selected, nil
}

func promptExperimentalRemoteModelCatalogue(prompt promptSession, provider string, models []providers.RemoteModel) (string, error) {
	if len(models) == 0 {
		return promptRemoteModel(prompt, provider)
	}
	choices := make([]terminalChoice, 0, len(models)+1)
	for _, model := range models {
		label := model.ID
		if model.DisplayName != "" && !strings.EqualFold(model.DisplayName, model.ID) {
			label = model.DisplayName + " · " + model.ID
		}
		choices = append(choices, terminalChoice{Label: label, Value: model.ID})
	}
	choices = append(choices, terminalChoice{Label: "Exact model ID · Experimental custom model", Value: customModelChoice})
	selected, err := chooseSetupOption(prompt, "Experimental hosted model", choices, choices[0].Value)
	if err != nil {
		return "", err
	}
	if selected == customModelChoice {
		return promptCustomModel(prompt, "Exact experimental model ID")
	}
	return selected, nil
}

func promptRemoteModel(prompt promptSession, provider string) (string, error) {
	profile, exists := hostedProviderProfileFor(provider)
	if !exists || len(profile.Models) == 0 {
		return promptCustomModel(prompt, "Remote model ID")
	}
	models := profile.Models
	if prompt.selectOne != nil {
		optionCount := len(models)
		if !profile.StandardSetup {
			optionCount++
		}
		options := make([]terminalChoice, 0, optionCount)
		for _, model := range models {
			options = append(options, terminalChoice{Label: model.ID + " · " + hostedModelStatus(model), Value: model.ID})
		}
		if !profile.StandardSetup {
			options = append(options, terminalChoice{Label: "Enter an experimental custom model ID", Value: customModelChoice})
		}
		selected, err := prompt.chooseOne("Remote model", models[0].ID, options)
		if err != nil {
			return "", err
		}
		if selected == customModelChoice {
			return promptCustomModel(prompt, "Custom remote model ID")
		}
		return selected, nil
	}
	if _, err := fmt.Fprintf(prompt.output, "  ComplyScan %s model shortlist:\n", reviewProviderLabel(provider)); err != nil {
		return "", err
	}
	for index, model := range models {
		if _, err := fmt.Fprintf(prompt.output, "    %d) %s · %s\n", index+1, model.ID, hostedModelStatus(model)); err != nil {
			return "", err
		}
	}
	for {
		label := "Remote model number"
		if !profile.StandardSetup {
			label = "Remote model number or experimental exact ID"
		}
		value, err := prompt.text(label, "1")
		if err != nil {
			return "", err
		}
		var selected int
		if _, scanErr := fmt.Sscanf(value, "%d", &selected); scanErr == nil {
			if selected >= 1 && selected <= len(models) && value == fmt.Sprintf("%d", selected) {
				return models[selected-1].ID, nil
			}
			if _, writeErr := fmt.Fprintf(prompt.output, "  Enter a number from 1 to %d, or an exact model ID.\n", len(models)); writeErr != nil {
				return "", writeErr
			}
			continue
		}
		if !profile.StandardSetup && strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00") {
			return strings.TrimSpace(value), nil
		}
	}
}

func remoteModelOptions(provider string) []string {
	profile, exists := hostedProviderProfileFor(provider)
	if !exists {
		return nil
	}
	models := make([]string, 0, len(profile.Models))
	for _, model := range profile.Models {
		models = append(models, model.ID)
	}
	return models
}

func defaultRemoteModel(provider string) string {
	models := remoteModelOptions(provider)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func defaultRemoteAPIKeyEnvironment(provider string) string {
	profile, exists := hostedProviderProfileFor(provider)
	if !exists {
		return ""
	}
	return profile.APIKeyEnv
}

func installOllama(ctx context.Context, stdout io.Writer) (string, error) {
	if brewPath, err := exec.LookPath("brew"); err == nil {
		if _, err := fmt.Fprintln(stdout, "Installing Ollama with Homebrew..."); err != nil {
			return "", err
		}
		if err := runSetupCommand(ctx, stdout, brewPath, "install", "ollama"); err != nil {
			return "", fmt.Errorf("brew install ollama: %w", err)
		}
		ollamaPath, err := exec.LookPath("ollama")
		if err != nil {
			return "", errors.New("Homebrew completed but ollama is not available on PATH")
		}
		if err := runSetupCommand(ctx, stdout, brewPath, "services", "start", "ollama"); err != nil {
			if _, writeErr := fmt.Fprintf(stdout, "Ollama was installed, but its Homebrew service did not start: %v\nStart it manually with `ollama serve`.\n", err); writeErr != nil {
				return "", writeErr
			}
		}
		return ollamaPath, nil
	}
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("automatic installation without Homebrew is not supported on %s", runtime.GOOS)
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return "", errors.New("curl is required to run Ollama's official Linux installer")
	}
	installer, err := os.CreateTemp("", "complyscan-ollama-install-*.sh")
	if err != nil {
		return "", fmt.Errorf("create temporary Ollama installer: %w", err)
	}
	installerPath := installer.Name()
	if closeErr := installer.Close(); closeErr != nil {
		_ = os.Remove(installerPath)
		return "", fmt.Errorf("close temporary Ollama installer: %w", closeErr)
	}
	defer os.Remove(installerPath)
	if _, err := fmt.Fprintln(stdout, "Downloading Ollama's official Linux installer from https://ollama.com/install.sh..."); err != nil {
		return "", err
	}
	if err := runSetupCommand(ctx, stdout, curlPath, "-fsSL", "-o", installerPath, "https://ollama.com/install.sh"); err != nil {
		return "", fmt.Errorf("download official Ollama installer: %w", err)
	}
	if err := runSetupCommand(ctx, stdout, "/bin/sh", installerPath); err != nil {
		return "", fmt.Errorf("run official Ollama installer: %w", err)
	}
	ollamaPath, err := exec.LookPath("ollama")
	if err != nil {
		return "", errors.New("official installer completed but ollama is not available on PATH")
	}
	return ollamaPath, nil
}

func runSetupCommand(ctx context.Context, output io.Writer, executable string, args ...string) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = output
	command.Stderr = output
	command.Stdin = os.Stdin
	return command.Run()
}

func ollamaModelInstalled(ctx context.Context, executable, model string) bool {
	for _, installed := range ollamaInstalledModels(ctx, executable) {
		if strings.EqualFold(installed.tag, strings.TrimSpace(model)) {
			return true
		}
	}
	return false
}

type ollamaInstalledModel struct {
	tag    string
	sizeGB float64
}

func ollamaInstalledModels(ctx context.Context, executable string) []ollamaInstalledModel {
	command := exec.CommandContext(ctx, executable, "list")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(output), "\n")
	models := make([]ollamaInstalledModel, 0, len(lines))
	seen := map[string]struct{}{}
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || index == 0 && strings.EqualFold(fields[0], "name") {
			continue
		}
		key := strings.ToLower(fields[0])
		if _, duplicate := seen[key]; !duplicate {
			seen[key] = struct{}{}
			sizeFields := []string{}
			if len(fields) > 2 {
				sizeFields = fields[2:]
			}
			models = append(models, ollamaInstalledModel{tag: fields[0], sizeGB: parseOllamaSizeGB(sizeFields)})
		}
	}
	return models
}

func parseOllamaSizeGB(fields []string) float64 {
	for index, field := range fields {
		value := strings.ToUpper(strings.TrimSpace(field))
		for _, unit := range []struct {
			suffix string
			factor float64
		}{
			{suffix: "TB", factor: 1000},
			{suffix: "GB", factor: 1},
			{suffix: "MB", factor: 0.001},
			{suffix: "KB", factor: 0.000001},
			{suffix: "B", factor: 0.000000001},
		} {
			if value == unit.suffix && index > 0 {
				number, err := strconv.ParseFloat(strings.ReplaceAll(fields[index-1], ",", ""), 64)
				if err == nil {
					return number * unit.factor
				}
			}
			if !strings.HasSuffix(value, unit.suffix) || value == unit.suffix {
				continue
			}
			number, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSuffix(value, unit.suffix), ",", ""), 64)
			if err == nil {
				return number * unit.factor
			}
		}
	}
	return 0
}

type setupModelOption struct {
	tag             string
	category        string
	detail          string
	downloadSizeGB  float64
	sizeApproximate bool
	installed       bool
}

const customModelChoice = "__complyscan_custom_model__"
const catalogueModelChoice = "__complyscan_catalogue_model__"

func recommendedOllamaModels() []setupModelOption {
	return []setupModelOption{
		{tag: defaultSetupModel, detail: "recommended default; onboarding benchmark recorded; automatic compatibility check after selection", downloadSizeGB: 6.6},
		{tag: "qwen3:8b", detail: "smaller general model; technical-review baseline recorded; automatic compatibility check after selection", downloadSizeGB: 5.2},
	}
}

func commonOllamaModels() []setupModelOption {
	return []setupModelOption{
		{tag: defaultSetupModel, category: "Recommended", downloadSizeGB: 6.6},
		{tag: "qwen3.5:4b", category: "Small", downloadSizeGB: 3.4},
		{tag: "llama3.2:3b", category: "Small", downloadSizeGB: 2.0},
		{tag: "gemma3:4b", category: "Small", downloadSizeGB: 3.3},
		{tag: "qwen2.5-coder:7b", category: "Coding", downloadSizeGB: 4.7},
		{tag: "qwen3-coder:30b", category: "Coding", downloadSizeGB: 18.6},
		{tag: "deepseek-coder-v2:16b", category: "Coding", downloadSizeGB: 8.9},
		{tag: "codestral:22b", category: "Coding", downloadSizeGB: 12.6},
		{tag: "gemma3:12b", category: "General", downloadSizeGB: 8.1},
		{tag: "mistral:7b", category: "General", downloadSizeGB: 4.4},
		{tag: "deepseek-r1:8b", category: "Reasoning", downloadSizeGB: 5.2},
		{tag: "phi4:14b", category: "Reasoning", downloadSizeGB: 9.1},
		{tag: "gpt-oss:20b", category: "Reasoning", downloadSizeGB: 14.0},
		{tag: "qwen3.5:27b", category: "Large", downloadSizeGB: 17.0},
	}
}

func promptOllamaModel(prompt promptSession, current string, installed []ollamaInstalledModel) (string, error) {
	current = strings.TrimSpace(current)
	if current == "" {
		current = defaultSetupModel
	}
	options := []setupModelOption{modelOption(current, modelStatus(current, installed), installed)}
	for _, model := range installed {
		options = appendUniqueModel(options, setupModelOption{tag: model.tag, detail: modelStatus(model.tag, installed), downloadSizeGB: model.sizeGB, installed: true})
	}
	for _, recommendation := range recommendedOllamaModels() {
		options = appendUniqueModel(options, recommendation)
	}
	options = groupOllamaModelOptions(options)
	if _, err := fmt.Fprintln(prompt.output, "  All choices below run through Ollama. Installed models are ready now; suggested models require a download."); err != nil {
		return "", err
	}
	choices := make([]terminalChoice, 0, len(options)+2)
	for _, option := range options {
		choices = append(choices, terminalChoice{Label: categorizedModelOptionLabel(option), Value: option.tag})
	}
	choices = append(choices, terminalChoice{
		Label: "Ollama catalogue · Choose from common local models",
		Value: catalogueModelChoice,
	})
	choices = append(choices, terminalChoice{
		Label: "Exact tag · Enter any other Ollama model tag",
		Value: customModelChoice,
	})
	selected, err := chooseSetupOption(prompt, "Ollama model", choices, current)
	if err != nil {
		return "", err
	}
	if selected == customModelChoice {
		return promptCustomOllamaModel(prompt)
	}
	if selected == catalogueModelChoice {
		return promptCommonOllamaModel(prompt, installed)
	}
	return selected, nil
}

func promptCommonOllamaModel(prompt promptSession, installed []ollamaInstalledModel) (string, error) {
	if _, err := fmt.Fprintln(prompt.output, "\n  Common local models are listed below by purpose. Choose Exact tag for any other model in the Ollama library."); err != nil {
		return "", err
	}
	models := commonOllamaModels()
	choices := make([]terminalChoice, 0, len(models)+1)
	for _, model := range models {
		model = modelOption(model.tag, model.category, installed)
		model.category = commonModelCategory(model.tag)
		choices = append(choices, terminalChoice{Label: commonOllamaModelLabel(model), Value: model.tag})
	}
	choices = append(choices, terminalChoice{Label: "Exact tag · Enter any other Ollama model tag", Value: customModelChoice})
	selected, err := chooseSetupOption(prompt, "Common Ollama models", choices, defaultSetupModel)
	if err != nil {
		return "", err
	}
	if selected == customModelChoice {
		return promptCustomOllamaModel(prompt)
	}
	return selected, nil
}

func chooseSetupOption(prompt promptSession, label string, choices []terminalChoice, defaultValue string) (string, error) {
	if prompt.selectOne != nil {
		return prompt.chooseOne(label, defaultValue, choices)
	}
	defaultIndex := 1
	for index, choice := range choices {
		if choice.Value == defaultValue {
			defaultIndex = index + 1
		}
		if _, err := fmt.Fprintf(prompt.output, "    %d) %s\n", index+1, choice.Label); err != nil {
			return "", err
		}
	}
	for {
		value, err := prompt.text(label+" number", strconv.Itoa(defaultIndex))
		if err != nil {
			return "", err
		}
		selected, parseErr := strconv.Atoi(value)
		if parseErr == nil && selected >= 1 && selected <= len(choices) && value == strconv.Itoa(selected) {
			return choices[selected-1].Value, nil
		}
		if _, err := fmt.Fprintf(prompt.output, "  Enter a number from 1 to %d.\n", len(choices)); err != nil {
			return "", err
		}
	}
}

func modelOption(model, detail string, installed []ollamaInstalledModel) setupModelOption {
	option := setupModelOption{tag: model, detail: detail}
	for _, candidate := range installed {
		if strings.EqualFold(candidate.tag, model) {
			option.downloadSizeGB = candidate.sizeGB
			option.installed = true
			return option
		}
	}
	for _, recommendation := range recommendedOllamaModels() {
		if strings.EqualFold(recommendation.tag, model) {
			option.downloadSizeGB = recommendation.downloadSizeGB
			option.sizeApproximate = recommendation.sizeApproximate
			return option
		}
	}
	for _, common := range commonOllamaModels() {
		if strings.EqualFold(common.tag, model) {
			option.downloadSizeGB = common.downloadSizeGB
			option.sizeApproximate = common.sizeApproximate
			return option
		}
	}
	return option
}

func modelOptionLabel(option setupModelOption) string {
	size := "size unavailable until Ollama resolves this tag"
	if option.downloadSizeGB > 0 {
		prefix := ""
		if option.sizeApproximate {
			prefix = "~"
		}
		size = fmt.Sprintf("%s%.1f GB", prefix, option.downloadSizeGB)
	}
	return fmt.Sprintf("%s — %s — %s", option.tag, size, option.detail)
}

func categorizedModelOptionLabel(option setupModelOption) string {
	category := "Suggested download"
	if option.installed {
		category = "Installed"
	}
	return category + " · " + modelOptionLabel(option)
}

func commonModelCategory(tag string) string {
	for _, model := range commonOllamaModels() {
		if strings.EqualFold(model.tag, tag) {
			return model.category
		}
	}
	return "Other"
}

func commonOllamaModelLabel(option setupModelOption) string {
	size := fmt.Sprintf("%.1f GB", option.downloadSizeGB)
	if option.installed {
		return fmt.Sprintf("Installed · %s · %s · %s", option.tag, size, option.category)
	}
	return fmt.Sprintf("%s · %s · %s", option.category, option.tag, size)
}

func groupOllamaModelOptions(options []setupModelOption) []setupModelOption {
	grouped := make([]setupModelOption, 0, len(options))
	for _, option := range options {
		if option.installed {
			grouped = append(grouped, option)
		}
	}
	for _, option := range options {
		if !option.installed {
			grouped = append(grouped, option)
		}
	}
	return grouped
}

func promptCustomModel(prompt promptSession, label string) (string, error) {
	for {
		value, err := prompt.text(label, "")
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value != "" && !strings.ContainsAny(value, "\r\n\x00") {
			return value, nil
		}
		if _, err := fmt.Fprintln(prompt.output, "  Enter a valid model name."); err != nil {
			return "", err
		}
	}
}

func promptCustomOllamaModel(prompt promptSession) (string, error) {
	for {
		model, err := promptCustomModel(prompt, "Custom Ollama model tag")
		if err != nil {
			return "", err
		}
		if !isOllamaCloudTag(model) {
			return model, nil
		}
		if _, err := fmt.Fprintln(prompt.output, "  Cloud-only Ollama tags are unavailable in local privacy mode. Choose a local model variant."); err != nil {
			return "", err
		}
	}
}

func isOllamaCloudTag(tag string) bool {
	separator := strings.LastIndex(tag, ":")
	return separator >= 0 && strings.Contains(strings.ToLower(tag[separator+1:]), "cloud")
}

func appendUniqueModel(options []setupModelOption, option setupModelOption) []setupModelOption {
	for _, existing := range options {
		if strings.EqualFold(existing.tag, option.tag) {
			return options
		}
	}
	return append(options, option)
}

func modelStatus(model string, installed []ollamaInstalledModel) string {
	for _, candidate := range installed {
		if strings.EqualFold(candidate.tag, model) {
			if strings.EqualFold(model, defaultSetupModel) {
				return "recommended default; onboarding benchmark recorded; compatibility checked after selection"
			}
			if strings.EqualFold(model, "qwen3:8b") {
				return "technical-review baseline recorded; compatibility checked after selection"
			}
			return "compatibility checked automatically after selection"
		}
	}
	if strings.EqualFold(model, defaultSetupModel) {
		return "recommended default; onboarding benchmark recorded"
	}
	if strings.EqualFold(model, "qwen3:8b") {
		return "technical-review baseline recorded"
	}
	return "configured choice; compatibility checked after download"
}

func setupModelDefault(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return defaultSetupModel
}

func ollamaResourceEstimate(model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(value, "70b") || strings.Contains(value, "72b"):
		return "roughly 40–50 GB to download and 48–64 GB of runtime memory; actual use varies by quantization and context"
	case strings.Contains(value, "30b") || strings.Contains(value, "32b"):
		return "roughly 18–24 GB to download and 20–32 GB of runtime memory; actual use varies by quantization and context"
	case strings.Contains(value, "20b") || strings.Contains(value, "22b"):
		return "roughly 12–16 GB to download and 16–24 GB of runtime memory; actual use varies by quantization and context"
	case strings.Contains(value, "14b") || strings.Contains(value, "16b"):
		return "roughly 9–13 GB to download and 12–20 GB of runtime memory; actual use varies by quantization and context"
	case strings.Contains(value, "7b"):
		return "roughly 4–6 GB to download and 6–9 GB of runtime memory; actual use varies by quantization and context"
	case strings.EqualFold(value, defaultSetupModel), strings.Contains(value, "8b"), strings.Contains(value, "9b"):
		return "roughly 5–8 GB to download and 6–10 GB of runtime memory; actual use varies by quantization and context"
	default:
		return "size is model- and quantization-dependent; Ollama will show live download progress and the model page should be checked for memory requirements"
	}
}

type setupGeneratedFile struct {
	Path string
	Kind discovery.FileKind
}

func refreshSetupDiscovery(result discovery.Result, target string, generated ...setupGeneratedFile) (discovery.Result, error) {
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return result, err
	}
	for _, candidate := range generated {
		absolutePath, err := filepath.Abs(candidate.Path)
		if err != nil {
			return result, err
		}
		relative, err := filepath.Rel(absoluteTarget, absolutePath)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		info, err := os.Lstat(absolutePath)
		if err != nil {
			return result, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return result, fmt.Errorf("%s must be a regular file and must not be a symlink", absolutePath)
		}
		content, err := os.ReadFile(absolutePath)
		if err != nil {
			return result, err
		}
		path := filepath.ToSlash(relative)
		replaced := false
		for index := range result.Repository.Files {
			if result.Repository.Files[index].Path != path {
				continue
			}
			result.Stats.BytesRead -= result.Repository.Files[index].Size
			result.Repository.Files[index] = discovery.File{Path: path, Kind: candidate.Kind, Size: int64(len(content)), Content: content}
			result.Stats.BytesRead += int64(len(content))
			replaced = true
			break
		}
		if !replaced {
			result.Repository.Files = append(result.Repository.Files, discovery.File{Path: path, Kind: candidate.Kind, Size: int64(len(content)), Content: content})
			result.Stats.FilesRead++
			result.Stats.BytesRead += int64(len(content))
		}
	}
	sort.Slice(result.Repository.Files, func(i, j int) bool { return result.Repository.Files[i].Path < result.Repository.Files[j].Path })
	return result, nil
}

func systemIndex(systems []profile.System, id string) int {
	for index := range systems {
		if systems[index].ID == id {
			return index
		}
	}
	return -1
}

func runFirstScan(parent *cobra.Command, stdout io.Writer, build BuildInfo, target string, mode setupScanMode, discovered discovery.Result) error {
	if _, err := fmt.Fprintln(stdout, "\nStarting first ComplyScan scan..."); err != nil {
		return err
	}
	command := newScanCommandWithDiscovery(stdout, build, &scanDiscoverySeed{Target: target, Discovery: discovered})
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetIn(parent.InOrStdin())
	command.SetOut(stdout)
	command.SetErr(parent.ErrOrStderr())
	command.SetArgs([]string{target})
	err := command.ExecuteContext(parent.Context())
	var status *exitError
	if errors.As(err, &status) && status.code == 1 {
		_, writeErr := fmt.Fprintln(stdout, "\nSetup completed. The first scan found items at or above the configured failure threshold; review the saved report.")
		return writeErr
	}
	return err
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	for _, character := range value {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-", character) {
			return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
		}
	}
	return value
}
