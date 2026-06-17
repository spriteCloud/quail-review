package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func renderQuality(t *testing.T, tmpl plan.Template, sym ast.Symbol, pageURL string) string {
	t.Helper()
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  pageURL,
		Template: tmpl,
		OutPath:  "tests/e2e/x/x.spec.ts",
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return string(out[0].Content)
}

func TestA11yTemplate_AxeBuilderSerious(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightA11y, sym, "https://x.test/")
	mustContain(t, body, "@kind:a11y @smoke")
	mustContain(t, body, "AxeBuilder")
	mustContain(t, body, "'wcag2a'")
	mustContain(t, body, "expect(serious).toHaveLength(0)")
}

func TestResponsiveTemplate_ThreeViewports(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightResponsive, sym, "https://x.test/")
	for _, v := range []string{"375", "768", "1280", "mobile", "tablet", "desktop", "@kind:responsive"} {
		if !strings.Contains(body, v) {
			t.Errorf("responsive spec missing %q", v)
		}
	}
}

func TestPerfTemplate_HasSLOAssertion(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightPerf, sym, "https://x.test/")
	mustContain(t, body, "@kind:perf @smoke")
	mustContain(t, body, "PERF_SLO_MS")
	mustContain(t, body, "toBeLessThan(SLO_MS")
}

func TestSecurityTemplate_ChecksAllHeaders(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightSecurity, sym, "https://x.test")
	for _, h := range []string{
		"strict-transport-security",
		"content-security-policy",
		"x-frame-options",
		"x-content-type-options",
		"referrer-policy",
		"@kind:security",
	} {
		if !strings.Contains(body, h) {
			t.Errorf("security spec missing %q", h)
		}
	}
}

func TestHealthTemplate_HitsAllProbePaths(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightHealth, sym, "https://x.test")
	for _, p := range []string{"/health", "/healthz", "/ready", "/readyz", "/status", "/livez", "@kind:health"} {
		if !strings.Contains(body, p) {
			t.Errorf("health spec missing %q", p)
		}
	}
}

func TestObservabilityTemplate_HeaderSet(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightObservability, sym, "https://x.test")
	for _, h := range []string{"x-request-id", "server-timing", "traceparent", "@kind:observability"} {
		if !strings.Contains(body, h) {
			t.Errorf("observability spec missing %q", h)
		}
	}
}

func TestI18nTemplate_EmitsPerLocale(t *testing.T) {
	sym := ast.Symbol{
		Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts",
		Meta: ast.MetaTags{Hreflang: map[string]string{
			"en": "https://x.test/en",
			"es": "https://x.test/es",
		}},
	}
	body := renderQuality(t, plan.TmplPlaywrightI18n, sym, "https://x.test/")
	mustContain(t, body, "@kind:i18n @smoke")
	mustContain(t, body, `{ lang: 'en', url: 'https://x.test/en' }`)
	mustContain(t, body, `{ lang: 'es', url: 'https://x.test/es' }`)
}

func TestContractTemplate_RendersDeclaredStatuses(t *testing.T) {
	form := &ast.FormSpec{
		Action: "/pets",
		Method: "get",
		Inputs: []ast.FormInput{{Name: "200"}, {Name: "400"}, {Name: "404"}},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://api.x.test/pets", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://api.x.test/pets",
		Template: plan.TmplPlaywrightContract,
		OutPath:  "tests/e2e/contract/x-get-pets.contract.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:contract")
	mustContain(t, body, "200,")
	mustContain(t, body, "400,")
	mustContain(t, body, "404,")
	mustContain(t, body, "request.get('https://api.x.test/pets')")
}

func TestConfigTemplate_CrossBrowserProjects(t *testing.T) {
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
	mustContain(t, body, "REVIEWQA_BROWSERS")
	mustContain(t, body, "Desktop Firefox")
	mustContain(t, body, "Desktop Safari")
	mustContain(t, body, "Desktop Chrome")
}

func TestPackageJSONListsAxeAndScripts(t *testing.T) {
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
	mustContain(t, body, `"@axe-core/playwright"`)
	mustContain(t, body, `"test:a11y"`)
	mustContain(t, body, `"test:perf"`)
	mustContain(t, body, `"test:contract"`)
}
