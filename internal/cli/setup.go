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
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
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
	installOllama     bool
	skipOllamaInstall bool
	skipScan          bool
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
	command.Flags().BoolVar(&options.installOllama, "install-ollama", false, "install Ollama when it is missing (requires explicit use in non-interactive mode)")
	command.Flags().BoolVar(&options.skipOllamaInstall, "skip-ollama-install", false, "do not offer to install Ollama when it is missing")
	command.Flags().BoolVar(&options.skipScan, "skip-scan", false, "do not offer to run the first scan")
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

	if _, err := fmt.Fprintln(stdout, "ComplyScan setup"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Repository: %s\n\n", target); err != nil {
		return err
	}

	prompt := newPromptSession(cmd.InOrStdin(), stdout)
	var repositorySummary setupRepositorySummary
	if interactive {
		summary, inspectErr := inspectRepositoryForSetup(cmd.Context(), stdout, target, cfg, build)
		if inspectErr != nil {
			return inspectErr
		}
		repositorySummary = summary
		var system profile.System
		var collectErr error
		if options.advanced {
			if err := configureFrameworkSelection(prompt, &cfg, true, options.frameworks); err != nil {
				return err
			}
			system, collectErr = collectSystemProfileWithPrompt(prompt, target, time.Now(), cfg.Frameworks...)
		} else {
			system, collectErr = collectBasicSystemProfile(prompt, target, time.Now(), summary)
			if collectErr == nil {
				collectErr = configureRecommendedFrameworks(prompt, &cfg, system, options.frameworks)
				applyFrameworksToSystem(&system, cfg.Frameworks)
			}
			if collectErr == nil && frameworkEnabled(cfg.Frameworks, framework.EUAIActTechnicalEvidencePackID) {
				collectErr = collectRelevantEUApplicabilityContext(prompt, &system, time.Now())
			}
		}
		if collectErr != nil {
			return collectErr
		}
		index := systemIndex(cfg.Systems, system.ID)
		if index >= 0 {
			if explainErr := explainSetupQuestion(prompt, "replace-profile"); explainErr != nil {
				return explainErr
			}
			replace, confirmErr := prompt.confirm(fmt.Sprintf("Replace existing system profile %q", system.ID), true)
			if confirmErr != nil {
				return confirmErr
			}
			if replace {
				cfg.Systems[index] = system
			}
		} else {
			cfg.Systems = append(cfg.Systems, system)
		}
		if err := offerOwnershipSetup(prompt, &cfg); err != nil {
			return err
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
	scanMode := setupScanNone
	if interactive && !options.skipScan {
		scanMode, err = promptSetupScanMode(prompt, repositorySummary)
		if err != nil {
			return err
		}
	}
	configureReview := !interactive || options.skipScan || scanMode == setupScanDeep || setupReviewExplicit(options)
	modelReady := true
	if configureReview {
		modelReady, err = configureSetupReview(cmd.Context(), prompt, stdout, &cfg, interactive, options)
		if err != nil {
			return err
		}
	}
	if err := config.Write(path, cfg, existed); err != nil {
		return err
	}
	if err := ensureReportGitIgnore(target); err != nil {
		return fmt.Errorf("saved %s but could not ignore generated reports: %w", path, err)
	}
	if _, err := fmt.Fprintf(stdout, "\nSaved %s with %d system profile(s).\n", path, len(cfg.Systems)); err != nil {
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
	return runFirstScan(cmd, stdout, build, target, scanMode)
}

func setupReviewExplicit(options setupOptions) bool {
	return options.reviewProvider != "" || options.ollamaModel != "" || options.remoteModel != "" || options.remoteAPIKeyEnv != "" ||
		options.allowRemoteReview || options.pullModel || options.skipModelPull || options.installOllama || options.skipOllamaInstall
}

func configureSetupReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := strings.ToLower(strings.TrimSpace(options.reviewProvider))
	if provider == "" && interactive {
		if err := explainSetupQuestion(prompt, "review-provider"); err != nil {
			return false, err
		}
		defaultProvider := cfg.AI.Provider
		if defaultProvider == "none" || defaultProvider == "" {
			defaultProvider = "ollama"
		}
		selected, err := promptChoice(prompt, "Advisory review provider", defaultProvider, "none", "ollama", "openai", "anthropic", "gemini")
		if err != nil {
			return false, err
		}
		provider = selected
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
	cfg.AI.Provider = provider
	if provider == "none" {
		if _, err := fmt.Fprintln(stdout, "Local AI review disabled. Deterministic scanning remains available."); err != nil {
			return false, err
		}
		return true, nil
	}
	if isRemoteReviewProvider(provider) {
		return configureRemoteReview(prompt, stdout, cfg, interactive, options)
	}

	ollamaPath, _ := exec.LookPath("ollama")
	model := strings.TrimSpace(options.ollamaModel)
	if model == "" && interactive {
		installed := []string{}
		if ollamaPath != "" {
			installed = ollamaInstalledModels(ctx, ollamaPath)
		}
		if _, err := fmt.Fprintln(stdout, "\nLocal model setup"); err != nil {
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
		return true, nil
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
	if _, err := fmt.Fprintf(stdout, "Downloading %s with Ollama...\n", model); err != nil {
		return false, err
	}
	command := exec.CommandContext(ctx, ollamaPath, "pull", model)
	command.Stdout = stdout
	command.Stderr = stdout
	if err := command.Run(); err != nil {
		if _, writeErr := fmt.Fprintf(stdout, "Model download did not complete: %v\nStart Ollama with `ollama serve`, then run: ollama pull %s\n", err, shellQuote(model)); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	return true, nil
}

func configureRemoteReview(prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := cfg.AI.Provider
	allowed := options.allowRemoteReview
	if interactive {
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
	return true, nil
}

func promptRemoteModel(prompt promptSession, provider string) (string, error) {
	models := remoteModelOptions(provider)
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

func promptOllamaModel(prompt promptSession, current string, installed []string) (string, error) {
	options := []setupModelOption{
		{tag: defaultSetupModel, detail: "recommended default; live ComplyScan validation pending"},
		{tag: "qwen3:8b", detail: "smaller previously validated model"},
		{tag: "qwen3-coder:30b", detail: "larger coding model; substantially more memory; unvalidated"},
	}
	if current = strings.TrimSpace(current); current != "" {
		options = prependUniqueModel(options, setupModelOption{tag: current, detail: modelStatus(current, installed)})
	}
	for _, model := range installed {
		options = appendUniqueModel(options, setupModelOption{tag: model, detail: modelStatus(model, installed)})
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

func prependUniqueModel(options []setupModelOption, option setupModelOption) []setupModelOption {
	result := []setupModelOption{option}
	for _, existing := range options {
		if !strings.EqualFold(existing.tag, option.tag) {
			result = append(result, existing)
		}
	}
	return result
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
				return "installed; recommended default; live validation pending"
			}
			if strings.EqualFold(model, "qwen3:8b") {
				return "installed; previously validated model"
			}
			return "installed; compatibility not yet validated by ComplyScan"
		}
	}
	if strings.EqualFold(model, defaultSetupModel) {
		return "recommended default; not currently installed; live validation pending"
	}
	if strings.EqualFold(model, "qwen3:8b") {
		return "previously validated model; not currently installed"
	}
	return "configured; not currently installed or validated"
}

func setupModelDefault(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return defaultSetupModel
}

func systemIndex(systems []profile.System, id string) int {
	for index := range systems {
		if systems[index].ID == id {
			return index
		}
	}
	return -1
}

func runFirstScan(parent *cobra.Command, stdout io.Writer, build BuildInfo, target string, mode setupScanMode) error {
	label := "quick preliminary scan"
	args := []string{"--quick", target}
	if mode == setupScanDeep {
		label = "deep AI-assisted scan"
		args = []string{"--deep", target}
	}
	if _, err := fmt.Fprintf(stdout, "\nStarting first %s...\n", label); err != nil {
		return err
	}
	command := newScanCommand(stdout, build)
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
