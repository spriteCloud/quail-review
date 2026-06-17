package composer

import "testing"

// v0.62 — ValidateAgainst should drop scenarios that assert against
// destination metadata the page does not actually render, and keep
// scenarios that match (loosely) or that reference pages with no
// recorded metadata.

func TestValidateAgainst_DropsMismatchedDestinationH1(t *testing.T) {
	j := Journey{
		URL:   "https://example.com/",
		Title: "Home",
		H1:    "Welcome",
		Pages: []PageContext{
			{Href: "/about", Title: "About — Example", H1: "About us"},
		},
	}
	scenarios := []ExtraScenario{
		{
			Name: "navigate to about and assert wrong h1",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I click the link to "/about"`},
				{Keyword: "Then", Text: `the main heading reads "Pricing"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 0 {
		t.Fatalf("expected scenario asserting wrong h1 to be dropped; got %d kept", len(out))
	}
}

func TestValidateAgainst_KeepsMatchingDestinationH1(t *testing.T) {
	j := Journey{
		URL:   "https://example.com/",
		Title: "Home",
		H1:    "Welcome",
		Pages: []PageContext{
			{Href: "/about", Title: "About — Example", H1: "About us"},
		},
	}
	scenarios := []ExtraScenario{
		{
			Name: "navigate and assert matching h1",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I click the link to "/about"`},
				{Keyword: "Then", Text: `the main heading reads "About"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 1 {
		t.Fatalf("expected scenario with matching h1 to be kept; got %d kept", len(out))
	}
}

func TestValidateAgainst_KeepsLandingPageMatch(t *testing.T) {
	j := Journey{
		URL:   "https://example.com/",
		Title: "Home — Example",
		H1:    "Welcome to Example",
	}
	scenarios := []ExtraScenario{
		{
			Name: "landing assertion",
			Steps: []Step{
				{Keyword: "Given", Text: `I am on the landing page`},
				{Keyword: "Then", Text: `the main heading reads "Welcome"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 1 {
		t.Fatalf("expected landing-page-h1 scenario to be kept; got %d", len(out))
	}
}

func TestValidateAgainst_DropsLandingPageMismatch(t *testing.T) {
	j := Journey{
		URL: "https://example.com/",
		H1:  "Welcome",
	}
	scenarios := []ExtraScenario{
		{
			Name: "landing assertion mismatch",
			Steps: []Step{
				{Keyword: "Given", Text: `I am on the landing page`},
				{Keyword: "Then", Text: `the main heading reads "Sign in"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 0 {
		t.Fatalf("expected landing mismatch to be dropped; got %d kept", len(out))
	}
}

func TestValidateAgainst_PermissiveForUnknownDestinations(t *testing.T) {
	j := Journey{
		URL:   "https://example.com/",
		H1:    "Welcome",
		Pages: []PageContext{
			{Href: "/known", H1: "Known Page"},
		},
	}
	scenarios := []ExtraScenario{
		{
			Name: "navigates to unknown page",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I click the link to "/unknown"`},
				{Keyword: "Then", Text: `the main heading reads "Anything"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 1 {
		t.Fatalf("expected unknown-destination scenario to be kept (no signal to refute); got %d", len(out))
	}
}

func TestValidateAgainst_MatchesTitleAssertion(t *testing.T) {
	j := Journey{
		URL: "https://example.com/",
		Pages: []PageContext{
			{Href: "/pricing", Title: "Pricing — Example", H1: "Choose a plan"},
		},
	}
	scenarios := []ExtraScenario{
		{
			Name: "title assertion matches",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I navigate directly to "/pricing"`},
				{Keyword: "Then", Text: `the page title contains "Pricing"`},
			},
		},
		{
			Name: "title assertion mismatches",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I navigate directly to "/pricing"`},
				{Keyword: "Then", Text: `the page title contains "Checkout"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 1 {
		t.Fatalf("expected exactly one of two title-assert scenarios to survive; got %d", len(out))
	}
	if out[0].Name != "title assertion matches" {
		t.Fatalf("expected the matching title scenario to survive; got %q", out[0].Name)
	}
}

func TestValidateAgainst_SeeTheHeadingFormAlsoChecked(t *testing.T) {
	j := Journey{
		URL: "https://example.com/",
		Pages: []PageContext{
			{Href: "/about", H1: "About us"},
		},
	}
	scenarios := []ExtraScenario{
		{
			Name: "see-the-heading mismatch is dropped",
			Steps: []Step{
				{Keyword: "Given", Text: `I open the landing page`},
				{Keyword: "When", Text: `I click the link to "/about"`},
				{Keyword: "Then", Text: `I see the heading "Pricing"`},
			},
		},
	}
	out := ValidateAgainst(scenarios, j)
	if len(out) != 0 {
		t.Fatalf("expected see-the-heading mismatch to be dropped; got %d", len(out))
	}
}
