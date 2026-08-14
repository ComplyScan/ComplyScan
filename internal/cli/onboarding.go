package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
)

type setupRepositorySummary struct {
	Discovery     discovery.Result
	Inventory     inventory.Report
	Languages     []string
	SourceFiles   int
	TestFiles     int
	Documentation int
	Configuration int
}

type setupScanMode string

const (
	setupScanQuick setupScanMode = "quick"
	setupScanDeep  setupScanMode = "deep"
	setupScanNone  setupScanMode = "none"
)

func inspectRepositoryForSetup(ctx context.Context, prompt promptSession, target string, activeConfigPath string, cfg config.Config, build BuildInfo) (setupRepositorySummary, error) {
	if _, err := fmt.Fprintln(prompt.output, "Inspecting repository files and technical AI signals locally. No model is used in this step."); err != nil {
		return setupRepositorySummary{}, err
	}
	excludes := withGeneratedReportExclusion(append([]string(nil), cfg.Scan.Exclude...))
	activeConfigExclusion := resolvedPathExclusion(target, activeConfigPath)
	if cfg.Baseline != "" {
		if exclusion := targetExclusion(target, resolveTargetPath(target, cfg.Baseline)); exclusion != "" {
			excludes = append(excludes, exclusion)
		}
	}
	result, err := discovery.Discover(ctx, target, discovery.Options{
		Exclude:                   excludes,
		ExcludeFiles:              nonEmptyValues(activeConfigExclusion),
		MaxFiles:                  cfg.Scan.MaxFiles,
		MaxTotalBytes:             cfg.Scan.MaxTotalBytes,
		IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories,
		TrackedOnly:               cfg.Scan.TrackedOnly,
	})
	if err != nil {
		return setupRepositorySummary{}, fmt.Errorf("inspect repository for setup: %w", err)
	}
	summary := setupRepositorySummary{
		Discovery: result,
		Inventory: inventory.NewReport(target, build.Version, inventory.Analyze(result.Repository), result.Warnings),
	}
	languages := map[string]struct{}{}
	for _, file := range result.Repository.Files {
		if language := setupLanguage(file.Path); language != "" {
			languages[language] = struct{}{}
		}
		switch {
		case setupTestPath(file.Path):
			summary.TestFiles++
		case file.Kind == discovery.KindDocumentation || file.Kind == discovery.KindReadme || setupDocumentationPath(file.Path):
			summary.Documentation++
		case file.Kind == discovery.KindSource:
			summary.SourceFiles++
		default:
			summary.Configuration++
		}
	}
	for language := range languages {
		summary.Languages = append(summary.Languages, language)
	}
	sort.Strings(summary.Languages)
	if err := writeSetupRepositorySummary(prompt, summary); err != nil {
		return setupRepositorySummary{}, err
	}
	return summary, nil
}

func ensureRepositoryDraftSystem(prompt promptSession, target string, cfg *config.Config, summary setupRepositorySummary) error {
	if len(cfg.Systems) > 0 {
		applyFrameworksToSystems(cfg.Systems, cfg.Frameworks)
		return prompt.status(setupStatusReady, fmt.Sprintf("Kept %d existing report target(s); detailed profile facts were not changed.", len(cfg.Systems)))
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve setup target: %w", err)
	}
	name := filepath.Base(absolute)
	id := profile.SlugID(name)
	if id == "" {
		id = "system"
	}
	system := profile.NewDraftSystem(id, name)
	if summary.Inventory.Summary.RuntimeSignals > 0 {
		system.AIActivities = []profile.AIActivity{profile.ActivityInference}
	}
	applyFrameworksToSystem(&system, cfg.Frameworks)
	if err := system.Validate(); err != nil {
		return fmt.Errorf("validate repository draft system: %w", err)
	}
	cfg.Systems = append(cfg.Systems, system)
	if err := prompt.status(setupStatusReview, fmt.Sprintf("Report target: %q (created from the repository name).", system.Name)); err != nil {
		return err
	}
	_, err = fmt.Fprintln(prompt.output,
		"  ComplyScan uses this internal draft to attach code evidence to a named system in reports. It does not infer legal applicability.\n"+
			"  Business, jurisdictional, and legal-applicability facts remain unconfirmed and are not required for this code scan.\n"+
			"  Use `complyscan profile setup --replace` or rerun `complyscan setup --advanced` when a detailed profile is needed.")
	return err
}

