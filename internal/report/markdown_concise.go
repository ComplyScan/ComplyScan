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
}

func writeDeveloperReportMarkdown(writer io.Writer, value Report) error {
	view := buildDeveloperReportView(value)
	if _, err := fmt.Fprintln(writer, "\n> A developer-focused review of code found in this repository. ComplyScan cannot decide on its own whether your product complies with a law."); err != nil {
		return err
	}
	if err := writeDeveloperOutcomeMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperAIUsesMarkdown(writer, value, view); err != nil {
		return err
	}
	if err := writeDeveloperEvidenceMarkdown(writer, view); err != nil {
		return err
	}
	if err := writeDeveloperActionsMarkdown(writer, view); err != nil {
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
	if _, err := fmt.Fprintf(writer, "\n- AI code review: **%s**\n- Parts of the scan that need attention: **%d**\n- Scan ID: %s\n- Full technical results: %s\n",
		markdownText(view.repositoryAnalysis), len(value.Warnings), inlineCode(value.Scan.ID), inlineCode("latest.json")); err != nil {
		return err
	}
	if view.verificationPassed+view.verificationFailed > 0 {
		if _, err := fmt.Fprintf(writer, "- Isolated execution checks: **%d passed, %d failed**\n", view.verificationPassed, view.verificationFailed); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer, "\n---\n\nCode-level verdicts describe the repository evidence ComplyScan reviewed. They do not decide whether a legal requirement applies or whether the implementation is enabled and effective in production.")
	return err
}

func buildDeveloperReportView(value Report) developerReportView {
	counts := markdownCounts(value)
	view := developerReportView{
		sourceFilesSeen: counts.SourceFilesSeen, filesIndexed: counts.FilesIndexed,
		unsupportedFiles: counts.UnsupportedFiles, repositoryAnalysis: developerRepositoryAnalysisLabel(value),
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
			verdict := observation.DerivedTechnicalVerdict()
			if verdict == providers.RepositoryVerdictImplemented {
				continue
			}
			if _, relevant := relevantObjectives[rawID]; !relevant {
				continue
			}
			why := developerVerdictLabel(verdict) + ". " + compactMarkdownText(observation.Rationale, 150)
			next := "Add the missing implementation, then rerun ComplyScan."
			if verdict == providers.RepositoryVerdictPartial {
				next = "Complete the missing implementation elements listed in latest.json, then rerun ComplyScan."
			} else if verdict == providers.RepositoryVerdictCannotDetermine {
				next = "Provide the missing code context or ownership information listed in latest.json, then rerun ComplyScan."
			}
			addAction("objective/"+observation.SystemID+"/"+rawID, developerAction{
				priority: "Review", issue: developerObjectiveTitle(rawID, objectiveTitles),
				why: compactMarkdownText(why, 180), next: compactMarkdownText(next, 180),
				evidence: developerCitationText(observation.SupportingEvidence), control: true,
			})
		}
		for index, observation := range value.RepositoryAnalysis.Result.UnmappedObservations {
			next := observation.SuggestedReview
			if next == "" {
				next = "Confirm what this code does and whether it belongs to one of the AI features listed above."
			}
			addAction(fmt.Sprintf("unmapped/%d/%s", index, observation.Summary), developerAction{
				priority: "Review", issue: observation.Summary,
				why: "AI-related code was found, but ComplyScan could not connect it to a known AI feature or safeguard.", next: compactMarkdownText(next, 180),
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
	case view.actionTotal > 0 || view.questionTotal > 0:
		view.outcome = "Review needed"
	default:
		view.outcome = "No urgent code problems found"
	}
	return view
}

func writeDeveloperOutcomeMarkdown(writer io.Writer, value Report, view developerReportView) error {
	aiUses := 0
	if value.RepositoryAnalysis != nil {
		aiUses = len(value.RepositoryAnalysis.Result.AIUses)
	}
	_, err := fmt.Fprintf(writer, "\n## Overall result\n\n**%s**\n\n- AI features found: **%d**\n- Important code problems: **%d**\n- Safeguards needing code changes or more evidence: **%d**\n- AI-reviewed code safeguards: **%d implemented, %d partial, %d not demonstrated, %d unclear**\n- Questions only a person can answer: **%d**\n",
		view.outcome, aiUses, view.importantRisks, view.controlsToReview, view.implemented, view.partial, view.notImplemented, view.cannotDetermine, view.questionTotal)
	return err
}

func writeDeveloperAIUsesMarkdown(writer io.Writer, value Report, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## 1. What ComplyScan found\n\n### AI features"); err != nil {
		return err
	}
	if value.RepositoryAnalysis == nil && developerRepositoryAnalysisIncomplete(value) {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review started but did not finish, so ComplyScan could not identify specific AI features from the selected code evidence. See the next-step section for the failure."); err != nil {
			return err
		}
	} else if value.RepositoryAnalysis == nil && value.RepositoryAnalysisRun == RepositoryAnalysisPending {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review has started, but this preliminary report does not contain its result yet."); err != nil {
			return err
		}
	} else if value.RepositoryAnalysis == nil {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review was not run, so ComplyScan could not identify specific AI features from the way the code works."); err != nil {
			return err
		}
	} else if len(value.RepositoryAnalysis.Result.AIUses) == 0 {
		if _, err := fmt.Fprintln(writer, "\nThe AI code review did not identify a specific AI feature in the selected evidence. This does not prove that the repository contains no AI activity."); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(writer, "\n| AI feature | What it does | Where ComplyScan found it |\n|---|---|---|"); err != nil {
			return err
		}
		for _, use := range value.RepositoryAnalysis.Result.AIUses {
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s |\n",
				markdownTableText(developerPlainLanguage(use.Name)), markdownTableText(developerPlainLanguage(compactMarkdownText(use.Purpose, 160))),
				markdownTableText(developerCitationText(use.Evidence))); err != nil {
				return err
			}
		}
	}
	if len(view.integrationSignals) > 0 {
		_, err := fmt.Fprintf(writer, "\n**AI libraries or configuration found:** %s. This shows that the repository refers to these tools, but not that they are active in the deployed product.\n", markdownText(strings.Join(view.integrationSignals, ", ")))
		return err
	}
	return nil
}

func writeDeveloperActionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## 2. What to do next"); err != nil {
		return err
	}
	if len(view.actions) == 0 {
		message := "ComplyScan did not identify an urgent code change."
		if view.questionTotal > 0 {
			message += " Answer the remaining questions before deciding whether more work is needed."
		}
		_, err := fmt.Fprintln(writer, "\n"+message)
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n| Priority | What ComplyScan found | Why it matters | What to do | Where |\n|---|---|---|---|---|"); err != nil {
		return err
	}
	for _, action := range view.actions {
		if _, err := fmt.Fprintf(writer, "| **%s** | %s | %s | %s | %s |\n",
			markdownTableText(action.priority), markdownTableText(developerPlainLanguage(action.issue)), markdownTableText(developerPlainLanguage(action.why)),
			markdownTableText(developerPlainLanguage(action.next)), markdownTableText(action.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.actionTotal - len(view.actions); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d more item(s) are available in %s.\n", remaining, inlineCode("latest.json"))
		return err
	}
	return nil
}

func writeDeveloperEvidenceMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n### Code-level safeguard decisions"); err != nil {
		return err
	}
	if len(view.evidence) == 0 {
		_, err := fmt.Fprintln(writer, "\nComplyScan did not produce a code-level decision for any checked safeguard.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n| Safeguard | Code-level result | Where |\n|---|---|---|"); err != nil {
		return err
	}
	for _, evidence := range view.evidence {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s |\n",
			markdownTableText(developerPlainLanguage(evidence.title)), markdownTableText(developerPlainLanguage(evidence.assessment)), markdownTableText(evidence.evidence)); err != nil {
			return err
		}
	}
	if remaining := view.evidenceTotal - len(view.evidence); remaining > 0 {
		_, err := fmt.Fprintf(writer, "\n%d more code-level safeguard decision(s) are available in %s.\n", remaining, inlineCode("latest.json"))
		return err
	}
	return nil
}

