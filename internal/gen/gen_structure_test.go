package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestStepsAPITemplate_EmitsHelperVerbs(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightSteps,
		OutPath:  "tests/e2e/lib/steps.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, verb := range []string{
		"async function visit",
		"async function fillForm",
		"async function submit",
		"async function clickNav",
		"async function search",
		"async function expectAt",
		"async function expectH1",
		"async function convert",
		"async function authenticate",
		"export const steps",
	} {
		if !strings.Contains(body, verb) {
			t.Errorf("steps.ts missing %q", verb)
		}
	}
	// Quality report block is for specs, not stakeholder helpers.
	if strings.HasPrefix(body, "/* quail quality report") {
		t.Errorf("steps.ts must not carry a quality-report header")
	}
}

func TestHappyFlow_EmitsPriorityTagForEachKind(t *testing.T) {
	cases := map[string]string{
		"convert":      "critical",
		"contact":      "critical",
		"authenticate": "critical",
		"evaluate":     "standard",
		"research":     "standard",
		"browse":       "standard",
		"discover":     "standard",
		"exercise":     "standard",
		"explore":      "nice-to-have",
		"read":         "nice-to-have",
	}
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	for kind, want := range cases {
		t.Run(kind, func(t *testing.T) {
			it := plan.Item{
				Symbol:      landing,
				Symbols:     []ast.Symbol{landing},
				PageURL:     "https://x.test/",
				Template:    plan.TmplPlaywrightHappyFlow,
				OutPath:     "tests/e2e/x.spec.ts",
				JourneyKind: kind,
			}
			out, err := Render([]plan.Item{it}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			body := string(out[0].Content)
			wantTag := "@priority:" + want
			if !strings.Contains(body, wantTag) {
				t.Errorf("happyflow for %s missing %q", kind, wantTag)
			}
		})
	}
}

func TestCatalogueTemplate_RendersPagesJourneysFuzz(t *testing.T) {
	cat := &plan.Catalogue{
		Origin:      "https://x.test",
		GeneratedAt: "2026-06-17T12:00:00Z",
		Pages: []plan.CataloguePage{
			{URL: "https://x.test/", Title: "Home", Tags: []string{"landing"}},
			{URL: "https://x.test/contact", Title: "Contact us", Tags: []string{"contact", "form"}},
		},
		Journeys: []plan.CatalogueJourney{
			{Kind: "convert", Priority: "critical", OutPath: "tests/e2e/x-convert.spec.ts",
				Steps: []plan.CatalogueStep{{URL: "https://x.test/", Title: "Home"}}},
			{Kind: "contact", Priority: "critical", OutPath: "tests/e2e/x-contact.spec.ts",
				Steps: []plan.CatalogueStep{
					{URL: "https://x.test/", Title: "Home"},
					{URL: "https://x.test/contact", Title: "Contact us", EnteredVia: "/contact"},
				},
			},
		},
		Fuzz: []plan.CatalogueFuzz{
			{PageURL: "https://x.test/", OutPath: "tests/e2e/x-fuzz.spec.ts"},
		},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:    sym,
		Symbols:   []ast.Symbol{sym},
		PageURL:   "https://x.test",
		Template:  plan.TmplPlaywrightCatalogue,
		OutPath:   "tests/e2e/docs/test-catalogue.md",
		Catalogue: cat,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "# Test catalogue")
	mustContain(t, body, "Probed origin: **https://x.test**")
	mustContain(t, body, "## Pages crawled (2)")
	mustContain(t, body, "`https://x.test/contact`")
	mustContain(t, body, "## Journeys identified (2)")
	mustContain(t, body, "@priority:critical")
	mustContain(t, body, "`tests/e2e/x-convert.spec.ts`")
	mustContain(t, body, "clicked `/contact`")
	mustContain(t, body, "## Fuzz coverage (1)")
}

func TestSummaryTemplate_RendersPriorityMix(t *testing.T) {
	cat := &plan.Catalogue{
		Origin: "https://x.test",
		Pages: []plan.CataloguePage{
			{URL: "https://x.test/", Title: "Home", Tags: []string{"landing"}},
		},
		Journeys: []plan.CatalogueJourney{
			{Kind: "convert", Priority: "critical", OutPath: "a", Steps: []plan.CatalogueStep{{URL: "x"}}},
			{Kind: "browse", Priority: "standard", OutPath: "b", Steps: []plan.CatalogueStep{{URL: "x"}}},
			{Kind: "read", Priority: "nice-to-have", OutPath: "c", Steps: []plan.CatalogueStep{{URL: "x"}}},
		},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:    sym,
		Symbols:   []ast.Symbol{sym},
		PageURL:   "https://x.test",
		Template:  plan.TmplPlaywrightSummary,
		OutPath:   "tests/e2e/docs/summary.html",
		Catalogue: cat,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "<!doctype html>")
	mustContain(t, body, "quail · stakeholder summary")
	mustContain(t, body, "https://x.test")
	mustContain(t, body, "spriteCloud")              // wordmark
	mustContain(t, body, "pixel-rail")               // brand motif
	mustContain(t, body, "At a glance")              // new narrative section
	mustContain(t, body, "Coverage map")             // layer grid
	mustContain(t, body, "Recommended next steps")   // checklist
	mustContain(t, body, "journey-card")             // journey card structure
	mustContain(t, body, "users submit the lead")    // convert blurb from journeyKindBlurb
	mustContain(t, body, ">3<")                      // total journeys somewhere
	mustContain(t, body, "critical (1)")
	mustContain(t, body, "standard (1)")
	mustContain(t, body, "nice-to-have (1)")
}

func TestRawContentItem_BypassesTemplateExecution(t *testing.T) {
	// The DOM-snapshot path uses Template = TmplRaw + Item.RawContent.
	// Render must write the bytes verbatim — no template parsing.
	raw := []byte("<html><body>literal {{not a template}}</body></html>")
	it := plan.Item{
		Symbol:     ast.Symbol{Name: "X", Language: "ts"},
		Template:   plan.TmplRaw,
		OutPath:    "tests/e2e/_dom/x.html",
		RawContent: raw,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 rendered file; got %d", len(out))
	}
	if string(out[0].Content) != string(raw) {
		t.Errorf("raw content mismatch.\ngot: %q\nwant: %q", out[0].Content, raw)
	}
	if out[0].Path != "tests/e2e/_dom/x.html" {
		t.Errorf("path mismatch: %s", out[0].Path)
	}
}
