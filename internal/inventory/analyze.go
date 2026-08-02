package inventory

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/1eonardodawinki/ComplyScan/internal/discovery"
)

const IgnoreSignalsMarker = "complyscan:ignore-ai-signals"

var (
	pythonImportPattern = regexp.MustCompile(`^\s*(?:from|import)\s+([A-Za-z0-9_.-]+)`)
	jsImportPattern     = regexp.MustCompile(`(?:from\s+|require\s*\(\s*|import\s*(?:\(\s*)?)["']([^"']+)["']`)
	javaImportPattern   = regexp.MustCompile(`^\s*import\s+(?:static\s+)?([A-Za-z0-9_.]+)`)
)

type dependencyRef struct {
	Ecosystem string
	Name      string
	Version   string
	Line      int
	Scope     Scope
}

type importRef struct {
	Ecosystem string
	Name      string
	Line      int
}

// Analyze returns deterministic, typed technical signals. Plain provider names
// are intentionally insufficient evidence.
func Analyze(repo discovery.Repository) []Signal {
	var signals []Signal
	seen := make(map[string]struct{})
	add := func(signal Signal) {
		key := strings.Join([]string{signal.Name, string(signal.EvidenceType), signal.Path, signal.Package, strconv.Itoa(signal.Line)}, "\x00")
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		signals = append(signals, signal)
	}

	for _, file := range repo.Files {
		if !technicalFile(file.Kind) || strings.Contains(string(file.Content), IgnoreSignalsMarker) {
			continue
		}
		for _, dependency := range manifestDependencies(file) {
			if definition, packageName := matchPackage(dependency.Ecosystem, dependency.Name); definition != nil {
				add(Signal{
					Name: definition.Name, Kind: definition.Kind, EvidenceType: EvidenceDependency,
					Scope: dependency.Scope, Confidence: "high", Path: file.Path, Line: dependency.Line,
					Package: packageName, Version: dependency.Version,
					Evidence: dependencyEvidence(packageName, dependency.Version),
				})
			}
		}
		for _, imported := range sourceImports(file) {
			if definition, packageName := matchPackage(imported.Ecosystem, imported.Name); definition != nil {
				add(Signal{
					Name: definition.Name, Kind: definition.Kind, EvidenceType: EvidenceImport,
					Scope: scopeForFile(file), Confidence: "high", Path: file.Path, Line: imported.Line,
					Package: packageName, Evidence: "import " + packageName,
				})
			}
		}
		for lineIndex, line := range splitLines(file.Content) {
			lowerLine := strings.ToLower(line)
			for definitionIndex := range componentDefinitions {
				definition := &componentDefinitions[definitionIndex]
				for _, endpoint := range definition.Endpoints {
					if strings.Contains(lowerLine, strings.ToLower(endpoint)) {
						add(Signal{
							Name: definition.Name, Kind: definition.Kind, EvidenceType: EvidenceEndpoint,
							Scope: scopeForFile(file), Confidence: "high", Path: file.Path, Line: lineIndex + 1,
							Evidence: "endpoint " + endpoint,
						})
					}
				}
				for _, variable := range definition.EnvVars {
					if hasEnvironmentReference(line, variable) {
						add(Signal{
							Name: definition.Name, Kind: definition.Kind, EvidenceType: EvidenceEnvironment,
							Scope: scopeForFile(file), Confidence: "medium", Path: file.Path, Line: lineIndex + 1,
							Evidence: "environment variable " + variable,
						})
					}
				}
			}
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		left, right := signals[i], signals[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if scopeRank(left.Scope) != scopeRank(right.Scope) {
			return scopeRank(left.Scope) < scopeRank(right.Scope)
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.EvidenceType < right.EvidenceType
	})
	return signals
}

func manifestDependencies(file discovery.File) []dependencyRef {
	base := strings.ToLower(filepath.Base(file.Path))
	switch {
	case base == "package.json":
		return nodeDependencies(file)
	case base == "go.mod":
		return goDependencies(file)
	case base == "pyproject.toml" || base == "poetry.lock" || base == "uv.lock" || base == "pipfile" ||
		strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt"):
		return lineDependencies(file, "python")
	default:
		return nil
	}
}

func nodeDependencies(file discovery.File) []dependencyRef {
	var manifest struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		PeerDependencies     map[string]string `json:"peerDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if json.Unmarshal(file.Content, &manifest) != nil {
		return nil
	}
	var refs []dependencyRef
	appendMap := func(values map[string]string, scope Scope) {
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			refs = append(refs, dependencyRef{Ecosystem: "node", Name: name, Version: values[name], Line: findLine(file.Content, name), Scope: scope})
		}
	}
	appendMap(manifest.Dependencies, ScopeConfig)
	appendMap(manifest.PeerDependencies, ScopeConfig)
	appendMap(manifest.OptionalDependencies, ScopeConfig)
	appendMap(manifest.DevDependencies, ScopeTest)
	return refs
}

func goDependencies(file discovery.File) []dependencyRef {
	var refs []dependencyRef
	for index, line := range splitLines(file.Content) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 || fields[0] == "module" || fields[0] == "go" || fields[0] == "toolchain" || fields[0] == "require" && len(fields) == 1 {
			continue
		}
		if fields[0] == "require" {
			fields = fields[1:]
		}
		if len(fields) == 0 || !strings.Contains(fields[0], ".") {
			continue
		}
		version := ""
		if len(fields) > 1 {
			version = fields[1]
		}
		refs = append(refs, dependencyRef{Ecosystem: "go", Name: fields[0], Version: version, Line: index + 1, Scope: ScopeConfig})
	}
	return refs
}

func lineDependencies(file discovery.File, ecosystem string) []dependencyRef {
	var refs []dependencyRef
	for index, line := range splitLines(file.Content) {
		trimmed := strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		for definitionIndex := range componentDefinitions {
			for _, candidate := range componentDefinitions[definitionIndex].Packages[ecosystem] {
				if version, ok := dependencyDeclaration(trimmed, candidate); ok {
					refs = append(refs, dependencyRef{Ecosystem: ecosystem, Name: candidate, Version: version, Line: index + 1, Scope: ScopeConfig})
				}
			}
		}
	}
	return refs
}

func dependencyDeclaration(line, name string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	line = strings.TrimLeft(line, `"'`)
	if len(line) < len(name) || !strings.EqualFold(line[:len(name)], name) {
		return "", false
	}
	rest := line[len(name):]
	if rest != "" && isPackageNameByte(rest[0]) {
		return "", false
	}
	rest = strings.TrimSpace(rest)
	rest = strings.TrimLeft(rest, `"'`)
	rest = strings.TrimSpace(rest)
	if rest == "" || strings.HasPrefix(rest, ",") || strings.HasPrefix(rest, "]") || strings.HasPrefix(rest, "}") {
		return "", true
	}
	rest = strings.TrimLeft(rest, ":=<>=~!^@ ")
	rest = strings.TrimLeft(rest, `"'`)
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", true
	}
	version := strings.TrimRight(fields[0], `"',}]`)
	return version, true
}

func isPackageNameByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' || strings.ContainsRune("._-/@", rune(value))
}

