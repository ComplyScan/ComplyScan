package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/usemapping"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

const (
	maxDeveloperActions   = 3
	maxDeveloperEvidence  = 8
	maxDeveloperQuestions = 5
	maxDeveloperUseChecks = 8
	maxDeveloperUseFacts  = 8
)

type developerAction struct {
	priority string
	issue    string
	why      string
	next     string
	evidence string
	control  bool
}

type developerEvidence struct {
	title      string
	assessment string
	followUp   string
	evidence   string
	verdict    providers.RepositoryTechnicalVerdict
}

type developerReportView struct {
	outcome            string
	importantRisks     int
	controlsToReview   int
	actions            []developerAction
	actionTotal        int
	evidence           []developerEvidence
	evidenceTotal      int
	questions          []string
	questionTotal      int
	integrationSignals []string
	sourceFilesSeen    int
	filesIndexed       int
	unsupportedFiles   int
	repositoryAnalysis string
	verificationPassed int
	verificationFailed int
	implemented        int
	partial            int
	notImplemented     int
	cannotDetermine    int
	referenceDetails   []string
	evidenceBundle     string
	hasPerUseMappings  bool
}

func writeDeveloperReportMarkdown(writer io.Writer, value Report, evidenceBundle string) error {
	view := buildDeveloperReportView(value, evidenceBundle)
	if _, err := fmt.Fprintln(writer, "\n> A developer-focused review of code found in this repository. ComplyScan cannot decide on its own whether your product complies with a law."); err != nil {
		return err
	}
	if err := writeDeveloperOutcomeMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperActionsMarkdown(writer, view); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n## 2. What ComplyScan found"); err != nil {
		return err
	}
	if err := writeDeveloperAIUseMappingsMarkdown(writer, value, view.evidenceBundle); err != nil {
		return err
	}
	if err := writeDeveloperEvidenceMarkdown(writer, view); err != nil {
		return err
	}
	if err := writeDeveloperAIUsesMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperQuestionsMarkdown(writer, view); err != nil {
		return err
	}
	if err := writeDeveloperReferenceDetailsMarkdown(writer, view); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "\n## How this scan was performed\n\n- Source files checked: **%d of %d**", view.filesIndexed, view.sourceFilesSeen); err != nil {
		return err
	}
	if view.unsupportedFiles > 0 {
		if _, err := fmt.Fprintf(writer, " (%d potentially relevant unsupported)", view.unsupportedFiles); err != nil {
			return err
		}
	}
	attentionParts := len(value.Warnings)
	if developerRepositoryAnalysisIncomplete(value) && attentionParts == 0 {
		attentionParts = 1
	}
	if _, err := fmt.Fprintf(writer, "\n- Repository AI review: **%s**\n- Scan execution warnings: **%d**\n- Scan ID: %s\n- Full technical results: %s\n",
		markdownText(view.repositoryAnalysis), attentionParts, inlineCode(value.Scan.ID), inlineCode(view.evidenceBundle)); err != nil {
		return err
	}
	if value.RepositoryAnalysis != nil {
		analysis := value.RepositoryAnalysis
		if analysis.Provider != providers.None || strings.TrimSpace(analysis.Model) != "" {
			if _, err := fmt.Fprintf(writer, "- Repository review provider/model: **%s / %s**\n",
				markdownText(providerDisplayName(analysis.Provider)), inlineCode(analysis.Model)); err != nil {
				return err
			}
		}
		accounting := fmt.Sprintf("%d provider request(s) in this run", analysis.Coverage.ProviderRequests)
		if analysis.CacheHit {
			accounting = fmt.Sprintf("cached review used %d provider request(s); current run sent no repository source", analysis.Coverage.ProviderRequests)
		}
		if analysis.Coverage.SourceBatchesTotal > 0 {
			batchCoverage := fmt.Sprintf("%d distinct source batch(es) started, %d of %d validated",
				analysis.Coverage.SourceBatchesStarted, analysis.Coverage.SourceBatchesCompleted, analysis.Coverage.SourceBatchesTotal)
			if analysis.CacheHit {
				accounting = fmt.Sprintf("cached review used %d provider request(s); %s; current run sent no repository source",
					analysis.Coverage.ProviderRequests, batchCoverage)
			} else {
				accounting += "; " + batchCoverage
			}
		}
		if _, err := fmt.Fprintf(writer, "- Repository review accounting: **%s**\n", markdownText(accounting)); err != nil {
			return err
		}
		if note := repositoryCurrentRunCompatibilityNote(*analysis); note != "" {
			if _, err := fmt.Fprintf(writer, "- Current-run compatibility accounting (source-free): **%s**\n", markdownText(note)); err != nil {
				return err
			}
		}
	}
	if view.verificationPassed+view.verificationFailed > 0 {
		if _, err := fmt.Fprintf(writer, "- Isolated execution checks: **%d passed, %d failed**\n", view.verificationPassed, view.verificationFailed); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\n---\n\nCode-level verdicts describe the repository evidence ComplyScan reviewed. They do not decide whether a legal requirement applies or whether the implementation is enabled and effective in production.")
	return err
}

