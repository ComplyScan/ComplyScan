package usemapping

import (
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
)

func TestBuildMapsOnlyActiveConfirmedUsesAndFiltersEvidencePaths(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework(
		"eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem,
		mappingTestMatch("inside", "apps/support/guard.go"), mappingTestMatch("outside", "apps/ranking/guard.go"),
	)
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{
		mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**"),
		mappingTestUse("draft", profile.ReviewDraft, aiuse.StatusActive, []string{"support"}, "apps/support/**"),
		mappingTestUse("retired", profile.ReviewConfirmed, aiuse.StatusRetired, []string{"support"}, "apps/support/**"),
	}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, mappingTestInventory(), nil)
	if len(result.Uses) != 1 || result.Uses[0].UseID != "support-chat" {
		t.Fatalf("mapped uses = %#v", result.Uses)
	}
	objective := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0]
	if objective.Requirement != reconciliation.RequirementLikelyRequired || objective.Mapping != reconciliation.MappingRequirementWithEvidence {
		t.Fatalf("objective mapping = %#v", objective)
	}
	if len(objective.EvidenceReferences) != 1 || objective.EvidenceReferences[0].Path != "apps/support/guard.go" || objective.EvidenceReferences[0].Ownership != ownership.StatusAssigned {
		t.Fatalf("scoped references = %#v", objective.EvidenceReferences)
	}
	if len(objective.EvidenceOutsideUse) != 1 || objective.EvidenceOutsideUse[0].Path != "apps/ranking/guard.go" {
		t.Fatalf("outside-use references = %#v", objective.EvidenceOutsideUse)
	}
	if result.Summary.Uses != 1 || result.Summary.LikelyRequired != 1 || result.Summary.WithInScopeCodeEvidence != 1 || result.Summary.ObjectivesWithEvidenceOutsideUse != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func TestBuildEvaluatesOneUseUnderEachAssociatedSystemAndFramework(t *testing.T) {
	highRisk := mappingTestSystem("eu-employment", "EU employment", profile.RegionEU, profile.DomainEmployment)
	other := mappingTestSystem("us-tools", "US tools", profile.RegionUS, profile.DomainSoftwareDevelopment)
	eu := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestMatch("inside", "shared/model.go"))
	assessment := profile.AssessEUAIAct([]profile.System{highRisk, other})
	eu.Applicability = &assessment
	nist := mappingTestFramework("nist-ai-rmf", framework.NatureVoluntaryFramework, framework.ApplicabilitySelectedFramework, mappingTestMatch("inside", "shared/model.go"))
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("shared-use", profile.ReviewConfirmed, aiuse.StatusActive, []string{"eu-employment", "us-tools"}, "shared/**")}

	result := Build(manifest, []profile.System{highRisk, other}, []FrameworkInput{eu, nist}, inventory.Report{}, nil)
	if len(result.Uses[0].Frameworks[0].Contexts) != 2 {
		t.Fatalf("EU contexts = %#v", result.Uses[0].Frameworks[0].Contexts)
	}
	statuses := map[string]reconciliation.RequirementStatus{}
	for _, context := range result.Uses[0].Frameworks[0].Contexts {
		statuses[context.Association.SystemID] = context.Objectives[0].Requirement
	}
	if statuses["eu-employment"] != reconciliation.RequirementLikelyRequired || statuses["us-tools"] != reconciliation.RequirementContextDependent {
		t.Fatalf("EU statuses = %#v", statuses)
	}
	for _, context := range result.Uses[0].Frameworks[1].Contexts {
		if context.Objectives[0].Requirement != reconciliation.RequirementRecommended {
			t.Fatalf("NIST requirement = %q", context.Objectives[0].Requirement)
		}
	}
}

