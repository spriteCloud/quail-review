package main

import (
	"context"
	"strings"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-core/mindmap"
	"github.com/spriteCloud/quail-core/oplist"
	"github.com/spriteCloud/quail-core/plan"
)

// composeOpListJourneys walks post-probe items and asks the LLM to
// compose an op-list Journey for each TmplPlaywrightHappyFlow item.
// Successful journeys are attached to `it.Journey`; the op-list
// renderer (gen.renderJourneyOpList) reads them at Render time.
//
// When the LLM is disabled or a compose call errors, the item is left
// alone — the renderer falls back to a canned "goto + main visible"
// journey so smoke coverage isn't zero.
func composeOpListJourneys(ctx context.Context, cfg config.Config, items []plan.Item) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() {
		return items
	}
	rlog.Info("oplist: requesting composed journeys",
		"model", cfg.Model, "endpoint", cfg.OpenAIBaseURL)
	composed := 0
	for i := range items {
		if items[i].Template != plan.TmplPlaywrightHappyFlow {
			continue
		}
		// Skip items whose Journey is already set — the suite-wide
		// pass in appendSuiteJourneys pre-populates its own additions,
		// and re-composing over them would throw the strategic output
		// away in favour of a title-only re-derivation.
		if items[i].Journey != nil {
			continue
		}
		in := composeInputFor(items[i])
		j, err := oplist.Compose(ctx, client, in)
		if err != nil {
			rlog.Warn("oplist: compose failed, falling back to canned journey",
				"page", in.URL, "err", err)
			continue
		}
		// v1.12 — post-decode grounding filter. Drops steps whose
		// (role, name) or (label) can't be found in ANY symbol's
		// anchors/inputs/contents across the journey chain. The LLM
		// routinely invents Contact-form fields, hero headings, and
		// nav links that don't exist on the terminal page; those
		// steps would fail verify with 'locator not found' at test
		// time. Grounding drops them BEFORE emit. Returns the number
		// of dropped steps + a boolean indicating whether the journey
		// itself should be dropped (too few steps left).
		grounded, dropped, drop := groundJourney(j, items[i])
		if drop {
			rlog.Info("oplist: dropped hallucinated journey", "page", in.URL, "dropped_steps", dropped)
			continue
		}
		items[i].Journey = &grounded
		composed++
		rlog.Info("oplist: composed journey",
			"page", in.URL, "steps", len(grounded.Steps), "dropped_steps", dropped)
	}
	rlog.Info("oplist: composition done", "composed", composed)
	return items
}

// groundJourney filters a composed Journey against the mindmap-observed
// anchors carried by an Item's Symbols. v1.14 — grounds PER-PAGE: each
// step's (role, name) or (label) must match an anchor on THE PAGE the
// step actually lands on, tracked via preceding `goto` verbs, not just
// "some page in the chain". Chain-wide grounding let LLM emissions
// slip through where the terminal page couldn't back the assertion —
// classic case: opGoto('/contact') then opSeen('heading', 'Performance
// Testing'), where Performance Testing IS on the site but on a
// different page. That test would emit, verify would fail, heal
// would exhaust. Per-page grounding kills it at emit time.
//
// Falls back to chain-wide grounding when the current URL doesn't
// resolve to a known symbol (LLM landed somewhere the mindmap
// didn't crawl) — trust the LLM rather than drop everything.
//
// Returns:
//   - filtered Journey (goto steps and grounded steps only)
//   - count of dropped steps
//   - drop bool: true if the journey should be discarded (too few
//     steps left OR terminal step is a bare `goto` with no assertion)
func groundJourney(j plan.Journey, it plan.Item) (plan.Journey, int, bool) {
	symbols := symbolsFor(it)
	if len(symbols) == 0 {
		// No mindmap context to ground against — trust the LLM and
		// return the journey untouched. This preserves the historical
		// behavior on Item shapes that don't carry probe symbols.
		return j, 0, false
	}

	// Build a URL → *Symbol map so per-step grounding is a
	// dictionary lookup keyed by the running "current URL".
	byURL := make(map[string]*ast.Symbol, len(symbols))
	for i := range symbols {
		u := normalizeGroundURL(symbols[i].AbsoluteURL)
		if u != "" {
			byURL[u] = &symbols[i]
		}
	}

	// Start URL: whichever URL the landing symbol carries. Empty
	// string is legal — the chain-wide fallback kicks in for
	// steps where the URL is unknown.
	currentURL := normalizeGroundURL(symbols[0].AbsoluteURL)

	out := plan.Journey{Title: j.Title, Kind: j.Kind}
	dropped := 0
	for _, s := range j.Steps {
		switch s.Op {
		case "goto":
			// Update the running URL. Absolute + relative paths both
			// normalize to a leading-slash path so the byURL map hits.
			currentURL = normalizeGroundURL(s.Path)
			out.Steps = append(out.Steps, s)
		case "press":
			// No content assertion — no grounding needed.
			out.Steps = append(out.Steps, s)
		case "click", "seen":
			if pageBacksRoleName(byURL, currentURL, symbols, s.Role, s.Name) {
				out.Steps = append(out.Steps, s)
			} else {
				dropped++
			}
		case "fill":
			if pageBacksInputLabel(byURL, currentURL, symbols, s.Label) {
				out.Steps = append(out.Steps, s)
			} else {
				dropped++
			}
		default:
			// Unknown op verbs are already blocked by decodeJourney
			// in quail-core; belt-and-suspenders here means we drop
			// rather than emit.
			dropped++
		}
	}
	// Journey needs at least one navigation + one assertion. A single
	// goto with everything else dropped is a smoke that asserts
	// nothing — worse than no journey.
	if len(out.Steps) < 2 {
		return plan.Journey{}, dropped, true
	}
	if !hasAssertion(out.Steps) {
		return plan.Journey{}, dropped, true
	}
	return out, dropped, false
}

