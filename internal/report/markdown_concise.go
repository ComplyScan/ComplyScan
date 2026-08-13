package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

const (
	maxDeveloperActions   = 8
	maxDeveloperEvidence  = 5
	maxDeveloperQuestions = 5
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
	evidence   string
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
}

func writeDeveloperReportMarkdown(writer io.Writer, value Report) error {
	view := buildDeveloperReportView(value)
	if _, err := fmt.Fprintln(writer, "\n> Developer review of repository evidence. This is not a legal compliance decision."); err != nil {
		return err
	}
	if err := writeDeveloperOutcomeMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperAIUsesMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperActionsMarkdown(writer, view); err != nil {
		return err
	}
	if err := writeDeveloperEvidenceMarkdown(writer, view); err != nil {
		return err
	}
	if err := writeDeveloperQuestionsMarkdown(writer, view); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "\n## Scan coverage\n\n- Source files indexed: **%d of %d**", view.filesIndexed, view.sourceFilesSeen); err != nil {
		return err
	}
	if view.unsupportedFiles > 0 {
		if _, err := fmt.Fprintf(writer, " (%d potentially relevant unsupported)", view.unsupportedFiles); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "\n- Repository AI analysis: **%s**\n- Scan warnings: **%d**\n- Scan ID: %s\n- Complete evidence and diagnostics: %s\n",
		markdownText(view.repositoryAnalysis), len(value.Warnings), inlineCode(value.Scan.ID), inlineCode("latest.json")); err != nil {
		return err
	}
	if view.verificationPassed+view.verificationFailed > 0 {
		if _, err := fmt.Fprintf(writer, "- Isolated execution checks: **%d passed, %d failed**\n", view.verificationPassed, view.verificationFailed); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\n---\n\nA found signal is a review lead, not proof that a control is effective. A missing signal means only that this scan did not find configured repository evidence.")
	return err
}

