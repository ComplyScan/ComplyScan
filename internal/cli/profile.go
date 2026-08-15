package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/framework"
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
			if profileShowIncludesEUAssessment(cfg.Frameworks) {
				report := profile.AssessEUAIAct(cfg.Systems)
				if outputFormat == "json" {
					return profile.WriteJSON(stdout, report)
				}
				return profile.WriteTerminal(stdout, report)
			}
			report, err := newDeclaredProfileReport(cfg.Frameworks, cfg.Systems)
			if err != nil {
				return err
			}
			if outputFormat == "json" {
				return writeDeclaredProfileJSON(stdout, report)
			}
			return writeDeclaredProfileTerminal(stdout, report)
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	return command
}

type declaredProfileFramework struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Nature          string `json:"nature"`
	SourceReference string `json:"source_reference"`
}

type declaredProfileReport struct {
	Frameworks []declaredProfileFramework `json:"selected_frameworks"`
	Systems    []profile.System           `json:"systems"`
	Notes      []string                   `json:"notes"`
}

func profileShowIncludesEUAssessment(frameworks []string) bool {
	// Configurations written before framework selection was introduced used the
	// EU assessment implicitly. Preserve that behavior for an absent selection.
	return len(frameworks) == 0 || frameworkEnabled(frameworks, framework.EUAIActTechnicalEvidencePackID)
}

func newDeclaredProfileReport(frameworkIDs []string, systems []profile.System) (declaredProfileReport, error) {
	report := declaredProfileReport{
		Frameworks: make([]declaredProfileFramework, 0, len(frameworkIDs)),
		Systems:    make([]profile.System, len(systems)),
		Notes: []string{
			"No legislation applicability screening is associated with the selected technical mappings.",
			"A voluntary-framework selection requests technical evidence mapping; it does not create a legal obligation or compliance conclusion.",
		},
	}
	for _, id := range frameworkIDs {
		pack, err := framework.LoadBuiltin(id)
		if err != nil {
			return declaredProfileReport{}, fmt.Errorf("load selected framework %q: %w", id, err)
		}
		report.Frameworks = append(report.Frameworks, declaredProfileFramework{
			ID: id, Name: pack.Name, Nature: pack.Coverage.Nature, SourceReference: pack.Source.Reference,
		})
	}
	copy(report.Systems, systems)
	for index := range report.Systems {
		// Applicability decisions belong to legislation-specific assessment
		// output. Do not expose a stale EU decision for a NIST-only selection.
		report.Systems[index].Applicability = nil
	}
	return report, nil
}

func writeDeclaredProfileJSON(writer io.Writer, report declaredProfileReport) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode declared profile JSON: %w", err)
	}
	return nil
}

func writeDeclaredProfileTerminal(writer io.Writer, report declaredProfileReport) error {
	if _, err := fmt.Fprintf(writer, "Declared system profiles: %d\n\n", len(report.Systems)); err != nil {
		return err
	}
	for _, system := range report.Systems {
		activities := "not established"
		if len(system.AIActivities) > 0 {
			values := make([]string, len(system.AIActivities))
			for index, activity := range system.AIActivities {
				values[index] = string(activity)
			}
			activities = strings.Join(values, ", ")
		}
		if _, err := fmt.Fprintf(writer, "%s (%s)\n  Intended purpose: %s\n  Lifecycle stage: %s\n  AI activities: %s\n  Profile review: %s\n\n",
			system.Name, system.ID, system.IntendedPurpose, system.LifecycleStage, activities, system.ProfileReview.Status); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer, "Selected technical mappings:"); err != nil {
		return err
	}
	for _, selected := range report.Frameworks {
		nature := strings.ReplaceAll(selected.Nature, "-", " ")
		if _, err := fmt.Fprintf(writer, "  %s (%s)\n    Source: %s\n", selected.Name, nature, selected.SourceReference); err != nil {
			return err
		}
	}
	for _, note := range report.Notes {
		if _, err := fmt.Fprintf(writer, "Note: %s\n", note); err != nil {
			return err
		}
	}
	return nil
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
			prompt := newPromptSession(cmd.InOrStdin(), stdout)
			system, err := collectSystemProfileWithPrompt(prompt, target, time.Now(), cfg.Frameworks...)
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
			if err := offerOwnershipSetup(prompt, &cfg); err != nil {
				return err
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
