package plan

import (
	"os"
	"path/filepath"
	"testing"

	_ "github.com/spriteCloud/quail-review/internal/ast/golang"
	_ "github.com/spriteCloud/quail-review/internal/ast/java"
	_ "github.com/spriteCloud/quail-review/internal/ast/python"
	_ "github.com/spriteCloud/quail-review/internal/ast/ts"
	"github.com/spriteCloud/quail-core/diff"
)

func TestBuildPicksTemplatesPerLanguage(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return rel
	}
	tsPath := must("src/math.ts", "export function add(a: number, b: number) { return a + b }\n")
	pyPath := must("app/users.py", "from fastapi import FastAPI\napp = FastAPI()\n\n@app.get(\"/u\")\ndef list_users():\n    return []\n")
	goPath := must("server/health.go", "package server\nimport \"net/http\"\nfunc Health(w http.ResponseWriter, r *http.Request) { w.Write([]byte(\"ok\")) }\n")
	javaPath := must("src/main/java/com/acme/UserController.java", "package com.acme;\nimport org.springframework.web.bind.annotation.*;\n@RestController\npublic class UserController {\n    @GetMapping(\"/u\")\n    public String list() { return \"\"; }\n}\n")

	files := []diff.File{
		{Path: tsPath, Added: []diff.Range{{Start: 1, End: 5}}},
		{Path: pyPath, Added: []diff.Range{{Start: 1, End: 10}}},
		{Path: goPath, Added: []diff.Range{{Start: 1, End: 10}}},
		{Path: javaPath, Added: []diff.Range{{Start: 1, End: 20}}, Status: "added"},
	}
	items := Build(files, Detect(dir))
	var byLang = map[string][]Item{}
	for _, it := range items {
		byLang[it.Symbol.Language] = append(byLang[it.Symbol.Language], it)
	}
	if len(byLang["ts"]) == 0 || byLang["ts"][0].Template != TmplJestUnit {
		t.Errorf("ts: %+v", byLang["ts"])
	}
	if len(byLang["python"]) == 0 || byLang["python"][0].Template != TmplPytestAPI {
		t.Errorf("python: %+v", byLang["python"])
	}
	if len(byLang["go"]) == 0 || byLang["go"][0].Template != TmplGoHTTPTest {
		t.Errorf("go: %+v", byLang["go"])
	}
	if len(byLang["java"]) == 0 || byLang["java"][0].Template != TmplJUnit5RestAssured {
		t.Errorf("java: %+v", byLang["java"])
	}
}

func TestBuildGroupsComponentsByPageRoot(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return rel
	}
	// Two TSX components, both mounted by pages/Home.tsx.
	must("pages/Home.tsx", `import { Counter } from '../src/Counter'
import { FAQ } from '../src/FAQ'
export default function Home() {
  return (<main><Counter /><FAQ items={[]} /></main>)
}
`)
	counterPath := must("src/Counter.tsx", `import { useState } from 'react'
export function Counter() {
  const [v, setV] = useState(0)
  return (<div data-testid="counter-root"><button data-testid="inc" onClick={()=>setV(v+1)}>+</button></div>)
}
`)
	faqPath := must("src/FAQ.tsx", `export function FAQ({items}: any) {
  return (<div data-testid="faq-list"><summary data-testid="faq-toggle">q</summary></div>)
}
`)

	files := []diff.File{
		{Path: counterPath, Added: []diff.Range{{Start: 1, End: 20}}, Status: "added"},
		{Path: faqPath, Added: []diff.Range{{Start: 1, End: 10}}, Status: "added"},
	}
	t.Setenv("QUAIL_E2E_STYLE", "")
	items := Build(files, Detect(dir))

	var flow *Item
	for i := range items {
		if items[i].Template == TmplPlaywrightFeature {
			flow = &items[i]
		}
		if items[i].Template == TmplPlaywrightE2E {
			t.Errorf("expected no per-component E2E items, found: %+v", items[i])
		}
	}
	if flow == nil {
		t.Fatalf("no feature item; items = %+v", items)
	}
	if len(flow.Symbols) != 2 {
		t.Errorf("flow.Symbols len = %d, want 2: %+v", len(flow.Symbols), flow.Symbols)
	}
	if flow.PageURL != "/home" {
		t.Errorf("PageURL = %q, want /home", flow.PageURL)
	}
	if flow.OutPath != "tests/e2e/features/Home.feature" {
		t.Errorf("OutPath = %q", flow.OutPath)
	}
}

