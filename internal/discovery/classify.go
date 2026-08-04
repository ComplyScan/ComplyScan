package discovery

import (
	"path/filepath"
	"strings"
)

var sourceExtensions = map[string]struct{}{
	".c": {}, ".cc": {}, ".cpp": {}, ".cs": {}, ".go": {}, ".h": {}, ".hpp": {},
	".java": {}, ".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {}, ".kt": {}, ".kts": {}, ".php": {},
	".py": {}, ".rb": {}, ".rs": {}, ".scala": {}, ".sh": {}, ".swift": {},
	".ts": {}, ".tsx": {}, ".mts": {}, ".cts": {},
}

var manifests = map[string]struct{}{
	"cargo.toml": {}, "composer.json": {}, "gemfile": {}, "go.mod": {}, "go.sum": {},
	"package-lock.json": {}, "package.json": {}, "pnpm-lock.yaml": {}, "poetry.lock": {},
	"pom.xml": {}, "pyproject.toml": {}, "requirements.txt": {}, "uv.lock": {},
	"yarn.lock": {},
}

func Classify(path string) FileKind {
	normalized := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(normalized))
	ext := strings.ToLower(filepath.Ext(base))

	if _, ok := manifests[base]; ok || strings.HasPrefix(base, "requirements-") && ext == ".txt" {
		return KindManifest
	}
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
		return KindDockerfile
	}
	if strings.HasPrefix(normalized, ".github/workflows/") && (ext == ".yml" || ext == ".yaml") {
		return KindGitHubAction
	}
	if base == ".gitlab-ci.yml" || base == ".travis.yml" || base == "azure-pipelines.yml" || base == "jenkinsfile" ||
		strings.Contains(normalized, "/.circleci/") || strings.HasPrefix(normalized, ".circleci/") ||
		strings.Contains(normalized, "/.buildkite/") || strings.HasPrefix(normalized, ".buildkite/") {
		return KindCI
	}
	if ext == ".tf" || ext == ".tfvars" {
		return KindTerraform
	}
	if base == ".env.example" || base == ".env.template" || strings.HasSuffix(base, ".env.example") || strings.HasSuffix(base, ".env.template") {
		return KindEnvTemplate
	}
	if strings.HasPrefix(base, "readme") {
		return KindReadme
	}
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(base)
	if strings.Contains(compact, "modelcard") {
		return KindModelCard
	}
	if strings.Contains(compact, "privacy") || strings.Contains(compact, "dataprotection") {
		return KindPrivacy
	}
	if strings.Contains(compact, "riskassessment") || strings.HasPrefix(compact, "airisk") {
		return KindRisk
	}
	if strings.Contains(compact, "aigovernance") || strings.Contains(compact, "aisystem") {
		return KindAIGovernance
	}
	if _, ok := sourceExtensions[ext]; ok {
		return KindSource
	}
	if ext == ".md" || ext == ".mdx" || ext == ".rst" || ext == ".adoc" {
		return KindDocumentation
	}
	if ext == ".json" || ext == ".toml" || ext == ".yaml" || ext == ".yml" || ext == ".xml" || ext == ".ini" {
		return KindConfig
	}
	return KindOtherText
}
