package main

import (
	"testing"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/plan"
)

// TestGrounding_KeepsMatchingSteps verifies that steps whose (role, name)
// or (label) match an anchor on some page in the chain survive the
// grounding filter unchanged.
func TestGrounding_KeepsMatchingSteps(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			PageTitle: "Home",
			Contents:  []ast.ContentAnchor{{Tag: "h1", Text: "Welcome home", AccessibleName: "Welcome home"}},
			Links:     []ast.LocatorAnchor{{Text: "Contact", AccessibleName: "Contact spriteCloud team"}},
			Inputs:    []ast.FormInput{{LabelText: "Email"}},
		}},
	}
	j := plan.Journey{
		Title: "smoke",
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "click", Role: "link", Name: "Contact"},
			{Op: "fill", Label: "Email", Value: "user@example.com"},
			{Op: "seen", Role: "heading", Name: "Welcome home"},
		},
	}
	grounded, dropped, drop := groundJourney(j, it)
	if drop {
		t.Fatal("expected journey to survive grounding, was dropped")
	}
	if dropped != 0 {
		t.Errorf("expected 0 dropped steps, got %d", dropped)
	}
	if len(grounded.Steps) != 4 {
		t.Errorf("expected 4 steps, got %d", len(grounded.Steps))
	}
}

// TestGrounding_DropsHallucinatedSteps verifies that click/seen steps
// whose name doesn't appear on any page in the chain are dropped.
func TestGrounding_DropsHallucinatedSteps(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
			Links:    []ast.LocatorAnchor{{Text: "Contact"}},
		}},
	}
	j := plan.Journey{
		Title: "hallucinated",
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "click", Role: "link", Name: "PostNL"}, // not on page
			{Op: "seen", Role: "heading", Name: "Home"}, // grounded
		},
	}
	grounded, dropped, drop := groundJourney(j, it)
	if drop {
		t.Fatalf("expected journey to survive (kept goto + seen), was dropped")
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped step, got %d", dropped)
	}
	if len(grounded.Steps) != 2 {
		t.Errorf("expected 2 steps after grounding, got %d", len(grounded.Steps))
	}
}

// TestGrounding_DropsWholeJourneyWhenReducedTooFar verifies that when
// filtering leaves fewer than 2 steps or no assertion, the whole
// journey is discarded.
func TestGrounding_DropsWholeJourneyWhenReducedTooFar(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
		}},
	}
	j := plan.Journey{
		Title: "click-and-hope",
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "click", Role: "link", Name: "Confabulated"}, // dropped
			{Op: "click", Role: "link", Name: "AlsoBogus"},    // dropped
		},
	}
	_, _, drop := groundJourney(j, it)
	if !drop {
		t.Fatal("expected journey to be dropped (no assertion after grounding)")
	}
}

// TestGrounding_EmptyNameAlwaysMatches preserves the v0.19 opSeen
// fallback where `seen('heading', '')` means "any heading is visible".
func TestGrounding_EmptyNameAlwaysMatches(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
		}},
	}
	j := plan.Journey{
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "seen", Role: "heading", Name: ""},
		},
	}
	_, dropped, drop := groundJourney(j, it)
	if drop || dropped != 0 {
		t.Errorf("empty-name seen should always survive; dropped=%d drop=%v", dropped, drop)
	}
}

// TestGrounding_StrictRoleRejection verifies v1.13's tightening:
// `click role=menuitem name=X` needs a menuitem-tagged anchor, not
// just any link. This was the root cause of the 54% exhausted-heal
// rate on v1.12 — the LLM emitted menuitem for what was actually a
// link, grounding accepted, verify failed, heal burned an LLM call.
func TestGrounding_StrictRoleRejection(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			// "Testing Services" exists as a plain link (Role empty).
			Links: []ast.LocatorAnchor{{Text: "Testing Services", Role: ""}},
			// A real menuitem exists but for a different label.
			Anchors: []ast.LocatorAnchor{{Text: "Company", Role: "menuitem"}},
			// A heading exists so seen() below can pass.
			Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
		}},
	}
	j := plan.Journey{
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			// Role mismatch: LLM said menuitem, but 'Testing Services'
			// is a plain link. Must be dropped.
			{Op: "click", Role: "menuitem", Name: "Testing Services"},
			// Role match: 'Company' IS role-tagged menuitem.
			{Op: "click", Role: "menuitem", Name: "Company"},
			{Op: "seen", Role: "heading", Name: "Home"},
		},
	}
	grounded, dropped, drop := groundJourney(j, it)
	if drop {
		t.Fatalf("expected journey to survive (Company menuitem grounds), was dropped")
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped step (Testing Services as menuitem), got %d", dropped)
	}
	// Verify the DROPPED step is the menuitem-with-wrong-role.
	for _, s := range grounded.Steps {
		if s.Name == "Testing Services" {
			t.Errorf("Testing Services (misrole'd menuitem) should have been dropped")
		}
	}
}

