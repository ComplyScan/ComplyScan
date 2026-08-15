package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const DefaultDirectory = ".complyscan/reports"

const historyDirectory = "history"

type historyBundleState uint8

const (
	historyBundleAbsent historyBundleState = iota
	historyBundleMatching
	historyBundleOccupied
)

type Artifacts struct {
	Markdown        string
	JSON            string
	HistoryMarkdown string
	HistoryJSON     string
}

// WriteArtifacts publishes one immutable historical bundle for a completed
// scan, then refreshes the backward-compatible latest snapshots.
func WriteArtifacts(directory string, value Report) (Artifacts, error) {
	artifacts := latestArtifacts(directory)
	if err := ensureReportDirectory(directory); err != nil {
		return Artifacts{}, err
	}
	markdown, jsonData, err := renderArtifacts(value)
	if err != nil {
		return Artifacts{}, err
	}
	history, err := writeHistoryBundle(directory, value, markdown, jsonData)
	if err != nil {
		return Artifacts{}, err
	}
	artifacts.HistoryMarkdown = history.Markdown
	artifacts.HistoryJSON = history.JSON
	if err := writeLatestArtifacts(artifacts, markdown, jsonData); err != nil {
		return Artifacts{}, err
	}
	return artifacts, nil
}

// WriteLatestArtifacts refreshes recovery checkpoints without adding an
// in-progress scan to immutable history.
func WriteLatestArtifacts(directory string, value Report) (Artifacts, error) {
	artifacts := latestArtifacts(directory)
	if err := ensureReportDirectory(directory); err != nil {
		return Artifacts{}, err
	}
	markdown, jsonData, err := renderArtifacts(value)
	if err != nil {
		return Artifacts{}, err
	}
	if err := writeLatestArtifacts(artifacts, markdown, jsonData); err != nil {
		return Artifacts{}, err
	}
	return artifacts, nil
}

func latestArtifacts(directory string) Artifacts {
	return Artifacts{Markdown: filepath.Join(directory, "latest.md"), JSON: filepath.Join(directory, "latest.json")}
}

func renderArtifacts(value Report) ([]byte, []byte, error) {
	var markdown, jsonData bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		return nil, nil, fmt.Errorf("render Markdown report: %w", err)
	}
	if err := WriteJSON(&jsonData, value); err != nil {
		return nil, nil, fmt.Errorf("render JSON evidence bundle: %w", err)
	}
	return markdown.Bytes(), jsonData.Bytes(), nil
}

func writeLatestArtifacts(artifacts Artifacts, markdown, jsonData []byte) error {
	for _, path := range []string{artifacts.Markdown, artifacts.JSON} {
		if err := validateArtifactDestination(path); err != nil {
			return err
		}
	}
	if err := writeArtifactAtomic(artifacts.Markdown, markdown); err != nil {
		return err
	}
	return writeArtifactAtomic(artifacts.JSON, jsonData)
}

func writeHistoryBundle(directory string, value Report, markdown, jsonData []byte) (Artifacts, error) {
	baseName, err := historyBundleName(value.Scan)
	if err != nil {
		return Artifacts{}, err
	}
	root := filepath.Join(directory, historyDirectory)
	if err := ensureReportDirectory(root); err != nil {
		return Artifacts{}, err
	}
	for sequence := 1; ; sequence++ {
		name := baseName
		if sequence > 1 {
			name = fmt.Sprintf("%s-%d", baseName, sequence)
		}
		artifacts, occupied, err := publishHistoryBundle(root, name, value.Scan.ID, markdown, jsonData)
		if err != nil {
			return Artifacts{}, err
		}
		if !occupied {
			return artifacts, nil
		}
	}
}

