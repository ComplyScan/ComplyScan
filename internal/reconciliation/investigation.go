package reconciliation

import "github.com/ComplyScan/ComplyScan/internal/providers"

// AttachTechnicalInvestigations adds advisory assurance metadata to the
// deterministic mapping without changing requirement, evidence, or mapping
// statuses. The strongest bounded observation represents each objective while
// counts preserve that more than one candidate may have been investigated.
func AttachTechnicalInvestigations(report *Report, review providers.TechnicalReviewResult) {
	if report == nil {
		return
	}
	byObjective := make(map[string][]providers.TechnicalObservation)
	for _, observation := range review.Observations {
		byObjective[observation.ObjectiveID] = append(byObjective[observation.ObjectiveID], observation)
	}
	for systemIndex := range report.Systems {
		for objectiveIndex := range report.Systems[systemIndex].Objectives {
			objective := &report.Systems[systemIndex].Objectives[objectiveIndex]
			observations := byObjective[objective.ObjectiveID]
			if len(observations) == 0 {
				continue
			}
			best := observations[0]
			supporting, contradictory := 0, 0
			for _, observation := range observations {
				supporting += len(observation.SupportingEvidence)
				contradictory += len(observation.ContradictoryEvidence)
				if assuranceRank(observation.Assurance) > assuranceRank(best.Assurance) {
					best = observation
				}
			}
			objective.Investigation = &ObjectiveInvestigation{
				Conclusion: best.Conclusion, Assurance: best.Assurance, Confidence: best.Confidence,
				Observations: len(observations), SupportingEvidence: supporting, ContradictoryEvidence: contradictory,
				RuntimeVerificationRequired: best.RuntimeVerificationRequired, LegalReviewRequired: best.LegalReviewRequired,
			}
			addInvestigationSummary(&report.Summary, best.Assurance)
		}
	}
}

func assuranceRank(value providers.AssuranceLevel) int {
	switch value {
	case providers.AssuranceStructurallyVerified:
		return 6
	case providers.AssuranceAISubstantiated:
		return 5
	case providers.AssuranceTestEvidenceObserved:
		return 4
	case providers.AssuranceSignalDetected:
		return 3
	case providers.AssuranceInvestigationNoEvidence:
		return 2
	default:
		return 1
	}
}

func addInvestigationSummary(summary *Summary, value providers.AssuranceLevel) {
	switch value {
	case providers.AssuranceStructurallyVerified:
		summary.StructurallyVerified++
	case providers.AssuranceAISubstantiated:
		summary.AISubstantiated++
	case providers.AssuranceInvestigationNoEvidence:
		summary.InvestigationNoEvidence++
	case providers.AssuranceUnableToDetermine:
		summary.InvestigationUnresolved++
	}
}
