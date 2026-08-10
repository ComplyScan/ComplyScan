// Package profiledraft builds the bounded repository input used for optional
// model-assisted onboarding and its maintained quality gate.
package profiledraft

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const MaxContexts = 24

// DeterministicSuggestions returns conservative repository-evident defaults
// that remain available when no model is selected or ready.
func DeterministicSuggestions(report inventory.Report) map[string]providers.ProfileSuggestion {
	result := make(map[string]providers.ProfileSuggestion)
	if report.Summary.RuntimeSignals == 0 {
		return result
	}
	evidence := make([]providers.ProfileEvidence, 0, 3)
	for _, component := range report.Components {
		for _, location := range component.Locations {
			if location.Scope != inventory.ScopeRuntime {
				continue
			}
			evidence = append(evidence, providers.ProfileEvidence{
				Path: location.Path, Line: location.Line,
				Summary: fmt.Sprintf("%s runtime evidence: %s", component.Name, location.Evidence),
			})
			if len(evidence) == 3 {
				break
			}
		}
		if len(evidence) == 3 {
			break
		}
	}
	if len(evidence) > 0 {
		result["ai-activities"] = providers.ProfileSuggestion{
			Field: "ai-activities", Values: []string{"inference"}, Confidence: "medium",
			Rationale: "Runtime AI-provider or framework usage supports an inference candidate, but does not establish the complete product workflow.",
			Evidence:  evidence,
		}
	}
	return result
}

// BuildRequest selects the exact bounded repository contexts sent to a profile
// drafter. Provider-level sanitization applies redaction and per-context limits
// again immediately before the request leaves the process.
func BuildRequest(target string, repository discovery.Repository, languages []string, report inventory.Report) providers.ProfileDraftRequest {
	name := filepath.Base(filepath.Clean(target))
	components := make([]string, 0, len(report.Components))
	anchorLines := make(map[string][]int)
	for _, component := range report.Components {
		components = append(components, component.Name)
		for _, location := range component.Locations {
			anchorLines[location.Path] = append(anchorLines[location.Path], location.Line)
		}
	}
	type rankedFile struct {
		file  discovery.File
		score int
	}
	ranked := make([]rankedFile, 0, len(repository.Files))
	for _, file := range repository.Files {
		score := fileScore(file, anchorLines[file.Path])
		if score > 0 {
			ranked = append(ranked, rankedFile{file: file, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].file.Path < ranked[j].file.Path
	})
	contexts := make([]providers.ProfileSourceContext, 0, len(ranked))
	for _, candidate := range ranked {
		contexts = append(contexts, providers.ProfileSourceContext{
			Path:   candidate.file.Path,
			Kind:   string(candidate.file.Kind),
			Source: sourceContext(candidate.file, anchorLines[candidate.file.Path]),
		})
		if len(contexts) == MaxContexts {
			break
		}
	}
	return providers.ProfileDraftRequest{
		RepositoryName: name,
		Languages:      append([]string(nil), languages...),
		Components:     components,
		Contexts:       contexts,
	}
}

// Languages returns the indexed-language names disclosed in the profile-draft
// request. Unsupported languages remain represented by their file contexts.
func Languages(repository discovery.Repository) []string {
	seen := map[string]struct{}{}
	for _, file := range repository.Files {
		if language := languageForPath(file.Path); language != "" {
			seen[language] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for language := range seen {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}

func fileScore(file discovery.File, anchors []int) int {
	switch file.Kind {
	case discovery.KindReadme, discovery.KindModelCard:
		return 100
	case discovery.KindManifest:
		return 90
	case discovery.KindDockerfile, discovery.KindGitHubAction, discovery.KindCI, discovery.KindTerraform, discovery.KindEnvTemplate, discovery.KindConfig:
		return 80
	case discovery.KindPrivacy, discovery.KindRisk, discovery.KindAIGovernance:
		return 70
	case discovery.KindSource:
		if len(anchors) > 0 {
			return 95
		}
	case discovery.KindDocumentation:
		return 40
	}
	return 0
}

func sourceContext(file discovery.File, anchors []int) string {
	lines := strings.Split(strings.ReplaceAll(string(file.Content), "\r\n", "\n"), "\n")
	selected := make(map[int]struct{})
	if len(anchors) == 0 {
		limit := len(lines)
		if limit > 100 {
			limit = 100
		}
		for index := 0; index < limit; index++ {
			selected[index] = struct{}{}
		}
	} else {
		for _, line := range anchors {
			start := line - 12
			if start < 0 {
				start = 0
			}
			end := line + 11
			if end > len(lines) {
				end = len(lines)
			}
			for index := start; index < end; index++ {
				selected[index] = struct{}{}
			}
		}
	}
	indexes := make([]int, 0, len(selected))
	for index := range selected {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	var result strings.Builder
	for _, index := range indexes {
		fmt.Fprintf(&result, "%d: %s\n", index+1, lines[index])
	}
	return result.String()
}

func languageForPath(path string) string {
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