func buildDeveloperReportView(value Report, evidenceBundle string) developerReportView {
	counts := markdownCounts(value)
	view := developerReportView{
		sourceFilesSeen: counts.SourceFilesSeen, filesIndexed: counts.FilesIndexed,
		unsupportedFiles: counts.UnsupportedFiles, repositoryAnalysis: developerRepositoryAnalysisLabel(value),
		evidenceBundle: evidenceBundle, hasPerUseMappings: value.AIUseMappings != nil,
	}
	if value.RepositoryAnalysis != nil {
		if view.sourceFilesSeen == 0 {
			view.sourceFilesSeen = value.RepositoryAnalysis.Coverage.RepositoryFiles
		}
		if view.filesIndexed == 0 {
			view.filesIndexed = value.RepositoryAnalysis.Coverage.RepositoryFiles
		}
	}
	view.integrationSignals = developerIntegrationSignals(value)
	view.referenceDetails = developerReferenceDetails(value)
	view.implemented, view.partial, view.notImplemented, view.cannotDetermine = developerTechnicalVerdictCounts(value)
	objectiveTitles := developerObjectiveTitles(value)
	actionKeys := make(map[string]struct{})
	addAction := func(key string, action developerAction) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			key = strings.ToLower(action.issue + "\x00" + action.evidence)
		}
		if _, exists := actionKeys[key]; exists {
			return
		}
		actionKeys[key] = struct{}{}
		view.actions = append(view.actions, action)
		if action.control {
			view.controlsToReview++
		}
	}
	if value.AIUseInventory != nil {
		inventory := value.AIUseInventory
		for _, observed := range inventory.Retired {
			if observed.Observation == aiuse.ObservationNotReviewed {
				continue
			}
			addAction("ai-uses/retired/"+observed.Use.ID, developerAction{
				priority: "Review", issue: "Current signals match retired AI use: " + observed.Use.Name,
				why:      "A retired register entry still matches repository evidence in this scan.",
				next:     "Check whether the use was reactivated or whether its saved repository paths need updating.",
				evidence: strings.Join(observed.Use.Paths, ", "),
			})
		}
	}
	coveredSystemObjectives := make(map[string]struct{})
	coveredRepositoryObservations := make(map[string]struct{})
	if value.AIUseMappings != nil {
		for _, use := range value.AIUseMappings.Uses {
			for _, frameworkResult := range use.Frameworks {
				for _, context := range frameworkResult.Contexts {
					if developerUseContextNeedsAssociation(context) {
						switch context.Association.Status {
						case usemapping.AssociationNone:
							addAction("ai-use-context/"+use.UseID, developerAction{
								priority: "Review", issue: "AI use has no configured system context: " + use.UseName,
								why:  "Code evidence can be scoped to this use, but ComplyScan cannot screen the context-dependent requirements without a linked system profile.",
								next: "Associate this use with the relevant configured system in the AI-use register.", evidence: aiuse.DefaultPath,
							})
						case usemapping.AssociationMissing:
							addAction("ai-use-context/"+use.UseID+"/"+context.Association.SystemID, developerAction{
								priority: "Review", issue: "AI use references a missing configured system: " + use.UseName,
								why:  "The saved system ID " + context.Association.SystemID + " is not present in the active configuration, so context-dependent applicability cannot be screened reliably.",
								next: "Correct the system association or restore the configured system before relying on this mapping.", evidence: aiuse.DefaultPath,
							})
						}
					}
					for _, objective := range context.Objectives {
						if context.Association.Status == usemapping.AssociationConfigured {
							coveredSystemObjectives[context.Association.SystemID+"\x00"+objective.ObjectiveID] = struct{}{}
						}
						if action, include := developerUseObjectiveAction(use, context.Association, objective); include {
							addAction("ai-use-objective/"+use.UseID+"/"+frameworkResult.ID+"/"+context.Association.SystemID+"/"+objective.ObjectiveID, action)
						}
						if objective.AIReview != nil {
							aiUseID := ""
							if objective.AIReview.Attribution == usemapping.ReviewAttributionExplicitUse {
								aiUseID = use.UseID
							}
							coveredRepositoryObservations[aiUseID+"\x00"+objective.AIReview.SystemID+"\x00"+objective.AIReview.RepositoryObjectiveID] = struct{}{}
						}
					}
				}
			}
		}
	}

	for _, finding := range value.Findings {
		if rules.SeverityRank(finding.Severity) < rules.SeverityRank(rules.SeverityMedium) {
			continue
		}
		view.importantRisks++
		next := finding.Remediation
		if next == "" {
			next = "Review the code path and decide whether a change is required."
		}
		addAction("finding/"+finding.Fingerprint, developerAction{
			priority: developerFindingPriority(finding.Severity), issue: finding.Title,
			why: compactMarkdownText(finding.Message, 180), next: compactMarkdownText(next, 180),
			evidence: locationText(finding.Path, finding.StartLine),
		})
	}

	for _, system := range developerSystemResults(value) {
		for _, objective := range system.Objectives {
			if _, covered := coveredSystemObjectives[system.SystemID+"\x00"+objective.ObjectiveID]; covered {
				continue
			}
			action, include := developerObjectiveAction(objective)
			if !include {
				continue
			}
			if action.evidence == "" {
				action.evidence = developerEvidenceReferenceText(objective.EvidenceReferences)
			}
			key := "objective/" + system.SystemID + "/" + objective.ObjectiveID
			addAction(key, action)
		}
	}

	if value.RepositoryAnalysis != nil {
		for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
			rawID := developerRawObjectiveID(observation.ObjectiveID)
			if _, covered := coveredRepositoryObservations[observation.AIUseID+"\x00"+observation.SystemID+"\x00"+observation.ObjectiveID]; covered {
				continue
			}
			verdict := observation.DerivedTechnicalVerdict()
			if verdict == providers.RepositoryVerdictImplemented {
				continue
			}
			why := developerVerdictLabel(verdict) + ". " + compactMarkdownText(observation.Rationale, 150)
			next := developerObjectiveNextStep(observation, view.evidenceBundle)
			addAction("objective/"+observation.SystemID+"/"+rawID, developerAction{
				priority: "Review", issue: developerObjectiveTitle(rawID, objectiveTitles),
				why: compactMarkdownText(why, 180), next: compactMarkdownText(next, 180),
				evidence: developerCitationText(observation.SupportingEvidence), control: true,
			})
		}
	}

	for _, result := range value.ExecutionVerifications {
		if result.Status == verification.StatusPassed {
			view.verificationPassed++
			continue
		}
		view.verificationFailed++
		addAction("verification/"+result.RecipeID, developerAction{
			priority: "High", issue: "Execution check failed: " + result.RecipeID,
			why:  fmt.Sprintf("The isolated check exited with status %d.", result.ExitCode),
			next: fmt.Sprintf("Inspect the check output in %s, fix the failure, and rerun the scan.", view.evidenceBundle), evidence: result.RecipeID,
		})
	}
	if len(value.Warnings) > 0 {
		warningSummary := compactMarkdownText(value.Warnings[0], 160)
		if len(value.Warnings) > 1 {
			warningSummary += fmt.Sprintf(" (%d additional warning(s) are in %s.)", len(value.Warnings)-1, view.evidenceBundle)
		}
		addAction("scan-warnings", developerAction{
			priority: "Review", issue: "Scan incomplete or uncertain",
			why:  warningSummary,
			next: "Resolve the warning and rerun the scan before relying on the result.", evidence: "Scan warning",
		})
	}
	if developerRepositoryAnalysisIncomplete(value) && len(value.Warnings) == 0 {
		addAction("repository-analysis-incomplete", developerAction{
			priority: "Review", issue: "AI code review incomplete",
			why:  "No globally synthesized model conclusion was retained; deterministic scan results remain available.",
			next: "Rerun the scan after checking provider availability and limits before relying on the AI review layer.", evidence: "Repository AI review",
		})
	}

	sort.SliceStable(view.actions, func(left, right int) bool {
		return developerPriorityRank(view.actions[left].priority) > developerPriorityRank(view.actions[right].priority)
	})
	view.actionTotal = len(view.actions)
	if len(view.actions) > maxDeveloperActions {
		view.actions = view.actions[:maxDeveloperActions]
	}
	view.evidence, view.evidenceTotal = developerSupportingEvidence(value, objectiveTitles)
	view.questions, view.questionTotal = developerQuestions(value)

	switch {
	case developerHasUrgentAction(view.actions):
		view.outcome = "Action required"
	case developerRepositoryAnalysisIncomplete(value):
		view.outcome = "Scan incomplete — deterministic results remain available"
	case view.controlsToReview > 0:
		view.outcome = "Scan completed — technical follow-up recommended"
	case view.actionTotal > 0:
		view.outcome = "Scan completed — review recommended"
	default:
		view.outcome = "Scan completed — no urgent code changes identified"
	}
	return view
}

func writeDeveloperOutcomeMarkdown(writer io.Writer, value Report, view developerReportView) error {
	reviewedLabel := "AI-reviewed safeguards"
	if value.AIUseMappings != nil {
		reviewedLabel = "AI-reviewed safeguards within confirmed-use scopes"
	}
	if _, err := fmt.Fprintf(writer, "\n## Summary\n\n**%s**\n\n- Scan status: **%s**\n- High/medium deterministic findings: **%d**\n",
		view.outcome, developerAnalysisSummaryLabel(value), view.importantRisks); err != nil {
		return err
	}
	if inventorySummary := developerAIInventorySummary(value); inventorySummary != "" {
		if _, err := fmt.Fprintf(writer, "- AI workflows and components: **%s**\n", markdownText(inventorySummary)); err != nil {
			return err
		}
	}
	if value.RepositoryAnalysis != nil || view.implemented+view.partial+view.notImplemented+view.cannotDetermine > 0 {
		if view.implemented+view.partial+view.notImplemented+view.cannotDetermine == 0 {
			if _, err := fmt.Fprintf(writer, "- %s: **no code-level decisions returned**\n", reviewedLabel); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(writer, "- %s: **%d fully evidenced in code, %d partial, %d not demonstrated, %d unclear**\n",
				reviewedLabel, view.implemented, view.partial, view.notImplemented, view.cannotDetermine); err != nil {
				return err
			}
		}
	}
	if view.controlsToReview > 0 {
		if _, err := fmt.Fprintf(writer, "- Safeguards needing follow-up or more evidence: **%d**\n", view.controlsToReview); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "- Open technical or product questions: **%d**\n", view.questionTotal); err != nil {
		return err
	}
	_, err := fmt.Fprintln(writer, "- Legal applicability: **not determined from repository code**")
	return err
}

func developerAIInventorySummary(value Report) string {
	parts := make([]string, 0, 3)
	suggested := 0
	if value.AIUseInventory != nil {
		if count := value.AIUseInventory.Summary.Confirmed; count > 0 {
			parts = append(parts, fmt.Sprintf("%d confirmed", count))
		}
		if count := value.AIUseInventory.Summary.Draft; count > 0 {
			parts = append(parts, fmt.Sprintf("%d optional draft", count))
		}
		suggested = value.AIUseInventory.Summary.Suggested
	} else if value.RepositoryAnalysis != nil {
		suggested = len(value.RepositoryAnalysis.Result.AIUses)
	}
	if suggested > 0 {
		parts = append(parts, fmt.Sprintf("%d suggested from reviewed code", suggested))
	}
	if len(parts) == 0 {
		return ""
	}
	result := strings.Join(parts, "; ")
	if value.AIUseInventory == nil || value.AIUseInventory.Summary.Confirmed == 0 {
		result += "; not confirmed as deployed"
	}
	return result
}

func developerAIUseCategory(name, purpose string) string {
	value := strings.ToLower(strings.TrimSpace(name + " " + purpose))
	for _, marker := range []string{"adapter", "model qualification", "model catalogue", "model catalog", "provider selection", "gateway", "infrastructure"} {
		if strings.Contains(value, marker) {
			return "Supporting infrastructure"
		}
	}
	for _, marker := range []string{"benchmark", "evaluation", "evaluate", "test harness", "test suite"} {
		if strings.Contains(value, marker) {
			return "Evaluation/test tooling"
		}
	}
	return "AI workflow"
}

