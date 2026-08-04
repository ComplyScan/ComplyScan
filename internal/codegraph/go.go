package codegraph

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ComplyScan/ComplyScan/internal/discovery"
)

const ignoreTechnicalEvidenceMarker = "complyscan:ignore-technical-evidence"

type parsedGoFile struct {
	repositoryFile discovery.File
	file           *ast.File
	fileSet        *token.FileSet
	packageKey     string
}

type goFunction struct {
	declaration *ast.FuncDecl
	symbolID    string
	packageKey  string
	path        string
	fileSet     *token.FileSet
}

// Build indexes the supported source languages in repository. Unsupported
// source files remain visible in coverage rather than being treated as clean.
func Build(repository discovery.Repository) Graph {
	graph := Graph{}
	parsedFiles := make([]parsedGoFile, 0)
	functions := make([]goFunction, 0)
	packageNames := make(map[string]map[string]string)
	globalNames := make(map[string][]string)
	pythonFiles := make([]parsedPythonFile, 0)
	pythonQualifiedNames := make(map[string]string)
	pythonGlobalNames := make(map[string][]string)
	javascriptFiles := make([]parsedJavaScriptFile, 0)
	javascriptQualifiedNames := make(map[string]string)
	javascriptGlobalNames := make(map[string][]string)
	goFilesIndexed := 0
	pythonFilesIndexed := 0
	javascriptFilesIndexed := 0
	typeScriptFilesIndexed := 0

	for _, repositoryFile := range repository.Files {
		if repositoryFile.Kind != discovery.KindSource {
			continue
		}
		if bytes.Contains(repositoryFile.Content, []byte(ignoreTechnicalEvidenceMarker)) {
			continue
		}
		graph.SourceFilesSeen++
		extension := strings.ToLower(filepath.Ext(repositoryFile.Path))
		if language, ok := javascriptLanguageForExtension(extension); ok {
			parsed, err := parseJavaScriptFile(repositoryFile, language)
			if err != nil {
				graph.Warnings = append(graph.Warnings, fmt.Sprintf("could not parse %s source %s: %v", language, repositoryFile.Path, err))
				continue
			}
			javascriptFiles = append(javascriptFiles, parsed)
			graph.FilesIndexed++
			if language == LanguageTypeScript {
				typeScriptFilesIndexed++
			} else {
				javascriptFilesIndexed++
			}
			graph.IndexedSourceFiles = append(graph.IndexedSourceFiles, repositoryFile.Path)
			graph.Imports = append(graph.Imports, parsed.imports...)
			graph.Symbols = append(graph.Symbols, parsed.symbols...)
			for _, function := range parsed.functions {
				javascriptQualifiedNames[function.qualifiedName] = function.symbolID
				javascriptGlobalNames[function.name] = append(javascriptGlobalNames[function.name], function.symbolID)
			}
			continue
		}
		if extension == ".py" {
			parsed, err := parsePythonFile(repositoryFile)
			if err != nil {
				graph.Warnings = append(graph.Warnings, fmt.Sprintf("could not parse Python source %s: %v", repositoryFile.Path, err))
				continue
			}
			pythonFiles = append(pythonFiles, parsed)
			graph.FilesIndexed++
			pythonFilesIndexed++
			graph.IndexedSourceFiles = append(graph.IndexedSourceFiles, repositoryFile.Path)
			graph.Imports = append(graph.Imports, parsed.imports...)
			graph.Symbols = append(graph.Symbols, parsed.symbols...)
			for _, function := range parsed.functions {
				pythonQualifiedNames[function.qualifiedName] = function.symbolID
				pythonGlobalNames[function.name] = append(pythonGlobalNames[function.name], function.symbolID)
			}
			continue
		}
		if extension != ".go" {
			graph.UnsupportedSourceFiles = append(graph.UnsupportedSourceFiles, repositoryFile.Path)
			continue
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, repositoryFile.Path, repositoryFile.Content, parser.ParseComments)
		if err != nil {
			graph.Warnings = append(graph.Warnings, fmt.Sprintf("could not parse Go source %s: %v", repositoryFile.Path, err))
			continue
		}

		packageKey := filepath.ToSlash(filepath.Dir(repositoryFile.Path)) + ":" + file.Name.Name
		parsedFiles = append(parsedFiles, parsedGoFile{
			repositoryFile: repositoryFile,
			file:           file,
			fileSet:        fileSet,
			packageKey:     packageKey,
		})
		graph.FilesIndexed++
		goFilesIndexed++
		graph.IndexedSourceFiles = append(graph.IndexedSourceFiles, repositoryFile.Path)
		for _, importSpec := range file.Imports {
			importedPath, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				continue
			}
			alias := ""
			if importSpec.Name != nil {
				alias = importSpec.Name.Name
			}
			graph.Imports = append(graph.Imports, Import{
				Language: LanguageGo, Path: repositoryFile.Path, Package: file.Name.Name,
				Alias: alias, ImportedPath: importedPath,
			})
		}
		if packageNames[packageKey] == nil {
			packageNames[packageKey] = make(map[string]string)
		}

		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				symbol := goFunctionSymbol(repositoryFile, fileSet, file.Name.Name, typed)
				graph.Symbols = append(graph.Symbols, symbol)
				functions = append(functions, goFunction{
					declaration: typed,
					symbolID:    symbol.ID,
					packageKey:  packageKey,
					path:        repositoryFile.Path,
					fileSet:     fileSet,
				})
				packageNames[packageKey][typed.Name.Name] = symbol.ID
				globalNames[typed.Name.Name] = append(globalNames[typed.Name.Name], symbol.ID)
			case *ast.GenDecl:
				if typed.Tok != token.TYPE {
					continue
				}
				for _, specification := range typed.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok {
						continue
					}
					symbol := goTypeSymbol(repositoryFile, fileSet, file.Name.Name, typeSpec)
					graph.Symbols = append(graph.Symbols, symbol)
					packageNames[packageKey][typeSpec.Name.Name] = symbol.ID
					globalNames[typeSpec.Name.Name] = append(globalNames[typeSpec.Name.Name], symbol.ID)
				}
			}
		}
	}

	if goFilesIndexed > 0 {
		graph.Languages = append(graph.Languages, LanguageGo)
	}
	if pythonFilesIndexed > 0 {
		graph.Languages = append(graph.Languages, LanguagePython)
	}
	if javascriptFilesIndexed > 0 {
		graph.Languages = append(graph.Languages, LanguageJavaScript)
	}
	if typeScriptFilesIndexed > 0 {
		graph.Languages = append(graph.Languages, LanguageTypeScript)
	}

	for _, function := range functions {
		indexGoFunction(&graph, function, packageNames, globalNames)
	}
	for _, file := range pythonFiles {
		indexPythonFile(&graph, file, pythonQualifiedNames, pythonGlobalNames)
	}
	for _, file := range javascriptFiles {
		indexJavaScriptFile(&graph, file, javascriptQualifiedNames, javascriptGlobalNames)
	}

	graph.finalize()
	classifyReachability(&graph)
	graph.finalize()
	return graph
}