func publishHistoryBundle(root, name, scanID string, markdown, jsonData []byte) (Artifacts, bool, error) {
	bundle := filepath.Join(root, name)
	artifacts := Artifacts{Markdown: filepath.Join(bundle, "report.md"), JSON: filepath.Join(bundle, "report.json")}
	state, err := inspectExistingHistory(bundle, artifacts, scanID, markdown, jsonData)
	if err != nil {
		return Artifacts{}, false, err
	}
	switch state {
	case historyBundleMatching:
		return artifacts, false, nil
	case historyBundleOccupied:
		return Artifacts{}, true, nil
	}

	temporary, err := os.MkdirTemp(root, ".complyscan-history-*")
	if err != nil {
		return Artifacts{}, false, fmt.Errorf("create temporary report history in %q: %w", root, err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := writeArtifactAtomic(filepath.Join(temporary, "report.md"), markdown); err != nil {
		return Artifacts{}, false, fmt.Errorf("write historical Markdown report: %w", err)
	}
	if err := writeArtifactAtomic(filepath.Join(temporary, "report.json"), jsonData); err != nil {
		return Artifacts{}, false, fmt.Errorf("write historical JSON report: %w", err)
	}
	if err := os.Rename(temporary, bundle); err != nil {
		state, inspectErr := inspectExistingHistory(bundle, artifacts, scanID, markdown, jsonData)
		if inspectErr != nil {
			return Artifacts{}, false, inspectErr
		}
		switch state {
		case historyBundleMatching:
			return artifacts, false, nil
		case historyBundleOccupied:
			return Artifacts{}, true, nil
		}
		return Artifacts{}, false, fmt.Errorf("publish immutable report history %q: %w", bundle, err)
	}
	removeTemporary = false
	return artifacts, false, nil
}

func historyBundleName(scan ScanMetadata) (string, error) {
	created, err := time.Parse(time.RFC3339Nano, scan.CreatedAt)
	if err != nil {
		return "", fmt.Errorf("create report history name from scan time %q: %w", scan.CreatedAt, err)
	}
	return created.UTC().Format("2006-01-02_15-04-05Z"), nil
}

func inspectExistingHistory(bundle string, artifacts Artifacts, scanID string, markdown, jsonData []byte) (historyBundleState, error) {
	info, err := os.Lstat(bundle)
	if errors.Is(err, os.ErrNotExist) {
		return historyBundleAbsent, nil
	}
	if err != nil {
		return historyBundleAbsent, fmt.Errorf("inspect report history %q: %w", bundle, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return historyBundleAbsent, fmt.Errorf("report history %q must be a directory and must not be a symlink", bundle)
	}
	for _, path := range []string{artifacts.Markdown, artifacts.JSON} {
		if err := validateArtifactDestination(path); err != nil {
			return historyBundleAbsent, err
		}
	}
	storedMarkdown, err := os.ReadFile(artifacts.Markdown)
	if err != nil {
		return historyBundleAbsent, fmt.Errorf("read immutable report history %q: %w", artifacts.Markdown, err)
	}
	storedJSON, err := os.ReadFile(artifacts.JSON)
	if err != nil {
		return historyBundleAbsent, fmt.Errorf("read immutable report history %q: %w", artifacts.JSON, err)
	}
	if bytes.Equal(storedMarkdown, markdown) && bytes.Equal(storedJSON, jsonData) {
		return historyBundleMatching, nil
	}
	var stored struct {
		Scan struct {
			ID string `json:"id"`
		} `json:"scan"`
	}
	if err := json.Unmarshal(storedJSON, &stored); err != nil {
		return historyBundleAbsent, fmt.Errorf("read scan identity from immutable report history %q: %w", artifacts.JSON, err)
	}
	if stored.Scan.ID == scanID {
		return historyBundleAbsent, fmt.Errorf("immutable report history %q already exists with different content", bundle)
	}
	return historyBundleOccupied, nil
}

func ensureReportDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("report path %q must be a directory and must not be a symlink", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect report directory %q: %w", path, err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create report directory %q: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect created report directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("report path %q must be a directory and must not be a symlink", path)
	}
	return nil
}

func validateArtifactDestination(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect report artifact %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("report artifact %q must be a regular file and must not be a symlink", path)
	}
	return nil
}

func writeArtifactAtomic(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect report artifact %q: %w", path, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".complyscan-report-*")
	if err != nil {
		return fmt.Errorf("create temporary report beside %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary report permissions for %q: %w", path, err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary report for %q: %w", path, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary report for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary report for %q: %w", path, err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace report artifact %q: %w", path, err)
	}
	return nil
}
