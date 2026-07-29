package main

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/plan"
)

// TestDedupeFallbackItems_KeepsOneCannedFallbackPerPage guards a
// quality gap found by reading a live-generated suite: browse-,
// exercise-, and explore-kind items landing on the same page can all
// independently fail LLM composition, and gen.Render's fallbackJourney
// then produces byte-identical "goto + any heading visible" output for
// each — three duplicate, content-free spec files. Items with a real
// composed Journey (Journey != nil) must never be touched, even when
// they share a page with a failed one.
// TestDedupeSemanticOverlap_DropsSubsetJourneyOnSamePage reproduces the
// exact case found by reading a live-generated suite: two differently-
// titled journeys land on /performance-testing, and one's single
// heading assertion is a subset of the other's two. The persona
// rotation's title-string dedup lets both through since the titles
// differ; this must drop the strictly-smaller one.
func TestDedupeSemanticOverlap_DropsSubsetJourneyOnSamePage(t *testing.T) {
	broader := &plan.Journey{Title: "Verify performance testing page highlights revenue angle", Steps: []plan.Op{
		{Op: "goto", Path: "/performance-testing"},
		{Op: "seen", Role: "heading", Name: "That's when they leave."},
		{Op: "seen", Role: "heading", Name: "Performance is a revenue conversation."},
	}}
	narrower := &plan.Journey{Title: "Verify performance testing page mentions revenue impact", Steps: []plan.Op{
		{Op: "goto", Path: "/performance-testing"},
		{Op: "seen", Role: "heading", Name: "Performance is a revenue conversation."},
	}}
	distinctPage := &plan.Journey{Title: "Verify cybersecurity page mentions NIS2", Steps: []plan.Op{
		{Op: "goto", Path: "/cybersecurity"},
		{Op: "seen", Role: "heading", Name: "Know if NIS2 applies."},
	}}
	items := []plan.Item{
		{JourneyKind: "suite", Journey: broader},
		{JourneyKind: "suite", Journey: narrower},
		{JourneyKind: "suite", Journey: distinctPage},
	}
	got := dedupeSemanticOverlap(items)
	if len(got) != 2 {
		t.Fatalf("expected 2 surviving items (broader + distinctPage); got %d: %+v", len(got), got)
	}
	for _, it := range got {
		if it.Journey == narrower {
			t.Errorf("expected the subset journey to be dropped, but it survived")
		}
	}
}

func TestDedupeFallbackItems_KeepsOneCannedFallbackPerPage(t *testing.T) {
	composed := &plan.Journey{Title: "real journey"}
	items := []plan.Item{
		{Template: plan.TmplPlaywrightHappyFlow, PageURL: "/", JourneyKind: "browse"},
		{Template: plan.TmplPlaywrightHappyFlow, PageURL: "/", JourneyKind: "exercise"},
		{Template: plan.TmplPlaywrightHappyFlow, PageURL: "/", JourneyKind: "explore"},
		{Template: plan.TmplPlaywrightHappyFlow, PageURL: "/pricing", Journey: composed},
		{Template: plan.TmplPlaywrightHappyFlow, PageURL: "/pricing", JourneyKind: "browse"}, // different page — must survive
	}
	got := dedupeFallbackItems(items)
	if len(got) != 3 {
		t.Fatalf("expected 3 items (1 canned for \"/\", the composed one, 1 canned for \"/pricing\"); got %d: %+v", len(got), got)
	}
	var canned int
	for _, it := range got {
		if it.Journey == nil {
			canned++
		}
	}
	if canned != 2 {
		t.Errorf("expected exactly 2 surviving canned-fallback items (one per distinct page); got %d", canned)
	}
}

func TestInteractionRole_PrefersCrawledRoleOverKindMapping(t *testing.T) {
	got := interactionRole(ast.Interaction{Kind: "tab", Role: "menuitem"})
	if got != "menuitem" {
		t.Errorf("expected crawled Role to win; got %q", got)
	}
}

func TestInteractionRole_MapsKindToValidARIARole(t *testing.T) {
	cases := map[string]string{
		"search":                    "searchbox",
		"tab":                       "tab",
		"dialog":                    "dialog",
		"date":                      "textbox",
		"details":                   "button",
		"collapse":                  "button",
		"data-toggle":               "button",
		"popup":                     "button",
		"unknown-kind-not-in-table": "button",
	}
	for kind, want := range cases {
		got := interactionRole(ast.Interaction{Kind: kind})
		if got != want {
			t.Errorf("kind %q: interactionRole = %q, want %q", kind, got, want)
		}
	}
}

// TestSymbolHints_InteractiveHintNeverLeadsWithBareInteractive guards
// against the regression this fix targets: a DGX-hosted LLM echoed the
// hint's leading word back as {"role": "interactive"} when the hint
// read "interactive: <kind> — <label>" with no explicit role= value.
func TestSymbolHints_InteractiveHintNeverLeadsWithBareInteractive(t *testing.T) {
	s := ast.Symbol{
		Interactions: []ast.Interaction{
			{Kind: "tab", Text: "Pricing"},
			{Kind: "data-toggle", Text: "Company", Role: "button"},
		},
	}
	hints := symbolHints(s)
	found := 0
	for _, h := range hints {
		if !strings.HasPrefix(h, "interactive:") {
			continue
		}
		found++
		if !strings.Contains(h, "role=") {
			t.Errorf("interactive hint has no explicit role=: %q", h)
		}
		if strings.Contains(h, "role=interactive") {
			t.Errorf("interactive hint regressed to using the literal word as a role: %q", h)
		}
	}
	if found != 2 {
		t.Fatalf("expected 2 interactive hints; got %d: %+v", found, hints)
	}
}
