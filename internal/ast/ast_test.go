package ast_test

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	_ "github.com/spriteCloud/quail-review/internal/ast/golang"
	_ "github.com/spriteCloud/quail-review/internal/ast/java"
	_ "github.com/spriteCloud/quail-review/internal/ast/python"
	_ "github.com/spriteCloud/quail-review/internal/ast/ts"
)

func TestRegistry(t *testing.T) {
	for _, ext := range []string{".ts", ".tsx", ".py", ".go", ".java"} {
		if ast.ForFile("x"+ext) == nil {
			t.Errorf("no extractor for %s", ext)
		}
	}
}

func TestTSExtract(t *testing.T) {
	src := []byte(`import express from 'express'
const app = express()

export function add(a: number, b: number) {
  return a + b
}

app.get('/health', (req, res) => res.send('ok'))
`)
	syms, _ := ast.ForFile("src/api.ts").Extract("src/api.ts", src)
	var fn, route bool
	for _, s := range syms {
		if s.Kind == ast.KindFunction && s.Name == "add" {
			fn = true
			if len(s.Params) != 2 || s.Params[0].Type != "number" {
				t.Errorf("params: %+v", s.Params)
			}
		}
		if s.Kind == ast.KindRoute && s.Method == "GET" && s.Path == "/health" {
			route = true
			if s.FrameworkHint != "express" {
				t.Errorf("framework: %s", s.FrameworkHint)
			}
		}
	}
	if !fn {
		t.Error("missing add()")
	}
	if !route {
		t.Error("missing route GET /health")
	}
}

func TestTSXLocators(t *testing.T) {
	src := []byte(`export function Card() {
  return <button data-testid="submit" aria-label="Save changes" role="button">Save</button>
}
`)
	_, anchors := ast.ForFile("src/Card.tsx").Extract("src/Card.tsx", src)
	if len(anchors) < 2 {
		t.Fatalf("expected >=2 anchors, got %d: %+v", len(anchors), anchors)
	}
}

func TestPythonExtract(t *testing.T) {
	src := []byte(`from fastapi import FastAPI
app = FastAPI()

@app.get("/users/{uid}")
def get_user(uid: int):
    return {"id": uid}

def _private():
    pass
`)
	syms, _ := ast.ForFile("api.py").Extract("api.py", src)
	if len(syms) != 1 {
		t.Fatalf("want 1 sym, got %d: %+v", len(syms), syms)
	}
	if syms[0].Kind != ast.KindRoute || syms[0].Path != "/users/{uid}" {
		t.Errorf("route mismatch: %+v", syms[0])
	}
}

func TestGoExtract(t *testing.T) {
	src := []byte(`package h
import "net/http"

func Health(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }
func Add(a, b int) int { return a + b }
func unexported() {}
`)
	syms, _ := ast.ForFile("h.go").Extract("h.go", src)
	if len(syms) != 2 {
		t.Fatalf("want 2 syms, got %d: %+v", len(syms), syms)
	}
	var route, fn bool
	for _, s := range syms {
		if s.Name == "Health" && s.Kind == ast.KindRoute {
			route = true
		}
		if s.Name == "Add" && s.Kind == ast.KindFunction && len(s.Params) == 2 {
			fn = true
		}
	}
	if !route || !fn {
		t.Errorf("missing symbols: route=%v fn=%v", route, fn)
	}
}

func TestJavaExtract(t *testing.T) {
	src := []byte(`package com.acme;
import org.springframework.web.bind.annotation.*;

@RestController
public class UserController {
    @GetMapping("/users/{id}")
    public User getById(@PathVariable Long id) {
        return new User(id);
    }
}
`)
	syms, _ := ast.ForFile("UserController.java").Extract("UserController.java", src)
	if len(syms) != 1 {
		t.Fatalf("want 1 sym, got %d: %+v", len(syms), syms)
	}
	s := syms[0]
	if s.Kind != ast.KindRoute || s.Method != "GET" || s.Path != "/users/{id}" {
		t.Errorf("route mismatch: %+v", s)
	}
	if s.FrameworkHint != "spring" {
		t.Errorf("framework: %s", s.FrameworkHint)
	}
}
