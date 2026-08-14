package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLLMActivityAnimatesAndFinishesCleanly(t *testing.T) {
	originalTerminal := llmActivityTerminal
	originalInterval := llmActivityInterval
	originalSlowAfter := llmActivitySlowAfter
	t.Cleanup(func() {
		llmActivityTerminal = originalTerminal
		llmActivityInterval = originalInterval
		llmActivitySlowAfter = originalSlowAfter
	})
	llmActivityTerminal = func(any) bool { return true }
	llmActivityInterval = time.Millisecond
	llmActivitySlowAfter = 0
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv(accessiblePromptEnvironment, "")

	var output bytes.Buffer
	activity := startLLMActivity(&output, llmActivityOptions{
		Waiting: "Waiting for Ollama qwen3.5:9b", Success: "Model response received",
		Failure: "Model request failed", SlowHint: "Local models may take several minutes",
	})
	time.Sleep(5 * time.Millisecond)
	activity.Finish(nil)

	value := output.String()
	for _, expected := range []string{"⠋", "Waiting for Ollama qwen3.5:9b", "Local models may take several minutes", "✓ Model response received in"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("activity output %q does not contain %q", value, expected)
		}
	}
}

func TestLLMActivityShowsFailure(t *testing.T) {
	originalTerminal := llmActivityTerminal
	t.Cleanup(func() { llmActivityTerminal = originalTerminal })
	llmActivityTerminal = func(any) bool { return true }
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv(accessiblePromptEnvironment, "")

	var output bytes.Buffer
	activity := startLLMActivity(&output, llmActivityOptions{Waiting: "Waiting for model", Failure: "Model request failed"})
	activity.Finish(errors.New("request failed"))
	if value := output.String(); !strings.Contains(value, "✗ Model request failed after") {
		t.Fatalf("failure output = %q", value)
	}
}

func TestLLMActivityDismissesRetryWithoutFailureMarker(t *testing.T) {
	originalTerminal := llmActivityTerminal
	t.Cleanup(func() { llmActivityTerminal = originalTerminal })
	llmActivityTerminal = func(any) bool { return true }
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv(accessiblePromptEnvironment, "")

	var output bytes.Buffer
	activity := startLLMActivity(&output, llmActivityOptions{Waiting: "Waiting for model", Failure: "Model request failed"})
	activity.Dismiss()
	if value := output.String(); strings.Contains(value, "✗") || strings.Contains(value, "Model request failed") || !strings.HasSuffix(value, "\r\x1b[2K") {
		t.Fatalf("dismissed retry output = %q", value)
	}
}

func TestLLMActivityIsSilentOutsideInteractiveTerminal(t *testing.T) {
	var output bytes.Buffer
	activity := startLLMActivity(&output, llmActivityOptions{Waiting: "Waiting for model"})
	activity.Finish(nil)
	if output.Len() != 0 {
		t.Fatalf("non-interactive activity output = %q", output.String())
	}
}

func TestLocalModelSlowHint(t *testing.T) {
	if got := localModelSlowHint("ollama"); got == "" {
		t.Fatal("Ollama activity should explain that local models may take several minutes")
	}
	if got := localModelSlowHint("anthropic"); got != "" {
		t.Fatalf("hosted slow hint = %q", got)
	}
}
