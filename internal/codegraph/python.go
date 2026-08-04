package codegraph

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

var (
	pythonDefinitionPattern    = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pythonClassPattern         = regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	pythonCallPattern          = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_]*)*)\s*\(`)
	pythonMainGuardPattern     = regexp.MustCompile(`^if\s+__name__\s*==\s*['"]__main__['"]\s*:`)
	pythonHTTPMethodPattern    = regexp.MustCompile(`(?i)['"](GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD)['"]`)
	pythonDependsPattern       = regexp.MustCompile(`\bDepends\s*\(\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)
	pythonDjangoRoutePattern   = regexp.MustCompile(`\b(path|re_path)\s*\(\s*['"]([^'"]+)['"]\s*,\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)
	pythonEnvironPattern       = regexp.MustCompile(`\bos\s*\.\s*environ\s*\[\s*['"]([^'"]+)['"]`)
	pythonEnvironAccessPattern = regexp.MustCompile(`\bos\s*\.\s*environ\s*\[`)
)

type pythonDecorator struct {
	expression string
	line       int
}

type pythonScope struct {
	kind          SymbolKind
	indent        int
	qualifiedName string
}

type pythonFunction struct {
	symbolID           string
	name               string
	qualifiedName      string
	classQualifiedName string
	path               string
	startLine          int
	endLine            int
	indent             int
	decorators         []pythonDecorator
}

type parsedPythonFile struct {
	repositoryFile discovery.File
	module         string
	lines          []string
	maskedLines    []string
	imports        []Import
	aliases        map[string]string
	symbols        []Symbol
	functions      []pythonFunction
}

func parsePythonFile(repositoryFile discovery.File) (parsedPythonFile, error) {
	content := strings.ReplaceAll(string(repositoryFile.Content), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	maskedLines, err := maskPythonLines(lines)
	if err != nil {
		return parsedPythonFile{}, err
	}
	parsed := parsedPythonFile{
		repositoryFile: repositoryFile,
		module:         pythonModuleName(repositoryFile.Path),
		lines:          lines,
		maskedLines:    maskedLines,
		aliases:        make(map[string]string),
	}
	scopes := make([]pythonScope, 0)
	pendingDecorators := make(map[int][]pythonDecorator)
	mainGuard := false

	for index, masked := range maskedLines {
		trimmed := strings.TrimSpace(masked)
		if trimmed == "" {
			continue
		}
		originalTrimmed := strings.TrimSpace(lines[index])
		if pythonMainGuardPattern.MatchString(originalTrimmed) {
			mainGuard = true
		}
		indent := pythonIndentWidth(lines[index])
		for len(scopes) > 0 && indent <= scopes[len(scopes)-1].indent {
			scopes = scopes[:len(scopes)-1]
		}
		if strings.HasPrefix(originalTrimmed, "@") {
			pendingDecorators[indent] = append(pendingDecorators[indent], pythonDecorator{
				expression: strings.TrimSpace(strings.TrimPrefix(originalTrimmed, "@")),
				line:       index + 1,
			})
			continue
		}

		if imported, ok := parsePythonImport(originalTrimmed, parsed.module); ok {
			for index := range imported.imports {
				imported.imports[index].Path = repositoryFile.Path
			}
			parsed.imports = append(parsed.imports, imported.imports...)
			for alias, target := range imported.aliases {
				parsed.aliases[alias] = target
			}
		}

		if strings.HasPrefix(trimmed, "class ") {
			delete(pendingDecorators, indent)
			match := pythonClassPattern.FindStringSubmatch(trimmed)
			if len(match) != 2 || !pythonDeclarationComplete(maskedLines, index) {
				return parsedPythonFile{}, fmt.Errorf("unsupported class declaration at line %d", index+1)
			}
			qualified := qualifyPythonName(parsed.module, scopes, match[1])
			endLine := pythonBlockEnd(lines, maskedLines, index, indent)
			parsed.symbols = append(parsed.symbols, pythonSymbol(repositoryFile, parsed.module, match[1], qualified, SymbolType, index+1, endLine, false))
			scopes = append(scopes, pythonScope{kind: SymbolType, indent: indent, qualifiedName: qualified})
			continue
		}

		if strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "async def ") {
			match := pythonDefinitionPattern.FindStringSubmatch(trimmed)
			if len(match) != 2 || !pythonDeclarationComplete(maskedLines, index) {
				return parsedPythonFile{}, fmt.Errorf("unsupported function declaration at line %d", index+1)
			}
			name := match[1]
			qualified := qualifyPythonName(parsed.module, scopes, name)
			kind := SymbolFunction
			classQualified := ""
			if len(scopes) > 0 && scopes[len(scopes)-1].kind == SymbolType {
				kind = SymbolMethod
				classQualified = scopes[len(scopes)-1].qualifiedName
			}
			if pythonTestFunction(repositoryFile.Path, name) {
				kind = SymbolTest
			}
			endLine := pythonBlockEnd(lines, maskedLines, index, indent)
			entryPoint := mainGuard && name == "main" && len(scopes) == 0
			decorators := append([]pythonDecorator(nil), pendingDecorators[indent]...)
			symbol := pythonSymbol(repositoryFile, parsed.module, name, qualified, kind, index+1, endLine, entryPoint)
			if len(decorators) > 0 {
				symbol.source = pythonSourceExcerpt(repositoryFile.Content, decorators[0].line, endLine)
			}
			parsed.symbols = append(parsed.symbols, symbol)
			parsed.functions = append(parsed.functions, pythonFunction{
				symbolID: symbol.ID, name: name, qualifiedName: qualified,
				classQualifiedName: classQualified, path: repositoryFile.Path,
				startLine: index + 1, endLine: endLine, indent: indent,
				decorators: decorators,
			})
			delete(pendingDecorators, indent)
			scopes = append(scopes, pythonScope{kind: kind, indent: indent, qualifiedName: qualified})
			continue
		}
		delete(pendingDecorators, indent)
	}

	if mainGuard {
		for index := range parsed.symbols {
			if parsed.symbols[index].Name == "main" && parsed.symbols[index].QualifiedName == parsed.module+".main" {
				parsed.symbols[index].EntryPoint = true
			}
		}
	}
	return parsed, nil
}

func pythonSymbol(repositoryFile discovery.File, module, name, qualified string, kind SymbolKind, startLine, endLine int, entryPoint bool) Symbol {
	return Symbol{
		ID:            fmt.Sprintf("python:%s:%d:%s", filepath.ToSlash(repositoryFile.Path), startLine, qualified),
		Name:          name,
		QualifiedName: qualified,
		Kind:          kind,
		Language:      LanguagePython,
		Package:       module,
		Path:          repositoryFile.Path,
		StartLine:     startLine,
		EndLine:       endLine,
		Exported:      !strings.HasPrefix(name, "_") && kind != SymbolTest,
		EntryPoint:    entryPoint,
		source:        pythonSourceExcerpt(repositoryFile.Content, startLine, endLine),
	}
}

func indexPythonFile(graph *Graph, file parsedPythonFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	for _, function := range file.functions {
		indexPythonDecorators(graph, function, file, qualifiedNames, globalNames)
		for lineNumber := function.startLine; lineNumber <= function.endLine && lineNumber <= len(file.maskedLines); lineNumber++ {
			if owner, ok := innermostPythonFunction(file.functions, lineNumber); !ok || owner.symbolID != function.symbolID {
				continue
			}
			line := file.maskedLines[lineNumber-1]
			originalLine := file.lines[lineNumber-1]
			for _, match := range pythonEnvironPattern.FindAllStringSubmatch(originalLine, -1) {
				if !pythonEnvironAccessPattern.MatchString(line) {
					continue
				}
				if len(match) == 2 {
					graph.Edges = append(graph.Edges, Edge{
						Kind: EdgeConfiguration, From: function.symbolID, To: "config:" + match[1], Label: match[1],
						Path: function.path, Line: lineNumber, Resolved: false,
					})
				}
			}
			for _, match := range pythonCallPattern.FindAllStringSubmatch(line, -1) {
				if len(match) != 2 {
					continue
				}
				callName := strings.ReplaceAll(match[1], " ", "")
				shortName := pythonLastName(callName)
				if shortName == "" || pythonCallKeyword(shortName) || (lineNumber == function.startLine && shortName == function.name) {
					continue
				}
				target, resolved := resolvePythonCall(callName, function, file, qualifiedNames, globalNames)
				kind := classifyPythonCall(callName)
				label := callName
				if kind == EdgeConfiguration {
					if key, ok := pythonFirstStringArgument(originalLine, callName); ok {
						target = "config:" + key
						label = key
						resolved = false
					}
				}
				if kind == EdgeCall {
					if symbol, ok := graphSymbolByID(graph.Symbols, function.symbolID); ok && symbol.Kind == SymbolTest {
						kind = EdgeTest
					}
				}
				graph.Edges = append(graph.Edges, Edge{
					Kind: kind, From: function.symbolID, To: target, Label: label,
					Path: function.path, Line: lineNumber, Resolved: resolved,
				})
			}
		}
	}
	indexPythonModuleRoutes(graph, file, qualifiedNames, globalNames)
}

func indexPythonDecorators(graph *Graph, function pythonFunction, file parsedPythonFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	for _, decorator := range function.decorators {
		for _, label := range pythonRouteLabels(decorator.expression) {
			markGraphEntryPoint(graph, function.symbolID)
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeRoute, From: "framework-route:" + label, To: function.symbolID, Label: label,
				Path: function.path, Line: decorator.line, Resolved: true,
			})
		}
		decoratorName := pythonDecoratorName(decorator.expression)
		if pythonAuthorizationName(decoratorName) {
			target, resolved := resolvePythonCall(decoratorName, function, file, qualifiedNames, globalNames)
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeAuthorization, From: function.symbolID, To: target, Label: decoratorName,
				Path: function.path, Line: decorator.line, Resolved: resolved,
			})
		}
		indexPythonDependencies(graph, function, file, decorator.expression, decorator.line, qualifiedNames, globalNames)
	}
	if function.startLine > 0 && function.startLine <= len(file.lines) {
		indexPythonDependencies(graph, function, file, file.lines[function.startLine-1], function.startLine, qualifiedNames, globalNames)
	}
}

