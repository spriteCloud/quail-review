package mindmap

import (
	"context"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
)

// fakeFetcher returns canned HTML per URL — lets us write deterministic
// tests without a real HTTP server.
type fakeFetcher map[string]string

func (f fakeFetcher) fetch(_ context.Context, requested string) ([]byte, string, error) {
	if body, ok := f[requested]; ok {
		return []byte(body), requested, nil
	}
	// Be tolerant of trailing-slash differences between fixture keys and
	// canonicalised crawl URLs.
	alt := requested + "/"
	if body, ok := f[alt]; ok {
		return []byte(body), requested, nil
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
	tags := tagPage(p, nil)
	if !contains(tags, TagForm) {
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
	tags := tagPage(p, nil)
	if !contains(tags, TagList) {
		t.Errorf("expected TagList; got %+v", tags)
	}
}

func TestTagPage_DetailDetected(t *testing.T) {
	p := &Page{
		URL:      "https://x.test/blog/post",
		Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Single Post Heading"}},
	}
	tags := tagPage(p, nil)
	if !contains(tags, TagDetail) {
		t.Errorf("expected TagDetail; got %+v", tags)
	}
}

func TestTagPage_PricingByURL(t *testing.T) {
	p := &Page{URL: "https://x.test/pricing"}
	if !contains(tagPage(p, nil), TagPricing) {
		t.Error("expected TagPricing from /pricing URL")
	}
}

func TestTagPage_ContactNeedsForm(t *testing.T) {
	withForm := &Page{
		URL:     "https://x.test/contact",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "msg", Type: "textarea", Required: true}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if !contains(tagPage(withForm, nil), TagContact) {
		t.Error("expected TagContact when contact URL has form")
	}
	noForm := &Page{URL: "https://x.test/contact"}
	if contains(tagPage(noForm, nil), TagContact) {
		t.Error("did not expect TagContact without form")
	}
}

func TestTagPage_Auth(t *testing.T) {
	p := &Page{
		URL:    "https://x.test/login",
		Inputs: []ast.FormInput{{Name: "pw", Type: "password"}},
	}
	if !contains(tagPage(p, nil), TagAuth) {
		t.Error("expected TagAuth")
	}
}

func TestTagPage_Service(t *testing.T) {
	p := &Page{URL: "https://x.test/services/devops"}
	if !contains(tagPage(p, nil), TagService) {
		t.Error("expected TagService")
	}
	// /blog/services-overview-2024 must NOT match.
	bad := &Page{URL: "https://x.test/blog/services-overview-2024"}
	if contains(tagPage(bad, nil), TagService) {
		t.Error("did not expect TagService on /blog/services-* path")
	}
}

func TestTagPage_CaseStudy(t *testing.T) {
	p := &Page{
		URL:      "https://x.test/case-studies/acme",
		Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Acme rollout"}},
	}
	if !contains(tagPage(p, nil), TagCaseStudy) {
		t.Error("expected TagCaseStudy")
	}
}

func TestIdentifyJourneys_NewKinds(t *testing.T) {
	pages := fakeFetcher{
		"https://x.test/": `<html><body><h1>Home</h1>
<a href="/pricing">Pricing</a>
<a href="/contact">Contact</a>
<a href="/services/devops">DevOps services</a>
<a href="/case-studies">Case studies</a>
<a href="/login">Login</a>
</body></html>`,
		"https://x.test/pricing": `<html><body><h1>Plans</h1>
<div>$49/month</div><div>$99/month</div></body></html>`,
		"https://x.test/contact": `<html><body><h1>Contact</h1>
<form><input name="email" type="email" required/>
<textarea name="msg" required></textarea>
<input type="submit" value="Send"/></form></body></html>`,
		"https://x.test/services/devops": `<html><body><h1>DevOps</h1>
<a href="/contact">Talk to us</a></body></html>`,
		"https://x.test/case-studies": `<html><body><h1>Case studies</h1>
<a href="/case-studies/a">A</a><a href="/case-studies/b">B</a>
<a href="/case-studies/c">C</a><a href="/case-studies/d">D</a>
<a href="/case-studies/e">E</a><a href="/case-studies/f">F</a></body></html>`,
		"https://x.test/case-studies/a": `<html><body><h1>Case A</h1></body></html>`,
		"https://x.test/case-studies/b": `<html><body><h1>Case B</h1></body></html>`,
		"https://x.test/case-studies/c": `<html><body><h1>Case C</h1></body></html>`,
		"https://x.test/case-studies/d": `<html><body><h1>Case D</h1></body></html>`,
		"https://x.test/case-studies/e": `<html><body><h1>Case E</h1></body></html>`,
		"https://x.test/case-studies/f": `<html><body><h1>Case F</h1></body></html>`,
		"https://x.test/login": `<html><body><h1>Login</h1>
<form><input name="user"/><input name="pw" type="password"/>
<input type="submit"/></form></body></html>`,
	}
	m, _ := Crawl(context.Background(), "https://x.test/", pages.fetch, Options{MaxPages: 30, MaxDepth: 3})
	js := IdentifyJourneys(m, 3)
	kinds := map[JourneyKind]bool{}
	for _, j := range js {
		kinds[j.Kind] = true
	}
	want := []JourneyKind{JourneyEvaluate, JourneyContact, JourneyResearch, JourneyDiscover}
	for _, k := range want {
		if !kinds[k] {
			t.Errorf("expected journey kind %q; got %+v", k, kinds)
		}
	}
}

func TestDedupJourneys_HigherPriorityWins(t *testing.T) {
	page := &Page{URL: "https://x.test/x"}
	in := []Journey{
		{Kind: JourneyExplore, Steps: []Step{{Page: page}}},
		{Kind: JourneyConvert, Steps: []Step{{Page: page}}},
	}
	out := dedupJourneys(in)
	if len(out) != 1 || out[0].Kind != JourneyConvert {
		t.Errorf("expected single Convert; got %+v", out)
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
func TestCrawl(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		Crawl(nil, "", nil, nil)
	})
}
