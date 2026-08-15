// Package aiuse owns the developer-confirmed AI-use register. Model output may
// propose entries, but only an explicit user action writes this manifest.
package aiuse

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/ComplyScan/ComplyScan/internal/profile"
	"github.com/ComplyScan/ComplyScan/internal/providers"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultPath is intentionally outside the ignored report directory so a
	// team may review and commit its AI-use register.
	DefaultPath = ".complyscan/ai-uses.yml"
	Version     = 1

	maxManifestBytes = 4 << 20
	maxUses          = 100
	maxDismissals    = 500
)

type RecordStatus string

const (
	StatusActive  RecordStatus = "active"
	StatusRetired RecordStatus = "retired"
)

// Manifest is the durable, human-owned register. It contains repository
// groupings only; it does not contain legal applicability or compliance
// conclusions.
type Manifest struct {
	Version    int         `yaml:"version" json:"version"`
	Uses       []Use       `yaml:"uses,omitempty" json:"uses"`
	Dismissals []Dismissal `yaml:"dismissed-suggestions,omitempty" json:"dismissed_suggestions,omitempty"`
}

// Use is one developer-owned grouping of repository paths that implement a
// product AI use. Its ID remains stable after creation.
type Use struct {
	ID                     string                `yaml:"id" json:"id"`
	Name                   string                `yaml:"name" json:"name"`
	Description            string                `yaml:"description" json:"description"`
	SystemIDs              []string              `yaml:"system-ids,omitempty" json:"system_ids,omitempty"`
	Paths                  []string              `yaml:"paths" json:"paths"`
	SuggestionFingerprints []string              `yaml:"suggestion-fingerprints,omitempty" json:"suggestion_fingerprints,omitempty"`
	Status                 RecordStatus          `yaml:"status" json:"status"`
	Review                 profile.ProfileReview `yaml:"review" json:"review"`
}

// Dismissal prevents an unchanged model suggestion from being repeatedly
// presented. The fingerprint deliberately excludes the model-authored ID.
type Dismissal struct {
	Fingerprint string `yaml:"fingerprint" json:"fingerprint"`
	Reason      string `yaml:"reason" json:"reason"`
}

var manifestIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func NewManifest() Manifest {
	return Manifest{Version: Version, Uses: []Use{}, Dismissals: []Dismissal{}}
}

// LoadOptional strictly loads a manifest. A missing path is represented by a
// valid empty manifest and exists=false.
func LoadOptional(path string) (Manifest, bool, error) {
	if err := rejectSymlinkPath(path); err != nil {
		return Manifest{}, false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewManifest(), false, nil
		}
		return Manifest{}, false, fmt.Errorf("inspect AI-use manifest %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, false, fmt.Errorf("AI-use manifest %q must not be a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, false, fmt.Errorf("AI-use manifest %q is not a regular file", path)
	}
	if info.Size() > maxManifestBytes {
		return Manifest{}, false, fmt.Errorf("AI-use manifest %q exceeds %d bytes", path, maxManifestBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, false, fmt.Errorf("read AI-use manifest %q: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, false, fmt.Errorf("parse AI-use manifest %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return Manifest{}, false, fmt.Errorf("parse AI-use manifest %q: %w", path, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, false, fmt.Errorf("validate AI-use manifest %q: %w", path, err)
	}
	manifest.normalize()
	return manifest, true, nil
}

// Write atomically replaces a validated manifest and refuses symlink targets
// or symlinked parent directories.
func Write(path string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate AI-use manifest: %w", err)
	}
	manifest.normalize()
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create AI-use manifest directory %q: %w", directory, err)
	}
	if err := rejectSymlinkPath(path); err != nil {
		return err
	}

	var encoded bytes.Buffer
	encoded.WriteString("# Developer-confirmed AI-use groupings.\n")
	encoded.WriteString("# Confirmation records repository ownership only; it is not a compliance conclusion.\n")
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode AI-use manifest %q: %w", path, err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("finish AI-use manifest %q: %w", path, err)
	}

	temporary, err := os.CreateTemp(directory, ".complyscan-ai-uses-*")
	if err != nil {
		return fmt.Errorf("create AI-use manifest beside %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set AI-use manifest permissions: %w", err)
	}
	if _, err := temporary.Write(encoded.Bytes()); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write AI-use manifest %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync AI-use manifest %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close AI-use manifest %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace AI-use manifest %q: %w", path, err)
	}
	return nil
}

