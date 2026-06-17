package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
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
	mustContain(t, out, "test('exercise: search box accepts input and navigates'")
	mustContain(t, out, "input[type=\"search\"]")
	mustContain(t, out, ".fill('test')")
	mustContain(t, out, "press('Enter')")
}

func TestExerciseTemplate_Dialog(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "dialog"})
	mustContain(t, out, "test('exercise: dialog opens and closes'")
	mustContain(t, out, "locator('dialog')")
	mustContain(t, out, "toBeAttached()")
}

func TestExerciseTemplate_Details(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "details", Text: "FAQ"})
	mustContain(t, out, "test('exercise: details element expands and collapses (FAQ)'")
	mustContain(t, out, "toHaveJSProperty('open', false)")
	mustContain(t, out, "toHaveJSProperty('open', true)")
}

func TestExerciseTemplate_Collapse(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "collapse", Text: "Toggle", Controls: "p1"})
	mustContain(t, out, "test('exercise: collapse toggle flips aria-expanded (Toggle)'")
	mustContain(t, out, "getAttribute('aria-expanded')")
	mustContain(t, out, "expect(after).not.toBe(before)")
}

func TestExerciseTemplate_Tab(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "tab", Text: "Reviews", Role: "tab"})
	mustContain(t, out, "test('exercise: tab activates (Reviews)'")
	mustContain(t, out, "getByRole('tab', { name: /Reviews/i })")
	mustContain(t, out, "toHaveAttribute('aria-selected', 'true')")
}

func TestExerciseTemplate_Date(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "date", InputType: "date"})
	mustContain(t, out, "test('exercise: date input accepts a value'")
	mustContain(t, out, "input[type=\"date\"]")
	mustContain(t, out, ".fill('2026-06-17')")
	mustContain(t, out, "toHaveValue('2026-06-17')")
}

func TestExerciseTemplate_DataToggle(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "data-toggle", Toggle: "modal", Text: "Open"})
	mustContain(t, out, "test('exercise: modal toggle responds to click (Open)'")
	mustContain(t, out, "[role=\"dialog\"]")
}

func TestExerciseTemplate_Popup(t *testing.T) {
	out := renderExercise(t, ast.Interaction{Kind: "popup", Text: "More options"})
	mustContain(t, out, "test('exercise: popup trigger reveals menu (More options)'")
	mustContain(t, out, "[role=\"menu\"]")
	mustContain(t, out, "[role=\"listbox\"]")
}

func TestExerciseTemplate_PageErrorGuard(t *testing.T) {
	// Every exercise spec should carry the shared pageerror tracking +
	// afterEach assertion. Verified against a single search interaction.
	out := renderExercise(t, ast.Interaction{Kind: "search", InputType: "search"})
	mustContain(t, out, "let pageErrors: string[]")
	mustContain(t, out, "test.beforeEach(async ({ page }) =>")
	mustContain(t, out, "page.on('pageerror'")
	mustContain(t, out, "test.afterEach(()")
	mustContain(t, out, "expect.soft(pageErrors")
}

func TestExerciseTemplate_NoRedundantGotoInEachTest(t *testing.T) {
	// With v0.13.0's beforeEach refactor, individual test blocks must NOT
	// repeat `await page.goto(TARGET)` — that was the per-test reload
	// waste called out in the candid review.
	out := renderExercise(t, ast.Interaction{Kind: "search", InputType: "search"})
	// One goto for beforeEach is fine; >1 means we regressed.
	if got := strings.Count(out, "page.goto(TARGET)"); got != 1 {
		t.Errorf("expected exactly 1 page.goto(TARGET) (in beforeEach); got %d", got)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("rendered spec missing expected token %q\n---\n%s\n---", needle, haystack)
	}
}
