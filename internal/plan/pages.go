package plan

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/diff"
	"github.com/reviewqa/reviewqa/internal/log"
)

// pageRoot is a candidate page file: either a static HTML page or a TSX/JSX
// entry that mounts React components.
type pageRoot struct {
	Path    string              // workdir-relative path
	Stem    string              // basename without extension
	URL     string              // derived page URL (e.g. "/", "/home")
	IsHTML  bool                // true for .html / .htm / template-engine pages
	Mounted []string            // component names referenced by JSX usage (for tsx/jsx roots)
	Anchors []ast.LocatorAnchor // direct anchors found in the page source (HTML or JSX)
	Inputs  []ast.FormInput     // form fields detected in this page's markup
	Links   []ast.LocatorAnchor // same-origin links (href / Link to)
	HasForm bool                // <form> element present
}

var (
	reJSXComponentUsage = regexp.MustCompile(`<([A-Z][\w$]*)\b`)
	rePageHTMLTestID    = regexp.MustCompile(`data-testid\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLAria      = regexp.MustCompile(`aria-label\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLRole      = regexp.MustCompile(`role\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLTag       = regexp.MustCompile(`<\s*([a-zA-Z][\w-]*)`)
	rePageHTMLInputType        = regexp.MustCompile(`type\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLInputName        = regexp.MustCompile(`name\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLInputID          = regexp.MustCompile(`\bid\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLInputPlaceholder = regexp.MustCompile(`placeholder\s*=\s*['"]([^'"]+)['"]`)
	rePageHTMLLabelFor         = regexp.MustCompile(`<label[^>]*\bfor\s*=\s*['"]([^'"]+)['"][^>]*>([^<]*)</label>`)
	rePageHTMLRequired         = regexp.MustCompile(`\brequired\b`)
	rePageHTMLHref             = regexp.MustCompile(`href\s*=\s*['"]([^'"]+)['"]`)
)

// findPageRoots walks workDir for candidate page entry files and returns the
// detected roots with their mounted component names and anchors prepopulated.
// Walks are bounded — depth ≤ 6, common artifact dirs skipped.
func findPageRoots(workDir string) []pageRoot {
	if workDir == "" {
		workDir = "."
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		abs = workDir
	}
	skip := map[string]bool{
		"node_modules": true, "dist": true, "build": true, ".next": true,
		"vendor": true, ".git": true, "target": true, "out": true,
	}
	var roots []pageRoot
	_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(abs, path)
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if depth > 6 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !isPageFile(rel, d.Name()) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !looksLikeMarkup(content) {
			return nil // matched the path heuristic but carries no HTML/JSX — skip
		}
		stem := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		root := pageRoot{
			Path:   filepath.ToSlash(rel),
			Stem:   stem,
			URL:    deriveURL(filepath.ToSlash(rel), stem),
			IsHTML: isHTMLExt(d.Name()),
		}
		// Always extract markup signals; the regex set ignores templating
		// engine syntax. For TSX/JSX, additionally collect mounted-component
		// names for grouping.
		root.Anchors = ExtractHTMLAnchors(root.Path, content)
		root.Inputs = ExtractHTMLInputs(root.Path, content)
		root.Links = ExtractHTMLLinks(root.Path, content)
		root.HasForm = bytes.Contains(bytes.ToLower(content), []byte("<form"))
		if !root.IsHTML {
			root.Mounted = extractJSXComponentNames(content)
		}
		roots = append(roots, root)
		return nil
	})
	sort.Slice(roots, func(i, j int) bool {
		return len(roots[i].Path) < len(roots[j].Path)
	})
	return roots
}

// isPageFile reports whether a file is a candidate page root across the major
// web frameworks. The strategy is broad on purpose — when in doubt we treat
// the file as a candidate and let the markup heuristic in the walker decide.
func isPageFile(rel, name string) bool {
	slash := "/" + filepath.ToSlash(rel)
	lower := strings.ToLower(name)
	if isWellKnownEntry(lower) {
		return true
	}
	if isFrameworkPageByPath(slash, lower) {
		return true
	}
	return isPlainHTMLLikeFile(lower)
}

func isWellKnownEntry(lowerName string) bool {
	switch lowerName {
	case "index.html", "index.htm",
		"index.tsx", "index.jsx",
		"app.tsx", "app.jsx", "app.vue", "app.svelte",
		"routes.tsx", "routes.jsx":
		return true
	}
	return false
}

// frameworkPageRule pairs a directory contains-check with a file-name match.
// First matching rule wins.
type frameworkPageRule struct {
	dirContains string
	matches     func(name string) bool
}

var frameworkPageRules = []frameworkPageRule{
	{"/pages/", func(n string) bool { return hasAnySuffix(n, ".tsx", ".jsx", ".ts", ".js", ".vue", ".astro") }},
	{"/app/", func(n string) bool { return n == "page.tsx" || n == "page.jsx" || n == "page.ts" || n == "page.js" }},
	{"/app/routes/", func(n string) bool { return hasAnySuffix(n, ".tsx", ".jsx", ".ts", ".js") }},
	{"/routes/", func(n string) bool { return strings.HasPrefix(n, "+page.") }},
	{"/src/pages/", func(n string) bool { return strings.HasSuffix(n, ".astro") }},
	{"/views/", func(n string) bool { return strings.HasSuffix(n, ".vue") }},
}

func isFrameworkPageByPath(slash, lower string) bool {
	for _, r := range frameworkPageRules {
		if strings.Contains(slash, r.dirContains) && r.matches(lower) {
			return true
		}
	}
	return false
}

func isPlainHTMLLikeFile(lowerName string) bool {
	return hasAnySuffix(lowerName,
		".html", ".htm",
		".erb", ".html.erb", ".haml",
		".blade.php",
		".jinja", ".j2",
		".hbs", ".handlebars",
		".tmpl", ".gohtml", ".html.tmpl",
		".astro",
	)
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

func isHTMLExt(name string) bool {
	lower := strings.ToLower(name)
	return isPlainHTMLLikeFile(lower)
}

// looksLikeMarkup is a cheap byte gate — true when the file content carries
// HTML-shaped tags. Used to skip non-template source quickly.
func looksLikeMarkup(content []byte) bool {
	// Heuristic: an opening '<' followed by a letter, anywhere in the first 4KB.
	limit := min(len(content), 4096)
	for i := 0; i < limit-1; i++ {
		if content[i] != '<' {
			continue
		}
		c := content[i+1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// deriveURL maps a page file path to its canonical relative URL.
//
//	index.html              -> "/"
//	pages/index.tsx         -> "/"
//	pages/About.tsx         -> "/about"
//	app/dashboard/page.tsx  -> "/dashboard"
// urlRules maps a page-file path to its canonical relative URL. First match
// wins; order matters (most-specific first).
var urlRules = []struct {
	match func(lower string) bool
	build func(lower, stem string) string
}{
	{matchIndexLike, returnRoot},
	{matchSpaShell, returnRoot},
	{matchAppDirPage, deriveAppDirURL},
	{matchRemixRoute, deriveRemixURL},
	{matchSvelteKitPage, deriveSvelteKitURL},
	{matchPagesDir, derivePagesDirURL},
}

func deriveURL(slashPath, stem string) string {
	lower := "/" + strings.ToLower(slashPath)
	for _, r := range urlRules {
		if r.match(lower) {
			return r.build(lower, stem)
		}
	}
	return "/"
}

func matchIndexLike(l string) bool {
	return hasAnySuffix(l, "/index.html", "/pages/index.tsx", "/pages/index.jsx")
}

func matchSpaShell(l string) bool {
	return hasAnySuffix(l, "/app.tsx", "/app.jsx", "/routes.tsx", "/routes.jsx")
}

func matchAppDirPage(l string) bool {
	return strings.Contains(l, "/app/") && hasAnySuffix(l, "/page.tsx", "/page.jsx")
}

func matchPagesDir(l string) bool {
	return strings.Contains(l, "/pages/")
}

func matchRemixRoute(l string) bool {
	return strings.Contains(l, "/app/routes/")
}

func matchSvelteKitPage(l string) bool {
	return strings.Contains(l, "/routes/") && strings.Contains(l, "+page.")
}

// deriveRemixURL: "/app/routes/welcome.tsx" → "/welcome".
// "/app/routes/auth/login.tsx" → "/auth/login".
func deriveRemixURL(lower, _ string) string {
	i := strings.Index(lower, "/app/routes/")
	if i == -1 {
		return "/"
	}
	rest := lower[i+len("/app/routes/"):]
	// Strip trailing file ext.
	for {
		ext := filepath.Ext(rest)
		if ext == "" {
			break
		}
		rest = strings.TrimSuffix(rest, ext)
	}
	if rest == "" {
		return "/"
	}
	return "/" + rest
}

// deriveSvelteKitURL: "/src/routes/profile/+page.svelte" → "/profile".
func deriveSvelteKitURL(lower, _ string) string {
	i := strings.Index(lower, "/routes/")
	if i == -1 {
		return "/"
	}
	rest := lower[i+len("/routes/"):]
	rest = strings.TrimSuffix(rest, "/+page.svelte")
	rest = strings.TrimSuffix(rest, "/+page.ts")
	rest = strings.TrimSuffix(rest, "/+page.js")
	if rest == "" || strings.HasPrefix(rest, "+page.") {
		return "/"
	}
	return "/" + rest
}

func returnRoot(_, _ string) string { return "/" }

func deriveAppDirURL(lower, _ string) string {
	i := strings.Index(lower, "/app/")
	if i == -1 {
		return "/"
	}
	dir := lower[i+len("/app/"):]
	dir = strings.TrimSuffix(dir, "/page.tsx")
	dir = strings.TrimSuffix(dir, "/page.jsx")
	if dir == "" {
		return "/"
	}
	return "/" + dir
}

func derivePagesDirURL(_ string, stem string) string {
	return "/" + strings.ToLower(stem)
}

func extractJSXComponentNames(content []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reJSXComponentUsage.FindAllSubmatch(content, -1) {
		name := string(m[1])
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func ExtractHTMLAnchors(file string, content []byte) []ast.LocatorAnchor {
	var anchors []ast.LocatorAnchor
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		tag := ""
		if m := rePageHTMLTag.FindStringSubmatch(line); m != nil {
			tag = strings.ToLower(m[1])
		}
		if m := rePageHTMLTestID.FindStringSubmatch(line); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{TestID: m[1], File: file, Line: i + 1, Tag: tag})
		}
		if m := rePageHTMLAria.FindStringSubmatch(line); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{Aria: m[1], File: file, Line: i + 1, Tag: tag})
		}
		if m := rePageHTMLRole.FindStringSubmatch(line); m != nil {
			anchors = append(anchors, ast.LocatorAnchor{Role: m[1], File: file, Line: i + 1, Tag: tag})
		}
		// Submit-capable element on this line — either <button type="submit">,
		// <input type="submit">, or <input type="image">. Emit an anchor with
		// Tag="submit" and the strongest locator we can derive (testid, then
		// aria-label, then accessible name from value=/button-text).
		for _, m := range reSubmitElementOpen.FindAllStringSubmatch(line, -1) {
			attrs := m[2]
			anchor := ast.LocatorAnchor{File: file, Line: i + 1, Tag: "submit"}
			switch {
			case rePageHTMLTestID.MatchString(attrs):
				anchor.TestID = rePageHTMLTestID.FindStringSubmatch(attrs)[1]
			case rePageHTMLAria.MatchString(attrs):
				anchor.Aria = rePageHTMLAria.FindStringSubmatch(attrs)[1]
			case reInputValue.MatchString(attrs):
				anchor.Name = reInputValue.FindStringSubmatch(attrs)[1]
			default:
				if btx := reButtonText.FindStringSubmatch(line); btx != nil {
					anchor.Name = strings.TrimSpace(btx[1])
				}
			}
			anchors = append(anchors, anchor)
		}
	}
	return anchors
}

// reFormElementOpen matches the opening of a form input element. Multi-tag
// lines like `<form><input ... /></form>` need a non-anchored scan because
// the first tag isn't necessarily the input.
var reFormElementOpen = regexp.MustCompile(`<\s*(input|select|textarea)\b([^>]*)>`)

// reLinkOpen matches `<a href="X">` (or hrefless). Allows multiple links per line.
var reLinkOpen = regexp.MustCompile(`<\s*a\s+([^>]*)>`)

// reSubmitElementOpen matches the opening of a submit-capable element —
// either a <button type="submit"> or an <input type="submit"> / <input
// type="image">. Captures the tag and attributes so the caller can read
// `value=`, `data-testid=`, etc.
var reSubmitElementOpen = regexp.MustCompile(`<\s*(button|input)\b([^>]*type\s*=\s*['"](?:submit|image)['"][^>]*)>`)

// reInputValue captures the `value="..."` attribute on a submit input — used
// as the button's accessible name (input[type=submit] has implicit role=button
// with `name` set from `value`).
var reInputValue = regexp.MustCompile(`value\s*=\s*['"]([^'"]+)['"]`)

// reButtonText captures the text content of a single-line <button>...</button>.
var reButtonText = regexp.MustCompile(`<\s*button\b[^>]*>([^<]+)</\s*button\s*>`)

// ExtractHTMLInputs collects form fields from a page's markup. Type/name/
// required must appear inside the opening tag of the input. Placeholder and
// label-for fallbacks are captured to support stable locators on inputs
// without testids.
func ExtractHTMLInputs(file string, content []byte) []ast.FormInput {
	var out []ast.FormInput
	lines := strings.Split(string(content), "\n")
	labelMap := collectHTMLLabelFor(content)
	for i, line := range lines {
		for _, m := range reFormElementOpen.FindAllStringSubmatch(line, -1) {
			tag := strings.ToLower(m[1])
			attrs := m[2]
			fi := ast.FormInput{File: file, Line: i + 1, Tag: tag}
			if tag == "input" {
				if im := rePageHTMLInputType.FindStringSubmatch(attrs); im != nil {
					fi.Type = strings.ToLower(im[1])
				} else {
					fi.Type = "text"
				}
			} else {
				fi.Type = tag
			}
			if nm := rePageHTMLInputName.FindStringSubmatch(attrs); nm != nil {
				fi.Name = nm[1]
			}
			if tm := rePageHTMLTestID.FindStringSubmatch(attrs); tm != nil {
				fi.TestID = tm[1]
			}
			if am := rePageHTMLAria.FindStringSubmatch(attrs); am != nil {
				fi.Aria = am[1]
			}
			if pm := rePageHTMLInputPlaceholder.FindStringSubmatch(attrs); pm != nil {
				fi.Placeholder = pm[1]
			}
			if idm := rePageHTMLInputID.FindStringSubmatch(attrs); idm != nil {
				if lbl, ok := labelMap[idm[1]]; ok {
					fi.LabelText = lbl
				}
			}
			if rePageHTMLRequired.MatchString(attrs) {
				fi.Required = true
			}
			out = append(out, fi)
		}
	}
	return out
}

func collectHTMLLabelFor(content []byte) map[string]string {
	out := map[string]string{}
	for _, m := range rePageHTMLLabelFor.FindAllSubmatch(content, -1) {
		out[string(m[1])] = strings.TrimSpace(string(m[2]))
	}
	return out
}

// ExtractHTMLLinks collects same-origin hrefs found inside <a> tags. Allows
// multiple links per line. Captures the visible anchor text when present
// on the same line — used by the nav-target ranker.
func ExtractHTMLLinks(file string, content []byte) []ast.LocatorAnchor {
	var out []ast.LocatorAnchor
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		for _, m := range reLinkOpen.FindAllStringSubmatch(line, -1) {
			attrs := m[1]
			h := rePageHTMLHref.FindStringSubmatch(attrs)
			if h == nil {
				continue
			}
			anchor := ast.LocatorAnchor{Aria: h[1], File: file, Line: i + 1, Tag: "link-a"}
			if txt := reAnchorText.FindStringSubmatch(line); txt != nil {
				anchor.Text = strings.TrimSpace(txt[1])
			}
			out = append(out, anchor)
		}
	}
	return out
}

// groupByPage post-processes a build's items. It folds component E2E items
// (TmplPlaywrightE2E) into page-scoped happy-flow items where the components
// are mounted on the same page root, and emits a happy-flow item for each
// static HTML page touched by the diff.
func groupByPage(items []Item, files []diff.File, layout Layout) []Item {
	style := os.Getenv("REVIEWQA_E2E_STYLE")
	roots := applyPageURLOverrides(findPageRoots(layout.WorkDir))
	if len(roots) == 0 && style != "page-flow" {
		return items
	}
	nonE2E, ungroupedE2E, grouped := matchComponentsToRoots(items, roots)
	out := append([]Item{}, nonE2E...)
	out = append(out, materializeGroups(grouped, &ungroupedE2E, style)...)
	out = append(out, ungroupedE2E...)
	out = append(out, materializePageRoots(roots, pathsInDiff(files), itemFilePaths(out))...)
	return chainMultiStep(out)
}

func pathsInDiff(files []diff.File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		if f.Status == "removed" {
			continue
		}
		out[filepath.ToSlash(f.Path)] = true
	}
	return out
}

