package gen

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestNetworkResilienceTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightNetworkResilience, sym, "https://x.test/")
	mustContain(t, body, "@kind:network")
	mustContain(t, body, "context.setOffline")
	mustContain(t, body, "Retry-After")
}

func TestRaceTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightRace, sym, "https://x.test/")
	mustContain(t, body, "@kind:race")
	mustContain(t, body, "Promise.allSettled")
	mustContain(t, body, "double-submit")
}

func TestStorageTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightStorage, sym, "https://x.test/")
	mustContain(t, body, "@kind:storage")
	mustContain(t, body, "localStorage.clear")
	mustContain(t, body, "context.clearCookies")
}

func TestZoomTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightZoom, sym, "https://x.test/")
	mustContain(t, body, "@kind:zoom")
	mustContain(t, body, "scale(${z})")
}

func TestA11yPrefsTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightA11yPrefs, sym, "https://x.test/")
	mustContain(t, body, "@kind:a11y-prefs")
	mustContain(t, body, "reducedMotion")
	mustContain(t, body, "forcedColors")
	mustContain(t, body, "contrast")
}

func TestPrintTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightPrint, sym, "https://x.test/")
	mustContain(t, body, "@kind:print")
	mustContain(t, body, "emulateMedia")
	mustContain(t, body, "'print'")
}

func TestClipboardTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightClipboard, sym, "https://x.test/")
	mustContain(t, body, "@kind:clipboard")
	mustContain(t, body, "ClipboardEvent")
	mustContain(t, body, "__pasteXSS")
}

func TestHTTPChainsTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightHTTPChains, sym, "https://x.test/")
	mustContain(t, body, "@kind:http-chain")
	mustContain(t, body, "maxRedirects")
	mustContain(t, body, "Retry-After")
}

func TestParamRowsFor_UnicodeDomainEmail(t *testing.T) {
	rows := paramRowsFor(ast.FormInput{Type: "email"})
	foundUnicode := false
	for _, r := range rows {
		if r.Variant == "unicode-domain" {
			foundUnicode = true
		}
	}
	if !foundUnicode {
		t.Error("expected unicode-domain row in email param sweep after v0.42")
	}
}

func TestParamRowsFor_NumberHasNegativeAndFloat(t *testing.T) {
	rows := paramRowsFor(ast.FormInput{Type: "number"})
	foundNeg, foundFloat := false, false
	for _, r := range rows {
		if r.Variant == "negative" {
			foundNeg = true
		}
		if r.Variant == "float" {
			foundFloat = true
		}
	}
	if !foundNeg {
		t.Error("expected negative row in number param sweep")
	}
	if !foundFloat {
		t.Error("expected float row in number param sweep")
	}
}
