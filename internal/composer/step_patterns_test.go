package composer

import "testing"

const stepsFixture = `import { expect } from '@playwright/test'
import { createBdd } from 'playwright-bdd'
import { steps } from '../lib/steps'

const { Given, When, Then } = createBdd()

Given('I open the landing page', async ({ page }) => {
  await steps.visit(page, '/')
})

Given(/^I open the page "([^"]+)"$/, async ({ page }, path: string) => {
  await steps.visit(page, path)
})

Then(/^the main heading reads "([^"]+)"$/, async ({ page }, text: string) => {
  await steps.expectH1(page, text)
})
`

func TestExtractStepPatterns(t *testing.T) {
	got := ExtractStepPatterns([]byte(stepsFixture))
	if len(got) != 3 {
		t.Fatalf("want 3 patterns, got %d", len(got))
	}
	want := []string{
		"'I open the landing page'",
		`/^I open the page "([^"]+)"$/`,
		`/^the main heading reads "([^"]+)"$/`,
	}
	for i, w := range want {
		if got[i].Raw != w {
			t.Errorf("pattern %d: got %q want %q", i, got[i].Raw, w)
		}
	}
}

func TestIsGherkinSafeAgainst(t *testing.T) {
	patterns := ExtractStepPatterns([]byte(stepsFixture))
	good := []byte(`Feature: x
  Scenario: y
    Given I open the landing page
    Given I open the page "/contact"
    Then the main heading reads "Welcome"
`)
	if !IsGherkinSafeAgainst(good, patterns) {
		t.Errorf("expected good feature to bind")
	}
	bad := []byte(`Feature: x
  Scenario: y
    Given I navigate to the homepage
`)
	if IsGherkinSafeAgainst(bad, patterns) {
		t.Errorf("expected bad feature to fail binding")
	}
}

func TestPatternParamsEqual(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		want     bool
	}{
		{"identical literal", "'I open the landing page'", "'I open the landing page'", true},
		{"rephrased literal same arity", "'I am on the landing page'", "'I am on the home page'", true},
		{"rephrased regex same captures",
			`/^I open the page "([^"]+)"$/`,
			`/^I visit the "([^"]+)" page$/`,
			true},
		{"regex dropped capture",
			`/^I open the page "([^"]+)"$/`,
			`/^I open the landing page$/`,
			false},
		{"cross form rejected",
			"'I open the landing page'",
			`/^I open the landing page$/`,
			false},
		{"two captures preserved",
			`/^I enter "([^"]+)" into the "([^"]+)" field$/`,
			`/^I type "([^"]+)" in the "([^"]+)" box$/`,
			true},
		{"two captures reordered shapes still equal",
			`/^I enter "([^"]+)" into the "([^"]+)" field$/`,
			`/^I fill "([^"]+)" with "([^"]+)"$/`,
			true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PatternParamsEqual(c.a, c.b); got != c.want {
				t.Errorf("PatternParamsEqual(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestHandlerHashesStable(t *testing.T) {
	a := HandlerHashes([]byte(stepsFixture))
	b := HandlerHashes([]byte(stepsFixture))
	if len(a) != 3 || len(b) != 3 {
		t.Fatalf("want 3 hashes each, got a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("hash %d unstable: %q vs %q", i, a[i], b[i])
		}
	}

	// Rewriting only the pattern (first arg) must keep handler hashes
	// identical — the body and signature didn't change.
	rewritten := []byte(`import { expect } from '@playwright/test'
import { createBdd } from 'playwright-bdd'
import { steps } from '../lib/steps'

const { Given, When, Then } = createBdd()

Given('I land on the homepage', async ({ page }) => {
  await steps.visit(page, '/')
})

Given(/^I visit the "([^"]+)" page$/, async ({ page }, path: string) => {
  await steps.visit(page, path)
})

Then(/^the headline says "([^"]+)"$/, async ({ page }, text: string) => {
  await steps.expectH1(page, text)
})
`)
	c := HandlerHashes(rewritten)
	if len(c) != 3 {
		t.Fatalf("want 3 hashes after rewrite, got %d", len(c))
	}
	for i := range a {
		if a[i] != c[i] {
			t.Errorf("handler hash changed for #%d after pattern-only rewrite: %q -> %q", i, a[i], c[i])
		}
	}
}
