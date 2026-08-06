package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

func writeAIInventoryTerminal(writer io.Writer, value inventory.Report) error {
	if _, err := fmt.Fprintf(writer, "AI component inventory: %d component(s), %d technical signal(s)\n", value.Summary.Components, value.Summary.Signals); err != nil {
		return err
	}
	if len(value.Components) == 0 {
		if _, err := fmt.Fprintln(writer, "  No configured AI provider or framework signal was detected."); err != nil {
			return err
		}
		_, err := fmt.Fprintln(writer)
		return err
	}
	for _, component := range value.Components {
		locations := make([]string, 0, len(component.Locations))
		for _, location := range component.Locations {
			locations = append(locations, locationText(location.Path, location.Line))
		}
		if _, err := fmt.Fprintf(writer, "  %-9s %s (%s) — %s\n", strings.ToUpper(string(component.Kind)), component.Name, component.Confidence, strings.Join(locations, ", ")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func writeReconciliationTerminal(writer io.Writer, value reconciliation.Report) error {
	if _, err := fmt.Fprintf(writer, "Requirement/evidence reconciliation (mapping %s): %d likely required, %d with candidate evidence, %d without detected evidence, %d mismatch(es), %d unresolved\n",
		value.MappingVersion, value.Summary.LikelyRequired, value.Summary.RequirementWithEvidence,
		value.Summary.RequirementWithoutEvidence, value.Summary.EvidenceMismatches, value.Summary.Unresolved); err != nil {
		return err
	}
	ownershipMode := "not configured"
	if value.Ownership.Configured {
		ownershipMode = fmt.Sprintf("configured (%d rule(s))", len(value.Ownership.Rules))
	} else if value.Summary.InferredReferences > 0 {
		ownershipMode = "single-system inference"
	}
	if _, err := fmt.Fprintf(writer, "Path ownership: %s; %d assigned, %d shared, %d conflicting, %d unassigned reference(s)\n",
		ownershipMode, value.Summary.AssignedReferences, value.Summary.SharedReferences,
		value.Summary.ConflictingReferences, value.Summary.UnassignedReferences); err != nil {
		return err
	}
	if len(value.Systems) == 0 {
		if _, err := fmt.Fprintln(writer, "  No system profile was declared; AI and control evidence is reported below as unassigned."); err != nil {
			return err
		}
	}
	for _, system := range value.Systems {
		if _, err := fmt.Fprintf(writer, "\n  System: %s (%s)\n", system.SystemName, system.SystemID); err != nil {
			return err
		}
		for _, objective := range system.Objectives {
			if _, err := fmt.Fprintf(writer, "  %-10s %-10s %s — %s\n", reconciliationLabel(objective.Mapping), objective.SourceReference, objective.Title, objective.Mapping); err != nil {
				return err
			}
			if showReconciliationReason(objective.Mapping) && len(objective.Reasons) > 0 {
				if _, err := fmt.Fprintf(writer, "             Why: %s (%s)\n", objective.Reasons[len(objective.Reasons)-1].Message, objective.Reasons[len(objective.Reasons)-1].Code); err != nil {
					return err
				}
			}
			if objective.Investigation != nil {
				if _, err := fmt.Fprintf(writer, "             AI investigation: %s; assurance %s; %d supporting and %d contradictory reference(s)\n",
					objective.Investigation.Conclusion, objective.Investigation.Assurance,
					objective.Investigation.SupportingEvidence, objective.Investigation.ContradictoryEvidence); err != nil {
					return err
				}
				if objective.Investigation.SystemID != "" {
					if _, err := fmt.Fprintf(writer, "             Investigation scope: %s ownership for system %s across %d repository file(s)\n",
						objective.Investigation.OwnershipScope, objective.Investigation.SystemID, objective.Investigation.RepositoryFiles); err != nil {
						return err
					}
				}
			}
			if objective.Verification != nil {
				if _, err := fmt.Fprintf(writer, "             Isolated tests: %d passed, %d failed; assurance %s (%s)\n",
					objective.Verification.Passed, objective.Verification.Failed, objective.Verification.Assurance,
					strings.Join(objective.Verification.Recipes, ", ")); err != nil {
					return err
				}
			}
			for _, reference := range objective.EvidenceReferences {
				if _, err := fmt.Fprintf(writer, "             Evidence: %s [%s]\n", locationText(reference.Path, reference.Line), ownershipReferenceText(reference)); err != nil {
					return err
				}
			}
		}
		for _, component := range system.ObservedComponents {
			if _, err := fmt.Fprintf(writer, "  COMPONENT  %s (%s) — %s\n", component.Name, component.Kind, component.Mapping); err != nil {
				return err
			}
			for _, reference := range component.Locations {
				if _, err := fmt.Fprintf(writer, "             Evidence: %s [%s]\n", locationText(reference.Path, reference.Line), ownershipReferenceText(reference)); err != nil {
					return err
				}
			}
		}
	}
	if len(value.Unmapped) > 0 {
		if _, err := fmt.Fprintf(writer, "\n  Repository evidence with unresolved ownership: %d\n", len(value.Unmapped)); err != nil {
			return err
		}
		for _, evidence := range value.Unmapped {
			if _, err := fmt.Fprintf(writer, "  UNRESOLVED %-20s %s — %s\n", evidence.Kind, evidence.Title, evidence.Reason.Code); err != nil {
				return err
			}
			for _, reference := range evidence.References {
				if _, err := fmt.Fprintf(writer, "             Evidence: %s [%s]\n", locationText(reference.Path, reference.Line), ownershipReferenceText(reference)); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func ownershipReferenceText(reference reconciliation.EvidenceReference) string {
	owners := "no system"
	if len(reference.Systems) > 0 {
		owners = strings.Join(reference.Systems, ", ")
	}
	return fmt.Sprintf("%s -> %s", reference.Ownership, owners)
}

func reconciliationLabel(status reconciliation.MappingStatus) string {
	switch status {
	case reconciliation.MappingRequirementWithEvidence:
		return "MATCH"
	case reconciliation.MappingRequirementWithoutEvidence:
		return "NOT FOUND"
	case reconciliation.MappingEvidenceMismatch:
		return "MISMATCH"
	case reconciliation.MappingEvidenceUnclear:
		return "UNCLEAR"
	case reconciliation.MappingNotCurrentlyIndicated:
		return "N/A NOW"
	case reconciliation.MappingUnassigned:
		return "UNASSIGNED"
	case reconciliation.MappingUnableToEvaluate:
		return "UNKNOWN"
	default:
		return "UNRESOLVED"
	}
}

func showReconciliationReason(status reconciliation.MappingStatus) bool {
	switch status {
	case reconciliation.MappingRequirementWithEvidence, reconciliation.MappingNotCurrentlyIndicated:
		return false
	default:
		return true
	}
}
