package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/spf13/cobra"
)

func newInitCommand(stdout io.Writer) *cobra.Command {
	var (
		force              bool
		forceInteractive   bool
		nonInteractive     bool
		selectedFrameworks []string
	)
	command := &cobra.Command{
		Use:   "init [path]",
		Short: "Create configuration and collect system applicability context",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if forceInteractive && nonInteractive {
				return errors.New("--interactive and --non-interactive cannot be used together")
			}
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			info, err := os.Stat(target)
			if err != nil {
				return fmt.Errorf("inspect init target %q: %w", target, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("init target %q is not a directory", target)
			}
			path := filepath.Join(target, config.FileName)
			if !force {
				if _, err := os.Stat(path); err == nil {
					return fmt.Errorf("%s already exists (use --force to overwrite)", path)
				} else if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("inspect config %q: %w", path, err)
				}
			}

			cfg := config.Default()
			interactive := forceInteractive || (!nonInteractive && isInteractiveReader(cmd.InOrStdin()))
			prompt := newPromptSession(cmd.InOrStdin(), stdout)
			if err := configureFrameworkSelection(prompt, &cfg, interactive, selectedFrameworks); err != nil {
				return err
			}
			if interactive {
				system, err := collectSystemProfileWithPrompt(prompt, target, time.Now(), cfg.Frameworks...)
				if err != nil {
					return err
				}
				cfg.Systems = []profile.System{system}
			} else {
				if _, err := fmt.Fprintln(stdout, "Non-interactive setup: no system profile was collected. Run `complyscan profile setup` from a terminal to add applicability context."); err != nil {
					return err
				}
			}
			if err := config.Write(path, cfg, force); err != nil {
				return err
			}
			if err := ensureReportGitIgnore(target); err != nil {
				return fmt.Errorf("created %s but could not ignore generated reports: %w", path, err)
			}
			_, err = fmt.Fprintf(stdout, "Created %s with %d system profile(s)\n", path, len(cfg.Systems))
			return err
		},
	}
	command.Flags().BoolVar(&force, "force", false, "overwrite an existing configuration")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "collect system context even when input is redirected")
	command.Flags().BoolVar(&nonInteractive, "non-interactive", false, "create scanner configuration without asking setup questions")
	command.Flags().StringSliceVar(&selectedFrameworks, "framework", nil, "built-in technical evidence pack to enable (repeatable)")
	return command
}

func configureFrameworkSelection(prompt promptSession, cfg *config.Config, interactive bool, explicit []string) error {
	if interactive {
		if err := prompt.sectionTitle("Choose technical mappings", true); err != nil {
			return err
		}
		if err := explainSetupQuestion(prompt, "frameworks"); err != nil {
			return err
		}
	}
	if len(explicit) > 0 {
		cfg.Frameworks = append([]string(nil), explicit...)
		return cfg.Validate()
	}
	if !interactive {
		return nil
	}
	selected, err := promptFrameworkSelection(prompt, nil)
	if err != nil {
		return err
	}
	cfg.Frameworks = selected
	return nil
}

func promptFrameworkSelection(prompt promptSession, defaults []string) ([]string, error) {
	const (
		euOption   = "EU AI Act — code evidence linked to potential legal obligations"
		nistOption = "NIST AI RMF — voluntary technical practices"
	)
	defaultSelections := make([]string, 0, 2)
	if containsFramework(defaults, framework.EUAIActTechnicalEvidencePackID) {
		defaultSelections = append(defaultSelections, euOption)
	}
	if containsFramework(defaults, framework.NISTAIRMFTechnicalEvidencePackID) {
		defaultSelections = append(defaultSelections, nistOption)
	}
	selections, err := promptChoices(prompt, "Technical evidence packs", defaultSelections, euOption, nistOption)
	if err != nil {
		return nil, err
	}
	selected := make([]string, 0, len(selections))
	for _, selection := range selections {
		switch selection {
		case euOption:
			selected = append(selected, framework.EUAIActTechnicalEvidencePackID)
		case nistOption:
			selected = append(selected, framework.NISTAIRMFTechnicalEvidencePackID)
		}
	}
	return selected, nil
}

