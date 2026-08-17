package technicalreview

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/providers"
)

const (
	cacheSchemaVersion = 3
	cacheFileName      = "technical-review-v3.json"
	maxCacheBytes      = 8 << 20
	maxCacheEntries    = 200
)

type Identity struct {
	Provider      providers.Kind `json:"provider"`
	Model         string         `json:"model"`
	ModelDigest   string         `json:"model_digest,omitempty"`
	PromptVersion string         `json:"prompt_version"`
	PackID        string         `json:"pack_id"`
	PackVersion   string         `json:"pack_version"`
	PackDigest    string         `json:"pack_digest"`
}

type cacheEntry struct {
	Identity        Identity                       `json:"identity"`
	SystemID        string                         `json:"system_id,omitempty"`
	ObjectiveID     string                         `json:"objective_id"`
	Fingerprint     string                         `json:"evidence_fingerprint"`
	CandidateDigest string                         `json:"candidate_digest"`
	Observation     providers.TechnicalObservation `json:"observation"`
}

type cacheFile struct {
	SchemaVersion int                   `json:"schema_version"`
	Entries       map[string]cacheEntry `json:"entries"`
}

// Cache stores bounded model observations in the current user's OS cache.
// Submitted candidate context is represented only by a SHA-256 digest.
type Cache struct {
	path    string
	entries map[string]cacheEntry
}

func DefaultPath() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(directory, "complyscan", cacheFileName), nil
}

func Open(path string) (*Cache, error) {
	cache := &Cache{path: path, entries: make(map[string]cacheEntry)}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect technical review cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("technical review cache must be a regular file and must not be a symlink")
	}
	if info.Size() > maxCacheBytes {
		return nil, fmt.Errorf("technical review cache exceeds %d bytes", maxCacheBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open technical review cache: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxCacheBytes+1))
	decoder.DisallowUnknownFields()
	var stored cacheFile
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("parse technical review cache: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("parse technical review cache: expected one JSON value")
	}
	if stored.SchemaVersion != cacheSchemaVersion {
		return nil, fmt.Errorf("unsupported technical review cache schema %d", stored.SchemaVersion)
	}
	if len(stored.Entries) > maxCacheEntries {
		return nil, fmt.Errorf("technical review cache exceeds %d entries", maxCacheEntries)
	}
	for key, entry := range stored.Entries {
		if key != entryKey(entry.Identity, entry.SystemID, entry.ObjectiveID, entry.Fingerprint, entry.CandidateDigest) {
			return nil, fmt.Errorf("technical review cache entry %q has an invalid key", key)
		}
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("technical review cache entry %q: %w", key, err)
		}
	}
	cache.entries = stored.Entries
	if cache.entries == nil {
		cache.entries = make(map[string]cacheEntry)
	}
	return cache, nil
}

func (cache *Cache) Lookup(identity Identity, candidate providers.TechnicalCandidate) (providers.TechnicalObservation, bool, error) {
	digest, err := providers.TechnicalCandidateDigest(candidate)
	if err != nil {
		return providers.TechnicalObservation{}, false, err
	}
	key := entryKey(identity, candidate.SystemID, candidate.ObjectiveID, candidate.EvidenceFingerprint, digest)
	entry, found := cache.entries[key]
	if !found {
		return providers.TechnicalObservation{}, false, nil
	}
	if err := validateEntry(entry); err != nil {
		return providers.TechnicalObservation{}, false, err
	}
	return entry.Observation, true, nil
}

