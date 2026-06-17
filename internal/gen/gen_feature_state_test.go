package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestFeatureTemplate_StateVariantsWhenAuthJourneyInSuite(t *testing.T) {
	cat := &plan.Catalogue{
		Journeys: []plan.CatalogueJourney{
			{Kind: "authenticate", OutPath: "tests/e2e/features/x-authenticate.feature"},
			{Kind: "browse", OutPath: "tests/e2e/features/x-browse.feature"},
		},
	}
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		Contents: []ast.ContentAnchor{{Tag: "h1", Text: "Home"}}}
	it := plan.Item{
		Symbol: landing, Symbols: []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-browse.feature",
		JourneyKind: "browse",
		Catalogue:   cat,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@state:logged-in @kind:state")
	mustContain(t, body, `I am signed in as "reviewqa-test-user"`)
	mustContain(t, body, "@state:anonymous @kind:state")
	mustContain(t, body, "I am not signed in")
}

func TestFeatureTemplate_NoStateVariantsWhenSuiteHasNoAuth(t *testing.T) {
	cat := &plan.Catalogue{
		Journeys: []plan.CatalogueJourney{
			{Kind: "browse", OutPath: "tests/e2e/features/x-browse.feature"},
		},
	}
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol: landing, Symbols: []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-browse.feature",
		JourneyKind: "browse",
		Catalogue:   cat,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out[0].Content), "@state:logged-in") {
		t.Errorf("suite without auth should not emit @state:logged-in")
	}
}

func TestFeatureTemplate_CrossJourneyWhenConvertExists(t *testing.T) {
	cat := &plan.Catalogue{
		Journeys: []plan.CatalogueJourney{
			{Kind: "convert"},
			{Kind: "browse"},
		},
	}
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol: landing, Symbols: []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x.feature",
		JourneyKind: "browse",
		Catalogue:   cat,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(out[0].Content), "@kind:cross-journey")
}
