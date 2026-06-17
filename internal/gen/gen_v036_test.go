package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

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