func (cache *Cache) Store(identity Identity, candidate providers.TechnicalCandidate, observation providers.TechnicalObservation) error {
	digest, err := providers.TechnicalCandidateDigest(candidate)
	if err != nil {
		return err
	}
	entry := cacheEntry{
		Identity: identity, SystemID: candidate.SystemID, ObjectiveID: candidate.ObjectiveID, Fingerprint: candidate.EvidenceFingerprint,
		CandidateDigest: digest, Observation: observation,
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	key := entryKey(identity, entry.SystemID, entry.ObjectiveID, entry.Fingerprint, digest)
	cache.entries[key] = entry
	cache.prune()
	return cache.write()
}

func entryKey(identity Identity, systemID, objectiveID, fingerprint, candidateDigest string) string {
	value := strings.Join([]string{
		string(identity.Provider), identity.Model, identity.ModelDigest, identity.PromptVersion,
		identity.PackID, identity.PackVersion, identity.PackDigest,
		systemID, objectiveID, fingerprint, candidateDigest,
	}, "\x00")
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func validateEntry(entry cacheEntry) error {
	if entry.Identity.Provider == "" || strings.TrimSpace(entry.Identity.Model) == "" || strings.TrimSpace(entry.Identity.PromptVersion) == "" ||
		strings.TrimSpace(entry.Identity.PackID) == "" || strings.TrimSpace(entry.Identity.PackVersion) == "" || strings.TrimSpace(entry.Identity.PackDigest) == "" {
		return errors.New("cache identity is incomplete")
	}
	if strings.ContainsAny(entry.Identity.ModelDigest, "\r\n\x00") || len(entry.Identity.ModelDigest) > 300 {
		return errors.New("cache model digest is invalid")
	}
	if entry.ObjectiveID == "" || len(entry.Fingerprint) != 64 || len(entry.CandidateDigest) != 64 {
		return errors.New("candidate identity is incomplete")
	}
	observation := entry.Observation
	if observation.SystemID != entry.SystemID || observation.ObjectiveID != entry.ObjectiveID || observation.EvidenceFingerprint != entry.Fingerprint {
		return errors.New("observation binding does not match its candidate")
	}
	if !validStrength(observation.Strength) || observation.ModelStrength != "" && !validStrength(observation.ModelStrength) {
		return errors.New("observation has an invalid strength")
	}
	if !validConclusion(observation.Conclusion) || !validAssurance(observation.Assurance) {
		return errors.New("observation has an invalid conclusion or assurance level")
	}
	if observation.Confidence != "low" && observation.Confidence != "medium" && observation.Confidence != "high" {
		return errors.New("observation has an invalid confidence")
	}
	if strings.TrimSpace(observation.Rationale) == "" || len([]rune(observation.Rationale)) > 4_000 || len(observation.UnresolvedQuestions) > 10 || len(observation.MissingEvidence) > 10 {
		return errors.New("observation rationale or questions exceed cache bounds")
	}
	if len(observation.FollowUpQueries) > 3 || observation.FollowUpExcerpts < 0 || observation.FollowUpExcerpts > 3 || !observation.FollowUpRequested && (len(observation.FollowUpQueries) > 0 || observation.FollowUpExcerpts > 0) {
		return errors.New("observation follow-up metadata exceeds cache bounds")
	}
	totalText := len([]rune(observation.Rationale)) + len([]rune(observation.SuggestedReview)) + len([]rune(observation.GuardrailNote))
	for _, query := range observation.FollowUpQueries {
		if len([]rune(query)) > 500 {
			return errors.New("observation follow-up query exceeds cache bounds")
		}
		totalText += len([]rune(query))
	}
	for _, question := range observation.UnresolvedQuestions {
		questionLength := len([]rune(question))
		if questionLength > 1_000 {
			return errors.New("observation question exceeds cache bounds")
		}
		totalText += questionLength
	}
	for _, missing := range observation.MissingEvidence {
		if len([]rune(missing)) > 1_000 {
			return errors.New("observation missing-evidence item exceeds cache bounds")
		}
		totalText += len([]rune(missing))
	}
	if len(observation.SupportingEvidence) > 10 || len(observation.ContradictoryEvidence) > 10 {
		return errors.New("observation evidence claims exceed cache bounds")
	}
	for _, claim := range append(append([]providers.TechnicalEvidenceClaim(nil), observation.SupportingEvidence...), observation.ContradictoryEvidence...) {
		if strings.TrimSpace(claim.Path) == "" || strings.TrimSpace(claim.Summary) == "" || claim.Line < 0 || len([]rune(claim.Summary)) > 4_000 {
			return errors.New("observation contains an invalid evidence claim")
		}
		totalText += len([]rune(claim.Path)) + len([]rune(claim.Summary))
	}
	if len([]rune(observation.SuggestedReview)) > 2_000 || len([]rune(observation.GuardrailNote)) > 4_000 {
		return errors.New("observation action or guardrail note exceeds cache bounds")
	}
	if totalText > 16_000 {
		return errors.New("observation text exceeds aggregate cache bounds")
	}
	return nil
}

func validConclusion(value providers.TechnicalConclusion) bool {
	switch value {
	case providers.ConclusionSubstantiated, providers.ConclusionPartial, providers.ConclusionTestOnly,
		providers.ConclusionUnreachable, providers.ConclusionNotSubstantiated,
		providers.ConclusionNotFoundAfterInvestigation, providers.ConclusionCannotDetermine:
		return true
	default:
		return false
	}
}

func validAssurance(value providers.AssuranceLevel) bool {
	switch value {
	case providers.AssuranceSignalDetected, providers.AssuranceAISubstantiated,
		providers.AssuranceStructurallyVerified, providers.AssuranceTestEvidenceObserved,
		providers.AssuranceInvestigationNoEvidence, providers.AssuranceUnableToDetermine:
		return true
	default:
		return false
	}
}

func validStrength(strength providers.EvidenceStrength) bool {
	switch strength {
	case providers.StrengthStrong, providers.StrengthPartial, providers.StrengthWeak, providers.StrengthUncertain, providers.StrengthNotSupported:
		return true
	default:
		return false
	}
}

func (cache *Cache) prune() {
	if len(cache.entries) <= maxCacheEntries {
		return
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys[:len(keys)-maxCacheEntries] {
		delete(cache.entries, key)
	}
}

func (cache *Cache) write() error {
	directory := filepath.Dir(cache.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create technical review cache directory: %w", err)
	}
	if info, err := os.Lstat(cache.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("technical review cache must be a regular file and must not be a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect technical review cache: %w", err)
	}
	data, err := json.MarshalIndent(cacheFile{SchemaVersion: cacheSchemaVersion, Entries: cache.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode technical review cache: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxCacheBytes {
		return fmt.Errorf("encoded technical review cache exceeds %d bytes", maxCacheBytes)
	}
	temporary, err := os.CreateTemp(directory, ".complyscan-technical-review-*")
	if err != nil {
		return fmt.Errorf("create temporary technical review cache: %w", err)
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
		return fmt.Errorf("set technical review cache permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write technical review cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync technical review cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close technical review cache: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, cache.path); err != nil {
		return fmt.Errorf("replace technical review cache: %w", err)
	}
	return nil
}
