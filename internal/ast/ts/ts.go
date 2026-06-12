// Package ts extracts symbols from TypeScript/JavaScript/TSX sources via
// regex heuristics. Good enough for v1 scaffolding; expected to miss exotic
// shapes (complex generics) — caller treats absence as "skip".
package ts

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
)

type extractor struct{}

func New() ast.Extractor { return extractor{} }

func init() {
	ast.Register([]string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}, New())
}

func (extractor) Language() string { return "ts" }

var (
	reExportFn    = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(([^)]*)\)`)
	reExportConst = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?(?:\(([^)]*)\)|([A-Za-z_$][\w$]*))\s*=>`)
	// Express/Fastify-style: app.get("/path", handler) | router.post('/x', ...)
	reRoute = regexp.MustCompile(`(?:^|\s)(?:app|router|fastify|server)\s*\.\s*(get|post|put|patch|delete|options|head)\s*\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)
	// React functional component: export function Foo(... ): JSX or returns <...>
	reReactComp = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:function\s+([A-Z][\w$]*)|const\s+([A-Z][\w$]*)\s*=)`)

	// Class-level decorator (Angular / NestJS / TypeORM style).
	reClassDecorator = regexp.MustCompile(`^\s*@(Component|Injectable|Controller|Module|Directive|Pipe|Entity)\b(?:\s*\(\s*['"]?([^'"\)]*)['"]?[^)]*\))?`)
	reExportClass    = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:abstract\s+)?class\s+([A-Z][\w$]*)`)
	reClassEnd       = regexp.MustCompile(`^\s*\}\s*$`)
	// HTTP-method decorators inside Nest controllers.
	reHttpDecorator = regexp.MustCompile(`^\s*@(Get|Post|Put|Patch|Delete|Options|Head)\s*(?:\(\s*['"]?([^'"\)]*)['"]?[^)]*\))?`)
	// Class method (used only inside decorated classes).
	reClassMethod = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+)?(?:async\s+)?([a-z_$][\w$]*)\s*\(([^)]*)\)`)

	// JSX attribute locator hints (single-line; multi-line attrs are missed by design)
	reTestID = regexp.MustCompile(`data-testid\s*=\s*['"]([^'"]+)['"]`)
	reAria   = regexp.MustCompile(`aria-label\s*=\s*['"]([^'"]+)['"]`)
	reRole   = regexp.MustCompile(`role\s*=\s*['"]([^'"]+)['"]`)
)