func indexPythonDependencies(graph *Graph, function pythonFunction, file parsedPythonFile, source string, line int, qualifiedNames map[string]string, globalNames map[string][]string) {
	for _, match := range pythonDependsPattern.FindAllStringSubmatch(source, -1) {
		if len(match) != 2 || !pythonAuthorizationName(match[1]) {
			continue
		}
		target, resolved := resolvePythonCall(match[1], function, file, qualifiedNames, globalNames)
		graph.Edges = append(graph.Edges, Edge{
			Kind: EdgeAuthorization, From: function.symbolID, To: target, Label: "Depends(" + match[1] + ")",
			Path: function.path, Line: line, Resolved: resolved,
		})
	}
}

func indexPythonModuleRoutes(graph *Graph, file parsedPythonFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	for index, line := range file.lines {
		lineNumber := index + 1
		if _, insideFunction := innermostPythonFunction(file.functions, lineNumber); insideFunction {
			continue
		}
		masked := file.maskedLines[index]
		if !strings.Contains(masked, "path") {
			continue
		}
		for _, match := range pythonDjangoRoutePattern.FindAllStringSubmatch(line, -1) {
			if len(match) != 4 {
				continue
			}
			dummy := pythonFunction{path: file.repositoryFile.Path}
			target, resolved := resolvePythonCall(match[3], dummy, file, qualifiedNames, globalNames)
			if resolved {
				markGraphEntryPoint(graph, target)
			}
			label := "ANY /" + strings.TrimPrefix(match[2], "/")
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeRoute, From: "django-route:" + label, To: target, Label: label,
				Path: file.repositoryFile.Path, Line: lineNumber, Resolved: resolved,
			})
		}
	}
}

