package ts

import (
	"regexp"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
)

// v0.26 diff-mode signal extractors. Lightweight regex-based detection
// of interface / type aliases (DTOs), classes with constructors, and
// state stores (Redux / Pinia / Zustand / Vuex). The detectors return
// extra Symbols that plan.fanOutAspects routes to the matching
// aspect template.

var (
	// `export interface Foo {` or `interface Foo {`
	reInterface = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+([A-Z][\w$]*)\s*(?:extends[^{]+)?\{`)
	// `export type Foo = {` or `type Foo = {`
	reTypeAlias = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+([A-Z][\w$]*)\s*=\s*\{`)
	// `export class Foo {` or `class Foo {`
	reClass = regexp.MustCompile(`(?m)^(?:export\s+)?class\s+([A-Z][\w$]*)\s*(?:extends[^{]+|implements[^{]+)?\{`)
	// Constructor signature: `constructor(a: string, b: number) {`
	reConstructor = regexp.MustCompile(`(?m)^\s*constructor\s*\(([^)]*)\)\s*\{`)
	// Redux `createSlice({ name: '...', initialState, reducers: { foo, bar } })`
	reCreateSlice    = regexp.MustCompile(`createSlice\s*\(\s*\{[^}]*name\s*:\s*['"]([\w-]+)['"]`)
	reReducersBlock  = regexp.MustCompile(`reducers\s*:\s*\{([^}]+)\}`)
	rePiniaDefine    = regexp.MustCompile(`defineStore\s*\(\s*['"]([\w-]+)['"]\s*,\s*\{`)
	reZustandCreate  = regexp.MustCompile(`(?m)^(?:export\s+)?const\s+(use[A-Z][\w$]*)\s*=\s*create\s*\(`)
	reVuexNew        = regexp.MustCompile(`new\s+Vuex\.Store\s*\(`)
	reActionFunction = regexp.MustCompile(`\b([a-z][\w$]*)\s*:\s*(?:\([^)]*\)\s*=>|function|async)`)
)

// extractV026Symbols scans the file content for interfaces, classes
// with constructors, and state stores. Returns the new Symbols to
// append to the regular extraction output.
func extractV026Symbols(file string, content []byte) []ast.Symbol {
	src := string(content)
	out := []ast.Symbol{}

	// Interfaces and `type X = { ... }` aliases — DTO candidates.
	out = append(out, extractDTOInterfaces(file, src)...)

	// Classes (with constructor signatures) — constructor test candidates.
	out = append(out, extractClassConstructors(file, src)...)

	// State stores — store test candidates.
	out = append(out, extractStores(file, src)...)

	return out
}

func extractDTOInterfaces(file, src string) []ast.Symbol {
	var out []ast.Symbol
	for _, re := range []*regexp.Regexp{reInterface, reTypeAlias} {
		for _, m := range re.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			start := m[0]
			body := bodyFromBrace(src, m[1]-1) // m[1] is end of `{`
			// DTO heuristic: body contains only `name: type;` fields, no
			// `(` / `=>` / `function`. Reject method-bearing types.
			if isDataOnlyBody(body) {
				out = append(out, ast.Symbol{
					Kind:     ast.KindFunction,
					Name:     name,
					File:     file,
					Language: "ts",
					Line:     lineNumberAt(src, start),
					IsDTO:    true,
					Params:   parseDataFields(body),
				})
			}
		}
	}
	return out
}

func extractClassConstructors(file, src string) []ast.Symbol {
	var out []ast.Symbol
	for _, m := range reClass.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Find the next constructor signature within the class body.
		bodyStart := m[1] - 1
		body := bodyFromBrace(src, bodyStart)
		ctor := reConstructor.FindStringSubmatch(body)
		if ctor == nil {
			continue
		}
		params := parseParams(ctor[1])
		if len(params) == 0 {
			// No-arg constructors don't need a constructor test.
			continue
		}
		out = append(out, ast.Symbol{
			Kind:          ast.KindFunction,
			Name:          name,
			File:          file,
			Language:      "ts",
			Line:          lineNumberAt(src, m[0]),
			FrameworkHint: "class",
			Params:        params,
		})
	}
	return out
}

func extractStores(file, src string) []ast.Symbol {
	var out []ast.Symbol
	// Redux createSlice
	for _, m := range reCreateSlice.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		// Pull the reducers block to enumerate action names.
		actions := findReducerActions(src[m[0]:])
		out = append(out, ast.Symbol{
			Kind:         ast.KindFunction,
			Name:         name + "Slice",
			File:         file,
			Language:     "ts",
			Line:         lineNumberAt(src, m[0]),
			StoreKind:    "redux",
			StoreActions: actions,
		})
	}
	// Pinia defineStore
	for _, m := range rePiniaDefine.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		actions := findReducerActions(src[m[0]:])
		out = append(out, ast.Symbol{
			Kind:         ast.KindFunction,
			Name:         "use" + capitalize(name) + "Store",
			File:         file,
			Language:     "ts",
			Line:         lineNumberAt(src, m[0]),
			StoreKind:    "pinia",
			StoreActions: actions,
		})
	}
	// Zustand create
	for _, m := range reZustandCreate.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		out = append(out, ast.Symbol{
			Kind:         ast.KindFunction,
			Name:         name,
			File:         file,
			Language:     "ts",
			Line:         lineNumberAt(src, m[0]),
			StoreKind:    "zustand",
			StoreActions: findReducerActions(src[m[0]:]),
		})
	}
	// Vuex Store
	if loc := reVuexNew.FindStringIndex(src); loc != nil {
		out = append(out, ast.Symbol{
			Kind:         ast.KindFunction,
			Name:         "vuexStore",
			File:         file,
			Language:     "ts",
			Line:         lineNumberAt(src, loc[0]),
			StoreKind:    "vuex",
			StoreActions: findReducerActions(src[loc[0]:]),
		})
	}
	return out
}

