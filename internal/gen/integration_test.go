package gen_test

// End-to-end exercise of the plan→gen pipeline against a synthetic in-memory
// PR diff. Network and GitHub are mocked out; the goal is to prove all four
// languages produce buildable test files for a representative happy-path
// symbol.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/spriteCloud/quail-review/internal/ast/golang"
	_ "github.com/spriteCloud/quail-review/internal/ast/java"
	_ "github.com/spriteCloud/quail-review/internal/ast/python"
	_ "github.com/spriteCloud/quail-review/internal/ast/ts"
	"github.com/spriteCloud/quail-core/diff"
	"github.com/spriteCloud/quail-review/internal/gen"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestEndToEnd_LoginFlow(t *testing.T) {
	dir := t.TempDir()
	loginPage := `import { LoginForm } from '../components/LoginForm'
export default function Login() { return (<main><LoginForm /></main>) }
`
	loginForm := `import { useState } from 'react'
export function LoginForm() {
  const [e, setE] = useState('')
  return (
    <form data-testid="login-form" onSubmit={()=>{}}>
      <input type="email" name="email" required onChange={(ev)=>setE(ev.target.value)} />
      <input type="password" name="password" required />
      <button type="submit" data-testid="submit">Sign in</button>
    </form>
  )
}
`
	must := func(rel, body string) {
		full := filepath.Join(dir, rel)
		mkdir(t, filepath.Dir(full))
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("pages/login.tsx", loginPage)
	must("src/components/LoginForm.tsx", loginForm)

	files := []diff.File{
		{Path: "src/components/LoginForm.tsx", Added: []diff.Range{{Start: 1, End: 20}}, Status: "added", NewBlob: loginForm},
	}
	items := plan.Build(files, plan.Detect(dir))
	if len(items) == 0 {
		t.Fatalf("no items; expected the LoginForm component to be picked up")
	}
	rs, err := gen.Render(items, dir)
	if err != nil {
		t.Fatal(err)
	}
	combined := ""
	for _, r := range rs {
		combined += string(r.Content)
	}
	// v0.97.0 — diff path now emits Gherkin (.feature) rather than
	// vanilla Playwright code. Assert the Gherkin shape: Feature
	// keyword, deterministic field-fill steps with literal values,
	// and a submit step. The matching playwright assertions live in
	// tests/e2e/steps/quail.steps.ts (emitted by the probe path).
	for _, want := range []string{
		"Feature: LoginForm",
		"\"test@example.com\"",
		"\"Passw0rd!\"",
		"I submit the form",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("login flow missing %q in:\n%s", want, combined)
		}
	}
}

func TestEndToEnd_PageFlowGrouping(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) {
		full := filepath.Join(dir, rel)
		mkdir(t, filepath.Dir(full))
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("pages/Home.tsx", `import { Counter } from '../src/Counter'
import { FAQ } from '../src/FAQ'
export default function Home() { return (<main><Counter /><FAQ /></main>) }
`)
	counter := `import { useState } from 'react'
export function Counter() {
  const [v, setV] = useState(0)
  return (<div data-testid="counter-root"><button data-testid="inc" onClick={()=>setV(v+1)}>+</button></div>)
}
`
	faq := `export function FAQ() {
  return (<div data-testid="faq-list"></div>)
}
`
	must("src/Counter.tsx", counter)
	must("src/FAQ.tsx", faq)

	files := []diff.File{
		{Path: "src/Counter.tsx", Added: []diff.Range{{Start: 1, End: 10}}, Status: "added", NewBlob: counter},
		{Path: "src/FAQ.tsx", Added: []diff.Range{{Start: 1, End: 10}}, Status: "added", NewBlob: faq},
	}
	items := plan.Build(files, plan.Detect(dir))
	// v0.97.0 — grouped page items now ship as one Gherkin .feature
	// rather than a TmplPlaywrightHappyFlow .spec.ts.
	var flow *plan.Item
	for i := range items {
		if items[i].Template == plan.TmplPlaywrightFeature {
			flow = &items[i]
		}
		if items[i].Template == plan.TmplPlaywrightE2E || items[i].Template == plan.TmplPlaywrightHappyFlow {
			t.Errorf("unexpected vanilla item: %+v", items[i])
		}
	}
	if flow == nil {
		t.Fatalf("expected one feature item, items = %+v", items)
	}
	rs, err := gen.Render([]plan.Item{*flow}, dir)
	if err != nil {
		t.Fatal(err)
	}
	body := string(rs[0].Content)
	// The Feature title comes from g.symbols[0].Name and the sort key
	// is line number, which both test components share (line 1) — so
	// either Counter or FAQ wins the title slot. Assert only on shape.
	for _, want := range []string{
		"Feature:",
		"As a visitor of /home",
		"Scenario:",
		"Given I open the landing page",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feature body missing %q in:\n%s", want, body)
		}
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestEndToEnd_AllLanguages(t *testing.T) {
	files := []diff.File{
		{
			Path: "src/math.ts", Status: "modified", Added: []diff.Range{{Start: 1, End: 10}},
			NewBlob: "export function add(a: number, b: number) {\n  return a + b\n}\n",
		},
		{
			Path: "app/users.py", Status: "added", Added: []diff.Range{{Start: 1, End: 10}},
			NewBlob: "from fastapi import FastAPI\napp = FastAPI()\n\n@app.get(\"/u\")\ndef list_users():\n    return []\n",
		},
		{
			Path: "server/health.go", Status: "added", Added: []diff.Range{{Start: 1, End: 5}},
			NewBlob: "package server\nimport \"net/http\"\nfunc Health(w http.ResponseWriter, r *http.Request) { w.Write([]byte(\"ok\")) }\n",
		},
		{
			Path: "src/main/java/com/acme/UserController.java", Status: "added", Added: []diff.Range{{Start: 1, End: 20}},
			NewBlob: "package com.acme;\nimport org.springframework.web.bind.annotation.*;\n@RestController\npublic class UserController {\n    @GetMapping(\"/u\")\n    public String list() { return \"\"; }\n}\n",
		},
	}
	items := plan.Build(files, plan.Layout{HasMavenLayout: true})
	if len(items) < 4 {
		t.Fatalf("expected >=4 items, got %d: %+v", len(items), items)
	}
	rs, err := gen.Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	gotByLang := map[string]string{}
	for _, r := range rs {
		gotByLang[r.Symbol.Language] = string(r.Content)
	}
	checks := map[string][]string{
		"ts":     {"describe(", "add(0, 0)", "it.each(", "'returns a value for arguments"},
		"python": {"def test_", "client.get", "application/json", "body is not None"},
		"go":     {"package server", "httptest.NewRequest", `t.Run("content-type header set"`, `t.Run("response body non-empty"`},
		"java":   {"package com.acme;", "RestAssured", "contentType(ContentType.JSON)", "notNullValue()"},
	}
	for lang, want := range checks {
		body, ok := gotByLang[lang]
		if !ok {
			t.Errorf("no rendered file for %s", lang)
			continue
		}
		for _, s := range want {
			if !strings.Contains(body, s) {
				t.Errorf("%s missing %q in:\n%s", lang, s, body)
			}
		}
	}
}
