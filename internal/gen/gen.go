// Package gen renders deterministic test scaffolds from plan.Items using
// Go text/template. Templates are embedded into the binary so the CLI ships
// as a single artifact with no runtime asset path.
package gen

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/log"
	"github.com/reviewqa/reviewqa/internal/plan"
)

//go:embed all:templates
var templatesFS embed.FS

// Mount point: we embed via a re-export below; the embed directive can't
// reach ../../templates, so we duplicate-link at build time via a //go:embed
// in the wrapper file.

type Rendered struct {
	Path         string
	Content      []byte
	Symbol       ast.Symbol
	QualityNotes []string // one entry per weak / skipped locator found in this spec
}

func Render(items []plan.Item, workDir string) ([]Rendered, error) {
	var out []Rendered
	for _, it := range items {
		tmpl, err := load(it.Template)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", it.Template, err)
		}
		data := buildData(it, workDir)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("render %s for %s: %w", it.Template, it.Symbol.Name, err)
		}
		content, notes := annotateQualityReport(buf.Bytes())
		out = append(out, Rendered{Path: it.OutPath, Content: content, Symbol: it.Symbol, QualityNotes: notes})
		log.Debug("rendered scaffold", "template", it.Template, "symbol", it.Symbol.Name, "path", it.OutPath, "quality_notes", len(notes))
	}
	return out, nil
}

// annotateQualityReport scans a rendered spec for weak-locator markers
// (`// SKIP:` and `// note: using <...>`) and prepends a block comment
// summarising them. Returns the (possibly modified) content + the list of
// note strings for the caller to surface in the PR body.
func annotateQualityReport(content []byte) ([]byte, []string) {
	var notes []string
	for line := range strings.SplitSeq(string(content), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "// SKIP:"):
			notes = append(notes, strings.TrimPrefix(t, "// "))
		case strings.HasPrefix(t, "// note:"):
			notes = append(notes, strings.TrimPrefix(t, "// "))
		}
	}
	if len(notes) == 0 {
		return content, nil
	}
	var b strings.Builder
	b.WriteString("/* reviewqa quality report\n")
	b.WriteString(" * Weak / missing locators on this page:\n")
	for _, n := range notes {
		fmt.Fprintf(&b, " *   - %s\n", n)
	}
	b.WriteString(" * Add data-testid to these elements for stable tests.\n")
	b.WriteString(" */\n")
	return append([]byte(b.String()), content...), notes
}

func load(t plan.Template) (*template.Template, error) {
	sub, file := templateLocation(t)
	body, err := templatesFS.ReadFile(path.Join("templates", sub, file))
	if err != nil {
		return nil, err
	}
	return template.New(string(t)).Funcs(funcs).Parse(string(body))
}

func templateLocation(t plan.Template) (string, string) {
	switch t {
	case plan.TmplJestUnit:
		return "ts", "jest_unit.tmpl"
	case plan.TmplJestAPI:
		return "ts", "jest_api.tmpl"
	case plan.TmplPlaywrightE2E:
		return "ts", "pw_e2e.tmpl"
	case plan.TmplPlaywrightHappyFlow:
		return "ts", "pw_happyflow.tmpl"
	case plan.TmplPytestUnit:
		return "py", "pytest_unit.tmpl"
	case plan.TmplPytestAPI:
		return "py", "pytest_api.tmpl"
	case plan.TmplGoUnit:
		return "go", "gotest_unit.tmpl"
	case plan.TmplGoHTTPTest:
		return "go", "gotest_httptest.tmpl"
	case plan.TmplJUnit5Unit:
		return "java", "junit5_unit.tmpl"
	case plan.TmplJUnit5RestAssured:
		return "java", "junit5_restassured.tmpl"
	}
	return "", ""
}

