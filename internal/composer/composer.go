// Package composer asks an LLM for additional Gherkin scenarios
// beyond what the deterministic templates emit. It's the answer to
// "templates are fixed; can scenarios be composed from parts to find
// more user flows?" — the deterministic baseline stays, and the model
// is constrained to compose Scenarios using ONLY the Given/When/Then
// patterns already registered in reviewqa.steps.ts.
//
// The composer is strictly OPT-IN. Default is off; the consumer must
// pass --llm <url>. The LLM is consulted at generation time only —
// emitted .feature files are deterministic Gherkin text and CI runs
// them with zero LLM dependency.
package composer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Client is the minimal LLM surface the composer needs. internal/llm
// Client satisfies it via its Chat method; tests pass a fake.
type Client interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// Journey is the probe-side context the composer renders into the
// model prompt. All fields optional; the model handles partial data.
type Journey struct {
	URL      string
	Kind     string // convert | contact | authenticate | ... | exercise
	Priority string // critical | standard | nice-to-have
	Title    string
	H1       string
	Links    []string // ranked, top-10
	Forms    []string // human-readable form summaries
	HasForm  bool
	// Pages provides per-link destination metadata so the composer
	// can propose Scenarios whose assertions match the page the
	// navigation lands on (not the journey's landing). v0.31 fix
	// for the cross-page h1 assertion bug observed against
	// spritecloud.com.
	Pages []PageContext
}

// PageContext is the resolved metadata of one page reachable from
// the journey's landing — Title + H1 keyed by relative href.
type PageContext struct {
	Href  string
	Title string
	H1    string
}

// ExtraScenario is one LLM-proposed Scenario in a normalized shape.
// Validated against the registered step patterns; invalid scenarios
// are dropped before the template renders.
type ExtraScenario struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags,omitempty"`
	Steps []Step   `json:"steps"`
}

// Step is one Gherkin step inside an ExtraScenario.
type Step struct {
	Keyword string `json:"keyword"` // Given | When | And | Then
	Text    string `json:"text"`    // verbatim Gherkin (placeholders substituted)
}

const systemPrompt = `You are a senior QA engineer composing additional Gherkin scenarios for a Playwright + playwright-bdd test suite.

You will receive a probe summary of a single user journey. The journey starts at a landing page and may chain through additional destination pages — each destination is listed with its own title and h1 so you can assert against the page the navigation actually lands on, not the landing.

Compose UP TO 5 additional Scenarios beyond the deterministic happy path. Prefer scenarios that:
- Chain across the listed destination pages (eg. submit → land on thank-you → reload)
- Exercise race conditions (eg. "I submit the form twice in rapid succession" → "the form is not double-submitted")
- Test reload mid-flow (eg. fill the form → reload → assert state)
- Explore boundary / failure variants the deterministic templates do not already cover

Do NOT propose scenarios that only assert the landing page's heading — those are already emitted deterministically.

Constraints:
- Each step.text MUST be a verbatim match (after placeholder substitution) of one of the registered patterns below.
- Each Scenario has 3-6 steps and starts with Given.
- Use Given for setup, When for actions, Then for assertions, And to chain.
- When asserting AFTER a navigation, use the destination page's title/h1 (NOT the landing's).
- Keep names short and concrete (under 10 words). Tag with @kind:edge or @kind:variant if relevant.
- Output STRICT JSON ONLY — no markdown fences, no commentary. Shape: an array of {name, tags?, steps:[{keyword,text}]}.

Registered step patterns (substitute the angle-bracketed placeholders with concrete values):

Given I open the landing page
Given I am on the landing page
Given I open the page "<path>"
When I click the link to "<href>"
When I navigate directly to "<href>"
When I enter "<value>" into the "<field>" field
When I submit the form
When I submit the form without filling any required field
Then the page title contains "<title>"
Then the main heading reads "<text>"
Then I see the heading "<text>"
Then no error message is shown in the form region
Then I remain on the same page
Then no success message is shown
Then the URL contains "<fragment>"
Then the page has at least <n> items
When I scroll to the bottom of the page
When I open the menu
When I close the menu
When I focus the "<field>" field
Then the "<field>" field has the value "<value>"
When I select "<option>" from the "<dropdown>" dropdown
When I press the "<key>" key
When I wait for <ms> milliseconds
Then the response status is <code>
Then I scroll into view of the "<text>" element
When I go back in the browser history
Given I am signed in as "<user>"
Given I am not signed in
When I submit the form twice in rapid succession
Then the form is not double-submitted
When I reload the page
`