func itemFilePaths(items []Item) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[filepath.ToSlash(it.Symbol.File)] = true
	}
	return out
}

// group holds components grouped under a page root.
type pageGroup struct {
	root    pageRoot
	symbols []ast.Symbol
}

// matchComponentsToRoots splits items into three buckets:
//   - nonE2E: items not destined for Playwright (unchanged passthrough)
//   - ungroupedE2E: per-component E2E items with no detected page root
//   - grouped: page-root → list of component symbols
func matchComponentsToRoots(items []Item, roots []pageRoot) ([]Item, []Item, map[string]*pageGroup) {
	var nonE2E, ungroupedE2E []Item
	grouped := map[string]*pageGroup{}
	for _, it := range items {
		if it.Template != TmplPlaywrightE2E {
			nonE2E = append(nonE2E, it)
			continue
		}
		matched := findMountingRoot(it.Symbol.Name, roots)
		if matched == nil {
			ungroupedE2E = append(ungroupedE2E, it)
			continue
		}
		g, ok := grouped[matched.Path]
		if !ok {
			g = &pageGroup{root: *matched}
			grouped[matched.Path] = g
		}
		g.symbols = append(g.symbols, it.Symbol)
	}
	return nonE2E, ungroupedE2E, grouped
}

func findMountingRoot(symbolName string, roots []pageRoot) *pageRoot {
	for i := range roots {
		r := &roots[i]
		if r.IsHTML {
			continue
		}
		if mountsComponent(r.Mounted, symbolName) {
			return r
		}
	}
	return nil
}

