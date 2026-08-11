package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
)

const accessiblePromptEnvironment = "COMPLYSCAN_ACCESSIBLE"

type terminalChoice struct {
	Label string
	Value string
}

type setupStatusKind string

const (
	setupStatusReady   setupStatusKind = "ready"
	setupStatusReview  setupStatusKind = "review"
	setupStatusMissing setupStatusKind = "missing"
)

func (session promptSession) status(kind setupStatusKind, message string) error {
	marker := map[setupStatusKind]string{
		setupStatusReady:   "[READY]",
		setupStatusReview:  "[NEEDS REVIEW]",
		setupStatusMissing: "[NOT CONFIGURED]",
	}[kind]
	if session.styleTitles {
		marker = map[setupStatusKind]string{
			setupStatusReady:   "✓",
			setupStatusReview:  "!",
			setupStatusMissing: "—",
		}[kind]
	}
	if marker == "" {
		return fmt.Errorf("unsupported setup status %q", kind)
	}
	_, err := fmt.Fprintf(session.output, "  %s %s\n", marker, message)
	return err
}

func terminalPromptAvailable(input io.Reader, output io.Writer) bool {
	if strings.TrimSpace(os.Getenv(accessiblePromptEnvironment)) != "" || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	return terminalFile(input) && terminalFile(output)
}

func terminalFile(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok || file.Name() == os.DevNull {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (session promptSession) sectionTitle(title string, leadingBlank bool) error {
	return writeSectionTitle(session.output, title, session.styleTitles, leadingBlank)
}

func writeSectionTitle(output io.Writer, title string, bold, leadingBlank bool) error {
	prefix := ""
	if leadingBlank {
		prefix = "\n"
	}
	if bold {
		_, err := fmt.Fprintf(output, "%s\x1b[1m%s\x1b[0m\n", prefix, title)
		return err
	}
	_, err := fmt.Fprintf(output, "%s%s\n", prefix, title)
	return err
}

func runTerminalSelect(input io.Reader, output io.Writer, label, defaultValue string, choices []terminalChoice) (string, error) {
	options := make([]huh.Option[string], 0, len(choices))
	for _, choice := range choices {
		options = append(options, huh.NewOption(choice.Label, choice.Value))
	}
	selected := defaultValue
	field := huh.NewSelect[string]().
		Title(label).
		Description("Use ↑/↓ to move and Enter to confirm.").
		Options(options...).
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(field)).WithInput(input).WithOutput(output)
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("select %s: %w", strings.ToLower(label), err)
	}
	return selected, nil
}

func runTerminalConfirm(input io.Reader, output io.Writer, label string, defaultValue bool) (bool, error) {
	selected := defaultValue
	field := huh.NewConfirm().
		Title(label).
		Description("Use ←/→ to choose and Enter to confirm.").
		Affirmative("Yes").
		Negative("No").
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(field)).WithInput(input).WithOutput(output)
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("confirm %s: %w", strings.ToLower(label), err)
	}
	return selected, nil
}

func runTerminalMultiSelect(input io.Reader, output io.Writer, label string, defaults []string, choices []terminalChoice, exclusive []string) ([]string, error) {
	selectedDefaults := make(map[string]struct{}, len(defaults))
	for _, value := range defaults {
		selectedDefaults[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	options := make([]huh.Option[string], 0, len(choices))
	for _, choice := range choices {
		_, selected := selectedDefaults[strings.ToLower(choice.Value)]
		options = append(options, huh.NewOption(choice.Label, choice.Value).Selected(selected))
	}
	exclusiveValues := make(map[string]struct{}, len(exclusive))
	for _, value := range exclusive {
		exclusiveValues[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	selected := append([]string(nil), defaults...)
	field := huh.NewMultiSelect[string]().
		Title(label).
		Description("Use ↑/↓ to move, Space to tick or untick, and Enter to confirm.").
		Options(options...).
		Value(&selected).
		Validate(func(values []string) error {
			if len(values) == 0 {
				return errors.New("select at least one option")
			}
			if len(values) > 1 {
				for _, value := range values {
					if _, isExclusive := exclusiveValues[strings.ToLower(strings.TrimSpace(value))]; isExclusive {
						return fmt.Errorf("%s must be selected alone", value)
					}
				}
			}
			return nil
		})
	form := huh.NewForm(huh.NewGroup(field)).WithInput(input).WithOutput(output)
	if err := form.Run(); err != nil {
		return nil, fmt.Errorf("select %s: %w", strings.ToLower(label), err)
	}
	return selected, nil
}
