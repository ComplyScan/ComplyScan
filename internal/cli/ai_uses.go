package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ComplyScan/ComplyScan/internal/aiuse"
	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	reportpkg "github.com/ComplyScan/ComplyScan/internal/report"
	"github.com/spf13/cobra"
)

const maximumAIUseReportBytes = 64 << 20

type aiUseSetupAction string

const (
	aiUseConfirmNew    aiUseSetupAction = "confirm as a new AI use — create a stable, developer-owned record"
	aiUseMergeExisting aiUseSetupAction = "merge into an existing AI use — add this evidence without changing its identity"
	aiUseDismiss       aiUseSetupAction = "dismiss this suggestion — remember that it is not a product AI use"
	aiUseDecideLater   aiUseSetupAction = "decide later — make no change and ask again after a later review"
)

func newAIUsesCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "ai-uses",
		Short: "Review and manage developer-confirmed AI-use groupings",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newAIUsesShowCommand(stdout))
	command.AddCommand(newAIUsesSetupCommand(stdout))
	command.AddCommand(newAIUsesEditCommand(stdout))
	return command
}

func newAIUsesShowCommand(stdout io.Writer) *cobra.Command {
	var format string
	command := &cobra.Command{
		Use:   "show [path]",
		Short: "Show developer-confirmed AI-use groupings",
		Long:  "Show the local, human-owned AI-use register without scanning the repository or contacting a model.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target := optionalTarget(args)
			manifestPath := filepath.Join(target, aiuse.DefaultPath)
			manifest, exists, err := aiuse.LoadOptional(manifestPath)
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "terminal":
				return writeAIUsesTerminal(stdout, manifestPath, manifest, exists)
			case "json":
				encoder := json.NewEncoder(stdout)
				encoder.SetIndent("", "  ")
				encoder.SetEscapeHTML(false)
				if err := encoder.Encode(struct {
					ManifestPath string         `json:"manifest_path"`
					Exists       bool           `json:"exists"`
					Manifest     aiuse.Manifest `json:"manifest"`
				}{ManifestPath: manifestPath, Exists: exists, Manifest: manifest}); err != nil {
					return fmt.Errorf("encode AI-use manifest JSON: %w", err)
				}
				return nil
			default:
				return fmt.Errorf("invalid format %q (want terminal or json)", format)
			}
		},
	}
	command.Flags().StringVarP(&format, "format", "f", "terminal", "output format: terminal or json")
	return command
}