func containsFramework(frameworks []string, wanted string) bool {
	for _, id := range frameworks {
		if id == wanted {
			return true
		}
	}
	return false
}

const reportGitIgnoreEntry = "/.complyscan/reports/"

func ensureReportGitIgnore(target string) error {
	path := filepath.Join(target, ".gitignore")
	mode := os.FileMode(0o644)
	content := []byte{}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("%s must be a regular file and must not be a symlink", path)
		}
		mode = info.Mode().Perm()
		content, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		normalized := strings.Trim(strings.TrimSpace(line), "/")
		if normalized == ".complyscan/reports" {
			return nil
		}
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, reportGitIgnoreEntry...)
	content = append(content, '\n')

	temporary, err := os.CreateTemp(target, ".complyscan-gitignore-*")
	if err != nil {
		return fmt.Errorf("create temporary .gitignore: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary .gitignore permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write temporary .gitignore: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary .gitignore: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary .gitignore: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func collectSystemProfile(input io.Reader, output io.Writer, target string, now time.Time) (profile.System, error) {
	return collectSystemProfileWithPrompt(newPromptSession(input, output), target, now)
}

func collectSystemProfileWithPrompt(prompt promptSession, target string, now time.Time, enabledFrameworks ...string) (profile.System, error) {
	output := prompt.output
	absolute, err := filepath.Abs(target)
	if err != nil {
		return profile.System{}, fmt.Errorf("resolve init target: %w", err)
	}
	defaultName := filepath.Base(absolute)
	defaultID := profile.SlugID(defaultName)
	if defaultID == "" {
		defaultID = "system"
	}
	value := profile.NewDraftSystem(defaultID, defaultName)
	if !frameworkEnabled(enabledFrameworks, framework.EUAIActTechnicalEvidencePackID) {
		value.Applicability = nil
	}
	prompt, err = prompt.startQuestionGroup("Advanced system questionnaire", 17, true)
	if err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(output, "Answer factual questions. Use `unknown` when context has not been established; do not enter secrets or personal records."); err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(output, "The explanations translate AI governance and regulatory screening concepts into developer language. They guide fact collection but are not legal advice."); err != nil {
		return profile.System{}, err
	}
	if frameworkEnabled(enabledFrameworks, framework.EUAIActTechnicalEvidencePackID) {
		if _, err := fmt.Fprintln(output, "EU reference: Regulation (EU) 2024/1689, especially Article 3 and Annex III: https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng"); err != nil {
			return profile.System{}, err
		}
	}
	if _, err := fmt.Fprintln(output); err != nil {
		return profile.System{}, err
	}

	value.IntendedPurpose = "unknown"
	value.Users = []string{"unknown"}
	value.AffectedGroups = []string{"unknown"}
	reviewer := "unknown"
	completed := make([]bool, 12)
	steps := []setupPromptStep{
		setupTextPromptStep("system-id", "System ID", &value.ID),
		setupTextPromptStep("system-name", "System name", &value.Name),
		setupTextPromptStep("intended-purpose", "Intended purpose", &value.IntendedPurpose),
		setupRequiredChoicePromptStep("lifecycle-stage", "Lifecycle stage", &value.LifecycleStage, &completed[0],
			profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown),
		setupRequiredChoicesPromptStep("organization-roles", "Organization roles", &value.OrganizationRoles, &completed[1],
			profile.RoleProvider, profile.RoleDeployer, profile.RoleImporter, profile.RoleDistributor, profile.RoleProductManufacturer, profile.RoleUnknown),
		setupRequiredChoicesPromptStep("operating-regions", "Operating regions", &value.OperatingRegions, &completed[2],
			profile.RegionEU, profile.RegionEEA, profile.RegionUK, profile.RegionUS, profile.RegionGlobal, profile.RegionOther, profile.RegionUnknown),
		setupRequiredChoicesPromptStep("use-case-domains", "Use-case domains", &value.UseCaseDomains, &completed[3],
			profile.DomainBiometrics, profile.DomainCriticalInfrastructure, profile.DomainEducation, profile.DomainEmployment,
			profile.DomainEssentialServices, profile.DomainLawEnforcement, profile.DomainMigrationBorderControl,
			profile.DomainJusticeDemocraticProcess, profile.DomainHealthcare, profile.DomainSoftwareDevelopment,
			profile.DomainGeneralPurpose, profile.DomainOther, profile.DomainUnknown),
		setupTextListPromptStep("users", "Users", &value.Users),
		setupTextListPromptStep("affected-groups", "Potentially affected groups", &value.AffectedGroups),
		setupRequiredChoicePromptStep("decision-impact", "Decision impact", &value.DecisionImpact, &completed[4],
			profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown),
		setupRequiredChoicePromptStep("human-oversight", "Human oversight", &value.HumanOversight, &completed[5],
			profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown),
		setupRequiredChoicesPromptStep("ai-activities", "AI activities", &value.AIActivities, &completed[6],
			profile.ActivityInference, profile.ActivityTraining, profile.ActivityFineTuning, profile.ActivityEvaluation,
			profile.ActivityAutomatedDecision, profile.ActivityAgentToolUse, profile.ActivitySyntheticContent, profile.ActivityUnknown),
		setupRequiredChoicePromptStep("personal-data", "Processes personal data", &value.Data.PersonalData, &completed[7],
			profile.TriYes, profile.TriNo, profile.TriUnknown),
		setupRequiredChoicePromptStep("special-category-data", "Processes special-category or similarly sensitive data", &value.Data.SpecialCategoryData, &completed[8],
			profile.TriYes, profile.TriNo, profile.TriUnknown),
		setupRequiredChoicePromptStep("children-data", "Processes children's data", &value.Data.ChildrenData, &completed[9],
			profile.TriYes, profile.TriNo, profile.TriUnknown),
		setupRequiredChoicesPromptStep("deployment-models", "Deployment models", &value.DeploymentModels, &completed[10],
			profile.DeploymentInternal, profile.DeploymentPrivateCustomer, profile.DeploymentPublic, profile.DeploymentOpenSource,
			profile.DeploymentEmbedded, profile.DeploymentAPI, profile.DeploymentLocalCLI, profile.DeploymentUnknown),
		setupTextPromptStep("profile-reviewer", "Profile reviewer (leave `unknown` to keep this draft)", &reviewer),
	}
	if err := runSetupPromptSteps(prompt, false, steps...); err != nil {
		return profile.System{}, err
	}
	if !strings.EqualFold(reviewer, "unknown") {
		value.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: reviewer, ReviewedAt: now.Format(time.DateOnly)}
	}
	if frameworkEnabled(enabledFrameworks, framework.EUAIActTechnicalEvidencePackID) {
		if err := prompt.sectionTitle("Provisional screening for the EU AI Act", true); err != nil {
			return profile.System{}, err
		}
		if err := profile.WriteTerminal(output, profile.AssessEUAIAct([]profile.System{value})); err != nil {
			return profile.System{}, err
		}
		prompt, err = prompt.startQuestionGroup("Applicability decision", 1, true)
		if err != nil {
			return profile.System{}, err
		}
		if err := explainSetupQuestion(prompt, "applicability-decision"); err != nil {
			return profile.System{}, err
		}
		decisionStatus, err := promptRequiredChoice(prompt, "Human EU AI Act applicability decision",
			profile.ApplicabilityNeedsReview, profile.ApplicabilityApplicable, profile.ApplicabilityNotApplicable, profile.ApplicabilityUncertain)
		if err != nil {
			return profile.System{}, err
		}
		decision := profile.ApplicabilityDecision{Framework: profile.FrameworkEUAIAct, Status: decisionStatus}
		if decisionStatus != profile.ApplicabilityNeedsReview {
			prompt, err = prompt.startQuestionGroup("Applicability review details", 2, true)
			if err != nil {
				return profile.System{}, err
			}
			defaultReviewer := ""
			if !strings.EqualFold(reviewer, "unknown") {
				defaultReviewer = reviewer
			}
			decision.ReviewedBy = defaultReviewer
			if err := runSetupPromptSteps(prompt, false,
				setupTextPromptStep("decision-rationale", "Decision rationale", &decision.Rationale),
				setupTextPromptStep("applicability-reviewer", "Applicability reviewer", &decision.ReviewedBy),
			); err != nil {
				return profile.System{}, err
			}
			decision.ReviewedAt = now.Format(time.DateOnly)
		}
		value.Applicability = []profile.ApplicabilityDecision{decision}
	}
	if err := value.Validate(); err != nil {
		return profile.System{}, fmt.Errorf("validate collected system profile: %w", err)
	}
	if _, err := fmt.Fprintln(output, "\nSystem context collected. Any applicability decisions are human records, not ComplyScan legal determinations."); err != nil {
		return profile.System{}, err
	}
	return value, nil
}