// pageBacksRoleName checks the specific page (by URL) first; falls
// back to chain-wide grounding when the URL doesn't resolve. The
// fallback preserves v1.13 behavior for chains landing on pages the
// probe didn't crawl.
func pageBacksRoleName(
	byURL map[string]*ast.Symbol, currentURL string, chain []ast.Symbol,
	role, name string,
) bool {
	if sym, ok := byURL[currentURL]; ok && sym != nil {
		return symbolHasRoleName(*sym, role, name)
	}
	return anySymbolHasRoleName(chain, role, name)
}

func pageBacksInputLabel(
	byURL map[string]*ast.Symbol, currentURL string, chain []ast.Symbol,
	label string,
) bool {
	if sym, ok := byURL[currentURL]; ok && sym != nil {
		return symbolHasInputLabel(*sym, label)
	}
	return anySymbolHasInputLabel(chain, label)
}

// normalizeGroundURL strips protocol + host + trailing slash so the
// byURL map can be keyed uniformly whether the source is a full
// Symbol.AbsoluteURL ("https://www.spritecloud.com/contact") or a
// relative goto step ("/contact" or just "contact").
func normalizeGroundURL(u string) string {
	u = trim(u)
	if u == "" {
		return ""
	}
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(u, prefix) {
			u = u[len(prefix):]
			if i := strings.Index(u, "/"); i >= 0 {
				u = u[i:]
			} else {
				u = "/"
			}
			break
		}
	}
	// Ensure leading slash (relative paths without one).
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	// Strip trailing slash except for the root.
	if len(u) > 1 && strings.HasSuffix(u, "/") {
		u = u[:len(u)-1]
	}
	return u
}

// symbolsFor returns the flat slice of ast.Symbol carried by an Item,
// preferring the multi-page chain (Symbols) over the single-page
// legacy field (Symbol). Empty when neither is populated.
func symbolsFor(it plan.Item) []ast.Symbol {
	if len(it.Symbols) > 0 {
		return it.Symbols
	}
	if it.Symbol.PageTitle != "" || len(it.Symbol.Contents) > 0 {
		return []ast.Symbol{it.Symbol}
	}
	return nil
}

// hasAssertion returns true when the step list contains at least one
// `seen` op — the terminal check that turns a spec into a smoke.
// Journeys without a seen step are click-and-hope; drop them.
func hasAssertion(steps []plan.Op) bool {
	for _, s := range steps {
		if s.Op == "seen" {
			return true
		}
	}
	return false
}