func mountsComponent(mounted []string, name string) bool {
	return slices.Contains(mounted, name)
}

func materializeGroups(grouped map[string]*pageGroup, ungrouped *[]Item, style string) []Item {
	var out []Item
	for _, g := range grouped {
		if len(g.symbols) < 2 && style != "page-flow" {
			for _, s := range g.symbols {
				*ungrouped = append(*ungrouped, Item{
					Symbol:   s,
					Template: TmplPlaywrightE2E,
					OutPath:  filepath.ToSlash(filepath.Join("tests", "e2e", s.Name+".spec.ts")),
				})
			}
			continue
		}
		sort.Slice(g.symbols, func(i, j int) bool { return g.symbols[i].Line < g.symbols[j].Line })
		out = append(out, Item{
			Symbol:   g.symbols[0],
			Symbols:  g.symbols,
			PageURL:  g.root.URL,
			Template: TmplPlaywrightHappyFlow,
			OutPath:  filepath.ToSlash(filepath.Join("tests", "e2e", g.root.Stem+".spec.ts")),
		})
	}
	return out
}

// materializePageRoots synthesizes one happy-flow Item for each page root
// that is in the diff, carries any anchors/inputs/links, and is not already
// represented by a per-component item (so we don't double-emit for TSX page
// roots that wrap a real component).
func materializePageRoots(roots []pageRoot, inDiff, alreadyCovered map[string]bool) []Item {
	var out []Item
	for _, r := range roots {
		if !inDiff[r.Path] || alreadyCovered[r.Path] {
			continue
		}
		if len(r.Anchors) == 0 && len(r.Inputs) == 0 && len(r.Links) == 0 {
			continue
		}
		synthetic := ast.Symbol{
			Name:     capitalizeFirst(stemOf(r.Stem)),
			Kind:     ast.KindComponent,
			File:     r.Path,
			Language: "ts",
			Anchors:  ast.DedupAnchors(r.Anchors),
			Inputs:   ast.DedupInputs(r.Inputs),
			Links:    ast.DedupLinks(r.Links),
			HasForm:  r.HasForm,
		}
		out = append(out, Item{
			Symbol:   synthetic,
			Symbols:  []ast.Symbol{synthetic},
			PageURL:  r.URL,
			Template: TmplPlaywrightHappyFlow,
			OutPath:  filepath.ToSlash(filepath.Join("tests", "e2e", stemOf(r.Stem)+".spec.ts")),
		})
	}
	return out
}

