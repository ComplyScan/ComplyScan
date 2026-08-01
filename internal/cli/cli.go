package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/complyscan/complyscan/internal/config"
	"github.com/complyscan/complyscan/internal/report"
	"github.com/complyscan/complyscan/internal/rules"
	"github.com/complyscan/complyscan/internal/scanner"
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
	command := newRootCommand(stdout, stderr, build)
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
	root.AddCommand(newInitCommand(stdout))
	root.AddCommand(newVersionCommand(stdout, build))
	return root
}

func newScanCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	var (
		format     string
		minimum    string
		configPath string
		noColor    bool
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
			if outputFormat != "terminal" && outputFormat != "json" {
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
			minimumSeverity, err := rules.ParseSeverity(minimum)
			if err != nil {
				return fmt.Errorf("--severity: %w", err)
			}
			cfg, _, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}

			result, err := scanner.New().Scan(cmd.Context(), target, scanner.Options{
				Exclude: cfg.Scan.Exclude, RuleEnabled: cfg.RuleEnabled,
			})
			if err != nil {
				return fmt.Errorf("scan %q: %w", target, err)
			}
			visible := report.FilterByMinimum(result.Findings, minimumSeverity)
			reportValue := report.New(target, build.Version, visible, result.Warnings)
			if outputFormat == "json" {
				if err := report.WriteJSON(stdout, reportValue); err != nil {
					return err
				}
			} else {
				if err := report.WriteTerminal(stdout, reportValue, report.TerminalOptions{
					Color: !noColor && supportsColor(stdout),
				}); err != nil {
					return fmt.Errorf("write terminal report: %w", err)
				}
			}
			if report.MeetsThreshold(result.Findings, cfg.FailOn) {
				return &exitError{code: 1}
			}
			return nil
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&minimum, "severity", "info", "minimum severity to include in output")
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&noColor, "no-color", false, "disable ANSI colors")
	return command
}

func newInitCommand(stdout io.Writer) *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Create a default .complyscan.yml configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path := config.FileName
			if err := config.WriteDefault(path, force); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "Created %s\n", path)
			return err
		},
	}
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing configuration")
	return command
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