func resolvePythonCall(callName string, function pythonFunction, file parsedPythonFile, qualifiedNames map[string]string, globalNames map[string][]string) (string, bool) {
	parts := strings.Split(callName, ".")
	shortName := parts[len(parts)-1]
	candidates := make([]string, 0, 4)
	if len(parts) == 1 {
		candidates = append(candidates, file.module+"."+shortName)
		if target, ok := file.aliases[shortName]; ok {
			candidates = append(candidates, target)
		}
	} else {
		if (parts[0] == "self" || parts[0] == "cls") && function.classQualifiedName != "" {
			candidates = append(candidates, function.classQualifiedName+"."+shortName)
		}
		if target, ok := file.aliases[parts[0]]; ok {
			candidates = append(candidates, target+"."+strings.Join(parts[1:], "."))
		}
		candidates = append(candidates, callName)
	}
	for _, candidate := range candidates {
		if target, ok := qualifiedNames[candidate]; ok {
			return target, true
		}
	}
	if candidates := globalNames[shortName]; len(candidates) == 1 {
		return candidates[0], true
	}
	return "unresolved:" + callName, false
}

func classifyPythonCall(callName string) EdgeKind {
	lower := strings.ToLower(callName)
	shortName := strings.ToLower(pythonLastName(callName))
	receiver := strings.TrimSuffix(lower, "."+shortName)
	if shortName == "getenv" || (shortName == "get" && pythonConfigurationReceiver(receiver)) || strings.Contains(shortName, "feature_enabled") || strings.Contains(shortName, "featureenabled") {
		return EdgeConfiguration
	}
	if pythonAuthorizationName(callName) {
		return EdgeAuthorization
	}
	if pythonLoggingCall(receiver, shortName) {
		return EdgeLogging
	}
	if pythonPersistenceCall(receiver, shortName) {
		return EdgePersistence
	}
	return classifyCall(shortName)
}

