package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	term "github.com/charmbracelet/x/term"
)

const (
	defaultPromptWidth = 96
	maximumPromptWidth = 100
	minimumPromptWidth = 48
)

func promptContentWidth(output io.Writer) int {
	width := defaultPromptWidth
	if file, ok := output.(*os.File); ok {
		if terminalWidth, _, err := term.GetSize(file.Fd()); err == nil && terminalWidth > 0 {
			width = terminalWidth - 4
		}
	}
	if width > maximumPromptWidth {
		width = maximumPromptWidth
	}
	if width < minimumPromptWidth {
		width = minimumPromptWidth
	}
	return width
}

func writePromptParagraph(output io.Writer, prefix, value string) error {
	width := promptContentWidth(output) - utf8.RuneCountInString(prefix)
	for _, line := range wrapPromptText(value, width) {
		if _, err := fmt.Fprintln(output, prefix+line); err != nil {
			return err
		}
	}
	return nil
}

func wrapPromptText(value string, width int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{""}
	}
	if width < 1 {
		return []string{value}
	}
	result := make([]string, 0, 2)
	for _, paragraph := range strings.Split(value, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			result = append(result, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width {
				line += " " + word
				continue
			}
			result = append(result, line)
			line = word
		}
		result = append(result, line)
	}
	return result
}
