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
		items[i].Journey = &j
		composed++
		rlog.Info("oplist: composed journey", "page", in.URL, "steps", len(j.Steps))
	}
	rlog.Info("oplist: composition done", "composed", composed)
	return items
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
			if t := trim(c.Text); t != "" {
				hints = append(hints, c.Tag+": "+t)
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
		// Prefer visible text (matches Playwright's accessible name for
		// a role='link' resolution). Fall back to aria-label. Skip
		// hrefs-as-name entirely — the LLM previously copied "/work"
		// into a click op and Playwright never resolves those.
		label := firstNonEmpty(l.Text, l.Name)
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
	added := 0
	for origin, m := range maps {
		if m == nil {
			continue
		}
		extras, err := oplist.ComposeSuite(ctx, client, m, existing)
		if err != nil {
			rlog.Warn("oplist: suite compose failed", "origin", origin, "err", err)
			continue
		}
		for i := range extras {
			j := extras[i]
			items = append(items, suiteJourneyItem(j, origin))
			added++
		}
	}
	if added > 0 {
		rlog.Info("oplist: suite compose done", "added", added)
	}
	return items
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
