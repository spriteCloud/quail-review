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

	_ "github.com/reviewqa/reviewqa/internal/ast/golang"
	_ "github.com/reviewqa/reviewqa/internal/ast/java"
	_ "github.com/reviewqa/reviewqa/internal/ast/python"
	_ "github.com/reviewqa/reviewqa/internal/ast/ts"
	"github.com/reviewqa/reviewqa/internal/diff"
	"github.com/reviewqa/reviewqa/internal/gen"
	"github.com/reviewqa/reviewqa/internal/plan"
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
	for _, want := range []string{
		".fill('test@example.com')",
		".fill('Passw0rd!')",
		"click()",
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
	var flow *plan.Item
	for i := range items {
		if items[i].Template == plan.TmplPlaywrightHappyFlow {
			flow = &items[i]
		}
		if items[i].Template == plan.TmplPlaywrightE2E {
			t.Errorf("unexpected per-component E2E item: %+v", items[i])
		}
	}
	if flow == nil {
		t.Fatalf("expected one happy-flow item, items = %+v", items)
	}
	rs, err := gen.Render([]plan.Item{*flow}, dir)
	if err != nil {
		t.Fatal(err)
	}
	body := string(rs[0].Content)
	for _, want := range []string{
		"page happy flow",
		"full user journey (2 step(s))",
		"// Step 1 — visit",
		"// Step 2 —",
		"getByTestId('counter-root')",
		"getByTestId('faq-list')",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("happy-flow body missing %q in:\n%s", want, body)
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
