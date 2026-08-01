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
	Exclude     []string
	MaxFileSize int64
}

type Result struct {
	Repository Repository
	Warnings   []string
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

	result := Result{Repository: Repository{Root: root}}
	if err := walk(ctx, root, "", nil, options, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func walk(ctx context.Context, root, relDir string, parents []gitignoreContext, options Options, result *Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
			if err := walk(ctx, root, relPath, contexts, options, result); err != nil {
				return err
			}
			continue
		}
		if isIgnored(relPath, false, contexts) || isExcluded(relPath, name, options.Exclude) {
			continue
		}
		if _, binary := binaryExtensions[strings.ToLower(filepath.Ext(name))]; binary {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not inspect %s: %v", relPath, err))
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > options.MaxFileSize {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not read %s: %v", relPath, err))
			continue
		}
		if isBinary(content) {
			continue
		}
		result.Repository.Files = append(result.Repository.Files, File{
			Path: relPath, Kind: Classify(relPath), Size: info.Size(), Content: content,
		})
	}
	return nil
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