func TestBuildEmitsHappyFlowForHTMLPage(t *testing.T) {
	dir := t.TempDir()
	indexHTML := `<!doctype html><html><body>
<header data-testid="hero">hi</header>
<nav role="navigation">n</nav>
<button aria-label="install">i</button>
</body></html>
`
	indexPath := "index.html"
	os.WriteFile(filepath.Join(dir, indexPath), []byte(indexHTML), 0o644)

	files := []diff.File{
		{Path: indexPath, Added: []diff.Range{{Start: 1, End: 10}}, Status: "modified", NewBlob: indexHTML},
	}
	items := Build(files, Detect(dir))

	var flow *Item
	for i := range items {
		if items[i].Template == TmplPlaywrightFeature {
			flow = &items[i]
		}
	}
	if flow == nil {
		t.Fatalf("no feature item for index.html; items = %+v", items)
	}
	if flow.PageURL != "/" {
		t.Errorf("PageURL = %q, want /", flow.PageURL)
	}
	if flow.OutPath != "tests/e2e/features/index.feature" {
		t.Errorf("OutPath = %q", flow.OutPath)
	}
	if len(flow.Symbols) != 1 || len(flow.Symbols[0].Anchors) < 3 {
		t.Errorf("expected 1 synthetic symbol with ≥3 anchors, got %+v", flow.Symbols)
	}
}

// v0.98 — verify the Component → Feature swap is gated on the
// project's base framework. When the workdir is a vanilla
// @playwright/test project (no playwright-bdd dep, no features dir,
// no defineBddConfig in the config), Component symbols emit the
// pre-v0.97.0 vanilla .spec.ts shape so the project's "base
// framework" stays consistent.
func TestBuildEmitsVanillaWhenNoBDD(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
		return rel
	}
	// Vanilla Playwright project shape: config file + tests/e2e dir,
	// no defineBddConfig, no playwright-bdd dep, no features/steps.
	must("playwright.config.ts", `import { defineConfig } from '@playwright/test'
export default defineConfig({})
`)
	must("package.json", `{"devDependencies":{"@playwright/test":"^1.49.0"}}`)
	must("tests/e2e/landing.spec.ts", `import { test } from '@playwright/test'`)
	counterPath := must("src/Counter.tsx", `export function Counter() {
  return (<div data-testid="counter-root"></div>)
}
`)
	files := []diff.File{
		{Path: counterPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
	}
	layout := Detect(dir)
	if layout.UsesBDD {
		t.Fatalf("vanilla project misclassified as BDD: %+v", layout)
	}
	items := Build(files, layout)
	for _, it := range items {
		if it.Template == TmplPlaywrightFeature {
			t.Errorf("vanilla project emitted .feature item: %+v", it)
		}
	}
	hasVanilla := false
	for _, it := range items {
		if it.Template == TmplPlaywrightE2E || it.Template == TmplPlaywrightHappyFlow {
			hasVanilla = true
		}
	}
	if !hasVanilla {
		t.Errorf("expected at least one TmplPlaywrightE2E item; items=%+v", items)
	}
}

