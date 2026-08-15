package aiuse

import (
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	ignore "github.com/sabhiram/go-gitignore"
)

type ObservationStatus string

const (
	ObservationModelReviewed   ObservationStatus = "observed-in-ai-review"
	ObservationTechnicalSignal ObservationStatus = "matching-technical-signal"
	ObservationNotReviewed     ObservationStatus = "not-reviewed"
)

type SuggestionStatus string

const (
	SuggestionNew           SuggestionStatus = "new"
	SuggestionPossibleMatch SuggestionStatus = "possible-match"
	SuggestionAmbiguous     SuggestionStatus = "ambiguous-match"
)

type SignalLocation struct {
	Component string                  `json:"component"`
	Kind      inventory.ComponentKind `json:"kind"`
	Path      string                  `json:"path"`
	Line      int                     `json:"line,omitempty"`
	Scope     inventory.Scope         `json:"scope"`
}

type ObservedUse struct {
	Use              Use                            `json:"use"`
	Observation      ObservationStatus              `json:"observation"`
	ReviewEvidence   []providers.RepositoryCitation `json:"review_evidence,omitempty"`
	TechnicalSignals []SignalLocation               `json:"technical_signals,omitempty"`
}

type Suggestion struct {
	Fingerprint         string                         `json:"fingerprint"`
	Name                string                         `json:"name"`
	Purpose             string                         `json:"purpose"`
	Lifecycle           string                         `json:"lifecycle,omitempty"`
	Confidence          string                         `json:"confidence"`
	Status              SuggestionStatus               `json:"status"`
	PossibleUseIDs      []string                       `json:"possible_use_ids,omitempty"`
	Evidence            []providers.RepositoryCitation `json:"evidence"`
	UnresolvedQuestions []string                       `json:"unresolved_questions,omitempty"`
}

type SnapshotSummary struct {
	Confirmed        int `json:"confirmed"`
	Draft            int `json:"draft"`
	Retired          int `json:"retired"`
	Suggested        int `json:"suggested"`
	UngroupedSignals int `json:"ungrouped_signals"`
}

// Snapshot combines the immutable input manifest with current local signals
// and optional model suggestions. It never mutates or writes the manifest.
type Snapshot struct {
	ManifestPath     string           `json:"manifest_path"`
	ChangedScope     bool             `json:"changed_scope"`
	Summary          SnapshotSummary  `json:"summary"`
	Confirmed        []ObservedUse    `json:"confirmed"`
	Draft            []ObservedUse    `json:"draft"`
	Retired          []ObservedUse    `json:"retired"`
	Suggested        []Suggestion     `json:"suggested"`
	UngroupedSignals []SignalLocation `json:"ungrouped_signals"`
}

