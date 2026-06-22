package prompt

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/mindmap"
)

func TestFilter_ApplyByKind(t *testing.T) {
	landing := &mindmap.Page{URL: "https://x.test/"}
	pricing := &mindmap.Page{URL: "https://x.test/pricing"}
	about := &mindmap.Page{URL: "https://x.test/about"}
	in := []mindmap.Journey{
		{Kind: mindmap.JourneyConvert, Steps: []mindmap.Step{{Page: landing}}},
		{Kind: mindmap.JourneyEvaluate, Steps: []mindmap.Step{{Page: landing}, {Page: pricing}}},
		{Kind: mindmap.JourneyExplore, Steps: []mindmap.Step{{Page: landing}, {Page: about}}},
	}
	f := Filter{JourneyKinds: []mindmap.JourneyKind{mindmap.JourneyEvaluate}}
	out := f.Apply(in)
	if len(out) != 1 || out[0].Kind != mindmap.JourneyEvaluate {
		t.Errorf("expected only Evaluate journey; got %+v", out)
	}
}

func TestFilter_ApplyByPathHint(t *testing.T) {
	landing := &mindmap.Page{URL: "https://x.test/"}
	checkout := &mindmap.Page{URL: "https://x.test/checkout"}
	in := []mindmap.Journey{
		{Kind: mindmap.JourneyExplore, Steps: []mindmap.Step{{Page: landing}, {Page: checkout}}},
		{Kind: mindmap.JourneyExplore, Steps: []mindmap.Step{{Page: landing}, {Page: &mindmap.Page{URL: "https://x.test/about"}}}},
	}
	f := Filter{PathHints: []string{"/checkout"}}
	out := f.Apply(in)
	if len(out) != 1 || out[0].Steps[1].Page.URL != "https://x.test/checkout" {
		t.Errorf("expected only checkout journey; got %+v", out)
	}
}

func TestFilter_ApplyByKeywordOnTitleOrPath(t *testing.T) {
	landing := &mindmap.Page{URL: "https://x.test/"}
	docsPage := &mindmap.Page{URL: "https://x.test/help", Title: "Documentation Center"}
	in := []mindmap.Journey{
		{Kind: mindmap.JourneyExplore, Steps: []mindmap.Step{{Page: landing}, {Page: docsPage}}},
		{Kind: mindmap.JourneyExplore, Steps: []mindmap.Step{{Page: landing}, {Page: &mindmap.Page{URL: "https://x.test/about", Title: "About Us"}}}},
	}
	// "documentation" doesn't map to a kind, but it should match the
	// title via keyword check.
	f := Filter{Keywords: []string{"documentation"}}
	out := f.Apply(in)
	if len(out) != 1 || out[0].Steps[1].Page.Title != "Documentation Center" {
		t.Errorf("expected documentation match; got %+v", out)
	}
}

func TestFilter_EmptyFilterIsPassthrough(t *testing.T) {
	in := []mindmap.Journey{
		{Kind: mindmap.JourneyConvert, Steps: []mindmap.Step{{Page: &mindmap.Page{URL: "https://x.test/"}}}},
	}
	out := Filter{}.Apply(in)
	if len(out) != 1 {
		t.Errorf("empty filter must pass everything through; got %d", len(out))
	}
}
