package gen

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestTouchTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightTouch, sym, "https://x.test/")
	mustContain(t, body, "@kind:touch")
	mustContain(t, body, "iPhone 13")
	mustContain(t, body, "touchscreen.tap")
}

func TestDragDropTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightDragDrop, sym, "https://x.test/")
	mustContain(t, body, "@kind:dragdrop")
	mustContain(t, body, `[draggable="true"]`)
	mustContain(t, body, "dragTo")
}

func TestAuthExpiryTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightAuthExpiry, sym, "https://x.test/")
	mustContain(t, body, "@kind:auth-expiry")
	mustContain(t, body, "clearCookies")
	mustContain(t, body, "traceback")
}

func TestLocaleSwitchTemplate(t *testing.T) {
	sym := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		Meta: ast.MetaTags{Hreflang: map[string]string{
			"en": "https://x.test/en",
			"es": "https://x.test/es",
		}},
	}
	body := renderQuality(t, plan.TmplPlaywrightLocaleSwitch, sym, "https://x.test/")
	mustContain(t, body, "@kind:locale-switch")
	mustContain(t, body, "https://x.test/en")
	mustContain(t, body, "https://x.test/es")
}