func frameworkEnabled(enabled []string, wanted string) bool {
	if len(enabled) == 0 {
		return wanted == framework.EUAIActTechnicalEvidencePackID
	}
	for _, id := range enabled {
		if id == wanted {
			return true
		}
	}
	return false
}

type promptSession struct {
	reader         *bufio.Reader
	output         io.Writer
	styleTitles    bool
	alwaysDetailed bool
	guidance       *questionGuidance
	questions      *questionProgress
	step           *setupStepProgress
	backAvailable  bool
	inputText      func(label, defaultValue string, guidanceAvailable, allowBack bool) (string, error)
	selectOne      func(label, defaultValue string, options []terminalChoice) (string, error)
	selectMany     func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error)
	confirmBool    func(label string, defaultValue bool) (bool, error)
}

type questionProgress struct {
	current int
	total   int
}

type setupStepProgress struct {
	current int
	total   int
}

func newPromptSession(input io.Reader, output io.Writer) promptSession {
	session := promptSession{reader: bufio.NewReader(input), output: output, guidance: &questionGuidance{}, step: &setupStepProgress{}}
	if terminalPromptAvailable(input, output) {
		session.styleTitles = os.Getenv("NO_COLOR") == ""
		session.inputText = func(label, defaultValue string, guidanceAvailable, allowBack bool) (string, error) {
			return runTerminalInput(input, output, label, defaultValue, guidanceAvailable, allowBack)
		}
		session.selectOne = func(label, defaultValue string, options []terminalChoice) (string, error) {
			return runTerminalSelect(input, output, label, defaultValue, options)
		}
		session.selectMany = func(label string, defaults []string, options []terminalChoice, exclusive []string) ([]string, error) {
			return runTerminalMultiSelect(input, output, label, defaults, options, exclusive)
		}
		session.confirmBool = func(label string, defaultValue bool) (bool, error) {
			return runTerminalConfirm(input, output, label, defaultValue)
		}
	}
	return session
}

