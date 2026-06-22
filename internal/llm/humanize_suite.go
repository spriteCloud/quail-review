package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"github.com/spriteCloud/quail-review/internal/composer"
	"github.com/spriteCloud/quail-review/internal/log"
)

// SuiteFile is one rendered file inside a BDD pair (either a .feature
// or the single .steps.ts). The humanize layer reads Body, may produce
// a rewritten copy, and writes it back into the caller's Rendered slice
// by Path.
type SuiteFile struct {
	Path string
	Body []byte
}

// HumanizeSuite humanizes a paired BDD batch — every .feature file plus
// its sibling .steps.ts — in a single LLM call. The model is told to
// rewrite step-text phrasing AND the matching cucumber-expression /
// regex pattern in lockstep so the runtime binding (Gherkin step ←→
// step-definition pattern) stays valid by construction.
//
// On any guard failure or LLM error the function returns the inputs
// unchanged. The whole pair reverts together — we never ship one
// humanized + one not, since a partial rewrite breaks the binding.
//
// v0.95.
func (c *Client) HumanizeSuite(ctx context.Context, symbol string, features []SuiteFile, steps SuiteFile) ([]SuiteFile, SuiteFile) {
	if !c.Enabled() || len(features) == 0 || len(steps.Body) == 0 {
		return features, steps
	}
	patterns := composer.ExtractStepPatterns(steps.Body)
	if len(patterns) == 0 {
		log.Warn("llm humanize suite: no step patterns extracted; skipping pair", "symbol", symbol)
		return features, steps
	}

	ctx, cancel := context.WithTimeout(ctx, c.cfg.LLMTimeout)
	defer cancel()
	raw, err := c.Chat(ctx, suiteSystemPrompt, buildSuitePrompt(symbol, features, steps, patterns))
	if err != nil {
		log.Warn("llm humanize suite failed; falling back to deterministic", "err", err, "symbol", symbol)
		return features, steps
	}

	rew, ok := parseSuiteRewrites(raw)
	if !ok {
		log.Warn("llm humanize suite: malformed response; falling back to deterministic", "symbol", symbol, "guard", "parse")
		return features, steps
	}

	// Apply rewrites in copies so a guard failure can roll back atomically.
	newSteps := SuiteFile{Path: steps.Path, Body: applyRewrites(steps.Body, rew.StepsPatternRewrites)}
	newFeatures := make([]SuiteFile, len(features))
	for i, f := range features {
		newFeatures[i] = SuiteFile{Path: f.Path, Body: applyRewrites(f.Body, rew.FeatureRewrites)}
	}

	if guard, ok := validateSuite(steps.Body, newSteps.Body, newFeatures, rew); !ok {
		log.Warn("llm humanize suite produced unrunnable Gherkin; falling back to deterministic",
			"symbol", symbol, "guard", guard)
		return features, steps
	}

	log.Debug("llm humanize suite applied", "symbol", symbol,
		"features", len(features), "feature_rewrites", len(rew.FeatureRewrites),
		"pattern_rewrites", len(rew.StepsPatternRewrites))
	return newFeatures, newSteps
}

// suiteRewrites mirrors the JSON schema the LLM must emit.
type suiteRewrites struct {
	FeatureRewrites      []rewrite `json:"feature_rewrites"`
	StepsPatternRewrites []rewrite `json:"steps_pattern_rewrites"`
}

func parseSuiteRewrites(s string) (suiteRewrites, bool) {
	// Tolerate fenced or prose-prefixed responses, same as parseRewrites.
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	var doc suiteRewrites
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return suiteRewrites{}, false
	}
	return doc, true
}

// validateSuite runs the four guards in order and returns the name of
// the first one that trips, for log-side diagnosability.
func validateSuite(oldSteps, newSteps []byte, newFeatures []SuiteFile, rew suiteRewrites) (string, bool) {
	if !structurePreserved("ts", oldSteps, newSteps) {
		return "structure", false
	}
	oldH, newH := composer.HandlerHashes(oldSteps), composer.HandlerHashes(newSteps)
	if len(oldH) != len(newH) {
		return "handler", false
	}
	for i := range oldH {
		if oldH[i] != newH[i] {
			return "handler", false
		}
	}
	for _, pr := range rew.StepsPatternRewrites {
		if pr.From == "" || pr.From == pr.To {
			continue
		}
		if !composer.PatternParamsEqual(pr.From, pr.To) {
			return "arity", false
		}
	}
	newPatterns := composer.ExtractStepPatterns(newSteps)
	if len(newPatterns) == 0 {
		return "patterns", false
	}
	for _, f := range newFeatures {
		if !composer.IsGherkinSafeAgainst(f.Body, newPatterns) {
			return "binding", false
		}
	}
	return "", true
}

