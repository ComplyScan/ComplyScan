package cli

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ComplyScan/ComplyScan/internal/config"
)

const (
	setupDraftSchemaVersion = 1
	setupDraftMaxBytes      = 512 << 10
	setupDraftValidity      = 30 * 24 * time.Hour
)

type setupDraftStage string

const (
	setupDraftAnalysis setupDraftStage = "analysis-configured"
	setupDraftContext  setupDraftStage = "context-confirmed"
	setupDraftReview   setupDraftStage = "ready-for-review"
)

type setupDraft struct {
	SchemaVersion int             `json:"schema_version"`
	Target        string          `json:"target"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Stage         setupDraftStage `json:"stage"`
	Config        config.Config   `json:"config"`
	ScanMode      setupScanMode   `json:"scan_mode"`
	ModelReady    bool            `json:"model_ready"`
}

func defaultSetupDraftPath(target string) (string, error) {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("resolve setup target: %w", err)
	}
	directory, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	identity := fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(absolute))))
	return filepath.Join(directory, "complyscan", "setup-drafts", identity+".json"), nil
}

func loadSetupDraft(path, target string, now time.Time) (setupDraft, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return setupDraft{}, false, nil
	}
	if err != nil {
		return setupDraft{}, false, fmt.Errorf("inspect setup draft: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return setupDraft{}, false, errors.New("setup draft must be a regular file and must not be a symlink")
	}
	if info.Size() > setupDraftMaxBytes {
		return setupDraft{}, false, fmt.Errorf("setup draft exceeds %d bytes", setupDraftMaxBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return setupDraft{}, false, fmt.Errorf("open setup draft: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, setupDraftMaxBytes+1))
	decoder.DisallowUnknownFields()
	var draft setupDraft
	if err := decoder.Decode(&draft); err != nil {
		return setupDraft{}, false, fmt.Errorf("parse setup draft: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return setupDraft{}, false, errors.New("parse setup draft: expected one JSON value")
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return setupDraft{}, false, fmt.Errorf("resolve setup target: %w", err)
	}
	if draft.SchemaVersion != setupDraftSchemaVersion || filepath.Clean(draft.Target) != filepath.Clean(absolute) {
		return setupDraft{}, false, errors.New("setup draft identity is invalid")
	}
	if draft.UpdatedAt.IsZero() || now.Before(draft.UpdatedAt) || now.Sub(draft.UpdatedAt) > setupDraftValidity {
		return setupDraft{}, false, errors.New("setup draft is expired or has an invalid timestamp")
	}
	if setupDraftStageRank(draft.Stage) == 0 {
		return setupDraft{}, false, fmt.Errorf("setup draft stage %q is invalid", draft.Stage)
	}
	if draft.ScanMode != setupScanNone && draft.ScanMode != setupScanQuick && draft.ScanMode != setupScanDeep {
		return setupDraft{}, false, fmt.Errorf("setup draft scan mode %q is invalid", draft.ScanMode)
	}
	if err := draft.Config.Validate(); err != nil {
		return setupDraft{}, false, fmt.Errorf("validate setup draft configuration: %w", err)
	}
	return draft, true, nil
}

func writeSetupDraft(path, target string, stage setupDraftStage, cfg config.Config, scanMode setupScanMode, modelReady bool, now time.Time) error {
	if setupDraftStageRank(stage) == 0 {
		return fmt.Errorf("setup draft stage %q is invalid", stage)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate setup draft configuration: %w", err)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve setup target: %w", err)
	}
	draft := setupDraft{
		SchemaVersion: setupDraftSchemaVersion,
		Target:        filepath.Clean(absolute),
		UpdatedAt:     now.UTC(),
		Stage:         stage,
		Config:        cfg,
		ScanMode:      scanMode,
		ModelReady:    modelReady,
	}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return fmt.Errorf("encode setup draft: %w", err)
	}
	data = append(data, '\n')
	if len(data) > setupDraftMaxBytes {
		return fmt.Errorf("encoded setup draft exceeds %d bytes", setupDraftMaxBytes)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create setup draft directory: %w", err)
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect setup draft directory: %w", err)
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return errors.New("setup draft directory must be a directory and must not be a symlink")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("set setup draft directory permissions: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("setup draft must be a regular file and must not be a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect setup draft: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".complyscan-setup-draft-*")
	if err != nil {
		return fmt.Errorf("create temporary setup draft: %w", err)
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
		return fmt.Errorf("set setup draft permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write setup draft: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync setup draft: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close setup draft: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace setup draft: %w", err)
	}
	return nil
}

func removeSetupDraft(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect setup draft: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("setup draft must be a regular file and must not be a symlink")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove setup draft: %w", err)
	}
	return nil
}

func setupDraftStageRank(stage setupDraftStage) int {
	switch stage {
	case setupDraftAnalysis:
		return 1
	case setupDraftContext:
		return 2
	case setupDraftReview:
		return 3
	default:
		return 0
	}
}

func promptSetupDraftResume(prompt promptSession, draft setupDraft) (bool, error) {
	if err := explainSetupQuestion(prompt, "resume-setup"); err != nil {
		return false, err
	}
	const (
		resume    = "Resume saved answers"
		startOver = "Start over and discard the draft"
	)
	selected, err := promptChoice(prompt, "setup recovery", resume, resume, startOver)
	if err != nil {
		return false, err
	}
	if selected == resume {
		return true, prompt.status(setupStatusReady, fmt.Sprintf("Resuming progress saved %s.", draft.UpdatedAt.Local().Format("2 Jan 2006 at 15:04")))
	}
	return false, nil
}

func checkpointSetupDraft(prompt promptSession, path, target string, stage setupDraftStage, cfg config.Config, scanMode setupScanMode, modelReady bool) bool {
	if path == "" {
		return false
	}
	if err := writeSetupDraft(path, target, stage, cfg, scanMode, modelReady, time.Now()); err != nil {
		_ = prompt.status(setupStatusReview, "Setup recovery checkpoint could not be saved: "+err.Error())
		return false
	}
	return true
}