func writeSetupRepositorySummary(prompt promptSession, summary setupRepositorySummary) error {
	languages := "not identified"
	if len(summary.Languages) > 0 {
		languages = strings.Join(summary.Languages, ", ")
	}
	if err := prompt.sectionTitle(fmt.Sprintf("Repository inspected: %d files, %s", summary.Discovery.Stats.FilesRead, formatByteCount(summary.Discovery.Stats.BytesRead)), true); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(prompt.output,
		"  Languages: %s\n"+
			"  Source: %d  Tests: %d  Documentation: %d  Configuration/other: %d\n"+
			"  AI components: %d  Technical AI signals: %d\n",
		languages, summary.SourceFiles, summary.TestFiles, summary.Documentation, summary.Configuration,
		summary.Inventory.Summary.Components, summary.Inventory.Summary.Signals); err != nil {
		return err
	}
	if len(summary.Inventory.Components) == 0 {
		_, err := fmt.Fprintln(prompt.output, "  No supported AI provider or framework was identified during preliminary discovery.")
		return err
	}
	names := make([]string, 0, len(summary.Inventory.Components))
	for _, component := range summary.Inventory.Components {
		names = append(names, component.Name)
	}
	_, err := fmt.Fprintf(prompt.output, "  Detected AI components: %s\n", strings.Join(names, ", "))
	return err
}

func collectBasicSystemProfile(prompt promptSession, target string, now time.Time, summary setupRepositorySummary, draft setupProfileDraft, existing *profile.System) (profile.System, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return profile.System{}, fmt.Errorf("resolve setup target: %w", err)
	}
	name := filepath.Base(absolute)
	id := profile.SlugID(name)
	if id == "" {
		id = "system"
	}
	value := profile.NewDraftSystem(id, name)
	if existing != nil {
		value = *existing
		if strings.TrimSpace(value.ID) == "" {
			value.ID = id
		}
		if strings.TrimSpace(value.Name) == "" {
			value.Name = name
		}
	}
	prompt, err = prompt.startQuestionGroup("System questionnaire", 7, true)
	if err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(prompt.output,
		"Repository-evident technical facts are shown separately. These short questions collect facts that source code cannot reliably establish.\n"+
			"Use the advanced setup later for detailed data, deployment, ownership, and reviewed applicability records."); err != nil {
		return profile.System{}, err
	}
	value.IntendedPurpose = draft.first("intended-purpose", value.IntendedPurpose)
	completed := make([]bool, 7)
	err = runSetupPromptSteps(prompt, false,
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "system-name"); err != nil {
				return err
			}
			answer, err := step.text("System name", value.Name)
			if err == nil {
				value.Name, completed[0] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "intended-purpose"); err != nil {
				return err
			}
			if err := draft.explain(step, "intended-purpose"); err != nil {
				return err
			}
			answer, err := step.text("Intended purpose", value.IntendedPurpose)
			if err == nil {
				value.IntendedPurpose, completed[1] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "operating-regions"); err != nil {
				return err
			}
			var answer []profile.OperatingRegion
			var err error
			if completed[2] {
				answer, err = promptChoices(step, "Operating regions", value.OperatingRegions,
					profile.RegionEU, profile.RegionEEA, profile.RegionUK, profile.RegionUS, profile.RegionGlobal, profile.RegionOther, profile.RegionUnknown)
			} else {
				answer, err = promptRequiredChoices(step, "Operating regions",
					profile.RegionEU, profile.RegionEEA, profile.RegionUK, profile.RegionUS, profile.RegionGlobal, profile.RegionOther, profile.RegionUnknown)
			}
			if err == nil {
				value.OperatingRegions, completed[2] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "organization-roles"); err != nil {
				return err
			}
			defaults := []profile.OrganizationRole(nil)
			if completed[3] {
				defaults = value.OrganizationRoles
			}
			answer, err := promptOrganizationRoles(step, defaults)
			if err == nil {
				value.OrganizationRoles, completed[3] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "decision-impact"); err != nil {
				return err
			}
			if err := draft.explain(step, "decision-impact"); err != nil {
				return err
			}
			var answer profile.DecisionImpact
			var err error
			if completed[4] {
				answer, err = promptChoice(step, "Decision impact", value.DecisionImpact,
					profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown)
			} else {
				answer, err = promptRequiredChoice(step, "Decision impact",
					profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown)
			}
			if err == nil {
				value.DecisionImpact, completed[4] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "lifecycle-stage"); err != nil {
				return err
			}
			if err := draft.explain(step, "lifecycle-stage"); err != nil {
				return err
			}
			var answer profile.LifecycleStage
			var err error
			if completed[5] {
				answer, err = promptChoice(step, "Lifecycle stage", value.LifecycleStage,
					profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown)
			} else {
				answer, err = promptRequiredChoice(step, "Lifecycle stage",
					profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown)
			}
			if err == nil {
				value.LifecycleStage, completed[5] = answer, true
			}
			return err
		},
		func(step promptSession) error {
			if err := explainSetupQuestion(step, "human-oversight"); err != nil {
				return err
			}
			if err := draft.explain(step, "human-oversight"); err != nil {
				return err
			}
			var answer profile.HumanOversight
			var err error
			if completed[6] {
				answer, err = promptChoice(step, "Human oversight", value.HumanOversight,
					profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown)
			} else {
				answer, err = promptRequiredChoice(step, "Human oversight",
					profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown)
			}
			if err == nil {
				value.HumanOversight, completed[6] = answer, true
			}
			return err
		},
	)
	if err != nil {
		return profile.System{}, err
	}

	value.ProfileReview = profile.ProfileReview{Status: profile.ReviewDraft}
	value.Applicability = []profile.ApplicabilityDecision{{Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityNeedsReview}}
	if err := value.Validate(); err != nil {
		return profile.System{}, fmt.Errorf("validate quick system profile: %w", err)
	}
	if _, err := fmt.Fprintln(prompt.output,
		"\nInitial context collected. If EU mapping is selected, setup will now ask the remaining conditionally relevant facts."); err != nil {
		return profile.System{}, err
	}
	return value, nil
}

