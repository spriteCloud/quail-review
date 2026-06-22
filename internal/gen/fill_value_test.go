package gen

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
)

// v0.92: name-hint dictionary fills calculator-shape fields with
// plausible values instead of the generic "42" for every numeric
// input. Tests cover EN + NL + DE + FR/ES/IT roots — site-agnostic.

func TestFillValueFor_NameHints(t *testing.T) {
	cases := []struct {
		name  string
		in    ast.FormInput
		want  string
	}{
		// Loan amount across languages.
		{"english loan", ast.FormInput{Name: "loanAmount", Type: "number"}, "250000"},
		{"dutch bedrag", ast.FormInput{Name: "bedrag", Type: "number"}, "250000"},
		{"german kredit", ast.FormInput{Name: "kreditsumme", Type: "number"}, "250000"},
		{"french pret", ast.FormInput{Name: "montant_pret", Type: "number"}, "250000"},
		// Income.
		{"english income", ast.FormInput{Name: "monthlyIncome", Type: "number"}, "4500"},
		{"dutch inkomen", ast.FormInput{Name: "bruto_inkomen", Type: "number"}, "4500"},
		{"french salaire", ast.FormInput{Name: "salaire_mensuel", Type: "number"}, "4500"},
		// Term / years.
		{"english years", ast.FormInput{Name: "termYears", Type: "number"}, "30"},
		{"dutch looptijd", ast.FormInput{Name: "looptijd", Type: "number"}, "30"},
		{"german laufzeit", ast.FormInput{Name: "laufzeit", Type: "number"}, "30"},
		// Rate.
		{"english rate", ast.FormInput{Name: "interestRate", Type: "number"}, "3.5"},
		{"dutch rente", ast.FormInput{Name: "rente", Type: "number"}, "3.5"},
		// Postcode.
		{"english zip", ast.FormInput{Name: "zip", Type: "text"}, "1011AB"},
		{"dutch postcode", ast.FormInput{Name: "postcode", Type: "text"}, "1011AB"},
		// Birthday.
		{"english birthday", ast.FormInput{Name: "birthday", Type: "date"}, "1990-01-01"},
		{"dutch geboorte", ast.FormInput{Name: "geboortedatum", Type: "date"}, "1990-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fillValueFor(tc.in)
			if got != tc.want {
				t.Errorf("fillValueFor(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Fields without name hints still fall back to the type map.
// "42" for generic number, "test@example.com" for email, etc.
func TestFillValueFor_NoHintFallsBackToType(t *testing.T) {
	cases := []struct {
		in   ast.FormInput
		want string
	}{
		{ast.FormInput{Name: "x", Type: "number"}, "42"},
		{ast.FormInput{Name: "field1", Type: "email"}, "test@example.com"},
		{ast.FormInput{Name: "phone", Type: "tel"}, "+15551234567"},
	}
	for _, tc := range cases {
		got := fillValueFor(tc.in)
		if got != tc.want {
			t.Errorf("fillValueFor(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Aria/placeholder/testid carry hints too — not just Name. A
// calculator that exposes its semantics via aria-label still gets a
// realistic value.
func TestFillValueFor_AriaCarriesHints(t *testing.T) {
	in := ast.FormInput{
		Name: "input_42",
		Aria: "Loan amount in euros",
		Type: "number",
	}
	if got := fillValueFor(in); got != "250000" {
		t.Errorf("aria-derived hint should fire: got %q, want 250000", got)
	}
}