func (extractor) Extract(file string, content []byte) ([]ast.Symbol, []ast.LocatorAnchor) {
	var syms []ast.Symbol
	var anchors []ast.LocatorAnchor
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	hasJSX := bytes.Contains(content, []byte("</")) || bytes.Contains(content, []byte("/>"))
	framework := inferFramework(content)

	// Class-tracking state for decorated classes.
	inDecoratedClass := false
	classDecoratorPath := "" // raw decorator arg (route prefix for @Controller)
	classFramework := ""
	className := ""
	braceDepth := 0
	pendingHTTP := struct {
		method string
		path   string
		ok     bool
	}{}

	for chunk, startLine, ok := nextLogicalLine(sc, 0); ok; chunk, startLine, ok = nextLogicalLine(sc, startLine) {
		text := chunk

		// Class-level decorator → arm for the next `export class` line.
		if m := reClassDecorator.FindStringSubmatch(text); m != nil {
			classDecoratorPath = m[2]
			classFramework = decoratorFramework(m[1])
			continue
		}
		if classFramework != "" && className == "" {
			if m := reExportClass.FindStringSubmatch(text); m != nil {
				className = m[1]
				inDecoratedClass = true
				braceDepth = 0
				// Emit a component symbol for the class itself.
				kind := ast.KindComponent
				if classFramework == "nest" {
					kind = ast.KindRoute
				}
				s := ast.Symbol{
					Kind: kind, Name: className, File: file, Language: "ts",
					Line: startLine, EndLine: startLine,
					FrameworkHint: classFramework,
				}
				if kind == ast.KindRoute {
					s.Method = "GET"
					s.Path = classDecoratorPath
					if s.Path == "" {
						s.Path = "/"
					}
				}
				syms = append(syms, s)
				continue
			}
		}

		if inDecoratedClass {
			braceDepth += strings.Count(text, "{") - strings.Count(text, "}")
			if m := reHttpDecorator.FindStringSubmatch(text); m != nil {
				pendingHTTP.method = strings.ToUpper(m[1])
				pendingHTTP.path = m[2]
				pendingHTTP.ok = true
				continue
			}
			if m := reClassMethod.FindStringSubmatch(text); m != nil {
				name := m[1]
				if name == "constructor" {
					continue
				}
				s := ast.Symbol{
					Kind: ast.KindMethod, Name: name, Receiver: className,
					Params: parseParams(m[2]), File: file, Language: "ts",
					Line: startLine, EndLine: startLine, FrameworkHint: classFramework,
				}
				if pendingHTTP.ok {
					s.Kind = ast.KindRoute
					s.Method = pendingHTTP.method
					s.Path = joinRoutePath(classDecoratorPath, pendingHTTP.path)
					pendingHTTP = struct {
						method string
						path   string
						ok     bool
					}{}
				}
				syms = append(syms, s)
				continue
			}
			if braceDepth <= 0 && reClassEnd.MatchString(text) {
				inDecoratedClass = false
				className = ""
				classDecoratorPath = ""
				classFramework = ""
				braceDepth = 0
			}
			continue
		}

		if m := reExportFn.FindStringSubmatch(text); m != nil {
			s := ast.Symbol{
				Kind: KindFor(m[1], hasJSX, file), Name: m[1],
				Params: parseParams(m[2]), File: file, Language: "ts",
				Line: startLine, EndLine: startLine,
				FrameworkHint: framework,
			}
			syms = append(syms, s)
			continue
		}
		if m := reExportConst.FindStringSubmatch(text); m != nil {
			params := m[2]
			if params == "" && m[3] != "" {
				params = m[3]
			}
			s := ast.Symbol{
				Kind: KindFor(m[1], hasJSX, file), Name: m[1],
				Params: parseParams(params), File: file, Language: "ts",
				Line: startLine, EndLine: startLine, FrameworkHint: framework,
			}
			syms = append(syms, s)
			continue
		}
		if m := reReactComp.FindStringSubmatch(text); m != nil && hasJSX {
			name := m[1]
			if name == "" {
				name = m[2]
			}
			if name != "" {
				syms = append(syms, ast.Symbol{
					Kind: ast.KindComponent, Name: name, File: file, Language: "ts",
					Line: startLine, EndLine: startLine, FrameworkHint: "react",
				})
			}
		}
		if m := reRoute.FindStringSubmatch(text); m != nil {
			syms = append(syms, ast.Symbol{
				Kind: ast.KindRoute, Name: m[1] + " " + m[2], Method: strings.ToUpper(m[1]), Path: m[2],
				File: file, Language: "ts", Line: startLine, EndLine: startLine,
				FrameworkHint: framework,
			})
		}
	}
	if hasJSX {
		anchors = extractAnchors(file, content)
		annotateComponents(syms, anchors, content)
	}
	return syms, anchors
}

// annotateComponents post-processes the discovered symbols, computing each
// KindComponent's EndLine via brace/paren balance and attaching the deduped
// anchors that fall inside its line window. It also sets the component's
// HasState/HasOnClick/HasOnSubmit flags based on substring presence within
// the body.
func annotateComponents(syms []ast.Symbol, anchors []ast.LocatorAnchor, content []byte) {
	if len(syms) == 0 {
		return
	}
	lines := splitLines(content)
	for i := range syms {
		if syms[i].Kind != ast.KindComponent {
			continue
		}
		end := componentEndLine(lines, syms[i].Line)
		syms[i].EndLine = end
		body := strings.Join(lines[syms[i].Line-1:end], "\n")
		if strings.Contains(body, "useState") || strings.Contains(body, "useReducer") {
			syms[i].HasState = true
		}
		if strings.Contains(body, "onClick=") {
			syms[i].HasOnClick = true
		}
		if strings.Contains(body, "onSubmit=") {
			syms[i].HasOnSubmit = true
		}
		seen := map[string]bool{}
		for _, a := range anchors {
			if a.Line < syms[i].Line || a.Line > end {
				continue
			}
			if a.Tag == "" {
				a.Tag = openTagBefore(lines, a.Line, syms[i].Line)
			}
			key := a.TestID + "|" + a.Role + "|" + a.Aria + "|" + a.Tag
			if seen[key] {
				continue
			}
			seen[key] = true
			syms[i].Anchors = append(syms[i].Anchors, a)
		}
	}
}

func splitLines(content []byte) []string {
	return strings.Split(string(content), "\n")
}

// componentEndLine returns the 1-based line number where the component body
// ends. It tracks combined brace + paren depth from startLine forward; the
// component ends when depth returns to 0 after first becoming positive.
func componentEndLine(lines []string, startLine int) int {
	if startLine < 1 || startLine > len(lines) {
		return startLine
	}
	depth := 0
	opened := false
	for i := startLine - 1; i < len(lines); i++ {
		depth += braceParenDelta(lines[i])
		if depth > 0 {
			opened = true
		}
		if opened && depth <= 0 {
			return i + 1
		}
	}
	return len(lines)
}

