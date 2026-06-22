package java

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
)

func TestSingleLineMapping(t *testing.T) {
	src := []byte(`package com.acme;
import org.springframework.web.bind.annotation.*;
@RestController
public class UserController {
    @GetMapping("/users/{id}")
    public String getById(@PathVariable Long id) { return ""; }
}
`)
	syms, _ := New().Extract("UserController.java", src)
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

func TestMultiLineAnnotation(t *testing.T) {
	src := []byte(`package com.acme;
import org.springframework.web.bind.annotation.*;
@RestController
public class OrderController {
    @PostMapping(
        value = "/orders",
        consumes = "application/json"
    )
    public String create(String body) { return ""; }
}
`)
	syms, _ := New().Extract("OrderController.java", src)
	if len(syms) != 1 {
		t.Fatalf("want 1 sym, got %d: %+v", len(syms), syms)
	}
	s := syms[0]
	if s.Method != "POST" || s.Path != "/orders" {
		t.Errorf("multi-line annotation mismatch: %+v", s)
	}
}

func TestMultiLineSignature(t *testing.T) {
	src := []byte(`package com.acme;
import org.springframework.web.bind.annotation.*;
@RestController
public class FooController {
    @GetMapping("/foo")
    public String bar(
        @RequestParam String q,
        @RequestParam int n
    ) { return ""; }
}
`)
	syms, _ := New().Extract("FooController.java", src)
	if len(syms) != 1 {
		t.Fatalf("want 1 sym, got %d: %+v", len(syms), syms)
	}
	s := syms[0]
	if s.Name != "bar" || s.Kind != ast.KindRoute || s.Path != "/foo" {
		t.Errorf("multi-line signature mismatch: %+v", s)
	}
	if len(s.Params) != 2 {
		t.Errorf("expected 2 params, got %d: %+v", len(s.Params), s.Params)
	}
}