func writeDeveloperAIUsesMarkdown(writer io.Writer, value Report, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n### AI workflows and supporting components"); err != nil {
		return err
	}
	if value.AIUseInventory != nil && (value.AIUseInventory.Summary.Draft > 0 || value.AIUseInventory.Summary.Suggested > 0 || value.AIUseInventory.Summary.UngroupedSignals > 0) {
		if _, err := fmt.Fprintln(writer, "\n> Organising code into confirmed AI uses is optional. It improves per-feature safeguard mapping in later scans, but you can act on this report without doing it."); err != nil {
			return err
		}
	}
	if value.AIUseInventory != nil {
		if err := writeDeveloperSavedAIUsesMarkdown(writer, "Confirmed AI-use scopes", value.AIUseInventory.Confirmed, value.AIUseInventory.ChangedScope); err != nil {
			return err
		}
		if err := writeDeveloperSavedAIUsesMarkdown(writer, "Draft AI-use scopes (optional)", value.AIUseInventory.Draft, value.AIUseInventory.ChangedScope); err != nil {
			return err
		}
		if err := writeDeveloperSavedAIUsesMarkdown(writer, "Retired AI uses", value.AIUseInventory.Retired, value.AIUseInventory.ChangedScope); err != nil {
			return err
		}
		if len(value.AIUseInventory.Suggested) > 0 {
			if _, err := fmt.Fprintln(writer, "\n#### Suggested workflows and components\n\nThe model proposed these descriptions from checked code citations. They are not confirmed as deployed product features. ComplyScan classifies them for presentation so shared infrastructure and evaluation tooling are not mistaken for separate product uses.\n\n| Type | Workflow or component | What it may do | Where it was found |\n|---|---|---|---|"); err != nil {
				return err
			}
			for _, suggestion := range value.AIUseInventory.Suggested {
				if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
					markdownTableText(developerAIUseCategory(suggestion.Name, suggestion.Purpose)),
					markdownTableText(developerPlainLanguage(suggestion.Name)), markdownTableText(developerPlainLanguage(compactMarkdownText(suggestion.Purpose, 160))),
					markdownTableText(developerCitationText(suggestion.Evidence))); err != nil {
					return err
				}
			}
			for _, suggestion := range value.AIUseInventory.Suggested {
				if err := writeDeveloperAIFactDetails(writer, suggestion.Name, suggestion.RepositoryFacts, suggestion.RoleCandidates); err != nil {
					return err
				}
			}
		}
		if len(value.AIUseInventory.UngroupedSignals) > 0 {
			if _, err := fmt.Fprintf(writer, "\n**Other AI-related code signals (%d):** %s. They remain part of the scan even though they are not assigned to a saved AI-use scope.\n",
				len(value.AIUseInventory.UngroupedSignals), markdownText(developerSignalLocationSummary(value.AIUseInventory.UngroupedSignals))); err != nil {
				return err
			}
		}
		if value.AIUseInventory.Summary.Confirmed == 0 && value.AIUseInventory.Summary.Draft == 0 && value.AIUseInventory.Summary.Suggested == 0 && value.AIUseInventory.Summary.UngroupedSignals == 0 {
			if _, err := fmt.Fprintln(writer, "\nNo saved AI uses, model suggestions, or ungrouped technical signals were recorded."); err != nil {
				return err
			}
		}
		if len(value.AIUseInventory.OrganizationUnknowns) > 0 &&
			(value.AIUseInventory.Summary.Confirmed > 0 || value.AIUseInventory.Summary.Draft > 0 || value.AIUseInventory.Summary.Suggested > 0 || value.AIUseInventory.Summary.UngroupedSignals > 0) {
			if _, err := fmt.Fprint(writer, "\n<details>\n<summary>Organisation context that repository code cannot establish</summary>\n\n"); err != nil {
				return err
			}
			for _, unknown := range value.AIUseInventory.OrganizationUnknowns {
				if _, err := fmt.Fprintf(writer, "- %s\n", markdownText(unknown)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(writer, "\nThese unknowns are report context only. They do not create a setup task or block the code scan.\n\n</details>\n"); err != nil {
				return err
			}
		}
	} else if value.RepositoryAnalysis != nil && len(value.RepositoryAnalysis.Result.AIUses) > 0 {
		if _, err := fmt.Fprintln(writer, "\n#### Suggested workflows and components\n\nThe model proposed these descriptions from checked code citations. They are not confirmed as deployed product features. ComplyScan classifies them for presentation so shared infrastructure and evaluation tooling are not mistaken for separate product uses.\n\n| Type | Workflow or component | What it may do | Where it was found |\n|---|---|---|---|"); err != nil {
			return err
		}
		for _, suggestion := range value.RepositoryAnalysis.Result.AIUses {
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
				markdownTableText(developerAIUseCategory(suggestion.Name, suggestion.Purpose)),
				markdownTableText(developerPlainLanguage(suggestion.Name)), markdownTableText(developerPlainLanguage(compactMarkdownText(suggestion.Purpose, 160))),
				markdownTableText(developerCitationText(suggestion.Evidence))); err != nil {
				return err
			}
		}
	}
	if developerRepositoryAnalysisIncomplete(value) {
		detail := "The deterministic scan completed, but the AI code review did not finish. Deterministic findings remain valid; no unsynthesized model conclusions were retained."
		if value.RepositoryAnalysis != nil && value.RepositoryAnalysis.Coverage.SourceBatchesTotal > 0 {
			detail = fmt.Sprintf("The deterministic scan completed, but the AI code review did not finish. %d distinct source batch(es) started a provider request and %d of %d produced validated responses; no unsynthesized model conclusions were retained.", value.RepositoryAnalysis.Coverage.SourceBatchesStarted, value.RepositoryAnalysis.Coverage.SourceBatchesCompleted, value.RepositoryAnalysis.Coverage.SourceBatchesTotal)
		}
		if _, err := fmt.Fprintln(writer, "\n"+detail); err != nil {
			return err
		}
	} else if value.RepositoryAnalysis == nil && value.RepositoryAnalysisRun == RepositoryAnalysisPending {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review has started, but this preliminary report does not contain its suggestions yet."); err != nil {
			return err
		}
	} else if value.RepositoryAnalysis == nil {
		if _, err := fmt.Fprintln(writer, "\nThis scan used deterministic checks only. No model assessed how the matched code works. Configure AI-assisted analysis and rerun `complyscan scan` to add code-level decisions."); err != nil {
			return err
		}
	} else if developerRepositoryAnalysisNoCandidate(value) {
		if _, err := fmt.Fprintln(writer, "\nLocal selection found no eligible structural AI code candidate, so no repository source was sent for AI code review and no model-derived zero-use conclusion was made."); err != nil {
			return err
		}
	} else if value.AIUseInventory == nil && len(value.RepositoryAnalysis.Result.AIUses) == 0 {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review did not suggest a specific AI use from the selected evidence. This does not prove that the repository contains no AI activity."); err != nil {
			return err
		}
	}
	if len(view.integrationSignals) > 0 {
		_, err := fmt.Fprintf(writer, "\n**AI libraries or configuration found:** %s. This shows that the repository refers to these tools, but not that they are active in the deployed product.\n", markdownText(strings.Join(view.integrationSignals, ", ")))
		return err
	}
	return nil
}

func writeDeveloperSavedAIUsesMarkdown(writer io.Writer, title string, values []aiuse.ObservedUse, changedScope bool) error {
	if len(values) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "\n#### %s\n\n| AI use | Description | Current scan | Configured repository scope |\n|---|---|---|---|\n", markdownText(title)); err != nil {
		return err
	}
	for _, observed := range values {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
			markdownTableText(observed.Use.Name), markdownTableText(compactMarkdownText(observed.Use.Description, 160)),
			markdownTableText(developerAIUseObservationLabel(observed.Observation, changedScope)),
			markdownTableText(strings.Join(observed.Use.Paths, ", "))); err != nil {
			return err
		}
	}
	for _, observed := range values {
		if err := writeDeveloperAIFactDetails(writer, observed.Use.Name, observed.RepositoryFacts, observed.RoleCandidates); err != nil {
			return err
		}
	}
	return nil
}

func writeDeveloperAIFactDetails(writer io.Writer, name string, review *aiuse.FactReview, roles []aiuse.RoleCandidate) error {
	if review == nil && len(roles) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "\n<details>\n<summary>Show code-derived details for %s</summary>\n\n##### What code indicates for %s\n", markdownText(name), markdownText(name)); err != nil {
		return err
	}
	if review != nil && len(review.Facts) == 0 && len(review.ModelProviders) == 0 {
		if _, err := fmt.Fprintln(writer, "\nThe reviewed code supported no positive per-use facts. This does not establish that a fact is false or that an implementation is absent."); err != nil {
			return err
		}
	}
	if review != nil && (len(review.Facts) > 0 || len(review.ModelProviders) > 0) {
		if _, err := fmt.Fprintln(writer, "\n| Code observation | Inferred value | Basis | Where |\n|---|---|---|---|"); err != nil {
			return err
		}
		written := 0
		for _, provider := range review.ModelProviders {
			if written == maxDeveloperUseFacts {
				break
			}
			if _, err := fmt.Fprintf(writer, "| Model provider integration | %s | %s | %s |\n",
				markdownTableText(provider.Name), markdownTableText(developerFactBasis(provider.Source, provider.Coverage)),
				markdownTableText(developerCitationText(provider.Evidence))); err != nil {
				return err
			}
			written++
		}
		for _, fact := range review.Facts {
			if written == maxDeveloperUseFacts {
				break
			}
			values := strings.Join(fact.Values, ", ")
			if fact.Confidence != "" {
				values += " (" + fact.Confidence + " confidence)"
			}
			basis := developerFactBasis(fact.Source, fact.Coverage)
			if strings.TrimSpace(fact.Rationale) != "" {
				basis = developerPlainLanguage(fact.Rationale) + " — " + basis
			}
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
				markdownTableText(developerCodeFactLabel(string(fact.Field))), markdownTableText(developerPlainLanguage(values)),
				markdownTableText(basis), markdownTableText(developerCitationText(fact.Evidence))); err != nil {
				return err
			}
			written++
		}
		if remaining := len(review.ModelProviders) + len(review.Facts) - written; remaining > 0 {
			if _, err := fmt.Fprintf(writer, "\n%d more code observation(s) are available in the JSON evidence bundle.\n", remaining); err != nil {
				return err
			}
		}
	}
	if len(roles) > 0 {
		if _, err := fmt.Fprintln(writer, "\n**Possible roles indicated by the repository**\n\n| Possible role | Why it is possible | What code cannot establish |\n|---|---|---|"); err != nil {
			return err
		}
		for _, role := range roles {
			missing := strings.Join(role.MissingOrganizationFacts, "; ")
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s |\n",
				markdownTableText(developerRoleLabel(string(role.Role))), markdownTableText(developerPlainLanguage(role.Rationale)),
				markdownTableText(developerPlainLanguage(missing))); err != nil {
				return err
			}
		}
	}
	if review != nil && len(review.UnresolvedQuestions) > 0 {
		if _, err := fmt.Fprintf(writer, "\n**Not resolved from the reviewed code:** %s\n", markdownText(strings.Join(review.UnresolvedQuestions, "; "))); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\n</details>")
	return err
}

