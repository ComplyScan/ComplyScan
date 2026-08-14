package discovery

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	ignore "github.com/sabhiram/go-gitignore"
)

const DefaultMaxFileSize int64 = 1 << 20 // 1 MiB

const (
	DefaultMaxFiles      = 25_000
	DefaultMaxTotalBytes = int64(100 << 20) // 100 MiB
	progressInterval     = 500
)

type FileKind string

const (
	KindSource        FileKind = "source"
	KindManifest      FileKind = "dependency-manifest"
	KindDockerfile    FileKind = "dockerfile"
	KindGitHubAction  FileKind = "github-actions"
	KindCI            FileKind = "ci"
	KindTerraform     FileKind = "terraform"
	KindEnvTemplate   FileKind = "environment-template"
	KindReadme        FileKind = "readme"
	KindDocumentation FileKind = "documentation"
	KindModelCard     FileKind = "model-card"
	KindPrivacy       FileKind = "privacy-documentation"
	KindRisk          FileKind = "risk-assessment"
	KindAIGovernance  FileKind = "ai-governance"
	KindConfig        FileKind = "configuration"
	KindOtherText     FileKind = "text"
)

type File struct {
	Path    string
	Kind    FileKind
	Size    int64
	Content []byte
}

type Repository struct {
	Root  string
	Files []File
}

type Options struct {
	Exclude                   []string
	ExcludeFiles              []string
	MaxFileSize               int64
	MaxFiles                  int
	MaxTotalBytes             int64
	IncludeNestedRepositories bool
	TrackedOnly               bool
	OnProgress                ProgressHandler
}

type Result struct {
	Repository Repository
	Warnings   []string
	Stats      Stats
	Limited    bool
}

type Stats struct {
	DirectoriesVisited int   `json:"directories_visited"`
	FilesRead          int   `json:"files_read"`
	BytesRead          int64 `json:"bytes_read"`
}

type Progress struct {
	Stats Stats
	Path  string
	Done  bool
}

type ProgressHandler func(Progress) error

// ApplyExclusions filters an existing discovery snapshot with the same path
// rules used during filesystem traversal. This keeps reused setup snapshots
// inside the scan's final privacy and scope boundary.
func ApplyExclusions(repository Repository, exclusions, excludedFiles []string) Repository {
	if len(exclusions) == 0 && len(excludedFiles) == 0 {
		return repository
	}
	filtered := Repository{Root: repository.Root, Files: make([]File, 0, len(repository.Files))}
	for _, file := range repository.Files {
		if isExcluded(file.Path, filepath.Base(filepath.FromSlash(file.Path)), exclusions) || isExcludedFile(file.Path, excludedFiles) {
			continue
		}
		filtered.Files = append(filtered.Files, file)
	}
	return filtered
}

type gitignoreContext struct {
	base    string
	matcher *ignore.GitIgnore
}

var alwaysIgnoredDirectories = map[string]struct{}{
	".git": {}, ".hg": {}, ".svn": {},
	"node_modules": {}, "vendor": {}, ".venv": {}, "venv": {},
	"dist": {}, "build": {}, "target": {}, "out": {}, "bin": {}, "coverage": {}, ".next": {},
	".cache": {}, ".gradle": {}, ".terraform": {}, ".pytest_cache": {}, "__pycache__": {},
}

var binaryExtensions = map[string]struct{}{
	".7z": {}, ".a": {}, ".avi": {}, ".bin": {}, ".bmp": {}, ".class": {},
	".dll": {}, ".dylib": {}, ".eot": {}, ".exe": {}, ".gif": {}, ".gz": {},
	".ico": {}, ".jar": {}, ".jpeg": {}, ".jpg": {}, ".mov": {}, ".mp3": {},
	".mp4": {}, ".o": {}, ".otf": {}, ".pdf": {}, ".png": {}, ".pyc": {},
	".so": {}, ".tar": {}, ".ttf": {}, ".wasm": {}, ".webm": {}, ".webp": {},
	".woff": {}, ".woff2": {}, ".xz": {}, ".zip": {},
}

