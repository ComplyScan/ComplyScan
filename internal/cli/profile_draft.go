package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/profile"
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

func (draft setupProfileDraft) explain(output io.Writer, field string) error {
	suggestion, exists := draft.suggestion(field)
	if !exists {
		return nil
	}
	if _, err := fmt.Fprintf(output, "\n  Suggested from repository evidence (%s confidence): %s\n", suggestion.Confidence, strings.Join(suggestion.Values, ", ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "  Why: %s\n", suggestion.Rationale); err != nil {
		return err
	}
	for _, evidence := range suggestion.Evidence {
		location := evidence.Path
		if evidence.Line > 0 {
			location += ":" + strconv.Itoa(evidence.Line)
		}
		if _, err := fmt.Fprintf(output, "  Evidence: %s — %s\n", location, evidence.Summary); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, "  This is an editable draft, not a confirmed fact. Review it before continuing.")
	return err
}

func (draft setupProfileDraft) lifecycle(fallback profile.LifecycleStage) profile.LifecycleStage {
	return profile.LifecycleStage(draft.first("lifecycle-stage", string(fallback)))
}

func (draft setupProfileDraft) decisionImpact(fallback profile.DecisionImpact) profile.DecisionImpact {
	return profile.DecisionImpact(draft.first("decision-impact", string(fallback)))
}

func (draft setupProfileDraft) humanOversight(fallback profile.HumanOversight) profile.HumanOversight {
	return profile.HumanOversight(draft.first("human-oversight", string(fallback)))
}

func (draft setupProfileDraft) useCaseDomains(fallback []profile.UseCaseDomain) []profile.UseCaseDomain {
	values := draft.values("use-case-domains", stringUseCaseDomains(fallback))
	result := make([]profile.UseCaseDomain, len(values))
	for index, value := range values {
		result[index] = profile.UseCaseDomain(value)
	}
	return result
}

func (draft setupProfileDraft) aiActivities(fallback []profile.AIActivity) []profile.AIActivity {
	values := draft.values("ai-activities", stringAIActivities(fallback))
	result := make([]profile.AIActivity, len(values))
	for index, value := range values {
		result[index] = profile.AIActivity(value)
	}
	return result
}

func (draft setupProfileDraft) deploymentModels(fallback []profile.DeploymentModel) []profile.DeploymentModel {
	values := draft.values("deployment-models", stringDeploymentModels(fallback))
	result := make([]profile.DeploymentModel, len(values))
	for index, value := range values {
		result[index] = profile.DeploymentModel(value)
	}
	return result
}

func (draft setupProfileDraft) triState(field string, fallback profile.TriState) profile.TriState {
	return profile.TriState(draft.first(field, string(fallback)))
}

func stringUseCaseDomains(values []profile.UseCaseDomain) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func stringAIActivities(values []profile.AIActivity) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
}

func stringDeploymentModels(values []profile.DeploymentModel) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = string(value)
	}
	return result
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
	reviewer, timeout, _, _, _, err := configuredReviewer(cfg.AI)
	if err != nil {
		_, _ = fmt.Fprintf(output, "Warning: AI-assisted profile drafting was unavailable: %v. Continuing with repository signals and human questions.\n", err)
		return draft
	}
	draftContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := reviewer.DraftProfile(draftContext, request)
	if err != nil {
		_, _ = fmt.Fprintf(output, "Warning: AI-assisted profile drafting was incomplete: %v. Continuing with repository signals and human questions.\n", err)
		return draft
	}
	for _, suggestion := range result.Suggestions {
		draft.Suggestions[suggestion.Field] = suggestion
	}
	_, _ = fmt.Fprintf(output, "Prepared %d editable setup suggestion(s). Business, jurisdictional, and legal facts still require your answer.\n", len(draft.Suggestions))
	return draft
}
