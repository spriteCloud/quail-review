// Page-intent classifier wiring. Runs in parallel with the existing
// composer pass; populates Symbol.PageIntent on each Feature item
// before gen.Render branches the template on it.
//
// LLM-off: no-op. Cache-warm: ~free. Misclassified (confidence below
// the template threshold of 0.7): templates fall back to the generic
// component scenarios, so a bad classification never breaks output.

package main

import (
	"context"
	"sync"

	"github.com/spriteCloud/quail-core/ast"
	"github.com/spriteCloud/quail-core/composer"
	"github.com/spriteCloud/quail-core/config"
	"github.com/spriteCloud/quail-core/llm"
	rlog "github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-core/plan"
)

// classifyPageIntents fans the LLM page-intent classifier across every
// Feature item with a PrimaryComponent — those are the only items
// whose templates currently branch on the intent (the calculator
// specialization in v0.10.13). Pages without a PrimaryComponent skip
// the classifier call (saves ~1-2s per non-component page).
//
// Items are mutated IN PLACE: `items[i].Symbols[0].PageIntent` and
// `items[i].Symbol.PageIntent` are populated. Subsequent gen.Render
// reads from `items[i].Symbol.PageIntent`.
func classifyPageIntents(ctx context.Context, cfg config.Config, items []plan.Item) []plan.Item {
	client := llm.New(cfg)
	if !client.Enabled() {
		return items
	}
	ladder := buildLadder(cfg, client)
	if ladder.Empty() {
		return items
	}
	cache := composer.Cache{Dir: composer.ResolveCacheDir("", cfg.WorkDir)}
	rlog.Info("classifier: requesting page-intent labels",
		"primary_model", ladder.First().Model,
		"endpoint", cfg.OpenAIBaseURL)

	sem := make(chan struct{}, composerParallelism())
	var wg sync.WaitGroup
	resultsMu := sync.Mutex{}
	type result struct {
		idx    int
		intent composer.PageIntent
	}
	var results []result

	for i := range items {
		if items[i].Template != plan.TmplPlaywrightFeature {
			continue
		}
		if items[i].Symbol.PrimaryComponent == nil {
			continue
		}
		idx := i
		input := buildClassifierInput(items[idx])
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			intent, _, err := composer.ClassifyWithLadderAndCache(ctx, ladder, input, cache)
			if err != nil {
				rlog.Warn("classifier: skipped item", "url", items[idx].PageURL, "err", err)
				return
			}
			resultsMu.Lock()
			results = append(results, result{idx: idx, intent: intent})
			resultsMu.Unlock()
		}()
	}
	wg.Wait()

	for _, r := range results {
		pi := ast.PageIntent{
			Intent:        r.intent.Intent,
			Confidence:    r.intent.Confidence,
			Vertical:      r.intent.Vertical,
			KeyAssertions: r.intent.KeyAssertions,
		}
		items[r.idx].Symbol.PageIntent = pi
		if len(items[r.idx].Symbols) > 0 {
			items[r.idx].Symbols[0].PageIntent = pi
		}
		rlog.Info("classifier: page tagged",
			"url", items[r.idx].PageURL,
			"intent", pi.Intent,
			"confidence", pi.Confidence,
			"vertical", pi.Vertical)
	}
	return items
}

// buildClassifierInput projects a plan Item onto the PageInput shape
// the classifier expects. Only the landing symbol contributes — the
// classifier is per-page, not per-journey.
func buildClassifierInput(it plan.Item) composer.PageInput {
	p := composer.PageInput{URL: it.PageURL}
	if len(it.Symbols) == 0 {
		return p
	}
	first := it.Symbols[0]
	p.Title = first.PageTitle
	p.H1 = firstH1Text(first.Contents)
	if first.HasForm {
		p.FormSummary = formSummary(first)
	}
	if first.PrimaryComponent != nil {
		for _, inp := range first.PrimaryComponent.Inputs {
			label := inp.LabelText
			if label == "" {
				label = inp.Aria
			}
			if label == "" {
				label = inp.Placeholder
			}
			if label == "" {
				label = inp.Name
			}
			if label != "" {
				p.Labels = append(p.Labels, label)
			}
			if len(p.Labels) >= 12 {
				break
			}
		}
	}
	return p
}
