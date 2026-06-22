package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/composer"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// Param Outline + Examples rows must produce step text that
// playwright-bdd's regex matcher can bind. Regression: v0.37
// shipped a `with-quotes` row whose value contained literal `"`
// chars; substituted into `When I enter "<value>"…` it produced
// three `"` in the value span and bddgen rejected the file with
// "Missing step definitions". composer.IsGherkinSafe already
// detects the malformed shape — this test runs the deterministic
// renderer's output through that same check so future template
// edits can't reintroduce the class.
func TestFeatureTemplate_ParamExamples_PassGherkinSafetyCheck(t *testing.T) {
	landing := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		PageTitle: "Home",
		HasForm:   true,
		Inputs: []ast.FormInput{
			// Pick a text-like input so paramRowsFor returns the
			// `short / with-spaces / unicode / emoji / rtl-mark /
			// zero-width / with-quotes` row set.
			{Name: "q", Type: "text"},
		},
	}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-convert.feature",
		JourneyKind: "convert",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := out[0].Content
	if !strings.Contains(string(body), "@kind:param") {
		t.Fatalf("expected @kind:param Scenario Outline in body:\n%s", body)
	}
	if !composer.IsGherkinSafe(body) {
		t.Errorf("deterministic param Examples produced unsafe Gherkin (step quote-nesting):\n%s", body)
	}
}

func TestFeatureTemplate_BoundaryAndRetryFamilies(t *testing.T) {
	landing := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		PageTitle: "Home",
		HasForm:   true,
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Required: true},
		},
	}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-convert.feature",
		JourneyKind: "convert",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:retry")
	mustContain(t, body, "retry after empty submission succeeds")
	mustContain(t, body, "@kind:boundary")
	mustContain(t, body, "long input does not break the form")
	mustContain(t, body, "@kind:tab-order")
	mustContain(t, body, "@kind:overflow")
	mustContain(t, body, "oversized input does not crash the page")
}

func TestFeatureTemplate_MultiStepFamilies(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	step2 := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/blog", Language: "ts",
		PageTitle:  "Blog",
		EnteredVia: "/blog",
		Contents:   []ast.ContentAnchor{{Tag: "h1", Text: "Blog"}},
	}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing, step2},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-browse.feature",
		JourneyKind: "browse",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:resume")
	mustContain(t, body, "deep-link to the terminal page renders correctly")
	mustContain(t, body, "@kind:back-button")
	mustContain(t, body, "I go back in the browser history")
}

func TestFeatureTemplate_SingleStepJourneySkipsMultiStepFamilies(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x.feature",
		JourneyKind: "exercise",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if strings.Contains(body, "@kind:resume") || strings.Contains(body, "@kind:back-button") {
		t.Errorf("single-step journey should not emit resume/back-button families")
	}
}

func TestBoundaryValueFor_RespectsType(t *testing.T) {
	cases := map[string]int{
		"email":    72, // 60-char local + @example.com
		"url":      200,
		"tel":      22,
		"textarea": 960,
		"text":     200,
	}
	helpers := funcs
	for typ, wantLen := range cases {
		fn := helpers["boundaryValueFor"].(func(ast.FormInput) string)
		got := fn(ast.FormInput{Type: typ})
		if len(got) != wantLen {
			t.Errorf("boundaryValueFor(%q) → len %d; want %d", typ, len(got), wantLen)
		}
	}
}