// Propose asks the LLM for up to n additional scenarios for the
// journey. Returns ([], nil) when the model declines or returns
// nothing useful — never an error.
//
// v0.31: retries once with a stricter prompt when the first attempt
// returns un-parseable JSON.
// v0.33: when fb is non-empty, the prompt is extended with a "DO NOT
// re-propose" list so the model avoids repeating known-broken
// scenarios surfaced by the bug-discovery ledger.
func Propose(ctx context.Context, llm Client, j Journey, n int) ([]ExtraScenario, error) {
	return ProposeWithFeedback(ctx, llm, j, n, Feedback{})
}

// ProposeWithFeedback is the feedback-aware variant.
func ProposeWithFeedback(ctx context.Context, llm Client, j Journey, n int, fb Feedback) ([]ExtraScenario, error) {
	if llm == nil {
		return nil, nil
	}
	if n <= 0 {
		n = 3
	}
	user := buildUserPrompt(j, n) + fb.String()
	scenarios, err := proposeOnce(ctx, llm, systemPrompt, user)
	if err != nil {
		// Single retry with a stricter "JSON ONLY" reinforcement.
		strict := systemPrompt + "\n\nIMPORTANT: Your previous response was not parseable. Return ONLY a JSON array with no trailing commas, no commentary, no markdown fences. Validate your output is parseable before sending."
		scenarios, err = proposeOnce(ctx, llm, strict, user)
		if err != nil {
			return nil, err
		}
	}
	scenarios = Validate(scenarios)
	if len(scenarios) > n {
		scenarios = scenarios[:n]
	}
	return scenarios, nil
}

func proposeOnce(ctx context.Context, llm Client, system, user string) ([]ExtraScenario, error) {
	raw, err := llm.Chat(ctx, system, user)
	if err != nil {
		return nil, fmt.Errorf("composer: llm chat: %w", err)
	}
	scenarios, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("composer: parse response: %w", err)
	}
	return scenarios, nil
}

// ScenarioKey returns a deterministic fingerprint of a scenario's
// step sequence, used to dedup composed scenarios across journeys.
// The Name is intentionally excluded so semantically identical
// scenarios with different titles still collapse to one row.
func ScenarioKey(s ExtraScenario) string {
	parts := make([]string, 0, len(s.Steps))
	for _, st := range s.Steps {
		parts = append(parts, strings.ToLower(st.Keyword)+"|"+strings.TrimSpace(st.Text))
	}
	return strings.Join(parts, "\n")
}

