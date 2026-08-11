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
	return &backNavigableForm{form: huh.NewForm(huh.NewGroup(field))}
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
