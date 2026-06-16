// Package probe fetches a live URL and synthesises a plan.Item carrying
// the page's anchors/inputs/links — so reviewqa can generate a Playwright
// happy-flow against the URL without any source code in the diff.
//
// The fetcher is deliberately conservative: short timeout, restricted to
// http(s), refuses redirects to private IP ranges, and limits redirects
// to at most 3 same-host hops. This keeps the action safe to run on
// hosted CI runners where SSRF would otherwise be a real concern.
package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/mindmap"
	"github.com/reviewqa/reviewqa/internal/plan"
)

const userAgent = "reviewqa-probe/1 (+https://github.com/spriteCloud/reviewqa)"

// Result is the outcome of a single Fetch.
type Result struct {
	URL  string
	Body []byte
}

// Fetch downloads the HTML at target. Returns (nil, error) on any
// safety guard failure (non-http(s) scheme, private-IP target, redirect
// off-host, etc.) or on transport / HTTP-status failure.
func Fetch(ctx context.Context, target string) (*Result, error) {
	u, err := parseAndValidate(target)
	if err != nil {
		return nil, err
	}
	if err := guardHost(u.Hostname()); err != nil {
		return nil, fmt.Errorf("probe: %w", err)
	}
	client := buildClient(u.Host)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("probe: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe: fetch %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("probe: %s returned %s", target, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB cap
	if err != nil {
		return nil, fmt.Errorf("probe: read body: %w", err)
	}
	return &Result{URL: resp.Request.URL.String(), Body: body}, nil
}

func parseAndValidate(target string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("probe: parse %q: %w", target, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("probe: scheme %q not allowed (use http or https)", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("probe: missing host")
	}
	return u, nil
}

func guardHost(host string) error {
	if host == "" {
		return errors.New("empty host")
	}
	if os.Getenv("REVIEWQA_PROBE_ALLOW_LOOPBACK") == "1" {
		return nil // test / local-dev escape hatch
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// DNS failure: let the http client surface a meaningful error.
		return nil
	}
	for _, ip := range ips {
		if isPrivate(ip) {
			return fmt.Errorf("host %q resolves to private/blocked IP %s", host, ip)
		}
	}
	return nil
}

func isPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10,
			v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31,
			v4[0] == 192 && v4[1] == 168,
			v4[0] == 169 && v4[1] == 254,
			v4[0] == 127:
			return true
		}
	}
	return false
}

func buildClient(initialHost string) *http.Client {
	hops := 0
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			hops++
			if hops > 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != initialHost {
				return fmt.Errorf("cross-host redirect blocked: %s → %s", initialHost, req.URL.Host)
			}
			if err := guardHost(req.URL.Hostname()); err != nil {
				return err
			}
			return nil
		},
	}
}

// BuildItem synthesises a plan.Item carrying the page's locator anchors,
// form inputs and links. The Item drives the pw_happyflow.tmpl template
// to produce a Playwright spec that hits the real URL.
func BuildItem(target string, html []byte) (plan.Item, error) {
	u, err := parseAndValidate(target)
	if err != nil {
		return plan.Item{}, err
	}
	anchors := ast.DedupAnchors(plan.ExtractHTMLAnchors(target, html))
	inputs := ast.DedupInputs(plan.ExtractHTMLInputs(target, html))
	links := ast.DedupLinks(plan.ExtractHTMLLinks(target, html))
	contents := plan.ExtractContentAnchors(html)
	pageTitle := plan.PageTitle(html)
	hasForm := strings.Contains(strings.ToLower(string(html)), "<form")

	name := hostToName(u.Hostname())
	stem := outPathStem(u)
	symbol := ast.Symbol{
		Name:      name,
		Kind:      ast.KindComponent,
		File:      target,
		Language:  "ts",
		Anchors:   anchors,
		Inputs:    inputs,
		Links:     links,
		Contents:  contents,
		PageTitle: pageTitle,
		HasForm:   hasForm,
	}
	return plan.Item{
		Symbol:   symbol,
		Symbols:  []ast.Symbol{symbol},
		PageURL:  target, // absolute URL — template emits it verbatim
		Template: plan.TmplPlaywrightHappyFlow,
		OutPath:  filepath.ToSlash(filepath.Join("tests", "e2e", stem+".spec.ts")),
	}, nil
}

