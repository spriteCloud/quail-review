package gen

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestMobileTemplate_iPhone13Emulation(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightMobile, sym, "https://x.test/")
	mustContain(t, body, "@kind:mobile @smoke")
	mustContain(t, body, "devices['iPhone 13']")
	mustContain(t, body, ".tap()")
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