func (manifest Manifest) Validate() error {
	if manifest.Version != Version {
		return fmt.Errorf("unsupported version %d", manifest.Version)
	}
	if len(manifest.Uses) > maxUses {
		return fmt.Errorf("uses must not exceed %d entries", maxUses)
	}
	seenUses := make(map[string]struct{}, len(manifest.Uses))
	seenSuggestionFingerprints := make(map[string]string)
	for index, use := range manifest.Uses {
		if err := use.Validate(); err != nil {
			return fmt.Errorf("uses[%d]: %w", index, err)
		}
		if _, duplicate := seenUses[use.ID]; duplicate {
			return fmt.Errorf("uses[%d].id %q is duplicated", index, use.ID)
		}
		seenUses[use.ID] = struct{}{}
		for _, fingerprint := range use.SuggestionFingerprints {
			if existingID, duplicate := seenSuggestionFingerprints[fingerprint]; duplicate {
				return fmt.Errorf("uses[%d].suggestion-fingerprints value is already linked to use %q", index, existingID)
			}
			seenSuggestionFingerprints[fingerprint] = use.ID
		}
	}
	if len(manifest.Dismissals) > maxDismissals {
		return fmt.Errorf("dismissed-suggestions must not exceed %d entries", maxDismissals)
	}
	seenDismissals := make(map[string]struct{}, len(manifest.Dismissals))
	for index, dismissal := range manifest.Dismissals {
		if !validFingerprint(dismissal.Fingerprint) {
			return fmt.Errorf("dismissed-suggestions[%d].fingerprint must be a 64-character SHA-256 value", index)
		}
		if err := validateText("reason", dismissal.Reason, 1000); err != nil {
			return fmt.Errorf("dismissed-suggestions[%d]: %w", index, err)
		}
		if _, duplicate := seenDismissals[dismissal.Fingerprint]; duplicate {
			return fmt.Errorf("dismissed-suggestions[%d].fingerprint is duplicated", index)
		}
		if useID, linked := seenSuggestionFingerprints[dismissal.Fingerprint]; linked {
			return fmt.Errorf("dismissed-suggestions[%d].fingerprint is already linked to use %q", index, useID)
		}
		seenDismissals[dismissal.Fingerprint] = struct{}{}
	}
	return nil
}

func (use Use) Validate() error {
	if !manifestIDPattern.MatchString(use.ID) {
		return errors.New("id must use 1-64 lowercase letters, numbers, dots, underscores, or hyphens")
	}
	if err := validateText("name", use.Name, 200); err != nil {
		return err
	}
	if err := validateText("description", use.Description, 2000); err != nil {
		return err
	}
	if use.Status != StatusActive && use.Status != StatusRetired {
		return fmt.Errorf("status %q is not supported", use.Status)
	}
	if len(use.SystemIDs) > 20 {
		return errors.New("system-ids must not exceed 20 values")
	}
	seenSystems := make(map[string]struct{}, len(use.SystemIDs))
	for index, systemID := range use.SystemIDs {
		if !manifestIDPattern.MatchString(systemID) {
			return fmt.Errorf("system-ids[%d] %q is invalid", index, systemID)
		}
		if _, duplicate := seenSystems[systemID]; duplicate {
			return fmt.Errorf("system-ids contains duplicate value %q", systemID)
		}
		seenSystems[systemID] = struct{}{}
	}
	if len(use.Paths) == 0 || len(use.Paths) > 50 {
		return errors.New("paths must contain between 1 and 50 positive repository-relative patterns")
	}
	seenPaths := make(map[string]struct{}, len(use.Paths))
	for index, pattern := range use.Paths {
		if err := validatePathPattern(pattern); err != nil {
			return fmt.Errorf("paths[%d]: %w", index, err)
		}
		if _, duplicate := seenPaths[pattern]; duplicate {
			return fmt.Errorf("paths contains duplicate value %q", pattern)
		}
		seenPaths[pattern] = struct{}{}
	}
	if len(use.SuggestionFingerprints) > 100 {
		return errors.New("suggestion-fingerprints must not exceed 100 values")
	}
	seenFingerprints := make(map[string]struct{}, len(use.SuggestionFingerprints))
	for index, fingerprint := range use.SuggestionFingerprints {
		if !validFingerprint(fingerprint) {
			return fmt.Errorf("suggestion-fingerprints[%d] must be a 64-character SHA-256 value", index)
		}
		if _, duplicate := seenFingerprints[fingerprint]; duplicate {
			return fmt.Errorf("suggestion-fingerprints contains duplicate value %q", fingerprint)
		}
		seenFingerprints[fingerprint] = struct{}{}
	}
	if err := use.Review.Validate(); err != nil {
		return fmt.Errorf("review: %w", err)
	}
	if use.Review.ReviewedBy != "" {
		if err := validateText("review.reviewed-by", use.Review.ReviewedBy, 200); err != nil {
			return err
		}
	}
	return nil
}

