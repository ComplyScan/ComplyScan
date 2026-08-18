package modelqualification

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	cacheSchemaVersion = 5
	cacheFileName      = "model-qualification-v5.json"
	maxCacheBytes      = 128 << 10
	maxCacheEntries    = 100
)

type cacheFile struct {
	SchemaVersion int               `json:"schema_version"`
	Entries       map[string]Result `json:"entries"`
}

type Cache struct {
	path    string
	entries map[string]Result
}

func DefaultPath() (string, error) {
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(directory, "complyscan", cacheFileName), nil
}

func Open(path string) (*Cache, error) {
	cache := &Cache{path: path, entries: make(map[string]Result)}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect model qualification cache: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("model qualification cache must be a regular file and must not be a symlink")
	}
	if info.Size() > maxCacheBytes {
		return nil, fmt.Errorf("model qualification cache exceeds %d bytes", maxCacheBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open model qualification cache: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxCacheBytes+1))
	decoder.DisallowUnknownFields()
	var stored cacheFile
	if err := decoder.Decode(&stored); err != nil {
		return nil, fmt.Errorf("parse model qualification cache: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("parse model qualification cache: expected one JSON value")
	}
	if stored.SchemaVersion != cacheSchemaVersion {
		return nil, fmt.Errorf("unsupported model qualification cache schema %d", stored.SchemaVersion)
	}
	if len(stored.Entries) > maxCacheEntries {
		return nil, fmt.Errorf("model qualification cache exceeds %d entries", maxCacheEntries)
	}
	for key, result := range stored.Entries {
		if key != identityKey(result.Identity) {
			return nil, fmt.Errorf("model qualification cache entry %q has an invalid key", key)
		}
		if err := validateResult(result); err != nil {
			return nil, fmt.Errorf("model qualification cache entry %q: %w", key, err)
		}
	}
	cache.entries = stored.Entries
	if cache.entries == nil {
		cache.entries = make(map[string]Result)
	}
	return cache, nil
}

func (cache *Cache) Lookup(identity Identity, now time.Time) (Result, bool, error) {
	if err := validateIdentity(identity); err != nil {
		return Result{}, false, err
	}
	result, found := cache.entries[identityKey(identity)]
	if !found || !now.UTC().Before(result.ExpiresAt) {
		return Result{}, false, nil
	}
	if err := validateResult(result); err != nil {
		return Result{}, false, err
	}
	result.FromCache = true
	return result, true, nil
}

func (cache *Cache) Store(result Result) error {
	if err := validateResult(result); err != nil {
		return err
	}
	cache.entries[identityKey(result.Identity)] = result
	cache.prune()
	return cache.write()
}

func validateResult(result Result) error {
	if err := validateIdentity(result.Identity); err != nil {
		return err
	}
	if result.Status != "compatible" || result.CheckedAt.IsZero() || !result.ExpiresAt.After(result.CheckedAt) || result.ExpiresAt.Sub(result.CheckedAt) > CacheValidity {
		return errors.New("cached model qualification result is invalid")
	}
	if len(result.Detail) == 0 || len(result.Detail) > 500 {
		return errors.New("cached model qualification detail is invalid")
	}
	return nil
}

func identityKey(identity Identity) string {
	data, _ := json.Marshal(identity)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (cache *Cache) prune() {
	if len(cache.entries) <= maxCacheEntries {
		return
	}
	keys := make([]string, 0, len(cache.entries))
	for key := range cache.entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return cache.entries[keys[i]].CheckedAt.Before(cache.entries[keys[j]].CheckedAt)
	})
	for _, key := range keys[:len(keys)-maxCacheEntries] {
		delete(cache.entries, key)
	}
}

func (cache *Cache) write() error {
	directory := filepath.Dir(cache.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create model qualification cache directory: %w", err)
	}
	if info, err := os.Lstat(cache.path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("model qualification cache must be a regular file and must not be a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect model qualification cache: %w", err)
	}
	data, err := json.MarshalIndent(cacheFile{SchemaVersion: cacheSchemaVersion, Entries: cache.entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode model qualification cache: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxCacheBytes {
		return fmt.Errorf("encoded model qualification cache exceeds %d bytes", maxCacheBytes)
	}
	temporary, err := os.CreateTemp(directory, ".complyscan-model-qualification-*")
	if err != nil {
		return fmt.Errorf("create temporary model qualification cache: %w", err)
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
		return fmt.Errorf("set model qualification cache permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write model qualification cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync model qualification cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close model qualification cache: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, cache.path); err != nil {
		return fmt.Errorf("replace model qualification cache: %w", err)
	}
	return nil
}
