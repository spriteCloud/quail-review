package mindmap

import (
	"context"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// fakeFetcher returns canned HTML per URL — lets us write deterministic
// tests without a real HTTP server.
type fakeFetcher map[string]string

func (f fakeFetcher) fetch(_ context.Context, url string) ([]byte, string, error) {
	if body, ok := f[url]; ok {
		return []byte(body), url, nil
	}
	return nil, "", nil
}

func TestCrawl_RespectsDepthAndPageBounds(t *testing.T) {
	pages := fakeFetcher{
		"https://x.test/":        `<html><body><h1>Home</h1><a href="/a">A</a><a href="/b">B</a></body></html>`,
		"https://x.test/a":       `<html><body><h1>A</h1><a href="/a/inner">A inner</a></body></html>`,
		"https://x.test/b":       `<html><body><h1>B</h1></body></html>`,
		"https://x.test/a/inner": `<html><body><h1>A inner</h1></body></html>`,
	}
	m, errs := Crawl(context.Background(), "https://x.test/", pages.fetch, Options{MaxPages: 10, MaxDepth: 2})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(m.Pages) != 4 {
		t.Errorf("expected 4 pages, got %d", len(m.Pages))
	}
	m2, _ := Crawl(context.Background(), "https://x.test/", pages.fetch, Options{MaxPages: 10, MaxDepth: 1})
	if len(m2.Pages) != 3 {
		t.Errorf("expected 3 pages at MaxDepth=1, got %d", len(m2.Pages))
	}
}

func TestCrawl_StaysOnSameOrigin(t *testing.T) {
	pages := fakeFetcher{
		"https://x.test/":      `<html><body><h1>Home</h1><a href="https://y.test/external">External</a><a href="/about">About</a></body></html>`,
		"https://x.test/about": `<html><body><h1>About</h1></body></html>`,
	}
	m, _ := Crawl(context.Background(), "https://x.test/", pages.fetch, Options{})
	if _, ok := m.Pages["https://y.test/external"]; ok {
		t.Error("off-origin URL must not be crawled")
	}
}

func TestTagPage_FormDetected(t *testing.T) {
	p := &Page{
		URL:     "https://x.test/",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Type: "email", Required: true}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit", TestID: "submit-btn"}},
	}
	tags := tagPage(p)
	if !hasTag(p, TagForm) && !contains(tags, TagForm) {
		t.Errorf("expected TagForm; got %+v", tags)
	}
	if !contains(tags, TagLanding) {
		t.Errorf("expected TagLanding for root URL; got %+v", tags)
	}
}

func TestTagPage_ListDetected(t *testing.T) {
	var links []ast.LocatorAnchor
	for _, slug := range []string{"a", "b", "c", "d", "e", "f"} {
		links = append(links, ast.LocatorAnchor{Tag: "link-a", Aria: "/blog/" + slug})
	}
	p := &Page{URL: "https://x.test/blog", Links: links}
	tags := tagPage(p)
	if !contains(tags, TagList) {
		t.Errorf("expected TagList; got %+v", tags)
	}
}

func TestTagPage_DetailDetected(t *testing.T) {
	p := &Page{
		URL:      "https://x.test/blog/post",
		Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Single Post Heading"}},
	}
	tags := tagPage(p)
	if !contains(tags, TagDetail) {
		t.Errorf("expected TagDetail; got %+v", tags)
	}
}

func TestIdentifyJourneys_ProducesConvertAndBrowse(t *testing.T) {
	pages := fakeFetcher{
		"https://x.test/": `<html><body><h1>Home</h1>
<form><input type="email" name="email" required/><input type="submit" value="Go"/></form>
<a href="/blog">Blog</a></body></html>`,
		"https://x.test/blog": `<html><body><h1>Blog</h1>
<a href="/blog/a">A</a><a href="/blog/b">B</a><a href="/blog/c">C</a>
<a href="/blog/d">D</a><a href="/blog/e">E</a><a href="/blog/f">F</a></body></html>`,
		"https://x.test/blog/a": `<html><body><h1>Post A</h1></body></html>`,
		"https://x.test/blog/b": `<html><body><h1>Post B</h1></body></html>`,
		"https://x.test/blog/c": `<html><body><h1>Post C</h1></body></html>`,
		"https://x.test/blog/d": `<html><body><h1>Post D</h1></body></html>`,
		"https://x.test/blog/e": `<html><body><h1>Post E</h1></body></html>`,
		"https://x.test/blog/f": `<html><body><h1>Post F</h1></body></html>`,
	}
	m, _ := Crawl(context.Background(), "https://x.test/", pages.fetch, Options{MaxPages: 20, MaxDepth: 2})
	js := IdentifyJourneys(m, 2)
	gotKinds := map[JourneyKind]bool{}
	for _, j := range js {
		gotKinds[j.Kind] = true
	}
	if !gotKinds[JourneyConvert] {
		t.Errorf("expected a Convert journey; got kinds %+v", gotKinds)
	}
	if !gotKinds[JourneyBrowse] {
		t.Errorf("expected a Browse journey; got kinds %+v", gotKinds)
	}
}

func contains(ts []string, t string) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}