func promptOrganizationRoles(session promptSession, defaults []profile.OrganizationRole) ([]profile.OrganizationRole, error) {
	allowed := []profile.OrganizationRole{
		profile.RoleProvider,
		profile.RoleDeployer,
		profile.RoleImporter,
		profile.RoleDistributor,
		profile.RoleProductManufacturer,
		profile.RoleUnknown,
	}
	if session.selectMany == nil {
		return promptChoices(session, "Organisation roles", defaults, allowed...)
	}
	labels := map[profile.OrganizationRole]string{
		profile.RoleProvider:            "Provider — we build, brand, or supply the AI system",
		profile.RoleDeployer:            "Deployer — we professionally use an AI system supplied by someone else",
		profile.RoleImporter:            "Importer — we bring a non-EU provider’s AI system into the EU market",
		profile.RoleDistributor:         "Distributor — we make another provider’s AI system available in the EU",
		profile.RoleProductManufacturer: "Product manufacturer — we supply the AI system with a product under our name or brand",
		profile.RoleUnknown:             "Unknown — our organisation’s role has not been confirmed",
	}
	defaultValues := make([]string, len(defaults))
	for index, value := range defaults {
		defaultValues[index] = string(value)
	}
	options := make([]terminalChoice, len(allowed))
	for index, value := range allowed {
		options[index] = terminalChoice{Label: labels[value], Value: string(value)}
	}
	selected, err := session.chooseMany(session.nextQuestionLabel("Organisation roles"), defaultValues, options, []string{string(profile.RoleUnknown)})
	if err != nil {
		return nil, err
	}
	result := make([]profile.OrganizationRole, 0, len(selected))
	seen := make(map[profile.OrganizationRole]struct{}, len(selected))
	for _, selectedValue := range selected {
		index, matched := choiceIndex(selectedValue, allowed)
		if !matched {
			return nil, fmt.Errorf("organisation roles selector returned unsupported value %q", selectedValue)
		}
		value := allowed[index]
		if _, duplicate := seen[value]; !duplicate {
			result = append(result, value)
			seen[value] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil, errors.New("organisation roles requires at least one selection")
	}
	return result, nil
}

func collectRelevantEUApplicabilityContext(prompt promptSession, system *profile.System, now time.Time, draft setupProfileDraft) error {
	if err := explainSetupQuestion(prompt, "applicability-context"); err != nil {
		return err
	}
	if err := prompt.sectionTitle("Relevant EU AI Act context", true); err != nil {
		return err
	}
	var err error
	prompt, err = prompt.startQuestionGroup("Technical context", 3, true)
	if err != nil {
		return err
	}
	if err := collectTechnicalSystemContext(prompt, system, draft, true); err != nil {
		return err
	}

	assessment := profile.AssessEUAIAct([]profile.System{*system}).Systems[0]
	peopleContext := needsPeopleContext(*system, assessment.HighRiskScreening)
	dataContext := needsDataContext(*system, assessment.HighRiskScreening)
	followUpQuestions := 1
	if peopleContext {
		followUpQuestions += 2
	}
	if dataContext {
		followUpQuestions += 3
	}
	prompt, err = prompt.startQuestionGroup("Conditional EU follow-up", followUpQuestions, true)
	if err != nil {
		return err
	}
	steps := make([]setupPromptStep, 0, followUpQuestions)
	completed := make([]bool, followUpQuestions)
	stepIndex := 0
	if peopleContext {
		if len(system.Users) == 0 {
			system.Users = draft.values("users", system.Users)
		}
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "users"); err != nil {
				return err
			}
			if err := draft.explain(step, "users"); err != nil {
				return err
			}
			answer, err := step.textList("Users", system.Users)
			if err == nil {
				system.Users = answer
			}
			return err
		})
		stepIndex++
		if len(system.AffectedGroups) == 0 {
			system.AffectedGroups = draft.values("affected-groups", system.AffectedGroups)
		}
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "affected-groups"); err != nil {
				return err
			}
			if err := draft.explain(step, "affected-groups"); err != nil {
				return err
			}
			answer, err := step.textList("Potentially affected groups", system.AffectedGroups)
			if err == nil {
				system.AffectedGroups = answer
			}
			return err
		})
		stepIndex++
	}
	if dataContext {
		personalIndex := stepIndex
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "personal-data"); err != nil {
				return err
			}
			if err := draft.explain(step, "personal-data"); err != nil {
				return err
			}
			answer, err := promptRevisitableRequiredChoice(step, completed[personalIndex], system.Data.PersonalData,
				"Processes personal data", profile.TriYes, profile.TriNo, profile.TriUnknown)
			if err == nil {
				system.Data.PersonalData, completed[personalIndex] = answer, true
			}
			return err
		})
		stepIndex++
		specialIndex := stepIndex
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "special-category-data"); err != nil {
				return err
			}
			if err := draft.explain(step, "special-category-data"); err != nil {
				return err
			}
			answer, err := promptRevisitableRequiredChoice(step, completed[specialIndex], system.Data.SpecialCategoryData,
				"Processes special-category or similarly sensitive data", profile.TriYes, profile.TriNo, profile.TriUnknown)
			if err == nil {
				system.Data.SpecialCategoryData, completed[specialIndex] = answer, true
			}
			return err
		})
		stepIndex++
		childrenIndex := stepIndex
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "children-data"); err != nil {
				return err
			}
			if err := draft.explain(step, "children-data"); err != nil {
				return err
			}
			answer, err := promptRevisitableRequiredChoice(step, completed[childrenIndex], system.Data.ChildrenData,
				"Processes children's data", profile.TriYes, profile.TriNo, profile.TriUnknown)
			if err == nil {
				system.Data.ChildrenData, completed[childrenIndex] = answer, true
			}
			return err
		})
		stepIndex++
	}
	reviewer := "unknown"
	steps = append(steps, func(step promptSession) error {
		if err := explainSetupQuestion(step, "profile-reviewer"); err != nil {
			return err
		}
		answer, err := step.text("Factual profile reviewer (leave `unknown` to keep this unreviewed)", reviewer)
		if err == nil {
			reviewer = answer
		}
		return err
	})
	if err := runSetupPromptSteps(prompt, false, steps...); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(reviewer), "unknown") {
		system.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: reviewer, ReviewedAt: now.Format(time.DateOnly)}
	}
	if err := system.Validate(); err != nil {
		return fmt.Errorf("validate relevant applicability context: %w", err)
	}
	return writeApplicabilityReadinessGate(prompt, *system)
}