// TestGrounding_PerPage_DropsWrongPageAssertion verifies v1.14's tightening:
// a step that lands on page A must be grounded against page A's anchors,
// not "any page in the chain". Root cause of the 25-38 skips per PR on
// v1.13 — LLM emitted opGoto('/contact') then opSeen('heading',
// 'Performance Testing'), 'Performance Testing' exists on the site but
// on /performance-testing, not /contact. v1.13's chain-wide check
// accepted it; runtime skipped. v1.14 drops at emit.
func TestGrounding_PerPage_DropsWrongPageAssertion(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{
			{
				AbsoluteURL: "https://www.spritecloud.com/",
				Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
				Links:       []ast.LocatorAnchor{{Text: "Contact"}, {Text: "Performance Testing"}},
			},
			{
				AbsoluteURL: "https://www.spritecloud.com/contact",
				Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Let's Chat"}},
				Inputs:      []ast.FormInput{{LabelText: "Email"}},
			},
			{
				AbsoluteURL: "https://www.spritecloud.com/performance-testing",
				Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Performance Testing"}},
			},
		},
	}
	j := plan.Journey{
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "click", Role: "link", Name: "Contact"}, // grounded on /
			{Op: "goto", Path: "/contact"},
			// Wrong-page assertion: 'Performance Testing' is a heading
			// on /performance-testing, NOT /contact. v1.13 accepted
			// (chain-wide). v1.14 drops.
			{Op: "seen", Role: "heading", Name: "Performance Testing"},
			// Grounded on /contact
			{Op: "seen", Role: "heading", Name: "Let's Chat"},
		},
	}
	grounded, dropped, drop := groundJourney(j, it)
	if drop {
		t.Fatalf("expected journey to survive (Let's Chat grounds on /contact)")
	}
	if dropped != 1 {
		t.Errorf("expected 1 dropped step (Performance Testing on wrong page), got %d", dropped)
	}
	for _, s := range grounded.Steps {
		if s.Name == "Performance Testing" {
			t.Errorf("Performance Testing (wrong-page heading) should have been dropped")
		}
	}
}

// TestGrounding_PerPage_URLNormalization verifies the URL matcher
// handles absolute + relative + trailing slash + empty path variants.
func TestGrounding_PerPage_URLNormalization(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://www.spritecloud.com/contact", "/contact"},
		{"https://www.spritecloud.com/contact/", "/contact"},
		{"https://www.spritecloud.com/", "/"},
		{"/contact", "/contact"},
		{"contact", "/contact"},
		{"/", "/"},
		{"", ""},
		{"http://example.com", "/"},
	}
	for _, tc := range cases {
		if got := normalizeGroundURL(tc.in); got != tc.want {
			t.Errorf("normalizeGroundURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestGrounding_PerPage_UnknownURLFallsBackToChain verifies the chain-
// wide fallback kicks in when the current URL isn't in the mindmap
// (LLM landed somewhere the probe didn't crawl).
func TestGrounding_PerPage_UnknownURLFallsBackToChain(t *testing.T) {
	it := plan.Item{
		Symbols: []ast.Symbol{{
			AbsoluteURL: "https://www.spritecloud.com/",
			Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Home"}},
		}},
	}
	j := plan.Journey{
		Steps: []plan.Op{
			// LLM navigates somewhere the mindmap didn't cover.
			{Op: "goto", Path: "/uncharted"},
			// Home IS in the chain but not on /uncharted. Chain-wide
			// fallback accepts (permissive when we don't know).
			{Op: "seen", Role: "heading", Name: "Home"},
		},
	}
	_, dropped, drop := groundJourney(j, it)
	if drop || dropped != 0 {
		t.Errorf("unknown-URL step should fall back to chain-wide grounding; dropped=%d drop=%v",
			dropped, drop)
	}
}

// TestGrounding_NoSymbolsSkipsFilter preserves historical behavior on
// Item shapes without probe symbols — we can't ground without a
// mindmap, so we trust the LLM.
func TestGrounding_NoSymbolsSkipsFilter(t *testing.T) {
	it := plan.Item{}
	j := plan.Journey{
		Steps: []plan.Op{
			{Op: "goto", Path: "/"},
			{Op: "click", Role: "link", Name: "AnythingGoes"},
			{Op: "seen", Role: "heading", Name: "AnythingGoes"},
		},
	}
	grounded, dropped, drop := groundJourney(j, it)
	if drop || dropped != 0 {
		t.Errorf("no-symbols item should pass through unchanged; dropped=%d drop=%v", dropped, drop)
	}
	if len(grounded.Steps) != 3 {
		t.Errorf("expected all 3 steps preserved, got %d", len(grounded.Steps))
	}
}