var funcs = template.FuncMap{
	"lower":     strings.ToLower,
	"upper":     strings.ToUpper,
	"hasPrefix": strings.HasPrefix,
	"firstClickable": func(as []ast.LocatorAnchor) []ast.LocatorAnchor {
		for _, a := range as {
			switch a.Tag {
			case "button", "summary", "a", "input":
				return []ast.LocatorAnchor{a}
			}
		}
		return nil
	},
	"locatorFor": func(a ast.LocatorAnchor) string {
		switch {
		case a.TestID != "":
			return fmt.Sprintf("getByTestId('%s')", a.TestID)
		case a.Aria != "":
			return fmt.Sprintf("getByLabel('%s')", a.Aria)
		case a.Role != "":
			return fmt.Sprintf("getByRole('%s')", a.Role)
		}
		return "locator('body')"
	},
	"anchorLabel": func(a ast.LocatorAnchor) string {
		switch {
		case a.TestID != "":
			return a.TestID
		case a.Aria != "":
			return a.Aria
		case a.Role != "":
			return a.Role
		}
		return "element"
	},
	"isPrimitiveType": func(t string) bool {
		switch strings.TrimSpace(t) {
		case "int", "long", "short", "byte", "double", "float", "boolean", "char":
			return true
		}
		return false
	},
	"defaultForType": func(t string) string {
		switch strings.TrimSpace(t) {
		case "int", "long", "short", "byte", "double", "float":
			return "0"
		case "boolean":
			return "false"
		case "char":
			return "'\\0'"
		case "string", "String":
			return "\"\""
		}
		return "null"
	},
	"fillValueFor":        fillValueFor,
	"inputLocator":        inputLocator,
	"firstSubmit":         firstSubmit,
	"firstSameOriginLink": firstSameOriginLink,
	"linkHref":            linkHref,
	"shouldCheck":         func(i ast.FormInput) bool { return i.Type == "checkbox" || i.Type == "radio" },
	"hasRequiredInput":    hasRequiredInput,
	"firstRequiredInput":  firstRequiredInput,
	"isAbsoluteURL": func(s string) bool {
		return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
	},
	"intentFor":         intentFor,
	"locatorProvenance": locatorProvenance,
}

// fillByType is the deterministic test-value table for form inputs.
// Empty value signals .check() (for checkbox/radio) or .selectOption (for
// select) — i.e. a fill() call is NOT what the caller should emit.
var fillByType = map[string]string{
	"email":    "test@example.com",
	"password": "Passw0rd!",
	"tel":      "+15551234567",
	"phone":    "+15551234567",
	"url":      "https://example.com",
	"number":   "42",
	"date":     "2026-01-01",
	"time":     "12:00",
	"search":   "query",
	"textarea": "sample text",
	"checkbox": "",
	"radio":    "",
	"select":   "",
}

// fillValueFor returns a deterministic test value for a form input. The
// value never reflects PR diff content — purely type/name-derived.
func fillValueFor(i ast.FormInput) string {
	if v, ok := fillByType[i.Type]; ok {
		return v
	}
	if i.Name != "" {
		return i.Name + "-value"
	}
	return "test"
}

// inputLocator chooses the Playwright locator for a form input in priority
// order. Returns "" when no stable locator can be derived — the template
// then emits a SKIP comment instead of a meaningless `locator('input')`.
func inputLocator(i ast.FormInput) string {
	switch {
	case i.TestID != "":
		return fmt.Sprintf("getByTestId('%s')", i.TestID)
	case i.Aria != "":
		return fmt.Sprintf("getByLabel('%s')", i.Aria)
	case i.Placeholder != "":
		return fmt.Sprintf("getByPlaceholder('%s')", i.Placeholder)
	case i.LabelText != "":
		return fmt.Sprintf("getByLabel('%s')", i.LabelText)
	case i.Name != "":
		return fmt.Sprintf("locator('[name=\"%s\"]')", i.Name)
	}
	return ""
}

