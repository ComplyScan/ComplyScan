// Package reviewconsent records a machine-local approval for automatic AI
// review. Repository configuration can request review, but it cannot grant
// permission to contact a model or read a provider credential by itself.
package reviewconsent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
)

const (
	schemaVersion                = 1
	maxRecordBytes               = 64 << 10
	reviewContextContractVersion = "adaptive-provider-pipeline-v5"
)

// Store persists review approvals outside the scanned repository. Directory
// is exported so callers and tests can report or inject the exact location.
type Store struct {
	Directory string
	root      string
}

type record struct {
	Version     int       `json:"version"`
	Repository  string    `json:"repository"`
	Config      string    `json:"config"`
	AIDigest    string    `json:"ai_digest"`
	ConsentedAt time.Time `json:"consented_at"`
}

type normalizedAI struct {
	Provider        string `json:"provider"`
	Endpoint        string `json:"endpoint"`
	Model           string `json:"model"`
	ContextContract string `json:"context_contract"`

	APIKeyEnvironment string `json:"api_key_environment,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`

	RepositoryMode       string `json:"repository_mode"`
	RepositoryTokenLimit int    `json:"repository_token_limit"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
	MaxFindings          int    `json:"max_findings"`
}

// DefaultStore returns the per-user cache location used by the CLI. It does
// not create anything, so checking consent cannot mutate local state.
func DefaultStore() (Store, error) {
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return Store{}, fmt.Errorf("locate user cache directory: %w", err)
	}
	cacheDirectory, err = filepath.Abs(cacheDirectory)
	if err != nil {
		return Store{}, fmt.Errorf("resolve user cache directory: %w", err)
	}
	root := ""
	if info, inspectErr := os.Lstat(cacheDirectory); inspectErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Store{}, errors.New("user cache directory is not a real directory")
		}
		cacheDirectory, err = filepath.EvalSymlinks(cacheDirectory)
		if err != nil {
			return Store{}, fmt.Errorf("resolve user cache directory: %w", err)
		}
		root = cacheDirectory
	} else if errors.Is(inspectErr, os.ErrNotExist) {
		root, cacheDirectory, err = anchorBelowExistingParent(cacheDirectory)
		if err != nil {
			return Store{}, fmt.Errorf("resolve user cache directory: %w", err)
		}
	} else {
		return Store{}, fmt.Errorf("inspect user cache directory: %w", inspectErr)
	}
	return Store{
		Directory: filepath.Join(cacheDirectory, "complyscan", "review-consent"),
		root:      root,
	}, nil
}

// NewStore creates an injectable store rooted at directory. Its parent must
// already exist; only the final directory is managed by this store.
func NewStore(directory string) Store {
	abs, err := filepath.Abs(directory)
	if err != nil {
		return Store{Directory: filepath.Clean(directory), root: filepath.Dir(filepath.Clean(directory))}
	}
	parent := filepath.Dir(abs)
	if canonicalParent, resolveErr := filepath.EvalSymlinks(parent); resolveErr == nil {
		parent = canonicalParent
	}
	directory = filepath.Join(parent, filepath.Base(abs))
	return Store{Directory: directory, root: parent}
}

// Grant records approval for this exact repository, config file, and model
// destination/settings. Changing any fingerprinted setting invalidates it.
func (s Store) Grant(repositoryPath, configPath string, settings config.AIConfig) error {
	repository, configFile, err := canonicalIdentity(repositoryPath, configPath)
	if err != nil {
		return err
	}
	digest, err := Digest(settings)
	if err != nil {
		return err
	}
	directory, err := s.ensureDirectory()
	if err != nil {
		return err
	}
	recordPath := filepath.Join(directory, identityKey(repository, configFile)+".json")
	if err := rejectUnsafeExistingFile(recordPath); err != nil {
		return err
	}
	value := record{
		Version: schemaVersion, Repository: repository, Config: configFile,
		AIDigest: digest, ConsentedAt: time.Now().UTC(),
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode review consent: %w", err)
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(directory, ".consent-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary review consent: %w", err)
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary review consent: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write temporary review consent: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary review consent: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary review consent: %w", err)
	}
	if err := os.Rename(temporaryPath, recordPath); err != nil {
		return fmt.Errorf("install review consent: %w", err)
	}
	keep = true
	if err := os.Chmod(recordPath, 0o600); err != nil {
		return fmt.Errorf("protect review consent: %w", err)
	}
	return nil
}

// Authorized reports whether a valid local approval matches the exact
// repository, config file, and current AI settings.
func (s Store) Authorized(repositoryPath, configPath string, settings config.AIConfig) (bool, error) {
	repository, configFile, err := canonicalIdentity(repositoryPath, configPath)
	if err != nil {
		return false, err
	}
	digest, err := Digest(settings)
	if err != nil {
		return false, err
	}
	directory, found, err := s.existingDirectory()
	if err != nil || !found {
		return false, err
	}
	recordPath := filepath.Join(directory, identityKey(repository, configFile)+".json")
	value, found, err := readRecord(recordPath)
	if err != nil || !found {
		return false, err
	}
	if value.Version != schemaVersion || value.Repository != repository || value.Config != configFile || value.AIDigest != digest || value.ConsentedAt.IsZero() {
		return false, nil
	}
	return true, nil
}

// Revoke removes approval for this repository/config identity, regardless of
// the previously approved provider settings.
func (s Store) Revoke(repositoryPath, configPath string) error {
	repository, configFile, err := canonicalIdentity(repositoryPath, configPath)
	if err != nil {
		return err
	}
	directory, found, err := s.existingDirectory()
	if err != nil || !found {
		return err
	}
	recordPath := filepath.Join(directory, identityKey(repository, configFile)+".json")
	info, err := os.Lstat(recordPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect review consent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("review consent path is not a regular file")
	}
	if err := os.Remove(recordPath); err != nil {
		return fmt.Errorf("remove review consent: %w", err)
	}
	return nil
}

// Digest fingerprints every setting that can change the model destination,
// credential source, model behavior, or repository-context strategy.
func Digest(settings config.AIConfig) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(settings.Provider))
	mode := strings.ToLower(strings.TrimSpace(settings.RepositoryAnalysis.Mode))
	if mode == "" {
		mode = "auto"
	}
	normalized := normalizedAI{
		Provider: provider, RepositoryMode: mode, ContextContract: reviewContextContractVersion,
		RepositoryTokenLimit: settings.RepositoryAnalysis.MaxInputTokens,
	}
	if provider == "ollama" {
		normalized.Endpoint = strings.TrimRight(strings.TrimSpace(settings.Ollama.Endpoint), "/")
		normalized.Model = strings.TrimSpace(settings.Ollama.Model)
		normalized.TimeoutSeconds = settings.Ollama.TimeoutSeconds
		normalized.MaxFindings = settings.Ollama.MaxFindings
	} else {
		normalized.Endpoint = effectiveRemoteEndpoint(provider, settings.Remote.BaseURL)
		normalized.Model = strings.TrimSpace(settings.Remote.Model)
		normalized.APIKeyEnvironment = strings.TrimSpace(settings.Remote.APIKeyEnv)
		normalized.ProviderName = strings.TrimSpace(settings.Remote.ProviderName)
		normalized.TimeoutSeconds = settings.Remote.TimeoutSeconds
		normalized.MaxFindings = settings.Remote.MaxFindings
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("fingerprint AI configuration: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func effectiveRemoteEndpoint(provider, configured string) string {
	if value := strings.TrimRight(strings.TrimSpace(configured), "/"); value != "" {
		return value
	}
	switch provider {
	case "openai":
		return "https://api.openai.com/v1/responses"
	case "anthropic":
		return "https://api.anthropic.com/v1/messages"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/interactions"
	default:
		return ""
	}
}

func canonicalIdentity(repositoryPath, configPath string) (string, string, error) {
	if strings.TrimSpace(configPath) == "" {
		return "", "", errors.New("automatic AI review requires a saved configuration file")
	}
	repository, err := canonicalExistingPath(repositoryPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve review-consent repository: %w", err)
	}
	repositoryInfo, err := os.Stat(repository)
	if err != nil || !repositoryInfo.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return "", "", fmt.Errorf("inspect review-consent repository: %w", err)
	}
	configFile, err := canonicalExistingPath(configPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve review-consent config: %w", err)
	}
	configInfo, err := os.Stat(configFile)
	if err != nil || !configInfo.Mode().IsRegular() {
		if err == nil {
			err = errors.New("not a regular file")
		}
		return "", "", fmt.Errorf("inspect review-consent config: %w", err)
	}
	return repository, configFile, nil
}

func canonicalExistingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func anchorBelowExistingParent(path string) (string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	current := filepath.Dir(abs)
	missing := []string{filepath.Base(abs)}
	for {
		canonical, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			directory := canonical
			for index := len(missing) - 1; index >= 0; index-- {
				directory = filepath.Join(directory, missing[index])
			}
			return canonical, directory, nil
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", "", resolveErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", "", resolveErr
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func identityKey(repository, configFile string) string {
	sum := sha256.Sum256([]byte(repository + "\x00" + configFile))
	return hex.EncodeToString(sum[:])
}

func (s Store) ensureDirectory() (string, error) {
	root, relative, err := s.pathParts()
	if err != nil {
		return "", err
	}
	current := root
	for _, part := range relative {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create review consent directory: %w", err)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect review consent directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("review consent directory is not a real directory")
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return "", fmt.Errorf("protect review consent directory: %w", err)
		}
	}
	return current, nil
}

func (s Store) existingDirectory() (string, bool, error) {
	root, relative, err := s.pathParts()
	if err != nil {
		return "", false, err
	}
	current := root
	for _, part := range relative {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("inspect review consent directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, errors.New("review consent directory is not a real directory")
		}
		if info.Mode().Perm() != 0o700 {
			return "", false, errors.New("review consent directory permissions are not private")
		}
	}
	return current, true, nil
}

func (s Store) pathParts() (string, []string, error) {
	directory := filepath.Clean(s.Directory)
	if directory == "." || directory == "" {
		return "", nil, errors.New("review consent directory is empty")
	}
	root := s.root
	if root == "" {
		root = filepath.Dir(directory)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve review consent root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve review consent root: %w", err)
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return "", nil, fmt.Errorf("resolve review consent directory: %w", err)
	}
	relativePath, err := filepath.Rel(root, directory)
	if err != nil || relativePath == "." || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", nil, errors.New("review consent directory must be below its trusted root")
	}
	return root, strings.Split(relativePath, string(filepath.Separator)), nil
}

func rejectUnsafeExistingFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing review consent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("review consent path is not a regular file")
	}
	return nil
}

func readRecord(path string) (record, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, fmt.Errorf("inspect review consent: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return record{}, false, errors.New("review consent path is not a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return record{}, false, errors.New("review consent permissions are not private")
	}
	if info.Size() > maxRecordBytes {
		return record{}, false, errors.New("review consent record is too large")
	}
	file, err := os.Open(path)
	if err != nil {
		return record{}, false, fmt.Errorf("open review consent: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return record{}, false, fmt.Errorf("inspect open review consent: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return record{}, false, errors.New("review consent changed while it was opened")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxRecordBytes+1))
	decoder.DisallowUnknownFields()
	var value record
	if err := decoder.Decode(&value); err != nil {
		return record{}, false, fmt.Errorf("decode review consent: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return record{}, false, fmt.Errorf("decode review consent: %w", err)
	}
	if value.Version != schemaVersion {
		return record{}, false, fmt.Errorf("unsupported review consent version %d", value.Version)
	}
	if strings.TrimSpace(value.Repository) == "" || strings.TrimSpace(value.Config) == "" || value.ConsentedAt.IsZero() {
		return record{}, false, errors.New("review consent record is incomplete")
	}
	if len(value.AIDigest) != sha256.Size*2 {
		return record{}, false, errors.New("review consent AI digest is invalid")
	}
	if _, err := hex.DecodeString(value.AIDigest); err != nil {
		return record{}, false, errors.New("review consent AI digest is invalid")
	}
	return value, true, nil
}
