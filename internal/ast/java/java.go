// Package java extracts symbols from Java sources via regex heuristics.
package java

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
	ast.Register([]string{".java"}, New())
}

func (extractor) Language() string { return "java" }

var (
	rePublicMethod = regexp.MustCompile(`^\s*public\s+(?:static\s+)?(?:final\s+)?([\w<>?\[\],\s]+?)\s+([A-Za-z_][\w]*)\s*\(([^)]*)\)`)
	reClass        = regexp.MustCompile(`^\s*(?:public\s+|final\s+|abstract\s+)*class\s+([A-Z][\w]*)`)
	reMapping      = regexp.MustCompile(`@(?:Get|Post|Put|Patch|Delete|Request)Mapping(?:\s*\(\s*(?:value\s*=\s*)?['"]?([^'"\)]+)['"]?[^)]*\))?`)
	reMethodAnnot  = regexp.MustCompile(`@(Get|Post|Put|Patch|Delete)Mapping`)
)

func (extractor) Extract(file string, content []byte) ([]ast.Symbol, []ast.LocatorAnchor) {
	var syms []ast.Symbol
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	framework := inferFramework(content)
	currentClass := ""
	var pending struct {
		isRoute bool
		method  string
		path    string
	}
	for chunk, startLine, ok := nextLogicalLine(sc, 0); ok; chunk, startLine, ok = nextLogicalLine(sc, startLine) {
		text := chunk
		if m := reClass.FindStringSubmatch(text); m != nil {
			currentClass = m[1]
		}
		if isAnnotationChunk(text) {
			if m := reMapping.FindStringSubmatch(text); m != nil {
				pending.isRoute = true
				pending.path = m[1]
				if mm := reMethodAnnot.FindStringSubmatch(text); mm != nil {
					pending.method = strings.ToUpper(mm[1])
				} else {
					pending.method = "GET"
				}
			}
			continue
		}
		if m := rePublicMethod.FindStringSubmatch(text); m != nil {
			ret, name, params := strings.TrimSpace(m[1]), m[2], m[3]
			if name == currentClass { // constructor
				pending = struct {
					isRoute bool
					method  string
					path    string
				}{}
				continue
			}
			s := ast.Symbol{
				Kind: ast.KindMethod, Name: name, Receiver: currentClass,
				Returns: ret, Params: parseParams(params),
				File: file, Language: "java", Line: startLine, EndLine: startLine,
				FrameworkHint: framework,
			}
			if pending.isRoute {
				s.Kind = ast.KindRoute
				s.Method = pending.method
				s.Path = pending.path
				if s.Path == "" {
					s.Path = "/"
				}
			}
			syms = append(syms, s)
			pending = struct {
				isRoute bool
				method  string
				path    string
			}{}
		}
	}
	return syms, nil
}

// nextLogicalLine joins continuation lines (where parens are unbalanced) into
// one logical chunk and returns the starting line number of that chunk. Returns
// ok=false when the scanner is exhausted.
func nextLogicalLine(sc *bufio.Scanner, prevLine int) (string, int, bool) {
	if !sc.Scan() {
		return "", 0, false
	}
	startLine := prevLine + 1
	text := sc.Text()
	curLine := startLine
	for parenDepth(text) > 0 && sc.Scan() {
		curLine++
		text += " " + strings.TrimSpace(sc.Text())
	}
	return text, startLine, true
}

func parenDepth(s string) int {
	depth := 0
	inStr := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr != 0 {
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			inStr = c
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	if depth < 0 {
		return 0
	}
	return depth
}

func isAnnotationChunk(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "@")
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
		if p == "" {
			continue
		}
		// strip annotations like @PathVariable("id")
		for strings.HasPrefix(p, "@") {
			end := strings.IndexAny(p, " \t")
			if end == -1 {
				p = ""
				break
			}
			p = strings.TrimSpace(p[end:])
		}
		fields := strings.Fields(p)
		if len(fields) < 2 {
			continue
		}
		typ := strings.Join(fields[:len(fields)-1], " ")
		name := fields[len(fields)-1]
		out = append(out, ast.Param{Name: name, Type: typ})
	}
	return out
}

func inferFramework(content []byte) string {
	s := string(content)
	switch {
	case strings.Contains(s, "org.springframework"):
		return "spring"
	case strings.Contains(s, "javax.ws.rs") || strings.Contains(s, "jakarta.ws.rs"):
		return "jaxrs"
	default:
		return ""
	}
}
