package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/config"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/reconciliation"
	"github.com/spf13/cobra"
)

type detectedTestCommand struct {
	ID       string
	Name     string
	Command  string
	Args     []string
	Evidence []string
}

func newVerifyCommand(stdout io.Writer) *cobra.Command {
	command := &cobra.Command{
		Use:   "verify",
		Short: "Configure isolated technical-objective test evidence",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newVerifySetupCommand(stdout))
	return command
}

func newVerifySetupCommand(stdout io.Writer) *cobra.Command {
	var configPath string
	var forceInteractive bool
	command := &cobra.Command{
		Use:   "setup [path]",
		Short: "Guidedly connect project tests to technical objectives",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "."
			if len(args) == 1 {
				target = args[0]
			}
			cfg, path, err := config.Resolve(target, configPath)
			if err != nil {
				return err
			}
			if path == "" {
				return fmt.Errorf("no %s found for %q; run `complyscan setup` first", config.FileName, target)
			}
			if len(cfg.Systems) == 0 {
				return errors.New("verification setup needs a configured system profile; run `complyscan profile setup` first")
			}
			if !forceInteractive && !isInteractiveReader(cmd.InOrStdin()) {
				return errors.New("verification setup requires a terminal; use --interactive when piping answers or edit .complyscan.yml directly")
			}
			return runVerifySetup(cmd, stdout, target, path, cfg)
		},
	}
	command.Flags().StringVar(&configPath, "config", "", "configuration file (defaults to <path>/.complyscan.yml)")
	command.Flags().BoolVar(&forceInteractive, "interactive", false, "run the verification wizard even when input is redirected")
	return command
}

