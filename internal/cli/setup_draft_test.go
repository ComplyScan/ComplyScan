package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
)

func TestSetupDraftRoundTripContainsConfigurationWithoutSecretsOrSource(t *testing.T) {
	target := t.TempDir()
	path := filepath.Join(t.TempDir(), "nested", "draft.json")
	cfg := config.Default()
	cfg.AI.Provider = "openai"
	cfg.AI.Remote.Model = "gpt-test"
	cfg.AI.Remote.APIKeyEnv = "PRIVATE_TEST_API_KEY"
	cfg.AI.Remote.TimeoutSeconds = 120
	cfg.AI.Remote.MaxFindings = 20
	now := time.Date(2026, time.August, 11, 10, 30, 0, 0, time.UTC)
	if err := writeSetupDraft(path, target, setupDraftReview, cfg, setupScanDeep, true, now); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("draft permissions = %o, want 600", info.Mode().Perm())
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("draft directory permissions = %o, want 700", directoryInfo.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-secret-value") || !strings.Contains(string(data), "PRIVATE_TEST_API_KEY") {
		t.Fatalf("draft credential handling is unsafe:\n%s", data)
	}
	loaded, found, err := loadSetupDraft(path, target, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.Stage != setupDraftReview || loaded.ScanMode != setupScanDeep || !loaded.ModelReady {
		t.Fatalf("loaded draft = %#v, found=%t", loaded, found)
	}
	if loaded.Config.AI.Provider != "openai" || loaded.Config.AI.Remote.Model != "gpt-test" {
		t.Fatalf("loaded AI config = %#v", loaded.Config.AI)
	}
}

func TestSetupDraftRejectsExpiredAndMismatchedTargets(t *testing.T) {
	target := t.TempDir()
	path := filepath.Join(t.TempDir(), "draft.json")
	now := time.Now().UTC()
	if err := writeSetupDraft(path, target, setupDraftAnalysis, config.Default(), setupScanNone, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadSetupDraft(path, t.TempDir(), now.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("mismatched target error = %v", err)
	}
	if _, _, err := loadSetupDraft(path, target, now.Add(setupDraftValidity+time.Second)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired draft error = %v", err)
	}
}

func TestSetupDraftRefusesSymlinkForReadWriteAndRemoval(t *testing.T) {
	target := t.TempDir()
	directory := t.TempDir()
	realPath := filepath.Join(directory, "real.json")
	if err := os.WriteFile(realPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(directory, "draft.json")
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadSetupDraft(linkPath, target, time.Now()); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("load symlink error = %v", err)
	}
	if err := writeSetupDraft(linkPath, target, setupDraftAnalysis, config.Default(), setupScanNone, true, time.Now()); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("write symlink error = %v", err)
	}
	if err := removeSetupDraft(linkPath); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("remove symlink error = %v", err)
	}
}

func TestPromptSetupDraftResumeDefaultsToResume(t *testing.T) {
	var output bytes.Buffer
	prompt := promptSession{reader: bufio.NewReader(strings.NewReader("\n")), output: &output}
	resume, err := promptSetupDraftResume(prompt, setupDraft{UpdatedAt: time.Date(2026, time.August, 11, 10, 0, 0, 0, time.Local)})
	if err != nil {
		t.Fatal(err)
	}
	if !resume || !strings.Contains(output.String(), "Resume saved answers") || !strings.Contains(output.String(), "[READY] Resuming progress") {
		t.Fatalf("resume=%t output:\n%s", resume, output.String())
	}
}

func TestRemoveSetupDraftDeletesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeSetupDraft(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("draft still exists: %v", err)
	}
}

func TestCheckpointSetupDraftReportsWhetherRecoveryWasSaved(t *testing.T) {
	target := t.TempDir()
	var output bytes.Buffer
	prompt := promptSession{output: &output}
	validPath := filepath.Join(t.TempDir(), "draft.json")
	if !checkpointSetupDraft(prompt, validPath, target, setupDraftAnalysis, config.Default(), setupScanNone, true) {
		t.Fatalf("valid checkpoint was not reported as saved: %s", output.String())
	}
	output.Reset()
	blockedParent := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if checkpointSetupDraft(prompt, filepath.Join(blockedParent, "draft.json"), target, setupDraftAnalysis, config.Default(), setupScanNone, true) {
		t.Fatal("invalid checkpoint was reported as saved")
	}
	if !strings.Contains(output.String(), "[NEEDS REVIEW] Setup recovery checkpoint could not be saved") {
		t.Fatalf("checkpoint failure was not explained: %s", output.String())
	}
}
