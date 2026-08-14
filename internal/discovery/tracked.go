package discovery

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func discoverTracked(ctx context.Context, root string, options Options, result *Result) error {
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "-z")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("list tracked files (is %q a Git repository?): %w", root, err)
	}
	result.Stats.DirectoriesVisited = 1
	for _, rawPath := range bytes.Split(output, []byte{0}) {
		if result.Limited {
			break
		}
		if len(rawPath) == 0 {
			continue
		}
		relPath := filepath.ToSlash(string(rawPath))
		if !safeRelativePath(relPath) || isExcludedTrackedPath(relPath, options.Exclude) || isExcludedFile(relPath, options.ExcludeFiles) {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relPath)))
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not inspect %s: %v", relPath, err))
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
			continue
		}
		if err := processFile(root, relPath, info, options, result); err != nil {
			return err
		}
	}
	return nil
}

func safeRelativePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean != "." && clean == path && !filepath.IsAbs(filepath.FromSlash(path)) &&
		path != ".." && !strings.HasPrefix(path, "../")
}

func isExcludedTrackedPath(path string, excludes []string) bool {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		partial := strings.Join(parts[:index+1], "/")
		if index < len(parts)-1 && shouldSkipDirectory(part, partial, excludes) {
			return true
		}
	}
	return isExcluded(path, filepath.Base(filepath.FromSlash(path)), excludes)
}
