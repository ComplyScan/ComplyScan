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
	"time"

	"github.com/ComplyScan/ComplyScan/internal/codegraph"
	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
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
	maxRateLimitRetries      = 3
	defaultRateLimitWait     = 60 * time.Second
	maxRateLimitWait         = 65 * time.Second
	minimumAdaptiveOutput    = 1024
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
	Ownership      []ownership.Rule
	OnProgress     func(Progress) error
	Wait           func(context.Context, time.Duration) error
}

type Progress struct {
	Stage      string
	Completed  int
	Total      int
	Scope      string
	InputBytes int64
	Wait       time.Duration
	Detail     string
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
	systemContext := repositorySystems(systems, options.Ownership)
	budget := sourceBudget(options.MaxInputTokens, objectives, systemContext)
	graph := codegraph.Build(repository)
	fullGraph := repositoryGraphContext(graph, files)
	fullBytes := requestContextBytes(files, fullGraph)
	if options.Mode != ModeHierarchical && fullBytes <= budget {
		if err := progress(options, Progress{Stage: "full-repository", Completed: 0, Total: 1, Scope: ".", InputBytes: fullBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		request := providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisFull, Scope: ".", RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository),
			MaxOutputTokens: repositoryOutputTokens(options.Provider, 0), Files: files, Objectives: objectives, Systems: systemContext, Graph: fullGraph,
		}
		result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge && options.Mode == ModeAuto {
				return runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, budget, options)
			}
			return providers.RepositoryAnalysisResult{}, err
		}
		if err := validateSystemAttribution(result.Result, systems, options.Ownership); err != nil {
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
	return runHierarchical(ctx, reviewer, repository, graph, files, objectives, systemContext, budget, options)
}