// SuggestionFingerprint binds the reviewed grouping but intentionally ignores
// RepositoryAIUse.ID because that value is generated independently per model
// response and is not durable identity.
func SuggestionFingerprint(suggestion providers.RepositoryAIUse) string {
	parts := []string{
		normalizeFingerprintText(suggestion.Name),
		normalizeFingerprintText(suggestion.Purpose),
		normalizeFingerprintText(suggestion.Lifecycle),
	}
	for _, citation := range suggestion.Evidence {
		parts = append(parts, filepath.ToSlash(strings.TrimSpace(citation.Path))+fmt.Sprintf(":%d", citation.Line)+":"+normalizeFingerprintText(citation.Summary))
	}
	sort.Strings(parts[3:])
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

func IsDismissed(manifest Manifest, suggestion providers.RepositoryAIUse) bool {
	fingerprint := SuggestionFingerprint(suggestion)
	for _, dismissal := range manifest.Dismissals {
		if dismissal.Fingerprint == fingerprint {
			return true
		}
	}
	return false
}

// LinkedSuggestionUses returns the stable uses to which a developer explicitly
// linked this exact suggestion. Path overlap alone is deliberately insufficient:
// distinct product AI uses may share a gateway, model client, or orchestration
// file.
func LinkedSuggestionUses(manifest Manifest, suggestion providers.RepositoryAIUse) []string {
	fingerprint := SuggestionFingerprint(suggestion)
	result := make([]string, 0, 1)
	for _, use := range manifest.Uses {
		for _, linked := range use.SuggestionFingerprints {
			if linked == fingerprint {
				result = append(result, use.ID)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

// NextID returns a free stable ID derived only at the moment a user creates a
// durable use. Later wording changes never regenerate it.
func NextID(manifest Manifest, name string) string {
	base := profile.SlugID(name)
	if base == "" {
		base = "ai-use"
	}
	if len(base) > 56 {
		base = strings.Trim(base[:56], "-._")
	}
	used := make(map[string]struct{}, len(manifest.Uses))
	for _, use := range manifest.Uses {
		used[use.ID] = struct{}{}
	}
	if _, exists := used[base]; !exists {
		return base
	}
	for index := 2; ; index++ {
		suffix := fmt.Sprintf("-%d", index)
		candidateBase := base
		if len(candidateBase)+len(suffix) > 64 {
			candidateBase = strings.Trim(candidateBase[:64-len(suffix)], "-._")
		}
		candidate := candidateBase + suffix
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func SuggestionPaths(suggestion providers.RepositoryAIUse) []string {
	seen := make(map[string]struct{}, len(suggestion.Evidence))
	paths := make([]string, 0, len(suggestion.Evidence))
	for _, citation := range suggestion.Evidence {
		path := filepath.ToSlash(strings.TrimSpace(citation.Path))
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (manifest *Manifest) normalize() {
	if manifest.Uses == nil {
		manifest.Uses = []Use{}
	}
	if manifest.Dismissals == nil {
		manifest.Dismissals = []Dismissal{}
	}
	for index := range manifest.Uses {
		manifest.Uses[index].SystemIDs = sortedUnique(manifest.Uses[index].SystemIDs)
		manifest.Uses[index].Paths = sortedUnique(manifest.Uses[index].Paths)
		manifest.Uses[index].SuggestionFingerprints = sortedUnique(manifest.Uses[index].SuggestionFingerprints)
	}
	sort.SliceStable(manifest.Uses, func(i, j int) bool { return manifest.Uses[i].ID < manifest.Uses[j].ID })
	sort.SliceStable(manifest.Dismissals, func(i, j int) bool { return manifest.Dismissals[i].Fingerprint < manifest.Dismissals[j].Fingerprint })
}

func validateText(field, value string, maximum int) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", field)
	}
	if len([]rune(value)) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", field, maximum)
	}
	for _, character := range value {
		if unsafeManifestTextRune(character) {
			return fmt.Errorf("%s must not contain control characters or unsafe formatting characters", field)
		}
	}
	return nil
}

func validatePathPattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" || strings.TrimSpace(pattern) != pattern || len([]rune(pattern)) > 500 {
		return errors.New("pattern must be non-empty, trimmed, and at most 500 characters")
	}
	if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "!") {
		return errors.New("pattern must be positive and repository-relative")
	}
	if strings.Contains(pattern, "\\") {
		return errors.New("pattern must use forward slashes and contain no control characters")
	}
	for _, character := range pattern {
		if unsafeManifestTextRune(character) {
			return errors.New("pattern must use forward slashes and contain no control characters or unsafe formatting characters")
		}
	}
	for _, part := range strings.Split(strings.TrimSuffix(pattern, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return errors.New("pattern contains an unsafe path segment")
		}
	}
	return nil
}

func unsafeManifestTextRune(character rune) bool {
	return unicode.IsControl(character) || unicode.Is(unicode.Cf, character) || unicode.Is(unicode.Zl, character) || unicode.Is(unicode.Zp, character)
}

func validFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func normalizeFingerprintText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func sortedUnique(values []string) []string {
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

func rejectSymlinkPath(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve AI-use manifest %q: %w", path, err)
	}
	// The manifest and its immediate, ComplyScan-owned directory must not be
	// symlinks. Higher ancestors belong to the user or operating system (macOS,
	// for example, exposes /var through /private/var) and are allowed.
	for _, current := range []string{absolute, filepath.Dir(absolute)} {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("AI-use manifest path %q contains symbolic link %q", path, current)
		}
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect AI-use manifest path %q: %w", current, statErr)
		}
	}
	return nil
}