func runVerifySetup(cmd *cobra.Command, stdout io.Writer, target, configPath string, cfg config.Config) error {
	if _, err := fmt.Fprintf(stdout, "ComplyScan verification setup\nRepository: %s\n\n", target); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "This wizard saves test recipes. It does not run tests, build images, decide whether a control is legally required, or establish compliance."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "Every objective-to-test association must be confirmed by you. Recipes run later only with `complyscan scan --verify`."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	discovered, err := discovery.Discover(cmd.Context(), target, discovery.Options{
		Exclude: append([]string(nil), cfg.Scan.Exclude...), MaxFiles: cfg.Scan.MaxFiles, MaxTotalBytes: cfg.Scan.MaxTotalBytes,
		IncludeNestedRepositories: cfg.Scan.IncludeNestedRepositories, TrackedOnly: cfg.Scan.TrackedOnly,
	})
	if err != nil {
		return fmt.Errorf("inspect repository for verification setup: %w", err)
	}
	detected := detectTestCommands(discovered.Repository)
	if len(detected) == 0 {
		return errors.New("no supported test command was detected; add a test manifest or configure a recipe directly in .complyscan.yml")
	}
	prompt := newPromptSession(cmd.InOrStdin(), stdout)
	selectedSystem, err := promptVerificationSystem(prompt, cfg.Systems)
	if err != nil {
		return err
	}
	selectedCommand, err := promptDetectedTestCommand(prompt, detected)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "\nDetected test command\n  %s\n  Evidence: %s\n\nThe command is only a suggestion. Confirm the exact executable and arguments used by your project.\n",
		commandText(selectedCommand.Command, selectedCommand.Args), strings.Join(selectedCommand.Evidence, ", ")); err != nil {
		return err
	}
	executable, err := prompt.text("Test executable (no shell syntax)", selectedCommand.Command)
	if err != nil {
		return err
	}
	arguments, err := prompt.textList("Test arguments", selectedCommand.Args)
	if err != nil {
		return err
	}

	components := inventory.NewReport(target, "setup", inventory.Analyze(discovered.Repository), discovered.Warnings)
	objectives, err := verificationObjectivesForFrameworks(cfg, discovered.Repository, components, selectedSystem.ID)
	if err != nil {
		return err
	}
	if err := writeVerificationObjectiveChoices(stdout, objectives); err != nil {
		return err
	}
	selection, err := prompt.text("Objective numbers this exact test command exercises (comma-separated, or none)", "none")
	if err != nil {
		return err
	}
	selectedObjectives, err := parseObjectiveSelection(selection, objectives)
	if err != nil {
		return err
	}
	if len(selectedObjectives) == 0 {
		_, err := fmt.Fprintln(stdout, "No recipe saved. Rerun when a reviewer can confirm which technical mechanism the tests exercise.")
		return err
	}

	if _, err := fmt.Fprintln(stdout, "\nContainer boundary\n  The image must already exist locally and contain the project's test dependencies.\n  ComplyScan will not build or pull it. The later run is read-only and network-disabled."); err != nil {
		return err
	}
	runtime, err := promptChoice(prompt, "Container runtime", "docker", "docker", "podman")
	if err != nil {
		return err
	}
	image, err := prompt.text("Preloaded local test image", "complyscan-project-tests:local")
	if err != nil {
		return err
	}
	defaultID := verificationRecipeID(selectedSystem.ID, selectedCommand.ID)
	if _, err := fmt.Fprintln(stdout, "\nRecipe identity\n  This stable label connects future test runs to reports. Reusing an existing ID replaces that recipe after confirmation."); err != nil {
		return err
	}
	recipeID, err := prompt.text("Recipe ID", defaultID)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "  The timeout stops a stalled container. Five minutes is the default; use a measured project-specific limit."); err != nil {
		return err
	}
	timeoutText, err := prompt.text("Timeout in seconds (1-1800)", strconv.Itoa(config.DefaultVerificationTimeoutSeconds))
	if err != nil {
		return err
	}
	timeoutSeconds, err := strconv.Atoi(timeoutText)
	if err != nil || timeoutSeconds < 1 || timeoutSeconds > 1800 {
		return errors.New("timeout must be a whole number between 1 and 1800 seconds")
	}
	recipe := config.VerificationRecipe{
		ID: recipeID, Runtime: runtime, Image: image, Command: executable, Arguments: arguments,
		Objectives: selectedObjectives, Systems: []string{selectedSystem.ID}, TimeoutSeconds: timeoutSeconds,
	}
	if err := validateVerificationRecipeInConfig(cfg, recipe); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "\nRecipe summary\n  ID: %s\n  System: %s\n  Objectives: %s\n  Command: %s\n  Image: %s\n",
		recipe.ID, selectedSystem.ID, strings.Join(recipe.Objectives, ", "), commandText(recipe.Command, recipe.Arguments), recipe.Image); err != nil {
		return err
	}
	saveLabel := "Save this inert verification recipe"
	saveDefault := true
	if verificationRecipeExists(cfg, recipe.ID) {
		saveLabel = "Replace the existing recipe with this ID"
		saveDefault = false
		if _, err := fmt.Fprintln(stdout, "  An existing recipe has this ID. Replacing it changes future explicitly requested verification runs."); err != nil {
			return err
		}
	}
	save, err := prompt.confirm(saveLabel, saveDefault)
	if err != nil {
		return err
	}
	if !save {
		_, err := fmt.Fprintln(stdout, "Recipe not saved.")
		return err
	}
	replaced := upsertVerificationRecipe(&cfg, recipe)
	if err := config.Write(configPath, cfg, true); err != nil {
		return err
	}
	action := "Saved"
	if replaced {
		action = "Replaced"
	}
	if _, err := fmt.Fprintf(stdout, "%s recipe %q in %s. Nothing was executed.\n", action, recipe.ID, configPath); err != nil {
		return err
	}
	if repositoryHasDockerfile(discovered.Repository) {
		if _, err := fmt.Fprintf(stdout, "If this repository's Dockerfile is its test environment, review it and build the local image explicitly:\n  %s\n", commandText(string(runtime), []string{"build", "-t", recipe.Image, target})); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(stdout, "Create or obtain a trusted local test image named %q that already contains the project dependencies.\n", recipe.Image); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stdout, "Then run: complyscan scan %s --verify\n",
		shellQuote(target))
	return err
}

