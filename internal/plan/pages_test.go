package plan

import (
	"os"
	"path/filepath"
	"testing"

	_ "github.com/spriteCloud/quail/internal/ast/ts"
	"github.com/spriteCloud/quail/internal/ast"
	"github.com/spriteCloud/quail/internal/diff"
)

func write(t *testing.T, dir, rel, body string) string {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestHTMLPageExtractsInputs(t *testing.T) {
	dir := t.TempDir()
	html := `<!doctype html><html><body>
<form data-testid="signup">
  <input type="email" name="email" required />
  <input type="password" name="password" required />
  <button type="submit">Go</button>
</form>
<a href="/about">About</a>
</body></html>
`
	indexPath := write(t, dir, "index.html", html)
	files := []diff.File{
		{Path: indexPath, Added: []diff.Range{{Start: 1, End: 30}}, Status: "modified", NewBlob: html},
	}
	items := Build(files, Detect(dir))
	var flow *Item
	for i := range items {
		if items[i].Template == TmplPlaywrightFeature {
			flow = &items[i]
		}
	}
	if flow == nil {
		t.Fatalf("no feature item; items = %+v", items)
	}
	if len(flow.Symbols) != 1 {
		t.Fatalf("expected 1 synthetic symbol, got %d", len(flow.Symbols))
	}
	syn := flow.Symbols[0]
	if !syn.HasForm {
		t.Error("HasForm should be true on synthetic HTML symbol")
	}
	if len(syn.Inputs) != 2 {
		t.Errorf("expected 2 inputs, got %d: %+v", len(syn.Inputs), syn.Inputs)
	}
	if len(syn.Links) != 1 || syn.Links[0].Aria != "/about" {
		t.Errorf("expected one /about link, got %+v", syn.Links)
	}
}

func TestChainMultiStep_DropsExternalLinks(t *testing.T) {
	items := []Item{
		{
			Template: TmplPlaywrightHappyFlow,
			PageURL:  "/home",
			Symbol:   ast.Symbol{Name: "Home"},
			Symbols: []ast.Symbol{{
				Name: "Home",
				Links: []ast.LocatorAnchor{
					{Aria: "/about", Tag: "link-a"},
					{Aria: "https://external.com", Tag: "link-a"},
				},
			}},
		},
		{
			Template: TmplPlaywrightHappyFlow,
			PageURL:  "/about",
			Symbol:   ast.Symbol{Name: "About"},
			Symbols:  []ast.Symbol{{Name: "About"}},
		},
	}
	out := chainMultiStep(items)
	home := out[0].Symbols[0]
	if len(home.Links) != 1 || home.Links[0].Aria != "/about" {
		t.Errorf("expected only /about to survive, got %+v", home.Links)
	}
}

func TestPageRootDetectionAcrossFrameworks(t *testing.T) {
	cases := []struct {
		name      string
		pagePath  string
		body      string
		wantURL   string
		wantStem  string
	}{
		{
			name:     "next-pages",
			pagePath: "pages/login.tsx",
			body:     `<form data-testid="login-form"><input type="email" name="email" /></form>`,
			wantURL:  "/login",
			wantStem: "login",
		},
		{
			name:     "next-app",
			pagePath: "app/dashboard/page.tsx",
			body:     `<form data-testid="d-form"><input type="text" name="q" /></form>`,
			wantURL:  "/dashboard",
			wantStem: "page",
		},
		{
			name:     "remix",
			pagePath: "app/routes/welcome.tsx",
			body:     `<form><input type="email" name="email" /></form>`,
			wantURL:  "/welcome",
			wantStem: "welcome",
		},
		{
			name:     "sveltekit",
			pagePath: "src/routes/profile/+page.svelte",
			body:     `<form><input type="text" name="bio" /></form>`,
			wantURL:  "/profile",
			wantStem: "+page",
		},
		{
			name:     "vue-pages",
			pagePath: "pages/Contact.vue",
			body:     `<template><form><input type="email" name="email" /></form></template>`,
			wantURL:  "/contact",
			wantStem: "Contact",
		},
		{
			name:     "rails-erb",
			pagePath: "app/views/sessions/new.html.erb",
			body:     `<form action="/sessions"><input type="email" name="email" /></form>`,
			wantURL:  "/",
			wantStem: "new.html",
		},
		{
			name:     "plain-html",
			pagePath: "index.html",
			body:     `<form><input type="email" name="email" /></form>`,
			wantURL:  "/",
			wantStem: "index",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.pagePath, tc.body)
			files := []diff.File{
				{Path: tc.pagePath, Added: []diff.Range{{Start: 1, End: 20}}, Status: "modified", NewBlob: tc.body},
			}
			items := Build(files, Detect(dir))
			var flow *Item
			for i := range items {
				if items[i].Template == TmplPlaywrightFeature {
					flow = &items[i]
				}
			}
			if flow == nil {
				t.Fatalf("no feature item for %s; items=%+v", tc.name, items)
			}
			if flow.PageURL != tc.wantURL {
				t.Errorf("PageURL = %q, want %q", flow.PageURL, tc.wantURL)
			}
		})
	}
}

