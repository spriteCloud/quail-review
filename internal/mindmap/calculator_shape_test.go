package mindmap

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
)

// v0.92: a calculator-shape page emits TagForm even without any
// `required` attrs. The signal is a URL/title hint OR ≥2 numeric
// inputs. Generic across sites; tested with neutral fixtures so no
// site-specific assumption leaks into the heuristic.

func TestIsFormPage_CalculatorURLHintBypassesRequired(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/calculator",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "loan", Type: "number"}, {Name: "term", Type: "number"}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if !isFormPage(p) {
		t.Errorf("calculator URL + numeric inputs + submit should bypass hasRequired")
	}
}

func TestIsFormPage_DutchBerekenURL(t *testing.T) {
	p := &Page{
		URL:     "https://anybank.example/hypotheek-berekenen",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "bedrag", Type: "number"}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if !isFormPage(p) {
		t.Errorf("Dutch `bereken` URL must signal calculator")
	}
}

func TestIsFormPage_TwoNumericInputsBypassesRequired(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/loan-application",
		Title:   "Apply Online",
		HasForm: true,
		Inputs: []ast.FormInput{
			{Name: "amount", Type: "number"},
			{Name: "months", Type: "number"},
			{Name: "purpose", Type: "text"},
		},
		Anchors: []ast.LocatorAnchor{{Role: "button"}},
	}
	if !isFormPage(p) {
		t.Errorf("≥2 numeric inputs + submit should signal calculator shape")
	}
}

// A regular search bar must NOT trigger the calculator fallback.
// Single text/search input, no calc URL hint, no required attr → false.
func TestIsFormPage_SearchBarRejected(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/news",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "q", Type: "search"}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if isFormPage(p) {
		t.Errorf("search bar without required attr should NOT emit form journey")
	}
}

// Single newsletter signup (email + submit, no required) — also
// rejected. Otherwise every blog footer in the world becomes a
// JourneyConvert.
func TestIsFormPage_NewsletterSignupRejected(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/blog/article-1",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "email", Type: "email"}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if isFormPage(p) {
		t.Errorf("newsletter signup (single email, no required) should NOT signal calculator")
	}
}

// Required attr alone is still enough — the pre-v0.92 path stays
// intact.
func TestIsFormPage_RequiredStillCounts(t *testing.T) {
	p := &Page{
		URL:     "https://example.com/contact",
		HasForm: true,
		Inputs:  []ast.FormInput{{Name: "name", Type: "text", Required: true}},
		Anchors: []ast.LocatorAnchor{{Tag: "submit"}},
	}
	if !isFormPage(p) {
		t.Errorf("required input + submit must still pass")
	}
}
