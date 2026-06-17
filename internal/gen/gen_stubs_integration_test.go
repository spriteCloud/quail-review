package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestIntegrationStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:integration-stub")
	mustContain(t, body, "test.skip")
	mustContain(t, body, "reviewqa.yml")
}

func TestI18nTemplate_AlwaysEmitsFallbackHtmlLangCheck(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightI18n, sym, "https://x.test/")
	if !strings.Contains(body, "<html lang> attribute is present") {
		t.Error("v0.43 i18n template missing always-on html-lang fallback check")
	}
}