// braceParenDelta returns the net change in combined {…} + (…) depth across
// the line, ignoring characters inside strings and after `//` comments.
func braceParenDelta(s string) int {
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
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			break
		}
		switch c {
		case '"', '\'', '`':
			inStr = c
		case '{', '(':
			depth++
		case '}', ')':
			depth--
		}
	}
	return depth
}

var reTagOnLine = regexp.MustCompile(`<\s*([a-zA-Z][\w-]*)`)

func tagOnLine(line string) string {
	if m := reTagOnLine.FindStringSubmatch(line); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// openTagBefore looks back from anchorLine (1-based, inclusive) to find the
// most recent unclosed `<tag` opening — for multi-line JSX attributes where
// the attribute and the tag-open live on different lines. Search is bounded
// at startLine.
func openTagBefore(lines []string, anchorLine, startLine int) string {
	floor := startLine - 1
	if floor < 0 {
		floor = 0
	}
	for i := anchorLine - 1; i >= floor; i-- {
		if i >= len(lines) {
			continue
		}
		text := lines[i]
		// `<tag …` open without a matching close on the same line marks the
		// element our attribute belongs to.
		if m := reTagOnLine.FindStringSubmatch(text); m != nil {
			tag := strings.ToLower(m[1])
			// Skip self-closing single-line tags (matched < ... /> on same line).
			if strings.Contains(text, "/>") {
				continue
			}
			return tag
		}
	}
	return ""
}

// nextLogicalLine joins continuation lines (where parens are unbalanced) into
// one logical chunk and returns the starting line number of that chunk.
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
		case '"', '\'', '`':
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

func decoratorFramework(dec string) string {
	switch dec {
	case "Component", "Injectable", "Directive", "Pipe", "Module":
		return "angular"
	case "Controller":
		return "nest"
	case "Entity":
		return "typeorm"
	}
	return ""
}

func joinRoutePath(prefix, suffix string) string {
	p := strings.TrimRight(prefix, "/")
	s := strings.TrimLeft(suffix, "/")
	switch {
	case p == "" && s == "":
		return "/"
	case p == "":
		return "/" + s
	case s == "":
		if strings.HasPrefix(p, "/") {
			return p
		}
		return "/" + p
	default:
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		return p + "/" + s
	}
}

func extractAnchors(file string, content []byte) []ast.LocatorAnchor {
	var anchors []ast.LocatorAnchor
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		tag := tagOnLine(text)
		if m := reTestID.FindStringSubmatch(text); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{TestID: m[1], File: file, Line: line, Tag: tag})
		}
		if m := reAria.FindStringSubmatch(text); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{Aria: m[1], File: file, Line: line, Tag: tag})
		}
		if m := reRole.FindStringSubmatch(text); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{Role: m[1], File: file, Line: line, Tag: tag})
		}
	}
	return anchors
}

func KindFor(name string, hasJSX bool, file string) ast.Kind {
	if hasJSX && strings.HasSuffix(file, ".tsx") && len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z' {
		return ast.KindComponent
	}
	return ast.KindFunction
}

func parseParams(raw string) []ast.Param {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := splitTopLevel(raw, ',')
	out := make([]ast.Param, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name, typ := p, ""
		if c := strings.Index(p, ":"); c != -1 {
			name = strings.TrimSpace(p[:c])
			typ = strings.TrimSpace(p[c+1:])
		}
		if eq := strings.Index(name, "="); eq != -1 {
			name = strings.TrimSpace(name[:eq])
		}
		out = append(out, ast.Param{Name: name, Type: typ})
	}
	return out
}

// splitTopLevel splits on sep at brace/bracket/paren depth 0.
func splitTopLevel(s string, sep byte) []string {
	depth := 0
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{', '<':
			depth++
		case ')', ']', '}', '>':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

func inferFramework(content []byte) string {
	s := string(content)
	switch {
	case strings.Contains(s, "from 'express'") || strings.Contains(s, `from "express"`):
		return "express"
	case strings.Contains(s, "from 'fastify'") || strings.Contains(s, `from "fastify"`):
		return "fastify"
	case strings.Contains(s, "from 'next'") || strings.Contains(s, `from "next"`):
		return "next"
	case strings.Contains(s, "from 'react'") || strings.Contains(s, `from "react"`):
		return "react"
	default:
		return ""
	}
}
