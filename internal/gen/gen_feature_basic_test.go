package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestFeatureTemplate_EmitsTaggedScenario(t *testing.T) {
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
	mustContain(t, body, "Feature: X — convert journey")
	mustContain(t, body, "@journey:convert @priority:critical @smoke")
	mustContain(t, body, "Scenario: convert journey reaches its terminal page")
	mustContain(t, body, "Given I open the landing page")
	mustContain(t, body, `And I enter "test@example.com" into the "email" field`)
	mustContain(t, body, "And I submit the form")
	mustContain(t, body, "@journey:convert @priority:critical @negative")
	mustContain(t, body, "Scenario: convert — empty submission is rejected")
	// Sanity: a feature file must not contain TypeScript runtime symbols.
	if strings.Contains(body, "import {") || strings.Contains(body, "test.describe") {
		t.Errorf("feature file should be pure Gherkin, not TS:\n%s", body)
	}
}

func TestFeatureTemplate_ChainedJourneyEmitsWhenSteps(t *testing.T) {
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	step2 := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/blog", Language: "ts",
		PageTitle: "Blog", EnteredVia: "/blog",
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
	mustContain(t, body, "@journey:browse @priority:standard @smoke")
	mustContain(t, body, `When I click the link to "/blog"`)
	mustContain(t, body, `And the page title contains "Blog"`)
}

func TestStepDefsBDDTemplate_BindsToStepsAPI(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightStepsBDD,
		OutPath:  "tests/e2e/steps/quail.steps.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `import { createBdd } from 'playwright-bdd'`)
	mustContain(t, body, `import { steps } from '../lib/steps'`)
	mustContain(t, body, "const { Given, When, Then } = createBdd()")
	// Coverage of the vocabulary the feature template emits.
	for _, pattern := range []string{
		`Given('I open the landing page'`,
		`When(/^I click the link to "([^"]+)"$/`,
		`When(/^I navigate directly to "([^"]+)"$/`,
		`When(/^I enter "([^"]+)" into the "([^"]+)" field$/`,
		`When('I submit the form'`,
		`When('I submit the form without filling any required field'`,
		`Then(/^the page title contains "([^"]+)"$/`,
		`Then(/^the main heading reads "([^"]+)"$/`,
		`Then(/^I see the heading "([^"]+)"$/`,
		`Then('no error message is shown in the form region'`,
		`Then('I remain on the same page'`,
		`Then('no success message is shown'`,
		`Then(/^the URL contains "([^"]+)"$/`,
		`Then(/^the page has at least (\d+) items$/`,
		`When('I scroll to the bottom of the page'`,
		`When(/^I press the "([^"]+)" key$/`,
	} {
		if !strings.Contains(body, pattern) {
			t.Errorf("steps file missing pattern %q", pattern)
		}
	}
}

func TestConfigTemplate_UsesDefineBddConfig(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightConfig,
		OutPath:  "playwright.config.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "import { defineBddConfig } from 'playwright-bdd'")
	mustContain(t, body, "defineBddConfig({")
	mustContain(t, body, "features: 'tests/e2e/features/*.feature'")
	mustContain(t, body, "steps: 'tests/e2e/steps/*.ts'")
	// v0.22: project naming switched to "<project>-<browser>" + the
	// testMatch grew to cover the quality companions.
	mustContain(t, body, "`bdd-${browser}`")
	mustContain(t, body, "`extras-${browser}`")
	mustContain(t, body, "api|fuzz|a11y|responsive|perf|security|health|observability|contract|i18n")
}

func TestPackageTemplate_DepsListPlaywrightBdd(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightPackage,
		OutPath:  "package.json",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `"playwright-bdd"`)
	mustContain(t, body, `"test:critical"`)
}
