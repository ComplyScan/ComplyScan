package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
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
	draft := deterministicSetupProfileDraft(summary)
	if cfg.AI.Provider == "none" || !modelReady {
		if len(draft.Suggestions) > 0 {
			_, _ = fmt.Fprintf(output, "\nPrepared %d repository-evident setup suggestion(s) without a model.\n", len(draft.Suggestions))
		}
		return draft
	}
	request := buildProfileDraftRequest(target, summary)
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

func deterministicSetupProfileDraft(summary setupRepositorySummary) setupProfileDraft {
	draft := newSetupProfileDraft()
	if summary.Inventory.Summary.RuntimeSignals == 0 {
		return draft
	}
	evidence := make([]providers.ProfileEvidence, 0, 3)
	for _, component := range summary.Inventory.Components {
		for _, location := range component.Locations {
			if location.Scope != inventory.ScopeRuntime {
				continue
			}
			evidence = append(evidence, providers.ProfileEvidence{
				Path: location.Path, Line: location.Line,
				Summary: fmt.Sprintf("%s runtime evidence: %s", component.Name, location.Evidence),
			})
			if len(evidence) == 3 {
				break
			}
		}
		if len(evidence) == 3 {
			break
		}
	}
	if len(evidence) > 0 {
		draft.Suggestions["ai-activities"] = providers.ProfileSuggestion{
			Field: "ai-activities", Values: []string{"inference"}, Confidence: "medium",
			Rationale: "Runtime AI-provider or framework usage supports an inference candidate, but does not establish the complete product workflow.",
			Evidence:  evidence,
		}
	}
	return draft
}

func buildProfileDraftRequest(target string, summary setupRepositorySummary) providers.ProfileDraftRequest {
	name := filepath.Base(filepath.Clean(target))
	components := make([]string, 0, len(summary.Inventory.Components))
	anchorLines := make(map[string][]int)
	for _, component := range summary.Inventory.Components {
		components = append(components, component.Name)
		for _, location := range component.Locations {
			anchorLines[location.Path] = append(anchorLines[location.Path], location.Line)
		}
	}
	type rankedFile struct {
		file  discovery.File
		score int
	}
	ranked := make([]rankedFile, 0, len(summary.Discovery.Repository.Files))
	for _, file := range summary.Discovery.Repository.Files {
		score := profileDraftFileScore(file, anchorLines[file.Path])
		if score > 0 {
			ranked = append(ranked, rankedFile{file: file, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].file.Path < ranked[j].file.Path
	})
	contexts := make([]providers.ProfileSourceContext, 0, len(ranked))
	for _, candidate := range ranked {
		contexts = append(contexts, providers.ProfileSourceContext{
			Path:   candidate.file.Path,
			Kind:   string(candidate.file.Kind),
			Source: profileDraftSource(candidate.file, anchorLines[candidate.file.Path]),
		})
		if len(contexts) == 24 {
			break
		}
	}
	return providers.ProfileDraftRequest{
		RepositoryName: name,
		Languages:      append([]string(nil), summary.Languages...),
		Components:     components,
		Contexts:       contexts,
	}
}

func profileDraftFileScore(file discovery.File, anchors []int) int {
	score := 0
	switch file.Kind {
	case discovery.KindReadme, discovery.KindModelCard:
		score = 100
	case discovery.KindManifest:
		score = 90
	case discovery.KindDockerfile, discovery.KindGitHubAction, discovery.KindCI, discovery.KindTerraform, discovery.KindEnvTemplate, discovery.KindConfig:
		score = 80
	case discovery.KindPrivacy, discovery.KindRisk, discovery.KindAIGovernance:
		score = 70
	case discovery.KindSource:
		if len(anchors) > 0 {
			score = 95
		}
	case discovery.KindDocumentation:
		score = 40
	}
	return score
}

func profileDraftSource(file discovery.File, anchors []int) string {
	lines := strings.Split(strings.ReplaceAll(string(file.Content), "\r\n", "\n"), "\n")
	selected := make(map[int]struct{})
	if len(anchors) == 0 {
		limit := len(lines)
		if limit > 100 {
			limit = 100
		}
		for index := 0; index < limit; index++ {
			selected[index] = struct{}{}
		}
	} else {
		for _, line := range anchors {
			start := line - 12
			if start < 0 {
				start = 0
			}
			end := line + 11
			if end > len(lines) {
				end = len(lines)
			}
			for index := start; index < end; index++ {
				selected[index] = struct{}{}
			}
		}
	}
	indexes := make([]int, 0, len(selected))
	for index := range selected {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	var result strings.Builder
	for _, index := range indexes {
		fmt.Fprintf(&result, "%d: %s\n", index+1, lines[index])
	}
	return result.String()
}
