package main

import (
	"strings"
	"testing"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/plan"
)

func TestBuildJourneyForComposer_LandingOnly(t *testing.T) {
	it := plan.Item{
		PageURL:     "https://example.com/",
		JourneyKind: "convert",
		Symbols: []ast.Symbol{
			{
				PageTitle: "Landing",
				Contents:  []ast.ContentAnchor{{Tag: "h1", Text: "Welcome"}},
				HasForm:   true,
				Inputs: []ast.FormInput{
					{Name: "email", Type: "email", LabelText: "Email address"},
					{Name: "msg", Type: "text", LabelText: "Message"},
				},
				Links: []ast.LocatorAnchor{{Aria: "Pricing"}, {Aria: "Login"}},
			},
		},
	}
	j := buildJourneyForComposer(it)
	if j.Title != "Landing" {
		t.Errorf("Title = %q", j.Title)
	}
	if j.H1 != "Welcome" {
		t.Errorf("H1 = %q", j.H1)
	}
	if !j.HasForm {
		t.Error("expected HasForm true")
	}
	if len(j.Pages) != 0 {
		t.Errorf("no destination pages; got %d", len(j.Pages))
	}
	if len(j.Forms) != 1 || !strings.Contains(j.Forms[0], "Email address") {
		t.Errorf("form summary missing email label; Forms = %v", j.Forms)
	}
	if !strings.Contains(j.Forms[0], "(email)") {
		t.Errorf("expected type annotation on email input; Forms = %v", j.Forms)
	}
}

func TestBuildJourneyForComposer_FansSymbolsIntoPages(t *testing.T) {
	it := plan.Item{
		PageURL:     "https://example.com/",
		JourneyKind: "convert",
		Symbols: []ast.Symbol{
			{
				PageTitle: "Pricing",
				Contents:  []ast.ContentAnchor{{Tag: "h1", Text: "Plans"}},
			},
			{
				PageTitle:   "Checkout",
				Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Pay"}},
				EnteredVia:  "/checkout",
				HasForm:     true,
				Inputs:      []ast.FormInput{{Name: "card", Type: "text", LabelText: "Card"}},
				AbsoluteURL: "https://example.com/checkout",
			},
			{
				PageTitle:   "Thank you",
				Contents:    []ast.ContentAnchor{{Tag: "h1", Text: "Thanks"}},
				DirectGoto:  true,
				AbsoluteURL: "https://example.com/thanks",
			},
		},
	}
	j := buildJourneyForComposer(it)
	if len(j.Pages) != 2 {
		t.Fatalf("expected 2 destination pages; got %d", len(j.Pages))
	}
	if j.Pages[0].Href != "/checkout" {
		t.Errorf("checkout Href = %q (expected /checkout)", j.Pages[0].Href)
	}
	if j.Pages[0].Title != "Checkout" || j.Pages[0].H1 != "Pay" {
		t.Errorf("checkout meta wrong: %+v", j.Pages[0])
	}
	if j.Pages[1].Href != "https://example.com/thanks" {
		t.Errorf("thanks Href = %q (expected absolute URL)", j.Pages[1].Href)
	}
	if j.Pages[1].H1 != "Thanks" {
		t.Errorf("thanks H1 = %q", j.Pages[1].H1)
	}
	// Forms should include the destination page's form fields, not only the landing's.
	found := false
	for _, f := range j.Forms {
		if strings.Contains(f, "Card") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected destination form summary to surface Card field; Forms = %v", j.Forms)
	}
}

func TestFormSummary_FallbacksThroughLabels(t *testing.T) {
	cases := []struct {
		name    string
		in      ast.FormInput
		wantSub string
	}{
		{"label", ast.FormInput{LabelText: "Email", Type: "email"}, "Email"},
		{"aria", ast.FormInput{Aria: "Search box", Type: "search"}, "Search box"},
		{"placeholder", ast.FormInput{Placeholder: "your@email.com", Type: "email"}, "your@email.com"},
		{"name", ast.FormInput{Name: "phone", Type: "tel"}, "phone"},
		{"type-only", ast.FormInput{Type: "password"}, "password"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := formSummary(ast.Symbol{Inputs: []ast.FormInput{c.in}})
			if !strings.Contains(s, c.wantSub) {
				t.Errorf("formSummary missing %q; got %q", c.wantSub, s)
			}
		})
	}
}

func TestFormSummary_NoInputsFallback(t *testing.T) {
	if got := formSummary(ast.Symbol{}); got != "form with inputs" {
		t.Errorf("empty symbol → %q; want fallback", got)
	}
}
