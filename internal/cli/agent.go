package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newAgentCommand(stdout io.Writer, build BuildInfo) *cobra.Command {
	command := &cobra.Command{
		Use:   "agent",
		Short: "Connect coding agents to ComplyScan's evidence-backed workflow",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newAgentInstructionsCommand(stdout))
	command.AddCommand(newAgentServeCommand(stdout, build))
	return command
}

func newAgentInstructionsCommand(stdout io.Writer) *cobra.Command {
	var format string
	var write, force bool
	command := &cobra.Command{
		Use:   "instructions [path]",
		Short: "Print or install concise coding-agent instructions",
		Long:  "Print a concise AGENTS.md fragment or install a reusable ComplyScan skill. Printing is the safe default; files are written only with --write.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := optionalTarget(args)
			var content, destination string
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "agents":
				content = complyScanAgentInstructions()
				destination = filepath.Join(target, ".complyscan", "AGENT_GUIDE.md")
			case "skill":
				content = complyScanAgentSkill()
				destination = filepath.Join(target, ".agents", "skills", "complyscan-actions", "SKILL.md")
			default:
				return fmt.Errorf("invalid format %q (want agents or skill)", format)
			}
			if !write {
				_, err := fmt.Fprint(stdout, content)
				return err
			}
			if !force {
				if _, err := os.Lstat(destination); err == nil {
					return fmt.Errorf("refusing to overwrite %s; use --force after reviewing the existing file", destination)
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("inspect %s: %w", destination, err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return fmt.Errorf("create agent instruction directory: %w", err)
			}
			if err := os.WriteFile(destination, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write agent instructions: %w", err)
			}
			_, err := fmt.Fprintf(stdout, "Wrote %s\n", destination)
			return err
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "agents", "instruction format: agents or skill")
	command.Flags().BoolVar(&write, "write", false, "write the selected integration into the repository")
	command.Flags().BoolVar(&force, "force", false, "replace an existing generated integration")
	return command
}

func complyScanAgentInstructions() string {
	return `# ComplyScan workflow

- Treat ComplyScan as code-evidence guidance, not a legal compliance decision.
- Before compliance-related edits, run ` + "`complyscan actions list`" + ` and inspect the relevant item with ` + "`complyscan actions show <action-id>`" + `.
- Change only the code or configuration supported by the cited evidence. Do not invent deployment, customer, organisation, or legal facts that the repository cannot establish.
- Satisfy the action's acceptance criteria and existing project tests.
- Run ` + "`complyscan actions verify <action-id>`" + ` after the change. Report unresolved product or organisation questions to the developer instead of guessing.
- Keep ` + "`latest.json`" + ` as the machine-readable evidence source; use the concise Markdown report for human handoff.
`
}

func complyScanAgentSkill() string {
	return `---
name: complyscan-actions
description: Resolve evidence-backed ComplyScan developer actions in a codebase. Use when asked to inspect, explain, implement, or verify an action from a ComplyScan report, or when changing AI-related code that should be checked against configured compliance frameworks.
---

# ComplyScan actions

1. Run ` + "`complyscan actions list`" + ` to find active actions.
2. Run ` + "`complyscan actions show <action-id>`" + ` before editing. Use its evidence, framework mapping, recommended change, and acceptance criteria as the bounded task definition.
3. Inspect the cited code and nearby tests. Never infer deployment, customer, organisation, or legal facts from missing repository evidence.
4. Implement the smallest code or configuration change that satisfies the acceptance criteria. Preserve unrelated user changes.
5. Run relevant project tests, then ` + "`complyscan actions verify <action-id>`" + `.
6. If verification remains open, explain the remaining evidence gap. If the action is human-only, do not fabricate a code fix; hand the question back to the developer or compliance owner.

ComplyScan reports technical evidence, not a final legal-compliance decision. Treat ` + "`.complyscan/reports/latest.json`" + ` as the machine-readable source of truth.
When an MCP client is available, configure ` + "`complyscan agent serve`" + ` for read-only action, evidence, changed-code scan, and deterministic verification tools. These tools never enable a model or write a report.
`
}
