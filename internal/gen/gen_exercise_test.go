package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// renderExercise builds a minimal exercise plan.Item carrying one Interaction
// of the given kind, runs the pw_happyflow template, and returns the
// rendered spec body.
func renderExercise(t *testing.T, i ast.Interaction) string {
	t.Helper()
	sym := ast.Symbol{
		Name:         "Test",
		Kind:         ast.KindComponent,
		File:         "https://x.test",
		Language:     "ts",
		PageTitle:    "Test Page",
		Interactions: []ast.Interaction{i},
	}
	it := plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     "https://x.test",
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/x-exercise.spec.ts",
		JourneyKind: "exercise",
	}
	rendered, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(rendered) != 1 {
		t.Fatalf("expected 1 rendered spec; got %d", len(rendered))
	}
	return string(rendered[0].Content)
}

func TestExerciseTemplate_Search(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "search", InputType: "search"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: search box accepts input and navigates'")
	mustContain(t, out, "input[type=\"search\"]")
	mustContain(t, out, ".fill('test')")
	mustContain(t, out, "press('Enter')")
}

func TestExerciseTemplate_Dialog(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "dialog"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: dialog opens and closes'")
	mustContain(t, out, "locator('dialog')")
	mustContain(t, out, "toBeAttached()")
}

func TestExerciseTemplate_Details(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "details", Text: "FAQ"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: details element expands and collapses (FAQ)'")
	mustContain(t, out, "toHaveJSProperty('open', false)")
	mustContain(t, out, "toHaveJSProperty('open', true)")
}

func TestExerciseTemplate_Collapse(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "collapse", Text: "Toggle", Controls: "p1"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: collapse toggle flips aria-expanded (Toggle)'")
	mustContain(t, out, "getAttribute('aria-expanded')")
	mustContain(t, out, "expect(after).not.toBe(before)")
}

func TestExerciseTemplate_Tab(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "tab", Text: "Reviews", Role: "tab"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: tab activates (Reviews)'")
	mustContain(t, out, "getByRole('tab', { name: /Reviews/i })")
	mustContain(t, out, "toHaveAttribute('aria-selected', 'true')")
}

func TestExerciseTemplate_Date(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "date", InputType: "date"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: date input accepts a value'")
	mustContain(t, out, "input[type=\"date\"]")
	mustContain(t, out, ".fill('2026-06-17')")
	mustContain(t, out, "toHaveValue('2026-06-17')")
}

func TestExerciseTemplate_DataToggle(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "data-toggle", Toggle: "modal", Text: "Open"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: modal toggle responds to click (Open)'")
	mustContain(t, out, "[role=\"dialog\"]")
}

func TestExerciseTemplate_Popup(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "popup", Text: "More options"})
	mustContain(t, out, "test('@journey:exercise @priority:standard @smoke exercise: popup trigger reveals menu (More options)'")
	mustContain(t, out, "[role=\"menu\"]")
	mustContain(t, out, "[role=\"listbox\"]")
}

func TestExerciseTemplate_ImportsFromFixtures(t *testing.T) {
	// v0.14.0: page-error tracking lives in the shared fixtures module.
	// Every spec imports `test`/`expect` from `./_fixtures` instead of
	// declaring the listener inline.
	out := renderExercise(t, ast.Interaction{Kind: "search", InputType: "search"})
	mustContain(t, out, `import { test, expect } from './_fixtures'`)
	// The 14-line inline beforeEach/afterEach block is gone.
	if strings.Contains(out, "let pageErrors: string[]") {
		t.Error("v0.14.0 specs must not declare pageErrors inline; that lives in _fixtures.ts now")
	}
	if strings.Contains(out, "page.on('pageerror'") {
		t.Error("v0.14.0 specs must not install the pageerror listener inline")
	}
}

func TestExerciseTemplate_NoRedundantGotoInEachTest(t *testing.T) {
	// With v0.14.0's baseURL refactor, the beforeEach does
	// `page.goto('/')` (or the landing path) — and individual test
	// blocks must NOT repeat the goto.
	out := renderExercise(t, ast.Interaction{Kind: "search", InputType: "search"})
	gotos := strings.Count(out, "await page.goto(")
	if gotos != 1 {
		t.Errorf("expected exactly 1 await page.goto (in beforeEach); got %d", gotos)
	}
}

