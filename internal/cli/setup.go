package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
	"github.com/spf13/cobra"
)

const defaultSetupModel = "qwen3:8b"

type setupOptions struct {
	configPath       string
	forceInteractive bool
	nonInteractive   bool
	reviewProvider   string
	ollamaModel      string
	pullModel        bool
	skipModelPull    bool
	skipScan         bool
}

func newSetupCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var options setupOptions
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Configure a repository, applicability context, and local AI review",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.forceInteractive && options.nonInteractive {
				return errors.New("--interactive and --non-interactive cannot be used together")
			}
			if options.pullModel && options.skipModelPull {
				return errors.New("--pull-model and --skip-model-pull cannot be used together")
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
	command.Flags().StringVar(&options.reviewProvider, "review", "", "advisory review provider: none or ollama")
	command.Flags().StringVar(&options.ollamaModel, "ollama-model", "", "Ollama model name")
	command.Flags().BoolVar(&options.pullModel, "pull-model", false, "download the configured Ollama model (requires --review ollama in non-interactive mode)")
	command.Flags().BoolVar(&options.skipModelPull, "skip-model-pull", false, "configure Ollama without offering to download the model")
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

	prompt := promptSession{reader: bufio.NewReader(cmd.InOrStdin()), output: stdout}
	if interactive {
		system, collectErr := collectSystemProfileWithPrompt(prompt, target, time.Now())
		if collectErr != nil {
			return collectErr
		}
		index := systemIndex(cfg.Systems, system.ID)
		if index >= 0 {
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
	} else if !existed {
		if _, err := fmt.Fprintln(stdout, "Non-interactive setup: no system profile was collected."); err != nil {
			return err
		}
	}

	modelReady, err := configureSetupReview(cmd.Context(), prompt, stdout, &cfg, interactive, options)
	if err != nil {
		return err
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

	if !interactive || options.skipScan {
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan %s\n", shellDisplayPath(target))
		return err
	}
	if cfg.AI.Provider == "ollama" && !modelReady {
		if _, err := fmt.Fprintf(stdout, "First scan not started because Ollama model %q is not ready.\n", cfg.AI.Ollama.Model); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "After Ollama is ready, run: ollama pull %s && complyscan scan %s\n", cfg.AI.Ollama.Model, shellDisplayPath(target))
		return err
	}
	runFirst, err := prompt.confirm("Run the first scan now", true)
	if err != nil {
		return err
	}
	if !runFirst {
		_, err := fmt.Fprintf(stdout, "Next: complyscan scan %s\n", shellDisplayPath(target))
		return err
	}
	return runFirstScan(cmd, stdout, build, target)
}

func configureSetupReview(ctx context.Context, prompt promptSession, stdout io.Writer, cfg *config.Config, interactive bool, options setupOptions) (bool, error) {
	provider := strings.ToLower(strings.TrimSpace(options.reviewProvider))
	if provider == "" && interactive {
		enable, err := prompt.confirm("Enable local AI review with Ollama", true)
		if err != nil {
			return false, err
		}
		if enable {
			provider = "ollama"
		} else {
			provider = "none"
		}
	}
	if provider == "" {
		provider = cfg.AI.Provider
	}
	if provider != "none" && provider != "ollama" {
		return false, fmt.Errorf("invalid review provider %q (want none or ollama)", provider)
	}
	if options.ollamaModel != "" && provider != "ollama" {
		return false, errors.New("--ollama-model requires --review ollama or an interactive Ollama selection")
	}
	if options.pullModel && provider != "ollama" {
		return false, errors.New("--pull-model requires --review ollama")
	}
	cfg.AI.Provider = provider
	if provider == "none" {
		if _, err := fmt.Fprintln(stdout, "Local AI review disabled. Deterministic scanning remains available."); err != nil {
			return false, err
		}
		return true, nil
	}

	model := strings.TrimSpace(options.ollamaModel)
	if model == "" && interactive {
		if _, err := fmt.Fprintln(stdout, "\nLocal model setup\n  Recommended: qwen3:8b\n  Larger coding model: qwen3-coder:30b\n  You may enter any local Ollama model tag."); err != nil {
			return false, err
		}
		var err error
		model, err = prompt.text("Ollama model", setupModelDefault(cfg.AI.Ollama.Model))
		if err != nil {
			return false, err
		}
	}
	if model == "" {
		model = setupModelDefault(cfg.AI.Ollama.Model)
	}
	cfg.AI.Ollama.Model = model

	ollamaPath, err := exec.LookPath("ollama")
	if err != nil {
		if _, writeErr := fmt.Fprintln(stdout, "Ollama was not found on PATH. Install it from https://ollama.com/download, then rerun `complyscan setup` or pull the selected model manually."); writeErr != nil {
			return false, writeErr
		}
		return false, nil
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
		var confirmErr error
		shouldPull, confirmErr = prompt.confirm(fmt.Sprintf("Download Ollama model %q now", model), true)
		if confirmErr != nil {
			return false, confirmErr
		}
	}
	if !shouldPull {
		if _, err := fmt.Fprintf(stdout, "Model not downloaded. When ready, run: ollama pull %s\n", model); err != nil {
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
		return false, fmt.Errorf("download Ollama model %q: %w", model, err)
	}
	return true, nil
}

func ollamaModelInstalled(ctx context.Context, executable, model string) bool {
	command := exec.CommandContext(ctx, executable, "list")
	output, err := command.Output()
	if err != nil {
		return false
	}
	wanted := strings.ToLower(strings.TrimSpace(model))
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.ToLower(fields[0]) == wanted {
			return true
		}
	}
	return false
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

func runFirstScan(parent *cobra.Command, stdout io.Writer, build BuildInfo, target string) error {
	if _, err := fmt.Fprintln(stdout, "\nStarting first scan..."); err != nil {
		return err
	}
	command := newScanCommand(stdout, build)
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

func shellDisplayPath(path string) string {
	if path == "." {
		return "."
	}
	return fmt.Sprintf("%q", path)
}
