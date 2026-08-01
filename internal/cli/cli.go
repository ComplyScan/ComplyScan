package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
	"github.com/1eonardodawinki/ComplyScan/internal/report"
	"github.com/1eonardodawinki/ComplyScan/internal/rules"
	"github.com/1eonardodawinki/ComplyScan/internal/scanner"
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
		format                    string
		minimum                   string
		configPath                string
		noColor                   bool
		additionalExcludes        []string
		trackedOnly               bool
		includeNestedRepositories bool
		maxFiles                  int
		maxTotalBytes             int64
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
			if cmd.Flags().Changed("max-files") && maxFiles <= 0 {
				return errors.New("--max-files must be greater than zero")
			}
			if cmd.Flags().Changed("max-total-bytes") && maxTotalBytes <= 0 {
				return errors.New("--max-total-bytes must be greater than zero")
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
			scanOptions := scanner.Options{
				Exclude:                   append(append([]string(nil), cfg.Scan.Exclude...), additionalExcludes...),
				MaxFiles:                  effectiveMaxFiles,
				MaxTotalBytes:             effectiveMaxTotalBytes,
				IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories || includeNestedRepositories,
				TrackedOnly:               cfg.Scan.TrackedOnly || trackedOnly,
				RuleEnabled:               cfg.RuleEnabled,
				Suppress:                  cfg.FindingSuppressed,
			}
			if outputFormat == "terminal" {
				if _, err := fmt.Fprintf(stdout, "ComplyScan scanning %s...\n\n", target); err != nil {
					return fmt.Errorf("write terminal report: %w", err)
				}
				scanOptions.OnFinding = func(finding rules.Finding) error {
					if rules.SeverityRank(finding.Severity) < rules.SeverityRank(minimumSeverity) {
						return nil
					}
					return report.WriteTerminalFinding(stdout, finding, terminalOptions)
				}
				scanOptions.OnProgress = func(progress discovery.Progress) error {
					if progress.Done {
						_, err := fmt.Fprintf(stdout, "Discovery complete: %d files, %s read\n\n", progress.Stats.FilesRead, formatByteCount(progress.Stats.BytesRead))
						return err
					}
					_, err := fmt.Fprintf(stdout, "Discovery progress: %d files, %s read\n", progress.Stats.FilesRead, formatByteCount(progress.Stats.BytesRead))
					return err
				}
			}

			result, err := scanner.New().Scan(cmd.Context(), target, scanOptions)
			if err != nil {
				return fmt.Errorf("scan %q: %w", target, err)
			}
			visible := report.FilterByMinimum(result.Findings, minimumSeverity)
			reportValue := report.New(target, build.Version, visible, result.Warnings, result.Suppressed)
			if outputFormat == "json" {
				if err := report.WriteJSON(stdout, reportValue); err != nil {
					return err
				}
			} else {
				if err := report.WriteTerminalCompletion(stdout, reportValue); err != nil {
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
	command.Flags().StringArrayVar(&additionalExcludes, "exclude", nil, "exclude a path or directory name (repeatable)")
	command.Flags().BoolVar(&trackedOnly, "tracked-only", false, "scan only files tracked by Git")
	command.Flags().BoolVar(&includeNestedRepositories, "include-nested-repositories", false, "scan inside nested Git repositories")
	command.Flags().IntVar(&maxFiles, "max-files", 0, "maximum number of text files to read")
	command.Flags().Int64Var(&maxTotalBytes, "max-total-bytes", 0, "maximum total bytes of text content to read")
	return command
}

func formatByteCount(value int64) string {
	const mebibyte = 1 << 20
	if value < mebibyte {
		return fmt.Sprintf("%.1f KiB", float64(value)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/mebibyte)
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