func goFunctionSymbol(repositoryFile discovery.File, fileSet *token.FileSet, packageName string, declaration *ast.FuncDecl) Symbol {
	receiver := goReceiverName(declaration)
	qualifiedName := packageName + "." + declaration.Name.Name
	kind := SymbolFunction
	if receiver != "" {
		qualifiedName = packageName + "." + receiver + "." + declaration.Name.Name
		kind = SymbolMethod
	}
	if strings.HasSuffix(repositoryFile.Path, "_test.go") && strings.HasPrefix(declaration.Name.Name, "Test") {
		kind = SymbolTest
	}
	position := fileSet.Position(declaration.Pos())
	end := fileSet.Position(declaration.End())
	entryPoint := packageName == "main" && declaration.Name.Name == "main"
	return Symbol{
		ID:            goSymbolID(repositoryFile.Path, qualifiedName, position.Line),
		Name:          declaration.Name.Name,
		QualifiedName: qualifiedName,
		Kind:          kind,
		Language:      LanguageGo,
		Package:       packageName,
		Path:          repositoryFile.Path,
		StartLine:     position.Line,
		EndLine:       end.Line,
		Exported:      ast.IsExported(declaration.Name.Name),
		EntryPoint:    entryPoint,
		source:        sourceExcerpt(repositoryFile.Content, position.Offset, end.Offset),
	}
}

