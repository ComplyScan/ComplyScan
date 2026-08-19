package discovery

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectGitProvenanceCapturesCommitTargetAndDirtyState(t *testing.T) {
	repository := t.TempDir()
	runGitTestCommand(t, repository, "init", "-q")
	runGitTestCommand(t, repository, "config", "user.email", "test@example.com")
	runGitTestCommand(t, repository, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(repository, "service"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, "service", "app.go")
	if err := os.WriteFile(path, []byte("package service\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTestCommand(t, repository, "add", "service/app.go")
	runGitTestCommand(t, repository, "commit", "-qm", "initial")
	expectedCommit := runGitTestCommand(t, repository, "rev-parse", "HEAD")

	provenance, err := InspectGitProvenance(context.Background(), filepath.Join(repository, "service"), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if provenance.Commit+"\n" != expectedCommit || provenance.BaseCommit != provenance.Commit || provenance.TargetPath != "service" || provenance.Dirty {
		t.Fatalf("clean provenance = %#v; commit output=%q", provenance, expectedCommit)
	}

	if err := os.WriteFile(path, []byte("package service\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provenance, err = InspectGitProvenance(context.Background(), filepath.Join(repository, "service"), "")
	if err != nil {
		t.Fatal(err)
	}
	if !provenance.Dirty {
		t.Fatalf("dirty provenance = %#v", provenance)
	}
}

func runGitTestCommand(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return string(output)
}