// locatorProvenance returns the rank label for the fallback chain when the
// chosen locator is NOT the strongest (testid). Empty string means the input
// has a strong locator and no provenance note is needed.
func locatorProvenance(i ast.FormInput) string {
	switch {
	case i.TestID != "":
		return ""
	case i.Aria != "":
		return ""
	case i.Placeholder != "":
		return "placeholder"
	case i.LabelText != "":
		return "label-for"
	case i.Name != "":
		return "name"
	}
	return ""
}

// firstSubmit returns the first submit-tagged anchor, otherwise the first
// button anchor. Inputs are NOT considered — they're for fills, not clicks.
// Single-element slice for use with {{with}} in templates.
func firstSubmit(anchors []ast.LocatorAnchor) []ast.LocatorAnchor {
	for _, a := range anchors {
		if a.Tag == "submit" {
			return []ast.LocatorAnchor{a}
		}
	}
	for _, a := range anchors {
		if a.Tag == "button" {
			return []ast.LocatorAnchor{a}
		}
	}
	return nil
}

// firstSameOriginLink returns the first link whose target starts with "/"
// (relative same-origin). Single-element slice for {{with}}.
func firstSameOriginLink(links []ast.LocatorAnchor) []ast.LocatorAnchor {
	for _, l := range links {
		if strings.HasPrefix(l.Aria, "/") && !strings.HasPrefix(l.Aria, "//") {
			return []ast.LocatorAnchor{l}
		}
	}
	return nil
}

// linkHref returns the link's href (stored in Aria during extraction).
func linkHref(l ast.LocatorAnchor) string {
	return l.Aria
}

// intentFor classifies a Symbol into one of three flow shapes. The template
// switches on the returned string to emit a form-fill, nav-only, or
// minimal-smoke spec — instead of the previous fire-everything approach
// that produced fill+submit calls on pages with no login intent.
func intentFor(s ast.Symbol) string {
	hasSubmit := false
	for _, a := range s.Anchors {
		if a.Tag == "submit" {
			hasSubmit = true
			break
		}
	}
	hasRequired := false
	for _, i := range s.Inputs {
		if i.Required {
			hasRequired = true
			break
		}
	}
	if s.HasForm && hasRequired && hasSubmit {
		return "form"
	}
	for _, l := range s.Links {
		if l.Aria != "" {
			return "nav"
		}
	}
	return "content"
}

// hasRequiredInput is true when any FormInput in the slice is marked
// required. Used to gate the onSubmit validation scenario.
func hasRequiredInput(inputs []ast.FormInput) bool {
	for _, i := range inputs {
		if i.Required {
			return true
		}
	}
	return false
}

// firstRequiredInput returns a single-element slice with the first input
// flagged required. Designed for {{with firstRequiredInput .Inputs}} in
// templates. Returns nil when none exist.
func firstRequiredInput(inputs []ast.FormInput) []ast.FormInput {
	for _, i := range inputs {
		if i.Required {
			return []ast.FormInput{i}
		}
	}
	return nil
}

type renderData struct {
	Symbol          ast.Symbol
	Symbols         []ast.Symbol // populated for happy-flow; first == Symbol
	PageURL         string       // populated for happy-flow; "/" default
	ImportPath      string
	AppImportPath   string
	SupertestMethod string
	HappyArgs       string
	SnakeName       string
	Package         string
}

func buildData(it plan.Item, workDir string) renderData {
	d := renderData{Symbol: it.Symbol}
	d.Symbols = it.Symbols
	if len(d.Symbols) == 0 {
		d.Symbols = []ast.Symbol{it.Symbol}
	}
	d.PageURL = it.PageURL
	if d.PageURL == "" {
		d.PageURL = "/"
	}
	d.HappyArgs = happyArgs(it.Symbol)
	d.SnakeName = toSnake(it.Symbol.Name)
	d.SupertestMethod = strings.ToLower(it.Symbol.Method)
	switch it.Symbol.Language {
	case "ts":
		d.ImportPath = relativeImport(it.OutPath, it.Symbol.File)
		d.AppImportPath = relativeImport(it.OutPath, deriveAppEntry(workDir, it.Symbol.File))
	case "python":
		d.ImportPath = pythonModule(it.Symbol.File)
		d.AppImportPath = pythonModule(deriveAppEntry(workDir, it.Symbol.File))
	case "go":
		d.Package = goPackageFor(it.OutPath)
	case "java":
		d.Package = javaPackageFor(it.OutPath)
	}
	return d
}

