package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
)

// WriteMarkdown renders the same scan result as a human-readable local report.
func WriteMarkdown(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintln(writer, "# ComplyScan technical evidence report"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "\n> Technical evidence only. This is not a legal compliance assessment.\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Scan ID: %s\n- Created: %s\n- Target: %s\n- Tool: ComplyScan %s\n", inlineCode(report.Scan.ID), inlineCode(report.Scan.CreatedAt), inlineCode(report.Target), markdownText(report.Tool.Version)); err != nil {
		return err
	}
	if report.Tool.Commit != "" {
		if _, err := fmt.Fprintf(writer, "- Tool commit: %s\n", inlineCode(report.Tool.Commit)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "- Finding scope: %s\n- Technical evidence scope: %s\n", markdownText(report.Scan.Scope.Findings), markdownText(report.Scan.Scope.TechnicalEvidence)); err != nil {
		return err
	}
	if report.Scan.Scope.ChangedSince != "" {
		if _, err := fmt.Fprintf(writer, "- Changed since: %s\n", inlineCode(report.Scan.Scope.ChangedSince)); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(writer, "\n## Rule findings"); err != nil {
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
		if _, err := fmt.Fprintf(writer, "- Rule: %s\n- Confidence: %s\n- Fingerprint: %s\n", inlineCode(finding.RuleID), markdownText(finding.Confidence), inlineCode(finding.Fingerprint)); err != nil {
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

	if report.Applicability != nil {
		if _, err := fmt.Fprintln(writer, "\n## Declared applicability context"); err != nil {
			return err
		}
		for _, system := range report.Applicability.Systems {
			if _, err := fmt.Fprintf(writer, "\n### %s\n\n- System ID: %s\n- Automated scope: %s\n- High-risk screening: %s\n", markdownText(system.SystemName), inlineCode(system.SystemID), markdownText(string(system.AutomatedScope)), markdownText(string(system.HighRiskScreening))); err != nil {
				return err
			}
			for _, missing := range system.MissingContext {
				if _, err := fmt.Fprintf(writer, "- Missing context: %s\n", markdownText(missing)); err != nil {
					return err
				}
			}
		}
	}

	if report.TechnicalEvidence != nil {
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
	if report.TechnicalReview != nil {
		if _, err := fmt.Fprintf(writer, "\n## Ollama technical-objective review\n\n- Model: %s\n- Candidates reviewed: %d of %d\n", inlineCode(report.TechnicalReview.Model), report.TechnicalReview.Reviewed, report.TechnicalReview.InputCandidates); err != nil {
			return err
		}
		for _, observation := range report.TechnicalReview.Observations {
			if _, err := fmt.Fprintf(writer, "\n### %s\n\n- Evidence fingerprint: %s\n- Strength: %s\n- Confidence: %s\n\n%s\n",
				inlineCode(observation.ObjectiveID), inlineCode(observation.EvidenceFingerprint), markdownText(string(observation.Strength)), markdownText(observation.Confidence), markdownText(observation.Rationale)); err != nil {
				return err
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

	_, err := fmt.Fprintln(writer, "\n---\n\nGenerated from the versioned JSON evidence bundle. Candidate evidence requires technical and human verification.")
	return err
}

func writeTechnicalEvidenceMarkdown(writer io.Writer, evidence framework.TechnicalEvidenceReport) error {
	if _, err := fmt.Fprintf(writer, "\n## EU AI Act technical evidence\n\n- Pack: %s at version %s\n- Pack digest: %s\n- Source: [%s](%s)\n- Checks with candidate evidence: %d\n- Checks with no evidence detected: %d\n- Checks not evaluated: %d\n",
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
