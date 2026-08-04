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
	javascriptFunctionPattern          = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	javascriptArrowPattern             = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b[^=]*=\s*(?:async\s+)?(?:\([^;]*\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*(?::\s*[^=]+)?=>`)
	javascriptFunctionValuePattern     = regexp.MustCompile(`^(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b[^=]*=\s*(?:async\s+)?function\b`)
	javascriptClassPattern             = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	javascriptTypePattern              = regexp.MustCompile(`^(?:export\s+)?(?:default\s+)?(?:interface|type|enum)\s+([A-Za-z_$][A-Za-z0-9_$]*)\b`)
	javascriptMethodPattern            = regexp.MustCompile(`^(?:(?:public|private|protected|static|async|readonly|abstract|override|get|set)\s+)*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?:<[^>{}]*>)?\s*\(`)
	javascriptCallPattern              = regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$]*(?:\s*(?:\.|\?\.)\s*[A-Za-z_$][A-Za-z0-9_$]*)*)\s*\(`)
	javascriptESMImportPattern         = regexp.MustCompile(`^\s*import\s+(.+?)\s+from\s+['"]([^'"]+)['"]`)
	javascriptSideEffectImportPattern  = regexp.MustCompile(`^\s*import\s+['"]([^'"]+)['"]`)
	javascriptRequirePattern           = regexp.MustCompile(`^\s*(?:const|let|var)\s+(.+?)\s*=\s*require\s*\(\s*['"]([^'"]+)['"]\s*\)`)
	javascriptExportListPattern        = regexp.MustCompile(`^\s*export\s*\{([^}]+)\}`)
	javascriptTestPattern              = regexp.MustCompile(`\b(?:it|test)(?:\.(?:only|skip|todo))?\s*\(`)
	javascriptRouteCallPattern         = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*)\.(get|post|put|patch|delete|options|head|use)\s*\(`)
	javascriptObjectRoutePattern       = regexp.MustCompile(`\b([A-Za-z_$][A-Za-z0-9_$]*)\.route\s*\(`)
	javascriptProcessEnvDotPattern     = regexp.MustCompile(`\b(?:process|Bun)\.env\.([A-Za-z_$][A-Za-z0-9_$]*)`)
	javascriptProcessEnvBracketPattern = regexp.MustCompile(`\b(?:process|Bun)\.env\s*\[\s*['"]([^'"]+)['"]\s*\]`)
)

type javascriptDecorator struct {
	expression string
	line       int
}

type javascriptClass struct {
	name          string
	qualifiedName string
	startLine     int
	endLine       int
	indent        int
	routePrefix   string
}

type javascriptFunction struct {
	symbolID           string
	name               string
	qualifiedName      string
	classQualifiedName string
	path               string
	startLine          int
	endLine            int
	indent             int
	exported           bool
	declarationLine    string
	decorators         []javascriptDecorator
}

type parsedJavaScriptFile struct {
	repositoryFile discovery.File
	language       Language
	module         string
	lines          []string
	maskedLines    []string
	imports        []Import
	aliases        map[string]string
	symbols        []Symbol
	functions      []javascriptFunction
	classes        []javascriptClass
}

func javascriptLanguageForExtension(extension string) (Language, bool) {
	switch strings.ToLower(extension) {
	case ".js", ".jsx", ".mjs", ".cjs":
		return LanguageJavaScript, true
	case ".ts", ".tsx", ".mts", ".cts":
		return LanguageTypeScript, true
	default:
		return "", false
	}
}

func parseJavaScriptFile(repositoryFile discovery.File, language Language) (parsedJavaScriptFile, error) {
	content := strings.ReplaceAll(string(repositoryFile.Content), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	masked, err := maskJavaScriptLines(lines)
	if err != nil {
		return parsedJavaScriptFile{}, err
	}
	parsed := parsedJavaScriptFile{
		repositoryFile: repositoryFile, language: language,
		module: javascriptModuleName(repositoryFile.Path), lines: lines, maskedLines: masked,
		aliases: make(map[string]string),
	}
	for index, line := range masked {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if imported, ok := parseJavaScriptImport(lines[index], repositoryFile.Path, parsed.module, language); ok {
			parsed.imports = append(parsed.imports, imported.imports...)
			for alias, target := range imported.aliases {
				parsed.aliases[alias] = target
			}
		}
		if match := javascriptClassPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			endLine, ok := javascriptBlockEnd(masked, index)
			if !ok {
				return parsedJavaScriptFile{}, fmt.Errorf("unterminated class %s at line %d", match[1], index+1)
			}
			qualified := parsed.module + "." + match[1]
			decorators := javascriptDecoratorsBefore(lines, masked, index)
			parsed.classes = append(parsed.classes, javascriptClass{
				name: match[1], qualifiedName: qualified, startLine: index + 1,
				endLine: endLine, indent: javascriptIndentWidth(lines[index]),
				routePrefix: javascriptControllerPrefix(decorators),
			})
			parsed.symbols = append(parsed.symbols, javascriptSymbol(
				repositoryFile, language, parsed.module, match[1], qualified,
				SymbolType, index+1, endLine, javascriptExportedDeclaration(trimmed), false,
			))
			continue
		}
		if match := javascriptTypePattern.FindStringSubmatch(trimmed); len(match) == 2 {
			endLine := index + 1
			if strings.Contains(trimmed, "{") {
				if blockEnd, ok := javascriptBlockEnd(masked, index); ok {
					endLine = blockEnd
				}
			}
			qualified := parsed.module + "." + match[1]
			parsed.symbols = append(parsed.symbols, javascriptSymbol(
				repositoryFile, language, parsed.module, match[1], qualified,
				SymbolType, index+1, endLine, javascriptExportedDeclaration(trimmed), false,
			))
		}
	}

	for index, line := range masked {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		class := javascriptClassForLine(parsed.classes, index+1)
		if class != nil && index+1 != class.startLine {
			match := javascriptMethodPattern.FindStringSubmatch(trimmed)
			if len(match) != 2 || javascriptControlKeyword(match[1]) {
				continue
			}
			endLine, ok := javascriptBlockEnd(masked, index)
			if !ok {
				continue
			}
			name := match[1]
			qualified := class.qualifiedName + "." + name
			kind := SymbolMethod
			if javascriptTestPath(repositoryFile.Path) {
				kind = SymbolTest
			}
			decorators := javascriptDecoratorsBefore(lines, masked, index)
			symbol := javascriptSymbol(repositoryFile, language, parsed.module, name, qualified, kind, index+1, endLine, false, false)
			if len(decorators) > 0 {
				symbol.source = javascriptSourceExcerpt(repositoryFile.Content, decorators[0].line, endLine)
			}
			parsed.symbols = append(parsed.symbols, symbol)
			parsed.functions = append(parsed.functions, javascriptFunction{
				symbolID: symbol.ID, name: name, qualifiedName: qualified,
				classQualifiedName: class.qualifiedName, path: repositoryFile.Path,
				startLine: index + 1, endLine: endLine, indent: javascriptIndentWidth(lines[index]),
				declarationLine: lines[index], decorators: decorators,
			})
			continue
		}
		if class != nil {
			continue
		}
		name, matched := javascriptFunctionName(trimmed)
		if !matched {
			continue
		}
		endLine, block := javascriptBlockEnd(masked, index)
		if !block {
			endLine = index + 1
		}
		kind := SymbolFunction
		if javascriptTestPath(repositoryFile.Path) {
			kind = SymbolTest
		}
		exported := javascriptExportedDeclaration(trimmed)
		qualified := parsed.module + "." + name
		entry := javascriptImplicitEntry(repositoryFile.Path, name, exported)
		symbol := javascriptSymbol(repositoryFile, language, parsed.module, name, qualified, kind, index+1, endLine, exported, entry)
		parsed.symbols = append(parsed.symbols, symbol)
		parsed.functions = append(parsed.functions, javascriptFunction{
			symbolID: symbol.ID, name: name, qualifiedName: qualified, path: repositoryFile.Path,
			startLine: index + 1, endLine: endLine, indent: javascriptIndentWidth(lines[index]),
			exported: exported, declarationLine: lines[index],
		})
	}

	indexJavaScriptTestCallbacks(&parsed)
	indexJavaScriptInlineRouteCallbacks(&parsed)
	markJavaScriptExportLists(&parsed)
	return parsed, nil
}

func javascriptFunctionName(trimmed string) (string, bool) {
	for _, pattern := range []*regexp.Regexp{javascriptFunctionPattern, javascriptArrowPattern, javascriptFunctionValuePattern} {
		if match := pattern.FindStringSubmatch(trimmed); len(match) == 2 {
			return match[1], true
		}
	}
	return "", false
}

func javascriptSymbol(repositoryFile discovery.File, language Language, module, name, qualified string, kind SymbolKind, startLine, endLine int, exported, entry bool) Symbol {
	return Symbol{
		ID:   fmt.Sprintf("%s:%s:%d:%s", language, filepath.ToSlash(repositoryFile.Path), startLine, qualified),
		Name: name, QualifiedName: qualified, Kind: kind, Language: language,
		Package: module, Path: repositoryFile.Path, StartLine: startLine, EndLine: endLine,
		Exported: exported && kind != SymbolTest, EntryPoint: entry,
		source: javascriptSourceExcerpt(repositoryFile.Content, startLine, endLine),
	}
}

func indexJavaScriptFile(graph *Graph, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	for _, function := range file.functions {
		indexJavaScriptDecorators(graph, function, file, qualifiedNames, globalNames)
		if label, ok := javascriptImplicitRouteLabel(function.path, function.name, function.exported); ok {
			markGraphEntryPoint(graph, function.symbolID)
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeRoute, From: "framework-route:" + label, To: function.symbolID, Label: label,
				Path: function.path, Line: function.startLine, Resolved: true,
			})
		}
		for lineNumber := function.startLine; lineNumber <= function.endLine && lineNumber <= len(file.maskedLines); lineNumber++ {
			owner, ok := innermostJavaScriptFunction(file.functions, lineNumber)
			if !ok || owner.symbolID != function.symbolID {
				continue
			}
			line := file.maskedLines[lineNumber-1]
			originalLine := file.lines[lineNumber-1]
			indexJavaScriptEnvironmentAccess(graph, function, line, originalLine, lineNumber)
			for _, match := range javascriptCallPattern.FindAllStringSubmatch(line, -1) {
				if len(match) != 2 {
					continue
				}
				callName := normalizeJavaScriptCallName(match[1])
				shortName := javascriptLastName(callName)
				if shortName == "" || javascriptControlKeyword(shortName) || (lineNumber == function.startLine && shortName == function.name) {
					continue
				}
				target, resolved := resolveJavaScriptCall(callName, function, file, qualifiedNames, globalNames)
				kind := classifyJavaScriptCall(callName)
				label := callName
				if kind == EdgeConfiguration {
					if key, found := pythonFirstStringArgument(originalLine, callName); found {
						target = "config:" + key
						label = key
						resolved = false
					}
				}
				if kind == EdgeCall {
					if symbol, found := graphSymbolByID(graph.Symbols, function.symbolID); found && symbol.Kind == SymbolTest {
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
	indexJavaScriptRoutes(graph, file, qualifiedNames, globalNames)
	markTopLevelJavaScriptCalls(graph, file, qualifiedNames, globalNames)
}

func indexJavaScriptEnvironmentAccess(graph *Graph, function javascriptFunction, masked, original string, line int) {
	if !strings.Contains(masked, ".env") {
		return
	}
	seen := make(map[string]bool)
	for _, pattern := range []*regexp.Regexp{javascriptProcessEnvDotPattern, javascriptProcessEnvBracketPattern} {
		for _, match := range pattern.FindAllStringSubmatch(original, -1) {
			if len(match) != 2 || seen[match[1]] {
				continue
			}
			seen[match[1]] = true
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeConfiguration, From: function.symbolID, To: "config:" + match[1], Label: match[1],
				Path: function.path, Line: line, Resolved: false,
			})
		}
	}
}

func resolveJavaScriptCall(callName string, function javascriptFunction, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) (string, bool) {
	parts := strings.Split(callName, ".")
	shortName := parts[len(parts)-1]
	candidates := make([]string, 0, 5)
	if len(parts) == 1 {
		candidates = append(candidates, file.module+"."+shortName)
		if target, ok := file.aliases[shortName]; ok {
			candidates = append(candidates, target)
		}
	} else {
		if parts[0] == "this" && function.classQualifiedName != "" {
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
	if matches := globalNames[shortName]; len(matches) == 1 {
		return matches[0], true
	}
	return "unresolved:" + callName, false
}

func indexJavaScriptRoutes(graph *Graph, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	for index := range file.maskedLines {
		if !javascriptRouteCallPattern.MatchString(file.maskedLines[index]) && !javascriptObjectRoutePattern.MatchString(file.maskedLines[index]) {
			continue
		}
		maskedStatement, originalStatement := javascriptStatement(file, index)
		if maskedStatement == "" {
			continue
		}
		for _, match := range javascriptRouteCallPattern.FindAllStringSubmatchIndex(maskedStatement, -1) {
			if len(match) != 6 {
				continue
			}
			method := strings.ToUpper(maskedStatement[match[4]:match[5]])
			if method == "USE" {
				method = "ANY"
			}
			arguments := splitJavaScriptArguments(originalStatement, match[1]-1)
			if len(arguments) < 2 {
				continue
			}
			path, ok := javascriptFirstQuotedValue(arguments[0])
			if !ok {
				path = "<dynamic>"
			}
			label := method + " " + normalizeRoutePath(path)
			handler, resolved := resolveJavaScriptRouteHandler(arguments[len(arguments)-1], index+1, file, qualifiedNames, globalNames)
			if resolved {
				markGraphEntryPoint(graph, handler)
			}
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeRoute, From: "framework-route:" + label, To: handler, Label: label,
				Path: file.repositoryFile.Path, Line: index + 1, Resolved: resolved,
			})
			for _, middleware := range arguments[1 : len(arguments)-1] {
				name := javascriptExpressionName(middleware)
				if !javascriptAuthorizationName(name) {
					continue
				}
				dummy := javascriptFunction{path: file.repositoryFile.Path}
				target, middlewareResolved := resolveJavaScriptCall(name, dummy, file, qualifiedNames, globalNames)
				graph.Edges = append(graph.Edges, Edge{
					Kind: EdgeAuthorization, From: handler, To: target, Label: name,
					Path: file.repositoryFile.Path, Line: index + 1, Resolved: middlewareResolved,
				})
			}
		}
		if javascriptObjectRoutePattern.MatchString(maskedStatement) {
			indexJavaScriptObjectRoute(graph, file, index+1, originalStatement, qualifiedNames, globalNames)
		}
	}
}

func indexJavaScriptObjectRoute(graph *Graph, file parsedJavaScriptFile, line int, statement string, qualifiedNames map[string]string, globalNames map[string][]string) {
	method := javascriptObjectProperty(statement, "method")
	path := javascriptObjectProperty(statement, "url")
	if path == "" {
		path = javascriptObjectProperty(statement, "path")
	}
	handlerExpression := javascriptObjectIdentifierProperty(statement, "handler")
	if method == "" || path == "" || handlerExpression == "" {
		return
	}
	label := strings.ToUpper(method) + " " + normalizeRoutePath(path)
	dummy := javascriptFunction{path: file.repositoryFile.Path}
	handler, resolved := resolveJavaScriptCall(handlerExpression, dummy, file, qualifiedNames, globalNames)
	if resolved {
		markGraphEntryPoint(graph, handler)
	}
	graph.Edges = append(graph.Edges, Edge{
		Kind: EdgeRoute, From: "framework-route:" + label, To: handler, Label: label,
		Path: file.repositoryFile.Path, Line: line, Resolved: resolved,
	})
	for _, property := range []string{"preHandler", "onRequest"} {
		name := javascriptObjectIdentifierProperty(statement, property)
		if !javascriptAuthorizationName(name) {
			continue
		}
		target, authResolved := resolveJavaScriptCall(name, dummy, file, qualifiedNames, globalNames)
		graph.Edges = append(graph.Edges, Edge{
			Kind: EdgeAuthorization, From: handler, To: target, Label: name,
			Path: file.repositoryFile.Path, Line: line, Resolved: authResolved,
		})
	}
}

func indexJavaScriptDecorators(graph *Graph, function javascriptFunction, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	if len(function.decorators) == 0 {
		return
	}
	class := javascriptClassForLine(file.classes, function.startLine)
	prefix := ""
	if class != nil {
		prefix = class.routePrefix
	}
	for _, decorator := range function.decorators {
		name := javascriptExpressionName(decorator.expression)
		shortName := strings.ToUpper(javascriptLastName(name))
		if strings.Contains(" GET POST PUT PATCH DELETE OPTIONS HEAD ALL ", " "+shortName+" ") {
			method := shortName
			if method == "ALL" {
				method = "ANY"
			}
			path, _ := javascriptFirstQuotedValue(decorator.expression)
			label := method + " " + joinRoutePath(prefix, path)
			markGraphEntryPoint(graph, function.symbolID)
			graph.Edges = append(graph.Edges, Edge{
				Kind: EdgeRoute, From: "framework-route:" + label, To: function.symbolID, Label: label,
				Path: function.path, Line: decorator.line, Resolved: true,
			})
		}
		if javascriptAuthorizationName(name) || strings.EqualFold(javascriptLastName(name), "UseGuards") || strings.EqualFold(javascriptLastName(name), "Roles") {
			authNames := javascriptDecoratorArguments(decorator.expression)
			if len(authNames) == 0 {
				authNames = []string{name}
			}
			for _, authName := range authNames {
				target, resolved := resolveJavaScriptCall(authName, function, file, qualifiedNames, globalNames)
				graph.Edges = append(graph.Edges, Edge{
					Kind: EdgeAuthorization, From: function.symbolID, To: target, Label: authName,
					Path: function.path, Line: decorator.line, Resolved: resolved,
				})
			}
		}
	}
}

func resolveJavaScriptRouteHandler(expression string, line int, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) (string, bool) {
	if strings.Contains(expression, "=>") || strings.Contains(expression, "function") {
		for _, function := range file.functions {
			if function.startLine == line && strings.HasPrefix(function.name, "route_handler_") {
				return function.symbolID, true
			}
		}
		return "unresolved:inline-route-handler", false
	}
	name := javascriptExpressionName(expression)
	if name == "" {
		return "unresolved:route-handler", false
	}
	dummy := javascriptFunction{path: file.repositoryFile.Path}
	return resolveJavaScriptCall(name, dummy, file, qualifiedNames, globalNames)
}

func javascriptStatement(file parsedJavaScriptFile, start int) (string, string) {
	if start < 0 || start >= len(file.lines) {
		return "", ""
	}
	var masked, original strings.Builder
	depth := 0
	opened := false
	limit := start + 20
	if limit > len(file.lines) {
		limit = len(file.lines)
	}
	for index := start; index < limit; index++ {
		if index > start {
			masked.WriteByte('\n')
			original.WriteByte('\n')
		}
		masked.WriteString(file.maskedLines[index])
		original.WriteString(file.lines[index])
		for _, character := range file.maskedLines[index] {
			switch character {
			case '(':
				depth++
				opened = true
			case ')':
				if depth > 0 {
					depth--
				}
			}
		}
		if opened && depth == 0 {
			break
		}
	}
	return masked.String(), original.String()
}

func splitJavaScriptArguments(source string, openParenthesis int) []string {
	if openParenthesis < 0 || openParenthesis >= len(source) || source[openParenthesis] != '(' {
		return nil
	}
	arguments := make([]string, 0)
	start := openParenthesis + 1
	paren, bracket, brace := 0, 0, 0
	quote := byte(0)
	template := false
	for index := start; index < len(source); index++ {
		character := source[index]
		if quote != 0 {
			if character == '\\' {
				index++
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		if template {
			if character == '\\' {
				index++
			} else if character == '`' {
				template = false
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '`':
			template = true
		case '(':
			paren++
		case ')':
			if paren == 0 && bracket == 0 && brace == 0 {
				if value := strings.TrimSpace(source[start:index]); value != "" {
					arguments = append(arguments, value)
				}
				return arguments
			}
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case '{':
			brace++
		case '}':
			if brace > 0 {
				brace--
			}
		case ',':
			if paren == 0 && bracket == 0 && brace == 0 {
				arguments = append(arguments, strings.TrimSpace(source[start:index]))
				start = index + 1
			}
		}
	}
	return arguments
}