// bodyFromBrace returns the substring from after the opening `{` at
// position openIdx through the matching closing `}`. Brace-counts.
func bodyFromBrace(src string, openIdx int) string {
	if openIdx < 0 || openIdx >= len(src) {
		return ""
	}
	depth := 1
	for i := openIdx + 1; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[openIdx+1 : i]
			}
		}
	}
	return src[openIdx+1:]
}

// isDataOnlyBody returns true when the interface/type body looks like
// a DTO — only `name: type;` fields, no method signatures.
func isDataOnlyBody(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	// Reject when the body contains call signatures or arrow functions.
	if strings.Contains(body, "=>") || strings.Contains(body, "(") {
		return false
	}
	return true
}

// parseDataFields extracts simple `name: type;` entries from an
// interface body and returns them as ast.Params (name + type).
func parseDataFields(body string) []ast.Param {
	var out []ast.Param
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ";")
		line = strings.TrimSuffix(line, ",")
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		// Optional `name?: type`
		line = strings.ReplaceAll(line, "?:", ":")
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		out = append(out, ast.Param{
			Name: strings.TrimSpace(line[:i]),
			Type: strings.TrimSpace(line[i+1:]),
		})
	}
	return out
}

// findReducerActions scans a Redux/Pinia/Zustand body for action
// function names. Returns up to 8 names so tests stay snappy.
func findReducerActions(src string) []string {
	// Restrict to the next ~4 KiB so we don't pick up siblings.
	if len(src) > 4096 {
		src = src[:4096]
	}
	out := []string{}
	seen := map[string]bool{}
	if m := reReducersBlock.FindStringSubmatch(src); m != nil {
		src = m[1]
	}
	for _, am := range reActionFunction.FindAllStringSubmatch(src, -1) {
		name := am[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// lineNumberAt returns the 1-based line number of byte position `pos`
// within `src`. Used to keep emitted Symbols consistent with the
// regular scanner.
func lineNumberAt(src string, pos int) int {
	if pos <= 0 {
		return 1
	}
	if pos > len(src) {
		pos = len(src)
	}
	return strings.Count(src[:pos], "\n") + 1
}
