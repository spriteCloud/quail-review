package gen

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestDbtSchemaTemplate_UniqueOnID(t *testing.T) {
	sym := ast.Symbol{
		Name: "orders", Kind: ast.KindFunction, File: "models/orders.sql", Language: "sql",
		Params: []ast.Param{
			{Name: "order_id", Type: "int"},
			{Name: "amount", Type: "numeric"},
		},
	}
	it := plan.Item{Symbol: sym, Symbols: []ast.Symbol{sym}, Template: plan.TmplDbtSchema, OutPath: "models/orders.yml"}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "name: orders")
	mustContain(t, body, "name: order_id")
	mustContain(t, body, "- unique")
	mustContain(t, body, "- not_null")
}

func TestPanderaTemplate_TwoCases(t *testing.T) {
	sym := ast.Symbol{
		Name: "OrdersSchema", Kind: ast.KindFunction, File: "schemas/orders.py", Language: "python",
		Params: []ast.Param{{Name: "id", Type: "int"}, {Name: "total", Type: "float"}},
	}
	it := plan.Item{Symbol: sym, Symbols: []ast.Symbol{sym}, Template: plan.TmplPanderaConformance, OutPath: "tests/conformance/orders_test.py"}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "_accepts_valid_frame")
	mustContain(t, body, "_rejects_obviously_invalid_frame")
}

func TestGreatExpectationsTemplate_PerColumn(t *testing.T) {
	sym := ast.Symbol{
		Name: "Customer", Kind: ast.KindFunction, File: "data/customer", Language: "python",
		Params: []ast.Param{{Name: "id", Type: "int"}, {Name: "email", Type: "str"}},
	}
	it := plan.Item{Symbol: sym, Symbols: []ast.Symbol{sym}, Template: plan.TmplGreatExpectations, OutPath: "great_expectations/expectations/customer.yml"}
	out, err := Render([]plan.Item{it}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := string(out[0].Content)
	mustContain(t, body, "expectation_suite_name: customer_suite")
	mustContain(t, body, "expect_column_to_exist")
	mustContain(t, body, "expect_column_values_to_not_be_null")
}
