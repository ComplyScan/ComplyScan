// Package repositoryanalysis prepares safe repository-wide model context and
// chooses between one-pass and hierarchical analysis.
package repositoryanalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const (
	DefaultRemoteInputTokens = 180_000
	DefaultLocalInputTokens  = 24_000
	minimumInputTokens       = 8_000
	charactersPerToken       = 3
	contextReservePercent    = 20
)

type Reviewer interface {
	ReviewRepository(context.Context, providers.RepositoryAnalysisRequest) (providers.RepositoryAnalysisResult, error)
}

type Mode string

const (
	ModeAuto         Mode = "auto"
	ModeFull         Mode = "full"
	ModeHierarchical Mode = "hierarchical"
)

type Options struct {
	Mode           Mode
	MaxInputTokens int
	Provider       providers.Kind
	Model          string
	OnProgress     func(Progress) error
}

type Progress struct {
	Stage      string
	Completed  int
	Total      int
	Scope      string
	InputBytes int64
}

// Run sends all relevant discovered repository text when it fits. Larger
// repositories are partitioned by subsystem and then synthesized from
// structured, citation-preserving summaries.
func Run(ctx context.Context, reviewer Reviewer, repository discovery.Repository, evidence []framework.TechnicalEvidenceReport, systems []profile.System, options Options) (providers.RepositoryAnalysisResult, error) {
	if reviewer == nil {
		return providers.RepositoryAnalysisResult{}, errors.New("repository analysis reviewer is required")
	}
	if options.Mode == "" {
		options.Mode = ModeAuto
	}
	if options.Mode != ModeAuto && options.Mode != ModeFull && options.Mode != ModeHierarchical {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("unsupported repository analysis mode %q", options.Mode)
	}
	if options.MaxInputTokens == 0 {
		options.MaxInputTokens = DefaultRemoteInputTokens
		if options.Provider == providers.Ollama {
			options.MaxInputTokens = DefaultLocalInputTokens
		}
	}
	if options.MaxInputTokens < minimumInputTokens {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("repository analysis input budget must be at least %d tokens", minimumInputTokens)
	}
	files := repositoryFiles(repository)
	if len(files) == 0 {
		return providers.RepositoryAnalysisResult{
			Provider: options.Provider, Model: options.Model,
			Coverage: providers.RepositoryCoverage{Mode: providers.RepositoryAnalysisFull, RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository)},
			Result:   providers.RepositorySectionResult{Scope: ".", AIUses: []providers.RepositoryAIUse{}, ObjectiveObservations: []providers.RepositoryObjectiveObservation{}, UnmappedObservations: []providers.RepositoryUnmappedObservation{}, UnresolvedQuestions: []string{}},
			Notes:    []string{"No relevant text files were available for repository-wide model analysis."},
		}, nil
	}
	objectives := repositoryObjectives(evidence)
	systemContext := repositorySystems(systems)
	budget := sourceBudget(options.MaxInputTokens, objectives, systemContext)
	fullBytes := sourceFileBytes(files)
	if options.Mode != ModeHierarchical && fullBytes <= budget {
		if err := progress(options, Progress{Stage: "full-repository", Completed: 0, Total: 1, Scope: ".", InputBytes: fullBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		result, err := reviewer.ReviewRepository(ctx, providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisFull, Scope: ".", RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
			Files: files, Objectives: objectives, Systems: systemContext,
		})
		if err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		if err := progress(options, Progress{Stage: "full-repository", Completed: 1, Total: 1, Scope: ".", InputBytes: fullBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		result.Notes = append(result.Notes, "The complete relevant discovered repository fit in one model request.")
		return result, nil
	}
	if options.Mode == ModeFull {
		return providers.RepositoryAnalysisResult{}, fmt.Errorf("relevant repository context is %d bytes, exceeding the configured full-analysis budget of %d bytes", fullBytes, budget)
	}
	return runHierarchical(ctx, reviewer, repository, files, objectives, systemContext, budget, options)
}

func runHierarchical(ctx context.Context, reviewer Reviewer, repository discovery.Repository, files []providers.RepositorySourceFile, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, budget int64, options Options) (providers.RepositoryAnalysisResult, error) {
	chunks, err := partitionRepository(files, budget)
	if err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	summaries := make([]providers.RepositorySectionResult, 0, len(chunks))
	aggregate := providers.RepositoryAnalysisResult{Provider: options.Provider, Model: options.Model}
	for index, chunk := range chunks {
		if err := progress(options, Progress{Stage: "subsystem", Completed: index, Total: len(chunks), Scope: chunk.scope, InputBytes: sourceFileBytes(chunk.files)}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		result, err := reviewer.ReviewRepository(ctx, providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisSubsystem, Scope: chunk.scope,
			RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), Files: chunk.files,
			Objectives: objectives, Systems: systems,
		})
		if err != nil {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze subsystem %s: %w", chunk.scope, err)
		}
		summaries = append(summaries, result.Result)
		addUsage(&aggregate.Usage, result.Usage)
		aggregate.Coverage.FilesSubmitted += result.Coverage.FilesSubmitted
		aggregate.Coverage.BytesSubmitted += result.Coverage.BytesSubmitted
		aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
		if err := progress(options, Progress{Stage: "subsystem", Completed: index + 1, Total: len(chunks), Scope: chunk.scope, InputBytes: sourceFileBytes(chunk.files)}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
	}
	levels := 0
	for len(summaries) > 1 {
		levels++
		groups := partitionSummaries(summaries, budget)
		next := make([]providers.RepositorySectionResult, 0, len(groups))
		for index, group := range groups {
			scope := fmt.Sprintf("synthesis-level-%d-part-%d", levels, index+1)
			if err := progress(options, Progress{Stage: "synthesis", Completed: index, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group)}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
			result, err := reviewer.ReviewRepository(ctx, providers.RepositoryAnalysisRequest{
				Mode: providers.RepositoryAnalysisSynthesis, Scope: scope,
				RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: citedFileIndex(group, files),
				Objectives: objectives, Systems: systems, SubsystemSummaries: group,
			})
			if err != nil {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis level %d part %d: %w", levels, index+1, err)
			}
			next = append(next, result.Result)
			addUsage(&aggregate.Usage, result.Usage)
			aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
			if err := progress(options, Progress{Stage: "synthesis", Completed: index + 1, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group)}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
		}
		if len(next) >= len(summaries) {
			return providers.RepositoryAnalysisResult{}, errors.New("repository synthesis could not reduce subsystem summaries within the configured input budget")
		}
		summaries = next
	}
	if len(summaries) == 1 && len(chunks) == 1 {
		// A forced hierarchical analysis still gets an explicit synthesis pass so
		// its output follows the same global contract as larger repositories.
		group := summaries
		result, err := reviewer.ReviewRepository(ctx, providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisSynthesis, Scope: "repository-synthesis",
			RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: citedFileIndex(group, files),
			Objectives: objectives, Systems: systems, SubsystemSummaries: group,
		})
		if err != nil {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis: %w", err)
		}
		summaries[0] = result.Result
		addUsage(&aggregate.Usage, result.Usage)
		aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
	}
	aggregate.Coverage.Mode = providers.RepositoryAnalysisSynthesis
	aggregate.Coverage.RepositoryFiles = len(repository.Files)
	aggregate.Coverage.RepositoryBytes = repositorySize(repository)
	aggregate.Coverage.Subsystems = len(chunks)
	aggregate.Result = summaries[0]
	aggregate.Result.Scope = "."
	aggregate.Notes = []string{
		fmt.Sprintf("The repository exceeded the one-request context budget and was analyzed as %d subsystem slice(s) followed by global synthesis.", len(chunks)),
		"Subsystem boundaries are a context-management mechanism, not declared AI-system boundaries.",
		"Repository-wide model analysis is advisory; deterministic findings and the bounded evidence investigation remain available for comparison.",
	}
	return aggregate, nil
}

type repositoryChunk struct {
	scope string
	files []providers.RepositorySourceFile
}

func partitionRepository(files []providers.RepositorySourceFile, budget int64) ([]repositoryChunk, error) {
	groups := make(map[string][]providers.RepositorySourceFile)
	for _, file := range files {
		parts := strings.Split(filepath.ToSlash(file.Path), "/")
		scope := "repository-root"
		if len(parts) > 1 {
			scope = parts[0]
		}
		groups[scope] = append(groups[scope], file)
	}
	scopes := make([]string, 0, len(groups))
	for scope := range groups {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	var chunks []repositoryChunk
	for _, scope := range scopes {
		part := 1
		current := repositoryChunk{scope: scope}
		var size int64
		for _, file := range groups[scope] {
			fileSize := int64(len(file.Content) + len(file.Path) + 100)
			if fileSize > budget {
				return nil, fmt.Errorf("repository file %s requires %d bytes and cannot fit the configured analysis budget of %d bytes", file.Path, fileSize, budget)
			}
			if len(current.files) > 0 && size+fileSize > budget {
				current.scope = fmt.Sprintf("%s (part %d)", scope, part)
				chunks = append(chunks, current)
				part++
				current = repositoryChunk{scope: scope}
				size = 0
			}
			current.files = append(current.files, file)
			size += fileSize
		}
		if len(current.files) > 0 {
			if part > 1 {
				current.scope = fmt.Sprintf("%s (part %d)", scope, part)
			}
			chunks = append(chunks, current)
		}
	}
	return chunks, nil
}

func partitionSummaries(values []providers.RepositorySectionResult, budget int64) [][]providers.RepositorySectionResult {
	var result [][]providers.RepositorySectionResult
	var current []providers.RepositorySectionResult
	var size int64
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		valueSize := int64(len(encoded) + 200)
		if len(current) > 0 && size+valueSize > budget {
			result = append(result, current)
			current = nil
			size = 0
		}
		current = append(current, value)
		size += valueSize
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result
}

func repositoryFiles(repository discovery.Repository) []providers.RepositorySourceFile {
	files := make([]providers.RepositorySourceFile, 0, len(repository.Files))
	for _, file := range repository.Files {
		if file.Kind == discovery.KindOtherText {
			continue
		}
		content := rules.RedactSecrets(strings.ReplaceAll(string(file.Content), "\r\n", "\n"))
		files = append(files, providers.RepositorySourceFile{
			Path: file.Path, Kind: string(file.Kind), LineCount: strings.Count(content, "\n") + 1, Content: content,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func repositoryObjectives(reports []framework.TechnicalEvidenceReport) []providers.RepositoryObjective {
	var result []providers.RepositoryObjective
	seen := make(map[string]struct{})
	for _, report := range reports {
		for _, objective := range report.Objectives {
			id := report.Pack.ID + "/" + objective.ID
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, providers.RepositoryObjective{
				ID: id, Title: objective.Title, SourceReference: objective.SourceReference,
				Description: objective.Description, Verification: objective.Verification,
			})
		}
	}
	return result
}

func repositorySystems(systems []profile.System) []providers.RepositorySystemContext {
	result := make([]providers.RepositorySystemContext, 0, len(systems))
	for _, system := range systems {
		facts := []string{
			"Intended purpose: " + system.IntendedPurpose,
			"Lifecycle stage: " + string(system.LifecycleStage),
			"Decision impact: " + string(system.DecisionImpact),
			"Human oversight: " + string(system.HumanOversight),
		}
		result = append(result, providers.RepositorySystemContext{ID: system.ID, Name: system.Name, DeclaredFacts: facts})
	}
	return result
}

func sourceBudget(tokens int, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext) int64 {
	overhead, _ := json.Marshal(struct {
		Objectives []providers.RepositoryObjective
		Systems    []providers.RepositorySystemContext
	}{objectives, systems})
	total := int64(tokens * charactersPerToken)
	reserve := total * contextReservePercent / 100
	budget := total - reserve - int64(len(overhead))
	if budget < 1 {
		return 1
	}
	return budget
}

func sourceFileBytes(files []providers.RepositorySourceFile) int64 {
	var size int64
	for _, file := range files {
		size += int64(len(file.Content) + len(file.Path) + 100)
	}
	return size
}

func repositorySize(repository discovery.Repository) int64 {
	var size int64
	for _, file := range repository.Files {
		size += int64(len(file.Content))
	}
	return size
}

func summaryBytes(values []providers.RepositorySectionResult) int64 {
	encoded, _ := json.Marshal(values)
	return int64(len(encoded))
}

func citedFileIndex(summaries []providers.RepositorySectionResult, files []providers.RepositorySourceFile) []providers.RepositoryFileReference {
	wanted := make(map[string]struct{})
	add := func(citations []providers.RepositoryCitation) {
		for _, citation := range citations {
			wanted[citation.Path] = struct{}{}
		}
	}
	for _, summary := range summaries {
		for _, use := range summary.AIUses {
			add(use.Evidence)
		}
		for _, observation := range summary.ObjectiveObservations {
			add(observation.SupportingEvidence)
			add(observation.ContradictoryEvidence)
		}
		for _, observation := range summary.UnmappedObservations {
			add(observation.Evidence)
		}
	}
	result := make([]providers.RepositoryFileReference, 0, len(wanted))
	for _, file := range files {
		if _, exists := wanted[file.Path]; exists {
			result = append(result, providers.RepositoryFileReference{Path: file.Path, Kind: file.Kind, LineCount: file.LineCount})
		}
	}
	return result
}

func addUsage(total *providers.Usage, value providers.Usage) {
	total.PromptTokens += value.PromptTokens
	total.CompletionTokens += value.CompletionTokens
	total.TotalDurationNS += value.TotalDurationNS
}

func progress(options Options, value Progress) error {
	if options.OnProgress == nil {
		return nil
	}
	return options.OnProgress(value)
}