func collectNonEUTechnicalContext(prompt promptSession, system *profile.System, draft setupProfileDraft) error {
	if err := prompt.sectionTitle("Repository technical context", true); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(prompt.output, "These answers prioritize voluntary technical recommendations. They remain factual engineering context, not a legal assessment."); err != nil {
		return err
	}
	var err error
	prompt, err = prompt.startQuestionGroup("Technical questionnaire", 2, true)
	if err != nil {
		return err
	}
	if err := collectTechnicalSystemContext(prompt, system, draft, false); err != nil {
		return err
	}
	if err := system.Validate(); err != nil {
		return fmt.Errorf("validate technical context: %w", err)
	}
	return nil
}

func collectTechnicalSystemContext(prompt promptSession, system *profile.System, draft setupProfileDraft, includeUseCase bool) error {
	steps := make([]setupPromptStep, 0, 3)
	completed := make([]bool, 3)
	stepIndex := 0
	if includeUseCase {
		index := stepIndex
		stepIndex++
		steps = append(steps, func(step promptSession) error {
			if err := explainSetupQuestion(step, "use-case-domains"); err != nil {
				return err
			}
			if err := draft.explain(step, "use-case-domains"); err != nil {
				return err
			}
			allowed := []profile.UseCaseDomain{
				profile.DomainBiometrics, profile.DomainCriticalInfrastructure, profile.DomainEducation, profile.DomainEmployment,
				profile.DomainEssentialServices, profile.DomainLawEnforcement, profile.DomainMigrationBorderControl,
				profile.DomainJusticeDemocraticProcess, profile.DomainHealthcare, profile.DomainSoftwareDevelopment,
				profile.DomainGeneralPurpose, profile.DomainOther, profile.DomainUnknown,
			}
			var answer []profile.UseCaseDomain
			var err error
			if completed[index] {
				answer, err = promptChoices(step, "Use-case domains", system.UseCaseDomains, allowed...)
			} else {
				answer, err = promptRequiredChoices(step, "Use-case domains", allowed...)
			}
			if err == nil {
				system.UseCaseDomains, completed[index] = answer, true
			}
			return err
		})
	}
	activityIndex := stepIndex
	stepIndex++
	steps = append(steps, func(step promptSession) error {
		if err := explainSetupQuestion(step, "ai-activities"); err != nil {
			return err
		}
		if err := draft.explain(step, "ai-activities"); err != nil {
			return err
		}
		allowed := []profile.AIActivity{
			profile.ActivityInference, profile.ActivityTraining, profile.ActivityFineTuning, profile.ActivityEvaluation,
			profile.ActivityAutomatedDecision, profile.ActivityAgentToolUse, profile.ActivitySyntheticContent, profile.ActivityUnknown,
		}
		var answer []profile.AIActivity
		var err error
		if completed[activityIndex] {
			answer, err = promptChoices(step, "AI activities", system.AIActivities, allowed...)
		} else {
			answer, err = promptRequiredChoices(step, "AI activities", allowed...)
		}
		if err == nil {
			system.AIActivities, completed[activityIndex] = answer, true
		}
		return err
	})
	deploymentIndex := stepIndex
	steps = append(steps, func(step promptSession) error {
		if err := explainSetupQuestion(step, "deployment-models"); err != nil {
			return err
		}
		if err := draft.explain(step, "deployment-models"); err != nil {
			return err
		}
		allowed := []profile.DeploymentModel{
			profile.DeploymentInternal, profile.DeploymentPrivateCustomer, profile.DeploymentPublic, profile.DeploymentOpenSource,
			profile.DeploymentEmbedded, profile.DeploymentAPI, profile.DeploymentLocalCLI, profile.DeploymentUnknown,
		}
		var answer []profile.DeploymentModel
		var err error
		if completed[deploymentIndex] {
			answer, err = promptChoices(step, "Deployment models", system.DeploymentModels, allowed...)
		} else {
			answer, err = promptRequiredChoices(step, "Deployment models", allowed...)
		}
		if err == nil {
			system.DeploymentModels, completed[deploymentIndex] = answer, true
		}
		return err
	})
	return runSetupPromptSteps(prompt, false, steps...)
}

