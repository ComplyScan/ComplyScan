package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExecuteUsesReadOnlyNetworkDisabledContainerWithoutShell(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, name string, arguments ...string) commandResult {
		calls = append(calls, append([]string{name}, arguments...))
		if len(calls) == 1 {
			return commandResult{exitCode: 0}
		}
		return commandResult{output: "PASS", exitCode: 0}
	}
	report, err := execute(context.Background(), Options{
		RecipeID: "go-tests", Target: t.TempDir(), Runtime: "docker", Image: "golang:local", Command: "go",
		Arguments: []string{"test", "./..."}, Objectives: []string{"eu-aia-15-robustness-failure-handling"}, Timeout: time.Minute,
	}, func(name string) (string, error) { return "/usr/bin/" + name, nil }, run)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusPassed || report.ExitCode != 0 || len(calls) != 2 {
		t.Fatalf("unexpected result: report=%#v calls=%#v", report, calls)
	}
	joined := strings.Join(calls[1], " ")
	for _, required := range []string{"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges", "readonly", "GOCACHE=/tmp/go-build", "golang:local go test ./..."} {
		if !strings.Contains(joined, required) {
			t.Errorf("container invocation missing %q: %s", required, joined)
		}
	}
	if strings.Contains(joined, " sh ") || strings.Contains(joined, "bash") {
		t.Fatalf("verification unexpectedly used a shell: %s", joined)
	}
}

func TestExecuteRecordsTestFailureWithoutTreatingItAsInfrastructureError(t *testing.T) {
	calls := 0
	report, err := execute(context.Background(), Options{
		RecipeID: "python-tests", Target: t.TempDir(), Runtime: "podman", Image: "test:local", Command: "pytest",
		Objectives: []string{"objective"}, Timeout: time.Minute,
	}, func(string) (string, error) { return "/usr/bin/podman", nil }, func(_ context.Context, _ string, _ ...string) commandResult {
		calls++
		if calls == 1 {
			return commandResult{exitCode: 0}
		}
		return commandResult{output: "1 failed", exitCode: 1, err: errors.New("exit status 1")}
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusFailed || report.ExitCode != 1 || report.Output != "1 failed" {
		t.Fatalf("unexpected failure report: %#v", report)
	}
}

func TestExecuteRequiresPreloadedImage(t *testing.T) {
	_, err := execute(context.Background(), Options{
		RecipeID: "missing-test", Target: t.TempDir(), Runtime: "docker", Image: "missing:local", Command: "go",
		Objectives: []string{"objective"}, Timeout: time.Minute,
	}, func(string) (string, error) { return "/usr/bin/docker", nil }, func(_ context.Context, _ string, _ ...string) commandResult {
		return commandResult{output: "not found", exitCode: 1, err: errors.New("exit status 1")}
	})
	if err == nil || !strings.Contains(err.Error(), "not available locally") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBoundedWriterCapsOutput(t *testing.T) {
	writer := &boundedWriter{remaining: 4}
	_, _ = writer.Write([]byte("abcdef"))
	if value := writer.String(); !strings.HasPrefix(value, "abcd") || !strings.Contains(value, "truncated") {
		t.Fatalf("unexpected bounded output %q", value)
	}
}