func writeDeveloperQuestionsMarkdown(writer io.Writer, view developerReportView) error {
	if _, err := fmt.Fprintln(writer, "\n## 3. What ComplyScan could not determine"); err != nil {
		return err
	}
	if len(view.questions) == 0 {
		_, err := fmt.Fprintln(writer, "\nComplyScan did not record any unanswered question for this scan.")
		return err
	}
	for _, question := range view.questions {
		if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(developerPlainQuestion(question))); err != nil {
			return err
		}
	}
	if remaining := view.questionTotal - len(view.questions); remaining > 0 {
		if _, err := fmt.Fprintf(writer, "\n- %d more question(s) are available in %s.", remaining, inlineCode("latest.json")); err != nil {
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
	_, err := fmt.Fprintln(writer, "\n\nRequirement IDs, raw model output, and complete scanner results are available in `latest.json`.\n\n</details>")
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
	observations := make(map[string]providers.RepositoryObjectiveObservation)
	add := func(key string, item developerEvidence) {
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}
	if value.RepositoryAnalysis != nil {
		for _, observation := range value.RepositoryAnalysis.Result.ObjectiveObservations {
			rawID := developerRawObjectiveID(observation.ObjectiveID)
			observations[rawID] = observation
			verdict := observation.DerivedTechnicalVerdict()
			if (verdict != providers.RepositoryVerdictImplemented && verdict != providers.RepositoryVerdictPartial) || len(observation.SupportingEvidence) == 0 {
				continue
			}
			add(rawID, developerEvidence{
				title: developerObjectiveTitle(rawID, titles), assessment: developerVerdictAssessment(observation),
				evidence: developerCitationText(observation.SupportingEvidence), verdict: verdict,
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
				if len(observation.SupportingEvidence) > 0 {
					item.evidence = developerCitationText(observation.SupportingEvidence)
				}
			} else if value.RepositoryAnalysis != nil {
				item.verdict = providers.RepositoryVerdictCannotDetermine
				item.assessment = "The deterministic scanner found a code match, but the AI review did not evaluate this safeguard"
			} else {
				item.verdict = providers.RepositoryVerdictCannotDetermine
				item.assessment = "A code match was found; run AI code review for a technical implementation decision"
			}
			add(objective.ID, item)
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
			assessment: "The configured check passed; production behaviour still needs confirmation",
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

func developerVerdictAssessment(observation providers.RepositoryObjectiveObservation) string {
	assessment := developerVerdictLabel(observation.DerivedTechnicalVerdict())
	if rationale := compactMarkdownText(observation.Rationale, 150); rationale != "" {
		assessment += ". " + rationale
	}
	return assessment
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
		add("Are the AI libraries or settings found here actually used in the deployed product, and what data do they process?")
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

func developerRepositoryAnalysisLabel(value Report) string {
	if value.RepositoryAnalysis != nil {
		return fmt.Sprintf("completed using %s", repositoryAnalysisModeLabel(value.RepositoryAnalysis.Coverage.Mode))
	}
	switch value.RepositoryAnalysisRun {
	case RepositoryAnalysisPending:
		return "started; result not available yet"
	case RepositoryAnalysisIncomplete:
		return "attempted but incomplete"
	default:
		if developerRepositoryAnalysisIncomplete(value) {
			return "attempted but incomplete"
		}
		return "not requested"
	}
}

func developerRepositoryAnalysisIncomplete(value Report) bool {
	if value.RepositoryAnalysisRun == RepositoryAnalysisIncomplete {
		return true
	}
	for _, warning := range value.Warnings {
		normalized := strings.ToLower(warning)
		if strings.Contains(normalized, "repository analysis was incomplete") || strings.Contains(normalized, "repository-wide analysis was incomplete") || strings.Contains(normalized, "review was incomplete because model qualification failed") {
			return true
		}
	}
	return false
}
