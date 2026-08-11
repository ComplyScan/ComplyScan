package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/spf13/cobra"
)

func newOwnershipCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "ownership",
		Short: "Configure repository paths belonging to each declared AI system",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newOwnershipShowCommand(stdout))
	command.AddCommand(newOwnershipSetupCommand(stdout))
	return command
}

func newOwnershipShowCommand(stdout io.Writer) *cobra.Command {
	var format, configPath string
	command := &cobra.Command{
		Use:   "show [path]",
		Short: "Show configured path-to-system ownership rules",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			cfg, path, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if path == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan setup` first", config.FileName, target)
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "terminal":
				return writeOwnershipTerminal(stdout, cfg.Ownership, false)
			case "json":
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(struct {
					Rules []ownership.Rule `json:"rules"`
				}{Rules: cfg.Ownership})
			default:
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	return command
}

func newOwnershipSetupCommand(stdout io.Writer) *cobra.Command {
	var configPath string
	var forceInteractive bool
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Interactively replace path-to-system ownership rules",
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
				return fmt.Errorf("no %s found for %q; run `complyscan setup` first", config.FileName, target)
			}
			if len(cfg.Systems) == 0 {
				return errors.New("ownership setup needs at least one declared system; run `complyscan profile setup` first")
			}
			if !forceInteractive && !isInteractiveReader(cmd.InOrStdin()) {
				return errors.New("ownership setup requires a terminal; use --interactive when piping answers or edit .complyscan.yml directly")
			}
			prompt := newPromptSession(cmd.InOrStdin(), stdout)
			changed, err := collectOwnershipRules(prompt, &cfg, false)
			if err != nil {
				return err
			}
			if !changed {
				_, err = fmt.Fprintln(stdout, "Path ownership was not changed.")
				return err
			}
			if err := config.Write(path, cfg, true); err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Saved %d ownership rule(s) in %s\n", len(cfg.Ownership), path)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "configure ownership even when input is redirected")
	return command
}

func offerOwnershipSetup(prompt promptSession, cfg *config.Config) error {
	if len(cfg.Systems) < 2 {
		return nil
	}
	if err := explainSetupQuestion(prompt, "path-ownership"); err != nil {
		return err
	}
	configure, err := prompt.confirm("Configure path ownership for the declared systems now", true)
	if err != nil {
		return err
	}
	if !configure {
		_, err = fmt.Fprintln(prompt.output, "Evidence will remain unassigned until ownership is configured with `complyscan ownership setup`.")
		return err
	}
	_, err = collectOwnershipRules(prompt, cfg, true)
	return err
}

func collectOwnershipRules(prompt promptSession, cfg *config.Config, alreadyConfirmed bool) (bool, error) {
	if !alreadyConfirmed {
		if err := explainSetupQuestion(prompt, "path-ownership"); err != nil {
			return false, err
		}
	}
	if len(cfg.Ownership) > 0 {
		if err := explainSetupQuestion(prompt, "replace-ownership"); err != nil {
			return false, err
		}
		replace, err := prompt.confirm(fmt.Sprintf("Replace the existing %d ownership rule(s)", len(cfg.Ownership)), false)
		if err != nil || !replace {
			return false, err
		}
	}

	systemIDs := make([]string, 0, len(cfg.Systems))
	for _, system := range cfg.Systems {
		systemIDs = append(systemIDs, system.ID)
	}
	if _, err := fmt.Fprintf(prompt.output, "\nDeclared systems: %s\n", strings.Join(systemIDs, ", ")); err != nil {
		return false, err
	}
	rules := make([]ownership.Rule, 0, len(systemIDs))
	for {
		if err := explainSetupQuestion(prompt, "ownership-paths"); err != nil {
			return false, err
		}
		paths, err := prompt.textList("Repository path patterns", nil)
		if err != nil {
			return false, err
		}
		if err := explainSetupQuestion(prompt, "ownership-systems"); err != nil {
			return false, err
		}
		owners, err := promptChoices(prompt, "Owning systems", []string{systemIDs[0]}, systemIDs...)
		if err != nil {
			return false, err
		}
		rules = append(rules, ownership.Rule{Paths: paths, Systems: owners})
		more, err := prompt.confirm("Add another path ownership rule", false)
		if err != nil {
			return false, err
		}
		if !more {
			break
		}
	}
	if err := ownership.Validate(rules, systemIDs); err != nil {
		return false, fmt.Errorf("validate path ownership: %w", err)
	}
	cfg.Ownership = rules
	if err := writeOwnershipTerminal(prompt.output, rules, prompt.styleTitles); err != nil {
		return false, err
	}
	return true, nil
}

func writeOwnershipTerminal(output io.Writer, rules []ownership.Rule, bold bool) error {
	if err := writeSectionTitle(output, "Path ownership", bold, true); err != nil {
		return err
	}
	if len(rules) == 0 {
		_, err := fmt.Fprintln(output, "  No rules configured. Multi-system evidence will remain unassigned.")
		return err
	}
	for index, rule := range rules {
		if _, err := fmt.Fprintf(output, "  %d. %s -> %s\n", index+1, strings.Join(rule.Paths, ", "), strings.Join(rule.Systems, ", ")); err != nil {
			return err
		}
	}
	return nil
}
