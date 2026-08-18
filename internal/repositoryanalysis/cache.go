package repositoryanalysis

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
	"github.com/ComplyScan/ComplyScan/internal/framework"
	"github.com/ComplyScan/ComplyScan/internal/ownership"
	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const (
	repositoryCacheSchemaVersion  = 6
	repositoryCacheContextVersion = "12"
	repositoryCacheFileName       = "repository-analysis-v6.json"
	maxRepositoryCacheBytes       = 32 << 20
	maxRepositoryCacheEntries     = 40
	maxRepositoryCacheEntryBytes  = 2 << 20
)

// CacheIdentity binds a repository result to the exact model contract and
// endpoint. EndpointDigest is a SHA-256 digest so private endpoint names are
// not written to the cache.
type CacheIdentity struct {
	Provider       providers.Kind `json:"provider"`
	Model          string         `json:"model"`
	ModelDigest    string         `json:"model_digest,omitempty"`
	PromptVersion  string         `json:"prompt_version"`
	EndpointDigest string         `json:"endpoint_digest"`
}

type repositoryCacheEntry struct {
	Identity    CacheIdentity                      `json:"identity"`
	InputDigest string                             `json:"input_digest"`
	CreatedAt   time.Time                          `json:"created_at"`
	Result      providers.RepositoryAnalysisResult `json:"result"`
}

type repositoryCacheFile struct {
	SchemaVersion int                             `json:"schema_version"`
	Entries       map[string]repositoryCacheEntry `json:"entries"`
}

// Cache stores completed advisory repository results in the current user's
// private OS cache. It stores only the result and SHA-256 input identity, never
// submitted source excerpts.
type Cache struct {
	path    string
	entries map[string]repositoryCacheEntry
}

// DefaultCachePath returns the private per-user repository-analysis cache.
func DefaultCachePath() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(directory, "complyscan", repositoryCacheFileName), nil
}

// OpenCache loads a bounded cache file without following symlinks.
func OpenCache(path string) (*Cache, error) {
	cache := &Cache{path: path, entries: make(map[string]repositoryCacheEntry)}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect repository analysis cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("repository analysis cache must be a regular file and must not be a symlink")
	}
	if info.Size() > maxRepositoryCacheBytes {
		return nil, fmt.Errorf("repository analysis cache exceeds %d bytes", maxRepositoryCacheBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open repository analysis cache: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxRepositoryCacheBytes+1))
	decoder.DisallowUnknownFields()
	var stored repositoryCacheFile
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("parse repository analysis cache: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("parse repository analysis cache: expected one JSON value")
	}
	if stored.SchemaVersion != repositoryCacheSchemaVersion {
		return nil, fmt.Errorf("unsupported repository analysis cache schema %d", stored.SchemaVersion)
	}
	if len(stored.Entries) > maxRepositoryCacheEntries {
		return nil, fmt.Errorf("repository analysis cache exceeds %d entries", maxRepositoryCacheEntries)
	}
	for key, entry := range stored.Entries {
		if key != repositoryCacheEntryKey(entry.Identity, entry.InputDigest) {
			return nil, fmt.Errorf("repository analysis cache entry %q has an invalid key", key)
		}
		if err := validateRepositoryCacheEntry(entry); err != nil {
			return nil, fmt.Errorf("repository analysis cache entry %q: %w", key, err)
		}
	}
	if stored.Entries != nil {
		cache.entries = stored.Entries
	}
	return cache, nil
}

