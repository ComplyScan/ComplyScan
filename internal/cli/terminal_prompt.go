package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

const accessiblePromptEnvironment = "COMPLYSCAN_ACCESSIBLE"

const moreGuidanceChoiceValue = "\x00complyscan-more-guidance"
const requiredAnswerChoiceValue = "\x00complyscan-required-answer"
const backChoiceValue = "\x00complyscan-back"

var errPromptBack = errors.New("return to previous setup question")

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
		if session.backAvailable {
			visible = append(visible, terminalChoice{Value: backChoiceValue})
		}
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
		if selected == backChoiceValue {
			session.clearQuestionGuidance()
			return "", errPromptBack
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
		if session.backAvailable {
			visible = append(visible, terminalChoice{Value: backChoiceValue})
		}
		if session.hasQuestionGuidance() {
			visible = append(visible, terminalChoice{
				Label:    "ⓘ Further explanation — highlight to expand",
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
			if value == backChoiceValue {
				session.clearQuestionGuidance()
				return nil, errPromptBack
			}
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
	visibleChoices := visibleTerminalChoices(choices)
	options := make([]huh.Option[string], 0, len(visibleChoices))
	for _, choice := range visibleChoices {
		options = append(options, huh.NewOption(choice.Label, choice.Value))
	}
	selected := defaultValue
	required := defaultValue == requiredAnswerChoiceValue
	interaction := newRequiredSelectInteraction(required)
	guidance := terminalChoiceGuidance(choices)
	instructions := "Use ↑/↓ to move and Enter to confirm."
	allowBack := containsTerminalChoice(choices, backChoiceValue)
	if allowBack {
		instructions += " Press ← to return."
	}
	field := huh.NewSelect[string]().
		Title(label).
		Description(instructions).
		Options(options...).
		Value(&selected).
		Validate(func(value string) error {
			if value == moreGuidanceChoiceValue {
				return errors.New("read the explanation, then choose an answer in this menu")
			}
			if value == requiredAnswerChoiceValue || required && !interaction.interacted.Load() {
				return errors.New("choose an answer; no option is selected automatically")
			}
			return nil
		})
	keyHandler := promptKeyHandler(interaction.Handle)
	if strings.TrimSpace(guidance) != "" {
		guidanceIndex := terminalChoiceIndex(visibleChoices, moreGuidanceChoiceValue)
		initialCursor := terminalChoiceIndex(visibleChoices, defaultValue)
		if initialCursor < 0 {
			initialCursor = 0
		}
		tracker := newMultiSelectGuidanceTracker(len(visibleChoices), guidanceIndex, initialCursor, func(highlighted bool) {
			field.Description(terminalGuidanceDescription(instructions, highlighted, guidance))
		})
		keyHandler = combinePromptKeyHandlers(interaction.Handle, tracker.Handle)
	}
	form := huh.NewForm(huh.NewGroup(field)).WithInput(input).WithOutput(output)
	if err := runTerminalForm(form, input, output, allowBack, keyHandler); err != nil {
		return "", fmt.Errorf("select %s: %w", strings.ToLower(label), err)
	}
	return selected, nil
}

func runTerminalInput(input io.Reader, output io.Writer, label, defaultValue string, guidanceAvailable, allowBack bool) (string, error) {
	selected := ""
	instructions := "Type a replacement."
	if strings.TrimSpace(defaultValue) != "" {
		instructions = "Press Enter to accept the proposed answer, or type a replacement."
	}
	description := instructions
	if strings.TrimSpace(defaultValue) != "" {
		lines := []string{"Proposed answer"}
		for _, line := range wrapPromptText(defaultValue, promptContentWidth(output)-6) {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "", instructions)
		description = strings.Join(lines, "\n")
	}
	field := huh.NewInput().
		Title(label).
		Description(description).
		Prompt("› ").
		Placeholder("Type a replacement").
		Value(&selected).
		Validate(func(value string) error {
			if strings.TrimSpace(value) == "" && strings.TrimSpace(defaultValue) == "" {
				return errors.New("enter an answer before continuing")
			}
			return nil
		})
	keymap := huh.NewDefaultKeyMap()
	keymap.Input.Submit.SetHelp("enter", "accept")
	form := huh.NewForm(huh.NewGroup(field)).WithKeyMap(keymap).WithInput(input).WithOutput(output).WithWidth(promptContentWidth(output))
	if guidanceAvailable || allowBack {
		navigator, err := runDecoratedTerminalForm(form, input, output, guidanceAvailable, allowBack, tea.KeyEsc)
		if err != nil {
			return "", fmt.Errorf("enter %s: %w", strings.ToLower(label), err)
		}
		if navigator.details {
			return moreGuidanceChoiceValue, nil
		}
		if navigator.back {
			return "", errPromptBack
		}
	} else if err := form.Run(); err != nil {
		return "", fmt.Errorf("enter %s: %w", strings.ToLower(label), err)
	}
	selected = strings.TrimSpace(selected)
	if selected == "" {
		selected = strings.TrimSpace(defaultValue)
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
	visibleChoices := visibleTerminalChoices(choices)
	options := make([]huh.Option[string], 0, len(visibleChoices))
	for _, choice := range visibleChoices {
		_, selected := selectedDefaults[strings.ToLower(choice.Value)]
		options = append(options, huh.NewOption(choice.Label, choice.Value).Selected(selected))
	}
	exclusiveValues := make(map[string]struct{}, len(exclusive))
	for _, value := range exclusive {
		exclusiveValues[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	selected := append([]string(nil), defaults...)
	guidance := terminalChoiceGuidance(choices)
	instructions := "Use ↑/↓ to move, Space to tick or untick, and Enter to confirm."
	allowBack := containsTerminalChoice(choices, backChoiceValue)
	if allowBack {
		instructions += " Press ← to return."
	}
	field := huh.NewMultiSelect[string]().
		Title(label).
		Description(instructions).
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
	var keyHandler promptKeyHandler
	if strings.TrimSpace(guidance) != "" {
		guidanceIndex := terminalChoiceIndex(visibleChoices, moreGuidanceChoiceValue)
		tracker := newMultiSelectGuidanceTracker(len(visibleChoices), guidanceIndex, 0, func(highlighted bool) {
			field.Description(terminalGuidanceDescription(instructions, highlighted, guidance))
		})
		keyHandler = tracker.Handle
	}
	if err := runTerminalForm(form, input, output, allowBack, keyHandler); err != nil {
		return nil, fmt.Errorf("select %s: %w", strings.ToLower(label), err)
	}
	return withoutTerminalValue(selected, moreGuidanceChoiceValue), nil
}

type backNavigableForm struct {
	form           *huh.Form
	back           bool
	details        bool
	backEnabled    bool
	detailsEnabled bool
	backKey        tea.KeyType
	keyHandler     promptKeyHandler
}

type promptRelayoutMsg struct{}

type promptKeyHandler func(tea.KeyMsg) bool

type multiSelectGuidanceTracker struct {
	cursor        int
	total         int
	guidanceIndex int
	onHighlight   func(bool)
	filtering     bool
	disabled      bool
}

type requiredSelectInteraction struct {
	required   bool
	interacted atomic.Bool
	filtering  bool
}

func newRequiredSelectInteraction(required bool) *requiredSelectInteraction {
	return &requiredSelectInteraction{required: required}
}

func (tracker *requiredSelectInteraction) Handle(message tea.KeyMsg) bool {
	if !tracker.required {
		return false
	}
	value := message.String()
	if tracker.filtering {
		if value == "esc" || value == "enter" {
			tracker.filtering = false
			return false
		}
		if message.Type == tea.KeyRunes || value == "backspace" || value == "delete" {
			tracker.interacted.Store(true)
		}
		return false
	}
	if value == "/" {
		tracker.filtering = true
		return false
	}
	switch value {
	case "up", "k", "ctrl+p", "down", "j", "ctrl+n", "right", "l", "home", "g", "end", "G", "ctrl+u", "ctrl+d":
		tracker.interacted.Store(true)
	}
	return false
}

func newMultiSelectGuidanceTracker(total, guidanceIndex, initialCursor int, onHighlight func(bool)) *multiSelectGuidanceTracker {
	return &multiSelectGuidanceTracker{
		cursor:        min(max(initialCursor, 0), max(total-1, 0)),
		total:         total,
		guidanceIndex: guidanceIndex,
		onHighlight:   onHighlight,
	}
}

func (tracker *multiSelectGuidanceTracker) Handle(message tea.KeyMsg) bool {
	value := message.String()
	if tracker.filtering {
		if value == "esc" || value == "enter" {
			tracker.filtering = false
			// Huh filters its private option list and cursor. Once filtering has
			// occurred, avoid guessing that cursor position and showing guidance
			// for a row that is not actually highlighted.
			tracker.disabled = true
		}
		tracker.setHighlighted(false)
		return false
	}
	if value == "/" {
		tracker.filtering = true
		tracker.setHighlighted(false)
		return false
	}
	if tracker.disabled {
		tracker.setHighlighted(false)
		return false
	}
	switch value {
	case "up", "k", "ctrl+p":
		tracker.move(-1)
	case "down", "j", "ctrl+n":
		tracker.move(1)
	case "home", "g":
		tracker.cursor = 0
	case "end", "G":
		tracker.cursor = tracker.total - 1
	case "ctrl+u":
		tracker.cursor = max(tracker.cursor-5, 0)
	case "ctrl+d":
		tracker.cursor = min(tracker.cursor+5, tracker.total-1)
	case " ", "x":
		return tracker.cursor == tracker.guidanceIndex
	}
	tracker.setHighlighted(tracker.cursor == tracker.guidanceIndex)
	return false
}

func (tracker *multiSelectGuidanceTracker) move(offset int) {
	if tracker.total <= 0 {
		return
	}
	tracker.cursor = min(max(tracker.cursor+offset, 0), tracker.total-1)
}

func (tracker *multiSelectGuidanceTracker) setHighlighted(value bool) {
	if tracker.onHighlight != nil {
		tracker.onHighlight(value)
	}
}

func combinePromptKeyHandlers(handlers ...promptKeyHandler) promptKeyHandler {
	return func(message tea.KeyMsg) bool {
		for _, handler := range handlers {
			if handler != nil && handler(message) {
				return true
			}
		}
		return false
	}
}

func (form *backNavigableForm) Init() tea.Cmd {
	return form.form.Init()
}

func (form *backNavigableForm) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if keyMessage, ok := message.(tea.KeyMsg); ok {
		if form.backEnabled && keyMessage.Type == form.backKey {
			form.back = true
			return form, tea.Quit
		}
		if form.detailsEnabled && keyMessage.Type == tea.KeyRunes && string(keyMessage.Runes) == "?" {
			form.details = true
			return form, tea.Quit
		}
		if form.keyHandler != nil && form.keyHandler(keyMessage) {
			return form, nil
		}
	}
	updated, command := form.form.Update(message)
	if next, ok := updated.(*huh.Form); ok {
		form.form = next
	}
	if _, relayout := message.(promptRelayoutMsg); relayout {
		return form, command
	}
	if _, keyMessage := message.(tea.KeyMsg); keyMessage && form.keyHandler != nil {
		// A key handler can change a dynamic description before Huh processes the
		// same navigation key. Send one follow-up update so the viewport is laid
		// out again using the new description height. Without it, leaving an
		// expanded multi-select explanation can leave the options viewport at its
		// one-row expanded height until another navigation key is pressed.
		return form, tea.Batch(command, func() tea.Msg { return promptRelayoutMsg{} })
	}
	if _, keyMessage := message.(tea.KeyMsg); !keyMessage {
		return form, tea.Batch(command, func() tea.Msg { return promptRelayoutMsg{} })
	}
	return form, command
}

func (form *backNavigableForm) View() string {
	view := form.form.View()
	help := form.form.Help()
	bindings := form.form.KeyBinds()
	currentFooter := help.ShortHelpView(bindings)
	enhancedBindings := bindings
	if form.detailsEnabled {
		enhancedBindings = appendHelpBinding(enhancedBindings, key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "details")))
	}
	if form.backEnabled {
		back := key.NewBinding(key.WithKeys(form.backKey.String()), key.WithHelp(form.backKey.String(), "back"))
		if form.backKey == tea.KeyLeft {
			back = key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "back"))
			enhancedBindings = insertHelpBindingAfterDown(enhancedBindings, back)
		} else {
			enhancedBindings = appendHelpBinding(enhancedBindings, back)
		}
	}
	enhancedFooter := help.ShortHelpView(enhancedBindings)
	if enhancedFooter == "" {
		return view
	}
	return replaceOrAppendHelpFooter(view, currentFooter, enhancedFooter)
}

func replaceOrAppendHelpFooter(view, currentFooter, footerWithBack string) string {
	if footerWithBack == "" || strings.Contains(view, footerWithBack) {
		return view
	}
	if currentFooter != "" {
		if footer := strings.LastIndex(view, currentFooter); footer >= 0 {
			return view[:footer] + footerWithBack + view[footer+len(currentFooter):]
		}
	}
	trailingNewlines := len(view) - len(strings.TrimRight(view, "\n"))
	view = strings.TrimRight(view, "\n")
	if view != "" {
		view += "\n"
	}
	return view + footerWithBack + strings.Repeat("\n", trailingNewlines)
}

func withBackHelpBinding(bindings []key.Binding) []key.Binding {
	back := key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "back"))
	return insertHelpBindingAfterDown(bindings, back)
}