// symbolHasRoleName is the per-page grounding predicate — checks a
// single symbol for a role+name match. v1.13 role-strict rules apply
// (menuitem needs explicit role tag, buttons accept implicit).
// Fuzzy match on name; empty name always matches. Composer callers:
// use pageBacksRoleName which layers a chain-wide fallback around this.
func symbolHasRoleName(s ast.Symbol, role, name string) bool {
	if trim(name) == "" {
		return true
	}
	switch role {
	case "heading":
		for _, c := range s.Contents {
			if (c.Tag == "h1" || c.Tag == "h2" || c.Tag == "title") &&
				(nameMatches(name, c.AccessibleName) || nameMatches(name, c.Text)) {
				return true
			}
		}
	case "link":
		// A link anchor may not carry an explicit Role attribute
		// (implicit role on <a href> is 'link'). Accept anchors
		// with no Role OR Role='link'.
		for _, l := range s.Links {
			if l.Role != "" && l.Role != "link" {
				continue
			}
			if nameMatches(name, l.AccessibleName) ||
				nameMatches(name, l.Text) ||
				nameMatches(name, l.Name) {
				return true
			}
		}
	case "menuitem":
		// v1.13 — menuitem is stricter: the anchor must have been
		// explicitly role-tagged as menuitem. A plain link is NOT a
		// menuitem for Playwright's getByRole purposes.
		for _, l := range s.Links {
			if l.Role != "menuitem" {
				continue
			}
			if nameMatches(name, l.AccessibleName) ||
				nameMatches(name, l.Text) ||
				nameMatches(name, l.Name) {
				return true
			}
		}
		for _, a := range s.Anchors {
			if a.Role != "menuitem" {
				continue
			}
			if nameMatches(name, a.AccessibleName) ||
				nameMatches(name, a.Text) ||
				nameMatches(name, a.Name) {
				return true
			}
		}
	case "button":
		for _, a := range s.Anchors {
			// v1.13 — accept implicit button role (empty Role,
			// Tag=submit/button) OR explicit Role='button' / 'submit'.
			if a.Role != "" && a.Role != "button" && a.Role != "submit" {
				continue
			}
			if nameMatches(name, a.AccessibleName) ||
				nameMatches(name, a.Text) ||
				nameMatches(name, a.Name) {
				return true
			}
		}
	default:
		// Roles like 'alert', 'status', 'main', 'navigation',
		// 'dialog', 'tab' aren't backed by a specific anchor
		// collection in probe output — trust the LLM.
		return true
	}
	return false
}

// symbolHasInputLabel checks a single symbol for a form input whose
// label/aria/placeholder matches. Fuzzy match; empty label matches.
func symbolHasInputLabel(s ast.Symbol, label string) bool {
	if trim(label) == "" {
		return true
	}
	for _, in := range s.Inputs {
		if nameMatches(label, in.LabelText) ||
			nameMatches(label, in.Aria) ||
			nameMatches(label, in.Placeholder) {
			return true
		}
	}
	return false
}

// anySymbolHasRoleName is the chain-wide fallback used by
// pageBacksRoleName when the current URL doesn't resolve to a known
// symbol. Loops symbolHasRoleName over the whole chain.
func anySymbolHasRoleName(symbols []ast.Symbol, role, name string) bool {
	if trim(name) == "" {
		return true
	}
	for _, s := range symbols {
		if symbolHasRoleName(s, role, name) {
			return true
		}
	}
	return false
}

// anySymbolHasInputLabel is the chain-wide fallback for opFill.
func anySymbolHasInputLabel(symbols []ast.Symbol, label string) bool {
	if trim(label) == "" {
		return true
	}
	for _, s := range symbols {
		if symbolHasInputLabel(s, label) {
			return true
		}
	}
	return false
}

// nameMatches is the fuzzy substring check used by grounding. Both
// directions: the step's name may be a shorter reference ("Contact")
// to a longer accessible name ("Contact spriteCloud team"), or the
// LLM may emit a slightly longer version of what's actually on the
// page. Empty candidate never matches (avoid false positives).
func nameMatches(stepName, candidate string) bool {
	stepName = strings.ToLower(trim(stepName))
	candidate = strings.ToLower(trim(candidate))
	if stepName == "" || candidate == "" {
		return false
	}
	return strings.Contains(candidate, stepName) || strings.Contains(stepName, candidate)
}

