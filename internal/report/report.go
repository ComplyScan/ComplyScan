package report

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/rules"
	"github.com/ComplyScan/ComplyScan/internal/usemapping"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	BuiltAt string `json:"built_at,omitempty"`
}

type ScanMetadata struct {
	ID        string    `json:"id"`
	CreatedAt string    `json:"created_at"`
	Scope     ScanScope `json:"scope"`
}

type ScanScope struct {
	Findings                  string `json:"findings"`
	TechnicalEvidence         string `json:"technical_evidence"`
	AIInventory               string `json:"ai_inventory"`
	Reconciliation            string `json:"reconciliation"`
	AIReview                  string `json:"ai_review,omitempty"`
	AIReviewFiles             int    `json:"ai_review_files,omitempty"`
	AIReviewChangedFiles      int    `json:"ai_review_changed_files,omitempty"`
	AIReviewConnectedFiles    int    `json:"ai_review_connected_files,omitempty"`
	ChangedSince              string `json:"changed_since,omitempty"`
	TrackedOnly               bool   `json:"tracked_only"`
	IncludeNestedRepositories bool   `json:"include_nested_repositories"`
}

type Summary struct {
	Total    int `json:"total"`
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}

// RepositoryAnalysisRunStatus records the lifecycle of the optional
// repository model pass independently from any returned analysis payload.
type RepositoryAnalysisRunStatus string

const (
	RepositoryAnalysisNotRequested RepositoryAnalysisRunStatus = "not-requested"
	RepositoryAnalysisPending      RepositoryAnalysisRunStatus = "pending"
	RepositoryAnalysisIncomplete   RepositoryAnalysisRunStatus = "incomplete"
	RepositoryAnalysisCompleted    RepositoryAnalysisRunStatus = "completed"
)

type Report struct {
	SchemaVersion          int                                 `json:"schema_version"`
	Tool                   Tool                                `json:"tool"`
	Scan                   ScanMetadata                        `json:"scan"`
	Target                 string                              `json:"target"`
	Summary                Summary                             `json:"summary"`
	Findings               []rules.Finding                     `json:"findings"`
	Warnings               []string                            `json:"warnings,omitempty"`
	Suppressed             int                                 `json:"suppressed"`
	Applicability          *profile.AssessmentReport           `json:"applicability,omitempty"`
	TechnicalEvidence      *framework.TechnicalEvidenceReport  `json:"technical_evidence,omitempty"`
	AIInventory            *inventory.Report                   `json:"ai_inventory,omitempty"`
	AIUseInventory         *aiuse.Snapshot                     `json:"ai_use_inventory,omitempty"`
	AIUseMappings          *usemapping.Report                  `json:"ai_use_mappings,omitempty"`
	Reconciliation         *reconciliation.Report              `json:"reconciliation,omitempty"`
	Review                 *providers.ReviewResult             `json:"review,omitempty"`
	RepositoryAnalysis     *providers.RepositoryAnalysisResult `json:"repository_analysis,omitempty"`
	RepositoryAnalysisRun  RepositoryAnalysisRunStatus         `json:"repository_analysis_run"`
	TechnicalReview        *providers.TechnicalReviewResult    `json:"evidence_investigation,omitempty"`
	ExecutionVerifications []verification.Report               `json:"execution_verification,omitempty"`
	Frameworks             []FrameworkResult                   `json:"frameworks,omitempty"`
}

// FrameworkResult keeps each framework's applicability, technical evidence,
// reconciliation, and optional model investigation together. The legacy
// singular fields remain populated for the primary EU pack during migration.
type FrameworkResult struct {
	ID                string                            `json:"id"`
	Name              string                            `json:"name"`
	Nature            string                            `json:"nature"`
	Applicability     *profile.AssessmentReport         `json:"applicability,omitempty"`
	TechnicalEvidence framework.TechnicalEvidenceReport `json:"technical_evidence"`
	Reconciliation    reconciliation.Report             `json:"reconciliation"`
	TechnicalReview   *providers.TechnicalReviewResult  `json:"evidence_investigation,omitempty"`
}