func TestBuildKeepsNoAndMissingSystemAssociationsExplicitlyUnresolved(t *testing.T) {
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestMatch("inside", "apps/chat/model.go"))
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{
		mappingTestUse("unassociated", profile.ReviewConfirmed, aiuse.StatusActive, nil, "apps/chat/**"),
		mappingTestUse("stale", profile.ReviewConfirmed, aiuse.StatusActive, []string{"removed-system"}, "apps/chat/**"),
	}

	result := Build(manifest, nil, []FrameworkInput{input}, inventory.Report{}, nil)
	if result.Summary.UnassociatedUses != 1 || result.Summary.MissingSystemReferences != 1 {
		t.Fatalf("association summary = %#v", result.Summary)
	}
	for _, use := range result.Uses {
		context := use.Frameworks[0].Contexts[0]
		if context.Association.Status == AssociationConfigured {
			t.Fatalf("unexpected configured association: %#v", context.Association)
		}
		objective := context.Objectives[0]
		if objective.Requirement != reconciliation.RequirementUnresolved || objective.Mapping != reconciliation.MappingEvidenceUnclear {
			t.Fatalf("unresolved objective = %#v", objective)
		}
		if len(objective.Reasons) == 0 || objective.Reasons[0].Code != "ai-use-system-context-missing" {
			t.Fatalf("missing context reason = %#v", objective.Reasons)
		}
		if len(objective.EvidenceReferences) != 1 || objective.EvidenceReferences[0].Ownership != ownership.StatusUnassigned || len(objective.EvidenceReferences[0].Systems) != 0 {
			t.Fatalf("unassociated evidence = %#v", objective.EvidenceReferences)
		}
	}
}

func TestBuildKeepsFrameworkWideVoluntaryPracticesRecommendedWithoutAssociation(t *testing.T) {
	input := mappingTestFramework("nist-ai-rmf", framework.NatureVoluntaryFramework, framework.ApplicabilitySelectedFramework, mappingTestObjective("measure-practice"))
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("unassociated", profile.ReviewConfirmed, aiuse.StatusActive, nil, "apps/chat/**")}

	result := Build(manifest, nil, []FrameworkInput{input}, inventory.Report{}, nil)
	objective := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0]
	if objective.Requirement != reconciliation.RequirementRecommended || objective.Mapping != reconciliation.MappingRecommendedWithoutEvidence {
		t.Fatalf("voluntary objective = %#v", objective)
	}
	if result.Summary.UnassociatedUses != 1 || result.Summary.Unresolved != 0 || result.Summary.Recommended != 1 || result.Summary.RecommendedWithoutInScopeEvidence != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
}

func TestBuildAllowsTwoConfirmedUsesToShareOneImplementationPath(t *testing.T) {
	system := mappingTestSystem("assistant", "Assistant", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestMatch("shared", "gateway/model.go"))
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{
		mappingTestUse("summarization", profile.ReviewConfirmed, aiuse.StatusActive, []string{"assistant"}, "gateway/**"),
		mappingTestUse("classification", profile.ReviewConfirmed, aiuse.StatusActive, []string{"assistant"}, "gateway/**"),
	}

	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "eu-ai-act-pack/objective", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Shared path",
		SupportingEvidence: []providers.RepositoryCitation{{Path: "gateway/model.go", Line: 1}},
	}}}}
	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, analysis)
	if len(result.Uses) != 2 {
		t.Fatalf("uses = %#v", result.Uses)
	}
	for _, use := range result.Uses {
		objective := use.Frameworks[0].Contexts[0].Objectives[0]
		if len(objective.EvidenceReferences) != 1 {
			t.Fatalf("shared evidence disappeared for %s", use.UseID)
		}
		if objective.AIReview != nil {
			t.Fatalf("ambiguous path-only AI review was assigned to %s: %#v", use.UseID, objective.AIReview)
		}
	}
}

func TestBuildAttributesRepositoryReviewOnlyWithFullyScopedCitations(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem,
		mappingTestObjective("inside-review", mappingTestMatch("one", "apps/support/guard.go")),
		mappingTestObjective("mixed-review", mappingTestMatch("two", "apps/support/fallback.go")),
		mappingTestObjective("wrong-system", mappingTestMatch("three", "apps/support/stop.go")),
		mappingTestObjective("no-citation", mappingTestMatch("four", "apps/support/log.go")),
	)
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{
		{ObjectiveID: "eu-ai-act-pack/inside-review", SystemID: "support", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Scoped", SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/guard.go", Line: 1}}},
		{ObjectiveID: "eu-ai-act-pack/mixed-review", SystemID: "support", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Mixed", SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/fallback.go", Line: 1}, {Path: "apps/ranking/fallback.go", Line: 1}}},
		{ObjectiveID: "eu-ai-act-pack/wrong-system", SystemID: "ranking", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Wrong", SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/stop.go", Line: 1}}},
		{ObjectiveID: "eu-ai-act-pack/no-citation", SystemID: "support", Strength: providers.StrengthNotSupported, Confidence: "high", Rationale: "No evidence"},
	}}}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, analysis)
	objectives := result.Uses[0].Frameworks[0].Contexts[0].Objectives
	if objectives[0].AIReview == nil || objectives[0].AIReview.Verdict != providers.RepositoryVerdictImplemented {
		t.Fatalf("scoped review missing: %#v", objectives[0].AIReview)
	}
	for index := 1; index < len(objectives); index++ {
		if objectives[index].AIReview != nil {
			t.Fatalf("unsafe review %s was attributed: %#v", objectives[index].ObjectiveID, objectives[index].AIReview)
		}
	}
}

