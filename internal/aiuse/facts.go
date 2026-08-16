package aiuse

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/inventory"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

// FactSource identifies whether a code fact came from a local mechanical
// signal or from the optional repository model pass.
type FactSource string

const (
	FactSourceDeterministic FactSource = "deterministic"
	FactSourceModel         FactSource = "model"
	FactSourceCombined      FactSource = "deterministic-and-model"
)

// FactCoverage records the repository boundary that was available to the
// fact-producing layer. Deterministic facts always use the complete discovered
// repository even when the model is restricted to a pull-request scope.
type FactCoverage string

const (
	FactCoverageFullRepository      FactCoverage = "full-repository"
	FactCoverageChangedAndConnected FactCoverage = "changed-plus-connected"
)

// FactStrength describes how a technical observation was produced. It does
// not express legal confidence or operational effectiveness.
type FactStrength string

const (
	FactStrengthDirectSignal   FactStrength = "direct-technical-signal"
	FactStrengthDirectArtifact FactStrength = "direct-artifact"
	FactStrengthModelReasoned  FactStrength = "model-reasoned"
)

type FactReviewStatus string

const (
	FactReviewDeterministicOnly FactReviewStatus = "deterministic-only"
	FactReviewModelReviewed     FactReviewStatus = "model-reviewed"
)

// Fact is a positive, code-level observation. Missing facts are represented by
// an absent observation or an unresolved question, never by a negative claim.
type Fact struct {
	Field      profile.CodeFactField          `json:"field"`
	Values     []string                       `json:"values"`
	Confidence string                         `json:"confidence"`
	Source     FactSource                     `json:"source"`
	Coverage   FactCoverage                   `json:"coverage"`
	Strength   FactStrength                   `json:"strength"`
	Rationale  string                         `json:"rationale"`
	Evidence   []providers.RepositoryCitation `json:"evidence"`
}

// ModelProviderObservation preserves a concrete provider signal independently
// from the canonical profile fact taxonomy. It says only that repository code
// refers to a provider in a runtime path.
type ModelProviderObservation struct {
	Name       string                         `json:"name"`
	Confidence string                         `json:"confidence"`
	Source     FactSource                     `json:"source"`
	Coverage   FactCoverage                   `json:"coverage"`
	Strength   FactStrength                   `json:"strength"`
	Evidence   []providers.RepositoryCitation `json:"evidence"`
}

// FactReview groups current scan observations for one AI use. A nil review
// means no fact-producing layer covered the use; an empty model-reviewed value
// means the model covered it but returned no positive supported fact.
type FactReview struct {
	Status                FactReviewStatus           `json:"status"`
	DeterministicCoverage FactCoverage               `json:"deterministic_coverage,omitempty"`
	ModelCoverage         FactCoverage               `json:"model_coverage,omitempty"`
	Facts                 []Fact                     `json:"facts"`
	ModelProviders        []ModelProviderObservation `json:"model_providers,omitempty"`
	UnresolvedQuestions   []string                   `json:"unresolved_questions,omitempty"`
}

type TechnicalRole string
type RoleCandidateStatus string

const (
	TechnicalRoleDeployer           TechnicalRole = "deployer"
	TechnicalRoleProvider           TechnicalRole = "provider"
	TechnicalRoleDownstreamProvider TechnicalRole = "downstream-provider"
)

const RoleCandidatePossible RoleCandidateStatus = "possible"

// RoleCandidate is an explicitly non-legal interpretation of repository
// behavior. Condition and MissingOrganizationFacts make clear what must still
// be established outside the codebase before an EU AI Act role can be assigned.
type RoleCandidate struct {
	Role                     TechnicalRole                  `json:"role"`
	Status                   RoleCandidateStatus            `json:"status"`
	Confidence               string                         `json:"confidence"`
	Source                   FactSource                     `json:"source"`
	Coverage                 FactCoverage                   `json:"coverage"`
	Strength                 FactStrength                   `json:"strength"`
	Rationale                string                         `json:"rationale"`
	Condition                string                         `json:"condition"`
	MissingOrganizationFacts []string                       `json:"missing_organization_facts"`
	Evidence                 []providers.RepositoryCitation `json:"evidence"`
}

var fixedOrganizationUnknowns = []string{
	"Which organisation develops, uses, imports, distributes, or places the AI system on the market cannot be established from repository code.",
	"Whether the AI system is offered or put into service under an organisation's own name or trademark cannot be established from repository code.",
	"The contracts, customer relationships, operating regions, and actual production use that determine legal roles cannot be established from repository code.",
}

