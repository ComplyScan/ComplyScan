package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/spf13/cobra"
)

func newProfileCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Manage system context and inspect provisional applicability",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newProfileShowCommand(stdout))
	command.AddCommand(newProfileSetupCommand(stdout))
	return command
}

func newProfileShowCommand(stdout io.Writer) *cobra.Command {
	var format, configPath string
	command := &cobra.Command{
		Use:   "show [path]",
		Short: "Show declared system profiles and provisional applicability",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			outputFormat := strings.ToLower(strings.TrimSpace(format))
			if outputFormat != "terminal" && outputFormat != "json" {
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
			cfg, path, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if path == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan init` first", config.FileName, target)
			}
			report := profile.AssessEUAIAct(cfg.Systems)
			if outputFormat == "json" {
				return profile.WriteJSON(stdout, report)
			}
			return profile.WriteTerminal(stdout, report)
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	return command
}

func newProfileSetupCommand(stdout io.Writer) *cobra.Command {
	var (
		configPath       string
		forceInteractive bool
		replace          bool
	)
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Add or replace a guided system profile in existing configuration",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			cfg, path, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if path == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan init` first", config.FileName, target)
			}
			if !forceInteractive && !isInteractiveReader(cmd.InOrStdin()) {
				return errors.New("profile setup requires a terminal; use --interactive when piping answers or edit .complyscan.yml directly")
			}
			system, err := collectSystemProfile(cmd.InOrStdin(), stdout, target, time.Now())
			if err != nil {
				return err
			}
			index := -1
			for candidate := range cfg.Systems {
				if cfg.Systems[candidate].ID == system.ID {
					index = candidate
					break
				}
			}
			if index >= 0 && !replace {
				return fmt.Errorf("system profile %q already exists (use --replace to update it)", system.ID)
			}
			if index >= 0 {
				cfg.Systems[index] = system
			} else {
				cfg.Systems = append(cfg.Systems, system)
			}
			if err := config.Write(path, cfg, true); err != nil {
				return err
			}
			action := "Added"
			if index >= 0 {
				action = "Replaced"
			}
			_, err = fmt.Fprintf(stdout, "%s system profile %q in %s\n", action, system.ID, path)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "collect system context even when input is redirected")
	command.Flags().BoolVar(&replace, "replace", false, "replace an existing profile with the same system ID")
	return command
}