func (session promptSession) confirm(label string, defaultValue bool) (bool, error) {
	label = session.nextQuestionLabel(label)
	if (session.hasQuestionGuidance() || session.backAvailable) && session.selectOne != nil {
		defaultChoice := "No"
		if defaultValue {
			defaultChoice = "Yes"
		}
		selected, err := session.chooseOne(label, defaultChoice, []terminalChoice{
			{Label: "Yes", Value: "Yes"},
			{Label: "No", Value: "No"},
		})
		if err != nil {
			return false, err
		}
		return selected == "Yes", nil
	}
	if session.confirmBool != nil {
		session.clearQuestionGuidance()
		return session.confirmBool(label, defaultValue)
	}
	defaultText := "y/N"
	if defaultValue {
		defaultText = "Y/n"
	}
	for {
		backHint := ""
		if session.backAvailable {
			backHint = " or `back`"
		}
		if _, err := fmt.Fprintf(session.output, "? %s [%s%s]: ", label, defaultText, backHint); err != nil {
			return false, err
		}
		line, err := session.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read %s: %w", strings.ToLower(label), err)
		}
		value := strings.ToLower(strings.TrimSpace(line))
		if session.backAvailable && value == "back" {
			session.clearQuestionGuidance()
			return false, errPromptBack
		}
		if value == "?" && session.hasQuestionGuidance() {
			if err := session.showQuestionGuidance(); err != nil {
				return false, err
			}
			continue
		}
		switch value {
		case "":
			session.clearQuestionGuidance()
			return defaultValue, nil
		case "y", "yes":
			session.clearQuestionGuidance()
			return true, nil
		case "n", "no":
			session.clearQuestionGuidance()
			return false, nil
		default:
			if _, writeErr := fmt.Fprintln(session.output, "  Enter yes or no."); writeErr != nil {
				return false, writeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read %s: input ended before a valid answer was supplied", strings.ToLower(label))
		}
	}
}