func sourceImports(file discovery.File) []importRef {
	extension := strings.ToLower(filepath.Ext(file.Path))
	switch extension {
	case ".py":
		return regexImports(file.Content, "python", pythonImportPattern)
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return regexImports(file.Content, "node", jsImportPattern)
	case ".java", ".kt", ".kts":
		return regexImports(file.Content, "java", javaImportPattern)
	case ".go":
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, file.Path, file.Content, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		refs := make([]importRef, 0, len(parsed.Imports))
		for _, imported := range parsed.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err == nil {
				refs = append(refs, importRef{Ecosystem: "go", Name: value, Line: fileSet.Position(imported.Pos()).Line})
			}
		}
		return refs
	default:
		return nil
	}
}

func regexImports(content []byte, ecosystem string, pattern *regexp.Regexp) []importRef {
	var refs []importRef
	for index, line := range splitLines(content) {
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			refs = append(refs, importRef{Ecosystem: ecosystem, Name: match[1], Line: index + 1})
		}
	}
	return refs
}

func matchPackage(ecosystem, value string) (*componentDefinition, string) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for index := range componentDefinitions {
		definition := &componentDefinitions[index]
		for _, candidate := range definition.Packages[ecosystem] {
			candidate = strings.ToLower(candidate)
			if normalized == candidate || strings.HasPrefix(normalized, candidate+"/") ||
				strings.HasPrefix(normalized, candidate+".") || strings.HasPrefix(normalized, candidate+"_") {
				return definition, value
			}
		}
	}
	return nil, ""
}

func hasEnvironmentReference(line, variable string) bool {
	upper := strings.ToUpper(line)
	variable = strings.ToUpper(variable)
	trimmed := strings.TrimSpace(upper)
	trimmed = strings.TrimPrefix(trimmed, "EXPORT ")
	if strings.HasPrefix(trimmed, variable+"=") || strings.HasPrefix(trimmed, variable+" =") {
		return true
	}
	for _, prefix := range []string{"PROCESS.ENV.", "OS.GETENV(\"", "OS.GETENV('", "OS.ENVIRON[\"", "OS.ENVIRON['", "GETENV(\"", "GETENV('", "ENV::VAR(\"", "ENV::VAR('"} {
		if strings.Contains(upper, prefix+variable) {
			return true
		}
	}
	return strings.Contains(upper, "${"+variable+"}")
}

func scopeForFile(file discovery.File) Scope {
	path := strings.ToLower(filepath.ToSlash(file.Path))
	base := strings.ToLower(filepath.Base(path))
	if strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") || strings.Contains(path, "/testdata/") ||
		strings.Contains(path, "/fixtures/") || strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") ||
		strings.HasPrefix(path, "testdata/") || strings.HasPrefix(path, "fixtures/") || strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
		return ScopeTest
	}
	if file.Kind != discovery.KindSource {
		return ScopeConfig
	}
	return ScopeRuntime
}

func technicalFile(kind discovery.FileKind) bool {
	switch kind {
	case discovery.KindReadme, discovery.KindDocumentation, discovery.KindModelCard,
		discovery.KindPrivacy, discovery.KindRisk, discovery.KindAIGovernance:
		return false
	default:
		return true
	}
}

func dependencyEvidence(name, version string) string {
	if version == "" {
		return "dependency " + name
	}
	return fmt.Sprintf("dependency %s %s", name, version)
}

func findLine(content []byte, value string) int {
	for index, line := range splitLines(content) {
		if strings.Contains(line, value) {
			return index + 1
		}
	}
	return 1
}

func splitLines(content []byte) []string {
	return strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
}

func scopeRank(scope Scope) int {
	switch scope {
	case ScopeRuntime:
		return 0
	case ScopeConfig:
		return 1
	default:
		return 2
	}
}
