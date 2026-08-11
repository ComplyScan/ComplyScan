package cli

import (
	"strings"
	"testing"

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

func TestBackNavigableFormAdvertisesLeftArrowInFooter(t *testing.T) {
	selected := "one"
	navigator := newTestBackNavigableForm(&selected)
	if view := navigator.View(); !strings.Contains(view, "← back") {
		t.Fatalf("left-arrow back binding is absent from selector help: %q", view)
	}
}