func pythonConfigurationReceiver(receiver string) bool {
	for _, marker := range []string{"config", "settings", "environ", "environment", "feature_flag", "featureflag", "flags"} {
		if strings.Contains(receiver, marker) {
			return true
		}
	}
	return false
}

func pythonAuthorizationName(value string) bool {
	lower := strings.ToLower(pythonLastName(value))
	for _, marker := range []string{"authoriz", "authenticat", "permission", "require_role", "requires_role", "role_required", "login_required", "access_control", "access_check", "reviewer"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return lower == "has_access" || lower == "check_access" || lower == "can_override" || lower == "current_user"
}

func pythonLoggingCall(receiver, shortName string) bool {
	if strings.Contains(shortName, "audit") || strings.Contains(shortName, "telemetry") || strings.Contains(shortName, "record_event") {
		return true
	}
	loggingReceiver := receiver == "log" || strings.Contains(receiver, "logger") || strings.Contains(receiver, "logging") || strings.Contains(receiver, "audit") || strings.Contains(receiver, "telemetry")
	if !loggingReceiver {
		return false
	}
	switch shortName {
	case "debug", "info", "warning", "warn", "error", "exception", "critical", "log", "event", "bind":
		return true
	default:
		return false
	}
}

func pythonPersistenceCall(receiver, shortName string) bool {
	switch shortName {
	case "save", "save_all", "commit", "flush", "add", "add_all", "create", "bulk_create", "delete", "execute", "executemany", "merge", "upsert", "insert", "update", "persist", "store":
		return true
	}
	for _, marker := range []string{"session", "database", "repository", "store", "collection", "table", "queryset"} {
		if strings.Contains(receiver, marker) && (strings.HasPrefix(shortName, "write") || strings.HasPrefix(shortName, "set")) {
			return true
		}
	}
	return false
}

func pythonRouteLabels(expression string) []string {
	name := strings.ToLower(pythonDecoratorName(expression))
	shortName := pythonLastName(name)
	path, ok := pythonFirstQuotedValue(expression)
	if !ok {
		path = "<dynamic>"
	}
	if path != "<dynamic>" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	switch shortName {
	case "get", "post", "put", "patch", "delete", "options", "head", "websocket":
		return []string{strings.ToUpper(shortName) + " " + path}
	case "route", "api_route":
		methods := pythonHTTPMethodPattern.FindAllStringSubmatch(expression, -1)
		if len(methods) == 0 {
			return []string{"GET " + path}
		}
		labels := make([]string, 0, len(methods))
		seen := make(map[string]bool)
		for _, method := range methods {
			if len(method) != 2 {
				continue
			}
			label := strings.ToUpper(method[1]) + " " + path
			if !seen[label] {
				labels = append(labels, label)
				seen[label] = true
			}
		}
		return labels
	default:
		return nil
	}
}

func pythonDecoratorName(expression string) string {
	name := expression
	if index := strings.Index(name, "("); index >= 0 {
		name = name[:index]
	}
	return strings.ReplaceAll(strings.TrimSpace(name), " ", "")
}

func pythonFirstStringArgument(source, callName string) (string, bool) {
	shortName := pythonLastName(callName)
	index := strings.Index(source, shortName)
	if index < 0 {
		return "", false
	}
	return pythonFirstQuotedValue(source[index+len(shortName):])
}

func pythonFirstQuotedValue(source string) (string, bool) {
	open := strings.Index(source, "(")
	if open < 0 {
		return "", false
	}
	for index := open + 1; index < len(source); index++ {
		quote := source[index]
		if quote != '\'' && quote != '"' {
			if source[index] == ')' {
				return "", false
			}
			continue
		}
		var builder strings.Builder
		for index++; index < len(source); index++ {
			if source[index] == '\\' && index+1 < len(source) {
				index++
				builder.WriteByte(source[index])
				continue
			}
			if source[index] == quote {
				return builder.String(), true
			}
			builder.WriteByte(source[index])
		}
		return "", false
	}
	return "", false
}

func markGraphEntryPoint(graph *Graph, symbolID string) {
	for index := range graph.Symbols {
		if graph.Symbols[index].ID == symbolID {
			graph.Symbols[index].EntryPoint = true
			return
		}
	}
}

func innermostPythonFunction(functions []pythonFunction, line int) (pythonFunction, bool) {
	var selected pythonFunction
	found := false
	for _, function := range functions {
		if line < function.startLine || line > function.endLine {
			continue
		}
		if !found || function.indent > selected.indent || (function.indent == selected.indent && function.startLine > selected.startLine) {
			selected = function
			found = true
		}
	}
	return selected, found
}

type pythonImportResult struct {
	imports []Import
	aliases map[string]string
}

func parsePythonImport(line, module string) (pythonImportResult, bool) {
	result := pythonImportResult{aliases: make(map[string]string)}
	if strings.HasPrefix(line, "import ") {
		for _, item := range strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "import ")), ",") {
			fields := strings.Fields(strings.TrimSpace(item))
			if len(fields) == 0 || !pythonDottedName(fields[0]) {
				continue
			}
			alias := strings.Split(fields[0], ".")[0]
			if len(fields) == 3 && fields[1] == "as" {
				alias = fields[2]
			}
			result.imports = append(result.imports, Import{Language: LanguagePython, Path: "", Package: module, Alias: alias, ImportedPath: fields[0]})
			result.aliases[alias] = fields[0]
		}
		return result, len(result.imports) > 0
	}
	if !strings.HasPrefix(line, "from ") {
		return result, false
	}
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "from" || fields[2] != "import" {
		return result, false
	}
	base := resolvePythonImportBase(module, fields[1])
	items := strings.TrimSpace(strings.SplitN(line, " import ", 2)[1])
	items = strings.Trim(items, "()")
	for _, item := range strings.Split(items, ",") {
		parts := strings.Fields(strings.TrimSpace(item))
		if len(parts) == 0 || parts[0] == "*" || !pythonDottedName(parts[0]) {
			continue
		}
		alias := parts[0]
		if len(parts) == 3 && parts[1] == "as" {
			alias = parts[2]
		}
		target := strings.Trim(base+"."+parts[0], ".")
		result.imports = append(result.imports, Import{Language: LanguagePython, Path: "", Package: module, Alias: alias, ImportedPath: target})
		result.aliases[alias] = target
	}
	return result, len(result.imports) > 0
}

