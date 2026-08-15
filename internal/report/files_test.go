package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteArtifactsCreatesMatchingMarkdownAndJSON(t *testing.T) {
	directory := filepath.Join(t.TempDir(), ".complyscan", "reports")
	value := New(".", "0.2.0-dev", nil, nil, 0)
	artifacts, err := WriteArtifacts(directory, value)
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(artifacts.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err := os.ReadFile(artifacts.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(markdown), value.Scan.ID) {
		t.Fatalf("Markdown does not identify scan %q:\n%s", value.Scan.ID, markdown)
	}
	var decoded Report
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scan.ID != value.Scan.ID {
		t.Fatalf("JSON scan ID = %q, want %q", decoded.Scan.ID, value.Scan.ID)
	}
	for _, path := range []string{artifacts.HistoryMarkdown, artifacts.HistoryJSON} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("historical artifact %q was not written: %v", path, err)
		}
		if strings.Contains(path, value.Scan.ID) {
			t.Fatalf("historical path %q unexpectedly contains scan ID %q", path, value.Scan.ID)
		}
	}
	historicalJSON, err := os.ReadFile(artifacts.HistoryJSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(historicalJSON) != string(jsonData) {
		t.Fatal("latest and historical JSON do not describe the same completed scan")
	}

	second := New(".", "0.2.1", nil, nil, 0)
	secondArtifacts, err := WriteArtifacts(directory, second)
	if err != nil {
		t.Fatal(err)
	}
	jsonData, err = os.ReadFile(secondArtifacts.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonData), "0.2.1") {
		t.Fatalf("artifact was not replaced:\n%s", jsonData)
	}
	historicalJSON, err = os.ReadFile(artifacts.HistoryJSON)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(historicalJSON), "0.2.1") {
		t.Fatalf("first historical artifact was replaced:\n%s", historicalJSON)
	}
	entries, err := os.ReadDir(filepath.Join(directory, historyDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("history entries = %d, want 2", len(entries))
	}
	if repeated, err := WriteArtifacts(directory, second); err != nil || repeated.HistoryJSON != secondArtifacts.HistoryJSON {
		t.Fatalf("idempotent history write = %#v, %v", repeated, err)
	}
	second.Tool.Version = "changed-after-scan"
	if _, err := WriteArtifacts(directory, second); err == nil || !strings.Contains(err.Error(), "immutable report history") {
		t.Fatalf("mutable history write error = %v", err)
	}
}

func TestWriteArtifactsAddsNumericSuffixForSameSecond(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "reports")
	createdAt := time.Date(2026, time.August, 14, 16, 56, 29, 0, time.UTC)
	scope := ScanScope{Findings: "full-repository", TechnicalEvidence: "full-repository"}
	first := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "one"}, scope, createdAt, nil, nil, 0)
	second := NewWithMetadata(".", Tool{Name: "ComplyScan", Version: "two"}, scope, createdAt, nil, nil, 0)

	firstArtifacts, err := WriteArtifacts(directory, first)
	if err != nil {
		t.Fatal(err)
	}
	secondArtifacts, err := WriteArtifacts(directory, second)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(filepath.Dir(firstArtifacts.HistoryJSON)), "2026-08-14_16-56-29Z"; got != want {
		t.Fatalf("first history directory = %q, want %q", got, want)
	}
	if got, want := filepath.Base(filepath.Dir(secondArtifacts.HistoryJSON)), "2026-08-14_16-56-29Z-2"; got != want {
		t.Fatalf("second history directory = %q, want %q", got, want)
	}
}

func TestWriteLatestArtifactsDoesNotPublishHistory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "reports")
	value := New(".", "dev", nil, nil, 0)
	checkpoint, err := WriteLatestArtifacts(directory, value)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.HistoryMarkdown != "" || checkpoint.HistoryJSON != "" {
		t.Fatalf("checkpoint unexpectedly returned history: %#v", checkpoint)
	}
	if _, err := os.Stat(filepath.Join(directory, historyDirectory)); !os.IsNotExist(err) {
		t.Fatalf("checkpoint created history directory: %v", err)
	}
	completed, err := WriteArtifacts(directory, value)
	if err != nil {
		t.Fatal(err)
	}
	if completed.HistoryMarkdown == "" || completed.HistoryJSON == "" {
		t.Fatalf("completed scan did not publish history: %#v", completed)
	}
}

func TestWriteArtifactsRejectsSymlinkDestination(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "outside.json")
	if err := os.WriteFile(target, []byte("do not replace"), 0o644); err != nil {
		t.Fatal(err)
	}
	reportDirectory := filepath.Join(directory, "reports")
	if err := os.Mkdir(reportDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(reportDirectory, "latest.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := WriteArtifacts(reportDirectory, New(".", "test", nil, nil, 0)); err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("got error %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "do not replace" {
		t.Fatalf("symlink target was modified: %q", data)
	}
}

func TestWriteArtifactsRejectsSymlinkHistoryDirectory(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "outside")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	reportDirectory := filepath.Join(directory, "reports")
	if err := os.Mkdir(reportDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(reportDirectory, historyDirectory)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := WriteArtifacts(reportDirectory, New(".", "test", nil, nil, 0)); err == nil || !strings.Contains(err.Error(), "must not be a symlink") {
		t.Fatalf("got error %v", err)
	}
}