// Dedup drops scenarios with duplicate step sequences. Returns the
// first occurrence of each unique sequence.
func Dedup(in []ExtraScenario) []ExtraScenario {
	seen := map[string]bool{}
	out := make([]ExtraScenario, 0, len(in))
	for _, s := range in {
		key := ScenarioKey(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func buildUserPrompt(j Journey, n int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\n", j.URL)
	fmt.Fprintf(&b, "Journey kind: %s (priority: %s)\n", j.Kind, j.Priority)
	if j.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", j.Title)
	}
	if j.H1 != "" {
		fmt.Fprintf(&b, "H1: %s\n", j.H1)
	}
	if len(j.Links) > 0 {
		fmt.Fprintf(&b, "Top links: %s\n", strings.Join(j.Links, ", "))
	}
	if j.HasForm {
		fmt.Fprintf(&b, "Page has a form. Form summaries: %s\n", strings.Join(j.Forms, "; "))
	}
	// v0.46 — cap destination-pages-in-prompt at 4. The DGX run
	// against spritecloud.com surfaced that journeys with 6+ steps
	// produced prompts whose response would not fit in the model's
	// output-token budget, returning truncated JSON. Keep the prompt
	// tight.
	pages := j.Pages
	const maxPagesInPrompt = 4
	if len(pages) > maxPagesInPrompt {
		pages = pages[:maxPagesInPrompt]
	}
	if len(pages) > 0 {
		b.WriteString("\nDestination pages (use these h1/title values when asserting AFTER navigation — do NOT assert the landing's h1 on a sub-page):\n")
		for _, p := range pages {
			fmt.Fprintf(&b, "  %s → title=%q, h1=%q\n", p.Href, p.Title, p.H1)
		}
		if len(j.Pages) > maxPagesInPrompt {
			fmt.Fprintf(&b, "  (... %d more destination pages omitted to keep the response short)\n", len(j.Pages)-maxPagesInPrompt)
		}
	}
	fmt.Fprintf(&b, "\nPropose up to %d additional Scenarios for this journey. JSON only.\n", n)
	return b.String()
}

// Parse extracts the JSON array of ExtraScenario from the model's raw
// response. Tolerant of leading prose, fenced code blocks, and the
// common LLM dirty-JSON shapes observed in production (trailing
// commas before `]` / `}`, smart quotes, doubled commas). v0.46 adds
// truncation recovery — when the response runs out mid-array because
// the model hit its output-token cap, we extract every complete
// scenario object that did fit and return those rather than throwing
// out the whole reply.
func Parse(raw string) ([]ExtraScenario, error) {
	start := strings.Index(raw, "[")
	if start < 0 {
		return nil, errors.New("composer: no JSON array in response")
	}
	end := strings.LastIndex(raw, "]")
	if end > start {
		jsonText := sanitizeDirtyJSON(raw[start : end+1])
		var out []ExtraScenario
		if err := json.Unmarshal([]byte(jsonText), &out); err == nil {
			return out, nil
		}
		// Fall through to partial recovery below — the full-array
		// parse failed, often because the model produced a truncated
		// or malformed-mid-stream array.
	}
	if recovered := parsePartialArray(raw[start:]); len(recovered) > 0 {
		return recovered, nil
	}
	return nil, errors.New("composer: response not parseable even with partial recovery")
}

// parsePartialArray walks a (possibly-truncated) JSON array body and
// returns every complete top-level {...} object that parses as an
// ExtraScenario. Objects are detected by brace balance, ignoring
// braces inside double-quoted strings.
func parsePartialArray(s string) []ExtraScenario {
	var out []ExtraScenario
	i := 0
	// Skip past the opening '['.
	for i < len(s) && s[i] != '[' {
		i++
	}
	if i >= len(s) {
		return nil
	}
	i++ // past '['
	for i < len(s) {
		// Skip whitespace + commas.
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ',') {
			i++
		}
		if i >= len(s) || s[i] == ']' {
			break
		}
		if s[i] != '{' {
			i++
			continue
		}
		// Find the matching '}'.
		depth := 0
		inStr := false
		escape := false
		j := i
		for ; j < len(s); j++ {
			c := s[j]
			if escape {
				escape = false
				continue
			}
			if c == '\\' && inStr {
				escape = true
				continue
			}
			if c == '"' {
				inStr = !inStr
				continue
			}
			if inStr {
				continue
			}
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if j >= len(s) || depth != 0 {
			// Truncated mid-object; stop here.
			break
		}
		obj := sanitizeDirtyJSON(s[i : j+1])
		var scenario ExtraScenario
		if err := json.Unmarshal([]byte(obj), &scenario); err == nil {
			out = append(out, scenario)
		}
		i = j + 1
	}
	return out
}

// sanitizeDirtyJSON strips the JSON dialects LLMs commonly emit:
// trailing commas, doubled commas, smart quotes, and `,]` / `,}`
// sequences. Tested in composer_v031_test.go against the actual
// dirty samples logged from the spritecloud.com DGX run.
func sanitizeDirtyJSON(s string) string {
	// Smart quotes → ASCII.
	s = strings.NewReplacer(
		"“", `"`, "”", `"`,
		"‘", "'", "’", "'",
	).Replace(s)
	// Collapse doubled commas.
	for strings.Contains(s, ",,") {
		s = strings.ReplaceAll(s, ",,", ",")
	}
	// Trailing commas before `]` / `}` — use a regex-free pass to
	// keep the parser cheap.
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == ']' || s[j] == '}') {
				// Skip the comma; the whitespace and closer follow naturally.
				continue
			}
		}
		out = append(out, c)
	}
	return string(out)
}

