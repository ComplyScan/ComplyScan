package discovery

import (
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ChangedPaths returns repository-relative files changed since ref, including
// committed changes, staged and unstaged changes, and untracked files.
func ChangedPaths(ctx context.Context, target, ref string) (map[string]struct{}, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
		return nil, fmt.Errorf("invalid Git reference %q", ref)
	}
	targetRoot, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolve target %q: %w", target, err)
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(targetRoot); evaluateErr == nil {
		targetRoot = evaluated
	}
	gitRootOutput, err := runGit(ctx, targetRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("locate Git repository for %q: %w", target, err)
	}
	gitRoot := strings.TrimSpace(string(gitRootOutput))
	if evaluated, evaluateErr := filepath.EvalSymlinks(gitRoot); evaluateErr == nil {
		gitRoot = evaluated
	}
	targetPrefix, err := filepath.Rel(gitRoot, targetRoot)
	if err != nil || targetPrefix == ".." || strings.HasPrefix(targetPrefix, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("target %q is outside Git repository %q", targetRoot, gitRoot)
	}
	targetPrefix = filepath.ToSlash(targetPrefix)

	resolvedOutput, err := runGit(ctx, gitRoot, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("resolve Git reference %q: %w", ref, err)
	}
	resolved := strings.TrimSpace(string(resolvedOutput))
	if !validObjectID(resolved) {
		return nil, fmt.Errorf("resolve Git reference %q: unexpected object ID", ref)
	}

	paths := make(map[string]struct{})
	commands := [][]string{
		{"diff", "--name-only", "--diff-filter=ACMR", "-z", resolved + "...HEAD", "--"},
		{"diff", "--name-only", "--diff-filter=ACMR", "-z", "HEAD", "--"},
		{"ls-files", "--others", "--exclude-standard", "-z", "--"},
	}
	for _, arguments := range commands {
		output, err := runGit(ctx, gitRoot, arguments...)
		if err != nil {
			return nil, fmt.Errorf("list files changed since %q: %w", ref, err)
		}
		for _, path := range splitNull(output) {
			if relative, ok := pathWithinTarget(path, targetPrefix); ok {
				paths[relative] = struct{}{}
			}
		}
	}
	return paths, nil
}

func runGit(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		message := strings.TrimSpace(string(exitError.Stderr))
		if message != "" {
			return nil, fmt.Errorf("git %s: %s", arguments[0], message)
		}
	}
	return nil, fmt.Errorf("git %s: %w", arguments[0], err)
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func splitNull(value []byte) []string {
	parts := strings.Split(string(value), "\x00")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			paths = append(paths, filepath.ToSlash(filepath.Clean(filepath.FromSlash(part))))
		}
	}
	return paths
}

func pathWithinTarget(path, targetPrefix string) (string, bool) {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") {
		return "", false
	}
	if targetPrefix == "." {
		return path, true
	}
	prefix := strings.TrimSuffix(targetPrefix, "/") + "/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}
