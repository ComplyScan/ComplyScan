package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/framework"
	"github.com/spf13/cobra"
)

func newFrameworkCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "framework",
		Short: "List versioned technical packs and inspect code evidence",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newFrameworkListCommand(stdout))
	command.AddCommand(newFrameworkAssessCommand(stdout))
	return command
}

func newFrameworkListCommand(stdout io.Writer) *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "list",
		Short: "List built-in technical evidence packs and their boundaries",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			outputFormat := strings.ToLower(strings.TrimSpace(format))
			if outputFormat != "terminal" && outputFormat != "json" {
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
			listings, err := framework.ListBuiltins()
			if err != nil {
				return err
			}
			if outputFormat == "json" {
				return framework.WriteJSON(stdout, listings)
			}
			return framework.WritePackListTerminal(stdout, listings)
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	return command
}

func newFrameworkAssessCommand(stdout io.Writer) *cobra.Command {
	var (
		format                    string
		packID                    string
		configPath                string
		additionalExcludes        []string
		trackedOnly               bool
		includeNestedRepositories bool
		maxFiles                  int
		maxTotalBytes             int64
	)
	command := &cobra.Command{
		Use:   "assess [path]",
		Short: "Map code evidence candidates to versioned technical objectives",
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
			pack, err := framework.LoadBuiltin(packID)
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
			excludes := append(append([]string(nil), cfg.Scan.Exclude...), additionalExcludes...)
			if cfg.Baseline != "" {
				if exclusion := targetExclusion(target, cfg.Baseline); exclusion != "" {
					excludes = append(excludes, exclusion)
				}
			}
			discoveryOptions := discovery.Options{
				Exclude: excludes, MaxFiles: effectiveMaxFiles, MaxTotalBytes: effectiveMaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
			}
			if outputFormat == "terminal" {
				if _, err := fmt.Fprintf(stdout, "ComplyScan assessing %s against %s...\n\n", target, packID); err != nil {
					return err
				}
				discoveryOptions.OnProgress = terminalProgress(stdout)
			}
			discovered, err := discovery.Discover(cmd.Context(), target, discoveryOptions)
			if err != nil {
				return fmt.Errorf("discover framework evidence in %q: %w", target, err)
			}
			report := framework.Evaluate(pack, cfg.Systems, discovered.Repository)
			report.Target = target
			report.Warnings = append([]string(nil), discovered.Warnings...)
			if outputFormat == "json" {
				return framework.WriteJSON(stdout, report)
			}
			if err := framework.WriteTechnicalEvidenceTerminal(stdout, report); err != nil {
				return err
			}
			return nil
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&packID, "pack", framework.EUAIActTechnicalEvidencePackID, "built-in technical evidence pack ID")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().StringArrayVar(&additionalExcludes, "exclude", nil, "exclude a path or directory name (repeatable)")
	command.Flags().BoolVar(&trackedOnly, "tracked-only", false, "assess only files tracked by Git")
	command.Flags().BoolVar(&includeNestedRepositories, "include-nested-repositories", false, "assess inside nested Git repositories")
	command.Flags().IntVar(&maxFiles, "max-files", 0, "maximum number of text files to read")
	command.Flags().Int64Var(&maxTotalBytes, "max-total-bytes", 0, "maximum total bytes of text content to read")
	return command
}