func TestResolveLinkHref(t *testing.T) {
	tests := []struct {
		name, base, href, want string
	}{
		{"leading slash kept", "https://x.test/", "/about", "/about"},
		{"relative resolved against base", "https://books.toscrape.com", "catalogue/x.html", "/catalogue/x.html"},
		{"relative with dotdot resolved", "https://x.test/blog/post", "../about", "/about"},
		{"absolute same-origin reduced to path", "https://x.test/", "https://x.test/about", "/about"},
		{"absolute cross-origin dropped", "https://x.test/", "https://other.test/x", ""},
		{"protocol-relative same host kept", "https://x.test/", "//x.test/y", "/y"},
		{"mailto dropped", "https://x.test/", "mailto:foo@bar.com", ""},
		{"tel dropped", "https://x.test/", "tel:+1234", ""},
		{"javascript dropped", "https://x.test/", "javascript:void(0)", ""},
		{"fragment dropped", "https://x.test/", "#section", ""},
		{"empty dropped", "https://x.test/", "", ""},
		{"protocol-relative cross-host dropped", "https://x.test/", "//other.test/y", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveLinkHref(tc.base, tc.href)
			if got != tc.want {
				t.Errorf("resolveLinkHref(%q, %q) = %q; want %q", tc.base, tc.href, got, tc.want)
			}
		})
	}
}

func TestExtractHTMLLinks_ResolvesRelative(t *testing.T) {
	html := []byte(`<html><body>
<a href="catalogue/books_1/index.html">Books</a>
<a href="/about">About</a>
<a href="https://books.toscrape.com/contact">Contact</a>
<a href="https://elsewhere.test/escape">Escape</a>
<a href="mailto:foo@bar.com">Email</a>
</body></html>`)
	links := ExtractHTMLLinks("https://books.toscrape.com", html)
	want := map[string]bool{
		"/catalogue/books_1/index.html": true,
		"/about":                        true,
		"/contact":                      true,
	}
	if len(links) != len(want) {
		t.Fatalf("expected %d links, got %d: %+v", len(want), len(links), links)
	}
	for _, l := range links {
		if !want[l.Aria] {
			t.Errorf("unexpected resolved href %q", l.Aria)
		}
	}
}

func TestPageURLsEnvOverride(t *testing.T) {
	dir := t.TempDir()
	// Put a TSX file at a path the conventional walker won't classify as a
	// page root.
	body := `<form><input type="email" name="email" /></form>`
	write(t, dir, "src/screens/Bespoke.html", body)
	t.Setenv("QUAIL_PAGE_URLS", `{"src/screens/Bespoke.html":"/bespoke"}`)
	files := []diff.File{
		{Path: "src/screens/Bespoke.html", Added: []diff.Range{{Start: 1, End: 5}}, Status: "added", NewBlob: body},
	}
	items := Build(files, Detect(dir))
	var found bool
	for _, it := range items {
		if it.Template == TmplPlaywrightFeature && it.PageURL == "/bespoke" {
			found = true
		}
	}
	if !found {
		t.Errorf("env override not honoured; items = %+v", items)
	}
}
