package baseline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/rules"
)

const FileName = ".complyscan-baseline.json"

const formatVersion = 1

// File is the deterministic, source-free representation committed by users.
type File struct {
	Version  int     `json:"version"`
	Findings []Entry `json:"findings"`
}

type Entry struct {
	Fingerprint string `json:"fingerprint"`
	RuleID      string `json:"rule_id"`
	Path        string `json:"path,omitempty"`
	Title       string `json:"title"`
}

func New(findings []rules.Finding) File {
	entries := make([]Entry, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		if _, exists := seen[finding.Fingerprint]; exists {
			continue
		}
		seen[finding.Fingerprint] = struct{}{}
		entries = append(entries, Entry{
			Fingerprint: finding.Fingerprint,
			RuleID:      finding.RuleID,
			Path:        finding.Path,
			Title:       finding.Title,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Fingerprint < entries[j].Fingerprint })
	return File{Version: formatVersion, Findings: entries}
}

func Load(path string) (File, error) {
	file, err := os.Open(path)
	if err != nil {
		return File{}, fmt.Errorf("open baseline %q: %w", path, err)
	}
	defer file.Close()

	var value File
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return File{}, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return File{}, fmt.Errorf("parse baseline %q: %w", path, err)
	}
	if err := value.Validate(); err != nil {
		return File{}, fmt.Errorf("validate baseline %q: %w", path, err)
	}
	sort.Slice(value.Findings, func(i, j int) bool {
		return value.Findings[i].Fingerprint < value.Findings[j].Fingerprint
	})
	return value, nil
}

func Write(path string, findings []rules.Finding) error {
	value := New(findings)
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".complyscan-baseline-*")
	if err != nil {
		return fmt.Errorf("create baseline beside %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		temporary.Close()
		return fmt.Errorf("encode baseline %q: %w", path, err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("set baseline permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close baseline %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace baseline %q: %w", path, err)
	}
	return nil
}

func (file File) Validate() error {
	if file.Version != formatVersion {
		return fmt.Errorf("unsupported version %d", file.Version)
	}
	for index, entry := range file.Findings {
		if !validFingerprint(entry.Fingerprint) {
			return fmt.Errorf("findings[%d].fingerprint is not a 64-character SHA-256 value", index)
		}
		if strings.TrimSpace(entry.RuleID) == "" {
			return fmt.Errorf("findings[%d].rule_id must not be empty", index)
		}
	}
	return nil
}

func (file File) Contains(fingerprint string) bool {
	index := sort.Search(len(file.Findings), func(index int) bool {
		return file.Findings[index].Fingerprint >= fingerprint
	})
	return index < len(file.Findings) && file.Findings[index].Fingerprint == fingerprint
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