func composeInputFor(it plan.Item) oplist.ComposeInput {
	in := oplist.ComposeInput{
		URL:         it.PageURL,
		JourneyKind: it.JourneyKind,
	}
	if len(it.Symbols) > 0 {
		in.Title = it.Symbols[0].PageTitle
		in.Hints = symbolHints(it.Symbols[0])
		// Intermediate + terminal pages the mindmap already walked.
		// Feeding these to the LLM lets it write a real multi-page
		// journey instead of proposing steps from the landing alone.
		for _, s := range it.Symbols[1:] {
			in.Chain = append(in.Chain, oplist.StepHint{
				URL:        symbolStepURL(s),
				Title:      s.PageTitle,
				EnteredVia: s.EnteredVia,
				Hints:      symbolHints(s),
			})
		}
	} else if it.Symbol.PageTitle != "" {
		in.Title = it.Symbol.PageTitle
		in.Hints = symbolHints(it.Symbol)
	}
	return in
}

// symbolStepURL prefers the resolved absolute URL when present (that's
// what a `page.goto()` argument looks like); falls back to the raw
// File field which the probe writes for pages reached via direct goto.
func symbolStepURL(s ast.Symbol) string {
	if s.AbsoluteURL != "" {
		return s.AbsoluteURL
	}
	return s.File
}

// symbolHints returns a short observed-on-page list from a probed
// symbol — headings, key form-input labels, top nav links. Kept small
// so the prompt stays cheap.
func symbolHints(s ast.Symbol) []string {
	var hints []string
	for _, c := range s.Contents {
		if c.Tag == "h1" || c.Tag == "h2" {
			// v1.10 — prefer the probe-computed AccessibleName over
			// raw innerText for heading hints. Playwright's
			// getByRole('heading', {name}) matches by accessible name
			// at test time, and aria-labelledby / aria-label overrides
			// on hero headings routinely diverge from innerText on
			// sites like spritecloud.com (innerText="Home", a11y-name=
			// "Home - spriteCloud test your software, not your reputation").
			name := trim(firstNonEmpty(c.AccessibleName, c.Text))
			if name != "" {
				hints = append(hints, c.Tag+": "+name)
			}
		}
		if len(hints) >= 4 {
			break
		}
	}
	for _, in := range s.Inputs {
		// Prefer real label/aria (getByLabel-resolvable). Fall back
		// to placeholder — opFill now tries getByLabel first and
		// getByPlaceholder second, so passing a placeholder-only hint
		// as the fill label still works. Skip inputs whose only
		// signal is the raw `name` attribute — that never resolves
		// to a Playwright-visible target.
		label := firstNonEmpty(in.LabelText, in.Aria, in.Placeholder)
		if label == "" {
			continue
		}
		hints = append(hints, "input: "+label)
		if len(hints) >= 8 {
			break
		}
	}
	for _, l := range s.Links {
		// v1.9 — prefer the browser-computed accessible name (quail-core
		// v0.19.0 emits it per link via aria-labelledby > aria-label >
		// visible textContent, whitespace-collapsed). Playwright's
		// getByRole('link', {name}) matches against that at test time.
		// Fall back to raw visible text for anchors extracted without
		// a probe run (older mindmap snapshots, TS extractor output).
		//
		// v1.8.1's "prefer l.Aria" attempt was a regression: on link
		// records `LocatorAnchor.Aria` is overloaded to carry the href
		// (browser_probe.go:279), so preferring l.Aria meant every
		// label became an href → looksLikeHref filter dropped it →
		// the LLM received NO link hints at all. Reverted here as a
		// side-effect of moving the semantic to AccessibleName.
		label := firstNonEmpty(l.AccessibleName, l.Text, l.Name)
		if label == "" || looksLikeHref(label) {
			continue
		}
		hints = append(hints, "link: "+label)
		if len(hints) >= 12 {
			break
		}
	}
	return hints
}

// looksLikeHref is a cheap check to keep href-shaped strings out of
// link-name hints. `/`, `#`-anchors, `mailto:`, absolute URLs — none
// of them are what a `getByRole('link', {name})` matches.
func looksLikeHref(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '/' || s[0] == '#' {
		return true
	}
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "mailto:") ||
		strings.HasPrefix(s, "tel:")
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if t := trim(v); t != "" {
			return t
		}
	}
	return ""
}

