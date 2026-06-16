// Package mindmap models a website as a graph after probe-crawling it:
// nodes are pages, edges are same-origin links between them. Each node is
// tagged by shape (landing / form / list / detail) so the journey layer
// can pick paths that exercise different user goals.
//
// The crawler is bounded (BFS, same-origin only, max-depth + max-pages
// caps) and reuses the same SSRF-guarded Fetch the single-URL probe
// uses. No JavaScript runtime — we read whatever the server emits.
package mindmap

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

// Map is the result of a crawl.
type Map struct {
	Origin string // base origin (scheme://host)
	Pages  map[string]*Page
	Order  []string // crawl order, useful for deterministic journey emission
}

// Page is a single crawled URL plus everything we extracted from its HTML.
type Page struct {
	URL          string
	Title        string
	Anchors      []ast.LocatorAnchor
	Inputs       []ast.FormInput
	Links        []ast.LocatorAnchor
	Contents     []ast.ContentAnchor
	Interactions []ast.Interaction
	HasForm      bool
	Tags         []string // landing | form | list | detail | …
}

// Fetcher abstracts plan/probe's Fetch — injected so the mindmap package
// stays decoupled from net/http.
type Fetcher func(ctx context.Context, url string) ([]byte, string, error)

// Options control the crawl bounds.
type Options struct {
	MaxPages int // hard cap on pages crawled (default 10)
	MaxDepth int // BFS depth from origin (default 2)
}

func (o Options) withDefaults() Options {
	if o.MaxPages <= 0 {
		o.MaxPages = 20
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = 3
	}
	return o
}

// Crawl walks the site from origin with the given Fetcher and bounds.
// Returns a Map populated with pages + their tags.
func Crawl(ctx context.Context, origin string, fetch Fetcher, opts Options) (*Map, []error) {
	opts = opts.withDefaults()
	originURL, err := url.Parse(origin)
	if err != nil {
		return nil, []error{fmt.Errorf("mindmap: parse origin %q: %w", origin, err)}
	}
	out := &Map{
		Origin: originURL.Scheme + "://" + originURL.Host,
		Pages:  map[string]*Page{},
	}
	type queued struct {
		url   string
		depth int
	}
	queue := []queued{{url: canonicalURL(origin), depth: 0}}
	// Seed the BFS with sitemap-discovered URLs at depth=1. Sitemap entries
	// are the site's own declaration of "pages that matter" — much higher
	// signal than third-level link discoveries the homepage didn't link to.
	for _, u := range discoverSitemapURLs(ctx, out.Origin, fetch) {
		queue = append(queue, queued{url: u, depth: 1})
	}
	var errs []error
	for len(queue) > 0 && len(out.Pages) < opts.MaxPages {
		head := queue[0]
		queue = queue[1:]
		if _, seen := out.Pages[head.url]; seen {
			continue
		}
		body, finalURL, err := fetch(ctx, head.url)
		if err != nil {
			errs = append(errs, fmt.Errorf("mindmap: fetch %s: %w", head.url, err))
			continue
		}
		finalURL = canonicalURL(finalURL)
		if _, seen := out.Pages[finalURL]; seen {
			continue
		}
		page := buildPage(finalURL, body)
		out.Pages[finalURL] = page
		out.Order = append(out.Order, finalURL)
		if head.depth >= opts.MaxDepth {
			continue
		}
		for _, l := range page.Links {
			abs := absoluteSameOrigin(out.Origin, finalURL, l.Aria)
			if abs == "" {
				continue
			}
			if _, seen := out.Pages[abs]; seen {
				continue
			}
			queue = append(queue, queued{url: abs, depth: head.depth + 1})
		}
	}
	return out, errs
}

func buildPage(u string, html []byte) *Page {
	p := &Page{URL: u}
	p.Anchors = ast.DedupAnchors(plan.ExtractHTMLAnchors(u, html))
	p.Inputs = ast.DedupInputs(plan.ExtractHTMLInputs(u, html))
	p.Links = ast.DedupLinks(plan.ExtractHTMLLinks(u, html))
	p.Contents = plan.ExtractContentAnchors(html)
	p.Interactions = plan.ExtractHTMLInteractions(u, html)
	p.Title = plan.PageTitle(html)
	p.HasForm = strings.Contains(strings.ToLower(string(html)), "<form")
	p.Tags = tagPage(p, html)
	return p
}

// canonicalURL normalises a URL so that "https://x/" and "https://x"
// (or "/blog" and "/blog/") collapse to the same key. Strips trailing
// slash except when the path itself is empty.
func canonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	if len(u.Path) > 1 && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

// absoluteSameOrigin resolves a relative href against a base URL and
// returns the resolved string ONLY when the result is same-origin. Any
// off-origin or non-http(s) result returns "".
func absoluteSameOrigin(originRoot, baseURL, href string) string {
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	if strings.HasPrefix(href, "mailto:") || strings.HasPrefix(href, "tel:") {
		return ""
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	target, err := base.Parse(href)
	if err != nil {
		return ""
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return ""
	}
	if (target.Scheme + "://" + target.Host) != originRoot {
		return ""
	}
	// Drop query + fragment — most sites use them for SPA state, not
	// distinct pages worth probing.
	target.RawQuery = ""
	target.Fragment = ""
	// Skip avoid-paths (legal/cookie/sitemap noise).
	if isAvoidedPath(strings.ToLower(target.Path)) {
		return ""
	}
	return canonicalURL(target.String())
}

func isAvoidedPath(p string) bool {
	for _, s := range []string{"privacy", "terms", "cookie", "legal", "sitemap", "rss", "feed"} {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}
