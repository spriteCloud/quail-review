package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestFeatureTemplate_AppendsLLMComposedScenarios(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x-explore.feature",
		JourneyKind: "explore",
		LLMModel:    "qwen3-coder-next:latest",
		ExtraScenarios: []plan.ExtraScenario{
			{
				Name: "exploring deep into the docs",
				Tags: []string{"@kind:variant"},
				Steps: []plan.ExtraScenarioStep{
					{Keyword: "Given", Text: `I open the landing page`},
					{Keyword: "When", Text: `I click the link to "/docs"`},
					{Keyword: "Then", Text: `I see the heading "Documentation"`},
				},
			},
		},
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	// Deterministic part still renders.
	mustContain(t, body, "Feature: X — explore journey")
	mustContain(t, body, "@journey:explore @priority:nice-to-have @smoke")
	// LLM block renders below.
	mustContain(t, body, "LLM-composed scenarios (model: qwen3-coder-next:latest)")
	mustContain(t, body, "@journey:explore @priority:nice-to-have @llm-composed @kind:variant")
	mustContain(t, body, "Scenario: exploring deep into the docs")
	mustContain(t, body, `Given I open the landing page`)
	mustContain(t, body, `When I click the link to "/docs"`)
	mustContain(t, body, `Then I see the heading "Documentation"`)
}

func TestFeatureTemplate_NoLLMBlockWhenExtraScenariosEmpty(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightFeature,
		OutPath:     "tests/e2e/features/x.feature",
		JourneyKind: "browse",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if strings.Contains(body, "LLM-composed") || strings.Contains(body, "@llm-composed") {
		t.Errorf("LLM block should not render when ExtraScenarios is empty:\n%s", body)
	}
}
