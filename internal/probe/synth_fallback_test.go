package probe

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/mindmap"
)

func TestSynthesiseFallbackJourneys_EmitsOneFromLanding(t *testing.T) {
	landingURL := "https://example.com/"
	subURL := "https://example.com/about"
	m := &mindmap.Map{
		Origin: landingURL,
		Order:  []string{landingURL, subURL},
		Pages: map[string]*mindmap.Page{
			landingURL: {
				URL:  landingURL,
				Tags: []string{mindmap.TagLanding},
				Links: []ast.LocatorAnchor{
					{Aria: subURL, Text: "About"},
					{Aria: "https://example.com/contact", Text: "Contact"},
					{Aria: "https://example.com/services", Text: "Services"},
				},
			},
			subURL: {URL: subURL},
		},
	}
	js := synthesiseFallbackJourneys(m, landingURL)
	if len(js) != 1 {
		t.Fatalf("got %d journeys, want 1: %+v", len(js), js)
	}
	if js[0].Kind != mindmap.JourneyDiscover {
		t.Errorf("kind = %q, want discover", js[0].Kind)
	}
	if len(js[0].Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(js[0].Steps))
	}
	if js[0].Steps[0].Page.URL != landingURL || js[0].Steps[1].Page.URL != subURL {
		t.Errorf("steps mis-routed: %+v", js[0].Steps)
	}
}

// With a single-page crawl (no second crawled URL to land on)
// the fallback bails — better to honestly emit no journey than a
// landing→landing loop.
func TestSynthesiseFallbackJourneys_BailsWhenSinglePage(t *testing.T) {
	m := &mindmap.Map{
		Origin: "https://example.com/",
		Order:  []string{"https://example.com/"},
		Pages: map[string]*mindmap.Page{
			"https://example.com/": {
				URL:   "https://example.com/",
				Tags:  []string{mindmap.TagLanding},
				Links: []ast.LocatorAnchor{{Aria: "/x"}, {Aria: "/y"}},
			},
		},
	}
	if js := synthesiseFallbackJourneys(m, "https://example.com/"); len(js) != 0 {
		t.Errorf("expected nil for single-page crawl, got %+v", js)
	}
}

// v0.87.1 — when landing.Links don't match m.Pages keys (the
// common SPA case where hrefs don't round-trip exactly), the
// fallback still fires by walking the crawl order.
func TestSynthesiseFallbackJourneys_WorksWhenLinksDontMatchPageKeys(t *testing.T) {
	landing := "https://example.com/"
	sub := "https://example.com/about"
	m := &mindmap.Map{
		Origin: landing,
		Order:  []string{landing, sub},
		Pages: map[string]*mindmap.Page{
			landing: {
				URL:  landing,
				Tags: []string{mindmap.TagLanding},
				// Links point to relative paths that DON'T match
				// the absolute keys in m.Pages.
				Links: []ast.LocatorAnchor{{Aria: "/about"}, {Aria: "/contact"}},
			},
			sub: {URL: sub},
		},
	}
	js := synthesiseFallbackJourneys(m, landing)
	if len(js) != 1 || js[0].Steps[1].Page.URL != sub {
		t.Fatalf("expected fallback to sub via crawl order; got %+v", js)
	}
}