// v0.98 — verify BDD projects still get Gherkin output.
func TestBuildEmitsGherkinWhenBDD(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
		return rel
	}
	must("playwright.config.ts", `import { defineConfig } from '@playwright/test'
import { defineBddConfig } from 'playwright-bdd'
const bdd = defineBddConfig({ features: 'tests/e2e/features/*.feature', steps: 'tests/e2e/steps/*.ts' })
export default defineConfig({ projects: [{ name: 'bdd', testDir: bdd }] })
`)
	must("package.json", `{"devDependencies":{"@playwright/test":"^1.49.0","playwright-bdd":"^9.0.0"}}`)
	counterPath := must("src/Counter.tsx", `export function Counter() {
  return (<div data-testid="counter-root"></div>)
}
`)
	files := []diff.File{
		{Path: counterPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
	}
	layout := Detect(dir)
	if !layout.UsesBDD {
		t.Fatalf("BDD project misclassified as vanilla: %+v", layout)
	}
	items := Build(files, layout)
	hasFeature := false
	for _, it := range items {
		if it.Template == TmplPlaywrightFeature {
			hasFeature = true
		}
		if it.Template == TmplPlaywrightE2E {
			t.Errorf("BDD project emitted vanilla .spec.ts item: %+v", it)
		}
	}
	if !hasFeature {
		t.Errorf("expected at least one TmplPlaywrightFeature item; items=%+v", items)
	}
}

func TestBuildFallsBackToPerComponentWhenNoPage(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
		return rel
	}
	counterPath := must("src/Counter.tsx", `export function Counter() {
  return (<div data-testid="counter-root"></div>)
}
`)
	faqPath := must("src/FAQ.tsx", `export function FAQ() {
  return (<div data-testid="faq-list"></div>)
}
`)
	files := []diff.File{
		{Path: counterPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
		{Path: faqPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
	}
	items := Build(files, Detect(dir))
	// v0.97.0 — Kind=Component now emits TmplPlaywrightFeature on the
	// diff path (Gherkin), not vanilla TmplPlaywrightE2E.
	got := 0
	for _, it := range items {
		if it.Template == TmplPlaywrightFeature {
			got++
		}
		if it.Template == TmplPlaywrightE2E || it.Template == TmplPlaywrightHappyFlow {
			t.Errorf("unexpected vanilla item: %+v", it)
		}
	}
	if got != 2 {
		t.Errorf("expected 2 per-component .feature items, got %d", got)
	}
}

func TestE2EStyleEnvOverride_ForcesPerComponent(t *testing.T) {
	dir := t.TempDir()
	must := func(rel, body string) string {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
		return rel
	}
	must("pages/Home.tsx", `import { Counter } from '../src/Counter'
import { FAQ } from '../src/FAQ'
export default function Home() { return (<main><Counter /><FAQ /></main>) }
`)
	counterPath := must("src/Counter.tsx", `export function Counter() { return (<div data-testid="c"></div>) }`)
	faqPath := must("src/FAQ.tsx", `export function FAQ() { return (<div data-testid="f"></div>) }`)

	files := []diff.File{
		{Path: counterPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
		{Path: faqPath, Added: []diff.Range{{Start: 1, End: 5}}, Status: "added"},
	}
	t.Setenv("QUAIL_E2E_STYLE", "per-component")
	items := Build(files, Detect(dir))
	for _, it := range items {
		// v0.97.0 — per-component mode now emits one Feature per
		// component instead of a grouped HappyFlow.
		if it.Template == TmplPlaywrightHappyFlow {
			t.Errorf("env override should disable happy-flow grouping, got %+v", it)
		}
	}
}

func TestLayoutDetection(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "__tests__"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src", "test", "java"), 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"devDependencies":{"vitest":"^1"}}`), 0o644)
	l := Detect(dir)
	if !l.HasJestDir {
		t.Error("expected HasJestDir")
	}
	if !l.UsesVitest {
		t.Error("expected UsesVitest")
	}
	if !l.HasMavenLayout {
		t.Error("expected HasMavenLayout")
	}
}