func developerFactBasis(source aiuse.FactSource, coverage aiuse.FactCoverage) string {
	if source == aiuse.FactSourceModel {
		if coverage == aiuse.FactCoverageChangedAndConnected {
			return "AI review of changed and connected code"
		}
		return "AI review with checked code citations"
	}
	return "Local repository evidence"
}

func developerCodeFactLabel(field string) string {
	switch field {
	case "intended-purpose":
		return "Intended purpose"
	case "lifecycle-stage":
		return "Lifecycle indication"
	case "use-case-domains":
		return "Use-case domain"
	case "decision-impact":
		return "Output impact"
	case "human-oversight":
		return "Human control"
	case "ai-activities":
		return "AI activity"
	case "deployment-models":
		return "Deployment mechanism"
	case "users":
		return "Possible users"
	case "affected-groups":
		return "Possibly affected groups"
	case "personal-data":
		return "Personal-data handling"
	case "special-category-data":
		return "Sensitive-data handling"
	case "children-data":
		return "Children's-data handling"
	default:
		return field
	}
}

func developerRoleLabel(role string) string {
	switch role {
	case "downstream-provider":
		return "Downstream provider"
	case "provider":
		return "Provider"
	case "deployer":
		return "Deployer"
	default:
		return role
	}
}

type developerUseCheck struct {
	framework   string
	context     string
	title       string
	requirement string
	codeResult  string
	evidence    string
	priority    int
}

func writeDeveloperAIUseMappingsMarkdown(writer io.Writer, value Report, evidenceBundle string) error {
	if value.AIUseMappings == nil || len(value.AIUseMappings.Uses) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\n### Requirements and code evidence by confirmed AI use\n\nEach use is checked only against code inside its saved repository paths. Applicability comes from its associated configured system and remains a screening result, not a legal conclusion."); err != nil {
		return err
	}
	for _, use := range value.AIUseMappings.Uses {
		if _, err := fmt.Fprintf(writer, "\n#### %s\n\n- Stable use ID: %s\n- Repository scope: %s\n", markdownText(use.UseName), inlineCode(use.UseID), markdownText(strings.Join(use.Paths, ", "))); err != nil {
			return err
		}
		checks := developerUseChecks(use)
		if len(checks) == 0 {
			if _, err := fmt.Fprintln(writer, "- No currently indicated or unresolved code objective was produced for this use."); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(writer, "\n| Framework and context | Code check | What may apply | Result for this use | Where |\n|---|---|---|---|---|"); err != nil {
			return err
		}
		visible := checks
		if len(visible) > maxDeveloperUseChecks {
			visible = visible[:maxDeveloperUseChecks]
		}
		for _, check := range visible {
			if _, err := fmt.Fprintf(writer, "| %s — %s | %s | %s | %s | %s |\n",
				markdownTableText(check.framework), markdownTableText(check.context), markdownTableText(check.title),
				markdownTableText(check.requirement), markdownTableText(check.codeResult), markdownTableText(check.evidence)); err != nil {
				return err
			}
		}
		if remaining := len(checks) - len(visible); remaining > 0 {
			if _, err := fmt.Fprintf(writer, "\n%d more per-use check(s) are available in %s.\n", remaining, inlineCode(evidenceBundle)); err != nil {
				return err
			}
		}
	}
	return nil
}

func developerUseChecks(use usemapping.UseResult) []developerUseCheck {
	result := make([]developerUseCheck, 0)
	for _, frameworkResult := range use.Frameworks {
		frameworkName := frameworkResult.Name
		if frameworkName == "" {
			frameworkName = frameworkResult.ID
		}
		for _, context := range frameworkResult.Contexts {
			contextName := developerUseContextLabel(context.Association)
			for _, objective := range context.Objectives {
				if objective.Requirement == reconciliation.RequirementNotCurrentlyIndicated && objective.Evidence != framework.ObjectiveCandidate && objective.AIReview == nil {
					continue
				}
				result = append(result, developerUseCheck{
					framework: frameworkName, context: contextName, title: objective.Title,
					requirement: developerUseRequirementLabel(objective.Requirement),
					codeResult:  developerUseCodeResult(objective), evidence: developerUseEvidence(objective),
					priority: developerUseCheckPriority(objective),
				})
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].priority != result[j].priority {
			return result[i].priority < result[j].priority
		}
		if result[i].framework != result[j].framework {
			return result[i].framework < result[j].framework
		}
		if result[i].context != result[j].context {
			return result[i].context < result[j].context
		}
		return result[i].title < result[j].title
	})
	return result
}

func developerUseContextLabel(value usemapping.Association) string {
	switch value.Status {
	case usemapping.AssociationConfigured:
		if value.SystemName != "" {
			return value.SystemName + " (" + value.SystemID + ")"
		}
		return value.SystemID
	case usemapping.AssociationMissing:
		return "Missing configured system " + value.SystemID
	default:
		return "No configured system association"
	}
}

func developerUseContextNeedsAssociation(value usemapping.ContextResult) bool {
	if value.Association.Status == usemapping.AssociationConfigured {
		return false
	}
	for _, objective := range value.Objectives {
		if objective.Requirement == reconciliation.RequirementUnresolved || objective.Requirement == reconciliation.RequirementContextDependent {
			return true
		}
	}
	return false
}

func developerUseRequirementLabel(value reconciliation.RequirementStatus) string {
	switch value {
	case reconciliation.RequirementLikelyRequired:
		return "Likely required from declared context"
	case reconciliation.RequirementRecommended:
		return "Recommended practice"
	case reconciliation.RequirementContextDependent:
		return "Applicability needs review"
	case reconciliation.RequirementUnresolved:
		return "Cannot determine from saved context"
	default:
		return "Not currently indicated"
	}
}

func developerUseCodeResult(value usemapping.ObjectiveResult) string {
	if value.AIReview != nil {
		return developerVerdictLabel(value.AIReview.Verdict)
	}
	if value.Investigation != nil {
		switch value.Investigation.Conclusion {
		case providers.ConclusionSubstantiated:
			return "Implementation substantiated by the bounded AI evidence review"
		case providers.ConclusionPartial:
			return "Only a partial implementation was substantiated by the bounded AI evidence review"
		case providers.ConclusionTestOnly:
			return "Implementation evidence was found only in test code"
		case providers.ConclusionUnreachable:
			return "The matching implementation appears unreachable from production code"
		case providers.ConclusionNotSubstantiated:
			return "The bounded AI evidence review did not substantiate the implementation"
		case providers.ConclusionNotFoundAfterInvestigation:
			return "No implementation was found in the bounded AI evidence search"
		default:
			return "The bounded AI evidence review could not determine the implementation state"
		}
	}
	switch value.Evidence {
	case framework.ObjectiveCandidate:
		return "Matching code signal found; run an AI review for a code-level implementation decision"
	case framework.ObjectiveNotEvaluated:
		return "Could not fully evaluate this use's code scope"
	default:
		if len(value.EvidenceOutsideUse) > 0 {
			return "No matching code signal inside the saved paths; related signal found elsewhere in the repository"
		}
		return "No matching code signal found in this use's saved paths"
	}
}

func developerUseEvidence(value usemapping.ObjectiveResult) string {
	if value.AIReview != nil {
		citations := append(append([]providers.RepositoryCitation(nil), value.AIReview.SupportingEvidence...), value.AIReview.ContradictoryEvidence...)
		if result := developerCitationText(citations); result != "" {
			return result
		}
	}
	if len(value.EvidenceReferences) == 0 && len(value.EvidenceOutsideUse) > 0 {
		return "Outside saved paths: " + developerOutsideUseEvidenceText(value.EvidenceOutsideUse)
	}
	return developerEvidenceReferenceText(value.EvidenceReferences)
}

func developerUseCheckPriority(value usemapping.ObjectiveResult) int {
	if value.Requirement == reconciliation.RequirementLikelyRequired {
		verdict, reviewed := developerUseReviewedVerdict(value)
		if reviewed && verdict == providers.RepositoryVerdictNotImplemented {
			return 0
		}
		if value.Mapping == reconciliation.MappingRequirementWithoutEvidence {
			return 1
		}
		if reviewed && verdict != providers.RepositoryVerdictImplemented {
			return 2
		}
		return 4
	}
	if value.Requirement == reconciliation.RequirementUnresolved || value.Requirement == reconciliation.RequirementContextDependent {
		return 3
	}
	if value.Requirement == reconciliation.RequirementRecommended {
		verdict, reviewed := developerUseReviewedVerdict(value)
		if value.Mapping == reconciliation.MappingRecommendedWithoutEvidence || (reviewed && verdict != providers.RepositoryVerdictImplemented) {
			return 5
		}
	}
	if value.Mapping == reconciliation.MappingRecommendedWithoutEvidence {
		return 5
	}
	return 6
}