func buildDeveloperReportView(value Report) developerReportView {
	counts := markdownCounts(value)
	view := developerReportView{
		sourceFilesSeen: counts.SourceFilesSeen, filesIndexed: counts.FilesIndexed,
		unsupportedFiles: counts.UnsupportedFiles, repositoryAnalysis: "not performed",
	}
	if value.RepositoryAnalysis != nil {
		view.repositoryAnalysis = fmt.Sprintf("completed using %s context", repositoryAnalysisModeLabel(value.RepositoryAnalysis.Coverage.Mode))
		if view.sourceFilesSeen == 0 {
			view.sourceFilesSeen = value.RepositoryAnalysis.Coverage.RepositoryFiles
		}
		if view.filesIndexed == 0 {
			view.filesIndexed = value.RepositoryAnalysis.Coverage.RepositoryFiles
		}
	}
	view.integrationSignals = developerIntegrationSignals(value)
	objectiveTitles := developerObjectiveTitles(value)
	relevantObjectives := developerRelevantObjectives(value)
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
			if observation.Strength != providers.StrengthWeak && observation.Strength != providers.StrengthUncertain && observation.Strength != providers.StrengthNotSupported {
				continue
			}
			if _, relevant := relevantObjectives[rawID]; !relevant {
				continue
			}
			next := "Confirm the implementation or add the missing technical control."
			if len(observation.MissingEvidence) > 0 {
				next = observation.MissingEvidence[0]
			}
			addAction("objective/"+observation.SystemID+"/"+rawID, developerAction{
				priority: "Review", issue: developerObjectiveTitle(rawID, objectiveTitles),
				why: compactMarkdownText(observation.Rationale, 180), next: compactMarkdownText(next, 180),
				evidence: developerCitationText(observation.SupportingEvidence), control: true,
			})
		}
		for index, observation := range value.RepositoryAnalysis.Result.UnmappedObservations {
			next := observation.SuggestedReview
			if next == "" {
				next = "Confirm the purpose and map this activity to the appropriate technical control."
			}
			addAction(fmt.Sprintf("unmapped/%d/%s", index, observation.Summary), developerAction{
				priority: "Review", issue: observation.Summary,
				why: compactMarkdownText(observation.Reason, 180), next: compactMarkdownText(next, 180),
				evidence: developerCitationText(observation.Evidence),
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
			next: "Inspect the check output in latest.json, fix the failure, and rerun the scan.", evidence: result.RecipeID,
		})
	}
	if len(value.Warnings) > 0 {
		warningSummary := compactMarkdownText(value.Warnings[0], 160)
		if len(value.Warnings) > 1 {
			warningSummary += fmt.Sprintf(" (%d additional warning(s) are in latest.json.)", len(value.Warnings)-1)
		}
		addAction("scan-warnings", developerAction{
			priority: "Review", issue: "Scan incomplete or uncertain",
			why:  warningSummary,
			next: "Resolve the warning and rerun the scan before relying on the result.", evidence: "Scan warning",
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
	case view.actionTotal > 0 || view.questionTotal > 0 || view.evidenceTotal > 0:
		view.outcome = "Review needed"
	default:
		view.outcome = "No urgent code risks found"
	}
	return view
}

func writeDeveloperOutcomeMarkdown(writer io.Writer, value Report, view developerReportView) error {
	aiUses := 0
	if value.RepositoryAnalysis != nil {
		aiUses = len(value.RepositoryAnalysis.Result.AIUses)
	}
	_, err := fmt.Fprintf(writer, "\n## Overall result\n\n**%s**\n\n- AI uses identified by repository analysis: **%d**\n- Important code risks: **%d**\n- Controls needing review: **%d**\n- Supporting code-evidence leads: **%d**\n- Questions requiring confirmation: **%d**\n",
		view.outcome, aiUses, view.importantRisks, view.controlsToReview, view.evidenceTotal, view.questionTotal)
	return err
}

func writeDeveloperAIUsesMarkdown(writer io.Writer, value Report, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## AI uses found"); err != nil {
		return err
	}
	if value.RepositoryAnalysis == nil {
		if _, err := fmt.Fprintln(writer, "\nNo repository-wide AI reasoning was performed, so this scan cannot conclude which AI uses are implemented."); err != nil {
			return err
		}
	} else if len(value.RepositoryAnalysis.Result.AIUses) == 0 {
		if _, err := fmt.Fprintln(writer, "\nThe repository analysis did not identify a specific AI use. This is not proof that the repository contains no AI activity."); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(writer, "\n| AI use | Purpose | Confidence | Code evidence |\n|---|---|---|---|"); err != nil {
			return err
		}
		for _, use := range value.RepositoryAnalysis.Result.AIUses {
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
				markdownTableText(use.Name), markdownTableText(compactMarkdownText(use.Purpose, 160)),
				markdownTableText(use.Confidence), markdownTableText(developerCitationText(use.Evidence))); err != nil {
				return err
			}
		}
	}
	if len(view.integrationSignals) > 0 {
		_, err := fmt.Fprintf(writer, "\n**Integration signals:** %s. These references show relevant dependencies or configuration, not necessarily an active AI use.\n", markdownText(strings.Join(view.integrationSignals, ", ")))
		return err
	}
	return nil
}

func writeDeveloperActionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## What needs attention"); err != nil {
		return err
	}
	if len(view.actions) == 0 {
		_, err := fmt.Fprintln(writer, "\nNo urgent repository action was identified. Review the questions and evidence below before drawing a compliance conclusion.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n| Priority | Issue | Why it matters | Next step | Evidence |\n|---|---|---|---|---|"); err != nil {
		return err
	}
	for _, action := range view.actions {
		if _, err := fmt.Fprintf(writer, "| **%s** | %s | %s | %s | %s |\n",
			markdownTableText(action.priority), markdownTableText(action.issue), markdownTableText(action.why),
			markdownTableText(action.next), markdownTableText(action.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.actionTotal - len(view.actions); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d additional item(s) are recorded in %s.\n", remaining, inlineCode("latest.json"))
		return err
	}
	return nil
}

func writeDeveloperEvidenceMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## Supporting code evidence"); err != nil {
		return err
	}
	if len(view.evidence) == 0 {
		_, err := fmt.Fprintln(writer, "\nNo positive technical-control evidence lead was identified in this scan.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n| Technical objective | Assessment | Code evidence |\n|---|---|---|"); err != nil {
		return err
	}
	for _, evidence := range view.evidence {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s |\n",
			markdownTableText(evidence.title), markdownTableText(evidence.assessment), markdownTableText(evidence.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.evidenceTotal - len(view.evidence); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d additional evidence lead(s) are recorded in %s.\n", remaining, inlineCode("latest.json"))
		return err
	}
	return nil
}

func writeDeveloperQuestionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## Questions to confirm"); err != nil {
		return err
	}
	if len(view.questions) == 0 {
		_, err := fmt.Fprintln(writer, "\nNo unresolved repository or applicability question was recorded.")
		return err
	}
	for _, question := range view.questions {
		if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(question)); err != nil {
			return err
		}
	}
	if remaining := view.questionTotal - len(view.questions); remaining > 0 {
		if _, err := fmt.Fprintf(writer, "\n- %d additional question(s) are recorded in %s.", remaining, inlineCode("latest.json")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func developerObjectiveAction(objective reconciliation.ObjectiveResult) (developerAction, bool) {
	action := developerAction{issue: objective.Title, control: true}
	switch objective.Mapping {
	case reconciliation.MappingEvidenceMismatch:
		action.priority = "High"
		action.why = "Code evidence was found, but it conflicts with the configured system ownership or applicability context."
		action.next = "Correct the system mapping or confirm which implementation owns this control."
		return action, true
	case reconciliation.MappingUnableToEvaluate:
		action.priority = "Review"
		action.why = "Potentially relevant code could not be fully analysed."
		action.next = "Review the unsupported code manually or add analyzer support, then rerun the scan."
		return action, true
	case reconciliation.MappingApplicabilityUnresolved:
		// Missing applicability facts are presented once as questions instead of
		// expanding every potentially affected control into a separate action.
		return developerAction{}, false
	case reconciliation.MappingRequirementWithoutEvidence:
		action.priority = "High"
		action.why = "The configured profile indicates this control may be required, but the scan found no supporting code evidence."
		action.next = "Confirm applicability, then implement the control or document where its technical evidence lives."
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

func developerRelevantObjectives(value Report) map[string]struct{} {
	relevant := make(map[string]struct{})
	for _, system := range developerSystemResults(value) {
		for _, objective := range system.Objectives {
			if objective.Requirement == reconciliation.RequirementLikelyRequired || objective.Mapping == reconciliation.MappingEvidenceMismatch {
				relevant[objective.ObjectiveID] = struct{}{}
			}
		}
	}
	return relevant
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
	return titles
}

func developerSupportingEvidence(value Report, titles map[string]string) ([]developerEvidence, int) {
	items := make([]developerEvidence, 0)
	seen := make(map[string]struct{})
	add := func(key string, item developerEvidence) {
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if value.RepositoryAnalysis != nil {
		for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
			if observation.Strength != providers.StrengthStrong && observation.Strength != providers.StrengthPartial {
				continue
			}
			if len(observation.SupportingEvidence) == 0 {
				continue
			}
			rawID := developerRawObjectiveID(observation.ObjectiveID)
			assessment := "Strong code-evidence lead"
			if observation.Strength == providers.StrengthPartial {
				assessment = "Partial code-evidence lead"
			}
			add(rawID, developerEvidence{
				title: developerObjectiveTitle(rawID, titles), assessment: assessment,
				evidence: developerCitationText(observation.SupportingEvidence),
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
			add(objective.ID, developerEvidence{
				title: objective.Title, assessment: "Automated code signal; human confirmation needed",
				evidence: strings.Join(locations, ", "),
			})
		}
	}
	for _, result := range value.Frameworks {
		addDeterministic(result.TechnicalEvidence)
	}
	if len(value.Frameworks) == 0 && value.TechnicalEvidence != nil {
		addDeterministic(*value.TechnicalEvidence)
	}
	for _, result := range value.ExecutionVerifications {
		if result.Status != verification.StatusPassed {
			continue
		}
		add("verification/"+result.RecipeID, developerEvidence{
			title:      "Execution check: " + result.RecipeID,
			assessment: "Passed in an isolated environment",
			evidence:   compactMarkdownText(result.Boundary, 160),
		})
	}
	total := len(items)
	if len(items) > maxDeveloperEvidence {
		items = items[:maxDeveloperEvidence]
	}
	return items, total
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
			for _, question := range observation.UnresolvedQuestions {
				add(question)
			}
		}
		for _, question := range value.RepositoryAnalysis.Result.UnresolvedQuestions {
			add(question)
		}
	}
	if len(developerIntegrationSignals(value)) > 0 && (value.RepositoryAnalysis == nil || len(value.RepositoryAnalysis.Result.AIUses) == 0) {
		add("Are the detected AI integration references active in a deployed product, and what data do they process?")
	}
	total := len(questions)
	if len(questions) > maxDeveloperQuestions {
		questions = questions[:maxDeveloperQuestions]
	}
	return questions, total
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
		return "No code evidence detected"
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
	case providers.RepositoryAnalysisFull:
		return "full-repository"
	case providers.RepositoryAnalysisSubsystem:
		return "subsystem"
	default:
		return string(mode)
	}
}