func newAIUsesSetupCommand(stdout io.Writer) *cobra.Command {
	var configPath, reportOverride string
	var forceInteractive bool
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Review AI-use suggestions from an existing ComplyScan report",
		Long: "Review AI-use suggestions from a completed AI-assisted `complyscan scan` report. This command makes no model request. " +
			"It writes the human-owned register only after explicit confirm, merge, or dismiss decisions; confirmation is not a compliance conclusion.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := optionalTarget(args)
			cfg, resolvedConfig, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if resolvedConfig == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan setup` first", config.FileName, target)
			}
			if !forceInteractive && !isInteractiveReader(cmd.InOrStdin()) {
				return errors.New("AI-use setup requires a terminal; use --interactive when piping answers")
			}

			manifestPath := filepath.Join(target, aiuse.DefaultPath)
			manifest, _, err := aiuse.LoadOptional(manifestPath)
			if err != nil {
				return err
			}
			reportPath := resolveAIUseFile(target, reportOverride, filepath.Join(reportpkg.DefaultDirectory, "latest.json"))
			scanReport, err := loadAIUseReport(reportPath)
			if err != nil {
				return err
			}
			if scanReport.RepositoryAnalysis == nil || scanReport.RepositoryAnalysisRun != reportpkg.RepositoryAnalysisCompleted {
				return fmt.Errorf("report %q has no completed repository AI review; configure AI assistance and run `complyscan scan` first", reportPath)
			}
			changedScope := scanReport.Scan.Scope.AIReview == "changed-plus-connected"
			if scanReport.AIUseInventory != nil {
				changedScope = changedScope || scanReport.AIUseInventory.ChangedScope
			}
			if changedScope {
				if _, err := fmt.Fprintln(stdout, "This report reviewed changed and connected code only. Unchanged saved AI uses were not re-reviewed, and this workflow will not retire or delete them."); err != nil {
					return err
				}
			}

			prompt := newPromptSession(cmd.InOrStdin(), stdout)
			changed, reviewed, err := reviewAIUseSuggestions(prompt, &manifest, cfg.Systems, scanReport.RepositoryAnalysis.Result.AIUses, time.Now())
			if err != nil {
				return err
			}
			if !changed {
				if reviewed == 0 {
					_, err = fmt.Fprintln(stdout, "No new AI-use suggestions need a decision.")
				} else {
					_, err = fmt.Fprintln(stdout, "No AI-use decisions were saved.")
				}
				return err
			}
			if err := aiuse.Write(manifestPath, manifest); err != nil {
				return err
			}
			confirmed := 0
			for _, use := range manifest.Uses {
				if use.Status == aiuse.StatusActive && use.Review.Status == profile.ReviewConfirmed {
					confirmed++
				}
			}
			_, err = fmt.Fprintf(stdout, "Saved %d confirmed AI use(s) and %d dismissed suggestion(s) in %s\n", confirmed, len(manifest.Dismissals), manifestPath)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().StringVar(&reportOverride, "report", "", "JSON report containing repository AI-use suggestions (defaults to <path>/.complyscan/reports/latest.json)")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "review suggestions even when input is redirected")
	return command
}

func newAIUsesEditCommand(stdout io.Writer) *cobra.Command {
	var configPath, manifestOverride string
	var forceInteractive bool
	command := &cobra.Command{
		Use:   "edit [path]",
		Short: "Edit a developer-confirmed AI use",
		Long: "Interactively edit one human-owned AI-use record without scanning the repository or contacting a model. " +
			"The stable AI-use ID and its reviewed suggestion links are preserved.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := optionalTarget(args)
			cfg, resolvedConfig, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if resolvedConfig == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan setup` first or provide --config", config.FileName, target)
			}
			if !forceInteractive && !isInteractiveReader(cmd.InOrStdin()) {
				return errors.New("AI-use editing requires a terminal; run this command in an interactive terminal (or use --interactive when deliberately piping answers)")
			}

			manifestPath := resolveAIUseFile(target, manifestOverride, aiuse.DefaultPath)
			manifest, exists, err := aiuse.LoadOptional(manifestPath)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("no AI-use register found at %q; run `complyscan ai-uses setup` first", manifestPath)
			}
			if len(manifest.Uses) == 0 {
				return fmt.Errorf("AI-use register %q has no saved AI uses to edit", manifestPath)
			}

			prompt := newPromptSession(cmd.InOrStdin(), stdout)
			if err := editSavedAIUse(prompt, &manifest, cfg.Systems, time.Now()); err != nil {
				return err
			}
			if err := aiuse.Write(manifestPath, manifest); err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Saved AI-use changes in %s\n", manifestPath)
			return err
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().StringVar(&manifestOverride, "manifest", "", "AI-use manifest to edit (defaults to <path>/.complyscan/ai-uses.yml)")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "edit even when input is redirected")
	return command
}