// RepositoryInputDigest hashes every input that can affect context selection
// or model interpretation. Source bytes are hashed in memory and are not
// retained in the returned identity or cache file.
func RepositoryInputDigest(repository discovery.Repository, evidence []framework.TechnicalEvidenceReport, systems []profile.System, mode Mode, maxInputTokens int, ownershipRules []ownership.Rule, confirmedAIUses []providers.RepositoryConfirmedAIUse) (string, error) {
	hash := sha256.New()
	files := append([]discovery.File(nil), repository.Files...)
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path == files[j].Path {
			return files[i].Kind < files[j].Kind
		}
		return files[i].Path < files[j].Path
	})
	for _, file := range files {
		writeRepositoryCacheField(hash, []byte(filepath.ToSlash(file.Path)))
		writeRepositoryCacheField(hash, []byte(file.Kind))
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(file.Size))
		writeRepositoryCacheField(hash, size[:])
		writeRepositoryCacheField(hash, file.Content)
	}
	metadata := struct {
		ContextVersion  string                               `json:"context_version"`
		Evidence        []framework.TechnicalEvidenceReport  `json:"evidence"`
		Systems         []profile.System                     `json:"systems"`
		Mode            Mode                                 `json:"mode"`
		MaxInputTokens  int                                  `json:"max_input_tokens"`
		Ownership       []ownership.Rule                     `json:"ownership"`
		ConfirmedAIUses []providers.RepositoryConfirmedAIUse `json:"confirmed_ai_uses"`
	}{repositoryCacheContextVersion, evidence, systems, mode, maxInputTokens, ownershipRules, confirmedAIUses}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("encode repository analysis cache identity: %w", err)
	}
	writeRepositoryCacheField(hash, encoded)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// DigestEndpoint returns a non-reversible identity for a configured review
// endpoint. Credentials must never be included in value.
func DigestEndpoint(value string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(digest[:])
}

// Lookup returns a completed result only when every model and input identity
// field matches. Usage is cleared because a cache hit makes no provider call.
func (cache *Cache) Lookup(identity CacheIdentity, inputDigest string) (providers.RepositoryAnalysisResult, bool, error) {
	if err := validateRepositoryCacheIdentity(identity, inputDigest); err != nil {
		return providers.RepositoryAnalysisResult{}, false, err
	}
	entry, found := cache.entries[repositoryCacheEntryKey(identity, inputDigest)]
	if !found {
		return providers.RepositoryAnalysisResult{}, false, nil
	}
	if err := validateRepositoryCacheEntry(entry); err != nil {
		return providers.RepositoryAnalysisResult{}, false, err
	}
	result := entry.Result
	result.CacheHit = true
	result.Usage = providers.Usage{}
	result.Notes = append(result.Notes, "Reused a matching private repository-analysis cache entry; no model request was made for this layer.")
	return result, true, nil
}

// Store atomically saves one successfully validated repository result.
func (cache *Cache) Store(identity CacheIdentity, inputDigest string, result providers.RepositoryAnalysisResult) error {
	if err := validateRepositoryCacheIdentity(identity, inputDigest); err != nil {
		return err
	}
	result.CacheHit = false
	entry := repositoryCacheEntry{Identity: identity, InputDigest: inputDigest, CreatedAt: time.Now().UTC(), Result: result}
	if err := validateRepositoryCacheEntry(entry); err != nil {
		return err
	}
	cache.entries[repositoryCacheEntryKey(identity, inputDigest)] = entry
	cache.prune()
	return cache.write()
}

func writeRepositoryCacheField(writer io.Writer, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write(value)
}

