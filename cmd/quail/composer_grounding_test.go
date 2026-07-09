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
