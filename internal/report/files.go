package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultDirectory = ".complyscan/reports"

type Artifacts struct {
	Markdown string
	JSON     string
}

// WriteArtifacts atomically replaces the human-readable report and the
// machine-readable evidence bundle generated from the same scan value.
func WriteArtifacts(directory string, value Report) (Artifacts, error) {
	artifacts := Artifacts{
		Markdown: filepath.Join(directory, "latest.md"),
		JSON:     filepath.Join(directory, "latest.json"),
	}
	if err := ensureReportDirectory(directory); err != nil {
		return Artifacts{}, err
	}
	for _, path := range []string{artifacts.Markdown, artifacts.JSON} {
		if err := validateArtifactDestination(path); err != nil {
			return Artifacts{}, err
		}
	}

	var markdown, jsonData bytes.Buffer
	if err := WriteMarkdown(&markdown, value); err != nil {
		return Artifacts{}, fmt.Errorf("render Markdown report: %w", err)
	}
	if err := WriteJSON(&jsonData, value); err != nil {
		return Artifacts{}, fmt.Errorf("render JSON evidence bundle: %w", err)
	}
	if err := writeArtifactAtomic(artifacts.Markdown, markdown.Bytes()); err != nil {
		return Artifacts{}, err
	}
	if err := writeArtifactAtomic(artifacts.JSON, jsonData.Bytes()); err != nil {
		return Artifacts{}, err
	}
	return artifacts, nil
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
