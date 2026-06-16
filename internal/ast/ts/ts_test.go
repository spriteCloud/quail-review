package ts

import (
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
)

func TestMultiLineExportFunction(t *testing.T) {
	src := []byte(`export async function loadUser(
  id: number,
  opts?: { trace?: boolean },
): Promise<User> {
  return null as any
}
`)
	syms, _ := New().Extract("src/api.ts", src)
	if len(syms) != 1 {
		t.Fatalf("want 1 sym, got %d: %+v", len(syms), syms)
	}
	s := syms[0]
	if s.Name != "loadUser" || s.Kind != ast.KindFunction {
		t.Errorf("multi-line export mismatch: %+v", s)
	}
	if len(s.Params) != 2 {
		t.Errorf("expected 2 params, got %d: %+v", len(s.Params), s.Params)
	}
}

func TestAngularComponentDecorator(t *testing.T) {
	src := []byte(`import { Component } from '@angular/core'

@Component({
  selector: 'app-foo',
  template: '<button (click)="onClick()"></button>',
})
export class FooComponent {
  onClick() { return 1 }
}
`)
	syms, _ := New().Extract("src/foo.component.ts", src)
	var comp, method *ast.Symbol
	for i := range syms {
		s := &syms[i]
		if s.Kind == ast.KindComponent && s.Name == "FooComponent" {
			comp = s
		}
		if s.Kind == ast.KindMethod && s.Name == "onClick" {
			method = s
		}
	}
	if comp == nil {
		t.Errorf("missing component symbol; got %+v", syms)
	} else if comp.FrameworkHint != "angular" {
		t.Errorf("framework hint: %q", comp.FrameworkHint)
	}
	if method == nil {
		t.Errorf("missing method symbol; got %+v", syms)
	} else if method.Receiver != "FooComponent" {
		t.Errorf("receiver: %q", method.Receiver)
	}
}

func TestReactComponentEndLineAndAnchors(t *testing.T) {
	src := []byte(`import { useState } from 'react'

export function FAQ() {
  const [open, setOpen] = useState<number | null>(null)
  return (
    <div className="faq-list" data-testid="faq-list">
      <button data-testid="faq-toggle" onClick={() => setOpen(0)}>One</button>
      <button data-testid="faq-toggle" onClick={() => setOpen(1)}>Two</button>
      <summary role="heading" aria-label="Item one">One</summary>
    </div>
  )
}
`)
	syms, _ := New().Extract("src/components/FAQ.tsx", src)
	var comp *ast.Symbol
	for i := range syms {
		if syms[i].Kind == ast.KindComponent && syms[i].Name == "FAQ" {
			comp = &syms[i]
			break
		}
	}
	if comp == nil {
		t.Fatalf("missing FAQ component; syms = %+v", syms)
	}
	if comp.EndLine <= comp.Line {
		t.Errorf("EndLine (%d) should be after Line (%d)", comp.EndLine, comp.Line)
	}
	if !comp.HasState {
		t.Error("HasState should be true for useState")
	}
	if !comp.HasOnClick {
		t.Error("HasOnClick should be true")
	}
	// Dedup: two `data-testid="faq-toggle"` on a <button> → one anchor.
	tagCount := map[string]int{}
	for _, a := range comp.Anchors {
		key := a.TestID + "|" + a.Role + "|" + a.Aria + "|" + a.Tag
		tagCount[key]++
	}
	for k, n := range tagCount {
		if n != 1 {
			t.Errorf("anchor %q deduped to %d, want 1", k, n)
		}
	}
	// Must have at least one button-tagged anchor for click scenarios.
	var hasButton bool
	for _, a := range comp.Anchors {
		if a.Tag == "button" {
			hasButton = true
		}
	}
	if !hasButton {
		t.Errorf("no button anchor on component; anchors = %+v", comp.Anchors)
	}
}

func TestExtractFormInputsAndLinks(t *testing.T) {
	src := []byte(`import { useState } from 'react'

export function LoginForm() {
  return (
    <form data-testid="login-form">
      <input type="email" name="email" required />
      <input type="password" name="password" required />
      <button type="submit" data-testid="submit">Sign in</button>
      <a href="/forgot">Forgot password?</a>
    </form>
  )
}
`)
	syms, _ := New().Extract("src/components/LoginForm.tsx", src)
	var comp *ast.Symbol
	for i := range syms {
		if syms[i].Kind == ast.KindComponent && syms[i].Name == "LoginForm" {
			comp = &syms[i]
			break
		}
	}
	if comp == nil {
		t.Fatalf("missing LoginForm; syms = %+v", syms)
	}
	if !comp.HasForm {
		t.Error("HasForm should be true")
	}
	if len(comp.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d: %+v", len(comp.Inputs), comp.Inputs)
	}
	byType := map[string]ast.FormInput{}
	for _, in := range comp.Inputs {
		byType[in.Type] = in
	}
	if email, ok := byType["email"]; !ok || email.Name != "email" || !email.Required {
		t.Errorf("email input wrong: %+v", email)
	}
	if pw, ok := byType["password"]; !ok || pw.Name != "password" || !pw.Required {
		t.Errorf("password input wrong: %+v", pw)
	}
	if len(comp.Links) != 1 || comp.Links[0].Aria != "/forgot" {
		t.Errorf("expected one /forgot link, got %+v", comp.Links)
	}
}