func Discover(ctx context.Context, target string, options Options) (Result, error) {
	root, err := filepath.Abs(target)
	if err != nil {
		return Result{}, fmt.Errorf("resolve target %q: %w", target, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect target %q: %w", target, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("target %q is not a directory", target)
	}
	if options.MaxFileSize <= 0 {
		options.MaxFileSize = DefaultMaxFileSize
	}
	if options.MaxFiles <= 0 {
		options.MaxFiles = DefaultMaxFiles
	}
	if options.MaxTotalBytes <= 0 {
		options.MaxTotalBytes = DefaultMaxTotalBytes
	}

	result := Result{Repository: Repository{Root: root}}
	if options.TrackedOnly {
		err = discoverTracked(ctx, root, options, &result)
	} else {
		err = walk(ctx, root, "", nil, options, &result)
	}
	if err != nil {
		return Result{}, err
	}
	if options.OnProgress != nil {
		if err := options.OnProgress(Progress{Stats: result.Stats, Done: true}); err != nil {
			return Result{}, fmt.Errorf("report discovery progress: %w", err)
		}
	}
	return result, nil
}

func walk(ctx context.Context, root, relDir string, parents []gitignoreContext, options Options, result *Result) error {
	if result.Limited {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result.Stats.DirectoriesVisited++
	absDir := filepath.Join(root, filepath.FromSlash(relDir))
	contexts := parents
	ignorePath := filepath.Join(absDir, ".gitignore")
	if data, err := os.ReadFile(ignorePath); err == nil {
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		contexts = append(append([]gitignoreContext(nil), parents...), gitignoreContext{
			base: relDir, matcher: ignore.CompileIgnoreLines(lines...),
		})
	} else if !os.IsNotExist(err) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not read %s: %v", displayPath(relDir, ".gitignore"), err))
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsPermission(err) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("permission denied: %s", displayPath(relDir, "")))
			return nil
		}
		return fmt.Errorf("read directory %q: %w", absDir, err)
	}

	for _, entry := range entries {
		if result.Limited {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		relPath := filepath.ToSlash(filepath.Join(relDir, name))
		if entry.Type()&fs.ModeSymlink != 0 {
			continue
		}
		if entry.IsDir() {
			if shouldSkipDirectory(name, relPath, options.Exclude) || isIgnored(relPath, true, contexts) {
				continue
			}
			if !options.IncludeNestedRepositories && isRepositoryRoot(filepath.Join(root, filepath.FromSlash(relPath))) {
				result.Warnings = append(result.Warnings, "skipped nested repository: "+relPath)
				continue
			}
			if err := walk(ctx, root, relPath, contexts, options, result); err != nil {
				return err
			}
			continue
		}
		if isIgnored(relPath, false, contexts) || isExcluded(relPath, name, options.Exclude) || isExcludedFile(relPath, options.ExcludeFiles) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not inspect %s: %v", relPath, err))
			continue
		}
		if err := processFile(root, relPath, info, options, result); err != nil {
			return err
		}
	}
	return nil
}

func processFile(root, relPath string, info fs.FileInfo, options Options, result *Result) error {
	if !info.Mode().IsRegular() || info.Size() > options.MaxFileSize {
		return nil
	}
	if _, binary := binaryExtensions[strings.ToLower(filepath.Ext(relPath))]; binary {
		return nil
	}
	if result.Stats.FilesRead >= options.MaxFiles {
		markLimited(result, fmt.Sprintf("file limit reached (%d); remaining files were not scanned", options.MaxFiles))
		return nil
	}
	if result.Stats.BytesRead+info.Size() > options.MaxTotalBytes {
		markLimited(result, fmt.Sprintf("content limit reached (%d bytes); remaining files were not scanned", options.MaxTotalBytes))
		return nil
	}

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not read %s: %v", relPath, err))
		return nil
	}
	if isBinary(content) {
		return nil
	}
	result.Repository.Files = append(result.Repository.Files, File{
		Path: relPath, Kind: Classify(relPath), Size: info.Size(), Content: content,
	})
	result.Stats.FilesRead++
	result.Stats.BytesRead += int64(len(content))
	if options.OnProgress != nil && result.Stats.FilesRead%progressInterval == 0 {
		if err := options.OnProgress(Progress{Stats: result.Stats, Path: relPath}); err != nil {
			return fmt.Errorf("report discovery progress: %w", err)
		}
	}
	return nil
}

func markLimited(result *Result, warning string) {
	if result.Limited {
		return
	}
	result.Limited = true
	result.Warnings = append(result.Warnings, warning)
}

func isRepositoryRoot(path string) bool {
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

func isIgnored(relPath string, directory bool, contexts []gitignoreContext) bool {
	ignored := false
	for _, context := range contexts {
		subPath, err := filepath.Rel(filepath.FromSlash(context.base), filepath.FromSlash(relPath))
		if err != nil || strings.HasPrefix(subPath, "..") {
			continue
		}
		subPath = filepath.ToSlash(subPath)
		matched := context.matcher.MatchesPath(subPath)
		if directory && !matched {
			matched = context.matcher.MatchesPath(subPath + "/")
		}
		if matched {
			ignored = true
		}
	}
	return ignored
}

func shouldSkipDirectory(name, relPath string, excludes []string) bool {
	if _, ok := alwaysIgnoredDirectories[strings.ToLower(name)]; ok {
		return true
	}
	return isExcluded(relPath, name, excludes)
}

func isExcluded(relPath, name string, excludes []string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, raw := range excludes {
		pattern := strings.Trim(strings.TrimSpace(filepath.ToSlash(raw)), "/")
		if pattern == "" {
			continue
		}
		if !strings.Contains(pattern, "/") && strings.EqualFold(pattern, name) {
			return true
		}
		if matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(relPath)); matched {
			return true
		}
		if relPath == pattern || strings.HasPrefix(relPath, pattern+"/") {
			return true
		}
	}
	return false
}

func isExcludedFile(relPath string, excludedFiles []string) bool {
	relPath = filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	for _, value := range excludedFiles {
		candidate := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
		if candidate != "." && relPath == candidate {
			return true
		}
	}
	return false
}

func isBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	probe := content
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	return bytes.IndexByte(probe, 0) >= 0 || !utf8.Valid(probe)
}

func displayPath(relDir, name string) string {
	path := filepath.ToSlash(filepath.Join(relDir, name))
	if path == "" {
		return "."
	}
	return path
}
