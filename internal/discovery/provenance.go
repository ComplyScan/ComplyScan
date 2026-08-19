package discovery

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// GitProvenance identifies the exact repository state represented by a scan
// without retaining changed file names or repository contents.
type GitProvenance struct {
	Commit        string
	Branch        string
	Dirty         bool
	TargetPath    string
	BaseReference string
	BaseCommit    string
}

func InspectGitProvenance(ctx context.Context, target, baseReference string) (GitProvenance, error) {
	targetRoot, err := filepath.Abs(target)
	if err != nil {
		return GitProvenance{}, fmt.Errorf("resolve target %q: %w", target, err)
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(targetRoot); evaluateErr == nil {
		targetRoot = evaluated
	}
	rootOutput, err := runGit(ctx, targetRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return GitProvenance{}, err
	}
	gitRoot := strings.TrimSpace(string(rootOutput))
	if evaluated, evaluateErr := filepath.EvalSymlinks(gitRoot); evaluateErr == nil {
		gitRoot = evaluated
	}
	targetPath, err := filepath.Rel(gitRoot, targetRoot)
	if err != nil || targetPath == ".." || strings.HasPrefix(targetPath, ".."+string(filepath.Separator)) {
		return GitProvenance{}, fmt.Errorf("target %q is outside Git repository %q", targetRoot, gitRoot)
	}
	targetPath = filepath.ToSlash(targetPath)

	commitOutput, err := runGit(ctx, gitRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return GitProvenance{}, err
	}
	commit := strings.TrimSpace(string(commitOutput))
	if !validObjectID(commit) {
		return GitProvenance{}, errorsUnexpectedGitObject("HEAD", commit)
	}
	branch := ""
	if branchOutput, branchErr := runGit(ctx, gitRoot, "symbolic-ref", "--short", "-q", "HEAD"); branchErr == nil {
		branch = strings.TrimSpace(string(branchOutput))
	}
	statusOutput, err := runGit(ctx, gitRoot, "status", "--porcelain=v1", "--untracked-files=normal", "--", targetPath)
	if err != nil {
		return GitProvenance{}, err
	}

	result := GitProvenance{
		Commit: commit, Branch: branch, Dirty: len(statusOutput) > 0, TargetPath: targetPath,
		BaseReference: strings.TrimSpace(baseReference),
	}
	if result.BaseReference != "" {
		if strings.HasPrefix(result.BaseReference, "-") || strings.ContainsAny(result.BaseReference, "\x00\r\n") {
			return GitProvenance{}, fmt.Errorf("invalid Git reference %q", result.BaseReference)
		}
		baseOutput, baseErr := runGit(ctx, gitRoot, "rev-parse", "--verify", result.BaseReference+"^{commit}")
		if baseErr != nil {
			return GitProvenance{}, baseErr
		}
		result.BaseCommit = strings.TrimSpace(string(baseOutput))
		if !validObjectID(result.BaseCommit) {
			return GitProvenance{}, errorsUnexpectedGitObject(result.BaseReference, result.BaseCommit)
		}
	}
	return result, nil
}

func errorsUnexpectedGitObject(reference, value string) error {
	return fmt.Errorf("resolve Git reference %q: unexpected object ID %q", reference, value)
}
