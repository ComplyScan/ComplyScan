package cli

import (
	"context"
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

func promptSetupScanMode(prompt promptSession, summary setupRepositorySummary, provider string, modelReady bool) (setupScanMode, error) {
	if err := explainSetupQuestion(prompt, "scan-mode"); err != nil {
		return setupScanNone, err
	}
	quick := "Quick scan — deterministic discovery and checks; no model"
	deep := "Deep AI review — bounded semantic review after the preliminary report"
	none := "Save setup without scanning"
	defaultMode := quick
	if provider != "none" && modelReady {
		defaultMode = deep
	}
	selected, err := promptChoice(prompt, "first-run action", defaultMode, quick, deep, none)
	if err != nil {
		return setupScanNone, err
	}
	switch selected {
	case deep:
		if _, err := fmt.Fprintf(prompt.output,
			"\nDeep review will inspect model-generated context for this repository after saving the preliminary report. Local duration depends on hardware, model, and the number of evidence targets; it may take many minutes.\n"); err != nil {
			return setupScanNone, err
		}
		return setupScanDeep, nil
	case none:
		return setupScanNone, nil
	default:
		return setupScanQuick, nil
	}
}

func inspectRepositoryForSetup(ctx context.Context, prompt promptSession, target string, cfg config.Config, build BuildInfo) (setupRepositorySummary, error) {
	if _, err := fmt.Fprintln(prompt.output, "Inspecting the repository before asking setup questions..."); err != nil {
		return setupRepositorySummary{}, err
	}
	excludes := withGeneratedReportExclusion(append([]string(nil), cfg.Scan.Exclude...))
	if cfg.Baseline != "" {
		if exclusion := targetExclusion(target, resolveTargetPath(target, cfg.Baseline)); exclusion != "" {
			excludes = append(excludes, exclusion)
		}
	}
	result, err := discovery.Discover(ctx, target, discovery.Options{
		Exclude:                   excludes,
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

func collectBasicSystemProfile(prompt promptSession, target string, now time.Time, summary setupRepositorySummary, draft setupProfileDraft) (profile.System, error) {
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
	prompt, err = prompt.startQuestionGroup("System questionnaire", 7, true)
	if err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(prompt.output,
		"Repository-evident technical facts are shown separately. These short questions collect facts that source code cannot reliably establish.\n"+
			"Use the advanced setup later for detailed data, deployment, supply-chain, and reviewed applicability records."); err != nil {
		return profile.System{}, err
	}

	if err := explainSetupQuestion(prompt, "system-name"); err != nil {
		return profile.System{}, err
	}
	if value.Name, err = prompt.text("System name", value.Name); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "intended-purpose"); err != nil {
		return profile.System{}, err
	}
	if err := draft.explain(prompt.output, "intended-purpose"); err != nil {
		return profile.System{}, err
	}
	if value.IntendedPurpose, err = prompt.text("Intended purpose", draft.first("intended-purpose", "unknown")); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "operating-regions"); err != nil {
		return profile.System{}, err
	}
	if value.OperatingRegions, err = promptChoices(prompt, "Operating regions", []profile.OperatingRegion{profile.RegionUnknown},
		profile.RegionEU, profile.RegionEEA, profile.RegionUK, profile.RegionUS, profile.RegionGlobal, profile.RegionOther, profile.RegionUnknown); err != nil {
		return profile.System{}, err
	}

	if err := explainSetupQuestion(prompt, "organization-role-basic"); err != nil {
		return profile.System{}, err
	}
	const (
		providerOption = "We develop, brand, or provide this AI system"
		deployerOption = "We use an AI system provided by another organisation"
		bothOption     = "We both provide and professionally use the system"
		unknownOption  = "The organisational role has not been established"
	)
	role, err := promptChoice(prompt, "organisation role", unknownOption, providerOption, deployerOption, bothOption, unknownOption)
	if err != nil {
		return profile.System{}, err
	}
	switch role {
	case providerOption:
		value.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider}
	case deployerOption:
		value.OrganizationRoles = []profile.OrganizationRole{profile.RoleDeployer}
	case bothOption:
		value.OrganizationRoles = []profile.OrganizationRole{profile.RoleProvider, profile.RoleDeployer}
	default:
		value.OrganizationRoles = []profile.OrganizationRole{profile.RoleUnknown}
	}

	if err := explainSetupQuestion(prompt, "decision-impact"); err != nil {
		return profile.System{}, err
	}
	if err := draft.explain(prompt.output, "decision-impact"); err != nil {
		return profile.System{}, err
	}
	if value.DecisionImpact, err = promptChoice(prompt, "Decision impact", draft.decisionImpact(profile.ImpactUnknown),
		profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "lifecycle-stage"); err != nil {
		return profile.System{}, err
	}
	if err := draft.explain(prompt.output, "lifecycle-stage"); err != nil {
		return profile.System{}, err
	}
	if value.LifecycleStage, err = promptChoice(prompt, "Lifecycle stage", draft.lifecycle(profile.LifecycleUnknown),
		profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "human-oversight"); err != nil {
		return profile.System{}, err
	}
	if err := draft.explain(prompt.output, "human-oversight"); err != nil {
		return profile.System{}, err
	}
	if value.HumanOversight, err = promptChoice(prompt, "Human oversight", draft.humanOversight(profile.OversightUnknown),
		profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown); err != nil {
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
	if peopleContext {
		if err = explainSetupQuestion(prompt, "users"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "users"); err != nil {
			return err
		}
		if system.Users, err = prompt.textList("Users", draft.values("users", system.Users)); err != nil {
			return err
		}
		if err = explainSetupQuestion(prompt, "affected-groups"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "affected-groups"); err != nil {
			return err
		}
		if system.AffectedGroups, err = prompt.textList("Potentially affected groups", draft.values("affected-groups", system.AffectedGroups)); err != nil {
			return err
		}
	}
	if dataContext {
		if err = explainSetupQuestion(prompt, "personal-data"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "personal-data"); err != nil {
			return err
		}
		if system.Data.PersonalData, err = promptChoice(prompt, "Processes personal data", draft.triState("personal-data", system.Data.PersonalData), profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
			return err
		}
		if err = explainSetupQuestion(prompt, "special-category-data"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "special-category-data"); err != nil {
			return err
		}
		if system.Data.SpecialCategoryData, err = promptChoice(prompt, "Processes special-category or similarly sensitive data", draft.triState("special-category-data", system.Data.SpecialCategoryData), profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
			return err
		}
		if err = explainSetupQuestion(prompt, "children-data"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "children-data"); err != nil {
			return err
		}
		if system.Data.ChildrenData, err = promptChoice(prompt, "Processes children's data", draft.triState("children-data", system.Data.ChildrenData), profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
			return err
		}
	}

	if err = explainSetupQuestion(prompt, "profile-reviewer"); err != nil {
		return err
	}
	reviewer, err := prompt.text("Factual profile reviewer (leave `unknown` to keep this unreviewed)", "unknown")
	if err != nil {
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
	var err error
	if includeUseCase {
		if err = explainSetupQuestion(prompt, "use-case-domains"); err != nil {
			return err
		}
		if err = draft.explain(prompt.output, "use-case-domains"); err != nil {
			return err
		}
		if system.UseCaseDomains, err = promptChoices(prompt, "Use-case domains", draft.useCaseDomains(system.UseCaseDomains),
			profile.DomainBiometrics, profile.DomainCriticalInfrastructure, profile.DomainEducation, profile.DomainEmployment,
			profile.DomainEssentialServices, profile.DomainLawEnforcement, profile.DomainMigrationBorderControl,
			profile.DomainJusticeDemocraticProcess, profile.DomainHealthcare, profile.DomainSoftwareDevelopment,
			profile.DomainGeneralPurpose, profile.DomainOther, profile.DomainUnknown); err != nil {
			return err
		}
	}
	if err = explainSetupQuestion(prompt, "ai-activities"); err != nil {
		return err
	}
	if err = draft.explain(prompt.output, "ai-activities"); err != nil {
		return err
	}
	if system.AIActivities, err = promptChoices(prompt, "AI activities", draft.aiActivities(system.AIActivities),
		profile.ActivityInference, profile.ActivityTraining, profile.ActivityFineTuning, profile.ActivityEvaluation,
		profile.ActivityAutomatedDecision, profile.ActivityAgentToolUse, profile.ActivitySyntheticContent, profile.ActivityUnknown); err != nil {
		return err
	}
	if err = explainSetupQuestion(prompt, "deployment-models"); err != nil {
		return err
	}
	if err = draft.explain(prompt.output, "deployment-models"); err != nil {
		return err
	}
	if system.DeploymentModels, err = promptChoices(prompt, "Deployment models", draft.deploymentModels(system.DeploymentModels),
		profile.DeploymentInternal, profile.DeploymentPrivateCustomer, profile.DeploymentPublic, profile.DeploymentOpenSource,
		profile.DeploymentEmbedded, profile.DeploymentAPI, profile.DeploymentLocalCLI, profile.DeploymentUnknown); err != nil {
		return err
	}
	return nil
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
	useRecommended, err := prompt.confirm("Use the recommended mapping", true)
	if err != nil {
		return err
	}
	if useRecommended {
		cfg.Frameworks = recommended
		return nil
	}
	selected, err := promptFrameworkSelection(prompt, recommended)
	if err != nil {
		return err
	}
	cfg.Frameworks = selected
	return nil
}

func applyFrameworksToSystem(system *profile.System, frameworks []string) {
	if frameworkEnabled(frameworks, framework.EUAIActTechnicalEvidencePackID) {
		system.Applicability = []profile.ApplicabilityDecision{{Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityNeedsReview}}
		return
	}
	system.Applicability = nil
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
