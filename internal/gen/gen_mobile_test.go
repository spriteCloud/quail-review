package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestMobileTemplate_iPhone13Emulation(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightMobile, sym, "https://x.test/")
	mustContain(t, body, "@kind:mobile @smoke")
	// v0.56: device matrix replaces the single iPhone-13 emulation.
	// iPhone 13 still appears as one of the four devices iterated.
	mustContain(t, body, "'iPhone 13'")
	mustContain(t, body, ".tap()")
}

// v0.56 — depth parity. Mobile template now iterates a 4-device
// matrix and runs 2 scenarios per device (smoke + landscape).
func TestMobileTemplate_DeviceMatrix_v056(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightMobile, sym, "https://x.test/")
	for _, dev := range []string{"iPhone 13", "Pixel 5", "iPad Pro 11", "Galaxy S9+"} {
		mustContain(t, body, "'"+dev+"'")
	}
	mustContain(t, body, "@kind:mobile @orientation")
	mustContain(t, body, "landscape rotation")
	// 4 devices × 2 scenarios = 8 tests. Template literals are
	// used inside the for-loop so each test is `test(\`...\``.
	count := strings.Count(body, "test(`")
	// The pre-v0.56 template used `test('...')` (single-quote);
	// the new template uses `test(\`...\``  inside a for-loop to
	// interpolate the device name. Count the description definitions
	// which appear once per literal scenario in the template source.
	descs := strings.Count(body, "@kind:mobile @smoke @device:") +
		strings.Count(body, "@kind:mobile @orientation @device:")
	// Each describe-scope is evaluated for every iteration of DEVICES
	// at runtime — the rendered file only shows 2 test() statements
	// inside the for-loop, but they parameterise across 4 devices.
	if count < 2 || descs < 2 {
		t.Errorf("expected ≥2 parameterised test definitions inside for-of-DEVICES loop; got tests=%d descs=%d", count, descs)
	}
	// Sanity: confirm the loop itself.
	mustContain(t, body, "for (const deviceName of DEVICES)")
}

func TestDeepLinkTemplate_RangesAnchors(t *testing.T) {
	sym := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		Anchors: []ast.LocatorAnchor{
			{Name: "/app/profile"},
			{Name: "/app/orders/123"},
		},
	}
	body := renderQuality(t, plan.TmplPlaywrightDeepLink, sym, "https://x.test/")
	mustContain(t, body, "@kind:deeplink")
	mustContain(t, body, "'/app/profile'")
	mustContain(t, body, "'/app/orders/123'")
}

func TestRNHappyFlow_DetoxScaffold(t *testing.T) {
	sym := ast.Symbol{Name: "LoginScreen", Kind: ast.KindComponent, File: "src/LoginScreen.tsx", Language: "ts"}
	body := renderQuality(t, plan.TmplRNHappyFlow, sym, "")
	mustContain(t, body, "@kind:mobile-rn")
	mustContain(t, body, "detoxExpect")
	mustContain(t, body, "by.id('loginscreen-screen')")
}
