package main

import (
	"context"
	"os"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-core/composer"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// buildLadder constructs the LLM model ladder. The primary rung is
// always the configured cfg.Model (the default qwen3-coder-next when
// --llm is set). Additional fallback rungs come from
// QUAIL_LLM_LADDER (comma-separated model ids) — each is tried in
// sequence on parse failure of the previous.
func buildLadder(cfg config.Config, primary *llm.Client) composer.Ladder {
	ladder := composer.Ladder{
		Rungs: []composer.Rung{{Model: cfg.Model, Client: primary}},
	}
	extra := strings.TrimSpace(os.Getenv("QUAIL_LLM_LADDER"))
	if extra == "" {
		return ladder
	}
	for _, m := range strings.Split(extra, ",") {
		m = strings.TrimSpace(m)
		if m == "" || m == cfg.Model {
			continue
		}
		// Clone the config with the alternate model; the same
		// endpoint + api key are reused.
		altCfg := cfg
		altCfg.Model = m
		ladder.Rungs = append(ladder.Rungs, composer.Rung{Model: m, Client: llm.New(altCfg)})
	}
	return ladder
}

// composeScenarios walks the post-probe item list and, when the LLM
// is enabled, asks the composer for additional Scenarios per Gherkin
// feature item. Returns the items list with `ExtraScenarios` populated
// in place. No-ops when the LLM is disabled (default).
//
// v0.34: composer now supports a model ladder. When QUAIL_LLM_LADDER
// is set (comma-separated model ids), each is tried in order and the
// first to return parseable scenarios wins. The chosen model is
// embedded as `@model:<id>` on each emitted scenario.
func composeScenarios(ctx context.Context, cfg config.Config, items []plan.Item) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() {
		return items
	}
	feedback := composer.LoadFeedback(cfg.WorkDir)
	if len(feedback.FailedTitles) > 0 {
		rlog.Info("composer: feeding ledger findings to LLM", "failed_titles", len(feedback.FailedTitles))
	}
	ladder := buildLadder(cfg, client)
	cache := composer.Cache{Dir: composer.ResolveCacheDir("", cfg.WorkDir)}
	if cache.Dir != "" {
		rlog.Info("composer: cache active", "dir", cache.Dir)
	}
	rlog.Info("composer: requesting LLM scenarios",
		"primary_model", ladder.First().Model,
		"endpoint", cfg.OpenAIBaseURL,
		"ladder_rungs", len(ladder.Rungs))
	// v0.31 cross-journey dedup. Track every step-sequence we've
	// accepted across the whole suite — duplicate scenarios from
	// different journeys (e.g. "no error and no success" repeated
	// across three journeys in the spritecloud.com run) get dropped
	// after the first.
	seenKeys := map[string]bool{}
	for i := range items {
		if items[i].Template != plan.TmplPlaywrightFeature {
			continue
		}
		j := buildJourneyForComposer(items[i])
		// v0.41c bumped the per-journey scenario count from 3→5;
		// v0.46 makes it adaptive — long multi-step journeys with
		// many destination pages produce prompts whose response
		// won't fit in a 2k output budget (observed against the DGX
		// when probing spritecloud.com at --coverage=depth). Falls
		// back to 3 when the journey has 4+ destination pages.
		n := 5
		if len(j.Pages) >= 4 {
			n = 3
		}
		extras, winningModel, err := composer.ProposeWithLadderAndCache(ctx, ladder, j, n, feedback, cache)
		if err != nil {
			rlog.Warn("composer: skipped journey", "kind", j.Kind, "err", err)
			continue
		}
		extras = composer.Dedup(extras)
		fresh := extras[:0]
		for _, s := range extras {
			k := composer.ScenarioKey(s)
			if seenKeys[k] {
				continue
			}
			seenKeys[k] = true
			fresh = append(fresh, s)
		}
		if len(fresh) == 0 {
			continue
		}
		items[i].ExtraScenarios = toExtraScenarios(fresh)
		items[i].LLMModel = winningModel
		rlog.Info("composer: added scenarios", "journey", j.Kind, "count", len(fresh), "model", winningModel)
	}
	return items
}