func repositoryCacheEntryKey(identity CacheIdentity, inputDigest string) string {
	value := strings.Join([]string{string(identity.Provider), identity.Model, identity.ModelDigest, identity.PromptVersion, identity.EndpointDigest, inputDigest}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validateRepositoryCacheIdentity(identity CacheIdentity, inputDigest string) error {
	if identity.Provider == "" || identity.Provider == providers.None || strings.TrimSpace(identity.Model) == "" || strings.TrimSpace(identity.PromptVersion) == "" {
		return errors.New("repository analysis cache identity is incomplete")
	}
	if strings.ContainsAny(identity.ModelDigest, "\r\n\x00") || len(identity.ModelDigest) > 300 {
		return errors.New("repository analysis cache model digest is invalid")
	}
	if !validSHA256(identity.EndpointDigest) || !validSHA256(inputDigest) {
		return errors.New("repository analysis cache digests must be SHA-256 values")
	}
	return nil
}

func validateRepositoryCacheEntry(entry repositoryCacheEntry) error {
	if err := validateRepositoryCacheIdentity(entry.Identity, entry.InputDigest); err != nil {
		return err
	}
	if entry.CreatedAt.IsZero() {
		return errors.New("repository analysis cache entry has no creation time")
	}
	result := entry.Result
	if result.Provider != entry.Identity.Provider || result.Model != entry.Identity.Model {
		return errors.New("repository analysis result does not match its model identity")
	}
	if !validRepositoryAnalysisMode(result.Coverage.Mode) || result.Coverage.ReviewScope != "" && result.Coverage.ReviewScope != providers.RepositoryReviewScopeChanged {
		return errors.New("repository analysis result contains an invalid review mode or scope")
	}
	if result.Coverage.RepositoryFiles < 0 || result.Coverage.RepositoryBytes < 0 || result.Coverage.FilesSubmitted < 0 || result.Coverage.BytesSubmitted < 0 || result.Coverage.ProviderRequests < 0 || result.Coverage.Subsystems < 0 || result.Coverage.SourceBatchesStarted < 0 || result.Coverage.SourceBatchesCompleted < 0 || result.Coverage.SourceBatchesTotal < 0 || result.Coverage.SourceBatchesCompleted != result.Coverage.SourceBatchesTotal || result.Coverage.SourceBatchesTotal > 0 && (result.Coverage.Subsystems != result.Coverage.SourceBatchesTotal || result.Coverage.SourceBatchesStarted < result.Coverage.SourceBatchesTotal || result.Coverage.ProviderRequests < result.Coverage.SourceBatchesStarted) || result.Coverage.CitationsChecked < 0 {
		return errors.New("repository analysis result contains invalid coverage counters")
	}
	if len(result.FollowUpQueries) > 3 || result.FollowUpExcerpts < 0 || result.FollowUpExcerpts > 3 || !result.FollowUpRequested && (len(result.FollowUpQueries) > 0 || result.FollowUpExcerpts > 0) {
		return errors.New("repository analysis result contains invalid follow-up metadata")
	}
	if result.Usage.PromptTokens < 0 || result.Usage.CompletionTokens < 0 || result.Usage.ReasoningTokens < 0 || result.Usage.TotalDurationNS < 0 {
		return errors.New("repository analysis result contains negative usage")
	}
	if len(result.Result.AIUses) > 100 || len(result.Result.AIUseFacts) > 600 || len(result.Result.ObjectiveObservations) > 500 || len(result.Result.UnmappedObservations) > 100 || len(result.Result.UnresolvedQuestions) > 100 || len(result.Result.ResolvedEvidenceGaps) > 100 || len(result.Result.EvidenceGaps) != 0 || len(result.Notes) > 100 {
		return errors.New("repository analysis result exceeds cache item limits")
	}
	candidateEvidencePaths := make(map[string]map[string]struct{}, len(result.Result.AIUses))
	seenObservationIDs := make(map[string]struct{})
	for _, use := range result.Result.AIUses {
		if strings.TrimSpace(use.ID) == "" || strings.TrimSpace(use.Name) == "" || !validRepositoryConfidence(use.Confidence) {
			return errors.New("repository analysis cache contains an invalid AI use")
		}
		if _, duplicate := candidateEvidencePaths[use.ID]; duplicate {
			return errors.New("repository analysis cache contains a duplicate AI use")
		}
		if len(use.Evidence) == 0 {
			return errors.New("repository analysis cache contains an uncited AI use")
		}
		if len(use.MemberObservationIDs) == 0 || len(use.MemberObservationIDs) > 100 {
			return errors.New("repository analysis cache contains an AI use without bounded observation membership")
		}
		members := append([]string(nil), use.MemberObservationIDs...)
		sort.Strings(members)
		for index, observationID := range members {
			if strings.TrimSpace(observationID) == "" || strings.TrimSpace(observationID) != observationID {
				return errors.New("repository analysis cache contains a non-canonical observation ID")
			}
			if index > 0 && members[index-1] == observationID {
				return errors.New("repository analysis cache contains duplicate observation membership")
			}
			if _, duplicate := seenObservationIDs[observationID]; duplicate {
				return errors.New("repository analysis cache assigns one observation to multiple AI uses")
			}
			seenObservationIDs[observationID] = struct{}{}
		}
		if use.ID != inferredCandidateID(members) {
			return errors.New("repository analysis cache contains an AI-use ID not derived from its observation membership")
		}
		if err := validateRepositoryCitations(use.Evidence); err != nil {
			return err
		}
		paths := make(map[string]struct{}, len(use.Evidence))
		for _, citation := range use.Evidence {
			paths[filepath.ToSlash(strings.TrimSpace(citation.Path))] = struct{}{}
		}
		candidateEvidencePaths[use.ID] = paths
	}
	seenGapIDs := make(map[string]struct{}, len(result.Result.ResolvedEvidenceGaps))
	for _, resolution := range result.Result.ResolvedEvidenceGaps {
		if strings.TrimSpace(resolution.GapID) == "" || strings.TrimSpace(resolution.GapID) != resolution.GapID || strings.TrimSpace(resolution.Kind) == "" || strings.TrimSpace(resolution.OriginalText) == "" || strings.TrimSpace(resolution.Reason) == "" || len(resolution.ResolvingObservationIDs) == 0 || len(resolution.Evidence) == 0 {
			return errors.New("repository analysis cache contains an invalid resolved evidence gap")
		}
		if _, duplicate := seenGapIDs[resolution.GapID]; duplicate {
			return errors.New("repository analysis cache contains a duplicate resolved evidence gap")
		}
		seenGapIDs[resolution.GapID] = struct{}{}
		seenResolvers := make(map[string]struct{}, len(resolution.ResolvingObservationIDs))
		for _, observationID := range resolution.ResolvingObservationIDs {
			if _, exists := seenObservationIDs[observationID]; !exists {
				return errors.New("repository analysis cache contains a resolved gap with an unknown observation")
			}
			if _, duplicate := seenResolvers[observationID]; duplicate {
				return errors.New("repository analysis cache contains a resolved gap with a duplicate observation")
			}
			seenResolvers[observationID] = struct{}{}
		}
		if err := validateRepositoryCitations(resolution.Evidence); err != nil {
			return err
		}
	}
	seenFactSets := make(map[string]struct{}, len(result.Result.AIUseFacts))
	for _, factSet := range result.Result.AIUseFacts {
		if strings.TrimSpace(factSet.AIUseID) == "" || strings.TrimSpace(factSet.AIUseID) != factSet.AIUseID || len(factSet.Facts) > len(profile.CodeFactFields()) || len(factSet.UnresolvedQuestions) > 100 {
			return errors.New("repository analysis cache contains an invalid AI-use fact set")
		}
		for _, question := range factSet.UnresolvedQuestions {
			if strings.TrimSpace(question) == "" || len([]rune(question)) > 2000 {
				return errors.New("repository analysis cache contains an invalid AI-use fact question")
			}
		}
		if _, duplicate := seenFactSets[factSet.AIUseID]; duplicate {
			return errors.New("repository analysis cache contains a duplicate AI-use fact set")
		}
		seenFactSets[factSet.AIUseID] = struct{}{}
		seenFields := make(map[profile.CodeFactField]struct{}, len(factSet.Facts))
		for _, fact := range factSet.Facts {
			field, supported := profile.ParseCodeFactField(string(fact.Field))
			if !supported || len(fact.Values) == 0 || len(fact.Values) > 8 || !validRepositoryConfidence(fact.Confidence) || strings.TrimSpace(fact.Rationale) == "" || len(fact.Evidence) == 0 {
				return errors.New("repository analysis cache contains an invalid AI-use fact")
			}
			if _, duplicate := seenFields[field]; duplicate {
				return errors.New("repository analysis cache contains a duplicate AI-use fact field")
			}
			seenFields[field] = struct{}{}
			seenValues := make(map[string]struct{}, len(fact.Values))
			for _, value := range fact.Values {
				if strings.TrimSpace(value) != value || !profile.CodeFactAllowsValue(field, value) || len([]rune(value)) > profile.CodeFactValueLimit(field) {
					return errors.New("repository analysis cache contains an invalid AI-use fact value")
				}
				if _, duplicate := seenValues[value]; duplicate {
					return errors.New("repository analysis cache contains a duplicate AI-use fact value")
				}
				seenValues[value] = struct{}{}
			}
			if cacheFactReliesOnAbsence(fact) {
				return errors.New("repository analysis cache contains an absence-based AI-use fact")
			}
			if err := validateRepositoryCitations(fact.Evidence); err != nil {
				return err
			}
			if candidatePaths, candidate := candidateEvidencePaths[factSet.AIUseID]; candidate {
				for _, citation := range fact.Evidence {
					if _, allowed := candidatePaths[filepath.ToSlash(strings.TrimSpace(citation.Path))]; !allowed {
						return errors.New("repository analysis cache contains a candidate fact citation outside that AI use's evidence paths")
					}
				}
			}
		}
	}
	for useID := range candidateEvidencePaths {
		if _, reviewed := seenFactSets[useID]; !reviewed {
			return errors.New("repository analysis cache omits a required candidate AI-use fact set")
		}
	}
	for _, observation := range result.Result.ObjectiveObservations {
		if strings.TrimSpace(observation.ObjectiveID) == "" || !validRepositoryConfidence(observation.Confidence) || !validRepositoryStrength(observation.Strength) {
			return errors.New("repository analysis cache contains an invalid objective observation")
		}
		if err := validateRepositoryCitations(append(append([]providers.RepositoryCitation(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...)); err != nil {
			return err
		}
	}
	for _, observation := range result.Result.UnmappedObservations {
		if strings.TrimSpace(observation.Summary) == "" || !validRepositoryConfidence(observation.Confidence) {
			return errors.New("repository analysis cache contains an invalid unmapped observation")
		}
		if err := validateRepositoryCitations(observation.Evidence); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode repository analysis cache entry: %w", err)
	}
	if len(encoded) > maxRepositoryCacheEntryBytes {
		return fmt.Errorf("repository analysis cache entry exceeds %d bytes", maxRepositoryCacheEntryBytes)
	}
	return nil
}

func validateRepositoryCitations(citations []providers.RepositoryCitation) error {
	for _, citation := range citations {
		path := filepath.ToSlash(strings.TrimSpace(citation.Path))
		if path == "" || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || citation.Line < 1 || strings.TrimSpace(citation.Summary) == "" {
			return errors.New("repository analysis cache contains an invalid citation")
		}
	}
	return nil
}

func cacheFactReliesOnAbsence(value providers.RepositoryAIUseFact) bool {
	parts := append([]string{value.Rationale}, value.Values...)
	for _, citation := range value.Evidence {
		parts = append(parts, citation.Summary)
	}
	text := strings.ToLower(strings.Join(parts, " "))
	for _, phrase := range []string{
		"no evidence", "without evidence", "lack of evidence", "lacks evidence", "absence of",
		"not found", "not detected", "no indication", "not established", "does not show", "does not contain",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func validRepositoryConfidence(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func validRepositoryStrength(value providers.EvidenceStrength) bool {
	switch value {
	case providers.StrengthStrong, providers.StrengthPartial, providers.StrengthWeak, providers.StrengthUncertain, providers.StrengthNotSupported:
		return true
	default:
		return false
	}
}

func validRepositoryAnalysisMode(value providers.RepositoryAnalysisMode) bool {
	switch value {
	case providers.RepositoryAnalysisTargeted, providers.RepositoryAnalysisFull, providers.RepositoryAnalysisSubsystem, providers.RepositoryAnalysisSynthesis:
		return true
	default:
		return false
	}
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func (cache *Cache) prune() {
	if len(cache.entries) <= maxRepositoryCacheEntries {
		return
	}
	type cacheAge struct {
		key       string
		createdAt time.Time
	}
	ages := make([]cacheAge, 0, len(cache.entries))
	for key, entry := range cache.entries {
		ages = append(ages, cacheAge{key: key, createdAt: entry.CreatedAt})
	}
	sort.Slice(ages, func(i, j int) bool {
		if ages[i].createdAt.Equal(ages[j].createdAt) {
			return ages[i].key < ages[j].key
		}
		return ages[i].createdAt.Before(ages[j].createdAt)
	})
	for _, entry := range ages[:len(ages)-maxRepositoryCacheEntries] {
		delete(cache.entries, entry.key)
	}
}

func (cache *Cache) write() error {
	directory := filepath.Dir(cache.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create repository analysis cache directory: %w", err)
	}
	if info, err := os.Lstat(cache.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("repository analysis cache must be a regular file and must not be a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect repository analysis cache: %w", err)
	}
	data, err := json.MarshalIndent(repositoryCacheFile{SchemaVersion: repositoryCacheSchemaVersion, Entries: cache.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode repository analysis cache: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxRepositoryCacheBytes {
		return fmt.Errorf("encoded repository analysis cache exceeds %d bytes", maxRepositoryCacheBytes)
	}
	temporary, err := os.CreateTemp(directory, ".complyscan-repository-analysis-*")
	if err != nil {
		return fmt.Errorf("create temporary repository analysis cache: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set repository analysis cache permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write repository analysis cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync repository analysis cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close repository analysis cache: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, cache.path); err != nil {
		return fmt.Errorf("replace repository analysis cache: %w", err)
	}
	return nil
}
