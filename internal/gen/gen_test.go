package gen

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestRenderJestUnit(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindFunction, Name: "add", File: "src/math.ts", Language: "ts",
			Params: []ast.Param{{Name: "a", Type: "number"}, {Name: "b", Type: "number"}},
		},
		Template: plan.TmplJestUnit,
		OutPath:  "src/__tests__/math.test.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d", len(out))
	}
	body := string(out[0].Content)
	for _, want := range []string{"import { add }", "../math", "describe('add'", "add(0, 0)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPytestAPI(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "get_user", Method: "GET", Path: "/users/{uid}",
			File: "app/users.py", Language: "python", FrameworkHint: "fastapi",
		},
		Template: plan.TmplPytestAPI,
		OutPath:  "tests/test_users.py",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{"from fastapi.testclient", "TestClient(app)", `client.get("/users/{uid}")`, "test_get_get_user"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderGoHTTPTest(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "Health", File: "server/handlers.go", Language: "go",
		},
		Template: plan.TmplGoHTTPTest,
		OutPath:  "server/handlers_test.go",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{"package server", "httptest.NewRequest", "Health(rr, req)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPlaywrightE2E_Rich(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindComponent, Name: "FAQ",
			File: "web/src/components/FAQ.tsx", Language: "ts",
			Line: 1, EndLine: 10, FrameworkHint: "react",
			HasState: true, HasOnClick: true,
			Anchors: []ast.LocatorAnchor{
				{TestID: "faq-list", Tag: "div"},
				{TestID: "faq-toggle", Tag: "button"},
			},
		},
		Template: plan.TmplPlaywrightE2E,
		OutPath:  "tests/e2e/FAQ.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		"test.describe('FAQ'",
		"getByTestId('faq-list')",
		"getByTestId('faq-toggle')",
		"toBeVisible()",
		"target.click()",
		"aria-expanded",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if got := strings.Count(body, "  test("); got < 4 {
		t.Errorf("expected ≥4 test blocks, got %d:\n%s", got, body)
	}
}

func TestRenderPlaywrightE2E_Fallback(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindComponent, Name: "Empty",
			File: "web/src/components/Empty.tsx", Language: "ts",
			Line: 1, EndLine: 3, FrameworkHint: "react",
		},
		Template: plan.TmplPlaywrightE2E,
		OutPath:  "tests/e2e/Empty.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{"test.describe('Empty'", "renders the Empty component", "getByRole('main')"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if got := strings.Count(body, "  test("); got != 1 {
		t.Errorf("expected exactly 1 test block, got %d:\n%s", got, body)
	}
}

func TestRenderJestAPI_RichAssertions(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "getUser", Method: "GET", Path: "/users/:id",
			File: "src/routes/users.ts", Language: "ts",
		},
		Template: plan.TmplJestAPI,
		OutPath:  "src/routes/__tests__/users.test.ts",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{"toBeGreaterThanOrEqual(200)", "application\\/json", "JSON.stringify(res.body)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPytestAPI_RichAssertions(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "list_items", Method: "GET", Path: "/items",
			File: "app/items.py", Language: "python", FrameworkHint: "fastapi",
		},
		Template: plan.TmplPytestAPI,
		OutPath:  "tests/test_items.py",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{"200 <= res.status_code", "application/json", "body is not None"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderGoHTTPTest_RichAssertions(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "Ping", File: "server/handlers.go", Language: "go",
		},
		Template: plan.TmplGoHTTPTest,
		OutPath:  "server/handlers_test.go",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{`t.Run("happy path 2xx"`, `t.Run("content-type header set"`, `t.Run("response body non-empty"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderGoUnit_TypeSubtestWhenResultKnown(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindFunction, Name: "Add", File: "math/math.go", Language: "go",
			HasResult: true, PrimaryReturn: "int",
		},
		Template: plan.TmplGoUnit,
		OutPath:  "math/math_test.go",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{`t.Run("happy path"`, `t.Run("returns expected type"`, `reflect.TypeOf(got)`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderJUnit5Unit_DoesNotThrow(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindMethod, Name: "compute", Receiver: "Calc", Returns: "int",
			File: "src/main/java/com/acme/Calc.java", Language: "java",
		},
		Template: plan.TmplJUnit5Unit,
		OutPath:  "src/test/java/com/acme/CalcTest.java",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{"@DisplayName", "doesNotThrow", "assertNotEquals(0, result)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPlaywrightHappyFlow(t *testing.T) {
	symbols := []ast.Symbol{
		{
			Kind: ast.KindComponent, Name: "Counter",
			File: "src/Counter.tsx", Language: "ts", Line: 1, EndLine: 10,
			HasState: true, HasOnClick: true,
			Anchors: []ast.LocatorAnchor{
				{TestID: "counter-root", Tag: "div"},
				{TestID: "counter-inc", Tag: "button"},
			},
		},
		{
			Kind: ast.KindComponent, Name: "FAQ",
			File: "src/FAQ.tsx", Language: "ts", Line: 11, EndLine: 20,
			Anchors: []ast.LocatorAnchor{
				{TestID: "faq-list", Tag: "div"},
			},
		},
	}
	items := []plan.Item{{
		Symbol:   symbols[0],
		Symbols:  symbols,
		PageURL:  "/home",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Home.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		"test.describe('Counter page happy flow'",
		"await page.goto('/home', { waitUntil: 'domcontentloaded' })",
		"full user journey (2 step(s))",
		"// Step 1 — visit",
		"getByTestId('counter-root')",
		"getByTestId('counter-inc')",
		"// Step 2 —",
		"getByTestId('faq-list')",
		"target.click()",
		"aria-expanded",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if got := strings.Count(body, "test.describe("); got != 1 {
		t.Errorf("describe count = %d, want 1", got)
	}
	if got := strings.Count(body, "  test("); got != 1 {
		t.Errorf("test count = %d, want 1 (single walk-through)", got)
	}
}

func TestRenderPlaywrightHappyFlow_FillsAndSubmits(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "LoginForm",
		File: "src/components/LoginForm.tsx", Language: "ts",
		Line: 1, EndLine: 20,
		HasForm: true,
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Tag: "input", Required: true},
			{Name: "password", Type: "password", Tag: "input", Required: true},
		},
		Anchors: []ast.LocatorAnchor{
			{TestID: "login-form", Tag: "form"},
			{TestID: "submit-btn", Tag: "submit"},
		},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/login",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Login.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		"await page.goto('/login', { waitUntil: 'domcontentloaded' })",
		".fill('test@example.com')",
		".fill('Passw0rd!')",
		"getByTestId('submit-btn').first().click()",
		"toHaveCount(0)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPlaywrightHappyFlow_AbsoluteURL(t *testing.T) {
	// In v0.14.0+, an absolute probe URL is reduced to its path for
	// page.goto() — the origin lives in playwright.config.ts as
	// baseURL. The spec MUST NOT carry a hardcoded `TARGET` or
	// `BASE + 'https://...'` (the old shape would have produced an
	// unrunnable concatenation).
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Spritecloud",
		File: "https://www.spritecloud.com/", Language: "ts",
		Anchors: []ast.LocatorAnchor{{Role: "banner", Tag: "header"}},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "https://www.spritecloud.com/",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/spritecloud-com.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if !strings.Contains(body, "await page.goto('/', { waitUntil: 'domcontentloaded' })") {
		t.Errorf("absolute URL not reduced to path:\n%s", body)
	}
	for _, bad := range []string{
		"const TARGET = 'https://",
		"BASE + 'https://",
		"const BASE = process.env.BASE_URL",
	} {
		if strings.Contains(body, bad) {
			t.Errorf("v0.14 specs must not carry %q:\n%s", bad, body)
		}
	}
}

func TestIntentFor_Form(t *testing.T) {
	s := ast.Symbol{
		HasForm: true,
		Inputs:  []ast.FormInput{{TestID: "e", Type: "email", Required: true}},
		Anchors: []ast.LocatorAnchor{{TestID: "submit", Tag: "submit"}},
	}
	if got := intentFor(s); got != "form" {
		t.Errorf("intent = %q, want form", got)
	}
}

func TestIntentFor_Nav(t *testing.T) {
	s := ast.Symbol{
		Links: []ast.LocatorAnchor{{Aria: "/about", Tag: "link-a"}},
	}
	if got := intentFor(s); got != "nav" {
		t.Errorf("intent = %q, want nav", got)
	}
}

func TestIntentFor_Content(t *testing.T) {
	s := ast.Symbol{
		Anchors: []ast.LocatorAnchor{{Role: "banner", Tag: "header"}},
	}
	if got := intentFor(s); got != "content" {
		t.Errorf("intent = %q, want content", got)
	}
}

func TestInputLocator_FallbackChain(t *testing.T) {
	cases := []struct {
		name string
		in   ast.FormInput
		want string
	}{
		{"testid wins", ast.FormInput{TestID: "x", Aria: "y", Placeholder: "z"}, "getByTestId('x')"},
		{"aria when no testid", ast.FormInput{Aria: "y", Placeholder: "z"}, "getByLabel('y')"},
		{"placeholder when no aria", ast.FormInput{Placeholder: "Your email"}, "getByPlaceholder('Your email')"},
		{"label fallback", ast.FormInput{LabelText: "Email"}, "getByLabel('Email')"},
		{"name fallback", ast.FormInput{Name: "email"}, "locator('[name=\"email\"]')"},
		{"all missing → skip", ast.FormInput{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inputLocator(tc.in); got != tc.want {
				t.Errorf("inputLocator(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLocatorProvenance(t *testing.T) {
	cases := []struct {
		name string
		in   ast.FormInput
		want string
	}{
		{"testid → empty", ast.FormInput{TestID: "x"}, ""},
		{"aria → empty", ast.FormInput{Aria: "y"}, ""},
		{"placeholder → placeholder", ast.FormInput{Placeholder: "z"}, "placeholder"},
		{"label → label-for", ast.FormInput{LabelText: "Email"}, "label-for"},
		{"name → name", ast.FormInput{Name: "q"}, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := locatorProvenance(tc.in); got != tc.want {
				t.Errorf("locatorProvenance(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRender_QualityReport_AndPerLineNotes(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Mixed",
		File: "src/Mixed.tsx", Language: "ts",
		HasForm: true,
		Inputs: []ast.FormInput{
			{TestID: "strong", Type: "email", Required: true, Line: 5},
			{Name: "weak", Type: "text", Line: 7}, // only name → weak locator
		},
		Anchors: []ast.LocatorAnchor{
			{TestID: "submit", Tag: "submit"},
		},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/mixed",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Mixed.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		"quail quality report",
		"note: using <name>",
		"Weak / missing locators",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if len(out[0].QualityNotes) == 0 {
		t.Errorf("Rendered.QualityNotes should be populated, got empty")
	}
}

func TestRender_NoQualityReport_WhenHealthy(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Clean",
		File: "src/Clean.tsx", Language: "ts",
		HasForm: true,
		Inputs: []ast.FormInput{
			{TestID: "email", Type: "email", Required: true},
		},
		Anchors: []ast.LocatorAnchor{{TestID: "submit", Tag: "submit"}},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/clean",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Clean.spec.ts",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	if strings.Contains(body, "quality report") {
		t.Errorf("healthy spec should NOT carry a quality report:\n%s", body)
	}
	if len(out[0].QualityNotes) != 0 {
		t.Errorf("QualityNotes should be empty, got %+v", out[0].QualityNotes)
	}
}

func TestRender_NavIntent_NoFillNoSubmit(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Marketing",
		File: "marketing.html", Language: "ts",
		Anchors: []ast.LocatorAnchor{{TestID: "hero", Tag: "section"}},
		Links:   []ast.LocatorAnchor{{Aria: "/contact", Tag: "link-a"}},
		// No required input → IntentNav
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Marketing.spec.ts",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	if strings.Contains(body, ".fill(") {
		t.Errorf("nav intent should not emit .fill():\n%s", body)
	}
	if !strings.Contains(body, "toHaveURL(new RegExp('/contact$'))") {
		t.Errorf("nav intent should emit link click + URL assert:\n%s", body)
	}
	if !strings.Contains(body, "intent: nav") {
		t.Errorf("expected intent marker:\n%s", body)
	}
}

func TestRankedNavTargets_PrefersVocabularyMatches(t *testing.T) {
	links := []ast.LocatorAnchor{
		{Aria: "/privacy-policy", Text: "Privacy", Tag: "link-a"},
		{Aria: "/contact", Text: "Contact us", Tag: "link-a"},
		{Aria: "/blog/some-post", Text: "Some Post", Tag: "link-a"},
		{Aria: "/case-studies", Text: "Case studies", Tag: "link-a"},
	}
	got := rankedNavTargets(links, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	// /contact and /case-studies should be in the top 2 (vocabulary match + short href)
	hrefs := []string{got[0].Aria, got[1].Aria}
	want := map[string]bool{"/contact": true, "/case-studies": true}
	for _, h := range hrefs {
		if !want[h] {
			t.Errorf("unexpected top pick %q; wanted /contact + /case-studies, got %v", h, hrefs)
		}
	}
	for _, h := range hrefs {
		if h == "/privacy-policy" {
			t.Errorf("/privacy-policy should be deprioritised")
		}
	}
}

func TestRankedNavTargets_FallsBackOnSourceOrder(t *testing.T) {
	// No vocabulary matches at all — picker still returns something stable.
	links := []ast.LocatorAnchor{
		{Aria: "/foo", Tag: "link-a"},
		{Aria: "/bar", Tag: "link-a"},
	}
	got := rankedNavTargets(links, 1)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
}

func TestContentLocator(t *testing.T) {
	cases := []struct {
		in   ast.ContentAnchor
		want string
	}{
		{ast.ContentAnchor{Tag: "h1", Text: "Test your software"}, "getByRole('heading', { level: 1, name: /Test your software/i })"},
		{ast.ContentAnchor{Tag: "cta", Text: "Get started"}, "getByText(/Get started/i)"},
	}
	for _, tc := range cases {
		if got := contentLocator(tc.in); got != tc.want {
			t.Errorf("contentLocator(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRender_QualityReport_ProactiveOnTestidlessPage(t *testing.T) {
	// Symbol has substantial signal (one input + many anchors) but ZERO
	// testid on anything. Proactive report should fire even though no
	// fallback was triggered during render.
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Marketing",
		File: "marketing.html", Language: "ts",
		Anchors: []ast.LocatorAnchor{
			{Role: "banner", Tag: "header"},
			{Role: "navigation", Tag: "nav"},
			{Role: "contentinfo", Tag: "footer"},
		},
		Inputs: []ast.FormInput{{Name: "email", Type: "email", Required: true}},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Marketing.spec.ts",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	if !strings.Contains(body, "no data-testid attributes found") {
		t.Errorf("proactive quality report missing:\n%s", body)
	}
}

func TestRenderPlaywrightE2E_OnSubmitValidation(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "LoginForm",
		File: "src/components/LoginForm.tsx", Language: "ts",
		Line: 1, EndLine: 20,
		HasForm: true, HasOnSubmit: true,
		Inputs: []ast.FormInput{
			{Name: "email", Type: "email", Tag: "input", Required: true, TestID: "login-email"},
			{Name: "password", Type: "password", Tag: "input", Required: true, TestID: "login-password"},
		},
		Anchors: []ast.LocatorAnchor{
			{TestID: "login-form", Tag: "form"},
			{TestID: "login-submit", Tag: "submit"},
		},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Template: plan.TmplPlaywrightE2E,
		OutPath:  "tests/e2e/LoginForm.spec.ts",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{
		"form blocks submit when required fields are empty",
		"getByTestId('login-submit').first().click()",
		"getByTestId('login-email').first()).toBeVisible()",
		"text=/success|welcome|signed in/i",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderPlaywrightHappyFlow_Navigates(t *testing.T) {
	sym := ast.Symbol{
		Kind: ast.KindComponent, Name: "Home",
		File: "pages/Home.tsx", Language: "ts",
		Anchors: []ast.LocatorAnchor{{TestID: "home-banner", Tag: "div"}},
		Links: []ast.LocatorAnchor{
			{Aria: "/about", Tag: "link-a"},
		},
	}
	items := []plan.Item{{
		Symbol:   sym,
		Symbols:  []ast.Symbol{sym},
		PageURL:  "/home",
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  "tests/e2e/Home.spec.ts",
	}}
	out, _ := Render(items, ".")
	body := string(out[0].Content)
	for _, want := range []string{
		`a[href="/about"]`,
		"toHaveURL(new RegExp('/about$'))",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

func TestRenderJUnit5RestAssured(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindRoute, Name: "getById", Receiver: "UserController",
			Method: "GET", Path: "/users/{id}", File: "src/main/java/com/acme/UserController.java",
			Language: "java",
		},
		Template: plan.TmplJUnit5RestAssured,
		OutPath:  "src/test/java/com/acme/UserControllerTest.java",
	}}
	out, err := Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, want := range []string{"package com.acme;", "RestAssured", `get("/users/{id}")`, "class UserControllerTest", "contentType(ContentType.JSON)", "notNullValue()"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}
