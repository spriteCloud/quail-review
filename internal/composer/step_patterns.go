package composer

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// StepPattern is one step-definition pattern extracted from a rendered
// `.steps.ts` file. The template emits two shapes:
//
//	Given('I open the landing page', async (...) => { ... })       // literal
//	Given(/^I open the page "([^"]+)"$/, async (...) => { ... })   // regex
//
// We preserve the raw source form (`Raw`) so a paired rewrite can
// substring-replace it back into the file, and a compiled `Match`
// regex so we can validate `.feature` step bodies against it the same
// way IsGherkinSafe does for the frozen registry.
type StepPattern struct {
	Raw   string         // exact source slice, e.g. `'I open the landing page'` or `/^I open the page "([^"]+)"$/`
	Match *regexp.Regexp // anchored regex used to validate step bodies
}

// reStepDefCall matches a `Given(/When(/Then(` invocation in the rendered
// `.steps.ts` and captures the pattern argument verbatim. The pattern can
// be either a single-quoted string literal or a slash-delimited regex
// literal; the closing `,` after the pattern terminates the capture.
//
// We don't try to parse the whole TS file — the template's invocations
// are line-anchored and the pattern arg is always the first arg, so a
// per-line regex is sufficient. Misses (multi-line patterns, escaped
// closing slashes) fall back through to no match, which is safe: the
// pattern just won't be exposed to validation and the .feature side
// will fail IsGherkinSafeAgainst, dropping the rewrite back to
// deterministic.
var reStepDefCall = regexp.MustCompile(`(?m)^\s*(?:Given|When|Then)\(\s*('[^']*'|/[^\n]+?/)\s*,`)

// ExtractStepPatterns walks a rendered `.steps.ts` file and returns
// every Given/When/Then pattern in source order.
func ExtractStepPatterns(stepsTS []byte) []StepPattern {
	var out []StepPattern
	for _, m := range reStepDefCall.FindAllSubmatch(stepsTS, -1) {
		raw := string(m[1])
		sp, ok := compileStepPattern(raw)
		if !ok {
			continue
		}
		out = append(out, sp)
	}
	return out
}

// compileStepPattern converts the raw pattern source (either `'lit'` or
// `/regex/`) into a compiled, anchored matcher for IsGherkinSafeAgainst.
func compileStepPattern(raw string) (StepPattern, bool) {
	switch {
	case strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2:
		body := raw[1 : len(raw)-1]
		// String-literal patterns are exact-match cucumber expressions.
		// {string} / {int} are the only placeholders the template uses
		// in this form; expand them to the same regex bodies the
		// equivalent regex-form patterns use.
		body = cucumberToRegex(body)
		re, err := regexp.Compile(`^` + body + `$`)
		if err != nil {
			return StepPattern{}, false
		}
		return StepPattern{Raw: raw, Match: re}, true
	case strings.HasPrefix(raw, "/") && strings.HasSuffix(raw, "/") && len(raw) >= 2:
		body := raw[1 : len(raw)-1]
		re, err := regexp.Compile(body)
		if err != nil {
			return StepPattern{}, false
		}
		return StepPattern{Raw: raw, Match: re}, true
	}
	return StepPattern{}, false
}

// cucumberToRegex expands the subset of cucumber-expression placeholders
// the step-def template uses. The template only uses {string} and {int}
// in literal-form patterns today; unknown placeholders are left literal
// so an unrunnable rewrite trips IsGherkinSafeAgainst rather than
// silently passing.
func cucumberToRegex(s string) string {
	// Escape regex metachars first, then substitute placeholders.
	s = regexp.QuoteMeta(s)
	s = strings.ReplaceAll(s, `\{string\}`, `"[^"]*"`)
	s = strings.ReplaceAll(s, `\{int\}`, `\d+`)
	return s
}