func buildJourneyForComposer(it plan.Item) composer.Journey {
	j := composer.Journey{
		URL:      it.PageURL,
		Kind:     it.JourneyKind,
		Priority: priorityForKind(it.JourneyKind),
	}
	if len(it.Symbols) == 0 {
		return j
	}
	first := it.Symbols[0]
	j.Title = first.PageTitle
	j.H1 = firstH1Text(first.Contents)
	j.HasForm = first.HasForm
	for _, l := range first.Links {
		if l.Aria != "" {
			j.Links = append(j.Links, l.Aria)
		}
		if len(j.Links) >= 10 {
			break
		}
	}
	if first.HasForm {
		j.Forms = append(j.Forms, formSummary(first))
	}
	// v0.41a — fan every subsequent journey step into composer.PageContext
	// so the LLM reasons about the actual destination pages (titles, h1,
	// form presence) rather than re-asserting the landing on every step.
	// Symbols[0] is the landing, already encoded above.
	for _, sym := range it.Symbols[1:] {
		ctx := composer.PageContext{
			Href:  hrefForSymbol(sym),
			Title: sym.PageTitle,
			H1:    firstH1Text(sym.Contents),
		}
		if sym.HasForm {
			j.Forms = append(j.Forms, formSummary(sym))
		}
		j.Pages = append(j.Pages, ctx)
	}
	return j
}

// hrefForSymbol prefers the relative href the journey followed
// (EnteredVia) when present so the composer can refer to the link by
// the same string a `When I click the link to "<href>"` step would use.
// Falls back to AbsoluteURL when the step was reached via direct goto
// (sitemap-discovered URLs, deep-links).
func hrefForSymbol(s ast.Symbol) string {
	if strings.TrimSpace(s.EnteredVia) != "" {
		return s.EnteredVia
	}
	return s.AbsoluteURL
}

// formSummary renders a one-line human-readable form description from
// the symbol's input list. Falls back to the v0.31 placeholder when no
// per-input detail is available.
func formSummary(s ast.Symbol) string {
	if len(s.Inputs) == 0 {
		return "form with inputs"
	}
	names := make([]string, 0, len(s.Inputs))
	for _, in := range s.Inputs {
		label := strings.TrimSpace(in.LabelText)
		if label == "" {
			label = strings.TrimSpace(in.Aria)
		}
		if label == "" {
			label = strings.TrimSpace(in.Placeholder)
		}
		if label == "" {
			label = strings.TrimSpace(in.Name)
		}
		if label == "" {
			label = in.Type
		}
		if label == "" {
			continue
		}
		typed := label
		if in.Type != "" && in.Type != "text" {
			typed = label + " (" + in.Type + ")"
		}
		names = append(names, typed)
		if len(names) >= 6 {
			break
		}
	}
	if len(names) == 0 {
		return "form with inputs"
	}
	return "form fields: " + strings.Join(names, ", ")
}

func toExtraScenarios(in []composer.ExtraScenario) []plan.ExtraScenario {
	out := make([]plan.ExtraScenario, 0, len(in))
	for _, e := range in {
		steps := make([]plan.ExtraScenarioStep, 0, len(e.Steps))
		for _, s := range e.Steps {
			steps = append(steps, plan.ExtraScenarioStep{Keyword: s.Keyword, Text: s.Text})
		}
		out = append(out, plan.ExtraScenario{Name: e.Name, Tags: e.Tags, Steps: steps})
	}
	return out
}

func priorityForKind(kind string) string {
	switch kind {
	case "convert", "contact", "authenticate":
		return "critical"
	case "explore", "read":
		return "nice-to-have"
	}
	return "standard"
}

func firstH1Text(contents []ast.ContentAnchor) string {
	for _, c := range contents {
		if c.Tag == "h1" {
			return c.Text
		}
	}
	return ""
}