func markTopLevelJavaScriptCalls(graph *Graph, file parsedJavaScriptFile, qualifiedNames map[string]string, globalNames map[string][]string) {
	dummy := javascriptFunction{path: file.repositoryFile.Path}
	for index, line := range file.maskedLines {
		if _, inside := innermostJavaScriptFunction(file.functions, index+1); inside || javascriptDeclarationLine(strings.TrimSpace(line)) {
			continue
		}
		for _, match := range javascriptCallPattern.FindAllStringSubmatch(line, -1) {
			if len(match) != 2 {
				continue
			}
			callName := normalizeJavaScriptCallName(match[1])
			target, resolved := resolveJavaScriptCall(callName, dummy, file, qualifiedNames, globalNames)
			if resolved {
				markGraphEntryPoint(graph, target)
			}
		}
	}
}

type javascriptImportResult struct {
	imports []Import
	aliases map[string]string
}

func parseJavaScriptImport(line, path, module string, language Language) (javascriptImportResult, bool) {
	result := javascriptImportResult{aliases: make(map[string]string)}
	if match := javascriptESMImportPattern.FindStringSubmatch(line); len(match) == 3 {
		base := javascriptImportTarget(path, match[2])
		bindings := strings.TrimSpace(match[1])
		addJavaScriptImportBindings(&result, language, path, module, match[2], base, bindings)
		return result, true
	}
	if match := javascriptSideEffectImportPattern.FindStringSubmatch(line); len(match) == 2 {
		result.imports = append(result.imports, Import{Language: language, Path: path, Package: module, ImportedPath: match[1]})
		return result, true
	}
	if match := javascriptRequirePattern.FindStringSubmatch(line); len(match) == 3 {
		base := javascriptImportTarget(path, match[2])
		addJavaScriptImportBindings(&result, language, path, module, match[2], base, strings.TrimSpace(match[1]))
		return result, true
	}
	return result, false
}