func TestCompanionTemplates_FixturesEmitsFixtureModule(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightFixtures,
		OutPath:  "tests/e2e/_fixtures.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "import { test as base, expect } from '@playwright/test'")
	mustContain(t, body, "export const test = base.extend")
	mustContain(t, body, "page.on('pageerror'")
	mustContain(t, body, "auto: true")
	mustContain(t, body, "export { expect }")
}

func TestCompanionTemplates_ConfigEmitsBaseURLFromProbe(t *testing.T) {
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
	mustContain(t, body, "defineConfig")
	mustContain(t, body, "process.env.BASE_URL ?? 'https://x.test'")
	mustContain(t, body, "baseURL: BASE_URL")
	mustContain(t, body, "testDir: './tests/e2e'")
	mustContain(t, body, "trace: 'on-first-retry'")
}

func TestCompanionTemplates_ReadmeListsKindsAndFiles(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightReadme,
		OutPath:  "tests/e2e/README.md",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "Generated Playwright suite")
	mustContain(t, body, "_fixtures.ts")
	mustContain(t, body, "playwright.config.ts")
	mustContain(t, body, "*-convert.spec.ts")
	mustContain(t, body, "*-exercise.spec.ts")
	mustContain(t, body, "BASE_URL")
}

func TestCompanionTemplates_PackageJSON(t *testing.T) {
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
	mustContain(t, body, `"@playwright/test"`)
	mustContain(t, body, `"test": "playwright test"`)
	mustContain(t, body, `"test:smoke": "playwright test --grep @smoke"`)
	mustContain(t, body, `"node": ">=18"`)
}

func TestCompanionTemplates_Tsconfig(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightTsconfig,
		OutPath:  "tsconfig.json",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `"strict": true`)
	mustContain(t, body, `"target": "ES2022"`)
	mustContain(t, body, `"@playwright/test"`)
}

func TestCompanionTemplates_CIWorkflow(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test",
		Template: plan.TmplPlaywrightCIFile,
		OutPath:  ".github/workflows/e2e.yml",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "name: e2e")
	mustContain(t, body, "npx playwright install")
	mustContain(t, body, "playwright-report/")
	mustContain(t, body, "playwright-traces")
}

func TestHappyFlowTemplate_TagsAndConfigure(t *testing.T) {
	// v0.16.0: each describe has test.describe.configure({ mode: 'parallel' })
	// and each generated test name carries @journey:<kind> and @smoke tags
	// so consumers can filter via --grep.
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/x.spec.ts",
		JourneyKind: "browse",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `test.describe.configure({ mode: 'parallel' })`)
	mustContain(t, body, `@journey:browse @priority:standard @smoke browse:`)
	mustContain(t, body, `* Filter to this kind with:`)
}

func TestHappyFlowTemplate_TestFailWhenDataWaitSubmit(t *testing.T) {
	// Webflow / similar pattern: input[type=submit] with data-wait attribute.
	// Extractor stamps anchor.CSS = "data-wait"; template should emit
	// test.fail() instead of plain test() for the journey.
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Spritecloud",
		File: "https://x.test/", Language: "ts",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Type: "email", Tag: "input", Required: true}},
		Anchors: []ast.LocatorAnchor{
			{TestID: "submit", Tag: "submit", CSS: "data-wait"},
		},
	}
	it := plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/x.spec.ts",
		JourneyKind: "convert",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "test.fail(")
	mustContain(t, body, "data-wait")
}

func TestHappyFlowTemplate_StepWrappingForChainedJourneys(t *testing.T) {
	// Chained journey: multi-step research/browse/explore. Each step
	// after Step 1 must be wrapped in test.step('label', async () => {})
	// so the trace UI shows steps as a tree.
	landing := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts", PageTitle: "Home"}
	step2 := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/blog", Language: "ts",
		PageTitle: "Blog", EnteredVia: "/blog",
	}
	it := plan.Item{
		Symbol:      landing,
		Symbols:     []ast.Symbol{landing, step2},
		PageURL:     "https://x.test/",
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/x-browse.spec.ts",
		JourneyKind: "browse",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, `await test.step('Step 2 — click "/blog"'`)
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("rendered spec missing expected token %q\n---\n%s\n---", needle, haystack)
	}
}
