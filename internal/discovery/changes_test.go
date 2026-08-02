package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedPathsIncludesCommittedWorkingTreeAndUntrackedFiles(t *testing.T) {
	root := initialiseGitRepository(t)
	writeChangeFixture(t, root, "root.txt", "initial\n")
	writeChangeFixture(t, root, "service/initial.txt", "initial\n")
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "initial")
	base := strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))

	writeChangeFixture(t, root, "service/committed.txt", "committed\n")
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "committed change")
	writeChangeFixture(t, root, "service/initial.txt", "modified\n")
	writeChangeFixture(t, root, "service/staged.txt", "staged\n")
	runTestGit(t, root, "add", "service/staged.txt")
	writeChangeFixture(t, root, "service/untracked.txt", "untracked\n")
	writeChangeFixture(t, root, "outside.txt", "outside target\n")

	paths, err := ChangedPaths(context.Background(), filepath.Join(root, "service"), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"committed.txt", "initial.txt", "staged.txt", "untracked.txt"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("changed paths missing %q: %#v", want, paths)
		}
	}
	if _, ok := paths["outside.txt"]; ok {
		t.Fatalf("path outside target was included: %#v", paths)
	}
}

func TestChangedPathsRejectsInvalidReference(t *testing.T) {
	root := initialiseGitRepository(t)
	if _, err := ChangedPaths(context.Background(), root, "--help"); err == nil {
		t.Fatal("expected invalid reference error")
	}
}

func initialiseGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runTestGit(t, root, "init", "-q")
	runTestGit(t, root, "config", "user.name", "ComplyScan tests")
	runTestGit(t, root, "config", "user.email", "tests@example.invalid")
	return root
}

func writeChangeFixture(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTestGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return string(output)
}