func writeApplicabilityReadinessGate(prompt promptSession, system profile.System) error {
	assessment := profile.AssessEUAIAct([]profile.System{system}).Systems[0]
	if err := prompt.sectionTitle(fmt.Sprintf("Applicability readiness gate: %s", assessment.MappingReadiness), true); err != nil {
		return err
	}
	switch assessment.MappingReadiness {
	case profile.MappingHumanReviewed:
		_, err := fmt.Fprintln(prompt.output, "  The factual inputs needed by the current EU technical control pack are present and have a named human reviewer.")
		return err
	case profile.MappingFactuallyReady:
		_, err := fmt.Fprintln(prompt.output, "  The needed factual inputs are present, but the profile and legal applicability decision still require accountable human review.")
		return err
	default:
		if _, err := fmt.Fprintln(prompt.output, "  Technical scanning can continue, but requirement mapping remains provisional because these facts are unresolved:"); err != nil {
			return err
		}
		for _, missing := range assessment.MissingContext {
			if _, err := fmt.Fprintf(prompt.output, "  - %s\n", missing); err != nil {
				return err
			}
		}
		return nil
	}
}

func needsPeopleContext(system profile.System, screening profile.HighRiskScreening) bool {
	return screening == profile.HighRiskPotential || system.DecisionImpact == profile.ImpactSignificant || system.DecisionImpact == profile.ImpactAutonomous
}

