package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/spf13/cobra"
)

func newActionsCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	command := &cobra.Command{
		Use:   "actions",
		Short: "List, inspect, and verify developer compliance actions",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newActionsListCommand(stdout))
	command.AddCommand(newActionsShowCommand(stdout))
	command.AddCommand(newActionsVerifyCommand(stdout, build))
	return command
}

func newActionsListCommand(stdout io.Writer) *cobra.Command {
	var format, status, reportPath string
	command := &cobra.Command{
		Use:   "list [path]",
		Short: "List actions from the latest evidence bundle without rescanning",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := optionalTarget(args)
			value, resolved, err := readActionsReport(target, reportPath)
			if err != nil {
				return err
			}
			actions, err := filterDeveloperActions(value.DeveloperActions, status)
			if err != nil {
				return err
			}
			sortDeveloperActions(actions)
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "terminal":
				return writeDeveloperActionsTerminal(stdout, actions, resolved)
			case "json":
				return writeDeveloperActionsJSON(stdout, struct {
					ReportPath string                   `json:"report_path"`
					Actions    []report.DeveloperAction `json:"actions"`
				}{ReportPath: resolved, Actions: actions})
			case "count":
				_, err := fmt.Fprintln(stdout, len(actions))
				return err
			default:
				return fmt.Errorf("invalid format %q (want terminal, json, or count)", format)
			}
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal, json, or count")
	command.Flags().StringVar(&status, "status", "active", "action status: active, all, new, open, reopened, resolved, or accepted")
	command.Flags().StringVar(&reportPath, "report", "", "JSON evidence bundle (defaults to <path>/.complyscan/reports/latest.json)")
	return command
}

func newActionsShowCommand(stdout io.Writer) *cobra.Command {
	var format, reportPath string
	command := &cobra.Command{
		Use:   "show <action-id> [path]",
		Short: "Show one action with evidence and acceptance criteria",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			target := "."
			if len(args) == 2 {
				target = args[1]
			}
			value, _, err := readActionsReport(target, reportPath)
			if err != nil {
				return err
			}
			action, ok := findDeveloperAction(value.DeveloperActions, args[0])
			if !ok {
				return fmt.Errorf("action %q was not found in the latest evidence bundle", args[0])
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "terminal":
				return writeDeveloperActionTerminal(stdout, action)
			case "json":
				return writeDeveloperActionsJSON(stdout, action)
			default:
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	command.Flags().StringVar(&reportPath, "report", "", "JSON evidence bundle (defaults to <path>/.complyscan/reports/latest.json)")
	return command
}

func newActionsVerifyCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify <action-id> [path]",
		Short: "Run a fresh scan and check whether an action is resolved",
		Long:  "Run the repository's normal configured scan, update the local evidence bundle, and check the requested stable action ID. This uses the same provider consent and cache rules as `complyscan scan`.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 2 {
				target = args[1]
			}
			var scanOutput bytes.Buffer
			scan := newScanCommand(&scanOutput, build)
			scan.SetIn(cmd.InOrStdin())
			scan.SetOut(&scanOutput)
			scan.SetErr(cmd.ErrOrStderr())
			scan.SetContext(cmd.Context())
			if err := scan.Flags().Set("format", "json"); err != nil {
				return err
			}
			scanErr := scan.RunE(scan, []string{target})
			var status *exitError
			if scanErr != nil && (!errors.As(scanErr, &status) || status.code != 1) {
				return scanErr
			}
			var value report.Report
			if err := json.Unmarshal(scanOutput.Bytes(), &value); err != nil {
				return fmt.Errorf("decode verification scan: %w", err)
			}
			action, ok := findDeveloperAction(value.DeveloperActions, args[0])
			if !ok {
				return fmt.Errorf("verification scan did not retain action %q; inspect the latest report before treating it as resolved", args[0])
			}
			if action.Status == report.DeveloperActionResolved || action.Status == report.DeveloperActionAccepted {
				_, err := fmt.Fprintf(stdout, "Resolved %s: %s\n", action.ID, action.Title)
				return err
			}
			if _, err := fmt.Fprintf(stdout, "Still %s %s: %s\n", action.Status, action.ID, action.Title); err != nil {
				return err
			}
			return &exitError{code: 1}
		},
	}
	return command
}

func readActionsReport(target, override string) (report.Report, string, error) {
	path := strings.TrimSpace(override)
	if path == "" {
		path = filepath.Join(target, report.DefaultDirectory, "latest.json")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(target, path)
	}
	value, err := report.ReadJSONFile(path)
	if err != nil {
		return report.Report{}, path, err
	}
	value = report.EnsureDeveloperActions(value)
	return value, path, nil
}

