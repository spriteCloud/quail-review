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
