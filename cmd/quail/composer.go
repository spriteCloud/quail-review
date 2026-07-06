package main

import (
	"context"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
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
	} else if it.Symbol.PageTitle != "" {
		in.Title = it.Symbol.PageTitle
		in.Hints = symbolHints(it.Symbol)
	}
	return in
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
		label := firstNonEmpty(in.LabelText, in.Aria, in.Placeholder, in.Name)
		if label != "" {
			hints = append(hints, "input: "+label)
		}
		if len(hints) >= 8 {
			break
		}
	}
	for _, l := range s.Links {
		if l.Aria != "" {
			hints = append(hints, "link: "+l.Aria)
		}
		if len(hints) >= 12 {
			break
		}
	}
	return hints
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