func TestBuildDoesNotAttributeRepositoryReviewWithRawUnnamespacedObjectiveID(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestMatch("inside", "apps/support/guard.go"))
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{{
		ObjectiveID: "objective", SystemID: "support", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Wrong namespace",
		SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/guard.go", Line: 1}},
	}}}}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, analysis)
	if review := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0].AIReview; review != nil {
		t.Fatalf("unnamespaced repository objective was attributed: %#v", review)
	}
}

func TestBuildPrefersExactSystemRepositoryReviewOverUnscopedReview(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestMatch("inside", "apps/support/guard.go"))
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}
	analysis := &providers.RepositoryAnalysisResult{Result: providers.RepositorySectionResult{ObjectiveObservations: []providers.RepositoryObjectiveObservation{
		{
			ObjectiveID: "eu-ai-act-pack/objective", Strength: providers.StrengthPartial, Confidence: "medium", Rationale: "Unscoped",
			SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/guard.go", Line: 1}},
		},
		{
			ObjectiveID: "eu-ai-act-pack/objective", SystemID: "support", Strength: providers.StrengthStrong, Confidence: "high", Rationale: "Exact system",
			SupportingEvidence: []providers.RepositoryCitation{{Path: "apps/support/guard.go", Line: 1}},
		},
	}}}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, analysis)
	review := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0].AIReview
	if review == nil || review.SystemID != "support" || review.Rationale != "Exact system" || review.Verdict != providers.RepositoryVerdictImplemented {
		t.Fatalf("preferred review = %#v", review)
	}
}

func TestBuildAttachesOnlyBoundedInvestigationForUseEvidenceFingerprint(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	inside := mappingTestMatch(strings.Repeat("a", 64), "apps/support/guard.go")
	outside := mappingTestMatch(strings.Repeat("b", 64), "apps/ranking/guard.go")
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, inside, outside)
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	input.TechnicalReview = &providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{
		{SystemID: "support", ObjectiveID: "objective", EvidenceFingerprint: inside.Fingerprint, InvestigationMode: "candidate-validation", Assurance: providers.AssuranceStructurallyVerified, Conclusion: providers.ConclusionSubstantiated},
		{SystemID: "support", ObjectiveID: "objective", EvidenceFingerprint: outside.Fingerprint, InvestigationMode: "candidate-validation", Assurance: providers.AssuranceAISubstantiated, Conclusion: providers.ConclusionSubstantiated},
	}}
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, nil)
	investigation := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0].Investigation
	if investigation == nil || investigation.Observations != 1 || investigation.Assurance != providers.AssuranceStructurallyVerified {
		t.Fatalf("investigation = %#v", investigation)
	}
	if result.Summary.AIReviewed != 1 {
		t.Fatalf("AI-reviewed summary = %#v", result.Summary)
	}
}

func TestBuildRejectsBoundedInvestigationWithMixedUseClaims(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	inside := mappingTestMatch(strings.Repeat("a", 64), "apps/support/guard.go")
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, inside)
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	input.TechnicalReview = &providers.TechnicalReviewResult{Observations: []providers.TechnicalObservation{{
		SystemID: "support", ObjectiveID: "objective", EvidenceFingerprint: inside.Fingerprint,
		InvestigationMode: "candidate-validation", Assurance: providers.AssuranceAISubstantiated, Conclusion: providers.ConclusionSubstantiated,
		SupportingEvidence: []providers.TechnicalEvidenceClaim{{Path: "apps/support/guard.go", Line: 1}, {Path: "apps/ranking/guard.go", Line: 1}},
	}}}
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, nil)
	if investigation := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0].Investigation; investigation != nil {
		t.Fatalf("mixed-scope bounded investigation was attributed: %#v", investigation)
	}
}

func TestBuildTreatsMissingFrameworkAssessmentAsUnresolved(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	input := mappingTestFramework("future-law", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, mappingTestObjective("future-control"))
	input.Applicability = &profile.AssessmentReport{Framework: "Future law", Systems: []profile.Assessment{}}
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, nil)
	objective := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0]
	if objective.Requirement != reconciliation.RequirementUnresolved || objective.Mapping != reconciliation.MappingApplicabilityUnresolved {
		t.Fatalf("missing assessment was not conservative: %#v", objective)
	}
}