func goTypeSymbol(repositoryFile discovery.File, fileSet *token.FileSet, packageName string, specification *ast.TypeSpec) Symbol {
	position := fileSet.Position(specification.Pos())
	end := fileSet.Position(specification.End())
	qualifiedName := packageName + "." + specification.Name.Name
	return Symbol{
		ID:            goSymbolID(repositoryFile.Path, qualifiedName, position.Line),
		Name:          specification.Name.Name,
		QualifiedName: qualifiedName,
		Kind:          SymbolType,
		Language:      LanguageGo,
		Package:       packageName,
		Path:          repositoryFile.Path,
		StartLine:     position.Line,
		EndLine:       end.Line,
		Exported:      ast.IsExported(specification.Name.Name),
		source:        sourceExcerpt(repositoryFile.Content, position.Offset, end.Offset),
	}
}

func goSymbolID(path, qualifiedName string, line int) string {
	return fmt.Sprintf("go:%s:%d:%s", filepath.ToSlash(path), line, qualifiedName)
}

func sourceExcerpt(content []byte, start, end int) string {
	if start < 0 || start >= len(content) || end <= start {
		return ""
	}
	if end > len(content) {
		end = len(content)
	}
	return string(content[start:end])
}

func goReceiverName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return ""
	}
	switch receiver := declaration.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return receiver.Name
	case *ast.StarExpr:
		if identifier, ok := receiver.X.(*ast.Ident); ok {
			return identifier.Name
		}
	}
	return "receiver"
}

func indexGoFunction(graph *Graph, function goFunction, packageNames map[string]map[string]string, globalNames map[string][]string) {
	if function.declaration.Body == nil {
		return
	}

	ast.Inspect(function.declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		name := goCallName(call.Fun)
		if name == "" {
			return true
		}
		line := function.fileSet.Position(call.Pos()).Line
		target, resolved := resolveGoCall(name, call.Fun, function.packageKey, packageNames, globalNames)
		label := name
		kind := classifyCall(name)

		if route, handler, ok := goRouteRegistration(call); ok {
			routeTarget, routeResolved := resolveGoExpression(handler, function.packageKey, packageNames, globalNames)
			graph.Edges = append(graph.Edges, Edge{
				Kind:     EdgeRoute,
				From:     function.symbolID,
				To:       routeTarget,
				Label:    route,
				Path:     function.path,
				Line:     line,
				Resolved: routeResolved,
			})
			return true
		}

		if kind == EdgeConfiguration {
			if value, ok := firstStringArgument(call); ok {
				target = "config:" + value
				label = value
				resolved = false
			}
		}
		if kind == EdgeCall {
			if symbol, ok := graphSymbolByID(graph.Symbols, function.symbolID); ok && symbol.Kind == SymbolTest {
				kind = EdgeTest
			}
		}

		graph.Edges = append(graph.Edges, Edge{
			Kind:     kind,
			From:     function.symbolID,
			To:       target,
			Label:    label,
			Path:     function.path,
			Line:     line,
			Resolved: resolved,
		})
		return true
	})
}

func goCallName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	}
	return ""
}

func resolveGoCall(name string, expression ast.Expr, packageKey string, packageNames map[string]map[string]string, globalNames map[string][]string) (string, bool) {
	if _, ok := expression.(*ast.Ident); ok {
		if target, exists := packageNames[packageKey][name]; exists {
			return target, true
		}
	}
	if candidates := globalNames[name]; len(candidates) == 1 {
		return candidates[0], true
	}
	return "unresolved:" + name, false
}