// Validate drops scenarios that don't conform to the registered step
// pattern set or carry an empty name. Returns the valid subset.
func Validate(in []ExtraScenario) []ExtraScenario {
	var out []ExtraScenario
	for _, s := range in {
		if strings.TrimSpace(s.Name) == "" {
			continue
		}
		if len(s.Steps) < 1 {
			continue
		}
		if !allStepsRegistered(s.Steps) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// allStepsRegistered returns true when every step in the scenario
// matches one of the registered patterns. Empty placeholder substitutions
// pass — the runtime step definitions handle them with sensible defaults.
func allStepsRegistered(steps []Step) bool {
	for _, s := range steps {
		if !matchesRegisteredPattern(s.Text) {
			return false
		}
	}
	return true
}

// registeredPatterns is the canonical list of Gherkin step texts the
// composer is allowed to produce. The "<...>" placeholders are
// substituted with concrete values; the regex normalises them out.
var registeredPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^I open the landing page$`),
	regexp.MustCompile(`^I am on the landing page$`),
	regexp.MustCompile(`^I open the page "[^"]+"$`),
	regexp.MustCompile(`^I click the link to "[^"]+"$`),
	regexp.MustCompile(`^I navigate directly to "[^"]+"$`),
	regexp.MustCompile(`^I enter "[^"]*" into the "[^"]+" field$`),
	regexp.MustCompile(`^I submit the form$`),
	regexp.MustCompile(`^I submit the form without filling any required field$`),
	regexp.MustCompile(`^the page title contains "[^"]+"$`),
	regexp.MustCompile(`^the main heading reads "[^"]+"$`),
	regexp.MustCompile(`^I see the heading "[^"]+"$`),
	regexp.MustCompile(`^no error message is shown in the form region$`),
	regexp.MustCompile(`^I remain on the same page$`),
	regexp.MustCompile(`^no success message is shown$`),
	// v0.32 additions — keep the order matching the step-defs file
	// for cross-reference.
	regexp.MustCompile(`^the URL contains "[^"]+"$`),
	regexp.MustCompile(`^the page has at least \d+ items$`),
	regexp.MustCompile(`^I scroll to the bottom of the page$`),
	regexp.MustCompile(`^I open the menu$`),
	regexp.MustCompile(`^I close the menu$`),
	regexp.MustCompile(`^I focus the "[^"]+" field$`),
	regexp.MustCompile(`^the "[^"]+" field has the value "[^"]*"$`),
	regexp.MustCompile(`^I select "[^"]+" from the "[^"]+" dropdown$`),
	regexp.MustCompile(`^I press the "[^"]+" key$`),
	regexp.MustCompile(`^I wait for \d+ milliseconds$`),
	regexp.MustCompile(`^the response status is \d+$`),
	regexp.MustCompile(`^I scroll into view of the "[^"]+" element$`),
	regexp.MustCompile(`^I go back in the browser history$`),
	regexp.MustCompile(`^I am signed in as "[^"]+"$`),
	regexp.MustCompile(`^I am not signed in$`),
	// v0.41c — race, reload, double-submit vocabulary. Each has a
	// matching step definition in pw_steps_bdd.tmpl.
	regexp.MustCompile(`^I submit the form twice in rapid succession$`),
	regexp.MustCompile(`^the form is not double-submitted$`),
	regexp.MustCompile(`^I reload the page$`),
}

func matchesRegisteredPattern(text string) bool {
	text = strings.TrimSpace(text)
	for _, re := range registeredPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}
