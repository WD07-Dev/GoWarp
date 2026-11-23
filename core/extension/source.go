package extension

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
	"unicode"
)

type Source struct {
	Go           []byte
	Requirements []ModuleRequirement
}

func ExtractGo(name string, js []byte) ([]byte, error) {
	source, err := ExtractSource(name, js)
	return source.Go, err
}

/*
ExtractSource reads config and Go blocks from leading JavaScript comments.
Scanning stops before executable JS; strings cannot supply a header.
*/
func ExtractSource(name string, js []byte) (Source, error) {
	rest := strings.TrimPrefix(string(js), "\ufeff")
	var source Source
	var configSeen, goSeen bool
	for {
		rest = strings.TrimLeftFunc(rest, unicode.IsSpace)
		if strings.HasPrefix(rest, "//") {
			end := strings.IndexAny(rest, "\n\r\u2028\u2029")
			if end < 0 {
				break
			}
			// Keep multibyte line separators intact for whitespace trimming.
			rest = rest[end:]
			continue
		}
		if !strings.HasPrefix(rest, "/*") {
			break
		}
		end := strings.Index(rest[2:], "*/")
		if end < 0 {
			return Source{}, fmt.Errorf("%s: unterminated JS header comment", name)
		}
		comment := rest[2 : 2+end]
		parsed := false
		for comment != "" {
			comment = strings.TrimLeftFunc(comment, unicode.IsSpace)
			if comment == "" {
				break
			}
			marker := ""
			for _, candidate := range []string{"@gowarp-config", "@gowarp-go"} {
				if !strings.HasPrefix(comment, candidate) {
					continue
				}
				tail := comment[len(candidate):]
				if tail == "" || strings.HasPrefix(tail, "{") || len(strings.TrimLeftFunc(tail, unicode.IsSpace)) != len(tail) {
					marker = candidate
					break
				}
			}
			if marker == "" {
				if parsed {
					return Source{}, fmt.Errorf("%s: unexpected content after GoWarp header block", name)
				}
				_, comment, _ = strings.Cut(comment, "\n")
				continue
			}
			body, remaining, err := headerBlock(name, marker, strings.TrimLeftFunc(comment[len(marker):], unicode.IsSpace))
			if err != nil {
				return Source{}, err
			}
			switch marker {
			case "@gowarp-config":
				if configSeen {
					return Source{}, fmt.Errorf("%s: multiple @gowarp-config blocks", name)
				}
				configSeen = true
				source.Requirements, err = parseRequirements(name, body)
				if err != nil {
					return Source{}, err
				}
			case "@gowarp-go":
				if goSeen {
					return Source{}, fmt.Errorf("%s: multiple @gowarp-go blocks", name)
				}
				goSeen = true
				source.Go = []byte(body)
			}
			parsed = true
			comment = remaining
		}
		rest = rest[end+4:]
	}
	if !goSeen {
		return Source{}, fmt.Errorf("%s: missing @gowarp-go header; use an extension with a Go implementation", name)
	}
	code, err := validateGo(name, source.Go, source.Requirements)
	source.Go = code
	return source, err
}

func headerBlock(name, marker, text string) (string, string, error) {
	if !strings.HasPrefix(text, "{") {
		return "", "", fmt.Errorf("%s: %s must be followed by {", name, marker)
	}
	positions := token.NewFileSet()
	file := positions.AddFile(name, -1, len(text))
	var scanner scanner.Scanner
	scanner.Init(file, []byte(text), func(token.Position, string) {}, 0)
	depth := 0
	for {
		pos, kind, _ := scanner.Scan()
		switch kind {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			depth--
			if depth == 0 {
				end := file.Offset(pos)
				return text[1:end], text[end+1:], nil
			}
		case token.EOF:
			return "", "", fmt.Errorf("%s: unterminated %s block", name, marker)
		}
	}
}

func validateGo(name string, source []byte, requirements []ModuleRequirement) ([]byte, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name+" (Go comment)", source, parser.AllErrors|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if file.Name.Name != "extension" {
		return nil, fmt.Errorf("%s: Go comment must declare package extension", name)
	}
	sdkName := "extension"
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == "gowarp/core/extension" && imp.Name != nil {
			sdkName = imp.Name.Name
		}
		first, _, _ := strings.Cut(path, "/")
		if path == "gowarp/core/extension" {
			continue
		}
		if first == "gowarp" || first == "C" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, ".") {
			return nil, fmt.Errorf("%s: import %q is not supported; use gowarp/core/extension for host APIs", name, path)
		}
		if strings.Contains(first, ".") {
			declared := false
			for _, requirement := range requirements {
				if path == requirement.Path || strings.HasPrefix(path, requirement.Path+"/") {
					declared = true
					break
				}
			}
			if !declared {
				return nil, fmt.Errorf("%s: import %q needs a require module/path version entry in @gowarp-config", name, path)
			}
		}
	}
	hasNew := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "New" || fn.Recv != nil || fn.Body == nil {
			continue
		}
		signature := fn.Type
		if signature.TypeParams != nil || signature.Params.NumFields() != 0 {
			continue
		}
		if signature.Results == nil || signature.Results.NumFields() != 1 {
			continue
		}
		result, ok := signature.Results.List[0].Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := result.X.(*ast.Ident)
		hasNew = ok && pkg.Name == sdkName && result.Sel.Name == "Extension"
	}
	if !hasNew {
		return nil, fmt.Errorf("%s: Go comment must define func New() extension.Extension", name)
	}
	return bytes.TrimSpace(source), nil
}