func (session promptSession) text(label, defaultValue string) (string, error) {
	return session.readText(session.nextQuestionLabel(label), defaultValue)
}

func (session promptSession) readText(label, defaultValue string) (string, error) {
	for {
		if session.inputText != nil {
			value, err := session.inputText(label, defaultValue, session.hasQuestionGuidance(), session.backAvailable)
			if err != nil {
				return "", err
			}
			if value == moreGuidanceChoiceValue && session.hasQuestionGuidance() {
				if err := session.showQuestionGuidance(); err != nil {
					return "", err
				}
				continue
			}
			session.clearQuestionGuidance()
			return value, nil
		}
		if _, err := fmt.Fprintln(session.output); err != nil {
			return "", err
		}
		if defaultValue != "" {
			if _, err := fmt.Fprintln(session.output, "  Proposed answer"); err != nil {
				return "", err
			}
			if err := writePromptParagraph(session.output, "    ", defaultValue); err != nil {
				return "", err
			}
		}
		actions := []string{"type a replacement"}
		if defaultValue != "" {
			actions = append([]string{"enter accept"}, actions...)
		} else {
			actions = append([]string{"answer required"}, actions...)
		}
		if session.hasQuestionGuidance() {
			actions = append(actions, "? details")
		}
		if session.backAvailable {
			actions = append(actions, "type `back` to return")
		}
		if err := writePromptParagraph(session.output, "  ", strings.Join(actions, " • ")); err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(session.output, "? %s\n› ", label); err != nil {
			return "", err
		}
		line, err := session.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
		}
		value := strings.TrimSpace(line)
		if session.backAvailable && strings.EqualFold(value, "back") {
			session.clearQuestionGuidance()
			return "", errPromptBack
		}
		if value == "?" && session.hasQuestionGuidance() {
			if err := session.showQuestionGuidance(); err != nil {
				return "", err
			}
			continue
		}
		if value == "" {
			value = defaultValue
		}
		if value != "" {
			session.clearQuestionGuidance()
			return value, nil
		}
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: input ended before a value was supplied", strings.ToLower(label))
		}
	}
}

func (session promptSession) textList(label string, defaultValue []string) ([]string, error) {
	value, err := session.text(label+" (comma-separated)", joinValues(defaultValue))
	if err != nil {
		return nil, err
	}
	return splitValues(value), nil
}