func developerUseObjectiveAction(use usemapping.UseResult, association usemapping.Association, objective usemapping.ObjectiveResult) (developerAction, bool) {
	if association.Status != usemapping.AssociationConfigured {
		return developerAction{}, false
	}
	evidence := developerUseEvidence(objective)
	context := use.UseName + " under " + developerUseContextLabel(association)
	if objective.Mapping == reconciliation.MappingEvidenceMismatch {
		return developerAction{
			priority: "Review", issue: context + ": check the declared context for " + objective.Title,
			why:  "Related code was found inside this AI use, but its declared system context does not currently indicate the safeguard.",
			next: "Confirm the AI-use association and the configured system facts before deciding whether this safeguard applies.", evidence: evidence, control: true,
		}, true
	}
	if objective.Requirement != reconciliation.RequirementLikelyRequired && objective.Requirement != reconciliation.RequirementRecommended {
		return developerAction{}, false
	}
	if objective.AIReview != nil {
		switch objective.AIReview.Verdict {
		case providers.RepositoryVerdictImplemented:
			return developerAction{}, false
		case providers.RepositoryVerdictNotImplemented:
			if objective.Requirement == reconciliation.RequirementRecommended {
				return developerAction{
					priority: "Review", issue: context + ": " + objective.Title,
					why:  "The selected voluntary framework recommends this safeguard, but the code review did not demonstrate it inside the confirmed AI-use paths.",
					next: "Decide whether to implement the practice for this AI use and document that decision.", evidence: evidence, control: true,
				}, true
			}
			return developerAction{
				priority: "High", issue: context + ": " + objective.Title,
				why:  "The code review did not demonstrate this safeguard inside the confirmed AI-use paths.",
				next: "Implement the safeguard for this AI use or correct its saved path scope, then rerun `complyscan scan`.", evidence: evidence, control: true,
			}, true
		case providers.RepositoryVerdictPartial:
			why := "The code review found only a partial implementation inside this AI use."
			if objective.Requirement == reconciliation.RequirementRecommended {
				why = "The selected voluntary framework recommends this safeguard, and the code review found only a partial implementation inside this AI use."
			}
			return developerAction{
				priority: "Review", issue: context + ": " + objective.Title,
				why:  why,
				next: "Address the missing implementation elements recorded in the evidence bundle, then rerun `complyscan scan`.", evidence: evidence, control: true,
			}, true
		default:
			why := "The code review could not determine whether this safeguard is implemented for this AI use."
			if objective.Requirement == reconciliation.RequirementRecommended {
				why = "The selected voluntary framework recommends this safeguard, but the code review could not determine whether it is implemented for this AI use."
			}
			return developerAction{
				priority: "Review", issue: context + ": " + objective.Title,
				why:  why,
				next: "Provide the missing code context or narrow the saved use paths, then rerun `complyscan scan`.", evidence: evidence, control: true,
			}, true
		}
	}
	if objective.Investigation != nil {
		return developerUseInvestigationAction(context, objective, evidence)
	}
	if objective.Mapping == reconciliation.MappingUnableToEvaluate {
		return developerAction{
			priority: "Review", issue: context + ": " + objective.Title,
			why:  "ComplyScan could not fully evaluate code inside this AI use that may be relevant to the safeguard.",
			next: "Review the unsupported code manually or add analyzer support, then rerun ComplyScan.", evidence: evidence, control: true,
		}, true
	}
	if objective.Mapping == reconciliation.MappingRequirementWithoutEvidence {
		why := "The declared system context indicates this safeguard is likely required, but no matching signal was found inside this AI use's saved paths."
		next := "Confirm the use scope and implement the safeguard if applicable; an explicit AI review can then assess the code-level implementation."
		if len(objective.EvidenceOutsideUse) > 0 {
			why = "The declared system context indicates this safeguard is likely required. A related signal exists elsewhere in the repository, but not inside this AI use's saved paths."
			next = "Check whether the outside code is shared by this AI use. If it is, add that path to the use scope; otherwise implement the safeguard for this use."
		}
		return developerAction{
			priority: "High", issue: context + ": " + objective.Title,
			why:  why,
			next: next, evidence: evidence, control: true,
		}, true
	}
	if objective.Mapping == reconciliation.MappingRecommendedWithoutEvidence {
		why := "The selected voluntary framework recommends this safeguard, but no matching signal was found inside this AI use's saved paths."
		next := "Decide whether to implement the practice for this AI use and document that decision."
		if len(objective.EvidenceOutsideUse) > 0 {
			why = "The selected voluntary framework recommends this safeguard. A related signal exists elsewhere in the repository, but not inside this AI use's saved paths."
			next = "Check whether the outside code is shared by this AI use. If it is, add that path to the use scope; otherwise decide whether to implement the practice here."
		}
		return developerAction{
			priority: "Review", issue: context + ": " + objective.Title,
			why:  why,
			next: next, evidence: evidence, control: true,
		}, true
	}
	if objective.Mapping == reconciliation.MappingRequirementWithEvidence || objective.Mapping == reconciliation.MappingRecommendedWithEvidence {
		return developerAction{
			priority: "Review", issue: context + ": verify " + objective.Title,
			why:  "A local code signal was found inside this AI use, but no use-scoped AI implementation decision is available.",
			next: "Rerun `complyscan scan` with AI-assisted analysis configured to obtain a code-level decision.", evidence: evidence, control: true,
		}, true
	}
	return developerAction{}, false
}

func developerUseInvestigationAction(context string, objective usemapping.ObjectiveResult, evidence string) (developerAction, bool) {
	conclusion := objective.Investigation.Conclusion
	if conclusion == providers.ConclusionSubstantiated {
		return developerAction{}, false
	}
	priority := "Review"
	if objective.Requirement == reconciliation.RequirementLikelyRequired &&
		(conclusion == providers.ConclusionNotSubstantiated || conclusion == providers.ConclusionNotFoundAfterInvestigation) {
		priority = "High"
	}
	why := developerUseCodeResult(objective) + "."
	if objective.Requirement == reconciliation.RequirementRecommended {
		why = "The selected voluntary framework recommends this safeguard. " + why
	}
	next := "Review the bounded investigation details in the evidence bundle, address the missing implementation or evidence, and rerun `complyscan scan`."
	return developerAction{
		priority: priority, issue: context + ": " + objective.Title,
		why: why, next: next, evidence: evidence, control: true,
	}, true
}

func developerAIUseObservationLabel(status aiuse.ObservationStatus, changedScope bool) string {
	switch status {
	case aiuse.ObservationModelReviewed:
		return "Matched by the completed AI review"
	case aiuse.ObservationTechnicalSignal:
		return "Matching local technical signal found"
	default:
		if changedScope {
			return "Not reviewed in this change-focused run"
		}
		return "No matching signal was observed in this scan"
	}
}

func developerSignalLocationSummary(values []aiuse.SignalLocation) string {
	parts := make([]string, 0, 3)
	for index, signal := range values {
		if index == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s at %s", signal.Component, locationText(signal.Path, signal.Line)))
	}
	if remaining := len(values) - len(parts); remaining > 0 {
		parts = append(parts, fmt.Sprintf("%d more", remaining))
	}
	return strings.Join(parts, ", ")
}

func writeDeveloperActionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## 1. What to do next"); err != nil {
		return err
	}
	if len(view.actions) == 0 {
		message := "ComplyScan did not identify an urgent code change."
		if view.questionTotal > 0 {
			message += " The unanswered questions are product context that code cannot establish; resolve them when you need a fuller compliance assessment."
		}
		_, err := fmt.Fprintln(writer, "\n"+message)
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nThe report starts with the three highest-priority next steps. The complete evidence remains in the sections below and in the JSON bundle.\n\n| Priority | Recommended action | Why | Evidence |\n|---|---|---|---|"); err != nil {
		return err
	}
	for _, action := range view.actions {
		why := strings.TrimRight(strings.TrimSpace(developerPlainLanguage(action.issue)), ".;: ")
		if detail := strings.TrimSpace(developerPlainLanguage(action.why)); detail != "" {
			why += ". " + detail
		}
		if _, err := fmt.Fprintf(writer, "| **%s** | %s | %s | %s |\n",
			markdownTableText(action.priority), markdownTableText(developerPlainLanguage(action.next)),
			markdownTableText(why), markdownTableText(action.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.actionTotal - len(view.actions); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d additional follow-up finding(s) are detailed below and in %s.\n", remaining, inlineCode(view.evidenceBundle))
		return err
	}
	return nil
}

func writeDeveloperEvidenceMarkdown(writer io.Writer, view developerReportView) error {
	if len(view.evidence) == 0 {
		if view.hasPerUseMappings {
			return nil
		}
		return nil
	}
	heading := "Code-level safeguard decisions"
	if view.hasPerUseMappings {
		heading = "Other code-level evidence"
	}
	if _, err := fmt.Fprintln(writer, "\n### "+heading); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nA safeguard is a code control or test intended to reduce AI failure or misuse. “Partial” means relevant code was found, but the complete safeguard was not demonstrated.\n\n| Safeguard | Code-level result | Missing or next check | Evidence |\n|---|---|---|---|"); err != nil {
		return err
	}
	for _, evidence := range view.evidence {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
			markdownTableText(developerPlainLanguage(evidence.title)), markdownTableText(developerPlainLanguage(evidence.assessment)),
			markdownTableText(developerPlainLanguage(evidence.followUp)), markdownTableText(evidence.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.evidenceTotal - len(view.evidence); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d more code-level safeguard decision(s) are available in %s.\n", remaining, inlineCode(view.evidenceBundle))
		return err
	}
	return nil
}

func writeDeveloperQuestionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## 3. Open questions"); err != nil {
		return err
	}
	if len(view.questions) == 0 {
		_, err := fmt.Fprintln(writer, "\nComplyScan did not record any unanswered question for this scan.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nSome questions need additional code; organisation, deployment, and legal context can be completed later in the dashboard. They do not block this code scan."); err != nil {
		return err
	}
	for _, question := range view.questions {
		if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(developerPlainQuestion(question))); err != nil {
			return err
		}
	}
	if remaining := view.questionTotal - len(view.questions); remaining > 0 {
		if _, err := fmt.Fprintf(writer, "\n- %d more question(s) are available in %s.", remaining, inlineCode(view.evidenceBundle)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func writeDeveloperReferenceDetailsMarkdown(writer io.Writer, view developerReportView) error {
	if len(view.referenceDetails) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\n<details>\n<summary>Legal and technical details</summary>\n\nThese are the sources used to define the available code checks. Their inclusion does not mean that every source or requirement applies to your product."); err != nil {
		return err
	}
	for _, detail := range view.referenceDetails {
		if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(detail)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(writer, "\n\nRequirement IDs, raw model output, and complete scanner results are available in %s.\n\n</details>\n", inlineCode(view.evidenceBundle))
	return err
}

func developerObjectiveAction(objective reconciliation.ObjectiveResult) (developerAction, bool) {
	action := developerAction{issue: objective.Title, control: true}
	switch objective.Mapping {
	case reconciliation.MappingEvidenceMismatch:
		action.priority = "High"
		action.why = "Related code was found, but ComplyScan could not tell which configured AI feature it belongs to."
		action.next = "Correct the code-to-feature mapping or confirm which AI feature uses this implementation."
		return action, true
	case reconciliation.MappingUnableToEvaluate:
		action.priority = "Review"
		action.why = "ComplyScan could not fully check code that may be relevant to this safeguard."
		action.next = "Review that code manually or add support for its programming language, then run ComplyScan again."
		return action, true
	case reconciliation.MappingApplicabilityUnresolved:
		// Missing applicability facts are presented once as questions instead of
		// expanding every potentially affected control into a separate action.
		return developerAction{}, false
	case reconciliation.MappingRequirementWithoutEvidence:
		action.priority = "High"
		action.why = "Your setup answers suggest this safeguard may be needed, but ComplyScan found no matching code."
		action.next = "First confirm that this applies. If it does, implement the safeguard or point ComplyScan to where it exists."
		return action, true
	}
	return developerAction{}, false
}

func developerSystemResults(value Report) []reconciliation.SystemResult {
	results := make([]reconciliation.SystemResult, 0)
	if len(value.Frameworks) > 0 {
		for _, result := range value.Frameworks {
			results = append(results, result.Reconciliation.Systems...)
		}
		return results
	}
	if value.Reconciliation != nil {
		return value.Reconciliation.Systems
	}
	return results
}

func developerObjectiveTitles(value Report) map[string]string {
	titles := make(map[string]string)
	addEvidence := func(evidence framework.TechnicalEvidenceReport) {
		for _, objective := range evidence.Objectives {
			titles[objective.ID] = objective.Title
		}
	}
	for _, result := range value.Frameworks {
		addEvidence(result.TechnicalEvidence)
	}
	if len(value.Frameworks) == 0 && value.TechnicalEvidence != nil {
		addEvidence(*value.TechnicalEvidence)
	}
	for _, system := range developerSystemResults(value) {
		for _, objective := range system.Objectives {
			titles[objective.ObjectiveID] = objective.Title
		}
	}
	if value.AIUseMappings != nil {
		for _, use := range value.AIUseMappings.Uses {
			for _, frameworkResult := range use.Frameworks {
				for _, context := range frameworkResult.Contexts {
					for _, objective := range context.Objectives {
						titles[objective.ObjectiveID] = objective.Title
					}
				}
			}
		}
	}
	return titles
}

func developerSupportingEvidence(value Report, titles map[string]string) ([]developerEvidence, int) {
	items := make([]developerEvidence, 0)
	seen := make(map[string]struct{})
	observations := make(map[string]providers.RepositoryObjectiveObservation)
	add := func(key string, item developerEvidence) {
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if value.AIUseMappings != nil {
		if value.RepositoryAnalysis != nil {
			for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
				if developerRepositoryObservationCoveredByUse(value, observation) {
					continue
				}
				verdict := observation.DerivedTechnicalVerdict()
				rawID := developerRawObjectiveID(observation.ObjectiveID)
				add("repository-level/"+observation.SystemID+"/"+observation.ObjectiveID, developerEvidence{
					title:      "Repository-level, not assigned to one confirmed AI use: " + developerObjectiveTitle(rawID, titles),
					assessment: developerVerdictAssessment(observation), followUp: developerObservationFollowUp(observation),
					evidence: developerCitationText(observation.SupportingEvidence), verdict: verdict,
				})
			}
		}
	} else if value.RepositoryAnalysis != nil {
		for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
			rawID := developerRawObjectiveID(observation.ObjectiveID)
			observations[rawID] = observation
			verdict := observation.DerivedTechnicalVerdict()
			add(rawID, developerEvidence{
				title: developerObjectiveTitle(rawID, titles), assessment: developerVerdictAssessment(observation),
				followUp: developerObservationFollowUp(observation), evidence: developerCitationText(observation.SupportingEvidence), verdict: verdict,
			})
		}
	}
	addDeterministic := func(evidence framework.TechnicalEvidenceReport) {
		for _, objective := range evidence.Objectives {
			if objective.Status != framework.ObjectiveCandidate || len(objective.Matches) == 0 {
				continue
			}
			locations := make([]string, 0, 2)
			for index, match := range objective.Matches {
				if index == 2 {
					break
				}
				locations = append(locations, locationText(match.Path, match.StartLine))
			}
			item := developerEvidence{title: objective.Title, evidence: strings.Join(locations, ", ")}
			if observation, reviewed := observations[objective.ID]; reviewed {
				item.verdict = observation.DerivedTechnicalVerdict()
				item.assessment = developerVerdictAssessment(observation)
				item.followUp = developerObservationFollowUp(observation)
				if len(observation.SupportingEvidence) > 0 {
					item.evidence = developerCitationText(observation.SupportingEvidence)
				}
			} else if value.RepositoryAnalysis != nil {
				item.verdict = providers.RepositoryVerdictCannotDetermine
				item.assessment = "The deterministic scanner found a code match, but the AI review did not evaluate this safeguard"
				item.followUp = "Review the complete implementation path or include the missing connected code in a later scan"
			} else {
				item.verdict = providers.RepositoryVerdictCannotDetermine
				item.assessment = "A code match was found; rerun `complyscan scan` with AI-assisted analysis for a code-level decision"
				item.followUp = "Run AI-assisted code review before treating this signal as an implemented safeguard"
			}
			add(objective.ID, item)
		}
	}
	if value.AIUseMappings == nil {
		for _, result := range value.Frameworks {
			addDeterministic(result.TechnicalEvidence)
		}
		if len(value.Frameworks) == 0 && value.TechnicalEvidence != nil {
			addDeterministic(*value.TechnicalEvidence)
		}
	}
	for _, result := range value.ExecutionVerifications {
		if result.Status != verification.StatusPassed {
			continue
		}
		add("verification/"+result.RecipeID, developerEvidence{
			title:      "Execution check: " + result.RecipeID,
			assessment: "The configured check passed; production behaviour still needs confirmation",
			followUp:   "Confirm that the same boundary and configuration are used in production",
			evidence:   compactMarkdownText(result.Boundary, 160),
		})
	}
	total := len(items)
	if len(items) > maxDeveloperEvidence {
		items = items[:maxDeveloperEvidence]
	}
	return items, total
}

func developerTechnicalVerdictCounts(value Report) (implemented, partial, notImplemented, cannotDetermine int) {
	if value.AIUseMappings != nil {
		seen := make(map[string]struct{})
		for _, use := range value.AIUseMappings.Uses {
			for _, frameworkResult := range use.Frameworks {
				for _, context := range frameworkResult.Contexts {
					for _, objective := range context.Objectives {
						verdict, reviewed := developerUseReviewedVerdict(objective)
						if !reviewed {
							continue
						}
						objectiveKey := objective.ObjectiveID
						if objective.AIReview != nil && objective.AIReview.RepositoryObjectiveID != "" {
							objectiveKey = objective.AIReview.RepositoryObjectiveID
						} else if frameworkResult.ID != "" {
							objectiveKey = frameworkResult.ID + "/" + objective.ObjectiveID
						}
						key := use.UseID + "\x00" + objectiveKey
						if _, exists := seen[key]; exists {
							continue
						}
						seen[key] = struct{}{}
						switch verdict {
						case providers.RepositoryVerdictImplemented:
							implemented++
						case providers.RepositoryVerdictPartial:
							partial++
						case providers.RepositoryVerdictNotImplemented:
							notImplemented++
						default:
							cannotDetermine++
						}
					}
				}
			}
		}
		return
	}
	if value.RepositoryAnalysis == nil {
		return 0, 0, 0, 0
	}
	for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
		switch observation.DerivedTechnicalVerdict() {
		case providers.RepositoryVerdictImplemented:
			implemented++
		case providers.RepositoryVerdictPartial:
			partial++
		case providers.RepositoryVerdictNotImplemented:
			notImplemented++
		default:
			cannotDetermine++
		}
	}
	return implemented, partial, notImplemented, cannotDetermine
}