// IsGherkinSafeAgainst is the dynamic-vocabulary sibling of IsGherkinSafe.
// It validates that every Given/When/Then step body in `feature` matches
// at least one pattern in `patterns`. Used by the paired-humanize path,
// where the LLM may have rewritten both the .feature text AND the
// step-def patterns in the same pass.
func IsGherkinSafeAgainst(feature []byte, patterns []StepPattern) bool {
	if len(patterns) == 0 {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(feature))
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		body, ok := stripGherkinKeyword(trimmed)
		if !ok {
			continue
		}
		matched := false
		for _, p := range patterns {
			if p.Match.MatchString(body) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// PatternParamsEqual reports whether two raw step-def patterns have the
// same placeholder / capture sequence. This is the contract that
// guarantees handler bodies — which destructure the captured args
// positionally — still work after the LLM rewrites the pattern phrasing.
//
// Two literal-form patterns are equal iff both have the same
// cucumber-expression placeholders in the same order (today: {string}
// and {int}). Two regex-form patterns are equal iff their non-escaped
// `(...)` capture groups occur in the same order and have the same
// inner shape. A literal-vs-regex cross-form rewrite is rejected: if
// the model wants to change the form it should leave the binding alone.
func PatternParamsEqual(oldRaw, newRaw string) bool {
	oldKind, oldParams := paramSequence(oldRaw)
	newKind, newParams := paramSequence(newRaw)
	if oldKind != newKind {
		return false
	}
	if len(oldParams) != len(newParams) {
		return false
	}
	for i := range oldParams {
		if oldParams[i] != newParams[i] {
			return false
		}
	}
	return true
}

// paramSequence returns the pattern kind ("lit" or "re") and its
// ordered placeholder/capture shapes. Unknown kind → ("", nil) which
// makes PatternParamsEqual reject the pair.
func paramSequence(raw string) (string, []string) {
	switch {
	case strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2:
		return "lit", reCucumberPlaceholder.FindAllString(raw[1:len(raw)-1], -1)
	case strings.HasPrefix(raw, "/") && strings.HasSuffix(raw, "/") && len(raw) >= 2:
		body := raw[1 : len(raw)-1]
		return "re", regexCaptureShapes(body)
	}
	return "", nil
}

var reCucumberPlaceholder = regexp.MustCompile(`\{(?:string|int|float|word)\}`)

// regexCaptureShapes returns the inner text of each non-escaped `(...)`
// capture group in source order. We treat (?:...) as non-capturing and
// honour backslash-escaped parens. Nested captures are flattened
// depth-first which is sufficient — the template doesn't nest.
func regexCaptureShapes(re string) []string {
	var shapes []string
	depth := 0
	start := 0
	for i := 0; i < len(re); i++ {
		switch re[i] {
		case '\\':
			i++ // skip escaped char
		case '(':
			// Skip non-capturing groups: `(?:`, `(?=`, `(?!`, `(?<...`
			if i+2 < len(re) && re[i+1] == '?' {
				continue
			}
			if depth == 0 {
				start = i + 1
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
				if depth == 0 && start > 0 && start <= i {
					shapes = append(shapes, re[start:i])
				}
			}
		}
	}
	return shapes
}

// reStepDefHandlerBlock matches a full Given(/When(/Then(...) invocation
// — pattern, handler signature, body, closing brace and paren — so we
// can hash the handler body across a paired rewrite. The body is
// captured non-greedily; the closing `})` is anchored to the start of
// a line so we don't truncate at the first `})` inside the handler
// (e.g. an inline object literal).
var reStepDefHandlerBlock = regexp.MustCompile(
	`(?ms)^\s*(Given|When|Then)\(\s*('[^']*'|/[^\n]+?/)\s*,\s*async\s*\(([^)]*)\)\s*=>\s*\{(.*?)^\}\)`,
)

// HandlerHashes returns one hash per Given/When/Then invocation in the
// rendered `.steps.ts`, keyed by handler-position index. Two files have
// equivalent handlers iff the slices are identical.
func HandlerHashes(stepsTS []byte) []string {
	var out []string
	for _, m := range reStepDefHandlerBlock.FindAllSubmatch(stepsTS, -1) {
		// m[3] = signature args, m[4] = body. We hash sig+body so a
		// silent rename of a destructured arg is also caught.
		out = append(out, fmt.Sprintf("%x:%x", trimAllWS(m[3]), trimAllWS(m[4])))
	}
	return out
}

func trimAllWS(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		out = append(out, c)
	}
	return out
}
