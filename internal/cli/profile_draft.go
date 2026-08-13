package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/profiledraft"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

type setupProfileDraft struct {
	Suggestions map[string]providers.ProfileSuggestion
}

func newSetupProfileDraft() setupProfileDraft {
	return setupProfileDraft{Suggestions: make(map[string]providers.ProfileSuggestion)}
}

func (draft setupProfileDraft) suggestion(field string) (providers.ProfileSuggestion, bool) {
	value, exists := draft.Suggestions[field]
	return value, exists
}

func (draft setupProfileDraft) values(field string, fallback []string) []string {
	if suggestion, exists := draft.suggestion(field); exists && len(suggestion.Values) > 0 {
		return append([]string(nil), suggestion.Values...)
	}
	return append([]string(nil), fallback...)
}

func (draft setupProfileDraft) first(field, fallback string) string {
	values := draft.values(field, nil)
	if len(values) == 0 {
		return fallback
	}
	return values[0]
}

func (draft setupProfileDraft) explain(prompt promptSession, field string) error {
	suggestion, exists := draft.suggestion(field)
	if !exists {
		return nil
	}
	confidence := strings.TrimSpace(suggestion.Confidence)
	if confidence == "" {
		confidence = "unknown"
	}
	confidence = strings.ToUpper(confidence[:1]) + confidence[1:]
	title := "AI suggestion · " + confidence + " confidence"
	if prompt.styleTitles {
		title = "\x1b[1mAI suggestion\x1b[0m · " + confidence + " confidence"
	}
	if _, err := fmt.Fprintf(prompt.output, "\n  %s\n", title); err != nil {
		return err
	}
	for _, value := range suggestion.Values {
		prefix := "    "
		if len(suggestion.Values) > 1 {
			prefix = "    • "
		}
		if err := writePromptParagraph(prompt.output, prefix, value); err != nil {
			return err
		}
	}
	count := len(suggestion.Evidence)
	unit := "repository references"
	if count == 1 {
		unit = "repository reference"
	}
	if _, err := fmt.Fprintf(prompt.output, "\n    Based on %d %s\n", count, unit); err != nil {
		return err
	}
	if rationale := strings.TrimSpace(suggestion.Rationale); rationale != "" {
		if err := writePromptParagraph(prompt.output, "    Rationale: ", rationale); err != nil {
			return err
		}
	}
	if count > 0 {
		if _, err := fmt.Fprintf(prompt.output, "    Evidence · %d %s\n", count, unit); err != nil {
			return err
		}
	}
	for _, evidence := range suggestion.Evidence {
		location := evidence.Path
		if evidence.Line > 0 {
			location += ":" + strconv.Itoa(evidence.Line)
		}
		if err := writePromptParagraph(prompt.output, "      • ", location+" — "+evidence.Summary); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(prompt.output, "    Advisory draft only; confirm the answer yourself."); err != nil {
		return err
	}
	return nil
}

func draftProfileForSetup(
	ctx context.Context,
	output io.Writer,
	target string,
	cfg config.Config,
	summary setupRepositorySummary,
	modelReady bool,
) setupProfileDraft {
	draft := setupProfileDraft{Suggestions: profiledraft.DeterministicSuggestions(summary.Inventory)}
	if cfg.AI.Provider == "none" || !modelReady {
		if len(draft.Suggestions) > 0 {
			_, _ = fmt.Fprintf(output, "\nPrepared %d repository-evident setup suggestion(s) without a model.\n", len(draft.Suggestions))
		}
		return draft
	}
	request := profiledraft.BuildRequest(target, summary.Discovery.Repository, summary.Languages, summary.Inventory)
	if len(request.Contexts) == 0 {
		_, _ = fmt.Fprintln(output, "\nNo bounded repository context was available for AI-assisted profile drafting; setup will ask for the facts directly.")
		return draft
	}
	_, _ = fmt.Fprintf(output, "\nDrafting setup answers with %s using %d bounded, secret-redacted repository context(s)...\n",
		reviewProviderLabel(cfg.AI.Provider), len(request.Contexts))
	draftStarted := time.Now()
	reviewer, timeout, _, _, _, err := configuredReviewer(cfg.AI)
	if err != nil {
		_, _ = fmt.Fprintf(output, "Warning: AI-assisted profile drafting was unavailable after %s: %v. Continuing with repository signals and human questions.\n", formatElapsed(time.Since(draftStarted)), err)
		return draft
	}
	draftContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	activity := startConfiguredLLMActivity(output, cfg.AI, "draft setup answers", "Setup-answer draft received", "Setup-answer draft failed")
	result, err := reviewer.DraftProfile(draftContext, request)
	activity.Finish(err)
	if err != nil {
		_, _ = fmt.Fprintf(output, "Warning: AI-assisted profile drafting was incomplete after %s: %v. Continuing with repository signals and human questions.\n", formatElapsed(time.Since(draftStarted)), err)
		return draft
	}
	draft.Suggestions = profiledraft.MergeSuggestions(draft.Suggestions, result.Suggestions)
	_, _ = fmt.Fprintf(output, "Prepared %d editable setup suggestion(s) in %s. Business, jurisdictional, and legal facts still require your answer.\n", len(draft.Suggestions), formatElapsed(time.Since(draftStarted)))
	return draft
}