func developerUseReviewedVerdict(objective usemapping.ObjectiveResult) (providers.RepositoryTechnicalVerdict, bool) {
	if objective.AIReview != nil {
		return objective.AIReview.Verdict, true
	}
	if objective.Investigation == nil {
		return "", false
	}
	switch objective.Investigation.Conclusion {
	case providers.ConclusionSubstantiated:
		return providers.RepositoryVerdictImplemented, true
	case providers.ConclusionPartial, providers.ConclusionTestOnly, providers.ConclusionUnreachable:
		return providers.RepositoryVerdictPartial, true
	case providers.ConclusionNotSubstantiated, providers.ConclusionNotFoundAfterInvestigation:
		return providers.RepositoryVerdictNotImplemented, true
	default:
		return providers.RepositoryVerdictCannotDetermine, true
	}
}

func developerVerdictAssessment(observation providers.RepositoryObjectiveObservation) string {
	assessment := developerVerdictLabel(observation.DerivedTechnicalVerdict())
	if rationale := compactMarkdownText(observation.Rationale, 150); rationale != "" {
		assessment += ". " + rationale
	}
	return assessment
}

func developerObjectiveNextStep(observation providers.RepositoryObjectiveObservation, evidenceBundle string) string {
	followUp := "Review the connected implementation and missing conditions"
	if len(observation.MissingEvidence) > 0 {
		followUp = developerListPreview(observation.MissingEvidence, 1)
	} else if len(observation.UnresolvedQuestions) > 0 {
		followUp = developerListPreview(observation.UnresolvedQuestions, 1)
	}
	followUp = developerPlainLanguage(strings.TrimRight(strings.TrimSpace(followUp), ".;: "))
	switch observation.DerivedTechnicalVerdict() {
	case providers.RepositoryVerdictNotImplemented:
		return "Implement this safeguard: " + followUp + ". Then rerun ComplyScan"
	case providers.RepositoryVerdictCannotDetermine:
		return "Show where this safeguard is enforced: " + followUp + ". Then rerun ComplyScan"
	case providers.RepositoryVerdictPartial:
		return "Complete this safeguard: " + followUp + ". Then rerun ComplyScan"
	default:
		return fmt.Sprintf("Review the supporting evidence in %s.", evidenceBundle)
	}
}

func developerObservationFollowUp(observation providers.RepositoryObjectiveObservation) string {
	if len(observation.MissingEvidence) > 0 {
		return "Missing evidence: " + developerListPreview(observation.MissingEvidence, 2)
	}
	if len(observation.UnresolvedQuestions) > 0 {
		return "Resolve: " + developerListPreview(observation.UnresolvedQuestions, 1)
	}
	switch observation.DerivedTechnicalVerdict() {
	case providers.RepositoryVerdictImplemented:
		return "No code change identified by this review; confirm production configuration separately"
	case providers.RepositoryVerdictPartial:
		return "Review the remaining paths and conditions not demonstrated by the cited code"
	case providers.RepositoryVerdictNotImplemented:
		return "No supporting implementation was demonstrated in the reviewed code"
	default:
		return "Provide the connected implementation or ownership context needed for a decision"
	}
}

func developerListPreview(values []string, maximum int) string {
	parts := make([]string, 0, maximum)
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), ".;: ")
		if value == "" {
			continue
		}
		parts = append(parts, value)
		if len(parts) == maximum {
			break
		}
	}
	result := strings.Join(parts, "; ")
	if remaining := len(values) - len(parts); remaining > 0 {
		result += fmt.Sprintf(" (%d more in latest.json)", remaining)
	}
	return compactMarkdownText(result, 220)
}

func developerVerdictLabel(verdict providers.RepositoryTechnicalVerdict) string {
	switch verdict {
	case providers.RepositoryVerdictImplemented:
		return "Implemented in the reviewed code"
	case providers.RepositoryVerdictPartial:
		return "Partially implemented in the reviewed code"
	case providers.RepositoryVerdictNotImplemented:
		return "Not implemented in the reviewed code"
	default:
		return "Could not determine from the reviewed code"
	}
}

func developerQuestions(value Report) ([]string, int) {
	questions := make([]string, 0)
	seen := make(map[string]struct{})
	add := func(question string) {
		question = strings.TrimSpace(question)
		if question == "" {
			return
		}
		key := strings.ToLower(strings.Join(strings.Fields(question), " "))
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		questions = append(questions, question)
	}
	if value.AIUseMappings != nil {
		for _, use := range value.AIUseMappings.Uses {
			for _, frameworkResult := range use.Frameworks {
				for _, context := range frameworkResult.Contexts {
					if developerUseContextNeedsAssociation(context) {
						switch context.Association.Status {
						case usemapping.AssociationNone:
							add("Which configured system contains the AI use " + use.UseName + "?")
						case usemapping.AssociationMissing:
							add("Which current configured system replaces " + context.Association.SystemID + " for the AI use " + use.UseName + "?")
						}
					}
					for _, objective := range context.Objectives {
						if objective.AIReview == nil {
							continue
						}
						for _, question := range objective.AIReview.UnresolvedQuestions {
							add(use.UseName + ": " + question)
						}
					}
				}
			}
		}
	}
	if len(value.Frameworks) > 0 {
		for _, result := range value.Frameworks {
			if result.Applicability == nil {
				continue
			}
			for _, system := range result.Applicability.Systems {
				for _, missing := range system.MissingContext {
					add(missing)
				}
			}
		}
	} else if value.Applicability != nil {
		for _, system := range value.Applicability.Systems {
			for _, missing := range system.MissingContext {
				add(missing)
			}
		}
	}
	if value.RepositoryAnalysis != nil {
		for _, use := range value.RepositoryAnalysis.Result.AIUses {
			for _, question := range use.UnresolvedQuestions {
				add(question)
			}
		}
		for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
			if developerRepositoryObservationCoveredByUse(value, observation) {
				continue
			}
			for _, question := range observation.UnresolvedQuestions {
				add(question)
			}
		}
		for _, question := range value.RepositoryAnalysis.Result.UnresolvedQuestions {
			add(question)
		}
	}
	if len(developerIntegrationSignals(value)) > 0 && (value.RepositoryAnalysis == nil || len(value.RepositoryAnalysis.Result.AIUses) == 0) {
		add("Are the AI libraries or settings found here actually used in the deployed product, and what data do they process?")
	}
	total := len(questions)
	if len(questions) > maxDeveloperQuestions {
		questions = questions[:maxDeveloperQuestions]
	}
	return questions, total
}

func developerRepositoryObservationCoveredByUse(value Report, observation providers.RepositoryObjectiveObservation) bool {
	if value.AIUseMappings == nil {
		return false
	}
	for _, use := range value.AIUseMappings.Uses {
		for _, frameworkResult := range use.Frameworks {
			for _, context := range frameworkResult.Contexts {
				for _, objective := range context.Objectives {
					if objective.AIReview == nil || objective.AIReview.RepositoryObjectiveID != observation.ObjectiveID || objective.AIReview.SystemID != observation.SystemID {
						continue
					}
					if observation.AIUseID != "" {
						if objective.AIReview.Attribution == usemapping.ReviewAttributionExplicitUse && use.UseID == observation.AIUseID {
							return true
						}
						continue
					}
					if objective.AIReview.Attribution == usemapping.ReviewAttributionMatchingCitations {
						return true
					}
				}
			}
		}
	}
	return false
}

func developerIntegrationSignals(value Report) []string {
	if value.AIInventory == nil {
		return nil
	}
	names := make([]string, 0, len(value.AIInventory.Components))
	for _, component := range value.AIInventory.Components {
		names = append(names, component.Name)
	}
	return names
}

func developerReferenceDetails(value Report) []string {
	details := make([]string, 0)
	add := func(name string, evidence framework.TechnicalEvidenceReport) {
		seen := make(map[string]struct{})
		references := make([]string, 0)
		for _, objective := range evidence.Objectives {
			reference := strings.TrimSpace(objective.SourceReference)
			if reference == "" {
				continue
			}
			if _, exists := seen[reference]; exists {
				continue
			}
			seen[reference] = struct{}{}
			references = append(references, reference)
		}
		if len(references) == 0 {
			return
		}
		details = append(details, fmt.Sprintf("%s — %s", name, strings.Join(references, ", ")))
	}
	for _, result := range value.Frameworks {
		add(result.Name, result.TechnicalEvidence)
	}
	if len(value.Frameworks) == 0 && value.TechnicalEvidence != nil {
		add(value.TechnicalEvidence.Pack.Name, *value.TechnicalEvidence)
	}
	return details
}