type TerminalOptions struct {
	Color bool
}

func New(target, version string, findings []rules.Finding, warnings []string, suppressed int) Report {
	return NewWithMetadata(
		target,
		Tool{Name: "ComplyScan", Version: version},
		ScanScope{Findings: "full-repository", TechnicalEvidence: "full-repository", AIInventory: "full-repository", Reconciliation: "full-repository"},
		time.Now(),
		findings,
		warnings,
		suppressed,
	)
}

func NewWithMetadata(target string, tool Tool, scope ScanScope, createdAt time.Time, findings []rules.Finding, warnings []string, suppressed int) Report {
	if findings == nil {
		findings = []rules.Finding{}
	}
	if scope.AIInventory == "" {
		scope.AIInventory = "full-repository"
	}
	if scope.Reconciliation == "" {
		scope.Reconciliation = "full-repository"
	}
	created := createdAt.UTC().Format(time.RFC3339Nano)
	identifier := sha256.Sum256([]byte(strings.Join([]string{target, tool.Version, tool.Commit, created}, "\x00")))
	return Report{
		SchemaVersion:         11,
		RepositoryAnalysisRun: RepositoryAnalysisNotRequested,
		Tool:                  tool,
		Scan: ScanMetadata{
			ID: "scan-" + fmt.Sprintf("%x", identifier[:12]), CreatedAt: created, Scope: scope,
		},
		Target:  target,
		Summary: Summarize(findings), Findings: findings, Warnings: warnings, Suppressed: suppressed,
	}
}

func Summarize(findings []rules.Finding) Summary {
	summary := Summary{Total: len(findings)}
	for _, finding := range findings {
		switch finding.Severity {
		case rules.SeverityCritical:
			summary.Critical++
		case rules.SeverityHigh:
			summary.High++
		case rules.SeverityMedium:
			summary.Medium++
		case rules.SeverityLow:
			summary.Low++
		case rules.SeverityInfo:
			summary.Info++
		}
	}
	return summary
}

func FilterByMinimum(findings []rules.Finding, minimum rules.Severity) []rules.Finding {
	filtered := make([]rules.Finding, 0, len(findings))
	for _, finding := range findings {
		if rules.SeverityRank(finding.Severity) >= rules.SeverityRank(minimum) {
			filtered = append(filtered, finding)
		}
	}
	return filtered
}

func MeetsThreshold(findings []rules.Finding, threshold rules.Severity) bool {
	for _, finding := range findings {
		if rules.SeverityRank(finding.Severity) >= rules.SeverityRank(threshold) {
			return true
		}
	}
	return false
}

