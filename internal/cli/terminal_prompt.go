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

const moreGuidanceChoiceValue = "\x00complyscan-more-guidance"
const requiredAnswerChoiceValue = "\x00complyscan-required-answer"

type terminalChoice struct {
	Label    string
	Value    string
	Guidance string
}

func (session promptSession) chooseOne(label, defaultValue string, choices []terminalChoice) (string, error) {
	if session.selectOne == nil {
		return "", fmt.Errorf("select %s: interactive selector is unavailable", strings.ToLower(label))
	}
	for {
		visible := append([]terminalChoice(nil), choices...)
		if session.hasQuestionGuidance() {
			visible = append(visible, terminalChoice{
				Label:    "ⓘ Further explanation — highlight to expand",
				Value:    moreGuidanceChoiceValue,
				Guidance: strings.Join(session.guidance.details, "\n"),
			})
		}
		selected, err := session.selectOne(label, defaultValue, visible)
		if err != nil {
			return "", err
		}
		if selected == moreGuidanceChoiceValue {
			if err := session.showQuestionGuidance(); err != nil {
				return "", err
			}
			continue
		}
		session.clearQuestionGuidance()
		return selected, nil
	}
}

func (session promptSession) chooseMany(label string, defaults []string, choices []terminalChoice, exclusive []string) ([]string, error) {
	if session.selectMany == nil {
		return nil, fmt.Errorf("select %s: interactive multi-selector is unavailable", strings.ToLower(label))
	}
	for {
		visible := append([]terminalChoice(nil), choices...)
		if session.hasQuestionGuidance() {
			visible = append(visible, terminalChoice{
				Label:    "ⓘ Further explanation — press Space to expand",
				Value:    moreGuidanceChoiceValue,
				Guidance: strings.Join(session.guidance.details, "\n"),
			})
		}
		selected, err := session.selectMany(label, defaults, visible, exclusive)
		if err != nil {
			return nil, err
		}
		showGuidance := false
		for _, value := range selected {
			if value == moreGuidanceChoiceValue {
				showGuidance = true
				break
			}
		}
		if showGuidance {
			if err := session.showQuestionGuidance(); err != nil {
				return nil, err
			}
			continue
		}
		session.clearQuestionGuidance()
		return selected, nil
	}
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

func (session promptSession) startQuestionGroup(title string, total int, leadingBlank bool) (promptSession, error) {
	if total < 1 {
		return session, fmt.Errorf("question group %q must contain at least one question", title)
	}
	session.questions = &questionProgress{total: total}
	unit := "questions"
	if total == 1 {
		unit = "question"
	}
	return session, session.sectionTitle(fmt.Sprintf("%s — %d %s", title, total, unit), leadingBlank)
}

func (session promptSession) nextQuestionLabel(label string) string {
	if session.questions == nil || session.questions.total < 1 {
		return label
	}
	session.questions.current++
	prefix := ""
	if session.step != nil && session.step.current > 0 && session.step.total > 0 {
		prefix = fmt.Sprintf("Step %d of %d · ", session.step.current, session.step.total)
	}
	return fmt.Sprintf("%sQuestion %d of %d — %s", prefix, session.questions.current, session.questions.total, label)
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
	guidance := terminalChoiceGuidance(choices)
	field := huh.NewSelect[string]().
		Title(label).
		DescriptionFunc(func() string {
			return terminalGuidanceDescription("Use ↑/↓ to move and Enter to confirm.", selected == moreGuidanceChoiceValue, guidance)
		}, &selected).
		Options(options...).
		Value(&selected).
		Validate(func(value string) error {
			if value == moreGuidanceChoiceValue {
				return errors.New("read the explanation, then choose an answer in this menu")
			}
			if value == requiredAnswerChoiceValue {
				return errors.New("choose an answer; no option is selected automatically")
			}
			return nil
		})
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
	guidance := terminalChoiceGuidance(choices)
	field := huh.NewMultiSelect[string]().
		Title(label).
		DescriptionFunc(func() string {
			return terminalGuidanceDescription("Use ↑/↓ to move, Space to tick or untick, and Enter to confirm.", containsTerminalValue(selected, moreGuidanceChoiceValue), guidance)
		}, &selected).
		Options(options...).
		Value(&selected).
		Validate(func(values []string) error {
			answers := withoutTerminalValue(values, moreGuidanceChoiceValue)
			if len(answers) == 0 {
				return errors.New("select at least one option")
			}
			if len(answers) > 1 {
				for _, value := range answers {
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
	return withoutTerminalValue(selected, moreGuidanceChoiceValue), nil
}

func terminalChoiceGuidance(choices []terminalChoice) string {
	for _, choice := range choices {
		if choice.Value == moreGuidanceChoiceValue {
			return choice.Guidance
		}
	}
	return ""
}

func terminalGuidanceDescription(instructions string, expanded bool, guidance string) string {
	if !expanded || strings.TrimSpace(guidance) == "" {
		return instructions
	}
	lines := []string{instructions, "", "Further explanation:"}
	for _, line := range strings.Split(guidance, "\n") {
		lines = append(lines, "• "+line)
	}
	lines = append(lines, "", "Move to an answer in this menu when you are ready to continue.")
	return strings.Join(lines, "\n")
}

func containsTerminalValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func withoutTerminalValue(values []string, omitted string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != omitted {
			filtered = append(filtered, value)
		}
	}
	return filtered
}
