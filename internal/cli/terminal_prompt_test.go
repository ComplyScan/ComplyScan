package cli

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

func newTestBackNavigableForm(selected *string) *backNavigableForm {
	field := huh.NewSelect[string]().
		Options(huh.NewOption("One", "one"), huh.NewOption("Two", "two")).
		Value(selected)
	return &backNavigableForm{form: huh.NewForm(huh.NewGroup(field)), backEnabled: true, backKey: tea.KeyLeft}
}

func TestBackNavigableFormTreatsLeftArrowAsBack(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	_, command := navigator.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !navigator.back {
		t.Fatal("left arrow did not trigger back navigation")
	}
	if command == nil {
		t.Fatal("left arrow did not stop the active selector")
	}
}

func TestBackNavigableFormLeavesOtherArrowKeysToSelector(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	navigator.Update(tea.KeyMsg{Type: tea.KeyDown})
	if navigator.back {
		t.Fatal("down arrow unexpectedly triggered back navigation")
	}
}

func TestTextFormUsesEscapeForBackWithoutStealingLeftArrow(t *testing.T) {
	selected := ""
	field := huh.NewInput().Value(&selected)
	navigator := &backNavigableForm{form: huh.NewForm(huh.NewGroup(field)), backEnabled: true, backKey: tea.KeyEsc}
	navigator.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if navigator.back {
		t.Fatal("left arrow unexpectedly left an editable text field")
	}
	_, command := navigator.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !navigator.back || command == nil {
		t.Fatal("escape did not return from an editable text field")
	}
}

func TestTextFormCanOpenDetailsImmediately(t *testing.T) {
	selected := ""
	field := huh.NewInput().Value(&selected)
	navigator := &backNavigableForm{form: huh.NewForm(huh.NewGroup(field)), detailsEnabled: true}
	_, command := navigator.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !navigator.details || command == nil {
		t.Fatal("question mark did not open text-question details")
	}
}

