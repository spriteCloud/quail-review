package llm

import (
	"strings"
	"testing"
)

const fxSteps = `import { expect } from '@playwright/test'
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

const fxFeature = `Feature: spritecloud

  Scenario: visit contact
    Given I open the page "/contact"
    Then the main heading reads "Contact"
`

func TestParseSuiteRewritesTolerant(t *testing.T) {
	in := "Here you go:\n```json\n" + `{"feature_rewrites":[{"from":"a","to":"b"}],"steps_pattern_rewrites":[]}` + "\n```"
	got, ok := parseSuiteRewrites(in)
	if !ok {
		t.Fatalf("parse failed")
	}
	if len(got.FeatureRewrites) != 1 || got.FeatureRewrites[0].From != "a" || got.FeatureRewrites[0].To != "b" {
		t.Errorf("bad feature rewrites: %+v", got.FeatureRewrites)
	}
}

func TestValidateSuiteHappyPath(t *testing.T) {
	newSteps := strings.NewReplacer(
		`'I open the landing page'`, `'I land on the homepage'`,
		`/^I open the page "([^"]+)"$/`, `/^I visit the "([^"]+)" page$/`,
		`/^the main heading reads "([^"]+)"$/`, `/^the headline says "([^"]+)"$/`,
	).Replace(fxSteps)
	newFeature := strings.NewReplacer(
		`I open the page "/contact"`, `I visit the "/contact" page`,
		`the main heading reads "Contact"`, `the headline says "Contact"`,
	).Replace(fxFeature)
	rew := suiteRewrites{
		StepsPatternRewrites: []rewrite{
			{From: `/^I open the page "([^"]+)"$/`, To: `/^I visit the "([^"]+)" page$/`},
			{From: `/^the main heading reads "([^"]+)"$/`, To: `/^the headline says "([^"]+)"$/`},
		},
	}
	guard, ok := validateSuite([]byte(fxSteps), []byte(newSteps),
		[]SuiteFile{{Path: "x.feature", Body: []byte(newFeature)}}, rew)
	if !ok {
		t.Fatalf("expected pass, tripped guard %q", guard)
	}
}

func TestValidateSuiteRejectsArityDrop(t *testing.T) {
	// Pattern rewrite drops the {string} capture — handler still expects
	// a path arg, so the binding would silently break at runtime.
	rew := suiteRewrites{
		StepsPatternRewrites: []rewrite{
			{From: `/^I open the page "([^"]+)"$/`, To: `/^I open the landing page$/`},
		},
	}
	guard, ok := validateSuite([]byte(fxSteps), []byte(fxSteps),
		[]SuiteFile{{Path: "x.feature", Body: []byte(fxFeature)}}, rew)
	if ok {
		t.Fatalf("expected guard to trip, got pass")
	}
	if guard != "arity" {
		t.Errorf("expected guard=arity, got %q", guard)
	}
}

func TestValidateSuiteRejectsBindingDrift(t *testing.T) {
	// Rewrite the feature but NOT the matching pattern -> binding fails.
	newFeature := strings.Replace(fxFeature,
		`I open the page "/contact"`, `I navigate to "/contact"`, 1)
	guard, ok := validateSuite([]byte(fxSteps), []byte(fxSteps),
		[]SuiteFile{{Path: "x.feature", Body: []byte(newFeature)}}, suiteRewrites{})
	if ok {
		t.Fatalf("expected binding guard to trip, got pass")
	}
	if guard != "binding" {
		t.Errorf("expected guard=binding, got %q", guard)
	}
}

func TestUniqueStepPhrases(t *testing.T) {
	f := []SuiteFile{
		{Path: "a.feature", Body: []byte(fxFeature)},
		{Path: "b.feature", Body: []byte(fxFeature)},
	}
	got := uniqueStepPhrases(f)
	if len(got) != 2 {
		t.Fatalf("want 2 unique phrases (deduped across files), got %d: %v", len(got), got)
	}
}