func needsDataContext(system profile.System, screening profile.HighRiskScreening) bool {
	if screening == profile.HighRiskPotential {
		return true
	}
	for _, activity := range system.AIActivities {
		if activity == profile.ActivityTraining || activity == profile.ActivityFineTuning || activity == profile.ActivityEvaluation || activity == profile.ActivityAutomatedDecision {
			return true
		}
	}
	return false
}

func recommendFrameworks(system profile.System) ([]string, string) {
	for _, region := range system.OperatingRegions {
		if region == profile.RegionEU || region == profile.RegionEEA || region == profile.RegionGlobal || region == profile.RegionUnknown {
			return []string{framework.EUAIActTechnicalEvidencePackID}, "EU AI Act technical mapping is recommended because EU/EEA effects are selected or have not yet been ruled out."
		}
	}
	return []string{framework.NISTAIRMFTechnicalEvidencePackID}, "NIST AI RMF technical mapping is recommended as voluntary engineering guidance; no EU/EEA operation was declared."
}

func configureRecommendedFrameworks(prompt promptSession, cfg *config.Config, system profile.System, explicit []string) error {
	if len(explicit) > 0 {
		if err := configureFrameworkSelection(prompt, cfg, false, explicit); err != nil {
			return err
		}
		if frameworkEnabled(cfg.Frameworks, framework.NISTAIRMFTechnicalEvidencePackID) {
			if _, err := fmt.Fprintln(prompt.output, "\nUsing the explicitly selected NIST AI RMF technical mapping. NIST AI RMF is voluntary guidance, not a legal applicability decision."); err != nil {
				return err
			}
		}
		return nil
	}
	recommended, reason := recommendFrameworks(system)
	if err := prompt.sectionTitle("Recommended technical mapping", true); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(prompt.output, "  %s\n", reason); err != nil {
		return err
	}
	for {
		useRecommended, err := prompt.confirm("Use the recommended mapping", true)
		if err != nil {
			return err
		}
		if useRecommended {
			cfg.Frameworks = recommended
			return nil
		}
		selectionPrompt := prompt
		selectionPrompt.backAvailable = true
		selected, err := promptFrameworkSelection(selectionPrompt, recommended)
		if errors.Is(err, errPromptBack) {
			continue
		}
		if err != nil {
			return err
		}
		cfg.Frameworks = selected
		return nil
	}
}

func applyFrameworksToSystem(system *profile.System, frameworks []string) {
	if frameworkEnabled(frameworks, framework.EUAIActTechnicalEvidencePackID) {
		for _, decision := range system.Applicability {
			if decision.Framework == profile.FrameworkEUAIAct {
				return
			}
		}
		system.Applicability = append(system.Applicability, profile.ApplicabilityDecision{Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityNeedsReview})
		return
	}
	filtered := system.Applicability[:0]
	for _, decision := range system.Applicability {
		if decision.Framework != profile.FrameworkEUAIAct {
			filtered = append(filtered, decision)
		}
	}
	system.Applicability = filtered
}

func applyFrameworksToSystems(systems []profile.System, frameworks []string) {
	for index := range systems {
		applyFrameworksToSystem(&systems[index], frameworks)
	}
}

func setupLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "Go"
	case ".py":
		return "Python"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "JavaScript"
	case ".ts", ".tsx", ".mts", ".cts":
		return "TypeScript"
	case ".java":
		return "Java"
	case ".rs":
		return "Rust"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".cs":
		return "C#"
	default:
		return ""
	}
}

func setupTestPath(path string) bool {
	lower := "/" + strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(path))
	return strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") || strings.Contains(lower, "/__tests__/") ||
		strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

func setupDocumentationPath(path string) bool {
	lower := "/" + strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(lower, "/docs/") || strings.Contains(lower, "/doc/") || strings.Contains(lower, "/examples/") || strings.Contains(lower, "/example/")
}
