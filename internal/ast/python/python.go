// Package python extracts symbols from Python sources via regex heuristics.
package python

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
)

type extractor struct{}

func New() ast.Extractor { return extractor{} }

func init() {
	ast.Register([]string{".py"}, New())
}

func (extractor) Language() string { return "python" }

var (
	reDef = regexp.MustCompile(`^(\s*)def\s+([A-Za-z_][\w]*)\s*\(([^)]*)\)`)
	// FastAPI/Flask: @app.get("/path") | @router.post('/x')
	reRoute = regexp.MustCompile(`^\s*@\s*(?:app|router|blueprint|bp)\s*\.\s*(get|post|put|patch|delete|options|head)\s*\(\s*['"]([^'"]+)['"]`)
	reClass = regexp.MustCompile(`^class\s+([A-Z][\w]*)`)
)

func (extractor) Extract(file string, content []byte) ([]ast.Symbol, []ast.LocatorAnchor) {
	var syms []ast.Symbol
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	line := 0
	framework := inferFramework(content)
	var pendingRoute *ast.Symbol
	currentClass := ""
	classIndent := -1
	for sc.Scan() {
		line++
		text := sc.Text()
		if m := reClass.FindStringSubmatch(text); m != nil {
			currentClass = m[1]
			classIndent = leadingSpaces(text)
		} else if currentClass != "" && leadingSpaces(text) <= classIndent && strings.TrimSpace(text) != "" {
			currentClass = ""
		}
		if m := reRoute.FindStringSubmatch(text); m != nil {
			pendingRoute = &ast.Symbol{
				Kind: ast.KindRoute, Method: strings.ToUpper(m[1]), Path: m[2],
				Name: strings.ToUpper(m[1]) + " " + m[2],
				File: file, Language: "python", Line: line, EndLine: line,
				FrameworkHint: framework,
			}
			continue
		}
		if m := reDef.FindStringSubmatch(text); m != nil {
			indent, name, params := m[1], m[2], m[3]
			if strings.HasPrefix(name, "_") && !strings.HasPrefix(name, "__") {
				pendingRoute = nil
				continue // private
			}
			if pendingRoute != nil {
				pendingRoute.Name = name
				pendingRoute.Line = line
				pendingRoute.EndLine = line
				pendingRoute.Params = parseParams(params)
				syms = append(syms, *pendingRoute)
				pendingRoute = nil
				continue
			}
			kind := ast.KindFunction
			receiver := ""
			if currentClass != "" && len(indent) > 0 {
				kind = ast.KindMethod
				receiver = currentClass
			}
			syms = append(syms, ast.Symbol{
				Kind: kind, Name: name, Receiver: receiver,
				Params: parseParams(params), File: file, Language: "python",
				Line: line, EndLine: line, FrameworkHint: framework,
			})
		}
	}
	return syms, nil
}

func leadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

func parseParams(raw string) []ast.Param {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]ast.Param, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "self" || p == "cls" {
			continue
		}
		name, typ := p, ""
		if c := strings.Index(p, ":"); c != -1 {
			name = strings.TrimSpace(p[:c])
			typ = strings.TrimSpace(p[c+1:])
			if eq := strings.Index(typ, "="); eq != -1 {
				typ = strings.TrimSpace(typ[:eq])
			}
		} else if eq := strings.Index(name, "="); eq != -1 {
			name = strings.TrimSpace(name[:eq])
		}
		out = append(out, ast.Param{Name: name, Type: typ})
	}
	return out
}

func inferFramework(content []byte) string {
	s := string(content)
	switch {
	case strings.Contains(s, "from fastapi") || strings.Contains(s, "import fastapi"):
		return "fastapi"
	case strings.Contains(s, "from flask") || strings.Contains(s, "import flask"):
		return "flask"
	case strings.Contains(s, "from django"):
		return "django"
	default:
		return ""
	}
}