func addJavaScriptImportBindings(result *javascriptImportResult, language Language, path, module, importedPath, base, bindings string) {
	add := func(alias, target string) {
		alias = strings.TrimSpace(alias)
		if !javascriptIdentifier(alias) {
			return
		}
		result.imports = append(result.imports, Import{Language: language, Path: path, Package: module, Alias: alias, ImportedPath: importedPath})
		result.aliases[alias] = target
	}
	if strings.HasPrefix(bindings, "{") {
		for _, item := range strings.Split(strings.Trim(bindings, "{} "), ",") {
			fields := strings.Fields(strings.TrimSpace(item))
			if len(fields) == 0 {
				continue
			}
			alias := fields[0]
			if len(fields) == 3 && fields[1] == "as" {
				alias = fields[2]
			}
			add(alias, base+"."+fields[0])
		}
		return
	}
	if strings.HasPrefix(bindings, "*") {
		fields := strings.Fields(bindings)
		if len(fields) == 3 && fields[1] == "as" {
			add(fields[2], base)
		}
		return
	}
	parts := strings.SplitN(bindings, ",", 2)
	add(strings.TrimSpace(parts[0]), base)
	if len(parts) == 2 {
		addJavaScriptImportBindings(result, language, path, module, importedPath, base, strings.TrimSpace(parts[1]))
	}
}

