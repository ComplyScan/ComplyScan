package cli

import (
	"context"
	"fmt"
	"io"
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

func promptSetupScanMode(prompt promptSession, summary setupRepositorySummary) (setupScanMode, error) {
	if err := explainSetupQuestion(prompt, "scan-mode"); err != nil {
		return setupScanNone, err
	}
	quick := "Quick scan — deterministic discovery and checks; no model"
	deep := "Deep AI review — bounded semantic review after the preliminary report"
	none := "Save setup without scanning"
	selected, err := promptChoice(prompt, "first-run action", quick, quick, deep, none)
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

func inspectRepositoryForSetup(ctx context.Context, output io.Writer, target string, cfg config.Config, build BuildInfo) (setupRepositorySummary, error) {
	if _, err := fmt.Fprintln(output, "Inspecting the repository before asking setup questions..."); err != nil {
		return setupRepositorySummary{}, err
	}
	result, err := discovery.Discover(ctx, target, discovery.Options{
		Exclude:                   withGeneratedReportExclusion(cfg.Scan.Exclude),
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
	if err := writeSetupRepositorySummary(output, summary); err != nil {
		return setupRepositorySummary{}, err
	}
	return summary, nil
}

func writeSetupRepositorySummary(output io.Writer, summary setupRepositorySummary) error {
	languages := "not identified"
	if len(summary.Languages) > 0 {
		languages = strings.Join(summary.Languages, ", ")
	}
	if _, err := fmt.Fprintf(output,
		"\nRepository inspected: %d files, %s\n"+
			"  Languages: %s\n"+
			"  Source: %d  Tests: %d  Documentation: %d  Configuration/other: %d\n"+
			"  AI components: %d  Technical AI signals: %d\n",
		summary.Discovery.Stats.FilesRead, formatByteCount(summary.Discovery.Stats.BytesRead), languages,
		summary.SourceFiles, summary.TestFiles, summary.Documentation, summary.Configuration,
		summary.Inventory.Summary.Components, summary.Inventory.Summary.Signals); err != nil {
		return err
	}
	if len(summary.Inventory.Components) == 0 {
		_, err := fmt.Fprintln(output, "  No supported AI provider or framework was identified during preliminary discovery.")
		return err
	}
	names := make([]string, 0, len(summary.Inventory.Components))
	for _, component := range summary.Inventory.Components {
		names = append(names, component.Name)
	}
	_, err := fmt.Fprintf(output, "  Detected AI components: %s\n", strings.Join(names, ", "))
	return err
}

func collectBasicSystemProfile(prompt promptSession, target string, now time.Time, summary setupRepositorySummary) (profile.System, error) {
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
	if _, err := fmt.Fprintln(prompt.output,
		"\nQuick system setup\n"+
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
	if value.IntendedPurpose, err = prompt.text("Intended purpose", "unknown"); err != nil {
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
	if value.DecisionImpact, err = promptChoice(prompt, "Decision impact", profile.ImpactUnknown,
		profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "lifecycle-stage"); err != nil {
		return profile.System{}, err
	}
	if value.LifecycleStage, err = promptChoice(prompt, "Lifecycle stage", profile.LifecycleUnknown,
		profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown); err != nil {
		return profile.System{}, err
	}
	if err := explainSetupQuestion(prompt, "human-oversight"); err != nil {
		return profile.System{}, err
	}
	if value.HumanOversight, err = promptChoice(prompt, "Human oversight", profile.OversightUnknown,
		profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown); err != nil {
		return profile.System{}, err
	}

	if summary.Inventory.Summary.RuntimeSignals > 0 {
		if _, err := fmt.Fprintf(prompt.output,
			"\nTechnical suggestion: runtime AI-provider or framework usage was detected. This supports an inference candidate, but does not prove how the product uses it.\n"); err != nil {
			return profile.System{}, err
		}
		confirmed, err := prompt.confirm("Record model inference as a human-confirmed setup fact", false)
		if err != nil {
			return profile.System{}, err
		}
		if confirmed {
			value.AIActivities = []profile.AIActivity{profile.ActivityInference}
		}
	}

	value.ProfileReview = profile.ProfileReview{Status: profile.ReviewDraft}
	value.Applicability = []profile.ApplicabilityDecision{{Framework: profile.FrameworkEUAIAct, Status: profile.ApplicabilityNeedsReview}}
	if err := value.Validate(); err != nil {
		return profile.System{}, fmt.Errorf("validate quick system profile: %w", err)
	}
	if _, err := fmt.Fprintln(prompt.output,
		"\nQuick context collected. Detailed fields remain explicitly unknown until a human completes advanced setup or reviews them in the future dashboard."); err != nil {
		return profile.System{}, err
	}
	return value, nil
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
	if _, err := fmt.Fprintf(prompt.output, "\nRecommended technical mapping\n  %s\n", reason); err != nil {
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
