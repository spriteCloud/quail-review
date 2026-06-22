package gen_test

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/gen"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestGoUnitWithErrorAndResult(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindFunction, Name: "Load", File: "x/y.go", Language: "go",
			HasError: true, HasResult: true, PrimaryReturn: "User",
		},
		Template: plan.TmplGoUnit,
		OutPath:  "x/y_test.go",
	}}
	out, err := gen.Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	for _, w := range []string{`"reflect"`, "got, err := Load", "unexpected error", "*new(User)"} {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in:\n%s", w, body)
		}
	}
}

func TestGoUnitVoidReturn(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindFunction, Name: "Noop", File: "x/y.go", Language: "go",
		},
		Template: plan.TmplGoUnit,
		OutPath:  "x/y_test.go",
	}}
	out, err := gen.Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if strings.Contains(body, "TODO") {
		t.Errorf("void-return template still contains TODO placeholder:\n%s", body)
	}
	if !strings.Contains(body, "Noop()") {
		t.Errorf("missing direct call: %s", body)
	}
}

func TestGoUnitErrorOnly(t *testing.T) {
	items := []plan.Item{{
		Symbol: ast.Symbol{
			Kind: ast.KindFunction, Name: "Flush", File: "x/y.go", Language: "go",
			HasError: true,
		},
		Template: plan.TmplGoUnit,
		OutPath:  "x/y_test.go",
	}}
	out, err := gen.Render(items, ".")
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	if !strings.Contains(body, "if err := Flush()") {
		t.Errorf("missing error-check form: %s", body)
	}
	if strings.Contains(body, "reflect") {
		t.Errorf("error-only template should not import reflect: %s", body)
	}
}
