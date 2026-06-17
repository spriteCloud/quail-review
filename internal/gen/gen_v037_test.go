package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestFeatureTemplate_ScenarioOutlineEmittedForTextInputs(t *testing.T) {
	landing := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Type: "email", Required: true}},
	}
	it := plan.Item{
		Symbol: landing, Symbols: []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x.feature",
		JourneyKind: "convert",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:param")
	mustContain(t, body, "Scenario Outline:")
	mustContain(t, body, "email accepts <variant> values")
	mustContain(t, body, "Examples:")
	mustContain(t, body, "| typical |")
	mustContain(t, body, "jane@example.com")
	mustContain(t, body, "plus-alias")
	mustContain(t, body, "subdomain")
	if !strings.Contains(body, `When I enter "<value>" into the "email" field`) {
		t.Errorf("Outline step should use the <value> placeholder: %s", body)
	}
}

func TestFeatureTemplate_NoOutlineForFormlessJourney(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol: landing, Symbols: []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x.feature",
		JourneyKind: "browse",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out[0].Content), "Scenario Outline:") {
		t.Errorf("formless journey should not emit Scenario Outline")
	}
}

func TestParamRowsFor_AllInputTypes(t *testing.T) {
	for _, typ := range []string{"email", "password", "tel", "url", "number", "text", "search", "textarea"} {
		rows := paramRowsFor(ast.FormInput{Type: typ})
		if len(rows) != 3 {
			t.Errorf("%s: expected 3 rows; got %d", typ, len(rows))
		}
		for _, r := range rows {
			if r.Variant == "" || r.Value == "" {
				t.Errorf("%s: row missing variant or value: %+v", typ, r)
			}
		}
	}
	if rows := paramRowsFor(ast.FormInput{Type: "checkbox"}); rows != nil {
		t.Errorf("checkbox should yield nil rows; got %+v", rows)
	}
}
