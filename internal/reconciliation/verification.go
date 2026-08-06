package reconciliation

import (
	"sort"

	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/verification"
)

// AttachExecutionVerifications adds user-declared isolated test evidence to
// matching objectives without changing applicability, deterministic evidence,
// mapping status, or legal-review boundaries.
func AttachExecutionVerifications(report *Report, verifications []verification.Report) {
	if report == nil || len(verifications) == 0 {
		return
	}
	for systemIndex := range report.Systems {
		system := &report.Systems[systemIndex]
		for objectiveIndex := range system.Objectives {
			objective := &system.Objectives[objectiveIndex]
			for _, result := range verifications {
				if !containsValue(result.Objectives, objective.ObjectiveID) || len(result.Systems) > 0 && !containsValue(result.Systems, system.SystemID) {
					continue
				}
				if objective.Verification == nil {
					objective.Verification = &ObjectiveVerification{
						Assurance: providers.AssuranceUnableToDetermine, Recipes: []string{},
						Boundary: "Isolated test results support only the declared technical-objective association; they do not establish production effectiveness or compliance.",
					}
				}
				objective.Verification.Runs++
				objective.Verification.Recipes = append(objective.Verification.Recipes, result.RecipeID)
				if result.Status == verification.StatusPassed {
					objective.Verification.Passed++
					objective.Verification.Assurance = providers.AssuranceTestEvidenceObserved
				} else {
					objective.Verification.Failed++
				}
			}
			if objective.Verification != nil {
				sort.Strings(objective.Verification.Recipes)
				if objective.Verification.Passed > 0 {
					report.Summary.TestEvidenceObserved++
				}
			}
		}
	}
}

func containsValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