// hostToName turns "www.spritecloud.com" into "WwwSpritecloudCom".
func hostToName(host string) string {
	parts := strings.FieldsFunc(host, func(r rune) bool { return r == '.' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(strings.ToLower(p[1:]))
		}
	}
	if b.Len() == 0 {
		return "Probe"
	}
	return b.String()
}

// outPathStem produces a slug for the output spec filename. Hostname plus
// a normalised path. "https://www.spritecloud.com/services" → "spritecloud-com-services".
func outPathStem(u *url.URL) string {
	host := strings.ReplaceAll(u.Hostname(), ".", "-")
	host = strings.TrimPrefix(host, "www-")
	pathPart := strings.Trim(u.Path, "/")
	pathPart = strings.ReplaceAll(pathPart, "/", "-")
	if pathPart == "" {
		return host
	}
	return host + "-" + pathPart
}

// RunAll crawls each source URL into a mindmap, then identifies multiple
// user journeys (convert / browse / explore / read) — one plan.Item per
// journey. A single source URL therefore yields several spec files,
// each exercising a different user goal across the site.
func RunAll(ctx context.Context, urls []string) ([]plan.Item, []error) {
	var items []plan.Item
	var errs []error
	fetcher := mindmapFetcher(ctx)
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		m, crawlErrs := mindmap.Crawl(ctx, u, fetcher, mindmap.Options{})
		errs = append(errs, crawlErrs...)
		if m == nil || len(m.Pages) == 0 {
			continue
		}
		journeys := mindmap.IdentifyJourneys(m, 2)
		for _, j := range journeys {
			items = append(items, itemFromJourney(j, u))
		}
	}
	return items, errs
}

// mindmapFetcher adapts probe.Fetch to the mindmap.Fetcher signature.
func mindmapFetcher(_ context.Context) mindmap.Fetcher {
	return func(ctx context.Context, url string) ([]byte, string, error) {
		res, err := Fetch(ctx, url)
		if err != nil {
			return nil, "", err
		}
		return res.Body, res.URL, nil
	}
}

// itemFromJourney materialises one plan.Item from a mindmap.Journey.
// Symbols carry the page chain; first symbol has empty EnteredVia
// (visited via direct goto), subsequent ones carry the path that was
// clicked to reach them.
func itemFromJourney(j mindmap.Journey, sourceURL string) plan.Item {
	if len(j.Steps) == 0 {
		return plan.Item{}
	}
	first := j.Steps[0].Page
	syms := make([]ast.Symbol, 0, len(j.Steps))
	for _, step := range j.Steps {
		s := symbolFromPage(step.Page)
		s.EnteredVia = step.EnteredVia
		syms = append(syms, s)
	}
	stem := outPathStemForJourney(j, first)
	return plan.Item{
		Symbol:      syms[0],
		Symbols:     syms,
		PageURL:     first.URL,
		Template:    plan.TmplPlaywrightHappyFlow,
		OutPath:     "tests/e2e/" + stem + ".spec.ts",
		JourneyKind: string(j.Kind),
	}
}

func symbolFromPage(p *mindmap.Page) ast.Symbol {
	u, _ := url.Parse(p.URL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	return ast.Symbol{
		Name:      hostToName(host),
		Kind:      ast.KindComponent,
		File:      p.URL,
		Language:  "ts",
		Anchors:   p.Anchors,
		Inputs:    p.Inputs,
		Links:     p.Links,
		Contents:  p.Contents,
		PageTitle: p.Title,
		HasForm:   p.HasForm,
	}
}

func outPathStemForJourney(j mindmap.Journey, first *mindmap.Page) string {
	u, _ := url.Parse(first.URL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	hostStem := strings.TrimPrefix(strings.ReplaceAll(host, ".", "-"), "www-")
	return hostStem + "-" + string(j.Kind)
}

// buildJourney fetches the source URL, then chains up to maxChain pages by
// (legacy buildJourney + pathOf removed — RunAll now goes through the
// mindmap package which handles crawling, journey identification, and
// path resolution end-to-end.)


// (Nav-target ranking now lives in internal/mindmap. The legacy single-URL
// chained probe is fully superseded by mindmap-driven journey emission.)