// suiteSystemPrompt is the strict-JSON instruction we send to the
// humanizer. Tightened in v0.96.2 to coax local Qwen-class models
// (which often emit prose around their JSON or stop mid-object) into
// returning a single, complete JSON object every time. Three additions
// vs v0.96.0: (1) the response MUST start with `{` and end with `}`;
// (2) a complete valid example AND a complete valid empty example are
// both shown; (3) explicit no-prose / no-fence reminder at the end.
const suiteSystemPrompt = `You are a senior QA engineer humanizing a BDD test suite so it reads naturally to non-technical stakeholders.

You receive a list of .feature files AND the single matching .steps.ts step-definition file. You may rephrase BOTH:
- Step phrasing in .feature files (Given/When/Then text).
- The pattern argument of Given(...)/When(...)/Then(...) calls in .steps.ts.

You MUST keep them in lockstep so every step in every .feature still binds to a step-definition pattern after your rewrites.

OUTPUT FORMAT — READ CAREFULLY:
- Reply with a SINGLE JSON object and NOTHING else.
- The first character of your reply MUST be "{". The last character MUST be "}".
- NO prose before or after the JSON. NO markdown fences (no triple-backticks).
- NO comments inside the JSON.
- The schema is exactly:
    {
      "feature_rewrites":      [ { "from": "...", "to": "..." } ],
      "steps_pattern_rewrites":[ { "from": "...", "to": "..." } ]
    }
- Both arrays MUST be present, even if empty.

Rewrite rules:
- Each "from" must match EXACTLY a substring already present in the input.
- NEVER change the parameter / capture group SEQUENCE inside a step-def pattern. If the original is /^I open the page "([^"]+)"$/ the rewrite must still have exactly one ([^"]+) capture in the same position. {string}/{int} placeholders count as captures.
- NEVER change a literal-string pattern into a regex pattern or vice versa.
- NEVER edit anything other than the pattern argument inside Given(...)/When(...)/Then(...) — handler bodies, imports, control flow, indentation stay byte-identical.
- NEVER add or remove Scenarios / Steps / step definitions.
- Keep phrasing concise, present-tense, plainly readable.

EXAMPLE 1 — a valid response with one paired rewrite (return exactly these bytes, no fences):
{"feature_rewrites":[{"from":"I open the page \"contact\"","to":"I visit the \"contact\" page"}],"steps_pattern_rewrites":[{"from":"/^I open the page \"([^\"]+)\"$/","to":"/^I visit the \"([^\"]+)\" page$/"}]}

EXAMPLE 2 — a valid response when no humanization is warranted:
{"feature_rewrites":[],"steps_pattern_rewrites":[]}

Return one of the two shapes above, populated with your rewrites. Nothing else.`

func buildSuitePrompt(symbol string, features []SuiteFile, steps SuiteFile, patterns []composer.StepPattern) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Symbol under test: %s\n\n", symbol)
	fmt.Fprintf(&b, "Current step-def patterns (rewrite ONLY by substring; same form, same capture count):\n")
	for i, p := range patterns {
		fmt.Fprintf(&b, "  %2d. %s\n", i+1, p.Raw)
	}
	b.WriteString("\nUnique step phrases currently in the .feature files:\n")
	for _, ph := range uniqueStepPhrases(features) {
		fmt.Fprintf(&b, "  - %s\n", ph)
	}
	b.WriteString("\n---- tests/e2e/steps/quail.steps.ts ----\n")
	b.Write(steps.Body)
	for _, f := range features {
		fmt.Fprintf(&b, "\n---- %s ----\n", f.Path)
		b.Write(f.Body)
	}
	b.WriteString("\n\nReturn the JSON rewrites only.")
	return b.String()
}

// uniqueStepPhrases collects every distinct Given/When/Then body across
// the feature batch, sorted for prompt determinism. The model uses this
// to plan which rewrites it needs without having to dedup across files
// itself.
func uniqueStepPhrases(features []SuiteFile) []string {
	seen := map[string]struct{}{}
	for _, f := range features {
		scanner := bufio.NewScanner(bytes.NewReader(f.Body))
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for scanner.Scan() {
			t := strings.TrimSpace(scanner.Text())
			body, ok := stripStepKeyword(t)
			if !ok {
				continue
			}
			seen[body] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func stripStepKeyword(line string) (string, bool) {
	for _, kw := range []string{"Given ", "When ", "Then ", "And ", "But ", "* "} {
		if strings.HasPrefix(line, kw) {
			return strings.TrimSpace(line[len(kw):]), true
		}
	}
	return line, false
}
