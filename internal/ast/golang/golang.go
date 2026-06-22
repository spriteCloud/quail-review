// Package golang extracts symbols from Go sources via go/parser. This is the
// one language where we have a real AST in the stdlib, so we use it.
package golang

import (
	gostd "go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
)

type extractor struct{}

func New() ast.Extractor { return extractor{} }

func init() {
	ast.Register([]string{".go"}, New())
}

func (extractor) Language() string { return "go" }

func (extractor) Extract(file string, content []byte) ([]ast.Symbol, []ast.LocatorAnchor) {
	if strings.HasSuffix(file, "_test.go") {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, content, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil
	}
	framework := inferFramework(f)
	var syms []ast.Symbol
	for _, decl := range f.Decls {
		fn, ok := decl.(*gostd.FuncDecl)
		if !ok || fn.Name == nil || !fn.Name.IsExported() {
			continue
		}
		start := fset.Position(fn.Pos())
		end := fset.Position(fn.End())
		s := ast.Symbol{
			Name:          fn.Name.Name,
			File:          file,
			Language:      "go",
			Line:          start.Line,
			EndLine:       end.Line,
			FrameworkHint: framework,
		}
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			s.Kind = ast.KindMethod
			s.Receiver = exprString(fn.Recv.List[0].Type)
		} else {
			s.Kind = ast.KindFunction
		}
		if isHandlerFunc(fn) {
			s.Kind = ast.KindRoute
			s.FrameworkHint = "nethttp"
		}
		for _, fld := range fn.Type.Params.List {
			typ := exprString(fld.Type)
			if len(fld.Names) == 0 {
				s.Params = append(s.Params, ast.Param{Type: typ})
				continue
			}
			for _, n := range fld.Names {
				s.Params = append(s.Params, ast.Param{Name: n.Name, Type: typ})
			}
		}
		if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
			s.Returns = exprString(fn.Type.Results.List[0].Type)
			s.PrimaryReturn, s.HasError, s.HasResult = classifyReturns(fn.Type.Results.List)
		}
		syms = append(syms, s)
	}
	return syms, nil
}

// classifyReturns walks the result list to decide how the test template
// should call the function. PrimaryReturn is the first non-error type
// string. hasResult is true only when there is exactly one non-error
// return — multi-return shapes would force the template into ambiguous
// `_,_,err := …` rewrites and aren't worth the complexity for v1.
func classifyReturns(list []*gostd.Field) (primary string, hasError, hasResult bool) {
	nonErr := 0
	for _, fld := range list {
		typ := exprString(fld.Type)
		count := len(fld.Names)
		if count == 0 {
			count = 1
		}
		for i := 0; i < count; i++ {
			if typ == "error" {
				hasError = true
				continue
			}
			nonErr++
			if primary == "" {
				primary = typ
			}
		}
	}
	hasResult = nonErr == 1
	return primary, hasError, hasResult
}

func isHandlerFunc(fn *gostd.FuncDecl) bool {
	if fn.Type == nil || fn.Type.Params == nil || len(fn.Type.Params.List) != 2 {
		return false
	}
	a := exprString(fn.Type.Params.List[0].Type)
	b := exprString(fn.Type.Params.List[1].Type)
	return (a == "http.ResponseWriter" || a == "ResponseWriter") &&
		(b == "*http.Request" || b == "*Request")
}

func exprString(e gostd.Expr) string {
	switch t := e.(type) {
	case *gostd.Ident:
		return t.Name
	case *gostd.StarExpr:
		return "*" + exprString(t.X)
	case *gostd.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *gostd.ArrayType:
		return "[]" + exprString(t.Elt)
	case *gostd.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	case *gostd.InterfaceType:
		return "interface{}"
	default:
		return ""
	}
}

func inferFramework(f *gostd.File) string {
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		p := strings.Trim(imp.Path.Value, `"`)
		switch {
		case p == "net/http":
			return "nethttp"
		case strings.Contains(p, "gin-gonic/gin"):
			return "gin"
		case strings.Contains(p, "labstack/echo"):
			return "echo"
		case strings.Contains(p, "go-chi/chi"):
			return "chi"
		}
	}
	return ""
}