func TestDynamicPromptUpdateSchedulesViewportRelayout(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	_, command := navigator.Update(struct{ changed string }{changed: "description"})
	if command == nil {
		t.Fatal("dynamic prompt update did not schedule a viewport relayout")
	}
	messages := collectTeaMessages(command)
	found := false
	for _, message := range messages {
		if _, ok := message.(promptRelayoutMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("dynamic prompt messages = %#v, want promptRelayoutMsg", messages)
	}
}

func TestPromptRelayoutDoesNotScheduleItselfAgain(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	_, command := navigator.Update(promptRelayoutMsg{})
	for _, message := range collectTeaMessages(command) {
		if _, ok := message.(promptRelayoutMsg); ok {
			t.Fatal("prompt relayout recursively scheduled another relayout")
		}
	}
}

func TestMultiSelectGuidanceExpandsOnHighlight(t *testing.T) {
	highlighted := false
	tracker := newMultiSelectGuidanceTracker(4, 3, &highlighted)
	for range 3 {
		if consumed := tracker.Handle(tea.KeyMsg{Type: tea.KeyDown}); consumed {
			t.Fatal("moving to guidance was unexpectedly consumed")
		}
	}
	if !highlighted {
		t.Fatal("guidance did not expand when highlighted")
	}
	if consumed := tracker.Handle(tea.KeyMsg{Type: tea.KeySpace}); !consumed {
		t.Fatal("Space selected the explanation as an answer")
	}
	if !highlighted {
		t.Fatal("guidance collapsed while its row remained highlighted")
	}
	tracker.Handle(tea.KeyMsg{Type: tea.KeyUp})
	if highlighted {
		t.Fatal("guidance remained expanded after leaving its row")
	}
}

func TestMultiSelectGuidanceTrackerMatchesClampedArrowNavigation(t *testing.T) {
	highlighted := false
	tracker := newMultiSelectGuidanceTracker(4, 3, &highlighted)
	tracker.Handle(tea.KeyMsg{Type: tea.KeyUp})
	if highlighted || tracker.cursor != 0 {
		t.Fatalf("top-boundary cursor=%d highlighted=%t", tracker.cursor, highlighted)
	}
	for range 3 {
		tracker.Handle(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !highlighted || tracker.cursor != 3 {
		t.Fatalf("guidance cursor=%d highlighted=%t", tracker.cursor, highlighted)
	}
	tracker.Handle(tea.KeyMsg{Type: tea.KeyDown})
	if !highlighted || tracker.cursor != 3 {
		t.Fatalf("bottom-boundary cursor=%d highlighted=%t", tracker.cursor, highlighted)
	}
}

func TestMultiSelectGuidanceDoesNotGuessCursorAfterFiltering(t *testing.T) {
	highlighted := false
	tracker := newMultiSelectGuidanceTracker(4, 3, &highlighted)
	tracker.Handle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	tracker.Handle(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	tracker.Handle(tea.KeyMsg{Type: tea.KeyEnter})
	for range 3 {
		tracker.Handle(tea.KeyMsg{Type: tea.KeyDown})
	}
	if highlighted {
		t.Fatal("guidance expanded from a guessed cursor after filtering")
	}
}

func TestLeavingExpandedGuidanceRestoresChoicesAfterOneKeypress(t *testing.T) {
	selected := moreGuidanceChoiceValue
	field := huh.NewSelect[string]().
		Title("Decision impact").
		DescriptionFunc(func() string {
			if selected == moreGuidanceChoiceValue {
				return "Further explanation:\nline one\nline two\nline three\nline four"
			}
			return "Choose the strongest effect."
		}, &selected).
		Options(
			huh.NewOption("Advisory", "advisory"),
			huh.NewOption("Low", "low"),
			huh.NewOption("Significant", "significant"),
			huh.NewOption("Autonomous", "autonomous"),
			huh.NewOption("Further explanation", moreGuidanceChoiceValue),
		).
		Value(&selected)
	navigator := &backNavigableForm{form: huh.NewForm(huh.NewGroup(field)).WithWidth(80).WithHeight(10)}
	drainTeaCommand(t, navigator, navigator.Init(), 50)
	_, resize := navigator.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	drainTeaCommand(t, navigator, resize, 50)
	_, command := navigator.Update(tea.KeyMsg{Type: tea.KeyUp})
	drainTeaCommand(t, navigator, command, 50)

	view := navigator.View()
	for _, choice := range []string{"Advisory", "Low", "Significant", "Autonomous"} {
		if !strings.Contains(view, choice) {
			t.Fatalf("collapsed guidance still hides %q after one keypress: %q", choice, view)
		}
	}
}

func TestTextFormFooterIncludesDetailsAndEscapeBack(t *testing.T) {
	selected := ""
	field := huh.NewInput().Value(&selected)
	keymap := huh.NewDefaultKeyMap()
	keymap.Input.Submit.SetHelp("enter", "accept")
	navigator := &backNavigableForm{
		form: huh.NewForm(huh.NewGroup(field)).WithKeyMap(keymap), detailsEnabled: true, backEnabled: true, backKey: tea.KeyEsc,
	}
	view := navigator.View()
	for _, expected := range []string{"enter", "accept", "?", "details", "esc", "back"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("text footer missing %q: %q", expected, view)
		}
	}
}

func TestBackNavigableFormCanExitAfterSubmission(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	navigator.form.SubmitCmd = tea.Quit
	navigator.form.CancelCmd = tea.Quit
	_, command := navigator.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("submitting a navigable form did not produce an exit command")
	}
}

func TestRunTerminalFormReturnsAfterEnter(t *testing.T) {
	selected := "one"
	field := huh.NewSelect[string]().
		Options(huh.NewOption("One", "one"), huh.NewOption("Two", "two")).
		Value(&selected)
	done := make(chan error, 1)
	go func() {
		done <- runTerminalForm(huh.NewForm(huh.NewGroup(field)), strings.NewReader("\r"), io.Discard, true)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("navigable form did not exit after Enter")
	}
}

func TestRequiredTerminalSelectRejectsUntouchedFirstChoice(t *testing.T) {
	done := make(chan struct {
		value string
		err   error
	}, 1)
	go func() {
		value, err := runTerminalSelect(strings.NewReader("\r\x1b[B\r"), io.Discard, "Lifecycle stage", requiredAnswerChoiceValue, []terminalChoice{
			{Label: "Development", Value: "development"},
			{Label: "Testing", Value: "testing"},
		})
		done <- struct {
			value string
			err   error
		}{value: value, err: err}
	}()
	select {
	case result := <-done:
		if result.err != nil || result.value != "testing" {
			t.Fatalf("value=%q err=%v", result.value, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("required selector did not recover after rejecting untouched submission")
	}
}

func TestRequiredChoicePlaceholderIsNotRendered(t *testing.T) {
	choices := []terminalChoice{
		{Value: requiredAnswerChoiceValue},
		{Label: "Development", Value: "development"},
		{Label: "Unknown", Value: "unknown"},
	}
	visible := visibleTerminalChoices(choices)
	if len(visible) != 2 || visible[0].Value != "development" || visible[1].Value != "unknown" {
		t.Fatalf("visible required choices = %#v", visible)
	}
}

func TestRunTerminalInputAcceptsProposedAnswer(t *testing.T) {
	done := make(chan struct {
		value string
		err   error
	}, 1)
	go func() {
		value, err := runTerminalInput(strings.NewReader("\r"), io.Discard, "Intended purpose", "Draft support replies.", true, true)
		done <- struct {
			value string
			err   error
		}{value: value, err: err}
	}()
	select {
	case result := <-done:
		if result.err != nil || result.value != "Draft support replies." {
			t.Fatalf("value=%q err=%v", result.value, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal text input did not accept the proposed answer")
	}
}

func TestRunTerminalInputReturnsOnEscape(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		_, err := runTerminalInput(strings.NewReader("\x1b"), io.Discard, "Intended purpose", "Draft support replies.", true, true)
		done <- err
	}()
	select {
	case err := <-done:
		if err != errPromptBack {
			t.Fatalf("escape error = %v, want errPromptBack", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal text input did not return after Escape")
	}
}

func TestBackNavigableFormAdvertisesLeftArrowInFooter(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	help := navigator.form.Help()
	wantFooter := help.ShortHelpView(withBackHelpBinding(navigator.form.KeyBinds()))
	if view := navigator.View(); !strings.Contains(view, wantFooter) || !strings.Contains(view, "←") || !strings.Contains(view, "back") {
		t.Fatalf("left-arrow back binding is absent from selector help: %q", view)
	}
}

func TestBackHelpBindingAppearsAfterDownNavigation(t *testing.T) {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up")),
		key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
	}
	withBack := withBackHelpBinding(bindings)
	if len(withBack) != 4 || withBack[2].Help().Key != "←" || withBack[2].Help().Desc != "back" {
		t.Fatalf("bindings = %#v", withBack)
	}
}

func TestBackHelpFooterRemainsVisibleWhenValidationHidesOriginalHelp(t *testing.T) {
	styledCurrent := "\x1b[2m↑ up • enter submit\x1b[0m"
	styledBack := "\x1b[2m↑ up • ← back • enter submit\x1b[0m"
	withHelp := "choices\n" + styledCurrent + "\n"
	if got := replaceOrAppendHelpFooter(withHelp, styledCurrent, styledBack); got != "choices\n"+styledBack+"\n" {
		t.Fatalf("replaced footer = %q", got)
	}
	validationView := "choices\n\x1b[31mselect at least one option\x1b[0m\n"
	want := validationView[:len(validationView)-1] + "\n" + styledBack + "\n"
	if got := replaceOrAppendHelpFooter(validationView, styledCurrent, styledBack); got != want {
		t.Fatalf("validation footer = %q, want %q", got, want)
	}
	if got := replaceOrAppendHelpFooter(validationView, "", styledBack); got != want {
		t.Fatalf("missing native footer = %q, want %q", got, want)
	}
}

func TestBackHelpFooterIsNotDuplicated(t *testing.T) {
	styledBack := "\x1b[2m↑ up • ← back • enter submit\x1b[0m"
	view := "choices\n" + styledBack + "\n"
	if got := replaceOrAppendHelpFooter(view, "", styledBack); got != view {
		t.Fatalf("duplicated footer = %q, want %q", got, view)
	}
}

func collectTeaMessages(command tea.Cmd) []tea.Msg {
	if command == nil {
		return nil
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		messages := make([]tea.Msg, 0, len(batch))
		for _, nested := range batch {
			messages = append(messages, collectTeaMessages(nested)...)
		}
		return messages
	}
	return []tea.Msg{message}
}

func drainTeaCommand(t *testing.T, navigator *backNavigableForm, command tea.Cmd, remaining int) {
	t.Helper()
	if command == nil {
		return
	}
	if remaining <= 0 {
		t.Fatal("terminal prompt command queue did not settle")
	}
	for _, message := range collectTeaMessages(command) {
		_, next := navigator.Update(message)
		drainTeaCommand(t, navigator, next, remaining-1)
	}
}