type distributionEvidence struct {
	packaged bool
	localCLI bool
	evidence []providers.RepositoryCitation
}

func deterministicFactReview(definition Use, technical inventory.Report, repository discovery.Repository) (*FactReview, distributionEvidence) {
	providersObserved := scopedModelProviders(definition, technical)
	distribution := inspectDistribution(definition, repository)
	facts := make([]Fact, 0, 1)
	if len(facts) == 0 && len(providersObserved) == 0 {
		return nil, distribution
	}
	review := &FactReview{
		Status: FactReviewDeterministicOnly, DeterministicCoverage: FactCoverageFullRepository,
		Facts: facts, ModelProviders: providersObserved,
	}
	normalizeFactReview(review)
	return review, distribution
}

func scopedModelProviders(definition Use, technical inventory.Report) []ModelProviderObservation {
	byName := make(map[string]ModelProviderObservation)
	for _, component := range technical.Components {
		if component.Kind != inventory.KindProvider {
			continue
		}
		for _, location := range component.Locations {
			if location.Scope != inventory.ScopeRuntime || !UseMatchesPath(definition, location.Path) {
				continue
			}
			observation := byName[component.Name]
			observation.Name = component.Name
			confidence := component.Confidence
			if confidence != "low" && confidence != "medium" && confidence != "high" {
				confidence = "medium"
			}
			observation.Confidence = conservativeConfidence(observation.Confidence, confidence)
			observation.Source = FactSourceDeterministic
			observation.Coverage = FactCoverageFullRepository
			observation.Strength = FactStrengthDirectSignal
			line := location.Line
			if line < 1 {
				line = 1
			}
			summary := strings.TrimSpace(location.Evidence)
			if summary == "" {
				summary = component.Name + " runtime provider signal"
			}
			observation.Evidence = appendUniqueCitations(observation.Evidence, providers.RepositoryCitation{
				Path: location.Path, Line: line, Summary: summary,
			})
			byName[component.Name] = observation
		}
	}
	result := make([]ModelProviderObservation, 0, len(byName))
	for _, observation := range byName {
		result = append(result, observation)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func providerObservationEvidence(values []ModelProviderObservation) []providers.RepositoryCitation {
	var result []providers.RepositoryCitation
	for _, value := range values {
		result = appendUniqueCitations(result, value.Evidence...)
	}
	return result
}

func modelFactReview(set providers.RepositoryAIUseFactSet, changedScope bool) *FactReview {
	coverage := FactCoverageFullRepository
	if changedScope {
		coverage = FactCoverageChangedAndConnected
	}
	review := &FactReview{
		Status: FactReviewModelReviewed, ModelCoverage: coverage, Facts: make([]Fact, 0, len(set.Facts)),
		UnresolvedQuestions: append([]string(nil), set.UnresolvedQuestions...),
	}
	for _, value := range set.Facts {
		review.Facts = append(review.Facts, Fact{
			Field: value.Field, Values: append([]string(nil), value.Values...), Confidence: value.Confidence,
			Source: FactSourceModel, Coverage: coverage, Strength: FactStrengthModelReasoned,
			Rationale: value.Rationale, Evidence: append([]providers.RepositoryCitation(nil), value.Evidence...),
		})
	}
	normalizeFactReview(review)
	return review
}

func mergeFactReviews(deterministic, model *FactReview) *FactReview {
	if deterministic == nil && model == nil {
		return nil
	}
	result := &FactReview{Status: FactReviewDeterministicOnly, Facts: []Fact{}}
	if deterministic != nil {
		result.DeterministicCoverage = deterministic.DeterministicCoverage
		result.Facts = append(result.Facts, deterministic.Facts...)
		result.ModelProviders = append(result.ModelProviders, deterministic.ModelProviders...)
		result.UnresolvedQuestions = append(result.UnresolvedQuestions, deterministic.UnresolvedQuestions...)
	}
	if model != nil {
		result.Status = FactReviewModelReviewed
		result.ModelCoverage = model.ModelCoverage
		result.Facts = append(result.Facts, model.Facts...)
		result.ModelProviders = append(result.ModelProviders, model.ModelProviders...)
		result.UnresolvedQuestions = append(result.UnresolvedQuestions, model.UnresolvedQuestions...)
	}
	normalizeFactReview(result)
	return result
}

func addDistributionFacts(review *FactReview, distribution distributionEvidence) {
	if review == nil || !distribution.localCLI || !factReviewHasAIActivity(review) {
		return
	}
	for _, fact := range review.Facts {
		if fact.Field == profile.CodeFactDeploymentModels {
			for _, value := range fact.Values {
				if value == string(profile.DeploymentLocalCLI) {
					return
				}
			}
		}
	}
	review.DeterministicCoverage = FactCoverageFullRepository
	review.Facts = append(review.Facts, Fact{
		Field: profile.CodeFactDeploymentModels, Values: []string{string(profile.DeploymentLocalCLI)}, Confidence: "medium",
		Source: FactSourceDeterministic, Coverage: FactCoverageFullRepository, Strength: FactStrengthDirectArtifact,
		Rationale: "Executable command or installation artifacts package the repository as a local command-line tool.",
		Evidence:  appendUniqueCitations(nil, distribution.evidence...),
	})
	normalizeFactReview(review)
}

func normalizeFactReview(review *FactReview) {
	if review == nil {
		return
	}
	for index := range review.Facts {
		review.Facts[index].Values = uniqueSortedStrings(review.Facts[index].Values)
		review.Facts[index].Evidence = appendUniqueCitations(nil, review.Facts[index].Evidence...)
	}
	review.Facts = deduplicateFacts(review.Facts)
	sort.SliceStable(review.Facts, func(i, j int) bool {
		if review.Facts[i].Field != review.Facts[j].Field {
			return review.Facts[i].Field < review.Facts[j].Field
		}
		if review.Facts[i].Source != review.Facts[j].Source {
			return review.Facts[i].Source < review.Facts[j].Source
		}
		return strings.Join(review.Facts[i].Values, "\x00") < strings.Join(review.Facts[j].Values, "\x00")
	})
	review.ModelProviders = deduplicateModelProviders(review.ModelProviders)
	review.UnresolvedQuestions = uniqueSortedStrings(review.UnresolvedQuestions)
}

func deduplicateFacts(values []Fact) []Fact {
	result := make([]Fact, 0, len(values))
	indexByKey := make(map[string]int)
	for _, value := range values {
		key := strings.Join([]string{string(value.Field), strings.Join(value.Values, "\x01"), string(value.Source), string(value.Coverage), string(value.Strength)}, "\x00")
		if index, duplicate := indexByKey[key]; duplicate {
			result[index].Evidence = appendUniqueCitations(result[index].Evidence, value.Evidence...)
			result[index].Confidence = conservativeConfidence(result[index].Confidence, value.Confidence)
			if result[index].Rationale == "" {
				result[index].Rationale = value.Rationale
			}
			continue
		}
		value.Values = append([]string(nil), value.Values...)
		value.Evidence = append([]providers.RepositoryCitation(nil), value.Evidence...)
		indexByKey[key] = len(result)
		result = append(result, value)
	}
	return result
}

func deduplicateModelProviders(values []ModelProviderObservation) []ModelProviderObservation {
	byName := make(map[string]ModelProviderObservation)
	for _, value := range values {
		existing, found := byName[value.Name]
		if !found {
			value.Evidence = appendUniqueCitations(nil, value.Evidence...)
			byName[value.Name] = value
			continue
		}
		existing.Confidence = conservativeConfidence(existing.Confidence, value.Confidence)
		existing.Evidence = appendUniqueCitations(existing.Evidence, value.Evidence...)
		byName[value.Name] = existing
	}
	result := make([]ModelProviderObservation, 0, len(byName))
	for _, value := range byName {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func deriveRoleCandidates(review *FactReview, distribution distributionEvidence) []RoleCandidate {
	if review == nil {
		return []RoleCandidate{}
	}
	providersObserved := review.ModelProviders
	roles := make([]RoleCandidate, 0, 3)
	if len(providersObserved) > 0 {
		roles = append(roles, RoleCandidate{
			Role: TechnicalRoleDeployer, Status: RoleCandidatePossible, Confidence: "low",
			Source: FactSourceDeterministic, Coverage: FactCoverageFullRepository, Strength: FactStrengthDirectSignal,
			Rationale:                "Runtime-scoped source inside this AI use refers to third-party model provider tooling.",
			Condition:                "This would support a deployer role only if the organisation actually uses the AI system under its authority in an operational process.",
			MissingOrganizationFacts: []string{"Which organisation operates the system", "Whether the code is used in an actual professional or production workflow"},
			Evidence:                 providerObservationEvidence(providersObserved),
		})
	}
	if distribution.packaged && factReviewHasAIActivity(review) {
		packageEvidence := appendUniqueCitations(nil, distribution.evidence...)
		packageEvidence = appendUniqueCitations(packageEvidence, factReviewAIEvidence(review)...)
		source, coverage := activityDerivationBoundary(review)
		roles = append(roles, RoleCandidate{
			Role: TechnicalRoleProvider, Status: RoleCandidatePossible, Confidence: "medium",
			Source: source, Coverage: coverage, Strength: FactStrengthDirectArtifact,
			Rationale:                "The repository contains an AI implementation and executable release, installation, or package-distribution artifacts.",
			Condition:                "This would support a provider role only if an organisation develops or places the resulting AI system on the market under its own name or trademark.",
			MissingOrganizationFacts: []string{"Which organisation controls the product", "Whether it is actually placed on the market or put into service", "Whose name or trademark is used"},
			Evidence:                 packageEvidence,
		})
	}
	if distribution.packaged && len(providersObserved) > 0 && factReviewHasAIActivity(review) {
		evidence := appendUniqueCitations(nil, distribution.evidence...)
		evidence = appendUniqueCitations(evidence, providerObservationEvidence(providersObserved)...)
		roles = append(roles, RoleCandidate{
			Role: TechnicalRoleDownstreamProvider, Status: RoleCandidatePossible, Confidence: "medium",
			Source: FactSourceDeterministic, Coverage: FactCoverageFullRepository, Strength: FactStrengthDirectArtifact,
			Rationale:                "The packaged repository integrates a third-party model provider into an AI-enabled product.",
			Condition:                "This would support a downstream-provider role only if an organisation integrates that third-party model into an AI system it provides under its own name or trademark.",
			MissingOrganizationFacts: []string{"The upstream model relationship and terms", "Which organisation integrates and provides the resulting system", "Whose name or trademark is used"},
			Evidence:                 evidence,
		})
	}
	for index := range roles {
		roles[index].MissingOrganizationFacts = uniqueSortedStrings(roles[index].MissingOrganizationFacts)
		roles[index].Evidence = appendUniqueCitations(nil, roles[index].Evidence...)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Role < roles[j].Role })
	return roles
}

func activityDerivationBoundary(review *FactReview) (FactSource, FactCoverage) {
	hasDeterministic := false
	hasModel := false
	coverage := FactCoverageFullRepository
	for _, fact := range review.Facts {
		if fact.Field != profile.CodeFactAIActivities {
			continue
		}
		switch fact.Source {
		case FactSourceModel:
			hasModel = true
			if fact.Coverage == FactCoverageChangedAndConnected && !hasDeterministic {
				coverage = FactCoverageChangedAndConnected
			}
		case FactSourceDeterministic:
			hasDeterministic = true
			coverage = FactCoverageFullRepository
		}
	}
	if hasModel && hasDeterministic {
		return FactSourceCombined, coverage
	}
	if hasModel {
		return FactSourceModel, coverage
	}
	return FactSourceDeterministic, coverage
}

func factReviewHasAIActivity(review *FactReview) bool {
	for _, fact := range review.Facts {
		if fact.Field == profile.CodeFactAIActivities && len(fact.Values) > 0 {
			return true
		}
	}
	return false
}

func factReviewAIEvidence(review *FactReview) []providers.RepositoryCitation {
	var result []providers.RepositoryCitation
	for _, fact := range review.Facts {
		if fact.Field == profile.CodeFactAIActivities {
			result = appendUniqueCitations(result, fact.Evidence...)
		}
	}
	return result
}

func inspectDistribution(definition Use, repository discovery.Repository) distributionEvidence {
	result := distributionEvidence{}
	for _, file := range repository.Files {
		if !UseMatchesPath(definition, file.Path) {
			continue
		}
		path := strings.ToLower(filepath.ToSlash(file.Path))
		base := strings.ToLower(filepath.Base(path))
		if file.Kind == discovery.KindReadme || file.Kind == discovery.KindDocumentation || base == "license" || strings.HasPrefix(base, "license.") {
			continue
		}
		content := strings.ToLower(strings.ReplaceAll(string(file.Content), "\r\n", "\n"))
		packaged, localCLI, pattern, summary := distributionMechanism(path, base, file.Kind, content)
		if !packaged && !localCLI {
			continue
		}
		result.packaged = result.packaged || packaged
		result.localCLI = result.localCLI || localCLI
		line := lineContaining(content, pattern)
		result.evidence = appendUniqueCitations(result.evidence, providers.RepositoryCitation{Path: file.Path, Line: line, Summary: summary})
	}
	return result
}

func distributionMechanism(path, base string, kind discovery.FileKind, content string) (bool, bool, string, string) {
	if (base == "action.yml" || base == "action.yaml") && strings.Contains(content, "runs:") {
		return true, false, "runs:", "GitHub Action metadata defines an executable packaged action"
	}
	if base == ".goreleaser.yml" || base == ".goreleaser.yaml" || base == "goreleaser.yml" || base == "goreleaser.yaml" {
		return true, true, "builds:", "GoReleaser configuration packages command-line release artifacts"
	}
	if kind == discovery.KindGitHubAction || kind == discovery.KindCI {
		for _, marker := range []string{"goreleaser/goreleaser-action", "gh release create", "softprops/action-gh-release", "actions/upload-release-asset", "npm publish", "cargo publish", "gem push", "pypa/gh-action-pypi-publish", "twine upload"} {
			if strings.Contains(content, marker) {
				localCLI := strings.Contains(marker, "goreleaser") || strings.Contains(content, "complyscan_")
				return true, localCLI, marker, "Release workflow publishes installable or packaged artifacts"
			}
		}
	}
	if base == "install.sh" && strings.Contains(content, "curl") &&
		(strings.Contains(content, "releases/download") || strings.Contains(content, "install_dir") || strings.Contains(content, "install -m")) {
		return true, true, "curl", "Installer downloads and installs a command-line release artifact"
	}
	if base == "package.json" {
		var manifest struct {
			Bin json.RawMessage `json:"bin"`
		}
		if json.Unmarshal([]byte(content), &manifest) == nil && packageBinConfigured(manifest.Bin) {
			return true, true, `"bin"`, "Package manifest exposes an installable command-line entry point"
		}
	}
	if base == "pyproject.toml" {
		for _, marker := range []string{"[project.scripts]", "[tool.poetry.scripts]"} {
			if strings.Contains(content, marker) {
				return true, true, marker, "Python package manifest exposes an installable command-line entry point"
			}
		}
	}
	if base == "cargo.toml" && strings.Contains(content, "[[bin]]") {
		return true, true, "[[bin]]", "Cargo package manifest defines an installable command-line binary"
	}
	if kind == discovery.KindSource {
		if strings.HasPrefix(path, "cmd/") && strings.HasSuffix(path, "/main.go") && strings.Contains(content, "package main") {
			return false, true, "package main", "Go command entry point implements a local command-line interface"
		}
		if strings.HasSuffix(path, "/__main__.py") || path == "__main__.py" {
			return false, true, "", "Python module entry point implements a local command-line interface"
		}
		if path == "src/main.rs" && strings.Contains(content, "fn main") {
			return false, true, "fn main", "Rust command entry point implements a local command-line interface"
		}
	}
	return false, false, "", ""
}

func packageBinConfigured(value json.RawMessage) bool {
	if len(value) == 0 {
		return false
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return false
	}
	switch typed := decoded.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		return len(typed) > 0
	default:
		return false
	}
}

func lineContaining(content, pattern string) int {
	if pattern == "" {
		return 1
	}
	for index, line := range strings.Split(content, "\n") {
		if strings.Contains(line, pattern) {
			return index + 1
		}
	}
	return 1
}

func appendUniqueCitations(existing []providers.RepositoryCitation, additions ...providers.RepositoryCitation) []providers.RepositoryCitation {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	result := make([]providers.RepositoryCitation, 0, len(existing)+len(additions))
	for _, group := range [][]providers.RepositoryCitation{existing, additions} {
		for _, value := range group {
			key := value.Path + "\x00" + strconv.Itoa(value.Line) + "\x00" + value.Summary
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, value)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Summary < result[j].Summary
	})
	return result
}

func conservativeConfidence(first, second string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	rank := map[string]int{"low": 0, "medium": 1, "high": 2}
	if rank[first] <= rank[second] {
		return first
	}
	return second
}
