package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	value.Tool.Version = "0.2.1"
	if _, err := WriteArtifacts(directory, value); err != nil {
		t.Fatal(err)
	}
	jsonData, err = os.ReadFile(artifacts.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(jsonData), "0.2.1") {
		t.Fatalf("artifact was not replaced:\n%s", jsonData)
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