func javascriptImportTarget(sourcePath, imported string) string {
	if !strings.HasPrefix(imported, ".") {
		return imported
	}
	joined := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(sourcePath), imported)))
	extension := filepath.Ext(joined)
	if _, ok := javascriptLanguageForExtension(extension); ok {
		joined = strings.TrimSuffix(joined, extension)
	}
	joined = strings.TrimSuffix(joined, "/index")
	return strings.ReplaceAll(strings.Trim(joined, "/"), "/", ".")
}

func indexJavaScriptTestCallbacks(parsed *parsedJavaScriptFile) {
	for index, line := range parsed.maskedLines {
		if !javascriptTestPattern.MatchString(line) {
			continue
		}
		endLine, ok := javascriptArrowBlockEnd(parsed.maskedLines, index)
		if !ok {
			endLine = index + 1
		}
		name := fmt.Sprintf("test_case_%d", index+1)
		qualified := parsed.module + "." + name
		symbol := javascriptSymbol(parsed.repositoryFile, parsed.language, parsed.module, name, qualified, SymbolTest, index+1, endLine, false, false)
		parsed.symbols = append(parsed.symbols, symbol)
		parsed.functions = append(parsed.functions, javascriptFunction{
			symbolID: symbol.ID, name: name, qualifiedName: qualified, path: parsed.repositoryFile.Path,
			startLine: index + 1, endLine: endLine, indent: javascriptIndentWidth(parsed.lines[index]), declarationLine: parsed.lines[index],
		})
	}
}

