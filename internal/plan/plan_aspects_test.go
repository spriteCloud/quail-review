package plan

import (
	"strings"
	"testing"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/diff"
)

func TestFanOutAspects_PureFunction(t *testing.T) {
	s := ast.Symbol{
		Name: "add", Kind: ast.KindFunction, File: "src/math.ts", Language: "ts",
		IsPure: true,
	}
	out := fanOutAspects(s, Layout{})
	if len(out) != 1 {
		t.Fatalf("expected 1 item; got %d", len(out))
	}
	if out[0].Template != TmplJestProperty {
		t.Errorf("template = %s; want jest_property", out[0].Template)
	}
	if !strings.Contains(out[0].OutPath, "tests/property/") {
		t.Errorf("OutPath should be under tests/property/; got %s", out[0].OutPath)
	}
}

func TestFanOutAspects_Validator(t *testing.T) {
	s := ast.Symbol{
		Name: "EmailValidator", Kind: ast.KindFunction, File: "src/v.ts", Language: "ts",
		IsValidator: true,
	}
	out := fanOutAspects(s, Layout{})
	if len(out) != 1 || out[0].Template != TmplJestValidatorPos {
		t.Fatalf("expected jest_validator_positive; got %+v", out)
	}
}

func TestFanOutAspects_JobKinds(t *testing.T) {
	cases := map[string]Template{
		"cron":  TmplScheduledJob,
		"event": TmplEventHandler,
		"email": TmplEmailTemplate,
	}
	for kind, want := range cases {
		s := ast.Symbol{
			Name: "x", Kind: ast.KindFunction, File: "src/x.ts", Language: "ts",
			JobKind: kind,
		}
		out := fanOutAspects(s, Layout{})
		if len(out) != 1 || out[0].Template != want {
			t.Errorf("kind %s: got %+v; want template %s", kind, out, want)
		}
	}
}

func TestBuildCompat_EmitsItemPerBreakingChange(t *testing.T) {
	files := []diff.File{
		{
			Path:    "api/openapi.json",
			Status:  "modified",
			OldBlob: `{"openapi":"3.0.0","info":{},"paths":{"/pets":{"get":{"responses":{"200":{}}}},"/users":{"get":{"responses":{"200":{}}}}}}`,
			NewBlob: `{"openapi":"3.0.0","info":{},"paths":{"/pets":{"get":{"responses":{"200":{}}}}}}`,
		},
		// unrelated source file — no compat item
		{Path: "src/index.ts", Status: "modified", OldBlob: "x", NewBlob: "y"},
	}
	items := BuildCompat(files, stubComparator)
	if len(items) != 1 {
		t.Fatalf("expected 1 compat item; got %d", len(items))
	}
	if items[0].Template != TmplOpenAPICompat {
		t.Errorf("template = %s; want openapi_compat", items[0].Template)
	}
	if !strings.HasSuffix(items[0].OutPath, ".compat.test.ts") {
		t.Errorf("OutPath should end in .compat.test.ts; got %s", items[0].OutPath)
	}
	if len(items[0].Symbol.Anchors) == 0 {
		t.Errorf("compat item should carry regression anchors")
	}
}

func stubComparator(path string, old, new_ []byte) (string, []CompatRegression, error) {
	if strings.Contains(string(new_), "openapi") {
		if strings.Contains(string(old), "/users") && !strings.Contains(string(new_), "/users") {
			return "openapi", []CompatRegression{
				{Kind: "endpoint-removed", Detail: "GET /users"},
			}, nil
		}
	}
	return "", nil, nil
}

func TestCamelize_DotsAndDashes(t *testing.T) {
	cases := map[string]string{
		"openapi.json":   "OpenapiJson",
		"foo-bar.proto":  "FooBarProto",
		"x_y_z":          "XYZ",
	}
	for in, want := range cases {
		if got := camelize(in); got != want {
			t.Errorf("camelize(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestSanitizeFilename_CollapseAndTrim(t *testing.T) {
	cases := map[string]string{
		"openapi.json":    "openapi-json",
		"a..b//c":         "a-b-c",
		"--leading":       "leading",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q; want %q", in, got, want)
		}
	}
}
