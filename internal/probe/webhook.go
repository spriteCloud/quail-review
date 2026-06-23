package probe

import (
	"context"
	"net/url"
	"strings"

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-core/log"
	"github.com/spriteCloud/quail-core/openapi"
	"github.com/spriteCloud/quail-review/internal/plan"
)

// webhookContractItems detects webhook endpoints from two sources:
//
//   1. OpenAPI paths matching `/webhooks/*`, `/api/webhooks/*`, or
//      `*/webhook` — reads /openapi.json/etc the same way the
//      OpenAPI contract emitter does.
//   2. Direct path scan against known provider patterns
//      (/webhooks/stripe, /webhooks/github, /webhooks/slack) — POST
//      with no body and expect 4xx (the rejection-of-unsigned signal
//      from a real webhook receiver).
//
// One spec per detected endpoint. Bounded at 8 endpoints per probe.
func webhookContractItems(ctx context.Context, sourceURL string, projectLabel string) []plan.Item {
	const cap = 8
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed == nil {
		return nil
	}
	origin := parsed.Scheme + "://" + parsed.Host
	host := parsed.Hostname()

	seen := map[string]bool{}
	endpoints := []string{}

	// Source 1: OpenAPI paths matching webhook patterns.
	for _, p := range []string{"/openapi.json", "/swagger.json", "/api-docs.json"} {
		res, err := Fetch(ctx, origin+p)
		if err != nil || res == nil {
			continue
		}
		_, eps, err := openapi.Parse(res.Body)
		if err != nil {
			continue
		}
		for _, e := range eps {
			if !looksLikeWebhookPath(e.Path) {
				continue
			}
			full := origin + e.Path
			if !seen[full] {
				seen[full] = true
				endpoints = append(endpoints, full)
				if len(endpoints) >= cap {
					break
				}
			}
		}
		break
	}

	// Source 2: probe well-known provider paths.
	if len(endpoints) < cap {
		for _, p := range knownProviderPaths {
			if len(endpoints) >= cap {
				break
			}
			full := origin + p
			if seen[full] {
				continue
			}
			// A 404 means "no webhook here"; ANY non-404 (including
			// 401/403/405 from a webhook receiver) means there's
			// something listening.
			res, err := Fetch(ctx, full)
			if err == nil && res != nil {
				seen[full] = true
				endpoints = append(endpoints, full)
			}
		}
	}

	if len(endpoints) == 0 {
		return nil
	}
	log.Info("webhook endpoints detected", "count", len(endpoints))

	stub := ast.Symbol{
		Name:     projectName(projectLabel, host),
		Kind:     ast.KindComponent,
		File:     origin,
		Language: "ts",
	}
	// Pack endpoints into Anchors for the template — Name=path,
	// CSS=full URL.
	var anchors []ast.LocatorAnchor
	for _, e := range endpoints {
		u, _ := url.Parse(e)
		path := e
		if u != nil {
			path = u.Path
		}
		anchors = append(anchors, ast.LocatorAnchor{Name: path, CSS: e})
	}
	stub.Anchors = anchors

	return []plan.Item{{
		Symbol:   stub,
		Symbols:  []ast.Symbol{stub},
		PageURL:  origin,
		Template: plan.TmplPlaywrightWebhook,
		OutPath:  "tests/e2e/webhooks/webhook.spec.ts",
	}}
}

func looksLikeWebhookPath(p string) bool {
	lower := strings.ToLower(p)
	switch {
	case strings.Contains(lower, "/webhooks/"),
		strings.Contains(lower, "/webhook/"),
		strings.HasSuffix(lower, "/webhook"),
		strings.HasSuffix(lower, "/webhooks"):
		return true
	}
	return false
}

var knownProviderPaths = []string{
	"/webhooks",
	"/api/webhooks",
	"/webhooks/stripe",
	"/webhooks/github",
	"/webhooks/slack",
	"/webhooks/twilio",
}