func trim(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

// appendCoverageJourneys asks the LLM to write journeys for pages the
// mindmap crawled but no existing journey visited. Runs AFTER
// composeOpListJourneys + appendSuiteJourneys + appendNegativeJourneys
// so it sees the fullest possible "touched URLs" set. Silent no-op
// when the LLM is disabled, no map available, or every page already
// covered.
func appendCoverageJourneys(ctx context.Context, cfg config.Config, items []plan.Item, maps map[string]*mindmap.Map) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() || len(maps) == 0 {
		return items
	}
	touched := collectTouchedURLs(items)
	existing := existingJourneyTitles(items)
	added := 0
	for origin, m := range maps {
		if m == nil {
			continue
		}
		extras, err := oplist.ComposeCoverageGaps(ctx, client, m, touched, existing)
		if err != nil {
			rlog.Warn("oplist: coverage compose failed", "origin", origin, "err", err)
			continue
		}
		for i := range extras {
			j := extras[i]
			items = append(items, coverageJourneyItem(j, origin))
			added++
		}
	}
	if added > 0 {
		rlog.Info("oplist: coverage compose done", "added", added)
	}
	return items
}

// collectTouchedURLs walks every attached Journey and returns the set
// of URLs referenced by any goto step. Used by the coverage-gap pass
// to decide what to skip.
func collectTouchedURLs(items []plan.Item) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if it.Journey == nil {
			continue
		}
		for _, s := range it.Journey.Steps {
			if s.Op != "goto" || s.Path == "" {
				continue
			}
			if seen[s.Path] {
				continue
			}
			seen[s.Path] = true
			out = append(out, s.Path)
		}
	}
	return out
}

// coverageJourneyItem wraps a coverage-gap Journey the same way suite
// and negative wrappers do — a synthetic Symbol carrying the title,
// OutPath under tests/e2e/coverage-<slug>.spec.ts.
func coverageJourneyItem(j plan.Journey, origin string) plan.Item {
	stem := jsSlug(j.Title)
	sym := ast.Symbol{
		Name:      stem,
		Kind:      ast.KindComponent,
		File:      origin,
		Language:  "ts",
		PageTitle: j.Title,
	}
	return plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     firstGotoPath(j),
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/coverage-" + stem + ".spec.ts",
		JourneyKind: "coverage",
		Journey:     &j,
	}
}

// appendNegativeJourneys asks the LLM to shadow every form-submit
// journey in `items` with a negative-path variant (empty submit,
// invalid email, etc.). New items are appended tagged Kind="negative"
// so the emitted spec title reads `@journey:negative`. No-op when the
// LLM is disabled or no form-submit positive is present.
//
// Must run AFTER composeOpListJourneys + appendSuiteJourneys — the
// pass reads the composed Journey fields on happyflow items, so
// nothing to shadow until those are populated.
func appendNegativeJourneys(ctx context.Context, cfg config.Config, items []plan.Item) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() {
		return items
	}
	positives := collectComposedJourneys(items)
	if len(positives) == 0 {
		return items
	}
	extras, err := oplist.ComposeNegatives(ctx, client, positives)
	if err != nil {
		rlog.Warn("oplist: negatives compose failed", "err", err)
		return items
	}
	for i := range extras {
		j := extras[i]
		items = append(items, negativeJourneyItem(j))
	}
	if len(extras) > 0 {
		rlog.Info("oplist: negatives compose done", "added", len(extras))
	}
	return items
}

// collectComposedJourneys returns the positive Journeys already
// attached to happyflow items — both per-page composed and suite
// additions. Excludes fallback journeys (Kind unset AND Journey nil)
// since those never touch a form.
func collectComposedJourneys(items []plan.Item) []plan.Journey {
	var out []plan.Journey
	for _, it := range items {
		if it.Template != plan.TmplPlaywrightHappyFlow || it.Journey == nil {
			continue
		}
		out = append(out, *it.Journey)
	}
	return out
}