func filterDeveloperActions(actions []report.DeveloperAction, status string) ([]report.DeveloperAction, error) {
	requested := strings.ToLower(strings.TrimSpace(status))
	if requested == "" {
		requested = "active"
	}
	result := make([]report.DeveloperAction, 0, len(actions))
	for _, action := range actions {
		include := false
		switch requested {
		case "all":
			include = true
		case "active":
			include = action.Status == report.DeveloperActionNew || action.Status == report.DeveloperActionOpen || action.Status == report.DeveloperActionReopened
		case "new", "open", "reopened", "resolved", "accepted":
			include = string(action.Status) == requested
		default:
			return nil, fmt.Errorf("invalid status %q (want active, all, new, open, reopened, resolved, or accepted)", status)
		}
		if include {
			result = append(result, action)
		}
	}
	return result, nil
}

func sortDeveloperActions(actions []report.DeveloperAction) {
	priority := map[string]int{"Critical": 0, "High": 1, "Medium": 2, "Review": 3, "Verify": 4}
	sort.SliceStable(actions, func(left, right int) bool {
		leftPriority, leftOK := priority[actions[left].Priority]
		if !leftOK {
			leftPriority = 3
		}
		rightPriority, rightOK := priority[actions[right].Priority]
		if !rightOK {
			rightPriority = 3
		}
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return actions[left].ID < actions[right].ID
	})
}

func findDeveloperAction(actions []report.DeveloperAction, id string) (report.DeveloperAction, bool) {
	for _, action := range actions {
		if action.ID == id {
			return action, true
		}
	}
	return report.DeveloperAction{}, false
}

func writeDeveloperActionsJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode developer actions: %w", err)
	}
	return nil
}

func writeDeveloperActionsTerminal(stdout io.Writer, actions []report.DeveloperAction, reportPath string) error {
	if len(actions) == 0 {
		_, err := fmt.Fprintf(stdout, "No matching developer actions in %s\n", reportPath)
		return err
	}
	if _, err := fmt.Fprintf(stdout, "%d developer action(s) from %s\n\n", len(actions), reportPath); err != nil {
		return err
	}
	for _, action := range actions {
		if _, err := fmt.Fprintf(stdout, "[%s] %s · %s\n  %s\n", action.Status, action.Priority, action.ID, action.Title); err != nil {
			return err
		}
		if len(action.Evidence) > 0 && action.Evidence[0].Path != "" {
			if _, err := fmt.Fprintf(stdout, "  Evidence: %s\n", developerActionLocation(action.Evidence[0])); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeDeveloperActionTerminal(stdout io.Writer, action report.DeveloperAction) error {
	if _, err := fmt.Fprintf(stdout, "%s · %s · %s\n%s\n\nWhy: %s\n\nRecommended change: %s\n", action.ID, action.Status, action.Priority, action.Title, action.Why, action.RecommendedChange); err != nil {
		return err
	}
	if len(action.AcceptanceCriteria) > 0 {
		if _, err := fmt.Fprintln(stdout, "\nDone when:"); err != nil {
			return err
		}
		for _, criterion := range action.AcceptanceCriteria {
			if _, err := fmt.Fprintf(stdout, "- %s\n", criterion); err != nil {
				return err
			}
		}
	}
	if len(action.Evidence) > 0 {
		if _, err := fmt.Fprintln(stdout, "\nEvidence:"); err != nil {
			return err
		}
		for _, evidence := range action.Evidence {
			location := developerActionLocation(evidence)
			if evidence.Summary != "" {
				location += " — " + evidence.Summary
			}
			if _, err := fmt.Fprintf(stdout, "- %s\n", location); err != nil {
				return err
			}
		}
	}
	if len(action.Frameworks) > 0 {
		if _, err := fmt.Fprintln(stdout, "\nFramework mapping:"); err != nil {
			return err
		}
		for _, framework := range action.Frameworks {
			if _, err := fmt.Fprintf(stdout, "- %s: %s\n", framework.Name, framework.SourceReference); err != nil {
				return err
			}
		}
	}
	return nil
}

func developerActionLocation(evidence report.DeveloperActionEvidence) string {
	if evidence.Path == "" {
		return "report context"
	}
	if evidence.StartLine > 0 {
		return fmt.Sprintf("%s:%d", evidence.Path, evidence.StartLine)
	}
	return evidence.Path
}
