package gen

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
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
