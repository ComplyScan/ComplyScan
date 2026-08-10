package profile

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func WriteJSON(writer io.Writer, report AssessmentReport) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode applicability JSON: %w", err)
	}
	return nil
}

func WriteTerminal(writer io.Writer, report AssessmentReport) error {
	if _, err := fmt.Fprintf(writer, "%s applicability profile: %d system(s)\n\n", report.Framework, len(report.Systems)); err != nil {
		return err
	}
	for _, assessment := range report.Systems {
		if _, err := fmt.Fprintf(writer, "%s (%s)\n", assessment.SystemName, assessment.SystemID); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "  Automated scope: %s\n  High-risk screening: %s\n  Technical mapping readiness: %s\n", assessment.AutomatedScope, assessment.HighRiskScreening, assessment.MappingReadiness); err != nil {
			return err
		}
		if assessment.HumanDecision == nil {
			if _, err := fmt.Fprintln(writer, "  Human decision: not recorded"); err != nil {
				return err
			}
		} else {
			attribution := ""
			if assessment.HumanDecision.ReviewedBy != "" {
				attribution = fmt.Sprintf(" by %s on %s", assessment.HumanDecision.ReviewedBy, assessment.HumanDecision.ReviewedAt)
			}
			if _, err := fmt.Fprintf(writer, "  Human decision: %s%s\n", assessment.HumanDecision.Status, attribution); err != nil {
				return err
			}
			if assessment.HumanDecision.Rationale != "" {
				if _, err := fmt.Fprintf(writer, "  Human rationale: %s\n", assessment.HumanDecision.Rationale); err != nil {
					return err
				}
			}
		}
		if len(assessment.Signals) > 0 {
			if _, err := fmt.Fprintf(writer, "  Signals: %s\n", strings.Join(assessment.Signals, " ")); err != nil {
				return err
			}
		}
		for _, missing := range assessment.MissingContext {
			if _, err := fmt.Fprintf(writer, "  Missing context: %s\n", missing); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}
	for _, note := range report.Notes {
		if _, err := fmt.Fprintf(writer, "Note: %s\n", note); err != nil {
			return err
		}
	}
	return nil
}
