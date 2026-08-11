package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	llmActivityInterval  = 100 * time.Millisecond
	llmActivitySlowAfter = 30 * time.Second
	llmActivityTerminal  = terminalFile
)

var llmActivityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type llmActivityOptions struct {
	Waiting  string
	Success  string
	Failure  string
	SlowHint string
}

type llmActivity struct {
	output  io.Writer
	options llmActivityOptions
	started time.Time
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	active  bool
}

func startLLMActivity(output io.Writer, options llmActivityOptions) *llmActivity {
	activity := &llmActivity{
		output: output, options: options, started: time.Now(),
		stop: make(chan struct{}), done: make(chan struct{}),
		active: llmActivityAvailable(output),
	}
	if !activity.active {
		return activity
	}
	go activity.animate()
	return activity
}

func (activity *llmActivity) Finish(err error) {
	if activity == nil || !activity.active {
		return
	}
	activity.once.Do(func() {
		close(activity.stop)
		<-activity.done
		elapsed := formatElapsed(time.Since(activity.started))
		message := strings.TrimSpace(activity.options.Success)
		symbol := "✓"
		connector := "in"
		if err != nil {
			message = strings.TrimSpace(activity.options.Failure)
			symbol = "✗"
			connector = "after"
		}
		if message == "" {
			message = strings.TrimSuffix(strings.TrimSpace(activity.options.Waiting), "…")
		}
		_, _ = fmt.Fprintf(activity.output, "\r\x1b[2K%s %s %s %s\n", symbol, message, connector, elapsed)
	})
}

func (activity *llmActivity) animate() {
	defer close(activity.done)
	ticker := time.NewTicker(llmActivityInterval)
	defer ticker.Stop()
	frame := 0
	for {
		activity.render(frame)
		select {
		case <-ticker.C:
			frame = (frame + 1) % len(llmActivityFrames)
		case <-activity.stop:
			return
		}
	}
}

func (activity *llmActivity) render(frame int) {
	elapsed := time.Since(activity.started)
	hint := ""
	if elapsed >= llmActivitySlowAfter && strings.TrimSpace(activity.options.SlowHint) != "" {
		hint = " · " + strings.TrimSpace(activity.options.SlowHint)
	}
	_, _ = fmt.Fprintf(activity.output, "\r\x1b[2K%s %s · %s%s", llmActivityFrames[frame], strings.TrimSpace(activity.options.Waiting), formatElapsed(elapsed), hint)
}

func llmActivityAvailable(output io.Writer) bool {
	if !llmActivityTerminal(output) {
		return false
	}
	if strings.TrimSpace(os.Getenv(accessiblePromptEnvironment)) != "" || strings.EqualFold(strings.TrimSpace(os.Getenv("TERM")), "dumb") {
		return false
	}
	return strings.TrimSpace(os.Getenv("CI")) == ""
}

func localModelSlowHint(provider string) string {
	if provider == "ollama" {
		return "Local models may take several minutes"
	}
	return ""
}
