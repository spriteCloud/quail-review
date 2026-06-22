package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestVisualStatesTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightVisualStates, sym, "https://x.test/")
	mustContain(t, body, "@kind:visual-state @smoke")
	mustContain(t, body, "STATES")
	mustContain(t, body, "hover")
	mustContain(t, body, "focus")
	mustContain(t, body, "toHaveScreenshot")
}

func TestKeyboardNavTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightKeyboardNav, sym, "https://x.test/")
	mustContain(t, body, "@kind:keyboard")
	mustContain(t, body, "keyboard.press('Tab')")
	mustContain(t, body, "outlineStyle")
	mustContain(t, body, "focus indicator")
}

func TestA11yLandmarksTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightA11yLandmarks, sym, "https://x.test/")
	mustContain(t, body, "@kind:a11y-landmarks")
	mustContain(t, body, `'main, [role="main"]'`)
	mustContain(t, body, `[aria-hidden="true"]`)
}

// v0.59 — depth parity. The a11y trio now ships 5 tests each.

func TestA11yTemplate_DepthParity_v059(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightA11y, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:a11y @smoke",
		"@kind:a11y @wcag-aa",
		"@kind:a11y @color-contrast",
		"@kind:a11y @aria-attrs",
		"@kind:a11y @form-labels",
		"runOnly: { type: 'rule', values: ['color-contrast'] }",
		"cat.aria",
		"unlabeled",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("pw_a11y missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 5 {
		t.Errorf("expected ≥5 tests in pw_a11y; got %d", count)
	}
}

func TestA11yLandmarksTemplate_DepthParity_v059(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightA11yLandmarks, sym, "https://x.test/")
	for _, needle := range []string{
		"@heading-hierarchy",
		"@landmark-names",
		"@skip-link",
		"WCAG 2.4.1",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("pw_a11y_landmarks missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 5 {
		t.Errorf("expected ≥5 tests in pw_a11y_landmarks; got %d", count)
	}
}

func TestKeyboardNavTemplate_DepthParity_v059(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightKeyboardNav, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:keyboard @smoke",
		"@kind:keyboard @focus-indicator",
		"@kind:keyboard @escape-dismiss",
		"@kind:keyboard @enter-space",
		"@kind:keyboard @no-trap",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("pw_keyboard_nav missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 5 {
		t.Errorf("expected ≥5 tests in pw_keyboard_nav; got %d", count)
	}
}
