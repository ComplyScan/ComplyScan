package cli

import (
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/ComplyScan/ComplyScan/internal/usemapping"
)

// buildConfirmedAIUseReviewContexts turns deterministic per-use screening into
// bounded, operator-owned model context. Only likely-required and explicitly
// selected voluntary practices are sent as direct use-level review targets;
// unresolved applicability remains a human-context question in the report.
func buildConfirmedAIUseReviewContexts(mappings *usemapping.Report, frameworks []report.FrameworkResult) []providers.RepositoryConfirmedAIUse {
	if mappings == nil {
		return nil
	}
	packByFramework := make(map[string]string, len(frameworks))
	for _, frameworkResult := range frameworks {
		packByFramework[frameworkResult.ID] = frameworkResult.TechnicalEvidence.Pack.ID
	}
	result := make([]providers.RepositoryConfirmedAIUse, 0, len(mappings.Uses))
	for _, use := range mappings.Uses {
		context := providers.RepositoryConfirmedAIUse{
			ID: use.UseID, Name: use.UseName, Description: use.Description,
			Paths: append([]string(nil), use.Paths...), SystemIDs: []string{},
			Objectives: []providers.RepositoryAIUseObjectiveContext{},
		}
		seen := make(map[string]struct{})
		seenSystems := make(map[string]struct{})
		for _, frameworkResult := range use.Frameworks {
			packID := packByFramework[frameworkResult.ID]
			if packID == "" {
				continue
			}
			for _, mappedContext := range frameworkResult.Contexts {
				systemID := ""
				switch mappedContext.Association.Status {
				case usemapping.AssociationConfigured:
					systemID = mappedContext.Association.SystemID
					if _, exists := seenSystems[systemID]; !exists {
						seenSystems[systemID] = struct{}{}
						context.SystemIDs = append(context.SystemIDs, systemID)
					}
				case usemapping.AssociationNone:
					// Framework-wide recommendations can be reviewed without a
					// legal/system applicability conclusion.
				default:
					continue
				}
				for _, objective := range mappedContext.Objectives {
					if objective.Requirement != reconciliation.RequirementLikelyRequired && objective.Requirement != reconciliation.RequirementRecommended {
						continue
					}
					objectiveID := packID + "/" + objective.ObjectiveID
					key := objectiveID + "\x00" + systemID
					if _, duplicate := seen[key]; duplicate {
						continue
					}
					seen[key] = struct{}{}
					context.Objectives = append(context.Objectives, providers.RepositoryAIUseObjectiveContext{
						ObjectiveID: objectiveID, SystemID: systemID, Requirement: string(objective.Requirement),
					})
				}
			}
		}
		sort.Slice(context.Objectives, func(i, j int) bool {
			if context.Objectives[i].ObjectiveID != context.Objectives[j].ObjectiveID {
				return context.Objectives[i].ObjectiveID < context.Objectives[j].ObjectiveID
			}
			return context.Objectives[i].SystemID < context.Objectives[j].SystemID
		})
		sort.Strings(context.SystemIDs)
		result = append(result, context)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