func insertHelpBindingAfterDown(bindings []key.Binding, addition key.Binding) []key.Binding {
	result := make([]key.Binding, 0, len(bindings)+1)
	inserted := false
	for _, binding := range bindings {
		result = append(result, binding)
		if !inserted && binding.Enabled() && binding.Help().Desc == "down" {
			result = append(result, addition)
			inserted = true
		}
	}
	if !inserted {
		result = append(result, addition)
	}
	return result
}

func appendHelpBinding(bindings []key.Binding, addition key.Binding) []key.Binding {
	result := append([]key.Binding(nil), bindings...)
	return append(result, addition)
}

func runTerminalForm(form *huh.Form, input io.Reader, output io.Writer, allowBack bool, keyHandlers ...promptKeyHandler) error {
	if !allowBack {
		if len(keyHandlers) == 0 || keyHandlers[0] == nil {
			return form.Run()
		}
	}
	var keyHandler promptKeyHandler
	if len(keyHandlers) > 0 {
		keyHandler = keyHandlers[0]
	}
	navigator, err := runDecoratedTerminalForm(form, input, output, false, allowBack, tea.KeyLeft, keyHandler)
	if err != nil {
		return err
	}
	if navigator.back {
		return errPromptBack
	}
	return nil
}

func runDecoratedTerminalForm(form *huh.Form, input io.Reader, output io.Writer, detailsEnabled, backEnabled bool, backKey tea.KeyType, keyHandlers ...promptKeyHandler) (*backNavigableForm, error) {
	// huh.Form.RunWithContext normally installs these commands before starting
	// Bubble Tea. The navigation wrapper starts the program directly so it can
	// distinguish setup navigation from Ctrl+C, and must preserve that lifecycle setup.
	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Quit
	var keyHandler promptKeyHandler
	if len(keyHandlers) > 0 {
		keyHandler = keyHandlers[0]
	}
	navigator := &backNavigableForm{form: form, detailsEnabled: detailsEnabled, backEnabled: backEnabled, backKey: backKey, keyHandler: keyHandler}
	result, err := tea.NewProgram(navigator, tea.WithInput(input), tea.WithOutput(output), tea.WithReportFocus()).Run()
	if err != nil {
		return nil, err
	}
	completed, ok := result.(*backNavigableForm)
	if !ok {
		return nil, errors.New("terminal form returned an unexpected navigation state")
	}
	if completed.form.State == huh.StateAborted {
		return nil, huh.ErrUserAborted
	}
	return completed, nil
}

func terminalChoiceGuidance(choices []terminalChoice) string {
	for _, choice := range choices {
		if choice.Value == moreGuidanceChoiceValue {
			return choice.Guidance
		}
	}
	return ""
}

func containsTerminalChoice(choices []terminalChoice, wanted string) bool {
	for _, choice := range choices {
		if choice.Value == wanted {
			return true
		}
	}
	return false
}

func terminalChoiceIndex(choices []terminalChoice, wanted string) int {
	for index, choice := range choices {
		if choice.Value == wanted {
			return index
		}
	}
	return -1
}

func visibleTerminalChoices(choices []terminalChoice) []terminalChoice {
	visible := make([]terminalChoice, 0, len(choices))
	for _, choice := range choices {
		if choice.Value != backChoiceValue && choice.Value != requiredAnswerChoiceValue {
			visible = append(visible, choice)
		}
	}
	return visible
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
