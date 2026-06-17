package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func TestAPITemplate_EmitsContractTests(t *testing.T) {
	form := &ast.FormSpec{
		Action:  "/api/contact",
		Method:  "post",
		EncType: "application/x-www-form-urlencoded",
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Required: true},
			{Name: "message", Type: "textarea", Required: true},
		},
	}
	sym := ast.Symbol{Name: "ContactAPI", Kind: ast.KindComponent, File: "https://x.test/api/contact", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/api/contact",
		Template: plan.TmplPlaywrightAPI,
		OutPath:  "tests/e2e/api/x-test-api-contact.api.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "@kind:api @smoke happy")
	mustContain(t, body, "@kind:api @negative missing required fields")
	mustContain(t, body, "@kind:api @negative malformed email")
	mustContain(t, body, "@kind:api @negative oversized payload")
	mustContain(t, body, "@kind:api @negative wrong method")
	mustContain(t, body, "request.post('https://x.test/api/contact'")
	mustContain(t, body, "'email': 'test@example.com'")
	mustContain(t, body, "huge = 'a'.repeat(50_000)")
}

func TestAPITemplate_GETFormSkipsWrongMethod(t *testing.T) {
	form := &ast.FormSpec{
		Action: "/search",
		Method: "get",
		Inputs: []ast.FormInput{{Name: "q", Type: "search"}},
	}
	sym := ast.Symbol{Name: "SearchAPI", Kind: ast.KindComponent, File: "https://x.test/search", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/search",
		Template: plan.TmplPlaywrightAPI,
		OutPath:  "tests/e2e/api/x-test-search.api.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if strings.Contains(body, "wrong method returns 4xx") {
		t.Errorf("GET-method form should not emit the wrong-method test")
	}
	mustContain(t, body, "request.get('https://x.test/search'")
}

func TestAPITemplate_JSONFormUsesDataKey(t *testing.T) {
	form := &ast.FormSpec{
		Action:  "/api/v2/login",
		Method:  "post",
		EncType: "application/json",
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Required: true},
			{Name: "password", Type: "password", Required: true},
		},
	}
	sym := ast.Symbol{Name: "LoginAPI", Kind: ast.KindComponent, File: "https://x.test/api/v2/login", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/api/v2/login",
		Template: plan.TmplPlaywrightAPI,
		OutPath:  "tests/e2e/api/x-test-api-v2-login.api.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	// application/json should use the `data:` request option, not `form:`.
	mustContain(t, body, "data: {")
}

// v0.50 — extended negative coverage on top of v0.20's baseline 4.
// Lives in the same file as the original API contract tests because
// it asserts the same template (pw_api.tmpl) at a richer surface.

func TestAPITemplate_EmitsExtendedNegatives_v050(t *testing.T) {
	form := &ast.FormSpec{
		Action:  "/api/contact",
		Method:  "post",
		EncType: "application/x-www-form-urlencoded",
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Required: true},
			{Name: "message", Type: "textarea", Required: true},
		},
	}
	sym := ast.Symbol{Name: "ContactAPI", Kind: ast.KindComponent, File: "https://x.test/api/contact", Language: "ts"}
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://x.test/api/contact",
		Template: plan.TmplPlaywrightAPI,
		OutPath:  "tests/e2e/api/x-test-api-contact.api.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := string(out[0].Content)
	for _, needle := range []string{
		"@kind:api @negative unicode payload",
		"@kind:api @negative sql-injection-shaped",
		"@kind:api @negative xss-shaped",
		"@kind:api @negative null-byte injection",
		"@kind:api @negative rapid burst",
		"DROP TABLE users",
		"window.__rqXSS=1",
		"value\\x00malicious",
		"Array.from({ length: 10 }",
	} {
		if !strings.Contains(body, needle) {
			t.Errorf("v0.50 API template missing %q", needle)
		}
	}
}

func TestAPITemplate_CountOfNegatives_v050(t *testing.T) {
	// At least 9 @kind:api @negative blocks (4 from v0.20 + 5 from v0.50).
	form := &ast.FormSpec{
		Action: "/api/contact", Method: "post",
		EncType: "application/x-www-form-urlencoded",
		Inputs:  []ast.FormInput{{Name: "email", Type: "email", Required: true}},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/", Language: "ts"}
	it := plan.Item{
		Symbol: sym, Symbols: []ast.Symbol{sym},
		PageURL:  "https://x.test/api/contact",
		Template: plan.TmplPlaywrightAPI,
		OutPath:  "tests/e2e/api/x.api.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	body := string(out[0].Content)
	count := strings.Count(body, "@kind:api @negative")
	if count < 9 {
		t.Errorf("expected ≥9 @kind:api @negative blocks (4 v0.20 + 5 v0.50); got %d", count)
	}
}
