package gen

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/plan"
)

func render024(t *testing.T, tmpl plan.Template, sym ast.Symbol, page string, form *ast.FormSpec) string {
	t.Helper()
	it := plan.Item{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  page,
		Template: tmpl,
		OutPath:  "tests/x.spec.ts",
		Form:     form,
	}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return string(out[0].Content)
}

func TestIdempotencyTemplate(t *testing.T) {
	form := &ast.FormSpec{
		Method: "put",
		Inputs: []ast.FormInput{{Name: "email", Type: "email"}},
	}
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/users/1", Language: "ts"}
	body := render024(t, plan.TmplPlaywrightIdempotency, sym, "https://x.test/users/1", form)
	mustContain(t, body, "@kind:api-idempotency @smoke")
	mustContain(t, body, "first call should succeed")
	mustContain(t, body, "request.put('https://x.test/users/1'")
}

func TestPaginationTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/pets", Language: "ts"}
	body := render024(t, plan.TmplPlaywrightPagination, sym, "https://x.test/pets", &ast.FormSpec{Method: "get"})
	mustContain(t, body, "@kind:api-pagination")
	mustContain(t, body, "?page=1&limit=5")
	mustContain(t, body, "?page=2&limit=5")
}

func TestContentNegotiationTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/pets", Language: "ts"}
	body := render024(t, plan.TmplPlaywrightContentNegotiation, sym, "https://x.test/pets", &ast.FormSpec{Method: "get"})
	mustContain(t, body, "@kind:api-content-negotiation")
	mustContain(t, body, "application/json")
	mustContain(t, body, "application/xml")
}

func TestAuthHeadersTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/users", Language: "ts"}
	form := &ast.FormSpec{Method: "get"}
	body := render024(t, plan.TmplPlaywrightAuthHeaders, sym, "https://x.test/users", form)
	mustContain(t, body, "@kind:api-auth @negative")
	mustContain(t, body, "@kind:api-auth @smoke")
	mustContain(t, body, "TEST_AUTH_TOKEN")
	mustContain(t, body, "Bearer ${TEST_AUTH_TOKEN}")
}

func TestVersioningTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "X", Kind: ast.KindComponent, File: "https://x.test/v1/pets", Language: "ts"}
	body := render024(t, plan.TmplPlaywrightVersioning, sym, "https://x.test/v1/pets", &ast.FormSpec{Method: "get"})
	mustContain(t, body, "@kind:api-versioning")
	mustContain(t, body, "v1")
	mustContain(t, body, "v2")
}

func TestOpenAPICompatTemplate(t *testing.T) {
	sym := ast.Symbol{
		Name: "CompatOpenapi", Kind: ast.KindFunction, File: "openapi.json", Language: "ts",
		Anchors: []ast.LocatorAnchor{
			{Tag: "endpoint-removed", Name: "GET /users"},
			{Tag: "status-removed", Name: "POST /pets no longer returns 201"},
		},
	}
	body := render024(t, plan.TmplOpenAPICompat, sym, "", nil)
	mustContain(t, body, "@kind:contract-compat no breaking changes detected")
	mustContain(t, body, "endpoint-removed")
	mustContain(t, body, "GET /users")
	mustContain(t, body, "POST /pets no longer returns 201")
}

func TestStoreTemplate_EnumeratesActions(t *testing.T) {
	sym := ast.Symbol{
		Name: "userStore", Kind: ast.KindFunction, File: "src/userStore.ts", Language: "ts",
		StoreKind: "redux", StoreActions: []string{"login", "logout", "setName"},
	}
	body := render024(t, plan.TmplJestStore, sym, "", nil)
	mustContain(t, body, "@kind:store")
	for _, a := range []string{"login", "logout", "setName"} {
		if !strings.Contains(body, "dispatches "+a) {
			t.Errorf("missing action %q", a)
		}
	}
}

func TestConstructorTemplate_AssertsEachField(t *testing.T) {
	sym := ast.Symbol{
		Name: "User", Kind: ast.KindFunction, File: "src/user.ts", Language: "ts",
		Params: []ast.Param{{Name: "id", Type: "string"}, {Name: "age", Type: "number"}},
	}
	body := render024(t, plan.TmplJestConstructor, sym, "", nil)
	mustContain(t, body, "@kind:constructor")
	mustContain(t, body, "new User(")
	mustContain(t, body, "(instance as any).id")
	mustContain(t, body, "(instance as any).age")
}

func TestScheduledJobTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "syncOrders", Kind: ast.KindFunction, File: "src/jobs.ts", Language: "ts", JobKind: "cron"}
	body := render024(t, plan.TmplScheduledJob, sym, "", nil)
	mustContain(t, body, "@kind:scheduled")
	mustContain(t, body, "syncOrders")
}

func TestEventHandlerTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "onOrderCreated", Kind: ast.KindFunction, File: "src/events.ts", Language: "ts", JobKind: "event"}
	body := render024(t, plan.TmplEventHandler, sym, "", nil)
	mustContain(t, body, "@kind:event")
	mustContain(t, body, "onOrderCreated")
}

func TestEmailTemplate(t *testing.T) {
	sym := ast.Symbol{Name: "sendWelcomeEmail", Kind: ast.KindFunction, File: "src/mail.ts", Language: "ts", JobKind: "email"}
	body := render024(t, plan.TmplEmailTemplate, sym, "", nil)
	mustContain(t, body, "@kind:email")
	mustContain(t, body, "sendWelcomeEmail")
}
