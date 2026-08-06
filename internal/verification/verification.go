// Package verification runs explicitly requested repository checks inside a
// constrained, network-disabled container and records their bounded result.
package verification

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ComplyScan/ComplyScan/internal/rules"
)

const maxOutputBytes = 16 << 10

type Status string

const (
	StatusPassed Status = "passed"
	StatusFailed Status = "failed"
)

type Options struct {
	Target     string
	Runtime    string
	Image      string
	Command    string
	Arguments  []string
	Objectives []string
	Timeout    time.Duration
}

type Report struct {
	Status       Status   `json:"status"`
	Runtime      string   `json:"runtime"`
	Image        string   `json:"image"`
	Command      []string `json:"command"`
	Objectives   []string `json:"objectives"`
	ExitCode     int      `json:"exit_code"`
	DurationMS   int64    `json:"duration_ms"`
	OutputDigest string   `json:"output_digest"`
	Output       string   `json:"output,omitempty"`
	Boundary     string   `json:"boundary"`
}

type commandResult struct {
	output   string
	exitCode int
	err      error
}

type commandExecutor func(context.Context, string, ...string) commandResult

// Execute runs the configured command without a shell in a pre-existing local
// Docker or Podman image. A non-zero test exit is a report result, not an
// execution error; infrastructure failures are returned as errors.
func Execute(ctx context.Context, options Options) (Report, error) {
	return execute(ctx, options, exec.LookPath, executeBoundedCommand)
}

func execute(ctx context.Context, options Options, lookPath func(string) (string, error), run commandExecutor) (Report, error) {
	options.Runtime = strings.TrimSpace(options.Runtime)
	options.Image = strings.TrimSpace(options.Image)
	options.Command = strings.TrimSpace(options.Command)
	if options.Runtime != "docker" && options.Runtime != "podman" {
		return Report{}, errors.New("verification runtime must be docker or podman")
	}
	if options.Image == "" || options.Command == "" || len(options.Objectives) == 0 {
		return Report{}, errors.New("verification requires an image, command, and at least one technical objective")
	}
	if strings.ContainsAny(options.Image, "\r\n\x00") || strings.HasPrefix(options.Image, "-") {
		return Report{}, errors.New("verification image is invalid")
	}
	if options.Timeout <= 0 || options.Timeout > 30*time.Minute {
		return Report{}, errors.New("verification timeout must be greater than zero and at most 30 minutes")
	}
	target, err := filepath.Abs(options.Target)
	if err != nil {
		return Report{}, fmt.Errorf("resolve verification target: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}
	if strings.Contains(target, ",") {
		return Report{}, errors.New("verification target paths containing commas are not supported by the container mount")
	}
	runtimePath, err := lookPath(options.Runtime)
	if err != nil {
		return Report{}, fmt.Errorf("find %s: %w", options.Runtime, err)
	}
	inspectContext, cancelInspect := context.WithTimeout(ctx, 15*time.Second)
	inspect := run(inspectContext, runtimePath, "image", "inspect", options.Image)
	cancelInspect()
	if inspect.err != nil || inspect.exitCode != 0 {
		return Report{}, fmt.Errorf("verification image %q is not available locally; pull it explicitly before scanning", options.Image)
	}

	containerArgs := []string{
		"run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges", "--pids-limit", "256", "--memory", "1g", "--cpus", "1",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=512m",
		"--mount", "type=bind,source=" + target + ",target=/workspace,readonly", "--workdir", "/workspace",
		options.Image, options.Command,
	}
	containerArgs = append(containerArgs, options.Arguments...)
	started := time.Now()
	runContext, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	result := run(runContext, runtimePath, containerArgs...)
	duration := time.Since(started)
	if errors.Is(runContext.Err(), context.DeadlineExceeded) {
		return Report{}, fmt.Errorf("isolated verification exceeded %s", options.Timeout)
	}
	if result.err != nil && result.exitCode < 0 {
		return Report{}, fmt.Errorf("run isolated verification: %w", result.err)
	}
	status := StatusPassed
	if result.exitCode != 0 {
		status = StatusFailed
	}
	outputDigest := sha256.Sum256([]byte(result.output))
	command := append([]string{options.Command}, options.Arguments...)
	for index := range command {
		command[index] = rules.RedactSecrets(command[index])
	}
	return Report{
		Status: status, Runtime: options.Runtime, Image: options.Image, Command: command,
		Objectives: append([]string(nil), options.Objectives...), ExitCode: result.exitCode,
		DurationMS: duration.Milliseconds(), OutputDigest: fmt.Sprintf("%x", outputDigest),
		Output:   strings.TrimSpace(rules.RedactSecrets(result.output)),
		Boundary: "The command ran in a read-only, network-disabled container. A passing result supports only the user-declared objective association and does not establish compliance or operational effectiveness.",
	}, nil
}

func executeBoundedCommand(ctx context.Context, name string, arguments ...string) commandResult {
	writer := &boundedWriter{remaining: maxOutputBytes}
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdout = writer
	command.Stderr = writer
	err := command.Run()
	exitCode := 0
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return commandResult{output: writer.String(), exitCode: exitCode, err: err}
}

type boundedWriter struct {
	data      []byte
	remaining int
	truncated bool
}

func (writer *boundedWriter) Write(value []byte) (int, error) {
	length := len(value)
	if writer.remaining > 0 {
		keep := length
		if keep > writer.remaining {
			keep = writer.remaining
		}
		writer.data = append(writer.data, value[:keep]...)
		writer.remaining -= keep
		writer.truncated = keep < length
	} else if length > 0 {
		writer.truncated = true
	}
	return length, nil
}

func (writer *boundedWriter) String() string {
	value := writer.data
	for len(value) > 0 && !utf8.Valid(value) {
		value = value[:len(value)-1]
	}
	result := string(value)
	if writer.truncated {
		result += "\n… output truncated by ComplyScan"
	}
	return result
}
