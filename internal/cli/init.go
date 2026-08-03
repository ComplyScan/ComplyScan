package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/1eonardodawinki/ComplyScan/internal/config"
	"github.com/1eonardodawinki/ComplyScan/internal/profile"
	"github.com/spf13/cobra"
)

func newInitCommand(stdout io.Writer) *cobra.Command {
	var (
		force            bool
		forceInteractive bool
		nonInteractive   bool
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
			if interactive {
				system, err := collectSystemProfile(cmd.InOrStdin(), stdout, target, time.Now())
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
	return command
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
	prompt := promptSession{reader: bufio.NewReader(input), output: output}
	if _, err := fmt.Fprintln(output, "\nSystem applicability setup"); err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(output, "Answer factual questions. Use `unknown` when context has not been established; do not enter secrets or personal records."); err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(output); err != nil {
		return profile.System{}, err
	}

	if value.ID, err = prompt.text("System ID", value.ID); err != nil {
		return profile.System{}, err
	}
	if value.Name, err = prompt.text("System name", value.Name); err != nil {
		return profile.System{}, err
	}
	if value.IntendedPurpose, err = prompt.text("Intended purpose", "unknown"); err != nil {
		return profile.System{}, err
	}
	if value.LifecycleStage, err = promptChoice(prompt, "Lifecycle stage", profile.LifecycleUnknown,
		profile.LifecycleDevelopment, profile.LifecycleTesting, profile.LifecycleProduction, profile.LifecycleRetired, profile.LifecycleUnknown); err != nil {
		return profile.System{}, err
	}
	if value.OrganizationRoles, err = promptChoices(prompt, "Organization roles", []profile.OrganizationRole{profile.RoleUnknown},
		profile.RoleProvider, profile.RoleDeployer, profile.RoleImporter, profile.RoleDistributor, profile.RoleProductManufacturer, profile.RoleUnknown); err != nil {
		return profile.System{}, err
	}
	if value.OperatingRegions, err = promptChoices(prompt, "Operating regions", []profile.OperatingRegion{profile.RegionUnknown},
		profile.RegionEU, profile.RegionEEA, profile.RegionUK, profile.RegionUS, profile.RegionGlobal, profile.RegionOther, profile.RegionUnknown); err != nil {
		return profile.System{}, err
	}
	if value.UseCaseDomains, err = promptChoices(prompt, "Use-case domains", []profile.UseCaseDomain{profile.DomainUnknown},
		profile.DomainBiometrics, profile.DomainCriticalInfrastructure, profile.DomainEducation, profile.DomainEmployment,
		profile.DomainEssentialServices, profile.DomainLawEnforcement, profile.DomainMigrationBorderControl,
		profile.DomainJusticeDemocraticProcess, profile.DomainHealthcare, profile.DomainSoftwareDevelopment,
		profile.DomainGeneralPurpose, profile.DomainOther, profile.DomainUnknown); err != nil {
		return profile.System{}, err
	}
	if value.Users, err = prompt.textList("Users", []string{"unknown"}); err != nil {
		return profile.System{}, err
	}
	if value.AffectedGroups, err = prompt.textList("Potentially affected groups", []string{"unknown"}); err != nil {
		return profile.System{}, err
	}
	if value.DecisionImpact, err = promptChoice(prompt, "Decision impact", profile.ImpactUnknown,
		profile.ImpactAdvisory, profile.ImpactLow, profile.ImpactSignificant, profile.ImpactAutonomous, profile.ImpactUnknown); err != nil {
		return profile.System{}, err
	}
	if value.HumanOversight, err = promptChoice(prompt, "Human oversight", profile.OversightUnknown,
		profile.OversightRequired, profile.OversightAvailable, profile.OversightLimited, profile.OversightNone, profile.OversightUnknown); err != nil {
		return profile.System{}, err
	}
	if value.Data.PersonalData, err = promptChoice(prompt, "Processes personal data", profile.TriUnknown, profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
		return profile.System{}, err
	}
	if value.Data.SpecialCategoryData, err = promptChoice(prompt, "Processes special-category or similarly sensitive data", profile.TriUnknown, profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
		return profile.System{}, err
	}
	if value.Data.ChildrenData, err = promptChoice(prompt, "Processes children's data", profile.TriUnknown, profile.TriYes, profile.TriNo, profile.TriUnknown); err != nil {
		return profile.System{}, err
	}
	if value.DeploymentModels, err = promptChoices(prompt, "Deployment models", []profile.DeploymentModel{profile.DeploymentUnknown},
		profile.DeploymentInternal, profile.DeploymentPrivateCustomer, profile.DeploymentPublic, profile.DeploymentOpenSource,
		profile.DeploymentEmbedded, profile.DeploymentAPI, profile.DeploymentLocalCLI, profile.DeploymentUnknown); err != nil {
		return profile.System{}, err
	}

	reviewer, err := prompt.text("Profile reviewer (leave `unknown` to keep this draft)", "unknown")
	if err != nil {
		return profile.System{}, err
	}
	if !strings.EqualFold(reviewer, "unknown") {
		value.ProfileReview = profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: reviewer, ReviewedAt: now.Format(time.DateOnly)}
	}
	if _, err := fmt.Fprintln(output, "\nProvisional screening from the declared facts:"); err != nil {
		return profile.System{}, err
	}
	if err := profile.WriteTerminal(output, profile.AssessEUAIAct([]profile.System{value})); err != nil {
		return profile.System{}, err
	}
	decisionStatus, err := promptChoice(prompt, "Human EU AI Act applicability decision", profile.ApplicabilityNeedsReview,
		profile.ApplicabilityNeedsReview, profile.ApplicabilityApplicable, profile.ApplicabilityNotApplicable, profile.ApplicabilityUncertain)
	if err != nil {
		return profile.System{}, err
	}
	decision := profile.ApplicabilityDecision{Framework: profile.FrameworkEUAIAct, Status: decisionStatus}
	if decisionStatus != profile.ApplicabilityNeedsReview {
		if decision.Rationale, err = prompt.text("Decision rationale", ""); err != nil {
			return profile.System{}, err
		}
		defaultReviewer := ""
		if !strings.EqualFold(reviewer, "unknown") {
			defaultReviewer = reviewer
		}
		if decision.ReviewedBy, err = prompt.text("Applicability reviewer", defaultReviewer); err != nil {
			return profile.System{}, err
		}
		decision.ReviewedAt = now.Format(time.DateOnly)
	}
	value.Applicability = []profile.ApplicabilityDecision{decision}
	if err := value.Validate(); err != nil {
		return profile.System{}, fmt.Errorf("validate collected system profile: %w", err)
	}
	if _, err := fmt.Fprintln(output, "\nSystem context collected. Applicability decisions are human records, not ComplyScan legal determinations."); err != nil {
		return profile.System{}, err
	}
	return value, nil
}

type promptSession struct {
	reader *bufio.Reader
	output io.Writer
}

func (session promptSession) text(label, defaultValue string) (string, error) {
	for {
		if _, err := fmt.Fprintf(session.output, "? %s [%s]: ", label, defaultValue); err != nil {
			return "", err
		}
		line, err := session.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = defaultValue
		}
		if value != "" {
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
	for {
		value, err := session.text(label+" ("+joinValues(allowed)+")", string(defaultValue))
		if err != nil {
			return defaultValue, err
		}
		for _, candidate := range allowed {
			if strings.EqualFold(value, string(candidate)) {
				return candidate, nil
			}
		}
		if _, err := fmt.Fprintf(session.output, "  Enter one of: %s\n", joinValues(allowed)); err != nil {
			return defaultValue, err
		}
	}
}

func promptChoices[T ~string](session promptSession, label string, defaultValue []T, allowed ...T) ([]T, error) {
	for {
		value, err := session.text(label+" (comma-separated: "+joinValues(allowed)+")", joinValues(defaultValue))
		if err != nil {
			return nil, err
		}
		parts := splitValues(value)
		result := make([]T, 0, len(parts))
		seen := make(map[T]struct{}, len(parts))
		valid := true
		for _, part := range parts {
			matched := false
			for _, candidate := range allowed {
				if strings.EqualFold(part, string(candidate)) {
					matched = true
					if _, duplicate := seen[candidate]; !duplicate {
						result = append(result, candidate)
						seen[candidate] = struct{}{}
					}
					break
				}
			}
			if !matched {
				valid = false
				break
			}
		}
		if valid && len(result) > 0 {
			return result, nil
		}
		if _, err := fmt.Fprintf(session.output, "  Enter one or more of: %s\n", joinValues(allowed)); err != nil {
			return nil, err
		}
	}
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