func pythonModuleName(path string) string {
	value := strings.TrimSuffix(filepath.ToSlash(path), filepath.Ext(path))
	value = strings.Trim(value, "/")
	if strings.HasSuffix(value, "/__init__") {
		value = strings.TrimSuffix(value, "/__init__")
	} else if value == "__init__" {
		value = "root"
	}
	return strings.ReplaceAll(value, "/", ".")
}

func resolvePythonImportBase(module, imported string) string {
	if !strings.HasPrefix(imported, ".") {
		return imported
	}
	dots := 0
	for dots < len(imported) && imported[dots] == '.' {
		dots++
	}
	packageParts := strings.Split(module, ".")
	if len(packageParts) > 0 {
		packageParts = packageParts[:len(packageParts)-1]
	}
	for count := 1; count < dots && len(packageParts) > 0; count++ {
		packageParts = packageParts[:len(packageParts)-1]
	}
	remainder := strings.TrimPrefix(imported, strings.Repeat(".", dots))
	if remainder != "" {
		packageParts = append(packageParts, remainder)
	}
	return strings.Join(packageParts, ".")
}

func qualifyPythonName(module string, scopes []pythonScope, name string) string {
	if len(scopes) == 0 {
		return module + "." + name
	}
	return scopes[len(scopes)-1].qualifiedName + "." + name
}