// BuildSnapshot overlays current observations onto every saved use. Absence
// from any review scope is represented as not-reviewed and can never remove,
// retire, or otherwise mutate a durable entry.
func BuildSnapshot(manifest Manifest, technical inventory.Report, analysis *providers.RepositoryAnalysisResult, changedScope bool) Snapshot {
	value := Snapshot{
		ManifestPath:     DefaultPath,
		ChangedScope:     changedScope,
		Confirmed:        []ObservedUse{},
		Draft:            []ObservedUse{},
		Retired:          []ObservedUse{},
		Suggested:        []Suggestion{},
		UngroupedSignals: []SignalLocation{},
	}
	observed := make(map[string]*ObservedUse, len(manifest.Uses))
	for _, definition := range manifest.Uses {
		copy := definition
		observed[definition.ID] = &ObservedUse{
			Use: copy, Observation: ObservationNotReviewed,
			ReviewEvidence: []providers.RepositoryCitation{}, TechnicalSignals: []SignalLocation{},
		}
	}

	for _, component := range technical.Components {
		for _, location := range component.Locations {
			signal := SignalLocation{
				Component: component.Name, Kind: component.Kind, Path: location.Path,
				Line: location.Line, Scope: location.Scope,
			}
			matched := false
			for _, definition := range manifest.Uses {
				if !UseMatchesPath(definition, location.Path) {
					continue
				}
				matched = true
				entry := observed[definition.ID]
				entry.TechnicalSignals = append(entry.TechnicalSignals, signal)
				if entry.Observation == ObservationNotReviewed {
					entry.Observation = ObservationTechnicalSignal
				}
			}
			if !matched {
				value.UngroupedSignals = append(value.UngroupedSignals, signal)
			}
		}
	}

	if analysis != nil {
		for _, candidate := range analysis.Result.AIUses {
			if IsDismissed(manifest, candidate) {
				continue
			}
			linked := LinkedSuggestionUses(manifest, candidate)
			confirmedLink := false
			for _, id := range linked {
				entry := observed[id]
				entry.Observation = ObservationModelReviewed
				entry.ReviewEvidence = append([]providers.RepositoryCitation(nil), candidate.Evidence...)
				if entry.Use.Status == StatusActive && entry.Use.Review.Status == profile.ReviewConfirmed {
					confirmedLink = true
				}
			}
			if confirmedLink {
				continue
			}
			matches := uniqueSortedStrings(append(linked, MatchSuggestion(manifest, candidate)...))
			status := SuggestionNew
			if len(matches) == 1 {
				status = SuggestionPossibleMatch
			} else if len(matches) > 1 {
				status = SuggestionAmbiguous
			}
			value.Suggested = append(value.Suggested, Suggestion{
				Fingerprint: SuggestionFingerprint(candidate), Name: candidate.Name, Purpose: candidate.Purpose,
				Lifecycle: candidate.Lifecycle, Confidence: candidate.Confidence, Status: status,
				PossibleUseIDs: matches, Evidence: append([]providers.RepositoryCitation(nil), candidate.Evidence...),
				UnresolvedQuestions: append([]string(nil), candidate.UnresolvedQuestions...),
			})
		}
	}

	ids := make([]string, 0, len(observed))
	for id := range observed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := *observed[id]
		sortSignalLocations(entry.TechnicalSignals)
		sort.SliceStable(entry.ReviewEvidence, func(i, j int) bool {
			if entry.ReviewEvidence[i].Path != entry.ReviewEvidence[j].Path {
				return entry.ReviewEvidence[i].Path < entry.ReviewEvidence[j].Path
			}
			return entry.ReviewEvidence[i].Line < entry.ReviewEvidence[j].Line
		})
		switch {
		case entry.Use.Status == StatusRetired:
			value.Retired = append(value.Retired, entry)
		case entry.Use.Review.Status == profile.ReviewConfirmed:
			value.Confirmed = append(value.Confirmed, entry)
		default:
			value.Draft = append(value.Draft, entry)
		}
	}
	sort.SliceStable(value.Suggested, func(i, j int) bool {
		if value.Suggested[i].Name != value.Suggested[j].Name {
			return value.Suggested[i].Name < value.Suggested[j].Name
		}
		return value.Suggested[i].Fingerprint < value.Suggested[j].Fingerprint
	})
	sortSignalLocations(value.UngroupedSignals)
	value.Summary = SnapshotSummary{
		Confirmed: len(value.Confirmed), Draft: len(value.Draft), Retired: len(value.Retired),
		Suggested: len(value.Suggested), UngroupedSignals: len(value.UngroupedSignals),
	}
	return value
}

// MatchSuggestion returns possible durable matches when every cited path
// belongs to a saved use. It never establishes identity: separate product uses
// may intentionally share the same repository paths.
func MatchSuggestion(manifest Manifest, suggestion providers.RepositoryAIUse) []string {
	paths := SuggestionPaths(suggestion)
	if len(paths) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(manifest.Uses))
	for _, definition := range manifest.Uses {
		matchesAll := true
		for _, path := range paths {
			if !UseMatchesPath(definition, path) {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			result = append(result, definition.ID)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func UseMatchesPath(definition Use, path string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	for _, pattern := range definition.Paths {
		if ignore.CompileIgnoreLines(pattern).MatchesPath(path) {
			return true
		}
	}
	return false
}

func sortSignalLocations(values []SignalLocation) {
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].Path != values[j].Path {
			return values[i].Path < values[j].Path
		}
		if values[i].Line != values[j].Line {
			return values[i].Line < values[j].Line
		}
		return values[i].Component < values[j].Component
	})
}