// stemOf returns the bare basename without compound extensions like
// "login.html" → "login".
func stemOf(s string) string {
	for {
		ext := filepath.Ext(s)
		if ext == "" {
			return s
		}
		s = strings.TrimSuffix(s, ext)
	}
}

// chainMultiStep prunes Links on every page-flow Item so only links pointing
// at another known page-flow URL (or any "/" same-origin) survive. The
// template emits one nav-assertion per surviving link.
func chainMultiStep(items []Item) []Item {
	urls := map[string]bool{}
	for _, it := range items {
		if it.Template == TmplPlaywrightHappyFlow {
			urls[it.PageURL] = true
		}
	}
	if len(urls) == 0 {
		return items
	}
	for idx := range items {
		it := &items[idx]
		if it.Template != TmplPlaywrightHappyFlow {
			continue
		}
		for sIdx := range it.Symbols {
			it.Symbols[sIdx].Links = filterSameOriginLinks(it.Symbols[sIdx].Links, urls)
		}
	}
	return items
}

func filterSameOriginLinks(links []ast.LocatorAnchor, knownURLs map[string]bool) []ast.LocatorAnchor {
	var kept []ast.LocatorAnchor
	for _, l := range links {
		href := l.Aria
		if !strings.HasPrefix(href, "/") {
			continue // external or relative-without-leading-slash; drop
		}
		// Keep if it terminates in a known page-flow URL, or unconditionally
		// kept if it's same-origin (the template handles "expected URL").
		if knownURLs[href] || isSameOriginRelative(href) {
			kept = append(kept, l)
		}
	}
	return kept
}

func isSameOriginRelative(href string) bool {
	return strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//")
}

// applyPageURLOverrides parses REVIEWQA_PAGE_URLS (JSON map of source path →
// URL) and updates matching roots' URL fields. Invalid JSON is logged and
// otherwise ignored.
func applyPageURLOverrides(roots []pageRoot) []pageRoot {
	raw := os.Getenv("REVIEWQA_PAGE_URLS")
	if raw == "" {
		return roots
	}
	var overrides map[string]string
	if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
		log.Warn("REVIEWQA_PAGE_URLS: invalid JSON; ignoring", "err", err)
		return roots
	}
	for i := range roots {
		if u, ok := overrides[roots[i].Path]; ok {
			roots[i].URL = u
		}
	}
	for path, url := range overrides {
		if hasRoot(roots, path) {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		roots = append(roots, pageRoot{Path: path, Stem: stem, URL: url, IsHTML: isHTMLExt(path)})
	}
	return roots
}

func hasRoot(roots []pageRoot, path string) bool {
	for _, r := range roots {
		if r.Path == path {
			return true
		}
	}
	return false
}
