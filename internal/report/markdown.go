package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

// WriteMarkdown renders a concise, human-readable decision report. Exhaustive
// scanner data remains available in the JSON evidence bundle.
func WriteMarkdown(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintln(writer, "# ComplyScan report"); err != nil {
		return err
	}
	return writeDeveloperReportMarkdown(writer, report)
}

// WriteDetailedMarkdown retains the exhaustive human-readable scanner trace
// for diagnostics and backwards-compatible programmatic use. It is not the
// default latest.md artifact.
func WriteDetailedMarkdown(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintln(writer, "# ComplyScan report"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n> This report identifies technical signals in the repository. It does not determine legal compliance or prove that a control works in production."); err != nil {
		return err
	}
	if err := writeReportOverviewMarkdown(writer, report); err != nil {
		return err
	}
	if report.AIInventory != nil {
		if err := writeAIComponentSummaryMarkdown(writer, *report.AIInventory); err != nil {
			return err
		}
	}
	if report.RepositoryAnalysis != nil {
		if err := writeRepositoryAnalysisMarkdown(writer, *report.RepositoryAnalysis); err != nil {
			return err
		}
	}
	if err := writeTechnicalChecklistMarkdown(writer, report); err != nil {
		return err
	}
	if err := writeRecommendedActionsMarkdown(writer, report); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(writer, "\n<details>\n<summary><strong>Show detailed scanner evidence</strong></summary>\n\n## Detailed scanner evidence\n\nThe sections below preserve locations, scanner reasoning, ownership records, and coverage details for technical review. The JSON report contains the same evidence in machine-readable form."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "\n### Scan metadata\n\n- Scan ID: %s\n- Created: %s\n- Target: %s\n- Tool: ComplyScan %s\n", inlineCode(report.Scan.ID), inlineCode(report.Scan.CreatedAt), inlineCode(report.Target), markdownText(report.Tool.Version)); err != nil {
		return err
	}
	if report.Tool.Commit != "" {
		if _, err := fmt.Fprintf(writer, "- Tool commit: %s\n", inlineCode(report.Tool.Commit)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "- Finding scope: %s\n- Technical evidence scope: %s\n- AI inventory scope: %s\n- Reconciliation scope: %s\n",
		markdownText(report.Scan.Scope.Findings), markdownText(report.Scan.Scope.TechnicalEvidence), markdownText(report.Scan.Scope.AIInventory), markdownText(report.Scan.Scope.Reconciliation)); err != nil {
		return err
	}
	if report.Scan.Scope.ChangedSince != "" {
		if _, err := fmt.Fprintf(writer, "- Changed since: %s\n", inlineCode(report.Scan.Scope.ChangedSince)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(writer, "\n### Deterministic rule findings"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "\n%s\n", markdownFindingSummary(report.Summary)); err != nil {
		return err
	}
	if len(report.Findings) == 0 {
		if _, err := fmt.Fprintln(writer, "\nNo visible deterministic rule findings were reported."); err != nil {
			return err
		}
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(writer, "\n### %s — %s\n\n", strings.ToUpper(markdownText(string(finding.Severity))), markdownText(finding.Title)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "- Rule: %s\n- Confidence: %s\n- Scope: %s\n- Fingerprint: %s\n", inlineCode(finding.RuleID), markdownText(finding.Confidence), markdownText(string(finding.Scope)), inlineCode(finding.Fingerprint)); err != nil {
			return err
		}
		if finding.Path != "" {
			if _, err := fmt.Fprintf(writer, "- Location: %s\n", inlineCode(locationText(finding.Path, finding.StartLine))); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, "\n%s\n", markdownText(finding.Message)); err != nil {
			return err
		}
		if finding.Evidence != "" {
			if _, err := fmt.Fprintf(writer, "\nCandidate evidence: %s\n", inlineCode(finding.Evidence)); err != nil {
				return err
			}
		}
		if finding.Remediation != "" {
			if _, err := fmt.Fprintf(writer, "\nSuggested review: %s\n", markdownText(finding.Remediation)); err != nil {
				return err
			}
		}
	}

	if len(report.Frameworks) == 0 && report.Applicability != nil {
		if _, err := fmt.Fprintln(writer, "\n## Declared applicability context"); err != nil {
			return err
		}
		for _, system := range report.Applicability.Systems {
			if _, err := fmt.Fprintf(writer, "\n### %s\n\n- System ID: %s\n- Automated scope: %s\n- High-risk screening: %s\n- Technical mapping readiness: %s\n", markdownText(system.SystemName), inlineCode(system.SystemID), markdownText(string(system.AutomatedScope)), markdownText(string(system.HighRiskScreening)), markdownText(string(system.MappingReadiness))); err != nil {
				return err
			}
			for _, missing := range system.MissingContext {
				if _, err := fmt.Fprintf(writer, "- Missing context: %s\n", markdownText(missing)); err != nil {
					return err
				}
			}
		}
	}

	if report.AIInventory != nil {
		if err := writeAIInventoryMarkdown(writer, *report.AIInventory); err != nil {
			return err
		}
	}
	for _, result := range report.Frameworks {
		if err := writeFrameworkResultMarkdown(writer, result); err != nil {
			return err
		}
	}

	if len(report.Frameworks) == 0 && report.Reconciliation != nil {
		if err := writeReconciliationMarkdown(writer, *report.Reconciliation); err != nil {
			return err
		}
	}

	if len(report.Frameworks) == 0 && report.TechnicalEvidence != nil {
		if err := writeTechnicalEvidenceMarkdown(writer, *report.TechnicalEvidence); err != nil {
			return err
		}
	}

	if report.Review != nil {
		if _, err := fmt.Fprintf(writer, "\n## Ollama advisory review\n\n- Model: %s\n- Findings reviewed: %d of %d\n", inlineCode(report.Review.Model), report.Review.Reviewed, report.Review.InputFindings); err != nil {
			return err
		}
		for _, observation := range report.Review.Observations {
			if _, err := fmt.Fprintf(writer, "\n### %s\n\n- Verdict: %s\n- Confidence: %s\n\n%s\n", inlineCode(observation.RuleID), markdownText(string(observation.Verdict)), markdownText(observation.Confidence), markdownText(observation.Rationale)); err != nil {
				return err
			}
			if observation.SuggestedAction != "" {
				if _, err := fmt.Fprintf(writer, "\nSuggested review: %s\n", markdownText(observation.SuggestedAction)); err != nil {
					return err
				}
			}
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalReview != nil {
		if _, err := fmt.Fprintf(writer, "\n## Ollama technical evidence investigation\n\n- Model: %s\n- Targets investigated: %d of %d\n- Boundary: repository evidence only; runtime verification and legal acceptance remain separate\n", inlineCode(report.TechnicalReview.Model), report.TechnicalReview.Reviewed, report.TechnicalReview.InputCandidates); err != nil {
			return err
		}
		for _, observation := range report.TechnicalReview.Observations {
			if _, err := fmt.Fprintf(writer, "\n### %s\n\n- System: %s\n- Ownership scope: %s\n- Repository files in scope: %d\n- Evidence fingerprint: %s\n- Investigation mode: %s\n- Prior evidence status: %s\n- Conclusion: %s\n- Assurance level: %s\n- Strength: %s\n- Confidence: %s\n- Runtime verification required: %t\n- Legal review required: %t\n\n%s\n",
				inlineCode(observation.ObjectiveID), markdownTechnicalSystem(observation), markdownText(observation.OwnershipScope), observation.RepositoryFiles,
				inlineCode(observation.EvidenceFingerprint), markdownText(observation.InvestigationMode),
				markdownText(observation.EvidenceStatus), markdownText(string(observation.Conclusion)), markdownText(string(observation.Assurance)),
				markdownText(string(observation.Strength)), markdownText(observation.Confidence), observation.RuntimeVerificationRequired,
				observation.LegalReviewRequired, markdownText(observation.Rationale)); err != nil {
				return err
			}
			for _, claim := range observation.SupportingEvidence {
				if _, err := fmt.Fprintf(writer, "\n- Supporting evidence: %s — %s", inlineCode(locationText(claim.Path, claim.Line)), markdownText(claim.Summary)); err != nil {
					return err
				}
			}
			for _, claim := range observation.ContradictoryEvidence {
				if _, err := fmt.Fprintf(writer, "\n- Contradictory evidence: %s — %s", inlineCode(locationText(claim.Path, claim.Line)), markdownText(claim.Summary)); err != nil {
					return err
				}
			}
			for _, missing := range observation.MissingEvidence {
				if _, err := fmt.Fprintf(writer, "\n- Missing evidence: %s", markdownText(missing)); err != nil {
					return err
				}
			}
			if observation.FollowUpRequested {
				if _, err := fmt.Fprintf(writer, "\n- Bounded follow-up: %d excerpt(s) retrieved from %s", observation.FollowUpExcerpts, markdownText(strings.Join(observation.FollowUpQueries, ", "))); err != nil {
					return err
				}
			}
			if observation.GuardrailNote != "" {
				if _, err := fmt.Fprintf(writer, "\nGuardrail: %s Original model strength: %s.\n", markdownText(observation.GuardrailNote), markdownText(string(observation.ModelStrength))); err != nil {
					return err
				}
			}
			for _, question := range observation.UnresolvedQuestions {
				if _, err := fmt.Fprintf(writer, "\n- Unresolved: %s", markdownText(question)); err != nil {
					return err
				}
			}
			if observation.SuggestedReview != "" {
				if _, err := fmt.Fprintf(writer, "\n\nSuggested review: %s\n", markdownText(observation.SuggestedReview)); err != nil {
					return err
				}
			}
		}
	}
	if len(report.ExecutionVerifications) > 0 {
		if _, err := fmt.Fprintln(writer, "\n## Isolated execution verification"); err != nil {
			return err
		}
	}
	for _, verification := range report.ExecutionVerifications {
		if _, err := fmt.Fprintf(writer, "\n### %s\n\n- Result: %s (exit %d)\n- Runtime and image: %s / %s\n- Command: %s\n- Declared objectives: %s\n- Declared systems: %s\n- Duration: %d ms\n- Output digest: %s\n- Boundary: %s\n",
			inlineCode(verification.RecipeID),
			markdownText(string(verification.Status)), verification.ExitCode, inlineCode(verification.Runtime), inlineCode(verification.Image),
			inlineCode(strings.Join(verification.Command, " ")), markdownText(strings.Join(verification.Objectives, ", ")), markdownText(strings.Join(verification.Systems, ", ")), verification.DurationMS,
			inlineCode(verification.OutputDigest), markdownText(verification.Boundary)); err != nil {
			return err
		}
		if verification.Output != "" {
			if _, err := fmt.Fprintf(writer, "\nBounded, redacted output:\n\n    %s\n", strings.ReplaceAll(verification.Output, "\n", "\n    ")); err != nil {
				return err
			}
		}
	}

	if len(report.Warnings) > 0 {
		if _, err := fmt.Fprintln(writer, "\n## Scan warnings"); err != nil {
			return err
		}
		for _, warning := range report.Warnings {
			if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(warning)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(writer, "\n</details>\n\n---\n\nGenerated from the versioned JSON evidence bundle. Candidate evidence requires technical and human verification.")
	return err
}

func writeRepositoryAnalysisMarkdown(writer io.Writer, analysis providers.RepositoryAnalysisResult) error {
	if _, err := fmt.Fprintf(writer, "\n## Repository-wide AI analysis\n\n- Provider/model: %s / %s\n- Context mode: %s\n- Repository: %d discovered file(s), %d byte(s)\n- Submitted context: %d file submission(s), %d byte(s), %d subsystem(s)\n- Deterministically checked citations: %d\n\nThis is advisory technical reasoning over repository context. It does not determine legal applicability or certify compliance.\n",
		inlineCode(string(analysis.Provider)), inlineCode(analysis.Model), markdownText(string(analysis.Coverage.Mode)), analysis.Coverage.RepositoryFiles,
		analysis.Coverage.RepositoryBytes, analysis.Coverage.FilesSubmitted, analysis.Coverage.BytesSubmitted, analysis.Coverage.Subsystems,
		analysis.Coverage.CitationsChecked); err != nil {
		return err
	}
	if len(analysis.Result.AIUses) == 0 {
		if _, err := fmt.Fprintln(writer, "\nNo AI implementation was identified by this model pass. This is not proof that the repository contains no AI activity."); err != nil {
			return err
		}
	}
	for _, use := range analysis.Result.AIUses {
		if _, err := fmt.Fprintf(writer, "\n### AI use: %s\n\n- ID: %s\n- Purpose: %s\n- Lifecycle indication: %s\n- Confidence: %s\n",
			markdownText(use.Name), inlineCode(use.ID), markdownText(use.Purpose), markdownText(use.Lifecycle), markdownText(use.Confidence)); err != nil {
			return err
		}
		if err := writeRepositoryCitationsMarkdown(writer, "Evidence", use.Evidence); err != nil {
			return err
		}
		for _, question := range use.UnresolvedQuestions {
			if _, err := fmt.Fprintf(writer, "- Unresolved: %s\n", markdownText(question)); err != nil {
				return err
			}
		}
	}
	if len(analysis.Result.ObjectiveObservations) > 0 {
		if _, err := fmt.Fprintln(writer, "\n### Technical-objective mapping"); err != nil {
			return err
		}
	}
	for _, observation := range analysis.Result.ObjectiveObservations {
		if _, err := fmt.Fprintf(writer, "\n#### %s\n\n- Strength: %s\n- Confidence: %s\n", inlineCode(observation.ObjectiveID), markdownText(string(observation.Strength)), markdownText(observation.Confidence)); err != nil {
			return err
		}
		if observation.SystemID != "" {
			if _, err := fmt.Fprintf(writer, "- Configured system: %s\n", inlineCode(observation.SystemID)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, "\n%s\n", markdownText(observation.Rationale)); err != nil {
			return err
		}
		if err := writeRepositoryCitationsMarkdown(writer, "Supporting evidence", observation.SupportingEvidence); err != nil {
			return err
		}
		if err := writeRepositoryCitationsMarkdown(writer, "Contradictory evidence", observation.ContradictoryEvidence); err != nil {
			return err
		}
		for _, missing := range observation.MissingEvidence {
			if _, err := fmt.Fprintf(writer, "- Missing: %s\n", markdownText(missing)); err != nil {
				return err
			}
		}
		for _, question := range observation.UnresolvedQuestions {
			if _, err := fmt.Fprintf(writer, "- Unresolved: %s\n", markdownText(question)); err != nil {
				return err
			}
		}
	}
	if len(analysis.Result.UnmappedObservations) > 0 {
		if _, err := fmt.Fprintln(writer, "\n### AI activity not mapped to a supplied objective"); err != nil {
			return err
		}
	}
	for _, observation := range analysis.Result.UnmappedObservations {
		if _, err := fmt.Fprintf(writer, "\n- **%s** (%s confidence): %s", markdownText(observation.Summary), markdownText(observation.Confidence), markdownText(observation.Reason)); err != nil {
			return err
		}
		if observation.SuggestedReview != "" {
			if _, err := fmt.Fprintf(writer, " Suggested review: %s", markdownText(observation.SuggestedReview)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
		if err := writeRepositoryCitationsMarkdown(writer, "Evidence", observation.Evidence); err != nil {
			return err
		}
	}
	for _, question := range analysis.Result.UnresolvedQuestions {
		if _, err := fmt.Fprintf(writer, "\n- Repository-level unresolved question: %s", markdownText(question)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func writeRepositoryCitationsMarkdown(writer io.Writer, label string, citations []providers.RepositoryCitation) error {
	for _, citation := range citations {
		if _, err := fmt.Fprintf(writer, "- %s: %s — %s\n", markdownText(label), inlineCode(locationText(citation.Path, citation.Line)), markdownText(citation.Summary)); err != nil {
			return err
		}
	}
	return nil
}

type markdownReportCounts struct {
	CandidateEvidence int
	NotDetected       int
	NotEvaluated      int
	SourceFilesSeen   int
	FilesIndexed      int
	UnsupportedFiles  int
}

func writeReportOverviewMarkdown(writer io.Writer, report Report) error {
	counts := markdownCounts(report)
	status := "Completed"
	if len(report.Warnings) > 0 {
		status = "Completed with warnings"
	}
	review := "Not performed — quick technical scan"
	if hasModelReview(report) {
		review = "Performed — advisory AI evidence review included"
	}
	components := 0
	if report.AIInventory != nil {
		components = report.AIInventory.Summary.Components
	}
	if _, err := fmt.Fprintf(writer, "\n## Result at a glance\n\n- Scan status: **%s**\n- AI-related components detected: **%d**\n- Technical objectives with candidate evidence: **%d**\n- Technical objectives with no evidence detected: **%d**\n- Technical objectives not fully assessed: **%d**\n- Source files analysed: **%d of %d**\n- Deterministic findings: **%d** (%d critical, %d high, %d medium, %d low, %d informational)\n- AI review: **%s**\n- Legal applicability: **Not assessed by this technical report**\n",
		status, components, counts.CandidateEvidence, counts.NotDetected, counts.NotEvaluated,
		counts.FilesIndexed, counts.SourceFilesSeen, report.Summary.Total, report.Summary.Critical, report.Summary.High,
		report.Summary.Medium, report.Summary.Low, report.Summary.Info, review); err != nil {
		return err
	}
	_, err := fmt.Fprintln(writer, "\nA detected component or candidate control is a lead for review, not confirmation that it is active, complete, or effective.")
	return err
}

func writeAIComponentSummaryMarkdown(writer io.Writer, value inventory.Report) error {
	if _, err := fmt.Fprintln(writer, "\n## AI-related components"); err != nil {
		return err
	}
	if len(value.Components) == 0 {
		_, err := fmt.Fprintln(writer, "\nNo configured AI provider or framework signal was detected in the bounded scan.")
		return err
	}
	runtimeComponents := make([]string, 0)
	configuredComponents := make([]string, 0)
	testOnlyComponents := make([]string, 0)
	otherComponents := make([]string, 0)
	for _, component := range value.Components {
		hasRuntime := containsInventoryScope(component.Scopes, inventory.ScopeRuntime)
		hasConfig := containsInventoryScope(component.Scopes, inventory.ScopeConfig)
		hasTest := containsInventoryScope(component.Scopes, inventory.ScopeTest)
		if hasRuntime {
			runtimeComponents = append(runtimeComponents, component.Name)
		}
		if hasConfig {
			configuredComponents = append(configuredComponents, component.Name)
		}
		if hasTest && !hasRuntime && !hasConfig {
			testOnlyComponents = append(testOnlyComponents, component.Name)
		}
		if !hasRuntime && !hasConfig && !hasTest {
			otherComponents = append(otherComponents, component.Name)
		}
	}
	writeGroup := func(label string, names []string) error {
		if len(names) == 0 {
			return nil
		}
		_, err := fmt.Fprintf(writer, "\n- %s: **%d** — %s", label, len(names), markdownText(strings.Join(names, ", ")))
		return err
	}
	if err := writeGroup("Runtime-source integrations", runtimeComponents); err != nil {
		return err
	}
	if err := writeGroup("Configuration references", configuredComponents); err != nil {
		return err
	}
	if err := writeGroup("Test-only references", testOnlyComponents); err != nil {
		return err
	}
	if err := writeGroup("Other repository references", otherComponents); err != nil {
		return err
	}
	_, err := fmt.Fprintln(writer, "\n\nRuntime-source integration code is not proof that a provider is enabled or processes production data. Confirm actual deployment and data flows.")
	return err
}

func writeTechnicalChecklistMarkdown(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintln(writer, "\n## Technical checklist"); err != nil {
		return err
	}
	if len(report.Frameworks) == 0 && report.TechnicalEvidence == nil {
		_, err := fmt.Fprintln(writer, "\nNo technical evidence pack was evaluated.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\nThese are code-focused checks from the selected evidence packs. Selecting a pack does not mean that its law or framework applies to the system."); err != nil {
		return err
	}
	for _, result := range report.Frameworks {
		if err := writeOneTechnicalChecklistMarkdown(writer, result.Name, result.TechnicalEvidence); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalEvidence != nil {
		if err := writeOneTechnicalChecklistMarkdown(writer, report.TechnicalEvidence.Pack.Name, *report.TechnicalEvidence); err != nil {
			return err
		}
	}
	return nil
}

func writeModelReviewSummaryMarkdown(writer io.Writer, report Report) error {
	type row struct {
		target, conclusion, confidence, summary string
	}
	rows := make([]row, 0)
	models := make([]string, 0)
	modelSeen := make(map[string]struct{})
	addModel := func(provider providers.Kind, model string) {
		value := string(provider) + " / " + model
		if _, exists := modelSeen[value]; exists {
			return
		}
		modelSeen[value] = struct{}{}
		models = append(models, value)
	}
	if report.Review != nil {
		addModel(report.Review.Provider, report.Review.Model)
		for _, observation := range report.Review.Observations {
			rows = append(rows, row{
				target: observation.RuleID, conclusion: string(observation.Verdict), confidence: observation.Confidence,
				summary: compactMarkdownText(observation.Rationale, 240),
			})
		}
	}
	appendTechnical := func(review providers.TechnicalReviewResult) {
		addModel(review.Provider, review.Model)
		for _, observation := range review.Observations {
			rows = append(rows, row{
				target: observation.ObjectiveID, conclusion: string(observation.Conclusion), confidence: observation.Confidence,
				summary: compactMarkdownText(observation.Rationale, 240),
			})
		}
	}
	if len(report.Frameworks) > 0 {
		for _, result := range report.Frameworks {
			if result.TechnicalReview != nil {
				appendTechnical(*result.TechnicalReview)
			}
		}
	} else if report.TechnicalReview != nil {
		appendTechnical(*report.TechnicalReview)
	}
	if len(models) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(writer, "\n## AI advisory review\n\n- Reviewer: %s\n- Boundary: advisory repository analysis; runtime verification and legal review remain separate\n", markdownText(strings.Join(models, ", "))); err != nil {
		return err
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(writer, "\nThe model returned no review observations.")
		return err
	}
	if _, err := fmt.Fprintln(writer, "\n| Target | Conclusion | Confidence | Summary |\n|---|---|---|---|"); err != nil {
		return err
	}
	for _, value := range rows {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n", markdownTableText(value.target), markdownTableText(value.conclusion), markdownTableText(value.confidence), markdownTableText(value.summary)); err != nil {
			return err
		}
	}
	return nil
}

func writeCompactVerificationMarkdown(writer io.Writer, report Report) error {
	if len(report.ExecutionVerifications) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(writer, "\n## Isolated execution verification\n\n| Recipe | Result | Declared objectives | Boundary |\n|---|---|---|---|"); err != nil {
		return err
	}
	for _, verification := range report.ExecutionVerifications {
		if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s |\n",
			markdownTableText(verification.RecipeID), markdownTableText(string(verification.Status)),
			markdownTableText(strings.Join(verification.Objectives, ", ")), markdownTableText(compactMarkdownText(verification.Boundary, 200))); err != nil {
			return err
		}
	}
	return nil
}

func compactMarkdownText(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func writeOneTechnicalChecklistMarkdown(writer io.Writer, name string, evidence framework.TechnicalEvidenceReport) error {
	if _, err := fmt.Fprintf(writer, "\n### %s\n", markdownText(name)); err != nil {
		return err
	}
	actionable := make([]framework.ObjectiveAssessment, 0)
	notDetectedBySource := make(map[string][]string)
	sourceOrder := make([]string, 0)
	for _, objective := range evidence.Objectives {
		if objective.Status != framework.ObjectiveNotDetected {
			actionable = append(actionable, objective)
			continue
		}
		if _, exists := notDetectedBySource[objective.SourceReference]; !exists {
			sourceOrder = append(sourceOrder, objective.SourceReference)
		}
		notDetectedBySource[objective.SourceReference] = append(notDetectedBySource[objective.SourceReference], objective.Title)
	}
	if len(actionable) > 0 {
		if _, err := fmt.Fprintln(writer, "\n| Source | Objective requiring review | Result | Evidence |\n|---|---|---|---|"); err != nil {
			return err
		}
		for _, objective := range actionable {
			result, found, _ := describeObjective(objective)
			if _, err := fmt.Fprintf(writer, "| %s | %s | **%s** | %s |\n",
				markdownTableText(objective.SourceReference), markdownTableText(objective.Title), markdownTableText(result), markdownTableText(found)); err != nil {
				return err
			}
		}
	}
	if len(sourceOrder) > 0 {
		if _, err := fmt.Fprintf(writer, "\n**No evidence detected for %d objective(s):**\n", evidence.Summary.NotDetected); err != nil {
			return err
		}
		for _, source := range sourceOrder {
			if _, err := fmt.Fprintf(writer, "\n- **%s:** %s", markdownText(source), markdownText(strings.Join(notDetectedBySource[source], "; "))); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer, "\n\nFirst decide which objectives are relevant. A missing signal does not prove that a control is absent."); err != nil {
			return err
		}
	}
	return nil
}

func writeRecommendedActionsMarkdown(writer io.Writer, report Report) error {
	counts := markdownCounts(report)
	if _, err := fmt.Fprintln(writer, "\n## Recommended next actions"); err != nil {
		return err
	}
	actions := make([]string, 0, 5)
	if report.AIInventory != nil && report.AIInventory.Summary.Components > 0 {
		actions = append(actions, "Confirm which detected AI components are actually enabled in deployed environments and what data they process.")
	}
	if counts.CandidateEvidence > 0 {
		actions = append(actions, fmt.Sprintf("Review the %d technical objective(s) with candidate evidence and confirm reachability, production use, completeness, and effectiveness.", counts.CandidateEvidence))
	}
	if counts.NotDetected > 0 {
		actions = append(actions, fmt.Sprintf("Decide which of the %d no-evidence objective(s) are relevant before implementing anything; no detected signal does not prove a control is absent.", counts.NotDetected))
	}
	if counts.NotEvaluated > 0 {
		actions = append(actions, fmt.Sprintf("Manually review the unsupported files identified for the %d objective(s) that could not be fully assessed.", counts.NotEvaluated))
	}
	if !hasModelReview(report) && counts.CandidateEvidence > 0 {
		actions = append(actions, "Configure an AI reviewer in `complyscan setup`, then rerun `complyscan scan` to add advisory semantic review of the candidate evidence.")
	}
	if len(actions) == 0 {
		actions = append(actions, "Review the detailed evidence and preserve the JSON bundle for future comparison.")
	}
	for index, action := range actions {
		if _, err := fmt.Fprintf(writer, "\n%d. %s", index+1, markdownText(action)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func markdownCounts(report Report) markdownReportCounts {
	counts := markdownReportCounts{}
	add := func(evidence framework.TechnicalEvidenceReport) {
		counts.CandidateEvidence += evidence.Summary.CandidateEvidence
		counts.NotDetected += evidence.Summary.NotDetected
		counts.NotEvaluated += evidence.Summary.NotEvaluated
		if evidence.Analysis.SourceFilesSeen > counts.SourceFilesSeen {
			counts.SourceFilesSeen = evidence.Analysis.SourceFilesSeen
		}
		if evidence.Analysis.FilesIndexed > counts.FilesIndexed {
			counts.FilesIndexed = evidence.Analysis.FilesIndexed
		}
		if len(evidence.Analysis.UnsupportedSourceFiles) > counts.UnsupportedFiles {
			counts.UnsupportedFiles = len(evidence.Analysis.UnsupportedSourceFiles)
		}
	}
	if len(report.Frameworks) > 0 {
		for _, result := range report.Frameworks {
			add(result.TechnicalEvidence)
		}
	} else if report.TechnicalEvidence != nil {
		add(*report.TechnicalEvidence)
	}
	return counts
}

func hasModelReview(report Report) bool {
	if report.Review != nil || report.TechnicalReview != nil {
		return true
	}
	for _, result := range report.Frameworks {
		if result.TechnicalReview != nil {
			return true
		}
	}
	return false
}

func containsInventoryScope(scopes []inventory.Scope, wanted inventory.Scope) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}

func describeObjective(objective framework.ObjectiveAssessment) (string, string, string) {
	switch objective.Status {
	case framework.ObjectiveCandidate:
		locations := make([]string, 0, 2)
		for index, match := range objective.Matches {
			if index == 2 {
				break
			}
			locations = append(locations, locationText(match.Path, match.StartLine))
		}
		found := strings.Join(locations, ", ")
		if remaining := len(objective.Matches) - len(locations); remaining > 0 {
			found += fmt.Sprintf(" and %d more location(s)", remaining)
		}
		return "Candidate evidence", found, "Confirm that this code is active, complete, and effective in production."
	case framework.ObjectiveNotEvaluated:
		found := "A potentially relevant file could not be analysed by a supported language analyser."
		if len(objective.UnresolvedQuestions) > 0 {
			found = objective.UnresolvedQuestions[0]
		}
		return "Not fully assessed", found, "Review the named unsupported file manually or add analyzer support."
	default:
		return "No evidence detected", "No configured technical signal was found in the supported files scanned.", "Decide whether this objective applies; implement or document it only if relevant."
	}
}

func markdownTableText(value string) string {
	return strings.ReplaceAll(markdownText(value), "|", "\\|")
}

func writeFrameworkResultMarkdown(writer io.Writer, result FrameworkResult) error {
	if _, err := fmt.Fprintf(writer, "\n## Framework: %s\n\n- Framework ID: %s\n- Nature: %s\n", markdownText(result.Name), inlineCode(result.ID), markdownText(result.Nature)); err != nil {
		return err
	}
	if result.Applicability != nil {
		if _, err := fmt.Fprintln(writer, "\n### Declared applicability context"); err != nil {
			return err
		}
		for _, system := range result.Applicability.Systems {
			if _, err := fmt.Fprintf(writer, "\n- %s (%s): scope %s; high-risk screening %s; technical mapping readiness **%s**\n",
				markdownText(system.SystemName), inlineCode(system.SystemID), markdownText(string(system.AutomatedScope)), markdownText(string(system.HighRiskScreening)), markdownText(string(system.MappingReadiness))); err != nil {
				return err
			}
			for _, missing := range system.MissingContext {
				if _, err := fmt.Fprintf(writer, "  - Unresolved fact: %s\n", markdownText(missing)); err != nil {
					return err
				}
			}
		}
	}
	if err := writeReconciliationMarkdown(writer, result.Reconciliation); err != nil {
		return err
	}
	if err := writeTechnicalEvidenceMarkdown(writer, result.TechnicalEvidence); err != nil {
		return err
	}
	if result.TechnicalReview != nil {
		return writeFrameworkTechnicalReviewMarkdown(writer, *result.TechnicalReview)
	}
	return nil
}

func writeFrameworkTechnicalReviewMarkdown(writer io.Writer, review providers.TechnicalReviewResult) error {
	if _, err := fmt.Fprintf(writer, "\n### Model evidence investigation\n\n- Model: %s\n- Targets investigated: %d of %d\n- Boundary: repository evidence only; runtime verification and legal acceptance remain separate\n", inlineCode(review.Model), review.Reviewed, review.InputCandidates); err != nil {
		return err
	}
	for _, observation := range review.Observations {
		if _, err := fmt.Fprintf(writer, "\n#### %s\n\n- System: %s\n- Ownership scope: %s\n- Repository files in scope: %d\n- Evidence fingerprint: %s\n- Investigation mode: %s\n- Prior evidence status: %s\n- Conclusion: %s\n- Assurance level: %s\n- Strength: %s\n- Confidence: %s\n- Runtime verification required: %t\n- Legal review required: %t\n\n%s\n",
			inlineCode(observation.ObjectiveID), markdownTechnicalSystem(observation), markdownText(observation.OwnershipScope), observation.RepositoryFiles,
			inlineCode(observation.EvidenceFingerprint), markdownText(observation.InvestigationMode), markdownText(observation.EvidenceStatus),
			markdownText(string(observation.Conclusion)), markdownText(string(observation.Assurance)), markdownText(string(observation.Strength)),
			markdownText(observation.Confidence), observation.RuntimeVerificationRequired, observation.LegalReviewRequired, markdownText(observation.Rationale)); err != nil {
			return err
		}
		for _, claim := range observation.SupportingEvidence {
			if _, err := fmt.Fprintf(writer, "\n- Supporting evidence: %s — %s", inlineCode(locationText(claim.Path, claim.Line)), markdownText(claim.Summary)); err != nil {
				return err
			}
		}
		for _, claim := range observation.ContradictoryEvidence {
			if _, err := fmt.Fprintf(writer, "\n- Contradictory evidence: %s — %s", inlineCode(locationText(claim.Path, claim.Line)), markdownText(claim.Summary)); err != nil {
				return err
			}
		}
		for _, missing := range observation.MissingEvidence {
			if _, err := fmt.Fprintf(writer, "\n- Missing evidence: %s", markdownText(missing)); err != nil {
				return err
			}
		}
		for _, question := range observation.UnresolvedQuestions {
			if _, err := fmt.Fprintf(writer, "\n- Unresolved: %s", markdownText(question)); err != nil {
				return err
			}
		}
		if observation.SuggestedReview != "" {
			if _, err := fmt.Fprintf(writer, "\n\nSuggested review: %s\n", markdownText(observation.SuggestedReview)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAIInventoryMarkdown(writer io.Writer, value inventory.Report) error {
	if _, err := fmt.Fprintf(writer, "\n## Independently observed AI components\n\n- Components: %d\n- Technical signals: %d\n- Runtime signals: %d\n- Test signals: %d\n- Configuration signals: %d\n",
		value.Summary.Components, value.Summary.Signals, value.Summary.RuntimeSignals, value.Summary.TestSignals, value.Summary.ConfigurationSignals); err != nil {
		return err
	}
	if len(value.Components) == 0 {
		_, err := fmt.Fprintln(writer, "\nNo configured AI provider or framework signal was detected in the bounded scan.")
		return err
	}
	for _, component := range value.Components {
		if _, err := fmt.Fprintf(writer, "\n### %s\n\n- Kind: %s\n- Confidence: %s\n- Occurrences: %d\n",
			markdownText(component.Name), markdownText(string(component.Kind)), markdownText(component.Confidence), component.Occurrences); err != nil {
			return err
		}
		for _, location := range component.Locations {
			if _, err := fmt.Fprintf(writer, "- Location: %s — %s scope, %s\n", inlineCode(locationText(location.Path, location.Line)), markdownText(string(location.Scope)), markdownText(string(location.EvidenceType))); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeReconciliationMarkdown(writer io.Writer, value reconciliation.Report) error {
	configured := "no"
	if value.Ownership.Configured {
		configured = "yes"
	}
	if _, err := fmt.Fprintf(writer, "\n## Requirement-to-evidence reconciliation\n\n- Mapping version: %s\n- Path ownership configured: %s (%d rules)\n- Assigned evidence references: %d\n- Intentionally shared evidence references: %d\n- Conflicting evidence references: %d\n- Unassigned evidence references: %d\n- Single-system inferred references: %d\n- Likely required objectives: %d\n- Voluntary-framework recommended practices: %d\n- With candidate evidence: %d\n- Without detected evidence: %d\n- Configuration/evidence mismatches: %d\n- Unresolved results: %d\n- Evidence groups with unresolved ownership: %d\n- AI-substantiated objectives: %d\n- Structurally verified objectives: %d\n- Objectives with isolated test evidence: %d\n- Extended investigations with no evidence: %d\n- Unresolved investigations: %d\n",
		inlineCode(value.MappingVersion), configured, len(value.Ownership.Rules), value.Summary.AssignedReferences,
		value.Summary.SharedReferences, value.Summary.ConflictingReferences, value.Summary.UnassignedReferences, value.Summary.InferredReferences,
		value.Summary.LikelyRequired, value.Summary.Recommended, value.Summary.RequirementWithEvidence,
		value.Summary.RequirementWithoutEvidence, value.Summary.EvidenceMismatches, value.Summary.Unresolved, value.Summary.UnmappedEvidence,
		value.Summary.AISubstantiated, value.Summary.StructurallyVerified, value.Summary.TestEvidenceObserved, value.Summary.InvestigationNoEvidence, value.Summary.InvestigationUnresolved); err != nil {
		return err
	}
	if len(value.Systems) == 0 {
		if _, err := fmt.Fprintln(writer, "\nNo system profile was declared, so repository evidence cannot yet be reconciled with applicability requirements."); err != nil {
			return err
		}
	}
	for _, system := range value.Systems {
		if _, err := fmt.Fprintf(writer, "\n### %s\n\nSystem ID: %s\n\n| Provision | Technical objective | Requirement | Evidence | Reconciliation | AI assurance | Test assurance |\n|---|---|---|---|---|---|---|\n",
			markdownText(system.SystemName), inlineCode(system.SystemID)); err != nil {
			return err
		}
		for _, objective := range system.Objectives {
			assurance := "—"
			if objective.Investigation != nil {
				assurance = string(objective.Investigation.Assurance)
			}
			testAssurance := "—"
			if objective.Verification != nil {
				testAssurance = fmt.Sprintf("%s (%d passed, %d failed)", objective.Verification.Assurance, objective.Verification.Passed, objective.Verification.Failed)
			}
			if _, err := fmt.Fprintf(writer, "| %s | %s | %s | %s | %s | %s | %s |\n",
				markdownText(objective.SourceReference), markdownText(objective.Title), markdownText(string(objective.Requirement)),
				markdownText(string(objective.Evidence)), markdownText(string(objective.Mapping)), markdownText(assurance), markdownText(testAssurance)); err != nil {
				return err
			}
		}
		for _, objective := range system.Objectives {
			if objective.Investigation == nil || objective.Investigation.SystemID == "" {
				continue
			}
			if _, err := fmt.Fprintf(writer, "\n- Investigation scope for %s: %s ownership for system %s across %d repository file(s).\n",
				inlineCode(objective.ObjectiveID), markdownText(objective.Investigation.OwnershipScope),
				inlineCode(objective.Investigation.SystemID), objective.Investigation.RepositoryFiles); err != nil {
				return err
			}
		}
		if hasSystemEvidenceReferences(system) {
			if _, err := fmt.Fprintln(writer, "\nEvidence references attributed to this system:"); err != nil {
				return err
			}
			for _, objective := range system.Objectives {
				for _, reference := range objective.EvidenceReferences {
					if _, err := fmt.Fprintf(writer, "\n- %s: %s — %s", inlineCode(objective.ObjectiveID), inlineCode(locationText(reference.Path, reference.Line)), markdownText(ownershipReferenceText(reference))); err != nil {
						return err
					}
				}
			}
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if len(system.ObservedComponents) > 0 {
			if _, err := fmt.Fprintln(writer, "\nAssociated AI components:"); err != nil {
				return err
			}
			for _, component := range system.ObservedComponents {
				if _, err := fmt.Fprintf(writer, "\n- %s (%s): %s", markdownText(component.Name), markdownText(string(component.Kind)), markdownText(string(component.Mapping))); err != nil {
					return err
				}
				for _, reference := range component.Locations {
					if _, err := fmt.Fprintf(writer, "\n  - %s — %s", inlineCode(locationText(reference.Path, reference.Line)), markdownText(ownershipReferenceText(reference))); err != nil {
						return err
					}
				}
			}
			if _, err := fmt.Fprintln(writer); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer, "\nResults requiring attention:"); err != nil {
			return err
		}
		attention := 0
		for _, objective := range system.Objectives {
			if !showReconciliationReason(objective.Mapping) {
				continue
			}
			attention++
			if _, err := fmt.Fprintf(writer, "\n- %s — %s", inlineCode(objective.ObjectiveID), markdownText(string(objective.Mapping))); err != nil {
				return err
			}
			for _, reason := range objective.Reasons {
				if _, err := fmt.Fprintf(writer, "\n  - %s: %s", inlineCode(reason.Code), markdownText(reason.Message)); err != nil {
					return err
				}
			}
		}
		if attention == 0 {
			if _, err := fmt.Fprint(writer, " none"); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	if len(value.Unmapped) > 0 {
		if _, err := fmt.Fprintln(writer, "\n### Repository evidence with unresolved ownership"); err != nil {
			return err
		}
		for _, evidence := range value.Unmapped {
			if _, err := fmt.Fprintf(writer, "\n- %s %s — %s (%s)", markdownText(string(evidence.Kind)), inlineCode(evidence.Title), markdownText(evidence.Reason.Message), inlineCode(evidence.Reason.Code)); err != nil {
				return err
			}
			for _, reference := range evidence.References {
				if _, err := fmt.Fprintf(writer, "\n  - %s — %s", inlineCode(locationText(reference.Path, reference.Line)), markdownText(ownershipReferenceText(reference))); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	return nil
}

func hasSystemEvidenceReferences(system reconciliation.SystemResult) bool {
	for _, objective := range system.Objectives {
		if len(objective.EvidenceReferences) > 0 {
			return true
		}
	}
	return false
}

func markdownTechnicalSystem(observation providers.TechnicalObservation) string {
	if observation.SystemID == "" {
		return "repository-wide / unassigned"
	}
	if observation.SystemName == "" {
		return inlineCode(observation.SystemID)
	}
	return markdownText(observation.SystemName) + " (" + inlineCode(observation.SystemID) + ")"
}

func writeTechnicalEvidenceMarkdown(writer io.Writer, evidence framework.TechnicalEvidenceReport) error {
	if _, err := fmt.Fprintf(writer, "\n### Technical evidence: %s\n\n- Pack: %s at version %s\n- Pack digest: %s\n- Source: [%s](%s)\n- Checks with candidate evidence: %d\n- Checks with no evidence detected: %d\n- Checks not evaluated: %d\n",
		markdownText(evidence.Pack.Name),
		inlineCode(evidence.Pack.ID), inlineCode(evidence.Pack.Version), inlineCode(evidence.Pack.Digest),
		markdownText(evidence.Source.Reference), evidence.Source.URL,
		evidence.Summary.CandidateEvidence, evidence.Summary.NotDetected, evidence.Summary.NotEvaluated); err != nil {
		return err
	}
	languages := make([]string, len(evidence.Analysis.Languages))
	for index, language := range evidence.Analysis.Languages {
		languages[index] = string(language)
	}
	if len(languages) == 0 {
		languages = []string{"none"}
	}
	if _, err := fmt.Fprintf(writer, "\n### Repository analysis\n\n- Source files seen: %d\n- Files indexed: %d\n- Indexed languages: %s\n- Symbols indexed: %d\n- Relationships indexed: %d\n- Unsupported source files: %d\n",
		evidence.Analysis.SourceFilesSeen, evidence.Analysis.FilesIndexed, inlineCode(strings.Join(languages, ", ")),
		evidence.Analysis.SymbolsIndexed, evidence.Analysis.RelationshipsIndexed, len(evidence.Analysis.UnsupportedSourceFiles)); err != nil {
		return err
	}
	for _, path := range evidence.Analysis.UnsupportedSourceFiles {
		if _, err := fmt.Fprintf(writer, "  - %s\n", inlineCode(path)); err != nil {
			return err
		}
	}
	currentReference := ""
	for _, objective := range evidence.Objectives {
		if objective.SourceReference != currentReference {
			currentReference = objective.SourceReference
			if _, err := fmt.Fprintf(writer, "\n### %s\n", markdownText(currentReference)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(writer, "\n#### %s — %s\n\n- Objective: %s\n- Verification: %s\n\n%s\n",
			objectiveStatusLabel(objective.Status), markdownText(objective.Title), inlineCode(objective.ID), markdownText(objective.Verification), markdownText(objective.Description)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "\n- Applicability conditions: %s\n", markdownText(framework.DescribeApplicability(objective.Applicability))); err != nil {
			return err
		}
		if objective.ApplicabilityNote != "" {
			if _, err := fmt.Fprintf(writer, "\nApplicability note: %s\n", markdownText(objective.ApplicabilityNote)); err != nil {
				return err
			}
		}
		for _, question := range objective.UnresolvedQuestions {
			if _, err := fmt.Fprintf(writer, "\nUnresolved: %s\n", markdownText(question)); err != nil {
				return err
			}
		}
		if len(objective.Matches) == 0 {
			if _, err := fmt.Fprintln(writer, "\nNo configured technical signal was detected in the bounded scan."); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(writer, "\nCandidate locations:"); err != nil {
			return err
		}
		for _, match := range objective.Matches {
			if _, err := fmt.Fprintf(writer, "\n- %s — matched %s", inlineCode(locationText(match.Path, match.StartLine)), inlineCode(strings.Join(match.MatchedTerms, ", "))); err != nil {
				return err
			}
			if match.Context.Anchor != nil {
				if _, err := fmt.Fprintf(writer, "\n  - Anchor: %s (%s)", inlineCode(match.Context.Anchor.QualifiedName), inlineCode(string(match.Context.Anchor.Reachability))); err != nil {
					return err
				}
			}
			for _, relationship := range match.Context.Relationships {
				if _, err := fmt.Fprintf(writer, "\n  - Relationship: %s — %s → %s", inlineCode(string(relationship.Kind)), inlineCode(relationship.From), inlineCode(relationship.To)); err != nil {
					return err
				}
				if relationship.Label != "" {
					if _, err := fmt.Fprintf(writer, " (%s)", inlineCode(relationship.Label)); err != nil {
						return err
					}
				}
			}
			for _, question := range match.Context.UnresolvedQuestions {
				if _, err := fmt.Fprintf(writer, "\n  - Unresolved: %s", markdownText(question)); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer, "\n### Coverage boundary"); err != nil {
		return err
	}
	for _, limitation := range evidence.Coverage.Limitations {
		if _, err := fmt.Fprintf(writer, "\n- %s", markdownText(limitation)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func markdownFindingSummary(summary Summary) string {
	label := "findings"
	if summary.Total == 1 {
		label = "finding"
	}
	return fmt.Sprintf("**%d %s:** %d critical, %d high, %d medium, %d low, %d info.", summary.Total, label, summary.Critical, summary.High, summary.Medium, summary.Low, summary.Info)
}

func objectiveStatusLabel(status framework.ObjectiveStatus) string {
	switch status {
	case framework.ObjectiveCandidate:
		return "Candidate evidence detected"
	case framework.ObjectiveNotEvaluated:
		return "Not evaluated"
	default:
		return "No evidence detected"
	}
}

func locationText(path string, line int) string {
	if line > 0 {
		return fmt.Sprintf("%s:%d", path, line)
	}
	return path
}

func markdownText(value string) string {
	value = strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")), " ")
	replacer := strings.NewReplacer(
		"\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]",
		"<", "&lt;", ">", "&gt;", "#", "\\#", "|", "\\|",
	)
	return replacer.Replace(value)
}

func inlineCode(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " ")
	if strings.Contains(value, "`") {
		return "`` " + strings.ReplaceAll(value, "``", "` `") + " ``"
	}
	return "`" + value + "`"
}
