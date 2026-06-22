package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestGraphQLStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightGraphQLStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:graphql-stub")
	mustContain(t, body, "__schema")
	mustContain(t, body, "QUAIL_GRAPHQL_ENDPOINT")
	mustContain(t, body, "test.skip(true")
}

// v0.55 — five new adversarial blocks plus the original introspection
// happy-path = six total tests.
func TestGraphQLStubTemplate_DepthParity_v055(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightGraphQLStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@negative empty-query",
		"@negative malformed-query",
		"@negative type-mismatch",
		"@negative deep-nested-query",
		"@negative bare GET",
		"deep-nested-query is bounded",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("graphql-stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 6 {
		t.Errorf("expected ≥6 tests in graphql-stub (1 happy + 5 negatives); got %d", count)
	}
}

func TestWebhookStubTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightWebhookStub, sym, "https://x.test/")
	mustContain(t, body, "@kind:webhook-stub")
	mustContain(t, body, "X-Hub-Signature-256")
	mustContain(t, body, "Stripe-Signature")
	mustContain(t, body, "createHmac")
	mustContain(t, body, "QUAIL_WEBHOOK_SECRET")
}

// v0.55 — five new adversarial blocks plus the original signed/unsigned
// baseline = six total tests. Each defends a real-world webhook attack.
func TestWebhookStubTemplate_DepthParity_v055(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	body := renderQuality(t, plan.TmplPlaywrightWebhookStub, sym, "https://x.test/")
	for _, needle := range []string{
		"@negative replay-attack",
		"@negative expired-timestamp",
		"@negative wrong-algorithm",
		"@negative truncated-signature",
		"@negative tampered-body",
		"signMd5",        // wrong-algorithm helper
		"tenMinutesAgo",  // expired-timestamp construction
		"tampered body",  // tampered-body comment
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("webhook-stub missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 6 {
		t.Errorf("expected ≥6 tests in webhook-stub (1 baseline + 5 negatives); got %d", count)
	}
}

// v0.55 — pw_contract.tmpl was 1 test; now 9 (1 happy + 8 negatives).
func TestContractTemplate_DepthParity_v055(t *testing.T) {
	form := &ast.FormSpec{
		Action: "/pets/123",
		Method: "post",
		Inputs: []ast.FormInput{{Name: "200"}, {Name: "201"}, {Name: "400"}},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://api.x.test/pets/123", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://api.x.test/pets/123",
		Template: plan.TmplPlaywrightContract,
		OutPath:  "tests/e2e/contract/x-post-pets-123.contract.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := string(out[0].Content)
	for _, needle := range []string{
		"@kind:contract @smoke",
		"@negative wrong-method",
		"@negative oversized-query",
		"@negative invalid-json",
		"@negative unicode body",
		"@negative sql-injection-shaped",
		"@negative xss-shaped",
		"@negative null-byte",
		"@negative rapid burst",
		"DROP TABLE users",
		"window.__rqXSS",
		"value\\x00malicious",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("pw_contract missing %q", needle)
		}
	}
	count := strings.Count(body, "test('")
	if count < 9 {
		t.Errorf("expected ≥9 tests in pw_contract (1 happy + 8 negatives); got %d", count)
	}
}
