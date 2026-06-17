package composer

import "testing"

func TestMatchesRegisteredPattern_V032Additions(t *testing.T) {
	yes := []string{
		`the URL contains "/cart"`,
		`the page has at least 3 items`,
		`I scroll to the bottom of the page`,
		`I open the menu`,
		`I close the menu`,
		`I focus the "email" field`,
		`the "email" field has the value "me@x.test"`,
		`I select "Large" from the "size" dropdown`,
		`I press the "Enter" key`,
		`I wait for 500 milliseconds`,
		`the response status is 204`,
		`I scroll into view of the "Contact" element`,
	}
	for _, s := range yes {
		if !matchesRegisteredPattern(s) {
			t.Errorf("%q should match a registered pattern", s)
		}
	}
}