func editSavedAIUse(prompt promptSession, manifest *aiuse.Manifest, systems []profile.System, now time.Time) error {
	ids := make([]string, 0, len(manifest.Uses))
	prompt.guidance.choiceDescriptions = make(map[string]string, len(manifest.Uses))
	for _, use := range manifest.Uses {
		ids = append(ids, use.ID)
		prompt.guidance.choiceDescriptions[use.ID] = fmt.Sprintf("%s — %s", use.Name, use.Status)
	}
	sort.Strings(ids)
	id, err := promptRequiredChoice(prompt, "AI use to edit", ids...)
	if err != nil {
		return err
	}

	index := -1
	for candidate := range manifest.Uses {
		if manifest.Uses[candidate].ID == id {
			index = candidate
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("saved AI use %q no longer exists", id)
	}

	updated := manifest.Uses[index]
	if _, err := fmt.Fprintf(prompt.output, "  Stable ID: %s (cannot be changed)\n", updated.ID); err != nil {
		return err
	}
	updated.Name, err = prompt.text("Name", updated.Name)
	if err != nil {
		return err
	}
	updated.Description, err = prompt.text("Description", updated.Description)
	if err != nil {
		return err
	}
	updated.Paths, err = prompt.textList("Positive repository paths", updated.Paths)
	if err != nil {
		return err
	}
	updated.SystemIDs, err = chooseAIUseSystemsForEdit(prompt, systems, updated.SystemIDs)
	if err != nil {
		return err
	}
	prompt.guidance.choiceDescriptions = map[string]string{
		string(aiuse.StatusActive):  "included in current per-use requirement mapping",
		string(aiuse.StatusRetired): "kept for history but excluded from current requirement mapping",
	}
	updated.Status, err = promptChoice(prompt, "Use status", updated.Status, aiuse.StatusActive, aiuse.StatusRetired)
	if err != nil {
		return err
	}
	updated.Review, err = editAIUseReview(prompt, updated.Review, now)
	if err != nil {
		return err
	}

	// Validate the complete record before replacing it in memory. aiuse.Write
	// validates the full manifest again immediately before its atomic rename.
	if err := updated.Validate(); err != nil {
		return fmt.Errorf("invalid AI-use changes: %w", err)
	}
	manifest.Uses[index] = updated
	return nil
}

func chooseAIUseSystemsForEdit(prompt promptSession, systems []profile.System, current []string) ([]string, error) {
	configured := make(map[string]struct{}, len(systems))
	for _, system := range systems {
		configured[system.ID] = struct{}{}
	}
	missing := make([]string, 0)
	for _, id := range current {
		if _, exists := configured[id]; !exists {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		if _, err := fmt.Fprintf(prompt.output, "  Current system association(s) are no longer configured and cannot be retained: %s\n", strings.Join(missing, ", ")); err != nil {
			return nil, err
		}
	}
	if len(systems) == 0 {
		if _, err := fmt.Fprintln(prompt.output, "  No declared system is configured; this AI use will remain unassociated."); err != nil {
			return nil, err
		}
		return []string{}, nil
	}

	defaults := make([]string, 0, len(current))
	for _, id := range current {
		if _, exists := configured[id]; exists {
			defaults = append(defaults, id)
		}
	}
	associate, err := prompt.confirm("Associate this AI use with one or more declared systems?", len(defaults) > 0)
	if err != nil {
		return nil, err
	}
	if !associate {
		return []string{}, nil
	}

	ids := make([]string, 0, len(systems))
	prompt.guidance.choiceDescriptions = make(map[string]string, len(systems))
	for _, system := range systems {
		ids = append(ids, system.ID)
		prompt.guidance.choiceDescriptions[system.ID] = system.Name
	}
	sort.Strings(ids)
	if len(ids) == 1 {
		if _, err := fmt.Fprintf(prompt.output, "  Associated system: %s (%s)\n", prompt.guidance.choiceDescriptions[ids[0]], ids[0]); err != nil {
			return nil, err
		}
		return ids, nil
	}
	if len(defaults) == 0 {
		return promptRequiredChoices(prompt, "Associated systems", ids...)
	}
	return promptChoices(prompt, "Associated systems", defaults, ids...)
}

func editAIUseReview(prompt promptSession, current profile.ProfileReview, now time.Time) (profile.ProfileReview, error) {
	prompt.guidance.choiceDescriptions = map[string]string{
		string(profile.ReviewDraft):     "saved for further developer review and excluded from current per-use mapping",
		string(profile.ReviewConfirmed): "developer-approved grouping included in current per-use mapping",
	}
	status, err := promptChoice(prompt, "Developer review status", current.Status, profile.ReviewDraft, profile.ReviewConfirmed)
	if err != nil {
		return profile.ProfileReview{}, err
	}
	if status == profile.ReviewDraft {
		if _, err := fmt.Fprintln(prompt.output, "  Draft status clears the previous reviewer and review date."); err != nil {
			return profile.ProfileReview{}, err
		}
		return profile.ProfileReview{Status: profile.ReviewDraft}, nil
	}
	reviewer, err := prompt.text("Reviewer", current.ReviewedBy)
	if err != nil {
		return profile.ProfileReview{}, err
	}
	reviewedAt, err := prompt.text("Review date (YYYY-MM-DD)", now.UTC().Format(time.DateOnly))
	if err != nil {
		return profile.ProfileReview{}, err
	}
	if _, err := time.Parse(time.DateOnly, reviewedAt); err != nil {
		return profile.ProfileReview{}, fmt.Errorf("invalid review date %q (want YYYY-MM-DD)", reviewedAt)
	}
	return profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: reviewer, ReviewedAt: reviewedAt}, nil
}

func optionalTarget(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

func resolveAIUseFile(target, explicit, fallback string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	return filepath.Join(target, fallback)
}

func loadAIUseReport(path string) (reportpkg.Report, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return reportpkg.Report{}, fmt.Errorf("inspect ComplyScan report %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q must not be a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q is not a regular file", path)
	}
	if info.Size() > maximumAIUseReportBytes {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q exceeds %d bytes", path, maximumAIUseReportBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return reportpkg.Report{}, fmt.Errorf("open ComplyScan report %q: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var value reportpkg.Report
	if err := decoder.Decode(&value); err != nil {
		return reportpkg.Report{}, fmt.Errorf("parse ComplyScan report %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values are not supported")
		}
		return reportpkg.Report{}, fmt.Errorf("parse ComplyScan report %q: %w", path, err)
	}
	if value.SchemaVersion != 6 && value.SchemaVersion != 7 && value.SchemaVersion != 8 && value.SchemaVersion != 9 {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q uses unsupported schema version %d (want 6, 7, 8, or 9)", path, value.SchemaVersion)
	}
	// Schema versions before 7 did not serialize the repository-analysis
	// lifecycle. A present validated result was necessarily a completed pass,
	// so preserve that safe upgrade path for reports created before this command
	// existed.
	if value.SchemaVersion > 0 && value.SchemaVersion < 7 && value.RepositoryAnalysisRun == "" && value.RepositoryAnalysis != nil {
		value.RepositoryAnalysisRun = reportpkg.RepositoryAnalysisCompleted
	}
	switch value.RepositoryAnalysisRun {
	case reportpkg.RepositoryAnalysisNotRequested, reportpkg.RepositoryAnalysisPending, reportpkg.RepositoryAnalysisIncomplete, reportpkg.RepositoryAnalysisCompleted:
	default:
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q has invalid repository_analysis_run %q", path, value.RepositoryAnalysisRun)
	}
	if value.RepositoryAnalysisRun == reportpkg.RepositoryAnalysisCompleted && value.RepositoryAnalysis == nil {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q marks repository analysis completed but has no result", path)
	}
	if value.RepositoryAnalysis != nil && value.RepositoryAnalysisRun != reportpkg.RepositoryAnalysisCompleted {
		return reportpkg.Report{}, fmt.Errorf("ComplyScan report %q contains repository analysis with lifecycle %q", path, value.RepositoryAnalysisRun)
	}
	return value, nil
}

func reviewAIUseSuggestions(prompt promptSession, manifest *aiuse.Manifest, systems []profile.System, suggestions []providers.RepositoryAIUse, now time.Time) (bool, int, error) {
	changed := false
	reviewed := 0
	for _, rawSuggestion := range suggestions {
		if aiuse.IsDismissed(*manifest, rawSuggestion) || suggestionAlreadyConfirmed(*manifest, rawSuggestion) {
			continue
		}
		fingerprint := aiuse.SuggestionFingerprint(rawSuggestion)
		suggestion, err := safeAIUseSuggestion(rawSuggestion)
		if err != nil {
			return changed, reviewed, err
		}
		reviewed++
		if err := writeAIUseSuggestion(prompt.output, suggestion, reviewed); err != nil {
			return changed, reviewed, err
		}
		actions := []aiUseSetupAction{aiUseConfirmNew}
		if activeAIUseIDs(*manifest) != nil {
			actions = append(actions, aiUseMergeExisting)
		}
		actions = append(actions, aiUseDismiss, aiUseDecideLater)
		action, err := promptRequiredChoice(prompt, "Decision", actions...)
		if err != nil {
			return changed, reviewed, err
		}
		switch action {
		case aiUseConfirmNew:
			if err := confirmNewAIUse(prompt, manifest, systems, suggestion, fingerprint, now); err != nil {
				return changed, reviewed, err
			}
			changed = true
		case aiUseMergeExisting:
			if err := mergeAIUseSuggestion(prompt, manifest, suggestion, fingerprint, now); err != nil {
				return changed, reviewed, err
			}
			changed = true
		case aiUseDismiss:
			reason, err := prompt.text("Reason for dismissal", "")
			if err != nil {
				return changed, reviewed, err
			}
			manifest.Dismissals = append(manifest.Dismissals, aiuse.Dismissal{Fingerprint: fingerprint, Reason: reason})
			changed = true
		case aiUseDecideLater:
			// Deliberately leave the suggestion out of the durable register so it
			// is presented again when a developer is ready to decide.
		default:
			return changed, reviewed, fmt.Errorf("unsupported AI-use decision %q", action)
		}
	}
	return changed, reviewed, nil
}

func safeAIUseSuggestion(suggestion providers.RepositoryAIUse) (providers.RepositoryAIUse, error) {
	suggestion.Evidence = append([]providers.RepositoryCitation(nil), suggestion.Evidence...)
	suggestion.UnresolvedQuestions = append([]string(nil), suggestion.UnresolvedQuestions...)
	suggestion.Name = safeAIUseTerminalText(suggestion.Name)
	suggestion.Purpose = safeAIUseTerminalText(suggestion.Purpose)
	suggestion.Lifecycle = safeAIUseTerminalText(suggestion.Lifecycle)
	suggestion.Confidence = safeAIUseTerminalText(suggestion.Confidence)
	if suggestion.Name == "" || suggestion.Purpose == "" {
		return providers.RepositoryAIUse{}, errors.New("AI-use suggestion has no safe display name or purpose")
	}
	for index := range suggestion.Evidence {
		for _, character := range suggestion.Evidence[index].Path {
			if unsafeAIUseTerminalRune(character) {
				return providers.RepositoryAIUse{}, fmt.Errorf("AI-use suggestion evidence path %d contains an unsafe terminal control or formatting character", index+1)
			}
		}
		suggestion.Evidence[index].Summary = safeAIUseTerminalText(suggestion.Evidence[index].Summary)
	}
	questions := suggestion.UnresolvedQuestions[:0]
	for _, question := range suggestion.UnresolvedQuestions {
		if cleaned := safeAIUseTerminalText(question); cleaned != "" {
			questions = append(questions, cleaned)
		}
	}
	suggestion.UnresolvedQuestions = questions
	return suggestion, nil
}

func safeAIUseTerminalText(value string) string {
	value = strings.Map(func(character rune) rune {
		if unsafeAIUseTerminalRune(character) {
			return ' '
		}
		return character
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func unsafeAIUseTerminalRune(character rune) bool {
	return unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character)
}

func suggestionAlreadyConfirmed(manifest aiuse.Manifest, suggestion providers.RepositoryAIUse) bool {
	matches := aiuse.LinkedSuggestionUses(manifest, suggestion)
	if len(matches) != 1 {
		return false
	}
	for _, use := range manifest.Uses {
		if use.ID == matches[0] {
			return use.Status == aiuse.StatusActive && use.Review.Status == profile.ReviewConfirmed
		}
	}
	return false
}

func confirmNewAIUse(prompt promptSession, manifest *aiuse.Manifest, systems []profile.System, suggestion providers.RepositoryAIUse, fingerprint string, now time.Time) error {
	name, err := prompt.text("Stable AI-use name", suggestion.Name)
	if err != nil {
		return err
	}
	description, err := prompt.text("Description", suggestion.Purpose)
	if err != nil {
		return err
	}
	paths, err := prompt.textList("Repository paths", aiuse.SuggestionPaths(suggestion))
	if err != nil {
		return err
	}
	systemIDs, err := chooseAIUseSystems(prompt, systems)
	if err != nil {
		return err
	}
	reviewer, err := prompt.text("Reviewer", "")
	if err != nil {
		return err
	}
	manifest.Uses = append(manifest.Uses, aiuse.Use{
		ID: aiuse.NextID(*manifest, name), Name: name, Description: description,
		SystemIDs: systemIDs, Paths: paths, SuggestionFingerprints: []string{fingerprint}, Status: aiuse.StatusActive,
		Review: confirmedAIUseReview(reviewer, now),
	})
	return nil
}

func mergeAIUseSuggestion(prompt promptSession, manifest *aiuse.Manifest, suggestion providers.RepositoryAIUse, fingerprint string, now time.Time) error {
	ids := activeAIUseIDs(*manifest)
	if len(ids) == 0 {
		return errors.New("no active saved AI use is available to merge")
	}
	prompt.guidance.choiceDescriptions = make(map[string]string, len(ids))
	for _, use := range manifest.Uses {
		if use.Status == aiuse.StatusActive {
			prompt.guidance.choiceDescriptions[use.ID] = use.Name + " — " + use.Description
		}
	}
	id, err := promptRequiredChoice(prompt, "Saved AI use", ids...)
	if err != nil {
		return err
	}
	reviewer, err := prompt.text("Reviewer", "")
	if err != nil {
		return err
	}
	for index := range manifest.Uses {
		if manifest.Uses[index].ID != id {
			continue
		}
		manifest.Uses[index].Paths = sortedUniqueStrings(append(manifest.Uses[index].Paths, aiuse.SuggestionPaths(suggestion)...))
		manifest.Uses[index].SuggestionFingerprints = sortedUniqueStrings(append(manifest.Uses[index].SuggestionFingerprints, fingerprint))
		manifest.Uses[index].Review = confirmedAIUseReview(reviewer, now)
		return nil
	}
	return fmt.Errorf("saved AI use %q no longer exists", id)
}

func chooseAIUseSystems(prompt promptSession, systems []profile.System) ([]string, error) {
	if len(systems) == 0 {
		if _, err := fmt.Fprintln(prompt.output, "  No declared system is configured; this AI use will remain unassociated."); err != nil {
			return nil, err
		}
		return []string{}, nil
	}
	associate, err := prompt.confirm("Associate this AI use with a declared system now?", false)
	if err != nil {
		return nil, err
	}
	if !associate {
		if _, err := fmt.Fprintln(prompt.output, "  The AI use will remain unassociated until a developer updates the register."); err != nil {
			return nil, err
		}
		return []string{}, nil
	}
	if len(systems) == 1 {
		if _, err := fmt.Fprintf(prompt.output, "  Associated system: %s (%s)\n", systems[0].Name, systems[0].ID); err != nil {
			return nil, err
		}
		return []string{systems[0].ID}, nil
	}
	ids := make([]string, 0, len(systems))
	if _, err := fmt.Fprintln(prompt.output, "  Declared systems:"); err != nil {
		return nil, err
	}
	for _, system := range systems {
		ids = append(ids, system.ID)
		if _, err := fmt.Fprintf(prompt.output, "    %s — %s\n", system.ID, system.Name); err != nil {
			return nil, err
		}
	}
	prompt.guidance.choiceDescriptions = make(map[string]string, len(systems))
	for _, system := range systems {
		prompt.guidance.choiceDescriptions[system.ID] = system.Name
	}
	return promptRequiredChoices(prompt, "Associated systems", ids...)
}

func confirmedAIUseReview(reviewer string, now time.Time) profile.ProfileReview {
	return profile.ProfileReview{Status: profile.ReviewConfirmed, ReviewedBy: strings.TrimSpace(reviewer), ReviewedAt: now.UTC().Format("2006-01-02")}
}

func activeAIUseIDs(manifest aiuse.Manifest) []string {
	ids := make([]string, 0, len(manifest.Uses))
	for _, use := range manifest.Uses {
		if use.Status == aiuse.StatusActive {
			ids = append(ids, use.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func sortedUniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func writeAIUseSuggestion(output io.Writer, suggestion providers.RepositoryAIUse, index int) error {
	if _, err := fmt.Fprintf(output, "\nSuggested AI use %d\n  Name: %s\n  Purpose: %s\n", index, suggestion.Name, suggestion.Purpose); err != nil {
		return err
	}
	if suggestion.Lifecycle != "" {
		if _, err := fmt.Fprintf(output, "  Lifecycle: %s\n", suggestion.Lifecycle); err != nil {
			return err
		}
	}
	if suggestion.Confidence != "" {
		if _, err := fmt.Fprintf(output, "  Model confidence: %s\n", suggestion.Confidence); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(output, "  Repository evidence:"); err != nil {
		return err
	}
	for _, citation := range suggestion.Evidence {
		location := citation.Path
		if citation.Line > 0 {
			location = fmt.Sprintf("%s:%d", citation.Path, citation.Line)
		}
		if _, err := fmt.Fprintf(output, "    %s — %s\n", location, citation.Summary); err != nil {
			return err
		}
	}
	if len(suggestion.UnresolvedQuestions) > 0 {
		if _, err := fmt.Fprintln(output, "  Questions still unresolved:"); err != nil {
			return err
		}
		for _, question := range suggestion.UnresolvedQuestions {
			if _, err := fmt.Fprintf(output, "    - %s\n", question); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAIUsesTerminal(output io.Writer, path string, manifest aiuse.Manifest, exists bool) error {
	if err := writeSectionTitle(output, "AI-use register", false, false); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Manifest: %s\n", path); err != nil {
		return err
	}
	if !exists {
		_, err := fmt.Fprintln(output, "No AI-use register exists yet. Run `complyscan ai-uses setup` after an AI-assisted review.")
		return err
	}
	if len(manifest.Uses) == 0 {
		if _, err := fmt.Fprintln(output, "No AI uses have been confirmed."); err != nil {
			return err
		}
	}
	for _, use := range manifest.Uses {
		systems := strings.Join(use.SystemIDs, ", ")
		if systems == "" {
			systems = "not associated"
		}
		if _, err := fmt.Fprintf(output, "\n%s (%s)\n  Status: %s\n  Description: %s\n  Systems: %s\n  Paths: %s\n  Human review: %s",
			use.Name, use.ID, use.Status, use.Description, systems, strings.Join(use.Paths, ", "), use.Review.Status); err != nil {
			return err
		}
		if use.Review.ReviewedBy != "" {
			if _, err := fmt.Fprintf(output, " by %s on %s", use.Review.ReviewedBy, use.Review.ReviewedAt); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(output, "\nDismissed suggestions: %d\n", len(manifest.Dismissals))
	return err
}
