package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestIntegrationStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:integration-stub")
	mustContain(t, body, "test.skip")
	mustContain(t, body, "quail.yml")
}

func TestI18nTemplate_AlwaysEmitsFallbackHtmlLangCheck(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightI18n, sym, "https://x.test/")
	if !strings.Contains(body, "<html lang> attribute is present") {
		t.Error("v0.43 i18n template missing always-on html-lang fallback check")
	}
}

// v0.57 — each per-kind stub ships 3 test.skip() blocks with concrete
// TODOs for the consumer to flip on once quail.yml declares the
// backing resource.

func TestIntegrationDBStubTemplate_v057(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationDBStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:integration-db",
		"@round-trip",
		"@transaction-rollback",
		"@connection-pool",
		"Testcontainers",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("integration-db stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test.skip(")
	if count < 3 {
		t.Errorf("expected ≥3 skipped blocks in integration-db stub; got %d", count)
	}
}

func TestIntegrationCacheStubTemplate_v057(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationCacheStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:integration-cache",
		"@set-get",
		"@ttl",
		"@invalidation",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("integration-cache stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test.skip(")
	if count < 3 {
		t.Errorf("expected ≥3 skipped blocks in integration-cache stub; got %d", count)
	}
}

func TestIntegrationObsStubTemplate_v057(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationObsStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:integration-obs",
		"@trace-propagation",
		"@request-id",
		"@server-timing",
		"traceparent",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("integration-obs stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test.skip(")
	if count < 3 {
		t.Errorf("expected ≥3 skipped blocks in integration-obs stub; got %d", count)
	}
}

func TestIntegrationAuthStubTemplate_v057(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightIntegrationAuthStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@kind:integration-auth",
		"@unauth",
		"@valid-token",
		"@expired",
		"QUAIL_AUTH_TOKEN",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("integration-auth stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test.skip(")
	if count < 3 {
		t.Errorf("expected ≥3 skipped blocks in integration-auth stub; got %d", count)
	}
}
