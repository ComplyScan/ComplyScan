package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiscoverRespectsGitignoreAndBuiltInExclusions(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".gitignore", "ignored.py\nsecrets/\n")
	writeTestFile(t, root, "main.py", "print('ok')\n")
	writeTestFile(t, root, "ignored.py", "print('ignored')\n")
	writeTestFile(t, root, "secrets/key.txt", "secret\n")
	writeTestFile(t, root, "node_modules/pkg/index.js", "console.log('ignored')\n")
	writeTestFile(t, root, "nested/.gitignore", "local.py\n")
	writeTestFile(t, root, "nested/local.py", "print('ignored')\n")
	writeTestFile(t, root, "nested/kept.py", "print('kept')\n")

	result, err := Discover(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	paths := repositoryPaths(result.Repository)
	for _, unwanted := range []string{"ignored.py", "secrets/key.txt", "node_modules/pkg/index.js", "nested/local.py"} {
		if contains(paths, unwanted) {
			t.Fatalf("discovered ignored file %q in %v", unwanted, paths)
		}
	}
	for _, wanted := range []string{"main.py", "nested/kept.py"} {
		if !contains(paths, wanted) {
			t.Fatalf("did not discover %q in %v", wanted, paths)
		}
	}
}

func TestDiscoverSkipsNestedRepositoriesByDefault(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "main.py", "print('root')\n")
	writeTestFile(t, root, "nested/.git/HEAD", "ref: refs/heads/main\n")
	writeTestFile(t, root, "nested/app.py", "print('nested')\n")

	result, err := Discover(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if contains(repositoryPaths(result.Repository), "nested/app.py") {
		t.Fatal("nested repository should be skipped by default")
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "nested repository") {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}

	result, err = Discover(context.Background(), root, Options{IncludeNestedRepositories: true})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(repositoryPaths(result.Repository), "nested/app.py") {
		t.Fatal("nested repository should be included when requested")
	}
}

func TestDiscoverEnforcesBudgetsAndReportsProgress(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "aaaa\n")
	writeTestFile(t, root, "b.txt", "bbbb\n")
	var finalProgress Progress
	result, err := Discover(context.Background(), root, Options{
		MaxFiles: 1,
		OnProgress: func(progress Progress) error {
			if progress.Done {
				finalProgress = progress
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Limited || result.Stats.FilesRead != 1 || finalProgress.Stats.FilesRead != 1 {
		t.Fatalf("unexpected budget result: %#v, progress=%#v", result, finalProgress)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "file limit") {
		t.Fatalf("unexpected warnings: %#v", result.Warnings)
	}
}

func TestDiscoverTrackedOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	writeTestFile(t, root, "tracked.py", "print('tracked')\n")
	writeTestFile(t, root, "untracked.py", "print('untracked')\n")
	for _, args := range [][]string{{"init"}, {"add", "tracked.py"}} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}

	result, err := Discover(context.Background(), root, Options{TrackedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	paths := repositoryPaths(result.Repository)
	if len(paths) != 1 || paths[0] != "tracked.py" {
		t.Fatalf("tracked-only paths = %v", paths)
	}
}

func TestDiscoverExcludesBinaryAndLargeFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "text.txt", "hello\n")
	if err := os.WriteFile(filepath.Join(root, "binary.dat"), []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "large.txt", "0123456789")

	result, err := Discover(context.Background(), root, Options{MaxFileSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	paths := repositoryPaths(result.Repository)
	if len(paths) != 1 || paths[0] != "text.txt" {
		t.Fatalf("got paths %v, want only text.txt", paths)
	}
}

func TestClassifyRelevantFiles(t *testing.T) {
	tests := map[string]FileKind{
		"main.go":                   KindSource,
		"package.json":              KindManifest,
		"Dockerfile.production":     KindDockerfile,
		".github/workflows/ci.yaml": KindGitHubAction,
		"infra/main.tf":             KindTerraform,
		".env.example":              KindEnvTemplate,
		"README.md":                 KindReadme,
		"docs/model-card.md":        KindModelCard,
		"docs/privacy-policy.md":    KindPrivacy,
		"docs/risk-assessment.md":   KindRisk,
		"docs/AI_SYSTEM.md":         KindAIGovernance,
	}
	for path, want := range tests {
		if got := Classify(path); got != want {
			t.Errorf("Classify(%q) = %q, want %q", path, got, want)
		}
	}
}

func writeTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	absPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repositoryPaths(repo Repository) []string {
	paths := make([]string, 0, len(repo.Files))
	for _, file := range repo.Files {
		paths = append(paths, file.Path)
	}
	sort.Strings(paths)
	return paths
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