func developerPlainQuestion(question string) string {
	normalized := strings.ToLower(strings.TrimSpace(question))
	switch normalized {
	case "operating regions have not been established.":
		return "Where will this AI feature be offered or used?"
	case "use-case domains have not been established.":
		return "What will this AI feature be used for?"
	case "the organization's ai value-chain role has not been established.":
		return "Is your organisation building, selling, deploying, or operating this AI feature?"
	case "the intended purpose has not been established.":
		return "What is this AI feature intended to do?"
	case "the lifecycle stage has not been established.":
		return "Is this AI feature in development, testing, production, or retired?"
	case "decision impact has not been established.":
		return "How much can this AI feature affect a person's outcome?"
	case "human oversight has not been established.":
		return "Can a person review, stop, or override this AI feature?"
	case "ai activities such as inference, training, evaluation, automated decisions, agent tool use, or synthetic-content generation have not been established.":
		return "Does this code train, evaluate, run, or control AI models?"
	case "one or more data categories have not been established.":
		return "What types of data does this AI feature process?"
	case "deployment models have not been established.":
		return "Where and how is this AI feature deployed?"
	case "the system's users have not been established.":
		return "Who uses this AI feature?"
	case "potentially affected groups have not been established.":
		return "Who could be affected by this AI feature's outputs?"
	default:
		return developerPlainLanguage(question)
	}
}

func developerPlainLanguage(value string) string {
	replacer := strings.NewReplacer(
		"Only test-side evidence is submitted", "The reviewed evidence only shows tests",
		"only test-side evidence is submitted", "the reviewed evidence only shows tests",
		"No retry or fallback behavior is shown in this submitted flow", "Add retry and fallback handling to the production AI call path",
		"no retry or fallback behavior is shown in this submitted flow", "add retry and fallback handling to the production AI call path",
		"No submitted code shows benchmark execution or pass/fail evaluation", "Run the benchmark in CI or another executable workflow and enforce its pass/fail result",
		"no submitted code shows benchmark execution or pass/fail evaluation", "run the benchmark in CI or another executable workflow and enforce its pass/fail result",
		"submitted executable flow", "reviewed executable flow",
		"submitted flow", "reviewed flow",
		"submitted code", "reviewed code",
		"submitted slice", "reviewed code",
		"submitted segment", "reviewed code",
		"submitted excerpt", "reviewed code",
		"candidate evidence", "possible matching code",
		"Candidate evidence", "Possible matching code",
		"technical objective", "code safeguard",
		"Technical objective", "Code safeguard",
		"technical control", "safeguard in the code",
		"Technical control", "Safeguard in the code",
		"applicability", "whether this applies",
		"Applicability", "Whether this applies",
		"not substantiated", "not confirmed",
		"Not substantiated", "Not confirmed",
		"unmapped", "not connected to an AI feature",
		"Unmapped", "Not connected to an AI feature",
		"outside this batch", "not established by the reviewed code",
		"Outside this batch", "Not established by the reviewed code",
		"were not submitted", "were not included in the reviewed evidence",
		"was not submitted", "was not included in the reviewed evidence",
		"No submitted test", "No reviewed test",
		"submitted test", "reviewed test",
		"in this batch", "in the reviewed code",
		"In this batch", "In the reviewed code",
	)
	return replacer.Replace(value)
}

func developerCitationText(citations []providers.RepositoryCitation) string {
	locations := make([]string, 0, 2)
	for index, citation := range citations {
		if index == 2 {
			break
		}
		locations = append(locations, locationText(citation.Path, citation.Line))
	}
	if len(locations) == 0 {
		return "No code location supplied"
	}
	if remaining := len(citations) - len(locations); remaining > 0 {
		locations = append(locations, fmt.Sprintf("%d more", remaining))
	}
	return strings.Join(locations, ", ")
}

func developerEvidenceReferenceText(references []reconciliation.EvidenceReference) string {
	locations := make([]string, 0, 2)
	for index, reference := range references {
		if index == 2 {
			break
		}
		locations = append(locations, locationText(reference.Path, reference.Line))
	}
	if len(locations) == 0 {
		return "No matching code location found"
	}
	return strings.Join(locations, ", ")
}

func developerOutsideUseEvidenceText(references []usemapping.EvidenceLocation) string {
	locations := make([]string, 0, 2)
	for index, reference := range references {
		if index == 2 {
			break
		}
		locations = append(locations, locationText(reference.Path, reference.Line))
	}
	if len(locations) == 0 {
		return "No matching code location found"
	}
	return strings.Join(locations, ", ")
}

func developerRawObjectiveID(value string) string {
	if index := strings.LastIndex(value, "/"); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}

func developerObjectiveTitle(id string, titles map[string]string) string {
	if title := strings.TrimSpace(titles[id]); title != "" {
		return title
	}
	return id
}

func developerFindingPriority(severity rules.Severity) string {
	switch severity {
	case rules.SeverityCritical:
		return "Critical"
	case rules.SeverityHigh:
		return "High"
	default:
		return "Medium"
	}
}

func developerPriorityRank(priority string) int {
	switch priority {
	case "Critical":
		return 4
	case "High":
		return 3
	case "Medium":
		return 2
	default:
		return 1
	}
}

func developerHasUrgentAction(actions []developerAction) bool {
	for _, action := range actions {
		if developerPriorityRank(action.priority) >= developerPriorityRank("High") {
			return true
		}
	}
	return false
}

func repositoryAnalysisModeLabel(mode providers.RepositoryAnalysisMode) string {
	switch mode {
	case providers.RepositoryAnalysisTargeted:
		return "selected structural code evidence"
	case providers.RepositoryAnalysisFull:
		return "all repository files"
	case providers.RepositoryAnalysisSubsystem:
		return "grouped sections of the repository"
	case providers.RepositoryAnalysisSynthesis:
		return "grouped sections of the repository"
	case providers.RepositoryAnalysisBoundedOnly:
		return "focused checks only"
	default:
		return "the available repository context"
	}
}

func developerAnalysisSummaryLabel(value Report) string {
	if developerRepositoryAnalysisIncomplete(value) {
		return "deterministic checks complete; AI code review incomplete"
	}
	if developerRepositoryAnalysisNoCandidate(value) {
		return "deterministic checks complete; no structural AI code candidate selected"
	}
	if value.RepositoryAnalysis != nil {
		return "deterministic checks + completed AI code review"
	}
	if value.RepositoryAnalysisRun == RepositoryAnalysisPending {
		return "deterministic checks complete; AI code review still running"
	}
	if developerHasBoundedAIReview(value) {
		return "deterministic checks + bounded AI safeguard review"
	}
	return "deterministic checks only"
}

func developerHasBoundedAIReview(value Report) bool {
	if value.Review != nil || value.TechnicalReview != nil {
		return true
	}
	for _, result := range value.Frameworks {
		if result.TechnicalReview != nil {
			return true
		}
	}
	return false
}

func developerRepositoryAnalysisLabel(value Report) string {
	if developerRepositoryAnalysisIncomplete(value) {
		if value.RepositoryAnalysis != nil && value.RepositoryAnalysis.Coverage.SourceBatchesTotal > 0 {
			return fmt.Sprintf("incomplete after %d source batch(es) started a provider request and %d of %d produced validated responses; deterministic results are available", value.RepositoryAnalysis.Coverage.SourceBatchesStarted, value.RepositoryAnalysis.Coverage.SourceBatchesCompleted, value.RepositoryAnalysis.Coverage.SourceBatchesTotal)
		}
		if value.RepositoryAnalysis != nil && value.RepositoryAnalysis.Coverage.Subsystems > 0 {
			return fmt.Sprintf("incomplete after %d bounded code batch(es); deterministic results are available", value.RepositoryAnalysis.Coverage.Subsystems)
		}
		return "incomplete; deterministic results are available"
	}
	if developerRepositoryAnalysisNoCandidate(value) {
		return "no source review run — no eligible structural candidate was selected"
	}
	if value.RepositoryAnalysis != nil {
		cacheSuffix := ""
		if value.RepositoryAnalysis.CacheHit {
			cacheSuffix = " (reused private cache)"
		}
		if value.RepositoryAnalysis.Coverage.ReviewScope == providers.RepositoryReviewScopeChanged {
			return fmt.Sprintf("completed for %d changed and %d connected code file(s)%s", value.RepositoryAnalysis.Coverage.ChangedFiles, value.RepositoryAnalysis.Coverage.ConnectedFiles, cacheSuffix)
		}
		return fmt.Sprintf("completed using %s%s", repositoryAnalysisModeLabel(value.RepositoryAnalysis.Coverage.Mode), cacheSuffix)
	}
	switch value.RepositoryAnalysisRun {
	case RepositoryAnalysisPending:
		return "still running; deterministic results are available"
	case RepositoryAnalysisIncomplete:
		return "incomplete; deterministic results are available"
	default:
		if developerRepositoryAnalysisIncomplete(value) {
			return "incomplete; deterministic results are available"
		}
		if developerHasBoundedAIReview(value) {
			return "not run; bounded safeguard review completed"
		}
		return "not run — deterministic checks only"
	}
}

func developerRepositoryAnalysisNoCandidate(value Report) bool {
	return value.RepositoryAnalysisRun == RepositoryAnalysisCompleted && value.RepositoryAnalysis != nil &&
		value.RepositoryAnalysis.Coverage.Mode == providers.RepositoryAnalysisTargeted && value.RepositoryAnalysis.Coverage.FilesSubmitted == 0
}

func developerRepositoryAnalysisIncomplete(value Report) bool {
	if value.RepositoryAnalysisRun == RepositoryAnalysisIncomplete {
		return true
	}
	for _, warning := range value.Warnings {
		normalized := strings.ToLower(warning)
		if strings.Contains(normalized, "repository analysis was incomplete") || strings.Contains(normalized, "repository-wide analysis was incomplete") {
			return true
		}
	}
	return false
}