func TestBuildDoesNotClaimMissingEvidenceWhenUseContainsUnsupportedSource(t *testing.T) {
	system := mappingTestSystem("support", "Support", profile.RegionEU, profile.DomainEmployment)
	objective := mappingTestObjective("source-control", mappingTestMatch("outside", "apps/ranking/guard.go"))
	objective.EligibleFileKinds = []string{"source"}
	input := mappingTestFramework("eu-ai-act", framework.NatureLegislation, framework.ApplicabilityHighRiskSystem, objective)
	input.TechnicalEvidence.Analysis.UnsupportedSourceFiles = []string{"apps/support/guard.rs", "apps/ranking/guard.rs"}
	assessment := profile.AssessEUAIAct([]profile.System{system})
	input.Applicability = &assessment
	manifest := aiuse.NewManifest()
	manifest.Uses = []aiuse.Use{mappingTestUse("support-chat", profile.ReviewConfirmed, aiuse.StatusActive, []string{"support"}, "apps/support/**")}

	result := Build(manifest, []profile.System{system}, []FrameworkInput{input}, inventory.Report{}, nil)
	mapped := result.Uses[0].Frameworks[0].Contexts[0].Objectives[0]
	if mapped.Evidence != framework.ObjectiveNotEvaluated || mapped.Mapping != reconciliation.MappingUnableToEvaluate {
		t.Fatalf("unsupported use scope became a false negative: %#v", mapped)
	}
}

func mappingTestUse(id string, review profile.ReviewStatus, status aiuse.RecordStatus, systems []string, paths ...string) aiuse.Use {
	return aiuse.Use{ID: id, Name: id, Description: "Test AI use", SystemIDs: systems, Paths: paths, Status: status, Review: profile.ProfileReview{Status: review}}
}

func mappingTestSystem(id, name string, region profile.OperatingRegion, domain profile.UseCaseDomain) profile.System {
	value := profile.NewDraftSystem(id, name)
	value.OperatingRegions = []profile.OperatingRegion{region}
	value.UseCaseDomains = []profile.UseCaseDomain{domain}
	value.AIActivities = []profile.AIActivity{profile.ActivityInference}
	value.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: "reviewer", ReviewedAt: "2026-08-15"}
	return value
}

func mappingTestFramework(id, nature, scope string, values ...any) FrameworkInput {
	objectives := make([]framework.ObjectiveAssessment, 0)
	for _, value := range values {
		switch typed := value.(type) {
		case framework.EvidenceMatch:
			if len(objectives) == 0 {
				objectives = append(objectives, mappingTestObjective("objective"))
			}
			objectives[0].Matches = append(objectives[0].Matches, typed)
		case framework.ObjectiveAssessment:
			objectives = append(objectives, typed)
		}
	}
	for index := range objectives {
		objectives[index].Applicability.Scope = scope
		if len(objectives[index].Matches) > 0 {
			objectives[index].Status = framework.ObjectiveCandidate
		}
	}
	return FrameworkInput{
		ID: id, Name: id, Nature: nature,
		TechnicalEvidence: framework.TechnicalEvidenceReport{
			Pack: framework.PackReference{ID: id + "-pack", Name: id, Version: "1"}, Coverage: framework.Coverage{Framework: id, Nature: nature}, Objectives: objectives,
		},
	}
}

func mappingTestObjective(id string, matches ...framework.EvidenceMatch) framework.ObjectiveAssessment {
	status := framework.ObjectiveNotDetected
	if len(matches) > 0 {
		status = framework.ObjectiveCandidate
	}
	return framework.ObjectiveAssessment{
		ID: id, Title: id, SourceReference: "Article 14", Status: status, Matches: matches,
		Applicability: framework.ObjectiveApplicability{Scope: framework.ApplicabilityHighRiskSystem},
	}
}

func mappingTestMatch(fingerprint, path string) framework.EvidenceMatch {
	return framework.EvidenceMatch{Fingerprint: fingerprint, Path: path, Kind: "source", StartLine: 1}
}

func mappingTestInventory() inventory.Report {
	return inventory.Report{Components: []inventory.Component{{
		Name: "OpenAI", Kind: inventory.KindProvider, Confidence: "high", Locations: []inventory.Location{
			{Path: "apps/support/model.go", Line: 1, Scope: inventory.ScopeRuntime},
			{Path: "apps/ranking/model.go", Line: 1, Scope: inventory.ScopeRuntime},
		},
	}}}
}