func resolveGoExpression(expression ast.Expr, packageKey string, packageNames map[string]map[string]string, globalNames map[string][]string) (string, bool) {
	name := goCallName(expression)
	if identifier, ok := expression.(*ast.Ident); ok {
		name = identifier.Name
	}
	if name == "" {
		return "unresolved:handler", false
	}
	return resolveGoCall(name, expression, packageKey, packageNames, globalNames)
}

func graphSymbolByID(symbols []Symbol, id string) (Symbol, bool) {
	for _, symbol := range symbols {
		if symbol.ID == id {
			return symbol, true
		}
	}
	return Symbol{}, false
}

func classifyCall(name string) EdgeKind {
	lower := strings.ToLower(name)
	switch {
	case lower == "getenv" || lower == "lookupenv" || strings.Contains(lower, "featureflag") || strings.Contains(lower, "featureenabled"):
		return EdgeConfiguration
	case strings.Contains(lower, "authoriz") || strings.Contains(lower, "permission") || strings.Contains(lower, "rolecheck") || strings.Contains(lower, "reviewer"):
		return EdgeAuthorization
	case strings.Contains(lower, "audit") || strings.Contains(lower, "telemetry") || strings.Contains(lower, "recordevent") || strings.Contains(lower, "logevent"):
		return EdgeLogging
	case strings.HasPrefix(lower, "save") || strings.HasPrefix(lower, "store") || strings.HasPrefix(lower, "persist") || strings.HasPrefix(lower, "insert") || strings.HasPrefix(lower, "update"):
		return EdgePersistence
	default:
		return EdgeCall
	}
}

func goRouteRegistration(call *ast.CallExpr) (string, ast.Expr, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	method := strings.ToUpper(selector.Sel.Name)
	handlerIndex := 1
	switch method {
	case "HANDLE", "HANDLEFUNC", "GET", "POST", "PUT", "PATCH", "DELETE":
	default:
		return "", nil, false
	}
	if len(call.Args) <= handlerIndex {
		return "", nil, false
	}
	route := method
	if value, ok := stringLiteral(call.Args[0]); ok {
		route += " " + value
	}
	return route, call.Args[handlerIndex], true
}

func firstStringArgument(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	return stringLiteral(call.Args[0])
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func classifyReachability(graph *Graph) {
	productionSeeds := make([]string, 0)
	testSeeds := make([]string, 0)
	for _, symbol := range graph.Symbols {
		if symbol.EntryPoint {
			productionSeeds = append(productionSeeds, symbol.ID)
		}
		if symbol.Kind == SymbolTest {
			testSeeds = append(testSeeds, symbol.ID)
		}
	}

	production := reachableSymbols(graph.Edges, productionSeeds, false)
	tests := reachableSymbols(graph.Edges, testSeeds, true)
	for index := range graph.Symbols {
		symbol := &graph.Symbols[index]
		switch {
		case production[symbol.ID]:
			symbol.Reachability = ReachableProduction
		case tests[symbol.ID]:
			symbol.Reachability = ReachableTestOnly
		case symbol.Exported && symbol.Kind != SymbolTest:
			symbol.Reachability = ReachableExported
		default:
			symbol.Reachability = ReachableUnknown
		}
	}
}

func reachableSymbols(edges []Edge, seeds []string, includeTests bool) map[string]bool {
	reached := make(map[string]bool)
	queue := append([]string(nil), seeds...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if reached[current] {
			continue
		}
		reached[current] = true
		for _, edge := range edges {
			if edge.From != current || !edge.Resolved {
				continue
			}
			if !includeTests && edge.Kind == EdgeTest {
				continue
			}
			if !reached[edge.To] {
				queue = append(queue, edge.To)
			}
		}
	}
	return reached
}
