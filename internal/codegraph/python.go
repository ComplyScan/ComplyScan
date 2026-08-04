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
	pythonDefinitionPattern = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pythonClassPattern      = regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	pythonCallPattern       = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_]*)*)\s*\(`)
	pythonMainGuardPattern  = regexp.MustCompile(`^if\s+__name__\s*==\s*['"]__main__['"]\s*:`)
)

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
			symbol := pythonSymbol(repositoryFile, parsed.module, name, qualified, kind, index+1, endLine, entryPoint)
			parsed.symbols = append(parsed.symbols, symbol)
			parsed.functions = append(parsed.functions, pythonFunction{
				symbolID: symbol.ID, name: name, qualifiedName: qualified,
				classQualifiedName: classQualified, path: repositoryFile.Path,
				startLine: index + 1, endLine: endLine, indent: indent,
			})
			scopes = append(scopes, pythonScope{kind: kind, indent: indent, qualifiedName: qualified})
		}
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
		for lineNumber := function.startLine; lineNumber <= function.endLine && lineNumber <= len(file.maskedLines); lineNumber++ {
			if owner, ok := innermostPythonFunction(file.functions, lineNumber); !ok || owner.symbolID != function.symbolID {
				continue
			}
			line := file.maskedLines[lineNumber-1]
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
				kind := classifyCall(shortName)
				if kind == EdgeCall {
					if symbol, ok := graphSymbolByID(graph.Symbols, function.symbolID); ok && symbol.Kind == SymbolTest {
						kind = EdgeTest
					}
				}
				graph.Edges = append(graph.Edges, Edge{
					Kind: kind, From: function.symbolID, To: target, Label: callName,
					Path: function.path, Line: lineNumber, Resolved: resolved,
				})
			}
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
