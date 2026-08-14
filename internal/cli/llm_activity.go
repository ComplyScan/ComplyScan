package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/providers"
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

// Dismiss clears an in-progress animation without presenting a retryable
// provider limit as a terminal request failure. The retry coordinator prints
// the cooldown and next attempt immediately afterwards.
func (activity *llmActivity) Dismiss() {
	if activity == nil || !activity.active {
		return
	}
	activity.once.Do(func() {
		close(activity.stop)
		<-activity.done
		_, _ = fmt.Fprint(activity.output, "\r\x1b[2K")
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

func startConfiguredLLMActivity(output io.Writer, settings config.AIConfig, action, success, failure string) *llmActivity {
	providerLabel := reviewProviderLabel(settings.Provider)
	if providers.Kind(settings.Provider) == providers.Compatible {
		providerLabel = remoteProviderName(settings)
	}
	return startLLMActivity(output, llmActivityOptions{
		Waiting:  fmt.Sprintf("Waiting for %s %q to %s", providerLabel, configuredReviewModel(settings), action),
		Success:  success,
		Failure:  failure,
		SlowHint: localModelSlowHint(settings.Provider),
	})
}

type technicalActivityReviewer struct {
	reviewer *providers.OllamaProvider
	output   io.Writer
	settings config.AIConfig
}

func (reviewer *technicalActivityReviewer) ReviewTechnical(ctx context.Context, request providers.TechnicalReviewRequest) (providers.TechnicalReviewResult, error) {
	if len(request.Candidates) == 0 {
		return reviewer.reviewer.ReviewTechnical(ctx, request)
	}
	action := "investigate technical evidence"
	if objective := strings.TrimSpace(request.Candidates[0].ObjectiveID); objective != "" {
		action += " for " + objective
	}
	activity := startConfiguredLLMActivity(reviewer.output, reviewer.settings, action, "Technical-evidence response received", "Technical-evidence request failed")
	result, err := reviewer.reviewer.ReviewTechnical(ctx, request)
	if rateLimit, retryable := providers.AsRemoteRateLimitError(err); retryable && !rateLimit.RequestTooLarge {
		activity.Dismiss()
	} else {
		activity.Finish(err)
	}
	return result, err
}

func (reviewer *technicalActivityReviewer) PlanTechnicalSearch(ctx context.Context, candidate providers.TechnicalCandidate) (providers.TechnicalSearchPlan, providers.Usage, error) {
	action := "plan a bounded evidence search"
	if objective := strings.TrimSpace(candidate.ObjectiveID); objective != "" {
		action += " for " + objective
	}
	activity := startConfiguredLLMActivity(reviewer.output, reviewer.settings, action, "Evidence-search plan received", "Evidence-search planning failed")
	result, usage, err := reviewer.reviewer.PlanTechnicalSearch(ctx, candidate)
	if rateLimit, retryable := providers.AsRemoteRateLimitError(err); retryable && !rateLimit.RequestTooLarge {
		activity.Dismiss()
	} else {
		activity.Finish(err)
	}
	return result, usage, err
}
