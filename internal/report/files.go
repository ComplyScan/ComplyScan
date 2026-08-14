package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const DefaultDirectory = ".complyscan/reports"

const historyDirectory = "history"

var safeScanIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

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
	name, err := historyBundleName(value.Scan)
	if err != nil {
		return Artifacts{}, err
	}
	root := filepath.Join(directory, historyDirectory)
	if err := ensureReportDirectory(root); err != nil {
		return Artifacts{}, err
	}
	bundle := filepath.Join(root, name)
	artifacts := Artifacts{Markdown: filepath.Join(bundle, "report.md"), JSON: filepath.Join(bundle, "report.json")}
	if same, err := existingHistoryMatches(bundle, artifacts, markdown, jsonData); err != nil {
		return Artifacts{}, err
	} else if same {
		return artifacts, nil
	}

	temporary, err := os.MkdirTemp(root, ".complyscan-history-*")
	if err != nil {
		return Artifacts{}, fmt.Errorf("create temporary report history in %q: %w", root, err)
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.RemoveAll(temporary)
		}
	}()
	if err := writeArtifactAtomic(filepath.Join(temporary, "report.md"), markdown); err != nil {
		return Artifacts{}, fmt.Errorf("write historical Markdown report: %w", err)
	}
	if err := writeArtifactAtomic(filepath.Join(temporary, "report.json"), jsonData); err != nil {
		return Artifacts{}, fmt.Errorf("write historical JSON report: %w", err)
	}
	if err := os.Rename(temporary, bundle); err != nil {
		if same, compareErr := existingHistoryMatches(bundle, artifacts, markdown, jsonData); compareErr == nil && same {
			return artifacts, nil
		}
		return Artifacts{}, fmt.Errorf("publish immutable report history %q: %w", bundle, err)
	}
	removeTemporary = false
	return artifacts, nil
}

func historyBundleName(scan ScanMetadata) (string, error) {
	created, err := time.Parse(time.RFC3339Nano, scan.CreatedAt)
	if err != nil {
		return "", fmt.Errorf("create report history name from scan time %q: %w", scan.CreatedAt, err)
	}
	if !safeScanIDPattern.MatchString(scan.ID) {
		return "", fmt.Errorf("create report history name from unsafe scan ID %q", scan.ID)
	}
	return created.UTC().Format("20060102T150405Z") + "-" + scan.ID, nil
}

func existingHistoryMatches(bundle string, artifacts Artifacts, markdown, jsonData []byte) (bool, error) {
	info, err := os.Lstat(bundle)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect report history %q: %w", bundle, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("report history %q must be a directory and must not be a symlink", bundle)
	}
	for _, path := range []string{artifacts.Markdown, artifacts.JSON} {
		if err := validateArtifactDestination(path); err != nil {
			return false, err
		}
	}
	storedMarkdown, err := os.ReadFile(artifacts.Markdown)
	if err != nil {
		return false, fmt.Errorf("read immutable report history %q: %w", artifacts.Markdown, err)
	}
	storedJSON, err := os.ReadFile(artifacts.JSON)
	if err != nil {
		return false, fmt.Errorf("read immutable report history %q: %w", artifacts.JSON, err)
	}
	if !bytes.Equal(storedMarkdown, markdown) || !bytes.Equal(storedJSON, jsonData) {
		return false, fmt.Errorf("immutable report history %q already exists with different content", bundle)
	}
	return true, nil
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
