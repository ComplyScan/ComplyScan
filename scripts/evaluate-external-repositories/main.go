package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/framework"
)

const externalSourceSchemaVersion = 1

var (
	sourceIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	revisionPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	githubURLPattern = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+\.git$`)
)

type sourceCatalog struct {
	SchemaVersion int                `json:"schema_version"`
	ReviewedAt    string             `json:"reviewed_at"`
	Repositories  []sourceRepository `json:"repositories"`
}

type sourceRepository struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Revision    string `json:"revision"`
	License     string `json:"license"`
	LicenseFile string `json:"license_file"`
}

func main() {
	os.Exit(run())
}

func run() int {
	manifestPath := flag.String("manifest", "testdata/technical-evaluation/external/manifest.json", "path to the source-free external benchmark manifest")
	sourcesPath := flag.String("sources", "testdata/technical-evaluation/external/sources.json", "path to pinned repository provenance")
	workspace := flag.String("workspace", "", "reuse an existing directory containing the pinned repositories")
	format := flag.String("format", "text", "output format: text or json")
	flag.Parse()
	if flag.NArg() != 0 || (*format != "text" && *format != "json") {
		fmt.Fprintln(os.Stderr, "Usage: evaluate-external-repositories [--workspace DIRECTORY] [--format text|json]")
		return 2
	}

	manifest, err := framework.LoadBenchmarkManifest(*manifestPath)
	if err != nil {
		return operationalError(err)
	}
	catalog, err := loadSourceCatalog(*sourcesPath)
	if err != nil {
		return operationalError(err)
	}
	if err := validateCatalogAgainstManifest(catalog, manifest); err != nil {
		return operationalError(err)
	}

	benchmarkWorkspace := *workspace
	removeWorkspace := false
	if benchmarkWorkspace == "" {
		benchmarkWorkspace, err = os.MkdirTemp("", "complyscan-external-evaluation.*")
		if err != nil {
			return operationalError(fmt.Errorf("create external benchmark workspace: %w", err))
		}
		removeWorkspace = true
		defer os.RemoveAll(benchmarkWorkspace)
	}
	benchmarkWorkspace, err = filepath.Abs(benchmarkWorkspace)
	if err != nil {
		return operationalError(fmt.Errorf("resolve external benchmark workspace: %w", err))
	}

	ctx := context.Background()
	for _, source := range catalog.Repositories {
		repositoryPath := filepath.Join(benchmarkWorkspace, source.ID)
		if removeWorkspace {
			fmt.Fprintf(os.Stderr, "Fetching %s at %s...\n", source.Name, source.Revision[:12])
			if err := clonePinnedRepository(ctx, repositoryPath, source); err != nil {
				return operationalError(err)
			}
		}
		if err := verifyPinnedRepository(ctx, repositoryPath, source); err != nil {
			return operationalError(err)
		}
	}

	pack, err := framework.LoadBuiltin(manifest.PackID)
	if err != nil {
		return operationalError(err)
	}
	report, err := framework.RunBenchmarkInWorkspace(ctx, benchmarkWorkspace, manifest, pack)
	if err != nil {
		return operationalError(err)
	}
	if *format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return operationalError(fmt.Errorf("write JSON result: %w", err))
		}
	} else if err := framework.WriteBenchmarkSummary(os.Stdout, report); err != nil {
		return operationalError(fmt.Errorf("write benchmark summary: %w", err))
	}
	if !report.Passed {
		return 1
	}
	return 0
}

func operationalError(err error) int {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return 2
}

func loadSourceCatalog(path string) (sourceCatalog, error) {
	file, err := os.Open(path)
	if err != nil {
		return sourceCatalog{}, fmt.Errorf("open external source catalog: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var catalog sourceCatalog
	if err := decoder.Decode(&catalog); err != nil {
		return sourceCatalog{}, fmt.Errorf("parse external source catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return sourceCatalog{}, fmt.Errorf("parse external source catalog: expected one JSON value")
	}
	if err := catalog.validate(); err != nil {
		return sourceCatalog{}, fmt.Errorf("validate external source catalog: %w", err)
	}
	return catalog, nil
}

func (catalog sourceCatalog) validate() error {
	if catalog.SchemaVersion != externalSourceSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", catalog.SchemaVersion)
	}
	if strings.TrimSpace(catalog.ReviewedAt) == "" || len(catalog.Repositories) == 0 {
		return fmt.Errorf("reviewed_at and repositories must not be empty")
	}
	seen := make(map[string]struct{}, len(catalog.Repositories))
	for index, source := range catalog.Repositories {
		if !sourceIDPattern.MatchString(source.ID) {
			return fmt.Errorf("repositories[%d].id %q is invalid", index, source.ID)
		}
		if _, exists := seen[source.ID]; exists {
			return fmt.Errorf("repositories[%d].id %q is duplicated", index, source.ID)
		}
		seen[source.ID] = struct{}{}
		if strings.TrimSpace(source.Name) == "" || !githubURLPattern.MatchString(source.URL) || !revisionPattern.MatchString(source.Revision) {
			return fmt.Errorf("repositories[%d] must have a name, HTTPS GitHub URL, and full lowercase commit SHA", index)
		}
		if source.License != "MIT" {
			return fmt.Errorf("repositories[%d].license must be MIT", index)
		}
		if source.LicenseFile == "" || filepath.IsAbs(source.LicenseFile) || filepath.Clean(source.LicenseFile) != source.LicenseFile || strings.HasPrefix(source.LicenseFile, "..") {
			return fmt.Errorf("repositories[%d].license_file must be a safe relative path", index)
		}
	}
	return nil
}

func validateCatalogAgainstManifest(catalog sourceCatalog, manifest framework.BenchmarkManifest) error {
	cases := make(map[string]string, len(manifest.Cases))
	for _, benchmarkCase := range manifest.Cases {
		cases[benchmarkCase.ID] = benchmarkCase.Path
	}
	for _, source := range catalog.Repositories {
		path, exists := cases[source.ID]
		if !exists || path != source.ID {
			return fmt.Errorf("source %s must have a manifest case whose path equals its ID", source.ID)
		}
		delete(cases, source.ID)
	}
	if len(cases) != 0 {
		return fmt.Errorf("manifest contains a case without pinned source provenance")
	}
	return nil
}

func clonePinnedRepository(ctx context.Context, path string, source sourceRepository) error {
	if err := runGit(ctx, "init", "--quiet", path); err != nil {
		return fmt.Errorf("initialize %s: %w", source.ID, err)
	}
	if err := runGit(ctx, "-C", path, "remote", "add", "origin", source.URL); err != nil {
		return fmt.Errorf("configure %s origin: %w", source.ID, err)
	}
	if err := runGit(ctx, "-C", path, "fetch", "--quiet", "--depth=1", "origin", source.Revision); err != nil {
		return fmt.Errorf("fetch %s at %s: %w", source.ID, source.Revision, err)
	}
	if err := runGit(ctx, "-C", path, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("check out %s: %w", source.ID, err)
	}
	return nil
}

func verifyPinnedRepository(ctx context.Context, path string, source sourceRepository) error {
	revision, err := gitOutput(ctx, "-C", path, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("verify %s revision: %w", source.ID, err)
	}
	if revision != source.Revision {
		return fmt.Errorf("verify %s revision: got %s, want %s", source.ID, revision, source.Revision)
	}
	if _, err := os.Stat(filepath.Join(path, filepath.FromSlash(source.LicenseFile))); err != nil {
		return fmt.Errorf("verify %s licence file: %w", source.ID, err)
	}
	return nil
}

func runGit(ctx context.Context, arguments ...string) error {
	_, err := gitOutput(ctx, arguments...)
	return err
}

func gitOutput(ctx context.Context, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