func WriteJSON(w io.Writer, report Report) error {
	if report.SchemaVersion >= 7 && report.RepositoryAnalysisRun == "" {
		report.RepositoryAnalysisRun = RepositoryAnalysisNotRequested
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	return nil
}

func WriteTerminal(w io.Writer, report Report, options TerminalOptions) error {
	if _, err := fmt.Fprintf(w, "ComplyScan found %d potential %s\n\n", report.Summary.Total, issueWord(report.Summary.Total)); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if err := WriteTerminalFinding(w, finding, options); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.Applicability != nil {
		if err := profile.WriteTerminal(w, *report.Applicability); err != nil {
			return err
		}
	}
	if report.AIInventory != nil {
		if err := writeAIInventoryTerminal(w, *report.AIInventory); err != nil {
			return err
		}
	}
	if report.AIUseInventory != nil {
		if err := writeAIUseInventoryTerminal(w, *report.AIUseInventory); err != nil {
			return err
		}
	}
	if report.AIUseMappings != nil {
		if err := writeAIUseMappingsTerminal(w, *report.AIUseMappings); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.Reconciliation != nil {
		if err := writeReconciliationTerminal(w, *report.Reconciliation); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalEvidence != nil {
		if err := framework.WriteTechnicalEvidenceTerminal(w, *report.TechnicalEvidence); err != nil {
			return err
		}
	}
	if report.Review != nil {
		if err := WriteTerminalReview(w, *report.Review); err != nil {
			return err
		}
	}
	if report.RepositoryAnalysis != nil {
		if err := WriteTerminalRepositoryAnalysis(w, *report.RepositoryAnalysis); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalReview != nil {
		if err := WriteTerminalTechnicalReview(w, *report.TechnicalReview); err != nil {
			return err
		}
	}
	if err := writeFrameworkResultsTerminal(w, report.Frameworks); err != nil {
		return err
	}
	for _, executionVerification := range report.ExecutionVerifications {
		if err := WriteTerminalExecutionVerification(w, executionVerification); err != nil {
			return err
		}
	}
	return writeTerminalSummary(w, report)
}

// WriteTerminalFinding renders one finding immediately for streaming scans.
func WriteTerminalFinding(w io.Writer, finding rules.Finding, options TerminalOptions) error {
	label := severityLabel(finding.Severity)
	if options.Color {
		label = colorize(finding.Severity, label)
	}
	if _, err := fmt.Fprintf(w, "%-5s  %s  %s\n", label, finding.RuleID, finding.Title); err != nil {
		return err
	}
	if finding.Path != "" {
		location := finding.Path
		if finding.StartLine > 0 {
			location += ":" + strconv.Itoa(finding.StartLine)
		}
		if _, err := fmt.Fprintf(w, "       %s\n", location); err != nil {
			return err
		}
	}
	if finding.Scope != "" {
		if _, err := fmt.Fprintf(w, "       Scope: %s\n", finding.Scope); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "       %s\n", finding.Message); err != nil {
		return err
	}
	if finding.Evidence != "" {
		if _, err := fmt.Fprintf(w, "       Evidence: %s\n", finding.Evidence); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// WriteTerminalCompletion closes a streaming report with final counts.
func WriteTerminalCompletion(w io.Writer, report Report) error {
	if len(report.Frameworks) == 0 && report.Applicability != nil {
		if err := profile.WriteTerminal(w, *report.Applicability); err != nil {
			return err
		}
	}
	if report.AIInventory != nil {
		if err := writeAIInventoryTerminal(w, *report.AIInventory); err != nil {
			return err
		}
	}
	if report.AIUseInventory != nil {
		if err := writeAIUseInventoryTerminal(w, *report.AIUseInventory); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.Reconciliation != nil {
		if err := writeReconciliationTerminal(w, *report.Reconciliation); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalEvidence != nil {
		if err := framework.WriteTechnicalEvidenceTerminal(w, *report.TechnicalEvidence); err != nil {
			return err
		}
	}
	if report.Review != nil {
		if err := WriteTerminalReview(w, *report.Review); err != nil {
			return err
		}
	}
	if report.RepositoryAnalysis != nil {
		if err := WriteTerminalRepositoryAnalysis(w, *report.RepositoryAnalysis); err != nil {
			return err
		}
	}
	if len(report.Frameworks) == 0 && report.TechnicalReview != nil {
		if err := WriteTerminalTechnicalReview(w, *report.TechnicalReview); err != nil {
			return err
		}
	}
	if err := writeFrameworkResultsTerminal(w, report.Frameworks); err != nil {
		return err
	}
	for _, executionVerification := range report.ExecutionVerifications {
		if err := WriteTerminalExecutionVerification(w, executionVerification); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Scan complete: %d potential %s\n", report.Summary.Total, issueWord(report.Summary.Total)); err != nil {
		return err
	}
	return writeTerminalSummary(w, report)
}

// WriteTerminalConciseCompletion closes a streaming scan without repeating the
// detailed evidence already saved in the Markdown and JSON artifacts.
func WriteTerminalConciseCompletion(w io.Writer, value Report) error {
	if _, err := fmt.Fprintf(w, "Scan complete: %d potential %s\n", value.Summary.Total, issueWord(value.Summary.Total)); err != nil {
		return err
	}
	if err := writeTerminalSummary(w, value); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Analysis: %s\n", developerAnalysisSummaryLabel(value)); err != nil {
		return err
	}
	if value.AIInventory != nil {
		if _, err := fmt.Fprintf(w, "AI inventory: %d component(s), %d technical signal(s)\n", value.AIInventory.Summary.Components, value.AIInventory.Summary.Signals); err != nil {
			return err
		}
	}
	if value.AIUseInventory != nil {
		if err := writeAIUseInventoryTerminal(w, *value.AIUseInventory); err != nil {
			return err
		}
	}
	if value.AIUseMappings != nil {
		if err := writeAIUseMappingsTerminal(w, *value.AIUseMappings); err != nil {
			return err
		}
	}
	if err := writeConciseApplicabilityReadiness(w, value); err != nil {
		return err
	}
	objectives := framework.ObjectiveSummary{}
	required, recommended, withEvidence, withoutEvidence, unresolved := 0, 0, 0, 0, 0
	reviewTargets, reviewedTargets := 0, 0
	for _, result := range value.Frameworks {
		objectives.Total += result.TechnicalEvidence.Summary.Total
		objectives.CandidateEvidence += result.TechnicalEvidence.Summary.CandidateEvidence
		objectives.NotDetected += result.TechnicalEvidence.Summary.NotDetected
		objectives.NotEvaluated += result.TechnicalEvidence.Summary.NotEvaluated
		required += result.Reconciliation.Summary.LikelyRequired
		recommended += result.Reconciliation.Summary.Recommended
		withEvidence += result.Reconciliation.Summary.RequirementWithEvidence
		withoutEvidence += result.Reconciliation.Summary.RequirementWithoutEvidence
		unresolved += result.Reconciliation.Summary.Unresolved
		if result.TechnicalReview != nil {
			reviewTargets += result.TechnicalReview.InputCandidates
			reviewedTargets += result.TechnicalReview.Reviewed
		}
	}
	if objectives.Total > 0 {
		if _, err := fmt.Fprintf(w, "Code safeguards checked: %d total; %d with matching code signals; %d without matching signals; %d not fully checked\n",
			objectives.Total, objectives.CandidateEvidence, objectives.NotDetected, objectives.NotEvaluated); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "Requirement screening: %d likely required; %d voluntary recommendations; %d with matching code; %d without matching code; %d unresolved\n",
			required, recommended, withEvidence, withoutEvidence, unresolved); err != nil {
			return err
		}
	}
	if value.Review != nil || reviewTargets > 0 {
		findingReviewed, findingTargets := 0, 0
		if value.Review != nil {
			findingReviewed = value.Review.Reviewed
			findingTargets = value.Review.InputFindings
		}
		if _, err := fmt.Fprintf(w, "Advisory AI review: %d/%d finding(s), %d/%d technical target(s) completed\n",
			findingReviewed, findingTargets, reviewedTargets, reviewTargets); err != nil {
			return err
		}
	}
	if value.RepositoryAnalysis != nil {
		analysis := value.RepositoryAnalysis
		if value.RepositoryAnalysisRun == RepositoryAnalysisCompleted && analysis.Coverage.Mode == providers.RepositoryAnalysisTargeted && analysis.Coverage.FilesSubmitted == 0 {
			if _, err := fmt.Fprintln(w, "Repository AI selection: no eligible structural candidate; no source was sent for repository AI review"); err != nil {
				return err
			}
		} else {
			detailLabel := "AI code review detail"
			if value.RepositoryAnalysisRun == RepositoryAnalysisIncomplete {
				detailLabel = "Partial AI code review detail"
			}
			if _, err := fmt.Fprintf(w, "%s: %s; %d likely AI use(s), %d safeguard decision(s), %d other observation(s); %d citation(s) checked\n", detailLabel,
				analysis.Coverage.Mode, len(analysis.Result.AIUses), len(analysis.Result.ObjectiveObservations), len(analysis.Result.UnmappedObservations), analysis.Coverage.CitationsChecked); err != nil {
				return err
			}
			implemented, partial, notImplemented, cannotDetermine := developerTechnicalVerdictCounts(value)
			verdictLabel := "Code-level AI verdicts"
			if value.AIUseMappings != nil {
				verdictLabel = "Use-scoped AI verdicts"
			}
			if implemented+partial+notImplemented+cannotDetermine == 0 {
				if _, err := fmt.Fprintf(w, "%s: no safeguard decisions returned\n", verdictLabel); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(w, "%s: %d implemented, %d partial, %d not demonstrated, %d unclear\n", verdictLabel,
					implemented, partial, notImplemented, cannotDetermine); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(w, "Use --verbose for full terminal evidence.")
	return err
}

func writeAIUseInventoryTerminal(w io.Writer, value aiuse.Snapshot) error {
	summary := value.Summary
	_, err := fmt.Fprintf(w, "AI-use organization (optional): %d confirmed, %d draft, %d retired; %d model-suggested; %d other AI-related code signal(s)\n",
		summary.Confirmed, summary.Draft, summary.Retired, summary.Suggested, summary.UngroupedSignals)
	return err
}

func writeAIUseMappingsTerminal(w io.Writer, value usemapping.Report) error {
	if len(value.Uses) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "Per-use safeguard detail (optional scope refinement): %d confirmed AI use(s), %d framework/system context(s)\n", len(value.Uses), value.Summary.FrameworkSystemContexts); err != nil {
		return err
	}
	for _, use := range value.Uses {
		contextNote := ""
		if use.Summary.UnassociatedUses > 0 {
			contextNote = "; no system association"
			if use.Summary.Unresolved+use.Summary.ContextDependent > 0 {
				contextNote += " (context needed)"
			}
		} else if use.Summary.MissingSystemReferences > 0 {
			contextNote = fmt.Sprintf("; %d missing system reference(s)", use.Summary.MissingSystemReferences)
		}
		if _, err := fmt.Fprintf(w, "  %s: %d likely-required check(s), %d recommended check(s), %d check(s) with matching code signals, %d with signals only outside the saved paths, %d likely-required and %d recommended check(s) without detected in-scope signals, %d unresolved%s\n",
			use.UseName, use.Summary.LikelyRequired, use.Summary.Recommended, use.Summary.WithInScopeCodeEvidence,
			use.Summary.ObjectivesWithEvidenceOutsideUse, use.Summary.LikelyRequiredWithoutInScopeEvidence, use.Summary.RecommendedWithoutInScopeEvidence,
			use.Summary.Unresolved+use.Summary.ContextDependent, contextNote); err != nil {
			return err
		}
	}
	return nil
}

// WriteTerminalRepositoryAnalysis renders repository reasoning and its bounded
// code-level verdicts. It never changes deterministic findings or gates.
func WriteTerminalRepositoryAnalysis(w io.Writer, analysis providers.RepositoryAnalysisResult) error {
	if _, err := fmt.Fprintf(w, "Repository AI code analysis (%s / %s): %s\n", analysis.Provider, analysis.Model, analysis.Coverage.Mode); err != nil {
		return err
	}
	if analysis.CacheHit {
		if _, err := fmt.Fprintln(w, "        Result: reused matching private cache; no model request for this layer"); err != nil {
			return err
		}
	}
	if analysis.CacheHit {
		if analysis.Coverage.SourceBatchesTotal > 0 {
			if _, err := fmt.Fprintf(w, "        Cached reviewed context: %d original file-excerpt submission(s), %d/%d bounded source batch(es) completed; current run transferred no source\n",
				analysis.Coverage.FilesSubmitted, analysis.Coverage.SourceBatchesCompleted, analysis.Coverage.SourceBatchesTotal); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(w, "        Cached reviewed context: %d original file-excerpt submission(s); current run transferred no source\n", analysis.Coverage.FilesSubmitted); err != nil {
			return err
		}
	} else if analysis.Coverage.Mode == providers.RepositoryAnalysisTargeted {
		if analysis.Coverage.FilesSubmitted == 0 {
			if _, err := fmt.Fprintln(w, "        Context: no eligible structural candidate; no source was sent for repository AI review"); err != nil {
				return err
			}
		} else if analysis.Coverage.SourceBatchesTotal > 0 {
			if _, err := fmt.Fprintf(w, "        Context: %d file-excerpt transfer(s), %d/%d bounded source batch(es) completed, %d verified citation(s)\n",
				analysis.Coverage.FilesSubmitted, analysis.Coverage.SourceBatchesCompleted, analysis.Coverage.SourceBatchesTotal, analysis.Coverage.CitationsChecked); err != nil {
				return err
			}
		} else if analysis.Coverage.ReviewScope == providers.RepositoryReviewScopeChanged {
			if _, err := fmt.Fprintf(w, "        Context: %d file-excerpt submission(s) from %d eligible review-scope file(s), %d verified citation(s)\n",
				analysis.Coverage.FilesSubmitted, analysis.Coverage.ScopeFiles, analysis.Coverage.CitationsChecked); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(w, "        Context: %d file-excerpt submission(s) from %d discovered file(s), %d verified citation(s)\n",
				analysis.Coverage.FilesSubmitted, analysis.Coverage.RepositoryFiles, analysis.Coverage.CitationsChecked); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintf(w, "        Context: %d file-excerpt transfer(s) across a %d-file discovered repository, %d subsystem(s), %d verified citation(s)\n",
			analysis.Coverage.FilesSubmitted, analysis.Coverage.RepositoryFiles, analysis.Coverage.Subsystems, analysis.Coverage.CitationsChecked); err != nil {
			return err
		}
	}
	if analysis.Coverage.ReviewScope == providers.RepositoryReviewScopeChanged {
		if _, err := fmt.Fprintf(w, "        Changed scope: %d changed eligible + %d connected file(s); full %d-file repository governance remained local\n",
			analysis.Coverage.ChangedFiles, analysis.Coverage.ConnectedFiles, analysis.Coverage.RepositoryFiles); err != nil {
			return err
		}
	}
	if analysis.FollowUpRequested {
		if _, err := fmt.Fprintf(w, "        Follow-up: %d bounded excerpt(s) retrieved in one additional review\n", analysis.FollowUpExcerpts); err != nil {
			return err
		}
	}
	if analysis.OutputRecoveryUsed {
		if _, err := fmt.Fprintln(w, "        Recovery: the initial output limit was reached; one terse recovery review completed"); err != nil {
			return err
		}
	}
	if analysis.Usage.PromptTokens > 0 || analysis.Usage.CompletionTokens > 0 {
		if _, err := fmt.Fprintf(w, "        Tokens: %d input, %d output (%d reasoning)\n", analysis.Usage.PromptTokens, analysis.Usage.CompletionTokens, analysis.Usage.ReasoningTokens); err != nil {
			return err
		}
	}
	for _, use := range analysis.Result.AIUses {
		if _, err := fmt.Fprintf(w, "AI USE  %-6s %s — %s\n", strings.ToUpper(use.Confidence), use.Name, use.Purpose); err != nil {
			return err
		}
		for _, citation := range use.Evidence {
			if _, err := fmt.Fprintf(w, "        Evidence: %s — %s\n", locationText(citation.Path, citation.Line), citation.Summary); err != nil {
				return err
			}
		}
	}
	for _, observation := range analysis.Result.ObjectiveObservations {
		if _, err := fmt.Fprintf(w, "REPO MAP %-38s %-6s %s\n        %s\n", observation.DerivedTechnicalVerdict(), strings.ToUpper(observation.Confidence), observation.ObjectiveID, observation.Rationale); err != nil {
			return err
		}
		for _, missing := range observation.MissingEvidence {
			if _, err := fmt.Fprintf(w, "        Missing: %s\n", missing); err != nil {
				return err
			}
		}
	}
	for _, observation := range analysis.Result.UnmappedObservations {
		if _, err := fmt.Fprintf(w, "UNMAPPED %-6s %s\n        %s\n", strings.ToUpper(observation.Confidence), observation.Summary, observation.Reason); err != nil {
			return err
		}
	}
	for _, question := range analysis.Result.UnresolvedQuestions {
		if _, err := fmt.Fprintf(w, "        Unresolved: %s\n", question); err != nil {
			return err
		}
	}
	for _, note := range analysis.Notes {
		if _, err := fmt.Fprintf(w, "Repository analysis note: %s\n", note); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeConciseApplicabilityReadiness(w io.Writer, value Report) error {
	reports := make([]*profile.AssessmentReport, 0, len(value.Frameworks)+1)
	if value.Applicability != nil {
		reports = append(reports, value.Applicability)
	}
	for _, result := range value.Frameworks {
		if result.Applicability != nil {
			reports = append(reports, result.Applicability)
		}
	}
	for _, report := range reports {
		for _, system := range report.Systems {
			if _, err := fmt.Fprintf(w, "Applicability context: %s — %s", system.SystemName, system.MappingReadiness); err != nil {
				return err
			}
			if len(system.MissingContext) > 0 {
				if _, err := fmt.Fprintf(w, " (%d unresolved fact(s); requirement mapping is provisional)", len(system.MissingContext)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeFrameworkResultsTerminal(w io.Writer, results []FrameworkResult) error {
	for _, result := range results {
		if _, err := fmt.Fprintf(w, "Framework: %s (%s; %s)\n", result.Name, result.ID, result.Nature); err != nil {
			return err
		}
		if result.Applicability != nil {
			if err := profile.WriteTerminal(w, *result.Applicability); err != nil {
				return err
			}
		}
		if err := writeReconciliationTerminal(w, result.Reconciliation); err != nil {
			return err
		}
		if err := framework.WriteTechnicalEvidenceTerminal(w, result.TechnicalEvidence); err != nil {
			return err
		}
		if result.TechnicalReview != nil {
			if err := WriteTerminalTechnicalReview(w, *result.TechnicalReview); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteTerminalReview renders advisory observations separately from findings.
func WriteTerminalReview(w io.Writer, review providers.ReviewResult) error {
	if _, err := fmt.Fprintf(w, "Ollama advisory review (%s): %d of %d finding(s) reviewed\n", review.Model, review.Reviewed, review.InputFindings); err != nil {
		return err
	}
	for _, observation := range review.Observations {
		if _, err := fmt.Fprintf(w, "REVIEW  %-13s %-6s %s\n", observation.Verdict, strings.ToUpper(observation.Confidence), observation.RuleID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        %s\n", observation.Rationale); err != nil {
			return err
		}
		if observation.SuggestedAction != "" {
			if _, err := fmt.Fprintf(w, "        Suggested: %s\n", observation.SuggestedAction); err != nil {
				return err
			}
		}
	}
	for _, note := range review.Notes {
		if _, err := fmt.Fprintf(w, "Review note: %s\n", note); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// WriteTerminalTechnicalReview renders bounded evidence investigations without
// merging them into deterministic evidence or legal applicability status.
func WriteTerminalTechnicalReview(w io.Writer, review providers.TechnicalReviewResult) error {
	if _, err := fmt.Fprintf(w, "Ollama technical evidence investigation (%s): %d of %d target(s) reviewed\n", review.Model, review.Reviewed, review.InputCandidates); err != nil {
		return err
	}
	for _, observation := range review.Observations {
		if _, err := fmt.Fprintf(w, "EVIDENCE %-24s %-23s %s\n", observation.Conclusion, observation.Assurance, observation.ObjectiveID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "        Strength: %s; confidence: %s; evidence: %s\n        %s\n", observation.Strength, strings.ToUpper(observation.Confidence), observation.EvidenceFingerprint, observation.Rationale); err != nil {
			return err
		}
		if observation.SystemID != "" {
			if _, err := fmt.Fprintf(w, "        Scope: %s ownership for %s (%s), %d repository file(s)\n", observation.OwnershipScope, observation.SystemName, observation.SystemID, observation.RepositoryFiles); err != nil {
				return err
			}
		}
		for _, claim := range observation.SupportingEvidence {
			if _, err := fmt.Fprintf(w, "        Supports: %s — %s\n", locationText(claim.Path, claim.Line), claim.Summary); err != nil {
				return err
			}
		}
		for _, claim := range observation.ContradictoryEvidence {
			if _, err := fmt.Fprintf(w, "        Contradicts: %s — %s\n", locationText(claim.Path, claim.Line), claim.Summary); err != nil {
				return err
			}
		}
		for _, missing := range observation.MissingEvidence {
			if _, err := fmt.Fprintf(w, "        Missing: %s\n", missing); err != nil {
				return err
			}
		}
		if observation.FollowUpRequested {
			if _, err := fmt.Fprintf(w, "        Follow-up: %d bounded excerpt(s) from %s\n", observation.FollowUpExcerpts, strings.Join(observation.FollowUpQueries, ", ")); err != nil {
				return err
			}
		}
		if observation.GuardrailNote != "" {
			if _, err := fmt.Fprintf(w, "        Guardrail: %s (model returned %s)\n", observation.GuardrailNote, observation.ModelStrength); err != nil {
				return err
			}
		}
		for _, question := range observation.UnresolvedQuestions {
			if _, err := fmt.Fprintf(w, "        Unresolved: %s\n", question); err != nil {
				return err
			}
		}
		if observation.SuggestedReview != "" {
			if _, err := fmt.Fprintf(w, "        Suggested: %s\n", observation.SuggestedReview); err != nil {
				return err
			}
		}
	}
	for _, note := range review.Notes {
		if _, err := fmt.Fprintf(w, "Evidence investigation note: %s\n", note); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

// WriteTerminalExecutionVerification renders the isolated command result as
// supporting evidence without treating it as a compliance decision.
func WriteTerminalExecutionVerification(w io.Writer, verification verification.Report) error {
	if _, err := fmt.Fprintf(w, "Isolated execution verification %s: %s (exit %d, %d ms)\n", verification.RecipeID, strings.ToUpper(string(verification.Status)), verification.ExitCode, verification.DurationMS); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "        Container: %s / %s; command: %s\n", verification.Runtime, verification.Image, strings.Join(verification.Command, " ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "        Declared objectives: %s\n        Boundary: %s\n", strings.Join(verification.Objectives, ", "), verification.Boundary); err != nil {
		return err
	}
	if len(verification.Systems) > 0 {
		if _, err := fmt.Fprintf(w, "        Declared systems: %s\n", strings.Join(verification.Systems, ", ")); err != nil {
			return err
		}
	}
	if verification.Output != "" {
		if _, err := fmt.Fprintf(w, "        Output:\n%s\n", verification.Output); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeTerminalSummary(w io.Writer, report Report) error {
	if _, err := fmt.Fprintf(w, "Summary: %s\n", summaryText(report.Summary)); err != nil {
		return err
	}
	if report.Suppressed > 0 {
		if _, err := fmt.Fprintf(w, "Suppressed: %d accepted or baselined %s\n", report.Suppressed, issueWord(report.Suppressed)); err != nil {
			return err
		}
	}
	for _, warning := range report.Warnings {
		if _, err := fmt.Fprintf(w, "Warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func issueWord(count int) string {
	if count == 1 {
		return "issue"
	}
	return "issues"
}

func severityLabel(severity rules.Severity) string {
	switch severity {
	case rules.SeverityCritical:
		return "CRIT"
	case rules.SeverityHigh:
		return "HIGH"
	case rules.SeverityMedium:
		return "MED"
	case rules.SeverityLow:
		return "LOW"
	default:
		return "INFO"
	}
}

func colorize(severity rules.Severity, value string) string {
	code := "36"
	switch severity {
	case rules.SeverityCritical:
		code = "35"
	case rules.SeverityHigh:
		code = "31"
	case rules.SeverityMedium:
		code = "33"
	case rules.SeverityLow:
		code = "34"
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func summaryText(summary Summary) string {
	parts := make([]string, 0, 5)
	appendCount := func(count int, name string) {
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, name))
		}
	}
	appendCount(summary.Critical, "critical")
	appendCount(summary.High, "high")
	appendCount(summary.Medium, "medium")
	appendCount(summary.Low, "low")
	appendCount(summary.Info, "info")
	if len(parts) == 0 {
		return "no findings"
	}
	return strings.Join(parts, ", ")
}