func happyArgs(s ast.Symbol) string {
	parts := make([]string, 0, len(s.Params))
	for _, p := range s.Params {
		parts = append(parts, defaultForType(s.Language, p.Type))
	}
	return strings.Join(parts, ", ")
}

func defaultForType(lang, typ string) string {
	t := strings.ToLower(strings.TrimSpace(typ))
	switch lang {
	case "ts":
		return defaultForTS(t)
	case "python":
		return defaultForPython(t)
	case "go":
		return defaultForGo(t)
	case "java":
		return defaultForJava(t)
	}
	return "null"
}

func defaultForTS(t string) string {
	switch {
	case t == "" || strings.Contains(t, "any") || strings.Contains(t, "unknown"):
		return "undefined"
	case strings.Contains(t, "number") || strings.Contains(t, "int") || strings.Contains(t, "float"):
		return "0"
	case strings.Contains(t, "string"):
		return `''`
	case strings.Contains(t, "bool"):
		return "false"
	case strings.HasPrefix(t, "array<") || strings.HasSuffix(t, "[]"):
		return "[]"
	}
	return "undefined"
}

func defaultForPython(t string) string {
	switch {
	case strings.Contains(t, "int"):
		return "0"
	case strings.Contains(t, "float"):
		return "0.0"
	case strings.Contains(t, "str"):
		return `""`
	case strings.Contains(t, "bool"):
		return "False"
	case strings.Contains(t, "list"):
		return "[]"
	case strings.Contains(t, "dict"):
		return "{}"
	}
	return "None"
}

func defaultForGo(t string) string {
	switch t {
	case "string":
		return `""`
	case "int", "int32", "int64", "uint", "uint32", "uint64", "byte", "rune",
		"float32", "float64":
		return "0"
	case "bool":
		return "false"
	}
	return "nil"
}

func defaultForJava(t string) string {
	switch t {
	case "int", "long", "short", "byte":
		return "0"
	case "double", "float":
		return "0.0"
	case "boolean":
		return "false"
	case "string":
		return `""`
	}
	return "null"
}

func toSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func relativeImport(testFile, srcFile string) string {
	if srcFile == "" {
		return "../src"
	}
	rel, err := filepath.Rel(filepath.Dir(testFile), srcFile)
	if err != nil {
		return strings.TrimSuffix(srcFile, filepath.Ext(srcFile))
	}
	rel = strings.TrimSuffix(rel, filepath.Ext(rel))
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func pythonModule(srcFile string) string {
	rel := strings.TrimSuffix(filepath.ToSlash(srcFile), ".py")
	return strings.ReplaceAll(rel, "/", ".")
}

func deriveAppEntry(workDir, source string) string {
	for _, c := range []string{"src/app.ts", "src/app.js", "src/index.ts", "src/server.ts", "app/main.py", "main.py"} {
		if _, err := os.Stat(filepath.Join(workDir, c)); err == nil {
			return c
		}
	}
	return source
}

func goPackageFor(testPath string) string {
	parts := strings.Split(filepath.ToSlash(testPath), "/")
	if len(parts) < 2 {
		return "main"
	}
	return parts[len(parts)-2]
}

func javaPackageFor(testPath string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(testPath), "src/test/java/")
	dir := path.Dir(rel)
	if dir == "." || dir == "" {
		return "tests"
	}
	return strings.ReplaceAll(dir, "/", ".")
}