// negativeJourneyItem wraps a negative-path Journey in a plan.Item
// whose OutPath sits alongside its positive siblings but is tagged
// distinctly. Same synthetic-symbol pattern as suite additions.
func negativeJourneyItem(j plan.Journey) plan.Item {
	stem := jsSlug(j.Title)
	sym := ast.Symbol{
		Name:      stem,
		Kind:      ast.KindComponent,
		Language:  "ts",
		PageTitle: j.Title,
	}
	return plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     firstGotoPath(j),
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/negative-" + stem + ".spec.ts",
		JourneyKind: "negative",
		Journey:     &j,
	}
}

// appendSuiteJourneys asks the LLM once per crawled origin for extra
// journeys the graph-walker missed, then synthesises a plan.Item per
// returned journey. No-op when the LLM is disabled or no maps were
// captured.
func appendSuiteJourneys(ctx context.Context, cfg config.Config, items []plan.Item, maps map[string]*mindmap.Map) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() || len(maps) == 0 {
		return items
	}
	existing := existingJourneyTitles(items)
	// Persona-rotation: one ComposeSuite call per persona per origin.
	// Each persona reshapes what the LLM finds valuable. Dedup by
	// title across rounds so overlapping proposals collapse.
	seen := map[string]bool{}
	for _, t := range existing {
		seen[strings.ToLower(t)] = true
	}
	added := 0
	for origin, m := range maps {
		if m == nil {
			continue
		}
		for _, persona := range oplist.DefaultPersonas {
			added += runSuitePersona(ctx, client, m, persona, origin, &existing, seen, &items)
		}
	}
	if added > 0 {
		rlog.Info("oplist: suite compose done", "added", added)
	}
	return items
}

// runSuitePersona iterates ComposeSuite for one persona in loop-until-
// dry mode: each round the LLM sees the growing `existing` list, so it
// must propose fresh titles or nothing. Stop when a round adds fewer
// than 2 unseen journeys or after 3 rounds (cost cap). Returns the
// number of items appended for this persona.
func runSuitePersona(
	ctx context.Context,
	client *llm.Client,
	m *mindmap.Map,
	persona, origin string,
	existing *[]string,
	seen map[string]bool,
	items *[]plan.Item,
) int {
	const maxRounds = 3
	const freshFloor = 2
	added := 0
	for round := 0; round < maxRounds; round++ {
		extras, err := oplist.ComposeSuite(ctx, client, m, *existing, persona)
		if err != nil {
			rlog.Warn("oplist: suite compose failed",
				"origin", origin, "persona", persona, "round", round, "err", err)
			return added
		}
		freshThisRound := 0
		for i := range extras {
			j := extras[i]
			key := strings.ToLower(j.Title)
			if seen[key] {
				continue
			}
			seen[key] = true
			*existing = append(*existing, j.Title)
			*items = append(*items, suiteJourneyItem(j, origin))
			freshThisRound++
			added++
		}
		if freshThisRound < freshFloor {
			return added
		}
	}
	return added
}

// existingJourneyTitles collects the composed titles the LLM already
// produced this run so the suite prompt can ask for ADDITIONAL ones.
func existingJourneyTitles(items []plan.Item) []string {
	var titles []string
	for _, it := range items {
		if it.Template == plan.TmplPlaywrightHappyFlow && it.Journey != nil {
			titles = append(titles, it.Journey.Title)
		}
	}
	return titles
}

// suiteJourneyItem wraps a suite-composed Journey in a plan.Item that
// the gen layer's op-list renderer can pick up. A synthetic Symbol
// carries the title so the fallback renderer still works if Journey
// ever becomes nil downstream.
func suiteJourneyItem(j plan.Journey, origin string) plan.Item {
	stem := jsSlug(j.Title)
	sym := ast.Symbol{
		Name:      stem,
		Kind:      ast.KindComponent,
		File:      origin,
		Language:  "ts",
		PageTitle: j.Title,
	}
	return plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     firstGotoPath(j),
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/suite-" + stem + ".spec.ts",
		JourneyKind: "suite",
		Journey:     &j,
	}
}

func firstGotoPath(j plan.Journey) string {
	for _, s := range j.Steps {
		if s.Op == "goto" {
			return s.Path
		}
	}
	return "/"
}

// jsSlug turns a title into a filesystem-safe filename stem: lower,
// non-alnum → dash, collapse dashes.
func jsSlug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "suite"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}