func detectTestCommands(repository discovery.Repository) []detectedTestCommand {
	paths := make(map[string]discovery.File, len(repository.Files))
	for _, file := range repository.Files {
		paths[strings.ToLower(filepath.ToSlash(file.Path))] = file
	}
	result := []detectedTestCommand{}
	if _, found := paths["go.mod"]; found {
		evidence := []string{"go.mod"}
		if path := firstMatchingPath(repository, func(path string) bool { return strings.HasSuffix(path, "_test.go") }); path != "" {
			evidence = append(evidence, path)
		}
		result = append(result, detectedTestCommand{ID: "go", Name: "Go tests", Command: "go", Args: []string{"test", "./..."}, Evidence: evidence})
	}
	if manifest, found := paths["package.json"]; found {
		var value struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal(manifest.Content, &value) == nil && strings.TrimSpace(value.Scripts["test"]) != "" && !strings.Contains(strings.ToLower(value.Scripts["test"]), "no test specified") {
			command := "npm"
			if _, exists := paths["pnpm-lock.yaml"]; exists {
				command = "pnpm"
			} else if _, exists := paths["yarn.lock"]; exists {
				command = "yarn"
			}
			result = append(result, detectedTestCommand{ID: "javascript", Name: "JavaScript/TypeScript tests", Command: command, Args: []string{"test"}, Evidence: []string{"package.json scripts.test"}})
		}
	}
	pythonEvidence := firstMatchingPath(repository, func(path string) bool {
		base := strings.ToLower(filepath.Base(path))
		return strings.HasSuffix(path, ".py") && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || strings.Contains(path, "/tests/"))
	})
	if pythonEvidence != "" || pathExists(paths, "pytest.ini") || pathExists(paths, "pyproject.toml") && fileContains(paths["pyproject.toml"], "pytest") {
		evidence := []string{pythonEvidence}
		if pythonEvidence == "" {
			evidence = []string{"pytest configuration"}
		}
		result = append(result, detectedTestCommand{ID: "python", Name: "Python pytest", Command: "python", Args: []string{"-m", "pytest"}, Evidence: evidence})
	}
	if makefile, found := paths["makefile"]; found && fileContains(makefile, "test:") {
		result = append(result, detectedTestCommand{ID: "make", Name: "Make test target", Command: "make", Args: []string{"test"}, Evidence: []string{"Makefile test target"}})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func promptVerificationSystem(prompt promptSession, systems []profile.System) (profile.System, error) {
	if len(systems) == 1 {
		if _, err := fmt.Fprintf(prompt.output, "System: %s (%s)\n", systems[0].Name, systems[0].ID); err != nil {
			return profile.System{}, err
		}
		return systems[0], nil
	}
	if _, err := fmt.Fprintln(prompt.output, "Configured systems:"); err != nil {
		return profile.System{}, err
	}
	if _, err := fmt.Fprintln(prompt.output, "Choose the product whose technical mechanism this test command actually verifies. ComplyScan will not infer ownership from paths."); err != nil {
		return profile.System{}, err
	}
	for index, system := range systems {
		if _, err := fmt.Fprintf(prompt.output, "  %d) %s (%s)\n", index+1, system.Name, system.ID); err != nil {
			return profile.System{}, err
		}
	}
	value, err := prompt.text("System number this recipe verifies", "1")
	if err != nil {
		return profile.System{}, err
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > len(systems) {
		return profile.System{}, errors.New("system selection is outside the displayed range")
	}
	return systems[index-1], nil
}

func promptDetectedTestCommand(prompt promptSession, commands []detectedTestCommand) (detectedTestCommand, error) {
	if len(commands) == 1 {
		return commands[0], nil
	}
	if _, err := fmt.Fprintln(prompt.output, "Detected test systems:"); err != nil {
		return detectedTestCommand{}, err
	}
	for index, command := range commands {
		if _, err := fmt.Fprintf(prompt.output, "  %d) %s — %s\n", index+1, command.Name, commandText(command.Command, command.Args)); err != nil {
			return detectedTestCommand{}, err
		}
	}
	value, err := prompt.text("Test system number", "1")
	if err != nil {
		return detectedTestCommand{}, err
	}
	index, err := strconv.Atoi(value)
	if err != nil || index < 1 || index > len(commands) {
		return detectedTestCommand{}, errors.New("test-system selection is outside the displayed range")
	}
	return commands[index-1], nil
}

type verificationObjectiveChoice struct {
	Objective framework.ObjectiveAssessment
	Mapping   reconciliation.ObjectiveResult
	Framework string
}

func verificationObjectivesForFrameworks(cfg config.Config, repository discovery.Repository, components inventory.Report, systemID string) ([]verificationObjectiveChoice, error) {
	choices := []verificationObjectiveChoice{}
	for _, packID := range cfg.Frameworks {
		pack, err := framework.LoadBuiltin(packID)
		if err != nil {
			return nil, err
		}
		technical := framework.Evaluate(pack, cfg.Systems, repository)
		assessment := profile.AssessmentReport{}
		if pack.ID == framework.EUAIActTechnicalEvidencePackID {
			assessment = profile.AssessEUAIAct(cfg.Systems)
		}
		mapping := reconciliation.Build(cfg.Systems, assessment, technical, components, cfg.Ownership)
		packChoices, err := verificationObjectivesForSystem(mapping, technical, systemID)
		if err != nil {
			return nil, fmt.Errorf("select objectives from %s: %w", pack.Name, err)
		}
		for index := range packChoices {
			packChoices[index].Framework = pack.Name
		}
		choices = append(choices, packChoices...)
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("system %q has no likely-required or recommended technical objectives; review its profile and selected frameworks before creating test mappings", systemID)
	}
	return choices, nil
}

func verificationObjectivesForSystem(mapping reconciliation.Report, technical framework.TechnicalEvidenceReport, systemID string) ([]verificationObjectiveChoice, error) {
	objectiveByID := make(map[string]framework.ObjectiveAssessment, len(technical.Objectives))
	for _, objective := range technical.Objectives {
		objectiveByID[objective.ID] = objective
	}
	for _, system := range mapping.Systems {
		if system.SystemID != systemID {
			continue
		}
		choices := []verificationObjectiveChoice{}
		for _, result := range system.Objectives {
			if result.Requirement != reconciliation.RequirementLikelyRequired && result.Requirement != reconciliation.RequirementRecommended {
				continue
			}
			choices = append(choices, verificationObjectiveChoice{Objective: objectiveByID[result.ObjectiveID], Mapping: result})
		}
		return choices, nil
	}
	return nil, fmt.Errorf("system %q was not found in reconciliation", systemID)
}

func writeVerificationObjectiveChoices(output io.Writer, choices []verificationObjectiveChoice) error {
	if _, err := fmt.Fprintln(output, "\nApplicable or recommended technical objectives\nLegal requirements and voluntary framework recommendations remain distinct. Select only mechanisms this exact test command genuinely exercises."); err != nil {
		return err
	}
	for index, choice := range choices {
		if _, err := fmt.Fprintf(output, "\n  %d) %s (%s)\n     Framework: %s\n     Developer meaning: %s\n     Current repository signal: %s",
			index+1, choice.Objective.Title, choice.Objective.SourceReference, choice.Framework, choice.Objective.Description, choice.Objective.Status); err != nil {
			return err
		}
		if len(choice.Objective.Matches) > 0 {
			paths := make([]string, 0, len(choice.Objective.Matches))
			for _, match := range choice.Objective.Matches {
				paths = append(paths, match.Path)
			}
			if _, err := fmt.Fprintf(output, " at %s", strings.Join(paths, ", ")); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(output); err != nil {
			return err
		}
	}
	return nil
}

func parseObjectiveSelection(value string, choices []verificationObjectiveChoice) ([]string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "none" {
		return []string{}, nil
	}
	seen := make(map[int]struct{})
	result := []string{}
	for _, part := range strings.Split(value, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index < 1 || index > len(choices) {
			return nil, fmt.Errorf("objective selection %q is outside the displayed range", strings.TrimSpace(part))
		}
		if _, duplicate := seen[index]; duplicate {
			continue
		}
		seen[index] = struct{}{}
		result = append(result, choices[index-1].Objective.ID)
	}
	return result, nil
}

func validateVerificationRecipeInConfig(cfg config.Config, recipe config.VerificationRecipe) error {
	candidate := cfg
	if candidate.Verification == nil {
		candidate.Verification = &config.VerificationConfig{Recipes: []config.VerificationRecipe{recipe}}
	} else {
		copyValue := *candidate.Verification
		copyValue.Recipes = append([]config.VerificationRecipe(nil), copyValue.Recipes...)
		replaced := false
		for index := range copyValue.Recipes {
			if copyValue.Recipes[index].ID == recipe.ID {
				copyValue.Recipes[index] = recipe
				replaced = true
			}
		}
		if !replaced {
			copyValue.Recipes = append(copyValue.Recipes, recipe)
		}
		candidate.Verification = &copyValue
	}
	return candidate.Validate()
}

func upsertVerificationRecipe(cfg *config.Config, recipe config.VerificationRecipe) bool {
	if cfg.Verification == nil {
		cfg.Verification = &config.VerificationConfig{Recipes: []config.VerificationRecipe{recipe}}
		return false
	}
	for index := range cfg.Verification.Recipes {
		if cfg.Verification.Recipes[index].ID == recipe.ID {
			cfg.Verification.Recipes[index] = recipe
			return true
		}
	}
	cfg.Verification.Recipes = append(cfg.Verification.Recipes, recipe)
	return false
}

func verificationRecipeExists(cfg config.Config, recipeID string) bool {
	if cfg.Verification == nil {
		return false
	}
	for _, recipe := range cfg.Verification.Recipes {
		if recipe.ID == recipeID {
			return true
		}
	}
	return false
}

func firstMatchingPath(repository discovery.Repository, match func(string) bool) string {
	for _, file := range repository.Files {
		path := strings.ToLower(filepath.ToSlash(file.Path))
		if match(path) {
			return file.Path
		}
	}
	return ""
}

func pathExists(paths map[string]discovery.File, path string) bool {
	_, exists := paths[path]
	return exists
}

func repositoryHasDockerfile(repository discovery.Repository) bool {
	for _, file := range repository.Files {
		if file.Kind == discovery.KindDockerfile {
			return true
		}
	}
	return false
}

func fileContains(file discovery.File, term string) bool {
	return strings.Contains(strings.ToLower(string(file.Content)), strings.ToLower(term))
}

func commandText(command string, args []string) string {
	values := []string{shellQuote(command)}
	for _, argument := range args {
		values = append(values, shellQuote(argument))
	}
	return strings.Join(values, " ")
}

func verificationRecipeID(systemID, commandID string) string {
	value := strings.Trim(strings.ToLower(systemID+"-"+commandID+"-controls"), "-")
	if len(value) > 100 {
		value = value[:100]
	}
	return value
}
