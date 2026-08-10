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

func runTerminalMultiSelect(input io.Reader, output io.Writer, label string, defaults, allowed []string) ([]string, error) {
	selectedDefaults := make(map[string]struct{}, len(defaults))
	for _, value := range defaults {
		selectedDefaults[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	options := make([]huh.Option[string], 0, len(allowed))
	for _, value := range allowed {
		_, selected := selectedDefaults[strings.ToLower(value)]
		options = append(options, huh.NewOption(value, value).Selected(selected))
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
					if strings.EqualFold(strings.TrimSpace(value), "unknown") {
						return errors.New("unknown must be selected alone")
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