func TestExtractFormInputsMultiLineAttributes(t *testing.T) {
	// Real LoginForm shape: every attribute on its own line. The fix
	// for v0.4.x must associate the testid with the input even when
	// data-testid lives on a line other than the <input.
	src := []byte(`import { useState } from 'react'

export function LoginForm() {
  const [email, setEmail] = useState('')
  return (
    <form data-testid="login-form">
      <input
        type="email"
        name="email"
        data-testid="login-email"
        required
      />
      <input
        type="password"
        name="password"
        data-testid="login-password"
        required
      />
      <button
        type="submit"
        data-testid="login-submit"
      >
        Sign in
      </button>
    </form>
  )
}
`)
	syms, _ := New().Extract("src/components/LoginForm.tsx", src)
	var comp *ast.Symbol
	for i := range syms {
		if syms[i].Kind == ast.KindComponent && syms[i].Name == "LoginForm" {
			comp = &syms[i]
			break
		}
	}
	if comp == nil {
		t.Fatalf("missing LoginForm; syms=%+v", syms)
	}
	if len(comp.Inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d: %+v", len(comp.Inputs), comp.Inputs)
	}
	byType := map[string]ast.FormInput{}
	for _, in := range comp.Inputs {
		byType[in.Type] = in
	}
	if email := byType["email"]; email.Name != "email" || email.TestID != "login-email" || !email.Required {
		t.Errorf("email input not associated with multi-line attrs: %+v", email)
	}
	if pw := byType["password"]; pw.Name != "password" || pw.TestID != "login-password" || !pw.Required {
		t.Errorf("password input not associated with multi-line attrs: %+v", pw)
	}
	// Submit button: must surface in Symbol.Anchors with Tag=="submit"
	// AND carry its testid, so firstSubmit + locatorFor pair up to
	// getByTestId('login-submit').click().
	var submit *ast.LocatorAnchor
	for i := range comp.Anchors {
		if comp.Anchors[i].Tag == "submit" {
			submit = &comp.Anchors[i]
			break
		}
	}
	if submit == nil {
		t.Fatalf("no submit anchor on Symbol; anchors=%+v", comp.Anchors)
	}
	if submit.TestID != "login-submit" {
		t.Errorf("submit anchor missing testid: %+v", submit)
	}
}

func TestReactComponentMultiLineJSXTagDetection(t *testing.T) {
	// Counter-shaped component: the button tag and the data-testid live on
	// different lines. The extractor must associate the testid with <button>
	// for click scenarios to fire.
	src := []byte(`import { useState } from 'react'

export function Counter() {
  const [v, setV] = useState(0)
  return (
    <div data-testid="counter-root" role="region">
      <span data-testid="counter-display">{v}</span>
      <button
        type="button"
        data-testid="counter-inc"
        onClick={() => setV((x) => x + 1)}
      >
        +
      </button>
    </div>
  )
}
`)
	syms, _ := New().Extract("src/components/Counter.tsx", src)
	var comp *ast.Symbol
	for i := range syms {
		if syms[i].Kind == ast.KindComponent && syms[i].Name == "Counter" {
			comp = &syms[i]
			break
		}
	}
	if comp == nil {
		t.Fatalf("missing Counter component; syms=%+v", syms)
	}
	var incTag string
	for _, a := range comp.Anchors {
		if a.TestID == "counter-inc" {
			incTag = a.Tag
		}
	}
	if incTag != "button" {
		t.Errorf("counter-inc anchor tag = %q, want \"button\" (multi-line JSX lookback failed)", incTag)
	}
}

func TestNestController(t *testing.T) {
	src := []byte(`import { Controller, Get } from '@nestjs/common'

@Controller('/users')
export class UsersController {
  @Get('list')
  list() { return [] }
}
`)
	syms, _ := New().Extract("src/users.controller.ts", src)
	var found bool
	for _, s := range syms {
		if s.Kind == ast.KindRoute && s.Name == "list" {
			found = true
			if s.Path != "/users/list" {
				t.Errorf("path: %q", s.Path)
			}
			if s.Method != "GET" {
				t.Errorf("method: %q", s.Method)
			}
		}
	}
	if !found {
		t.Errorf("missing route symbol; got %+v", syms)
	}
}