func promptChoice[T ~string](session promptSession, label string, defaultValue T, allowed ...T) (T, error) {
	if len(allowed) == 0 {
		return defaultValue, fmt.Errorf("%s has no available choices", strings.ToLower(label))
	}
	label = session.nextQuestionLabel(label)
	if session.selectOne != nil {
		options := make([]terminalChoice, len(allowed))
		for index, candidate := range allowed {
			options[index] = terminalChoice{Label: setupChoiceLabel(label, string(candidate)), Value: string(candidate)}
		}
		selected, err := session.chooseOne(label, string(defaultValue), options)
		if err != nil {
			return defaultValue, err
		}
		if index, matched := choiceIndex(selected, allowed); matched {
			return allowed[index], nil
		}
		return defaultValue, fmt.Errorf("%s selector returned unsupported value %q", strings.ToLower(label), selected)
	}
	defaultIndex := 0
	for index, candidate := range allowed {
		if strings.EqualFold(string(defaultValue), string(candidate)) {
			defaultIndex = index
		}
		if _, err := fmt.Fprintf(session.output, "  %d) %s\n", index+1, setupChoiceLabel(label, string(candidate))); err != nil {
			return defaultValue, err
		}
	}
	for {
		value, err := session.readText(fmt.Sprintf("Select %s (1-%d)", label, len(allowed)), strconv.Itoa(defaultIndex+1))
		if err != nil {
			return defaultValue, err
		}
		if index, matched := choiceIndex(value, allowed); matched {
			return allowed[index], nil
		}
		if _, err := fmt.Fprintf(session.output, "  Enter a number from 1 to %d.\n", len(allowed)); err != nil {
			return defaultValue, err
		}
	}
}

func setupChoiceLabel(questionLabel, value string) string {
	var description string
	switch {
	case strings.HasSuffix(strings.TrimSpace(questionLabel), "Decision impact"):
		description = map[string]string{
			string(profile.ImpactAdvisory):    "AI suggests; a person independently reviews before action",
			string(profile.ImpactLow):         "limited, reversible effect without material impact on people",
			string(profile.ImpactSignificant): "materially influences an important outcome for a person",
			string(profile.ImpactAutonomous):  "can trigger a consequential action without prior human approval",
			string(profile.ImpactUnknown):     "downstream effect has not been established",
		}[value]
	case strings.HasSuffix(strings.TrimSpace(questionLabel), "Lifecycle stage"):
		description = map[string]string{
			string(profile.LifecycleDevelopment): "being designed or implemented; not used in normal operation",
			string(profile.LifecycleTesting):     "controlled validation, pilot, or pre-production use",
			string(profile.LifecycleProduction):  "available or used in normal real-world operation",
			string(profile.LifecycleRetired):     "no longer used, though records or obligations may remain",
			string(profile.LifecycleUnknown):     "current stage has not been established",
		}[value]
	default:
		return value
	}
	if description == "" {
		return value
	}
	return value + " — " + description
}

func promptRequiredChoice[T ~string](session promptSession, label string, allowed ...T) (T, error) {
	var zero T
	if len(allowed) == 0 {
		return zero, fmt.Errorf("%s has no available choices", strings.ToLower(label))
	}
	label = session.nextQuestionLabel(label)
	if session.selectOne != nil {
		options := make([]terminalChoice, 0, len(allowed)+1)
		options = append(options, terminalChoice{Value: requiredAnswerChoiceValue})
		for _, candidate := range allowed {
			options = append(options, terminalChoice{Label: setupChoiceLabel(label, string(candidate)), Value: string(candidate)})
		}
		selected, err := session.chooseOne(label, requiredAnswerChoiceValue, options)
		if err != nil {
			return zero, err
		}
		if index, matched := choiceIndex(selected, allowed); matched {
			return allowed[index], nil
		}
		return zero, fmt.Errorf("%s selector returned unsupported value %q", strings.ToLower(label), selected)
	}
	for index, candidate := range allowed {
		if _, err := fmt.Fprintf(session.output, "  %d) %s\n", index+1, setupChoiceLabel(label, string(candidate))); err != nil {
			return zero, err
		}
	}
	for {
		value, err := session.readText(fmt.Sprintf("Select %s (1-%d)", label, len(allowed)), "")
		if err != nil {
			return zero, err
		}
		if index, matched := choiceIndex(value, allowed); matched {
			return allowed[index], nil
		}
		if _, err := fmt.Fprintf(session.output, "  Enter a number from 1 to %d.\n", len(allowed)); err != nil {
			return zero, err
		}
	}
}