func runHierarchical(ctx context.Context, reviewer Reviewer, repository discovery.Repository, graph codegraph.Graph, files []providers.RepositorySourceFile, objectives []providers.RepositoryObjective, systems []providers.RepositorySystemContext, budget int64, options Options) (providers.RepositoryAnalysisResult, error) {
	chunks, err := partitionRepository(files, budget*80/100)
	if err != nil {
		return providers.RepositoryAnalysisResult{}, err
	}
	summaries := make([]providers.RepositorySectionResult, 0, len(chunks))
	aggregate := providers.RepositoryAnalysisResult{Provider: options.Provider, Model: options.Model}
	adaptiveTokenLimit := 0
	for index := 0; index < len(chunks); {
		chunk := chunks[index]
		chunkGraph := repositoryGraphContext(graph, chunk.files)
		chunkBytes := requestContextBytes(chunk.files, chunkGraph)
		if chunkBytes > budget && chunk.maxOutputTokens == 0 {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("subsystem %s requires %d context bytes, exceeding the configured budget of %d bytes after repository graph construction", chunk.scope, chunkBytes, budget)
		}
		if err := progress(options, Progress{Stage: "subsystem", Completed: index, Total: len(chunks), Scope: chunk.scope, InputBytes: chunkBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		maxOutputTokens := chunk.maxOutputTokens
		if maxOutputTokens == 0 {
			maxOutputTokens = repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
		}
		request := providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisSubsystem, Scope: chunk.scope,
			RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), Files: chunk.files,
			Objectives: objectives, Systems: systems, Graph: chunkGraph, MaxOutputTokens: maxOutputTokens,
		}
		result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge {
				adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, rateLimit.LimitTokens)
				parts, split := splitRepositoryChunk(chunk, repositoryOutputTokens(options.Provider, adaptiveTokenLimit))
				if !split {
					return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze subsystem %s: provider token limit %d is too small for the smallest safe repository segment: %w", chunk.scope, rateLimit.LimitTokens, err)
				}
				chunks = replaceRepositoryChunk(chunks, index, parts)
				if err := progress(options, Progress{
					Stage: "adaptive-split", Completed: index, Total: len(chunks), Scope: chunk.scope,
					InputBytes: chunkBytes, Detail: fmt.Sprintf("provider limit %d tokens; split into %d smaller part(s)", rateLimit.LimitTokens, len(parts)),
				}); err != nil {
					return providers.RepositoryAnalysisResult{}, err
				}
				continue
			}
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze subsystem %s: %w", chunk.scope, err)
		}
		if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership); err != nil {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("analyze subsystem %s: %w", chunk.scope, err)
		}
		summaries = append(summaries, result.Result)
		addUsage(&aggregate.Usage, result.Usage)
		aggregate.Coverage.FilesSubmitted += result.Coverage.FilesSubmitted
		aggregate.Coverage.BytesSubmitted += result.Coverage.BytesSubmitted
		aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
		if err := progress(options, Progress{Stage: "subsystem", Completed: index + 1, Total: len(chunks), Scope: chunk.scope, InputBytes: chunkBytes}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		index++
	}
	levels := 0
	for len(summaries) > 1 {
		levels++
		plainGroups := partitionSummaries(summaries, budget)
		groups := make([]repositorySummaryGroup, len(plainGroups))
		for index := range plainGroups {
			groups[index] = repositorySummaryGroup{summaries: plainGroups[index]}
		}
		next := make([]providers.RepositorySectionResult, 0, len(groups))
		for index := 0; index < len(groups); {
			group := groups[index]
			scope := fmt.Sprintf("synthesis-level-%d-part-%d", levels, index+1)
			if err := progress(options, Progress{Stage: "synthesis", Completed: index, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group.summaries)}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
			maxOutputTokens := group.maxOutputTokens
			if maxOutputTokens == 0 {
				maxOutputTokens = repositoryOutputTokens(options.Provider, adaptiveTokenLimit)
			}
			request := providers.RepositoryAnalysisRequest{
				Mode: providers.RepositoryAnalysisSynthesis, Scope: scope,
				RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: citedFileIndex(group.summaries, files),
				Objectives: objectives, Systems: systems, SubsystemSummaries: group.summaries, MaxOutputTokens: maxOutputTokens,
			}
			result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
			if err != nil {
				if rateLimit, ok := providers.AsRemoteRateLimitError(err); ok && rateLimit.RequestTooLarge {
					adaptiveTokenLimit = smallerPositive(adaptiveTokenLimit, rateLimit.LimitTokens)
					parts, split := splitRepositorySummaryGroup(group, repositoryOutputTokens(options.Provider, adaptiveTokenLimit))
					if !split {
						return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis level %d part %d: provider token limit %d is too small for one subsystem summary: %w", levels, index+1, rateLimit.LimitTokens, err)
					}
					groups = replaceRepositorySummaryGroup(groups, index, parts)
					if err := progress(options, Progress{
						Stage: "adaptive-split", Completed: index, Total: len(groups), Scope: scope,
						InputBytes: summaryBytes(group.summaries), Detail: fmt.Sprintf("provider limit %d tokens; split synthesis into %d smaller part(s)", rateLimit.LimitTokens, len(parts)),
					}); err != nil {
						return providers.RepositoryAnalysisResult{}, err
					}
					continue
				}
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis level %d part %d: %w", levels, index+1, err)
			}
			if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership); err != nil {
				return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis level %d part %d: %w", levels, index+1, err)
			}
			next = append(next, result.Result)
			addUsage(&aggregate.Usage, result.Usage)
			aggregate.Coverage.CitationsChecked += result.Coverage.CitationsChecked
			if err := progress(options, Progress{Stage: "synthesis", Completed: index + 1, Total: len(groups), Scope: scope, InputBytes: summaryBytes(group.summaries)}); err != nil {
				return providers.RepositoryAnalysisResult{}, err
			}
			index++
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
		request := providers.RepositoryAnalysisRequest{
			Mode: providers.RepositoryAnalysisSynthesis, Scope: "repository-synthesis",
			RepositoryFiles: len(repository.Files), RepositoryBytes: repositorySize(repository), FileIndex: citedFileIndex(group, files),
			Objectives: objectives, Systems: systems, SubsystemSummaries: group,
			MaxOutputTokens: repositoryOutputTokens(options.Provider, adaptiveTokenLimit),
		}
		result, err := reviewRepositoryWithRetry(ctx, reviewer, request, options)
		if err != nil {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("synthesize repository analysis: %w", err)
		}
		if err := validateSystemAttribution(result.Result, profileSystems(systems), options.Ownership); err != nil {
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

type repositorySummaryGroup struct {
	summaries       []providers.RepositorySectionResult
	maxOutputTokens int
}

type repositoryChunk struct {
	scope           string
	files           []providers.RepositorySourceFile
	maxOutputTokens int
}

func reviewRepositoryWithRetry(ctx context.Context, reviewer Reviewer, request providers.RepositoryAnalysisRequest, options Options) (providers.RepositoryAnalysisResult, error) {
	for attempt := 0; ; attempt++ {
		result, err := reviewer.ReviewRepository(ctx, request)
		if err == nil {
			return result, nil
		}
		rateLimit, ok := providers.AsRemoteRateLimitError(err)
		if !ok || rateLimit.RequestTooLarge || attempt >= maxRateLimitRetries {
			return providers.RepositoryAnalysisResult{}, err
		}
		delay := rateLimit.RetryAfter
		if delay <= 0 {
			delay = defaultRateLimitWait
		}
		if delay > maxRateLimitWait {
			return providers.RepositoryAnalysisResult{}, fmt.Errorf("provider requested a rate-limit wait of %s, exceeding the safe automatic wait of %s: %w", delay.Round(time.Second), maxRateLimitWait, err)
		}
		if err := progress(options, Progress{
			Stage: "rate-limit-wait", Completed: attempt + 1, Total: maxRateLimitRetries,
			Scope: request.Scope, Wait: delay, Detail: "temporary provider token limit",
		}); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
		wait := options.Wait
		if wait == nil {
			wait = waitForRateLimit
		}
		if err := wait(ctx, delay); err != nil {
			return providers.RepositoryAnalysisResult{}, err
		}
	}
}

func waitForRateLimit(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func repositoryOutputTokens(provider providers.Kind, tokenLimit int) int {
	if provider == providers.Ollama || tokenLimit <= 0 {
		return 8192
	}
	value := tokenLimit / 5
	if value < minimumAdaptiveOutput {
		return minimumAdaptiveOutput
	}
	if value > 4096 {
		return 4096
	}
	return value
}

func smallerPositive(current, candidate int) int {
	if candidate <= 0 {
		return current
	}
	if current == 0 || candidate < current {
		return candidate
	}
	return current
}

func splitRepositoryChunk(chunk repositoryChunk, maxOutputTokens int) ([]repositoryChunk, bool) {
	if len(chunk.files) > 1 {
		split := balancedFileSplit(chunk.files)
		if split <= 0 || split >= len(chunk.files) {
			return nil, false
		}
		return []repositoryChunk{
			{scope: chunk.scope + " (adaptive part 1)", files: append([]providers.RepositorySourceFile(nil), chunk.files[:split]...), maxOutputTokens: maxOutputTokens},
			{scope: chunk.scope + " (adaptive part 2)", files: append([]providers.RepositorySourceFile(nil), chunk.files[split:]...), maxOutputTokens: maxOutputTokens},
		}, true
	}
	if len(chunk.files) != 1 {
		return nil, false
	}
	left, right, ok := splitRepositorySourceFile(chunk.files[0])
	if !ok {
		return nil, false
	}
	return []repositoryChunk{
		{scope: chunk.scope + " (adaptive segment 1)", files: []providers.RepositorySourceFile{left}, maxOutputTokens: maxOutputTokens},
		{scope: chunk.scope + " (adaptive segment 2)", files: []providers.RepositorySourceFile{right}, maxOutputTokens: maxOutputTokens},
	}, true
}

func balancedFileSplit(files []providers.RepositorySourceFile) int {
	total := sourceFileBytes(files)
	var size int64
	for index := 0; index < len(files)-1; index++ {
		size += sourceFileBytes(files[index : index+1])
		if size >= total/2 {
			return index + 1
		}
	}
	return len(files) / 2
}

func splitRepositorySourceFile(file providers.RepositorySourceFile) (providers.RepositorySourceFile, providers.RepositorySourceFile, bool) {
	if len(file.Content) < 2 {
		return providers.RepositorySourceFile{}, providers.RepositorySourceFile{}, false
	}
	start := file.ContentStartLine
	if start <= 0 {
		start = 1
	}
	middle := len(file.Content) / 2
	leftBreak := strings.LastIndex(file.Content[:middle], "\n")
	rightBreak := strings.Index(file.Content[middle:], "\n")
	split := middle
	if leftBreak >= 0 {
		split = leftBreak + 1
	}
	if rightBreak >= 0 && (leftBreak < 0 || rightBreak < middle-leftBreak) {
		split = middle + rightBreak + 1
	}
	if split <= 0 || split >= len(file.Content) {
		return providers.RepositorySourceFile{}, providers.RepositorySourceFile{}, false
	}
	left, right := file, file
	left.Content = file.Content[:split]
	right.Content = file.Content[split:]
	left.ContentStartLine = start
	right.ContentStartLine = start + strings.Count(left.Content, "\n")
	return left, right, true
}

func replaceRepositoryChunk(values []repositoryChunk, index int, replacements []repositoryChunk) []repositoryChunk {
	result := make([]repositoryChunk, 0, len(values)-1+len(replacements))
	result = append(result, values[:index]...)
	result = append(result, replacements...)
	result = append(result, values[index+1:]...)
	return result
}

func splitRepositorySummaryGroup(group repositorySummaryGroup, maxOutputTokens int) ([]repositorySummaryGroup, bool) {
	if len(group.summaries) < 3 {
		return nil, false
	}
	middle := len(group.summaries) / 2
	return []repositorySummaryGroup{
		{summaries: append([]providers.RepositorySectionResult(nil), group.summaries[:middle]...), maxOutputTokens: maxOutputTokens},
		{summaries: append([]providers.RepositorySectionResult(nil), group.summaries[middle:]...), maxOutputTokens: maxOutputTokens},
	}, true
}

func replaceRepositorySummaryGroup(values []repositorySummaryGroup, index int, replacements []repositorySummaryGroup) []repositorySummaryGroup {
	result := make([]repositorySummaryGroup, 0, len(values)-1+len(replacements))
	result = append(result, values[:index]...)
	result = append(result, replacements...)
	result = append(result, values[index+1:]...)
	return result
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
			segments, err := repositoryFileSegments(file, budget)
			if err != nil {
				return nil, err
			}
			for _, segment := range segments {
				fileSize := int64(len(segment.Content) + len(segment.Path) + 100)
				if len(current.files) > 0 && size+fileSize > budget {
					current.scope = fmt.Sprintf("%s (part %d)", scope, part)
					chunks = append(chunks, current)
					part++
					current = repositoryChunk{scope: scope}
					size = 0
				}
				current.files = append(current.files, segment)
				size += fileSize
			}
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

func repositoryFileSegments(file providers.RepositorySourceFile, budget int64) ([]providers.RepositorySourceFile, error) {
	queue := []providers.RepositorySourceFile{file}
	result := make([]providers.RepositorySourceFile, 0, 1)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		fileSize := int64(len(current.Content) + len(current.Path) + 100)
		if fileSize <= budget {
			result = append(result, current)
			continue
		}
		left, right, ok := splitRepositorySourceFile(current)
		if !ok {
			return nil, fmt.Errorf("repository file %s requires %d bytes and cannot be safely divided to fit the configured analysis budget of %d bytes", file.Path, fileSize, budget)
		}
		queue = append([]providers.RepositorySourceFile{left, right}, queue...)
	}
	return result, nil
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
			Path: file.Path, Kind: string(file.Kind), LineCount: strings.Count(content, "\n") + 1, ContentStartLine: 1, Content: content,
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

func repositorySystems(systems []profile.System, rules []ownership.Rule) []providers.RepositorySystemContext {
	result := make([]providers.RepositorySystemContext, 0, len(systems))
	for _, system := range systems {
		facts := []string{
			"Intended purpose: " + system.IntendedPurpose,
			"Lifecycle stage: " + string(system.LifecycleStage),
			"Decision impact: " + string(system.DecisionImpact),
			"Human oversight: " + string(system.HumanOversight),
		}
		paths := []string{}
		for _, rule := range rules {
			for _, owner := range rule.Systems {
				if owner == system.ID {
					paths = append(paths, rule.Paths...)
					break
				}
			}
		}
		if len(systems) > 1 && len(paths) == 0 {
			facts = append(facts, "Repository path ownership is not established for this system; leave system_id empty unless cited evidence is explicitly owned by this system.")
		}
		result = append(result, providers.RepositorySystemContext{ID: system.ID, Name: system.Name, Paths: paths, DeclaredFacts: facts})
	}
	return result
}

func profileSystems(values []providers.RepositorySystemContext) []profile.System {
	result := make([]profile.System, 0, len(values))
	for _, value := range values {
		result = append(result, profile.System{ID: value.ID, Name: value.Name})
	}
	return result
}

func validateSystemAttribution(result providers.RepositorySectionResult, systems []profile.System, rules []ownership.Rule) error {
	if len(systems) <= 1 && len(rules) == 0 {
		return nil
	}
	resolver := ownership.New(rules)
	for _, observation := range result.ObjectiveObservations {
		if observation.SystemID == "" {
			continue
		}
		citations := append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...)
		if len(citations) == 0 {
			if len(systems) == 1 && systems[0].ID == observation.SystemID {
				// An objective with no claimed repository evidence can safely remain
				// associated with the only configured system. There is no evidence
				// path to own and no competing system to confuse it with.
				continue
			}
			return fmt.Errorf("objective %q attributed system %q without cited evidence", observation.ObjectiveID, observation.SystemID)
		}
		if !resolver.Configured() {
			return fmt.Errorf("objective %q attributed evidence to system %q without configured path ownership", observation.ObjectiveID, observation.SystemID)
		}
		for _, citation := range citations {
			resolution := resolver.Resolve(citation.Path)
			if resolution.Status == ownership.StatusConflicting || !containsSystem(resolution.Systems, observation.SystemID) {
				return fmt.Errorf("objective %q attributed %s to system %q but path ownership is %s for %v", observation.ObjectiveID, citation.Path, observation.SystemID, resolution.Status, resolution.Systems)
			}
		}
	}
	return nil
}

func containsSystem(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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

func requestContextBytes(files []providers.RepositorySourceFile, graph providers.RepositoryGraphContext) int64 {
	encoded, _ := json.Marshal(graph)
	return sourceFileBytes(files) + int64(len(encoded))
}

func repositoryGraphContext(graph codegraph.Graph, files []providers.RepositorySourceFile) providers.RepositoryGraphContext {
	type submittedRange struct{ start, end int }
	paths := make(map[string]submittedRange, len(files))
	for _, file := range files {
		start := file.ContentStartLine
		if start <= 0 {
			start = 1
		}
		paths[file.Path] = submittedRange{start: start, end: start + strings.Count(file.Content, "\n")}
	}
	result := providers.RepositoryGraphContext{}
	for _, language := range graph.Languages {
		result.Languages = append(result.Languages, string(language))
	}
	for _, path := range graph.UnsupportedSourceFiles {
		if _, included := paths[path]; included {
			result.UnsupportedSourceFiles = append(result.UnsupportedSourceFiles, path)
		}
	}
	for _, path := range graph.IndexedSourceFiles {
		if _, included := paths[path]; included {
			result.IndexedSourceFiles++
		}
	}
	names := make(map[string]string, len(graph.Symbols))
	for _, symbol := range graph.Symbols {
		name := symbol.QualifiedName
		if name == "" {
			name = symbol.Name
		}
		names[symbol.ID] = name
		submitted, included := paths[symbol.Path]
		if !included || symbol.StartLine < submitted.start || symbol.EndLine > submitted.end {
			continue
		}
		result.Symbols = append(result.Symbols, providers.RepositoryGraphSymbol{
			Name: name, Kind: string(symbol.Kind), Path: symbol.Path, StartLine: symbol.StartLine, EndLine: symbol.EndLine, Reachability: string(symbol.Reachability),
		})
	}
	for _, imported := range graph.Imports {
		if _, included := paths[imported.Path]; included {
			result.Imports = append(result.Imports, providers.RepositoryGraphImport{Path: imported.Path, ImportedPath: imported.ImportedPath})
		}
	}
	for _, edge := range graph.Edges {
		submitted, included := paths[edge.Path]
		if !included || edge.Line < submitted.start || edge.Line > submitted.end {
			continue
		}
		from := names[edge.From]
		if from == "" {
			from = edge.From
		}
		to := names[edge.To]
		if to == "" {
			to = edge.To
		}
		result.Relationships = append(result.Relationships, providers.RepositoryGraphRelationship{
			Kind: string(edge.Kind), From: from, To: to, Label: edge.Label, Path: edge.Path, Line: edge.Line, Resolved: edge.Resolved,
		})
	}
	return result
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
