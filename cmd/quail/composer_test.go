package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/mindmap"
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

// TestDedupeTitleCollisions_DropsCaseInsensitiveDuplicate guards the
// v1.26 post-hoc replacement for the mid-flight title-dedup that
// running personas concurrently gave up: two items with the same
// title (any case) must collapse to one.
func TestDedupeTitleCollisions_DropsCaseInsensitiveDuplicate(t *testing.T) {
	items := []plan.Item{
		{Journey: &plan.Journey{Title: "Book a demo"}},
		{Journey: &plan.Journey{Title: "book a demo"}}, // different case, same title
		{Journey: &plan.Journey{Title: "Check pricing"}},
		{Journey: nil}, // no journey yet (pending compose) — must survive untouched
	}
	got := dedupeTitleCollisions(items)
	if len(got) != 3 {
		t.Fatalf("expected 3 surviving items (1 dropped duplicate); got %d: %+v", len(got), got)
	}
}

// TestAppendSuiteJourneys_ConcurrentPersonasDedupSameTitle exercises
// the real concurrent path: every persona's ComposeSuite call hits the
// same stub and gets back the identical journey title. Without the
// personas seeing each other's mid-flight output, all 5 would land it
// independently — the post-hoc dedupeTitleCollisions pass must still
// collapse them to one, same end result a serial run would have had
// via the old shared `seen` map.
func TestAppendSuiteJourneys_ConcurrentPersonasDedupSameTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(map[string]any{"journeys": []map[string]any{{
			"title": "Book a demo",
			"steps": []map[string]any{
				{"op": "goto", "path": "/"},
				{"op": "seen", "role": "heading", "name": "Book a demo"},
			},
		}}})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": string(body)}}},
		})
	}))
	defer srv.Close()

	maps := map[string]*mindmap.Map{
		"https://example.com": {
			Origin: "https://example.com",
			Order:  []string{"https://example.com/"},
			Pages:  map[string]*mindmap.Page{"https://example.com/": {URL: "https://example.com/", Title: "Home"}},
		},
	}
	cfg := config.Config{OpenAIBaseURL: srv.URL, OpenAIAPIKey: "x", Model: "test", LLMTimeout: 5 * time.Second, LLMTokenCap: 4096}
	out := appendSuiteJourneys(context.Background(), cfg, nil, maps)

	titles := map[string]int{}
	for _, it := range out {
		if it.Journey != nil {
			titles[strings.ToLower(it.Journey.Title)]++
		}
	}
	if n := titles["book a demo"]; n != 1 {
		t.Errorf("expected exactly 1 surviving \"book a demo\" item after cross-persona dedup, got %d (out=%+v)", n, out)
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
