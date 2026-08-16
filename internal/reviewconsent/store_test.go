package reviewconsent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ComplyScan/ComplyScan/internal/config"
)

func TestConsentMatchesExactIdentityAndAISettings(t *testing.T) {
	repository := t.TempDir()
	configPath := filepath.Join(repository, config.FileName)
	settings := configuredTestAI()
	if err := config.Write(configPath, configuredTestConfig(settings), false); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(t.TempDir(), "review-consent"))
	if authorized, err := store.Authorized(repository, configPath, settings); err != nil || authorized {
		t.Fatalf("authorization before grant = %v, %v", authorized, err)
	}
	if err := store.Grant(repository, configPath, settings); err != nil {
		t.Fatal(err)
	}
	if authorized, err := store.Authorized(repository, configPath, settings); err != nil || !authorized {
		t.Fatalf("authorization after grant = %v, %v", authorized, err)
	}
	directoryInfo, err := os.Stat(store.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("consent directory permissions = %o", directoryInfo.Mode().Perm())
	}
	files, err := os.ReadDir(store.Directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("consent records = %d, %v", len(files), err)
	}
	fileInfo, err := files[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("consent record permissions = %o", fileInfo.Mode().Perm())
	}

	changed := settings
	changed.Remote.Model = "different-model"
	if authorized, err := store.Authorized(repository, configPath, changed); err != nil || authorized {
		t.Fatalf("authorization after model change = %v, %v", authorized, err)
	}
	changed = settings
	changed.Remote.BaseURL = "https://different.example/v1"
	if authorized, err := store.Authorized(repository, configPath, changed); err != nil || authorized {
		t.Fatalf("authorization after destination change = %v, %v", authorized, err)
	}
	changed = settings
	changed.Remote.APIKeyEnv = "DIFFERENT_API_KEY"
	if authorized, err := store.Authorized(repository, configPath, changed); err != nil || authorized {
		t.Fatalf("authorization after credential source change = %v, %v", authorized, err)
	}
	changed = settings
	changed.RepositoryAnalysis.Mode = "deep"
	if authorized, err := store.Authorized(repository, configPath, changed); err != nil || authorized {
		t.Fatalf("authorization after strategy change = %v, %v", authorized, err)
	}

	if err := store.Revoke(repository, configPath); err != nil {
		t.Fatal(err)
	}
	if authorized, err := store.Authorized(repository, configPath, settings); err != nil || authorized {
		t.Fatalf("authorization after revoke = %v, %v", authorized, err)
	}
}

func TestConsentStoreRejectsUnsafeOrNonStrictRecords(t *testing.T) {
	repository := t.TempDir()
	configPath := filepath.Join(repository, config.FileName)
	settings := configuredTestAI()
	if err := config.Write(configPath, configuredTestConfig(settings), false); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(t.TempDir(), "review-consent"))
	if err := store.Grant(repository, configPath, settings); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(store.Directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("consent records = %d, %v", len(files), err)
	}
	recordPath := filepath.Join(store.Directory, files[0].Name())
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "\n}", ",\n  \"unexpected\": true\n}", 1))
	if err := os.WriteFile(recordPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if authorized, err := store.Authorized(repository, configPath, settings); err == nil || authorized {
		t.Fatalf("unknown-field record = %v, %v", authorized, err)
	}

	if err := os.Remove(recordPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, recordPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if authorized, err := store.Authorized(repository, configPath, settings); err == nil || authorized {
		t.Fatalf("symlink record = %v, %v", authorized, err)
	}
}

func TestConsentStoreRejectsSymlinkDirectory(t *testing.T) {
	repository := t.TempDir()
	configPath := filepath.Join(repository, config.FileName)
	settings := configuredTestAI()
	if err := config.Write(configPath, configuredTestConfig(settings), false); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	realDirectory := t.TempDir()
	link := filepath.Join(root, "review-consent")
	if err := os.Symlink(realDirectory, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := NewStore(link)
	if err := store.Grant(repository, configPath, settings); err == nil {
		t.Fatal("grant unexpectedly accepted a symlink consent directory")
	}
}

func configuredTestAI() config.AIConfig {
	settings := config.Default().AI
	settings.Provider = "openai-compatible"
	settings.ReviewOnScan = true
	settings.Remote = config.RemoteConfig{
		ProviderName: "Private gateway", BaseURL: "https://models.example/v1", Model: "review-model",
		APIKeyEnv: "REVIEW_API_KEY", TimeoutSeconds: 120, MaxFindings: 10,
	}
	return settings
}

func configuredTestConfig(settings config.AIConfig) config.Config {
	cfg := config.Default()
	cfg.AI = settings
	return cfg
}
