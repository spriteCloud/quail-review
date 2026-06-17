package composer

import (
	"strings"
	"testing"
)

// Verifies the composer's registered-step-pattern vocabulary stays in
// lockstep with pw_steps_bdd.tmpl. Adding a pattern in one place
// without the other lets the LLM propose Gherkin steps that have no
// runtime implementation.

func TestMatchesRegisteredPattern_BaselineVocabulary(t *testing.T) {
	// The v0.32 step additions — URL fragments, item counts, scroll,
	// menu, focus, dropdowns, keystrokes, waits, response status.
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

func TestRegisteredPatterns_RaceAndReloadVocab(t *testing.T) {
	// The v0.41c race + reload additions — exercise the LLM's edge
	// vocabulary without inventing step shapes the runtime can't bind.
	cases := []struct {
		text   string
		expect bool
	}{
		{"I submit the form twice in rapid succession", true},
		{"the form is not double-submitted", true},
		{"I reload the page", true},
		{"I submit the form THREE times", false},
		{"the form is not triple-submitted", false},
	}
	for _, c := range cases {
		got := matchesRegisteredPattern(c.text)
		if got != c.expect {
			t.Errorf("matchesRegisteredPattern(%q) = %v; want %v", c.text, got, c.expect)
		}
	}
}

func TestSystemPrompt_MentionsRaceAndReloadFamilies(t *testing.T) {
	// systemPrompt has to advertise the new vocabulary so the LLM
	// knows it can use it — otherwise we add patterns the model
	// will never propose.
	for _, sub := range []string{"rapid succession", "double-submitted", "reload"} {
		if !strings.Contains(systemPrompt, sub) {
			t.Errorf("systemPrompt missing %q vocabulary cue", sub)
		}
	}
}

func TestValidate_DoubleSubmitScenario(t *testing.T) {
	in := []ExtraScenario{
		{
			Name: "Double-submit is debounced",
			Steps: []Step{
				{Keyword: "Given", Text: "I open the landing page"},
				{Keyword: "When", Text: "I submit the form twice in rapid succession"},
				{Keyword: "Then", Text: "the form is not double-submitted"},
			},
		},
	}
	out := Validate(in)
	if len(out) != 1 {
		t.Fatalf("validate dropped a valid race-scenario; got %d", len(out))
	}
}
