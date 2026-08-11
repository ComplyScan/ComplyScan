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
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/spf13/cobra"
)

const defaultSetupModel = "qwen3.5:9b"

type setupOptions struct {
	configPath        string
	forceInteractive  bool
	nonInteractive    bool
	advanced          bool
	reviewProvider    string
	ollamaModel       string
	remoteModel       string
	remoteAPIKeyEnv   string
	frameworks        []string
	allowRemoteReview bool
	pullModel         bool
	skipModelPull     bool
	qualifyModel      bool
	installOllama     bool
	skipOllamaInstall bool
	skipScan          bool
	detailedGuidance  bool
}

func newSetupCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var options setupOptions
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Configure a repository, applicability context, and optional AI review",
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
	command.Flags().StringVar(&options.reviewProvider, "review", "", "advisory review provider: none, ollama, openai, anthropic, or gemini")
	command.Flags().StringVar(&options.ollamaModel, "ollama-model", "", "Ollama model name")
	command.Flags().StringVar(&options.remoteModel, "model", "", "remote-provider model name")
	command.Flags().StringVar(&options.remoteAPIKeyEnv, "api-key-env", "", "environment-variable name containing the remote-provider API key")
	command.Flags().StringSliceVar(&options.frameworks, "framework", nil, "built-in technical evidence pack to enable (repeatable)")
	command.Flags().BoolVar(&options.allowRemoteReview, "allow-remote-review", false, "confirm that bounded repository context may be sent to the selected remote provider")
	command.Flags().BoolVar(&options.pullModel, "pull-model", false, "download the configured Ollama model (requires --review ollama in non-interactive mode)")
	command.Flags().BoolVar(&options.skipModelPull, "skip-model-pull", false, "configure Ollama without offering to download the model")
	command.Flags().BoolVar(&options.qualifyModel, "qualify-model", false, "run the bounded synthetic model compatibility check (automatic in interactive setup; remote providers may charge)")
	command.Flags().BoolVar(&options.installOllama, "install-ollama", false, "install Ollama when it is missing (requires explicit use in non-interactive mode)")
	command.Flags().BoolVar(&options.skipOllamaInstall, "skip-ollama-install", false, "do not offer to install Ollama when it is missing")
	command.Flags().BoolVar(&options.skipScan, "skip-scan", false, "do not offer to run the first scan")
	command.Flags().BoolVar(&options.detailedGuidance, "detailed-guidance", false, "show complete explanations and examples for every setup question")
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
	if err := prompt.sectionTitle("ComplyScan setup", false); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Repository: %s\n\n", target); err != nil {
		return err
	}
	prompt.alwaysDetailed = options.detailedGuidance

	var repositorySummary setupRepositorySummary
	profileDraft := newSetupProfileDraft()
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
				resume, resumeErr := promptSetupDraftResume(prompt, stored)
				if resumeErr != nil {
					return resumeErr
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
		if err := setupStepTitle(prompt, 1, 5, "Analysis and privacy mode", false); err != nil {
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
		if err := setupStepTitle(prompt, 2, 5, "Repository inspection", false); err != nil {
			return err
		}
		summary, inspectErr := inspectRepositoryForSetup(cmd.Context(), prompt, target, cfg, build)
		if inspectErr != nil {
			return inspectErr
		}
		repositorySummary = summary
		if err := setupStepTitle(prompt, 3, 5, "System and framework context", true); err != nil {
			return err
		}
		if setupDraftStageRank(resumeStage) >= setupDraftStageRank(setupDraftContext) {
			if err := prompt.status(setupStatusReady, "Resumed the saved system, framework, applicability, and ownership answers."); err != nil {
				return err
			}
		} else {
			if !options.advanced {
				profileDraft = draftProfileForSetup(cmd.Context(), stdout, target, cfg, summary, modelReady)
			}
			if err := setupStepTitle(prompt, 4, 5, "Applicability and evidence ownership", true); err != nil {
				return err
			}
			if err := collectInteractiveSetupContext(prompt, target, &cfg, options, summary, profileDraft, true); err != nil {
				return err
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
		if !options.skipScan && setupDraftStageRank(resumeStage) < setupDraftStageRank(setupDraftReview) {
			scanMode, err = promptSetupScanMode(prompt, repositorySummary, cfg.AI.Provider, modelReady)
			if err != nil {
				return err
			}
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
		save, reviewErr := reviewSetupBeforeSave(cmd.Context(), prompt, stdout, target, &cfg, options, repositorySummary, profileDraft, &scanMode, &modelReady)
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
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan --quick %s\nDeep review when ready: complyscan scan --deep %s\n", shellQuote(target), shellQuote(target))
		return err
	}
	if scanMode == setupScanDeep && cfg.AI.Provider == "none" {
		if _, err := fmt.Fprintln(stdout, "Deep review was not started because no AI review provider was configured. The saved configuration can still run a quick scan."); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan --quick %s\n", shellQuote(target))
		return err
	}
	if scanMode == setupScanDeep && !modelReady {
		if _, err := fmt.Fprintln(stdout, "Deep review was not started because the selected model or provider is not ready. The setup and preliminary scan remain available."); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan --quick %s\nDeep review when ready: complyscan scan --deep %s\n", shellQuote(target), shellQuote(target))
		return err
	}
	return runFirstScan(cmd, stdout, build, target, scanMode, repositorySummary.Discovery)
}

func collectInteractiveSetupContext(prompt promptSession, target string, cfg *config.Config, options setupOptions, summary setupRepositorySummary, draft setupProfileDraft, confirmReplace bool) error {
	var system profile.System
	var err error
	if options.advanced {
		if err = configureFrameworkSelection(prompt, cfg, true, options.frameworks); err != nil {
			return err
		}
		system, err = collectSystemProfileWithPrompt(prompt, target, time.Now(), cfg.Frameworks...)
	} else {
		system, err = collectBasicSystemProfile(prompt, target, time.Now(), summary, draft)
		if err == nil {
			err = configureRecommendedFrameworks(prompt, cfg, system, options.frameworks)
			applyFrameworksToSystem(&system, cfg.Frameworks)
		}
	}
	if err != nil {
		return err
	}
	if !options.advanced && frameworkEnabled(cfg.Frameworks, framework.EUAIActTechnicalEvidencePackID) {
		err = collectRelevantEUApplicabilityContext(prompt, &system, time.Now(), draft)
	} else if !options.advanced {
		err = collectNonEUTechnicalContext(prompt, &system, draft)
	}
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

type setupReviewAction string

const (
	setupReviewSave     setupReviewAction = "Save configuration"
	setupReviewAnalysis setupReviewAction = "Change analysis and privacy mode"
	setupReviewContext  setupReviewAction = "Repeat system and framework questions"
	setupReviewScan     setupReviewAction = "Change first-run action"
	setupReviewCancel   setupReviewAction = "Cancel without saving"
)

func reviewSetupBeforeSave(ctx context.Context, prompt promptSession, stdout io.Writer, target string, cfg *config.Config, options setupOptions, summary setupRepositorySummary, draft setupProfileDraft, scanMode *setupScanMode, modelReady *bool) (bool, error) {
	for {
		if err := writeSetupReviewSummary(prompt, *cfg, *scanMode, *modelReady); err != nil {
			return false, err
		}
		actions := []setupReviewAction{setupReviewSave, setupReviewAnalysis, setupReviewContext}
		if !options.skipScan {
			actions = append(actions, setupReviewScan)
		}
		actions = append(actions, setupReviewCancel)
		action, err := promptChoice(prompt, "review action", setupReviewSave, actions...)
		if err != nil {
			return false, err
		}
		switch action {
		case setupReviewSave:
			return true, nil
		case setupReviewAnalysis:
			revisit := options
			revisit.reviewProvider = ""
			revisit.ollamaModel = ""
			revisit.remoteModel = ""
			revisit.remoteAPIKeyEnv = ""
			revisit.allowRemoteReview = false
			revisit.pullModel = false
			revisit.qualifyModel = false
			if err := prompt.sectionTitle("Analysis and privacy mode", true); err != nil {
				return false, err
			}
			*modelReady, err = configureSetupReview(ctx, prompt, stdout, cfg, true, revisit)
			if err != nil {
				return false, err
			}
		case setupReviewContext:
			revisit := options
			revisit.frameworks = nil
			if err := prompt.sectionTitle("System, framework, and applicability context", true); err != nil {
				return false, err
			}
			if err := collectInteractiveSetupContext(prompt, target, cfg, revisit, summary, draft, false); err != nil {
				return false, err
			}
		case setupReviewScan:
			if err := prompt.sectionTitle("First-run action", true); err != nil {
				return false, err
			}
			*scanMode, err = promptSetupScanMode(prompt, summary, cfg.AI.Provider, *modelReady)
			if err != nil {
				return false, err
			}
		case setupReviewCancel:
			return false, nil
		}
	}
}

func writeSetupReviewSummary(prompt promptSession, cfg config.Config, scanMode setupScanMode, modelReady bool) error {
	if err := prompt.sectionTitle("Review setup", true); err != nil {
		return err
	}
	analysis := "Fast technical analysis (no model)"
	if cfg.AI.Provider == "ollama" {
		analysis = fmt.Sprintf("Local Ollama — %s", cfg.AI.Ollama.Model)
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
	firstRun := "save without scanning"
	if scanMode == setupScanQuick {
		firstRun = "quick deterministic scan"
	} else if scanMode == setupScanDeep {
		firstRun = "deep AI-assisted scan"
	}
	analysisStatus := setupStatusReady
	if cfg.AI.Provider != "none" && !modelReady {
		analysisStatus = setupStatusReview
	}
	if err := prompt.status(analysisStatus, "Analysis: "+analysis); err != nil {
		return err
	}
	if err := prompt.status(setupStatusReady, "Frameworks: "+strings.Join(frameworks, ", ")); err != nil {
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
	if err := prompt.status(systemStatus, "Systems: "+systems); err != nil {
		return err
	}
	ownershipStatus := setupStatusReady
	if len(cfg.Systems) == 0 || (len(cfg.Systems) > 1 && len(cfg.Ownership) == 0) {
		ownershipStatus = setupStatusMissing
	}
	if err := prompt.status(ownershipStatus, "Evidence ownership: "+ownership); err != nil {
		return err
	}
	if err := prompt.status(setupStatusReady, "First run: "+firstRun); err != nil {
		return err
	}
	_, err := fmt.Fprintln(prompt.output)
	return err
}

func setupStepTitle(prompt promptSession, current, total int, title string, leadingBlank bool) error {
	return prompt.sectionTitle(fmt.Sprintf("Step %d of %d — %s", current, total, title), leadingBlank)
}

func setupReviewExplicit(options setupOptions) bool {
	return options.reviewProvider != "" || options.ollamaModel != "" || options.remoteModel != "" || options.remoteAPIKeyEnv != "" ||
		options.allowRemoteReview || options.pullModel || options.skipModelPull || options.qualifyModel || options.installOllama || options.skipOllamaInstall
}

func configureSetupReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := strings.ToLower(strings.TrimSpace(options.reviewProvider))
	if provider == "" && interactive {
		if err := explainSetupQuestion(prompt, "review-provider"); err != nil {
			return false, err
		}
		const (
			localOption     = "Local AI-assisted analysis — Ollama keeps context on this machine"
			openAIOption    = "Cloud AI-assisted analysis — OpenAI"
			anthropicOption = "Cloud AI-assisted analysis — Anthropic"
			geminiOption    = "Cloud AI-assisted analysis — Gemini"
			fastOption      = "Fast technical analysis — no model"
		)
		defaultProvider := localOption
		switch cfg.AI.Provider {
		case "openai":
			defaultProvider = openAIOption
		case "anthropic":
			defaultProvider = anthropicOption
		case "gemini":
			defaultProvider = geminiOption
		}
		selected, err := promptChoice(prompt, "Analysis mode", defaultProvider, localOption, openAIOption, anthropicOption, geminiOption, fastOption)
		if err != nil {
			return false, err
		}
		switch selected {
		case localOption:
			provider = "ollama"
		case openAIOption:
			provider = "openai"
		case anthropicOption:
			provider = "anthropic"
		case geminiOption:
			provider = "gemini"
		default:
			provider = "none"
		}
	}
	if provider == "" {
		provider = cfg.AI.Provider
	}
	if provider != "none" && provider != "ollama" && !isRemoteReviewProvider(provider) {
		return false, fmt.Errorf("invalid review provider %q (want none, ollama, openai, anthropic, or gemini)", provider)
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
		return false, errors.New("--model requires --review openai, anthropic, or gemini")
	}
	if options.remoteAPIKeyEnv != "" && !isRemoteReviewProvider(provider) {
		return false, errors.New("--api-key-env requires --review openai, anthropic, or gemini")
	}
	if options.allowRemoteReview && !isRemoteReviewProvider(provider) {
		return false, errors.New("--allow-remote-review requires --review openai, anthropic, or gemini")
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
		installed := []string{}
		if ollamaPath != "" {
			installed = ollamaInstalledModels(ctx, ollamaPath)
		}
		if err := prompt.sectionTitle("Local model setup", true); err != nil {
			return false, err
		}
		if err := explainSetupQuestion(prompt, "ollama-model"); err != nil {
			return false, err
		}
		var err error
		model, err = promptOllamaModel(prompt, setupModelDefault(cfg.AI.Ollama.Model), installed)
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
			shouldInstall, err = prompt.confirm("Install and start Ollama now", true)
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
		shouldPull, confirmErr = prompt.confirm(fmt.Sprintf("Download Ollama model %q now", model), true)
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

func configureRemoteReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := cfg.AI.Provider
	allowed := options.allowRemoteReview
	if interactive {
		if err := prompt.sectionTitle("Cloud model setup", true); err != nil {
			return false, err
		}
		if err := explainSetupQuestion(prompt, "remote-disclosure"); err != nil {
			return false, err
		}
		if !allowed {
			confirmed, err := prompt.confirm(fmt.Sprintf("Allow bounded repository context to be sent to %s", reviewProviderLabel(provider)), false)
			if err != nil {
				return false, err
			}
			allowed = confirmed
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

	model := strings.TrimSpace(options.remoteModel)
	if model == "" && interactive {
		if err := explainSetupQuestion(prompt, "remote-model"); err != nil {
			return false, err
		}
		var err error
		model, err = promptRemoteModel(prompt, provider)
		if err != nil {
			return false, err
		}
	}
	if model == "" {
		model = defaultRemoteModel(provider)
	}
	keyEnvironment := strings.TrimSpace(options.remoteAPIKeyEnv)
	if keyEnvironment == "" && interactive {
		if err := explainSetupQuestion(prompt, "api-key-env"); err != nil {
			return false, err
		}
		var err error
		keyEnvironment, err = prompt.text("API key environment-variable name", defaultRemoteAPIKeyEnvironment(provider))
		if err != nil {
			return false, err
		}
	}
	if keyEnvironment == "" {
		keyEnvironment = defaultRemoteAPIKeyEnvironment(provider)
	}
	cfg.AI.Remote = config.RemoteConfig{
		Model: model, APIKeyEnv: keyEnvironment, TimeoutSeconds: 360, MaxFindings: 20,
	}
	if err := cfg.AI.Remote.Validate(); err != nil {
		return false, fmt.Errorf("remote review configuration: %w", err)
	}
	if value, exists := os.LookupEnv(keyEnvironment); !exists || strings.TrimSpace(value) == "" {
		if _, err := fmt.Fprintf(stdout, "%s is not currently set. Add it to your shell or CI secret store before scanning; the key itself is never written to .complyscan.yml.\n", keyEnvironment); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, err := fmt.Fprintf(stdout, "%s review configured with model %q. The credential was found in %s and was not saved.\n", reviewProviderLabel(provider), model, keyEnvironment); err != nil {
		return false, err
	}
	return finishSetupModelQualification(ctx, stdout, cfg.AI, interactive || options.qualifyModel)
}

func promptRemoteModel(prompt promptSession, provider string) (string, error) {
	models := remoteModelOptions(provider)
	if len(models) == 0 {
		return "", fmt.Errorf("unsupported remote review provider %q", provider)
	}
	if prompt.selectOne != nil {
		options := make([]terminalChoice, 0, len(models)+1)
		for _, model := range models {
			options = append(options, terminalChoice{Label: model, Value: model})
		}
		options = append(options, terminalChoice{Label: "Enter a custom model ID", Value: customModelChoice})
		selected, err := prompt.chooseOne("Remote model", models[0], options)
		if err != nil {
			return "", err
		}
		if selected == customModelChoice {
			return promptCustomModel(prompt, "Custom remote model ID")
		}
		return selected, nil
	}
	if _, err := fmt.Fprintf(prompt.output, "  Suggested %s models (you may also type another exact model ID):\n", reviewProviderLabel(provider)); err != nil {
		return "", err
	}
	for index, model := range models {
		if _, err := fmt.Fprintf(prompt.output, "    %d) %s\n", index+1, model); err != nil {
			return "", err
		}
	}
	for {
		value, err := prompt.text("Remote model number or exact ID", "1")
		if err != nil {
			return "", err
		}
		var selected int
		if _, scanErr := fmt.Sscanf(value, "%d", &selected); scanErr == nil {
			if selected >= 1 && selected <= len(models) && value == fmt.Sprintf("%d", selected) {
				return models[selected-1], nil
			}
			if _, writeErr := fmt.Fprintf(prompt.output, "  Enter a number from 1 to %d, or an exact model ID.\n", len(models)); writeErr != nil {
				return "", writeErr
			}
			continue
		}
		if strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n\x00") {
			return strings.TrimSpace(value), nil
		}
	}
}

func remoteModelOptions(provider string) []string {
	switch provider {
	case "openai":
		return []string{"gpt-5.6-terra", "gpt-5.6-sol", "gpt-5.6-luna"}
	case "anthropic":
		return []string{"claude-sonnet-5", "claude-opus-5", "claude-haiku-4-5"}
	case "gemini":
		return []string{"gemini-3.6-flash", "gemini-3.5-flash-lite", "gemini-3.5-flash"}
	default:
		return nil
	}
}

func defaultRemoteModel(provider string) string {
	models := remoteModelOptions(provider)
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func defaultRemoteAPIKeyEnvironment(provider string) string {
	switch provider {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	default:
		return ""
	}
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
		if strings.EqualFold(installed, strings.TrimSpace(model)) {
			return true
		}
	}
	return false
}

func ollamaInstalledModels(ctx context.Context, executable string) []string {
	command := exec.CommandContext(ctx, executable, "list")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(output), "\n")
	models := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 || index == 0 && strings.EqualFold(fields[0], "name") {
			continue
		}
		key := strings.ToLower(fields[0])
		if _, duplicate := seen[key]; !duplicate {
			seen[key] = struct{}{}
			models = append(models, fields[0])
		}
	}
	return models
}

type setupModelOption struct {
	tag    string
	detail string
}

const customModelChoice = "__complyscan_custom_model__"

func recommendedOllamaModels() []setupModelOption {
	return []setupModelOption{
		{tag: defaultSetupModel, detail: "recommended default; onboarding benchmark recorded; automatic compatibility check after selection"},
		{tag: "qwen3:8b", detail: "smaller general model; technical-review baseline recorded; automatic compatibility check after selection"},
		{tag: "qwen3-coder:30b", detail: "larger coding model; substantially more memory; no maintained quality baseline"},
		{tag: "qwen2.5-coder:7b", detail: "smaller coding model; lower resource use; no maintained quality baseline"},
		{tag: "deepseek-coder-v2:16b", detail: "mid-sized coding model; no maintained quality baseline"},
		{tag: "codestral:22b", detail: "larger coding model; no maintained quality baseline"},
	}
}

func promptOllamaModel(prompt promptSession, current string, installed []string) (string, error) {
	current = strings.TrimSpace(current)
	if current == "" {
		current = defaultSetupModel
	}
	options := []setupModelOption{{tag: current, detail: modelStatus(current, installed)}}
	for _, model := range installed {
		options = appendUniqueModel(options, setupModelOption{tag: model, detail: modelStatus(model, installed)})
	}
	for _, recommendation := range recommendedOllamaModels() {
		options = appendUniqueModel(options, recommendation)
	}
	if prompt.selectOne != nil {
		choices := make([]terminalChoice, 0, len(options)+1)
		choices = append(choices, terminalChoice{Label: options[0].tag + " — " + options[0].detail, Value: options[0].tag})
		choices = append(choices, terminalChoice{
			Label: "Use another Ollama model — enter any exact installed or Ollama library tag",
			Value: customModelChoice,
		})
		for _, option := range options[1:] {
			choices = append(choices, terminalChoice{Label: option.tag + " — " + option.detail, Value: option.tag})
		}
		selected, err := prompt.chooseOne("Ollama model", current, choices)
		if err != nil {
			return "", err
		}
		if selected == customModelChoice {
			return promptCustomModel(prompt, "Custom Ollama model tag")
		}
		return selected, nil
	}
	if _, err := fmt.Fprintln(prompt.output, "  Select an installed or recommended model, or type any Ollama model tag:"); err != nil {
		return "", err
	}
	defaultIndex := 1
	for index, option := range options {
		if strings.EqualFold(option.tag, current) {
			defaultIndex = index + 1
		}
		if _, err := fmt.Fprintf(prompt.output, "    %d) %s — %s\n", index+1, option.tag, option.detail); err != nil {
			return "", err
		}
	}
	for {
		value, err := prompt.text("Ollama model number or tag", fmt.Sprintf("%d", defaultIndex))
		if err != nil {
			return "", err
		}
		var selected int
		if _, scanErr := fmt.Sscanf(value, "%d", &selected); scanErr == nil {
			if selected >= 1 && selected <= len(options) && value == fmt.Sprintf("%d", selected) {
				return options[selected-1].tag, nil
			}
			if _, writeErr := fmt.Fprintf(prompt.output, "  Enter a number from 1 to %d, or an exact Ollama model tag.\n", len(options)); writeErr != nil {
				return "", writeErr
			}
			continue
		}
		if strings.ContainsAny(value, "\r\n\x00") || strings.TrimSpace(value) == "" {
			if _, writeErr := fmt.Fprintln(prompt.output, "  Enter a valid Ollama model tag."); writeErr != nil {
				return "", writeErr
			}
			continue
		}
		return strings.TrimSpace(value), nil
	}
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

func appendUniqueModel(options []setupModelOption, option setupModelOption) []setupModelOption {
	for _, existing := range options {
		if strings.EqualFold(existing.tag, option.tag) {
			return options
		}
	}
	return append(options, option)
}

func modelStatus(model string, installed []string) string {
	for _, candidate := range installed {
		if strings.EqualFold(candidate, model) {
			if strings.EqualFold(model, defaultSetupModel) {
				return "installed; recommended default; onboarding benchmark recorded; compatibility checked after selection"
			}
			if strings.EqualFold(model, "qwen3:8b") {
				return "installed; technical-review baseline recorded; compatibility checked after selection"
			}
			return "installed; compatibility checked automatically after selection"
		}
	}
	if strings.EqualFold(model, defaultSetupModel) {
		return "recommended default; not currently installed; onboarding benchmark recorded"
	}
	if strings.EqualFold(model, "qwen3:8b") {
		return "technical-review baseline recorded; not currently installed"
	}
	return "configured; not currently installed; compatibility checked after installation"
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
	label := "quick preliminary scan"
	args := []string{"--quick", target}
	if mode == setupScanDeep {
		label = "deep AI-assisted scan"
		args = []string{"--deep", target}
	}
	if _, err := fmt.Fprintf(stdout, "\nStarting first %s...\n", label); err != nil {
		return err
	}
	command := newScanCommandWithDiscovery(stdout, build, &scanDiscoverySeed{Target: target, Discovery: discovered})
	command.SilenceErrors = true
	command.SilenceUsage = true
	command.SetIn(parent.InOrStdin())
	command.SetOut(stdout)
	command.SetErr(parent.ErrOrStderr())
	command.SetArgs(args)
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