func pythonBlockEnd(lines, maskedLines []string, declarationIndex, declarationIndent int) int {
	end := len(lines)
	for index := declarationIndex + 1; index < len(lines); index++ {
		if strings.TrimSpace(maskedLines[index]) == "" {
			continue
		}
		if pythonIndentWidth(lines[index]) <= declarationIndent {
			return index
		}
	}
	return end
}

func pythonDeclarationComplete(maskedLines []string, declarationIndex int) bool {
	balance := 0
	for index := declarationIndex; index < len(maskedLines); index++ {
		line := maskedLines[index]
		for _, character := range line {
			switch character {
			case '(', '[', '{':
				balance++
			case ')', ']', '}':
				balance--
				if balance < 0 {
					return false
				}
			case ':':
				if balance == 0 {
					return true
				}
			}
		}
	}
	return false
}

func pythonIndentWidth(line string) int {
	width := 0
	for _, character := range line {
		switch character {
		case ' ':
			width++
		case '\t':
			width += 8 - width%8
		default:
			return width
		}
	}
	return width
}

func pythonTestFunction(path, name string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasPrefix(strings.ToLower(name), "test_") || strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
}

func pythonSourceExcerpt(content []byte, startLine, endLine int) string {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if startLine <= 0 || startLine > len(lines) || endLine < startLine {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

func pythonLastName(value string) string {
	parts := strings.Split(value, ".")
	return parts[len(parts)-1]
}

func pythonCallKeyword(value string) bool {
	switch value {
	case "if", "for", "while", "with", "return", "yield", "lambda", "class", "def", "match", "case", "except", "assert", "del", "raise":
		return true
	default:
		return false
	}
}

func pythonDottedName(value string) bool {
	if value == "" {
		return false
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return false
		}
		for index, character := range part {
			if (index == 0 && character != '_' && !unicode.IsLetter(character)) || (index > 0 && character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character)) {
				return false
			}
		}
	}
	return true
}

func maskPythonLines(lines []string) ([]string, error) {
	masked := make([]string, len(lines))
	triple := ""
	for lineIndex, line := range lines {
		var builder strings.Builder
		for index := 0; index < len(line); {
			if triple != "" {
				if strings.HasPrefix(line[index:], triple) {
					builder.WriteString(strings.Repeat(" ", len(triple)))
					index += len(triple)
					triple = ""
				} else {
					builder.WriteByte(' ')
					index++
				}
				continue
			}
			if line[index] == '#' {
				builder.WriteString(strings.Repeat(" ", len(line)-index))
				break
			}
			if line[index] == '\'' || line[index] == '"' {
				quote := line[index]
				candidate := strings.Repeat(string(quote), 3)
				if strings.HasPrefix(line[index:], candidate) {
					triple = candidate
					builder.WriteString("   ")
					index += 3
					continue
				}
				builder.WriteByte(' ')
				index++
				closed := false
				for index < len(line) {
					builder.WriteByte(' ')
					if line[index] == '\\' {
						index++
						if index < len(line) {
							builder.WriteByte(' ')
							index++
						}
						continue
					}
					if line[index] == quote {
						index++
						closed = true
						break
					}
					index++
				}
				if !closed {
					return nil, fmt.Errorf("unterminated string at line %d", lineIndex+1)
				}
				continue
			}
			builder.WriteByte(line[index])
			index++
		}
		masked[lineIndex] = builder.String()
	}
	if triple != "" {
		return nil, errors.New("unterminated triple-quoted string")
	}
	return masked, nil
}