func indexJavaScriptInlineRouteCallbacks(parsed *parsedJavaScriptFile) {
	for index, line := range parsed.maskedLines {
		if !javascriptRouteCallPattern.MatchString(line) || !strings.Contains(line, "=>") {
			continue
		}
		endLine, ok := javascriptArrowBlockEnd(parsed.maskedLines, index)
		if !ok {
			endLine = index + 1
		}
		name := fmt.Sprintf("route_handler_%d", index+1)
		qualified := parsed.module + "." + name
		symbol := javascriptSymbol(parsed.repositoryFile, parsed.language, parsed.module, name, qualified, SymbolFunction, index+1, endLine, false, false)
		parsed.symbols = append(parsed.symbols, symbol)
		parsed.functions = append(parsed.functions, javascriptFunction{
			symbolID: symbol.ID, name: name, qualifiedName: qualified, path: parsed.repositoryFile.Path,
			startLine: index + 1, endLine: endLine, indent: javascriptIndentWidth(parsed.lines[index]), declarationLine: parsed.lines[index],
		})
	}
}

func javascriptDecoratorsBefore(lines, masked []string, declarationIndex int) []javascriptDecorator {
	decorators := make([]javascriptDecorator, 0)
	for index := declarationIndex - 1; index >= 0; index-- {
		trimmedMasked := strings.TrimSpace(masked[index])
		if trimmedMasked == "" {
			continue
		}
		trimmed := strings.TrimSpace(lines[index])
		if !strings.HasPrefix(trimmed, "@") {
			break
		}
		decorators = append([]javascriptDecorator{{
			expression: strings.TrimSpace(strings.TrimPrefix(trimmed, "@")), line: index + 1,
		}}, decorators...)
	}
	return decorators
}

