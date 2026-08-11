package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWrapPromptTextKeepsQuestionCopyReadable(t *testing.T) {
	lines := wrapPromptText("Describe what the system is designed to do, who uses it, and what outcome it produces.", 32)
	if len(lines) < 2 {
		t.Fatalf("wrapped lines = %#v", lines)
	}
	for _, line := range lines {
		if len([]rune(line)) > 32 {
			t.Fatalf("line exceeds requested width: %q", line)
		}
	}
}

func TestWritePromptParagraphPreservesIndentation(t *testing.T) {
	var output bytes.Buffer
	if err := writePromptParagraph(&output, "  ", strings.Repeat("word ", 30)); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("unindented line = %q", line)
		}
	}
}
