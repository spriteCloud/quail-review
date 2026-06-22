package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestTouchTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightTouch, sym, "https://x.test/")
	mustContain(t, body, "@kind:touch")
	mustContain(t, body, "iPhone 13")
	mustContain(t, body, "touchscreen.tap")
}

// v0.56 — depth parity. Touch template now ships 5 gesture families.
func TestTouchTemplate_GestureFamilies_v056(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightTouch, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:touch @smoke long-press",
		"@kind:touch @swipe",
		"@kind:touch @pinch-zoom",
		"@kind:touch @scroll-momentum",
		"@kind:touch @tap-then-rotate",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("pw_touch missing gesture family %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 5 {
		t.Errorf("expected ≥5 tests in pw_touch (5 gesture families); got %d", count)
	}
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