func javascriptControllerPrefix(decorators []javascriptDecorator) string {
	for _, decorator := range decorators {
		if strings.EqualFold(javascriptLastName(javascriptExpressionName(decorator.expression)), "Controller") {
			value, _ := javascriptFirstQuotedValue(decorator.expression)
			return value
		}
	}
	return ""
}

func javascriptDecoratorArguments(expression string) []string {
	open := strings.Index(expression, "(")
	close := strings.LastIndex(expression, ")")
	if open < 0 || close <= open {
		return nil
	}
	arguments := splitJavaScriptArguments(expression, open)
	result := make([]string, 0, len(arguments))
	for _, argument := range arguments {
		name := javascriptExpressionName(argument)
		if javascriptIdentifier(strings.TrimPrefix(name, "this.")) || strings.Contains(name, ".") {
			result = append(result, name)
		}
	}
	return result
}

func javascriptExpressionName(expression string) string {
	value := strings.TrimSpace(expression)
	value = strings.TrimPrefix(value, "async ")
	value = strings.TrimPrefix(value, "new ")
	value = strings.Trim(value, "[]{} ")
	match := javascriptCallPattern.FindStringSubmatch(value)
	if len(match) == 2 && strings.Index(value, match[1]) == 0 {
		name := normalizeJavaScriptCallName(match[1])
		if strings.HasSuffix(name, ".bind") {
			name = strings.TrimSuffix(name, ".bind")
		}
		return name
	}
	end := 0
	for end < len(value) {
		character := rune(value[end])
		if character == '.' || character == '?' || character == '$' || character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character) {
			end++
			continue
		}
		break
	}
	return normalizeJavaScriptCallName(value[:end])
}