func promptChoices[T ~string](session promptSession, label string, defaultValue []T, allowed ...T) ([]T, error) {
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%s has no available choices", strings.ToLower(label))
	}
	label = session.nextQuestionLabel(label)
	if session.selectMany != nil {
		defaultStrings := make([]string, len(defaultValue))
		for index, value := range defaultValue {
			defaultStrings[index] = string(value)
		}
		options := make([]terminalChoice, len(allowed))
		exclusive := []string{}
		for index, value := range allowed {
			options[index] = terminalChoice{Label: string(value), Value: string(value)}
			if strings.EqualFold(string(value), "unknown") {
				exclusive = append(exclusive, string(value))
			}
		}
		selected, err := session.chooseMany(label, defaultStrings, options, exclusive)
		if err != nil {
			return nil, err
		}
		result := make([]T, 0, len(selected))
		seen := make(map[int]struct{}, len(selected))
		for _, value := range selected {
			index, matched := choiceIndex(value, allowed)
			if !matched {
				return nil, fmt.Errorf("%s selector returned unsupported value %q", strings.ToLower(label), value)
			}
			if _, duplicate := seen[index]; !duplicate {
				result = append(result, allowed[index])
				seen[index] = struct{}{}
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("%s requires at least one selection", strings.ToLower(label))
		}
		return result, nil
	}
	for index, candidate := range allowed {
		if _, err := fmt.Fprintf(session.output, "  %d) %s\n", index+1, candidate); err != nil {
			return nil, err
		}
	}
	defaultIndexes := make([]string, 0, len(defaultValue))
	seenDefaults := make(map[int]struct{}, len(defaultValue))
	for _, value := range defaultValue {
		if index, matched := choiceIndex(string(value), allowed); matched {
			if _, duplicate := seenDefaults[index]; !duplicate {
				defaultIndexes = append(defaultIndexes, strconv.Itoa(index+1))
				seenDefaults[index] = struct{}{}
			}
		}
	}
	for {
		value, err := session.readText(label+" numbers (comma-separated)", strings.Join(defaultIndexes, ","))
		if err != nil {
			return nil, err
		}
		parts := splitValues(value)
		result := make([]T, 0, len(parts))
		seen := make(map[int]struct{}, len(parts))
		valid := true
		for _, part := range parts {
			index, matched := choiceIndex(part, allowed)
			if !matched {
				valid = false
				break
			}
			if _, duplicate := seen[index]; !duplicate {
				result = append(result, allowed[index])
				seen[index] = struct{}{}
			}
		}
		if valid && len(result) > 0 {
			return result, nil
		}
		if _, err := fmt.Fprintf(session.output, "  Enter one or more numbers from 1 to %d, separated by commas.\n", len(allowed)); err != nil {
			return nil, err
		}
	}
}

func promptRequiredChoices[T ~string](session promptSession, label string, allowed ...T) ([]T, error) {
	return promptChoices(session, label, nil, allowed...)
}

func choiceIndex[T ~string](value string, allowed []T) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if number, err := strconv.Atoi(trimmed); err == nil && strconv.Itoa(number) == trimmed && number >= 1 && number <= len(allowed) {
		return number - 1, true
	}
	// Continue accepting the former textual input so redirected interactive
	// setup scripts do not break, while the visible UI advertises numbers only.
	for index, candidate := range allowed {
		if strings.EqualFold(trimmed, string(candidate)) {
			return index, true
		}
	}
	return 0, false
}

func splitValues(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func joinValues[T ~string](values []T) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = string(value)
	}
	return strings.Join(parts, ",")
}

func isInteractiveReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && file.Name() != os.DevNull
}