func javascriptFirstQuotedValue(source string) (string, bool) {
	for index := 0; index < len(source); index++ {
		quote := source[index]
		if quote != '\'' && quote != '"' && quote != '`' {
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

func javascriptObjectProperty(source, property string) string {
	pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(property) + `\s*:\s*['"]([^'"]+)['"]`)
	match := pattern.FindStringSubmatch(source)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func javascriptObjectIdentifierProperty(source, property string) string {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(property) + `\s*:\s*(?:\[\s*)?([A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*)`)
	match := pattern.FindStringSubmatch(source)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func normalizeRoutePath(path string) string {
	if path == "" {
		return "/"
	}
	if path == "<dynamic>" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

func joinRoutePath(prefix, path string) string {
	value := strings.Trim(strings.TrimSpace(prefix)+"/"+strings.TrimSpace(path), "/")
	if value == "" {
		return "/"
	}
	return "/" + value
}

func javascriptImplicitRouteLabel(path, name string, exported bool) (string, bool) {
	if !exported {
		return "", false
	}
	base := strings.ToLower(filepath.Base(path))
	upperName := strings.ToUpper(name)
	method := ""
	if strings.Contains(" GET POST PUT PATCH DELETE OPTIONS HEAD ", " "+upperName+" ") {
		method = upperName
	}
	normalized := "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	lower := strings.ToLower(normalized)
	if strings.HasPrefix(base, "route.") && method != "" {
		if marker := strings.Index(lower, "/app/"); marker >= 0 {
			route := normalized[marker+len("/app/"):]
			route = route[:len(route)-len(filepath.Base(route))]
			return method + " " + normalizeRoutePath(strings.TrimSuffix(route, "/")), true
		}
	}
	if marker := strings.Index(lower, "/pages/api/"); marker >= 0 && (name == "handler" || strings.Contains(strings.ToLower(name), "handler")) {
		route := normalized[marker+len("/pages"):]
		route = strings.TrimSuffix(route, filepath.Ext(route))
		return "ANY " + normalizeRoutePath(route), true
	}
	return "", false
}

func javascriptArrowBlockEnd(lines []string, declarationIndex int) (int, bool) {
	foundArrow := false
	opened := false
	depth := 0
	for index := declarationIndex; index < len(lines); index++ {
		line := lines[index]
		start := 0
		if !foundArrow {
			arrow := strings.Index(line, "=>")
			if arrow < 0 {
				continue
			}
			foundArrow = true
			start = arrow + 2
		}
		for _, character := range line[start:] {
			switch character {
			case '{':
				opened = true
				depth++
			case '}':
				if opened {
					depth--
					if depth == 0 {
						return index + 1, true
					}
				}
			}
		}
	}
	return len(lines), false
}

func markJavaScriptExportLists(parsed *parsedJavaScriptFile) {
	exportedNames := make(map[string]bool)
	for _, line := range parsed.lines {
		if match := javascriptExportListPattern.FindStringSubmatch(line); len(match) == 2 {
			for _, item := range strings.Split(match[1], ",") {
				fields := strings.Fields(strings.TrimSpace(item))
				if len(fields) > 0 {
					exportedNames[fields[0]] = true
				}
			}
		}
	}
	for index := range parsed.symbols {
		if exportedNames[parsed.symbols[index].Name] && parsed.symbols[index].Kind != SymbolTest {
			parsed.symbols[index].Exported = true
		}
	}
}

func javascriptBlockEnd(lines []string, declarationIndex int) (int, bool) {
	braceDepth := 0
	parenDepth := 0
	opened := false
	for index := declarationIndex; index < len(lines); index++ {
		for _, character := range lines[index] {
			switch character {
			case '(':
				parenDepth++
			case ')':
				if parenDepth > 0 {
					parenDepth--
				}
			case '{':
				if !opened && parenDepth == 0 {
					opened = true
				}
				if opened {
					braceDepth++
				}
			case '}':
				if opened {
					braceDepth--
					if braceDepth == 0 {
						return index + 1, true
					}
				}
			}
		}
		if !opened && strings.Contains(lines[index], ";") {
			return index + 1, false
		}
	}
	return len(lines), false
}

func innermostJavaScriptFunction(functions []javascriptFunction, line int) (javascriptFunction, bool) {
	var selected javascriptFunction
	found := false
	for _, function := range functions {
		if line < function.startLine || line > function.endLine {
			continue
		}
		if !found || function.endLine-function.startLine < selected.endLine-selected.startLine {
			selected = function
			found = true
		}
	}
	return selected, found
}

func javascriptClassForLine(classes []javascriptClass, line int) *javascriptClass {
	var selected *javascriptClass
	for index := range classes {
		candidate := &classes[index]
		if line <= candidate.startLine || line > candidate.endLine {
			continue
		}
		if selected == nil || candidate.endLine-candidate.startLine < selected.endLine-selected.startLine {
			selected = candidate
		}
	}
	return selected
}

func javascriptModuleName(path string) string {
	value := strings.TrimSuffix(filepath.ToSlash(path), filepath.Ext(path))
	value = strings.TrimSuffix(value, "/index")
	value = strings.Trim(value, "/")
	if value == "" {
		value = "root"
	}
	return strings.ReplaceAll(value, "/", ".")
}

func javascriptExportedDeclaration(trimmed string) bool {
	return strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "module.exports") || strings.HasPrefix(trimmed, "exports.")
}

func javascriptImplicitEntry(path, name string, exported bool) bool {
	if !exported {
		return false
	}
	lowerName := strings.ToUpper(name)
	base := strings.ToLower(filepath.Base(path))
	normalized := strings.ToLower(filepath.ToSlash(path))
	if (base == "route.ts" || base == "route.js" || base == "route.tsx" || base == "route.jsx") && strings.Contains(" GET POST PUT PATCH DELETE OPTIONS HEAD ", " "+lowerName+" ") {
		return true
	}
	return strings.Contains("/"+strings.TrimPrefix(normalized, "/"), "/pages/api/") && (name == "handler" || strings.Contains(strings.ToLower(name), "handler"))
}

func javascriptTestPath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	base := strings.ToLower(filepath.Base(lower))
	return strings.Contains(lower, "/__tests__/") || strings.HasPrefix(lower, "__tests__/") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

func classifyJavaScriptCall(callName string) EdgeKind {
	normalized := strings.ReplaceAll(callName, "?.", ".")
	lower := strings.ToLower(normalized)
	shortName := strings.ToLower(javascriptLastName(normalized))
	if javascriptAuthorizationName(normalized) {
		return EdgeAuthorization
	}
	if shortName == "get" && (strings.Contains(lower, "config") || strings.Contains(lower, "env") || strings.Contains(lower, "settings") || strings.Contains(lower, "feature")) {
		return EdgeConfiguration
	}
	return classifyPythonCall(normalized)
}

func javascriptAuthorizationName(value string) bool {
	lower := strings.ToLower(javascriptLastName(value))
	for _, marker := range []string{"auth", "permission", "role", "guard", "access", "reviewer", "verifytoken", "verify_token", "currentuser", "current_user"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return lower == "canoverride" || lower == "can_override"
}

func normalizeJavaScriptCallName(value string) string {
	value = strings.ReplaceAll(value, " ", "")
	return strings.ReplaceAll(value, "?.", ".")
}

func javascriptLastName(value string) string {
	parts := strings.Split(strings.ReplaceAll(value, "?.", "."), ".")
	return parts[len(parts)-1]
}

func javascriptControlKeyword(value string) bool {
	switch value {
	case "if", "for", "while", "switch", "catch", "function", "return", "typeof", "new", "delete", "throw", "await", "super":
		return true
	default:
		return false
	}
}

func javascriptDeclarationLine(trimmed string) bool {
	_, function := javascriptFunctionName(trimmed)
	return function || javascriptClassPattern.MatchString(trimmed) || javascriptTypePattern.MatchString(trimmed)
}

func javascriptIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if (index == 0 && character != '_' && character != '$' && !unicode.IsLetter(character)) || (index > 0 && character != '_' && character != '$' && !unicode.IsLetter(character) && !unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}

func javascriptIndentWidth(line string) int {
	width := 0
	for _, character := range line {
		if character == ' ' {
			width++
			continue
		}
		if character == '\t' {
			width += 8 - width%8
			continue
		}
		break
	}
	return width
}

func javascriptSourceExcerpt(content []byte, startLine, endLine int) string {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if startLine <= 0 || startLine > len(lines) || endLine < startLine {
		return ""
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return strings.Join(lines[startLine-1:endLine], "\n")
}

func maskJavaScriptLines(lines []string) ([]string, error) {
	masked := make([]string, len(lines))
	blockComment := false
	template := false
	for lineIndex, line := range lines {
		var builder strings.Builder
		for index := 0; index < len(line); {
			if blockComment {
				if strings.HasPrefix(line[index:], "*/") {
					builder.WriteString("  ")
					index += 2
					blockComment = false
				} else {
					builder.WriteByte(' ')
					index++
				}
				continue
			}
			if template {
				builder.WriteByte(' ')
				if line[index] == '\\' && index+1 < len(line) {
					index++
					builder.WriteByte(' ')
				} else if line[index] == '`' {
					template = false
				}
				index++
				continue
			}
			if strings.HasPrefix(line[index:], "//") {
				builder.WriteString(strings.Repeat(" ", len(line)-index))
				break
			}
			if strings.HasPrefix(line[index:], "/*") {
				builder.WriteString("  ")
				index += 2
				blockComment = true
				continue
			}
			if line[index] == '`' {
				builder.WriteByte(' ')
				template = true
				index++
				continue
			}
			if line[index] == '\'' || line[index] == '"' {
				quote := line[index]
				builder.WriteByte(' ')
				index++
				closed := false
				for index < len(line) {
					builder.WriteByte(' ')
					if line[index] == '\\' && index+1 < len(line) {
						index++
						builder.WriteByte(' ')
						index++
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
	if blockComment {
		return nil, errors.New("unterminated block comment")
	}
	if template {
		return nil, errors.New("unterminated template literal")
	}
	return masked, nil
}
