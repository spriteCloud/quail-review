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

	"golang.org/x/net/publicsuffix"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/log"
	"github.com/reviewqa/reviewqa/internal/mindmap"
	"github.com/reviewqa/reviewqa/internal/openapi"
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
	// v0.52 — broadened Accept so OpenAPI/Swagger JSON endpoints
	// don't 406 on us (petstore3.swagger.io did). We still prefer
	// HTML for the crawl path; JSON/XML come second.
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,application/yaml;q=0.8,*/*;q=0.5")
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
	initialBase := registrableDomain(initialHost)
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			hops++
			if hops > 3 {
				return errors.New("too many redirects")
			}
			// Allow redirects within the same registrable domain (eTLD+1)
			// so `www.example.com ↔ example.com`, `https://example.com →
			// https://www.example.com`, etc. all follow correctly. SSRF
			// safety still enforced by guardHost on the target.
			if !sameRegistrableDomain(initialBase, req.URL.Host) {
				return fmt.Errorf("cross-org redirect blocked: %s → %s", initialHost, req.URL.Host)
			}
			if err := guardHost(req.URL.Hostname()); err != nil {
				return err
			}
			return nil
		},
	}
}

// registrableDomain extracts the eTLD+1 from a host (e.g. "www.example.com"
// → "example.com", "blog.example.co.uk" → "example.co.uk"). Returns the
// raw host on any failure so the same-host special case still works.
func registrableDomain(host string) string {
	// Strip optional port.
	if i := strings.IndexByte(host, ':'); i != -1 {
		host = host[:i]
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}

// sameRegistrableDomain reports whether the host belongs to the same
// registrable domain as the (already-parsed) base. Case-insensitive.
func sameRegistrableDomain(base, host string) bool {
	if base == "" {
		return false
	}
	return strings.EqualFold(registrableDomain(host), base)
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

// hostToName turns "www.spritecloud.com" into "Spritecloud" — strips
// the leading "www." subdomain and the public suffix (".com", ".co.uk",
// ".github.io", …) so the generated symbol reads as the brand only.
// The stripping rules live in BrandFromHost; this function only owns
// the camel-case formatting on top.
func hostToName(host string) string {
	parts := strings.FieldsFunc(BrandFromHost(host), func(r rune) bool { return r == '.' || r == '-' })
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
// JourneyFilter is the contract a prompt-derived filter satisfies. The
// concrete implementation lives in internal/prompt to keep that package
// free of probe-side dependencies; this interface lets RunAllWithFilter
// accept any filter without an import cycle.
type JourneyFilter interface {
	Apply([]mindmap.Journey) []mindmap.Journey
	IsEmpty() bool
}

// RunAllWithFilter is the focused variant of RunAll: it crawls each URL
// then applies the given filter to the discovered journeys before
// generating items. If the filter is nil or empty, behaviour is
// identical to RunAll. When the filter drops every journey it logs a
// warning and falls back to the unfiltered set — better to ship
// something than nothing.
func RunAllWithFilter(ctx context.Context, urls []string, filter JourneyFilter) ([]plan.Item, []error) {
	return runAllImpl(ctx, urls, filter, CoverageStandard)
}

func RunAll(ctx context.Context, urls []string) ([]plan.Item, []error) {
	return runAllImpl(ctx, urls, nil, CoverageStandard)
}

// RunAllWithCoverage is the coverage-mode variant of RunAll. Lets the
// caller dial breadth-vs-depth without touching the filter machinery.
// nil filter means unfiltered.
func RunAllWithCoverage(ctx context.Context, urls []string, filter JourneyFilter, c CoverageMode) ([]plan.Item, []error) {
	return runAllImpl(ctx, urls, filter, c)
}

// CoverageMode dials the breadth-vs-depth tradeoff at probe time. The
// three modes compose three knobs in lockstep:
//
//   - mindmap.Options.MaxPages + MaxDepth (how many pages to crawl)
//   - IdentifyJourneys(m, N) — max journeys emitted per kind
//   - fuzzCap — max per-page fuzz specs emitted
//
// breadth = fast CI smoke; standard = current default; depth = high-
// stakes audits.
type CoverageMode string

const (
	CoverageBreadth  CoverageMode = "breadth"
	CoverageStandard CoverageMode = "standard"
	CoverageDepth    CoverageMode = "depth"
	// CoverageMax pushes the spider as wide and deep as the in-process
	// budget reasonably allows. Use when probing a real product surface
	// — a marketing site rarely needs it. Added in v0.41b.
	CoverageMax CoverageMode = "max"
)

// ParseCoverage maps a string to a CoverageMode, returning CoverageStandard
// for empty / unknown values. Case-insensitive.
func ParseCoverage(raw string) CoverageMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(CoverageBreadth):
		return CoverageBreadth
	case string(CoverageDepth):
		return CoverageDepth
	case string(CoverageMax):
		return CoverageMax
	case "", string(CoverageStandard):
		return CoverageStandard
	}
	return CoverageStandard
}

// crawlOpts returns the mindmap.Options for this coverage mode.
//
// v0.41b — bumped MaxPages on standard (20→30) and depth (50→75) to
// match the depth-closing arc's promise of probing real product
// surfaces, not just landing pages. Max mode is new.
func (c CoverageMode) crawlOpts() mindmap.Options {
	switch c {
	case CoverageBreadth:
		return mindmap.Options{MaxPages: 8, MaxDepth: 2}
	case CoverageDepth:
		return mindmap.Options{MaxPages: 75, MaxDepth: 5}
	case CoverageMax:
		return mindmap.Options{MaxPages: 120, MaxDepth: 5}
	}
	return mindmap.Options{MaxPages: 30, MaxDepth: 3}
}

// JourneysPerKind returns the cap on journeys emitted per journey kind.
func (c CoverageMode) JourneysPerKind() int {
	switch c {
	case CoverageBreadth:
		return 1
	case CoverageDepth:
		return 6
	}
	return 3
}

// FuzzCap returns the cap on fuzz-spec emissions per probe.
func (c CoverageMode) FuzzCap() int {
	switch c {
	case CoverageBreadth:
		return 3
	case CoverageDepth:
		return 10
	}
	return 5
}

func runAllImpl(ctx context.Context, urls []string, filter JourneyFilter, coverage CoverageMode) ([]plan.Item, []error) {
	var items []plan.Item
	var errs []error
	fetcher := mindmapFetcher(ctx)
	useBrowser := os.Getenv("REVIEWQA_BROWSER_PROBE") == "1"
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		urlItems, urlErrs := probeOneOrigin(ctx, u, coverage, filter, fetcher, useBrowser)
		errs = append(errs, urlErrs...)
		items = append(items, urlItems...)
	}
	return items, errs
}

// probeOneOrigin is the per-URL fan-out, split out of runAllImpl to
// keep cyclomatic complexity in check. Orchestrates crawl → journey
// identification → filter → fan-out across the spec families.
func probeOneOrigin(ctx context.Context, u string, coverage CoverageMode, filter JourneyFilter, fetcher mindmap.Fetcher, useBrowser bool) ([]plan.Item, []error) {
	opts := coverage.crawlOpts()
	// v0.41b — REVIEWQA_IGNORE_ROBOTS=1 lets the operator disable
	// robots.txt Disallow honoring (eg. for an internal QA crawl of
	// their own site whose /admin/ is excluded from public indexing
	// but is in scope for test generation). Default is to honor.
	if os.Getenv("REVIEWQA_IGNORE_ROBOTS") == "1" {
		opts.IgnoreRobots = true
	}
	m, crawlErrs := crawlOriginWithFallback(ctx, u, fetcher, opts, useBrowser)
	if m == nil || len(m.Pages) == 0 {
		// v0.52 — even when the spider gets zero pages back (SPA shell
		// with no static HTML), the origin may still expose an
		// OpenAPI / GraphQL / Webhook surface that's worth contract-
		// testing. Probe those independently so an API-only target
		// like petstore3.swagger.io produces a populated suite.
		var items []plan.Item
		items = append(items, openAPIContractItems(ctx, u)...)
		items = append(items, graphQLContractItems(ctx, u)...)
		items = append(items, webhookContractItems(ctx, u)...)
		return items, crawlErrs
	}
	journeys := identifyAndFilterJourneys(m, coverage, filter, u)
	if len(journeys) == 0 {
		// v0.52 — no journeys detected (SPA, API-only origin) but the
		// crawler still has at least one page. Emit the contract +
		// quality companions against that bare mindmap so the
		// taxonomy isn't empty.
		var items []plan.Item
		items = append(items, qualityCompanions(u, m, coverage)...)
		items = append(items, openAPIContractItems(ctx, u)...)
		items = append(items, graphQLContractItems(ctx, u)...)
		items = append(items, webhookContractItems(ctx, u)...)
		return items, crawlErrs
	}
	journeyItems := promoteJourneysToFeatures(journeys, u)
	fuzzItems := emitFuzzItems(m, u, coverage.FuzzCap())
	catalogue := buildCatalogue(u, m, journeyItems, fuzzItems)
	catalogue.CoverageMode = string(coverage)
	// v0.38: journey items reference the suite-level catalogue so the
	// pw_feature.tmpl can gate stateful / cross-journey families on
	// "does this suite have an auth or convert journey?"
	for i := range journeyItems {
		journeyItems[i].Catalogue = catalogue
	}

	var items []plan.Item
	items = append(items, companionItems(u, m, catalogue)...)
	items = append(items, journeyItems...)
	items = append(items, fuzzItems...)
	items = append(items, apiSpecItems(u, m)...)
	items = append(items, qualityCompanions(u, m, coverage)...)
	items = append(items, openAPIContractItems(ctx, u)...)
	items = append(items, graphQLContractItems(ctx, u)...)
	items = append(items, webhookContractItems(ctx, u)...)
	items = append(items, domSnapshotItems(u, m)...)
	return items, crawlErrs
}

// crawlOriginWithFallback runs the browser crawl when requested and
// falls back to the static crawl on any failure. Pure plumbing — no
// item emission.
func crawlOriginWithFallback(ctx context.Context, u string, fetcher mindmap.Fetcher, opts mindmap.Options, useBrowser bool) (*mindmap.Map, []error) {
	if !useBrowser {
		return mindmap.Crawl(ctx, u, fetcher, opts)
	}
	m, errs := runBrowserCrawl(ctx, u)
	if m != nil && len(m.Pages) > 0 {
		return m, errs
	}
	for _, e := range errs {
		log.Warn("browser probe failed; falling back to static", "err", e)
	}
	return mindmap.Crawl(ctx, u, fetcher, opts)
}

// identifyAndFilterJourneys is the journey-discovery + prompt-filter
// step, lifted out of the main loop for testability.
func identifyAndFilterJourneys(m *mindmap.Map, coverage CoverageMode, filter JourneyFilter, u string) []mindmap.Journey {
	journeys := mindmap.IdentifyJourneys(m, coverage.JourneysPerKind())
	if filter == nil || filter.IsEmpty() {
		return journeys
	}
	narrowed := filter.Apply(journeys)
	if len(narrowed) == 0 {
		log.Warn("prompt filter dropped every journey; falling back to unfiltered probe", "url", u)
		return journeys
	}
	log.Info("prompt filter applied", "journeys_before", len(journeys), "journeys_after", len(narrowed))
	return narrowed
}

// promoteJourneysToFeatures wraps each mindmap.Journey in a plan.Item
// whose Template is the .feature shape. v0.21 inversion: no .spec.ts
// sibling — playwright-bdd compiles features into runnable specs.
func promoteJourneysToFeatures(journeys []mindmap.Journey, u string) []plan.Item {
	out := make([]plan.Item, 0, len(journeys))
	for _, j := range journeys {
		item := itemFromJourney(j, u)
		item.Template = plan.TmplPlaywrightFeature
		item.OutPath = featurePathFor(item.OutPath)
		out = append(out, item)
	}
	return out
}

// emitFuzzItems emits up to cap fuzz spec items, one per page that
// satisfies pageNeedsFuzz.
func emitFuzzItems(m *mindmap.Map, u string, cap int) []plan.Item {
	out := make([]plan.Item, 0, cap)
	for _, url := range m.Order {
		if len(out) >= cap {
			break
		}
		page := m.Pages[url]
		if !pageNeedsFuzz(page) {
			continue
		}
		out = append(out, fuzzItemForPage(page, u))
	}
	return out
}

// qualityCompanions emits the v0.22 quality-layer spec items — a11y,
// responsive, perf, security, health, observability, i18n — for the
// crawled mindmap. The per-page kinds (a11y/responsive/perf) are
// bounded at the same cap as fuzz; the per-origin ones (security,
// health, observability) emit exactly one. i18n only emits when the
// landing page exposes hreflang siblings.
func qualityCompanions(sourceURL string, m *mindmap.Map, coverage CoverageMode) []plan.Item {
	if m == nil || len(m.Pages) == 0 {
		return nil
	}
	parsed, _ := url.Parse(sourceURL)
	origin := sourceURL
	if parsed != nil && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	host := ""
	if parsed != nil {
		host = parsed.Hostname()
	}
	stub := ast.Symbol{
		Name:     hostToName(host),
		Kind:     ast.KindComponent,
		File:     origin,
		Language: "ts",
	}

	var out []plan.Item
	originSlug := strings.TrimPrefix(strings.ReplaceAll(host, ".", "-"), "www-")
	if originSlug == "" {
		originSlug = "origin"
	}

	// Per-origin: one of each. The page URL is the origin itself so the
	// templates can hit "/" relative to baseURL.
	//
	// v0.42 — added HTTPChains (3xx chains, 410, 429) as per-origin
	// alongside security/health/observability.
	for _, kind := range []struct {
		tmpl   plan.Template
		subdir string
	}{
		{plan.TmplPlaywrightSecurity, "security"},
		{plan.TmplPlaywrightHealth, "health"},
		{plan.TmplPlaywrightObservability, "observability"},
		{plan.TmplPlaywrightHTTPChains, "http-chains"},
		// v0.43: integration scaffold — emits a skipped placeholder
		// so the integration layer is represented in the catalogue
		// even when the consumer has no reviewqa.yml.
		{plan.TmplPlaywrightIntegrationStub, "integration"},
		// v0.49: GraphQL + Webhook always-attempt stubs — drop the
		// endpoint-discovery gate so the Contract / Non-functional
		// layers are visible in the catalogue on every probe. The
		// stubs skip gracefully when the candidate paths 404.
		{plan.TmplPlaywrightGraphQLStub, "graphql"},
		{plan.TmplPlaywrightWebhookStub, "webhook"},
		// v0.57: per-kind integration scaffolds. Each ships 3
		// `test.skip()` blocks specific to its backing resource
		// kind. Pragmatic emission policy: unconditionally per
		// origin (rather than gated by header sniffing) because
		// they're scaffolds, not assertions — false positives cost
		// one skipped file per origin and the catalogue benefit
		// of seeing all four kinds documented outweighs the noise.
		{plan.TmplPlaywrightIntegrationDBStub, "integration-db"},
		{plan.TmplPlaywrightIntegrationCacheStub, "integration-cache"},
		{plan.TmplPlaywrightIntegrationObsStub, "integration-obs"},
		{plan.TmplPlaywrightIntegrationAuthStub, "integration-auth"},
	} {
		out = append(out, plan.Item{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  origin,
			Template: kind.tmpl,
			OutPath:  "tests/e2e/" + kind.subdir + "/" + originSlug + "." + kind.subdir + ".spec.ts",
		})
	}

	// v0.59 — split the per-page loop into two passes:
	//
	//   Pass A (always emit, no cap): a11y / landmarks / keyboard.
	//     axe-core runs in ~2s per page; capping these produces
	//     avoidable blind spots on crawls that exceed the cap.
	//
	//   Pass B (capped by coverage.FuzzCap()): responsive / perf /
	//     visual / visual-states / zoom / prefs / network / storage /
	//     print / mobile / iframe / history-depth / touch /
	//     auth-expiry / race / clipboard / file-upload / date-edges /
	//     pwa / integration-* stubs. These templates have per-page
	//     costs that DO scale (multi-viewport snapshots, perf
	//     budgets, multi-device emulation).
	perPageCap := coverage.FuzzCap() // breadth 3, standard 5, depth/max 10

	// Pass A — uncapped a11y trio for every crawled page.
	for _, pURL := range m.Order {
		page := m.Pages[pURL]
		if page == nil {
			continue
		}
		stem := domSnapshotStem(page.URL)
		pageStub := ast.Symbol{
			Name:     hostToName(parseHost(page.URL)),
			Kind:     ast.KindComponent,
			File:     page.URL,
			Language: "ts",
		}
		for _, kind := range []struct {
			tmpl   plan.Template
			subdir string
			suffix string
		}{
			{plan.TmplPlaywrightA11y, "a11y", "a11y"},
			{plan.TmplPlaywrightKeyboardNav, "a11y", "keyboard"},
			{plan.TmplPlaywrightA11yLandmarks, "a11y", "landmarks"},
		} {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: kind.tmpl,
				OutPath:  "tests/e2e/" + kind.subdir + "/" + stem + "." + kind.suffix + ".spec.ts",
			})
		}
	}

	// Pass B — capped per-page companions.
	emitted := 0
	for _, pURL := range m.Order {
		if emitted >= perPageCap {
			break
		}
		page := m.Pages[pURL]
		if page == nil {
			continue
		}
		stem := domSnapshotStem(page.URL) // re-uses the slug builder
		pageStub := ast.Symbol{
			Name:     hostToName(parseHost(page.URL)),
			Kind:     ast.KindComponent,
			File:     page.URL,
			Language: "ts",
		}
		for _, kind := range []struct {
			tmpl   plan.Template
			subdir string
			suffix string // optional disambiguator when multiple kinds share a subdir
		}{
			{plan.TmplPlaywrightResponsive, "responsive", "responsive"},
			{plan.TmplPlaywrightPerf, "perf", "perf"},
			{plan.TmplPlaywrightVisual, "visual", "visual"},
			// v0.39: deeper visual axes — interaction-state baselines.
			{plan.TmplPlaywrightVisualStates, "visual", "visual-states"},
			// v0.42: edge-case families — capped because they're heavy.
			{plan.TmplPlaywrightNetworkResilience, "network", "network"},
			{plan.TmplPlaywrightStorage, "storage", "storage"},
			{plan.TmplPlaywrightZoom, "a11y", "zoom"},
			{plan.TmplPlaywrightA11yPrefs, "a11y", "prefs"},
			{plan.TmplPlaywrightPrint, "print", "print"},
			// v0.43: Mobile — capped because it's 4 devices × 2
			// orientations (v0.56) so per-page emission is heavy.
			{plan.TmplPlaywrightMobile, "mobile", "mobile"},
		} {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: kind.tmpl,
				OutPath:  "tests/e2e/" + kind.subdir + "/" + stem + "." + kind.suffix + ".spec.ts",
			})
		}
		// v0.42: gated edge templates emitted per page based on probe
		// signals — race when the page has a form, clipboard when it
		// exposes a text-like input. Avoids polluting pages that
		// can't meaningfully run the assertion.
		if page.HasForm {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightRace,
				OutPath:  "tests/e2e/race/" + stem + ".race.spec.ts",
			})
		}
		if pageHasTextInput(page) {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightClipboard,
				OutPath:  "tests/e2e/clipboard/" + stem + ".clipboard.spec.ts",
			})
		}
		// v0.44: gated edge templates — emit only when the matching
		// probe signal is present so the spec runs against real surface.
		if pageHasInputType(page, "file") {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightFileUpload,
				OutPath:  "tests/e2e/file-upload/" + stem + ".file-upload.spec.ts",
			})
		}
		if page.HasIframe {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightIframe,
				OutPath:  "tests/e2e/iframe/" + stem + ".iframe.spec.ts",
			})
		}
		if pageHasInputType(page, "date", "datetime-local") {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightDateEdges,
				OutPath:  "tests/e2e/date-edges/" + stem + ".date-edges.spec.ts",
			})
		}
		if page.HasManifestLink {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightPWA,
				OutPath:  "tests/e2e/pwa/" + stem + ".pwa.spec.ts",
			})
		}
		// History-depth: always emit per page — every page benefits
		// from the back-back-forward smoke; the test skips itself when
		// the page has no outgoing links.
		out = append(out, plan.Item{
			Symbol:   pageStub,
			Symbols:  []ast.Symbol{pageStub},
			PageURL:  page.URL,
			Template: plan.TmplPlaywrightHistoryDepth,
			OutPath:  "tests/e2e/history-depth/" + stem + ".history-depth.spec.ts",
		})
		// v0.45: touch (always-on under mobile project), dragdrop
		// (gated on [draggable] / [ondrop]), auth-expiry (always-on —
		// the test skips when no internal link exists).
		out = append(out, plan.Item{
			Symbol:   pageStub,
			Symbols:  []ast.Symbol{pageStub},
			PageURL:  page.URL,
			Template: plan.TmplPlaywrightTouch,
			OutPath:  "tests/e2e/touch/" + stem + ".touch.spec.ts",
		})
		if page.HasDraggable {
			out = append(out, plan.Item{
				Symbol:   pageStub,
				Symbols:  []ast.Symbol{pageStub},
				PageURL:  page.URL,
				Template: plan.TmplPlaywrightDragDrop,
				OutPath:  "tests/e2e/dragdrop/" + stem + ".dragdrop.spec.ts",
			})
		}
		out = append(out, plan.Item{
			Symbol:   pageStub,
			Symbols:  []ast.Symbol{pageStub},
			PageURL:  page.URL,
			Template: plan.TmplPlaywrightAuthExpiry,
			OutPath:  "tests/e2e/auth-expiry/" + stem + ".auth-expiry.spec.ts",
		})
		emitted++
	}

	// i18n: prefer a page that advertises hreflang; fall back to the
	// landing page so the spec always emits and exercises whatever
	// translation surface (or absence thereof) the site has. v0.43
	// drops the strict hreflang gate so a marketing site without
	// hreflang siblings still gets a deterministic check that <html
	// lang> is present and that no UI string is duplicated across
	// locales.
	var i18nPage *mindmap.Page
	for _, pURL := range m.Order {
		p := m.Pages[pURL]
		if p == nil {
			continue
		}
		if len(p.Meta.Hreflang) > 0 {
			i18nPage = p
			break
		}
		if i18nPage == nil {
			i18nPage = p // fallback: first crawled page
		}
	}
	if i18nPage != nil {
		i18nStub := ast.Symbol{
			Name:     hostToName(parseHost(i18nPage.URL)),
			Kind:     ast.KindComponent,
			File:     i18nPage.URL,
			Language: "ts",
			Meta:     i18nPage.Meta,
		}
		out = append(out, plan.Item{
			Symbol:   i18nStub,
			Symbols:  []ast.Symbol{i18nStub},
			PageURL:  i18nPage.URL,
			Template: plan.TmplPlaywrightI18n,
			OutPath:  "tests/e2e/i18n/" + originSlug + ".i18n.spec.ts",
		})
		// v0.45 — when the origin actually exposes ≥2 hreflang
		// siblings, emit the mid-session locale-switch spec alongside
		// the basic i18n companion.
		if len(i18nPage.Meta.Hreflang) >= 2 {
			out = append(out, plan.Item{
				Symbol:   i18nStub,
				Symbols:  []ast.Symbol{i18nStub},
				PageURL:  i18nPage.URL,
				Template: plan.TmplPlaywrightLocaleSwitch,
				OutPath:  "tests/e2e/i18n/" + originSlug + ".locale-switch.spec.ts",
			})
		}
	}

	return out
}

// openAPIContractItems looks for /openapi.json / /swagger.json /
// /api-docs.json under the origin and, if found, emits one contract
// spec per declared endpoint. Bounded at 12 endpoints to keep probes
// from exploding on huge APIs.
func openAPIContractItems(ctx context.Context, sourceURL string) []plan.Item {
	const cap = 12
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed == nil {
		return nil
	}
	origin := parsed.Scheme + "://" + parsed.Host
	// v0.52 — added common modern API root variants. Petstore3
	// exposes /api/v3/openapi.json; Spring services frequently expose
	// /v3/api-docs; many fastify/koa setups expose /docs/json.
	candidates := []string{
		"/openapi.json", "/swagger.json", "/api-docs.json",
		"/v1/openapi.json", "/v2/openapi.json", "/v3/openapi.json",
		"/api/openapi.json", "/api/v1/openapi.json", "/api/v3/openapi.json",
		"/v3/api-docs", "/docs/json", "/swagger/v1/swagger.json",
	}
	var doc []byte
	var docURL string
	for _, p := range candidates {
		res, err := Fetch(ctx, origin+p)
		if err != nil || res == nil || len(res.Body) == 0 {
			continue
		}
		// Cheap content sniff: must contain "openapi" or "swagger".
		lower := strings.ToLower(string(res.Body[:min(len(res.Body), 200)]))
		if !strings.Contains(lower, "openapi") && !strings.Contains(lower, "swagger") {
			continue
		}
		doc = res.Body
		docURL = res.URL
		break
	}
	if doc == nil {
		return nil
	}
	_, endpoints, err := openapi.Parse(doc)
	if err != nil {
		log.Warn("openapi: parse failed; skipping contract emission", "url", docURL, "err", err)
		return nil
	}
	if len(endpoints) > cap {
		log.Info("openapi: capping endpoints", "discovered", len(endpoints), "cap", cap)
		endpoints = endpoints[:cap]
	}
	host := parsed.Hostname()
	hostSlug := strings.TrimPrefix(strings.ReplaceAll(host, ".", "-"), "www-")
	var out []plan.Item
	for i, ep := range endpoints {
		// Encode statuses + method as a synthetic FormSpec so the
		// contract template can render the allowed-status list.
		inputs := make([]ast.FormInput, 0, len(ep.Statuses))
		for _, s := range ep.Statuses {
			if s == "default" {
				continue
			}
			inputs = append(inputs, ast.FormInput{Name: s})
		}
		form := &ast.FormSpec{
			Action: ep.Path,
			Method: ep.Method,
			Inputs: inputs,
		}
		endpoint := origin + ep.Path
		stub := ast.Symbol{
			Name:     hostToName(host),
			Kind:     ast.KindComponent,
			File:     endpoint,
			Language: "ts",
		}
		slug := pathSlug(ep.Path)
		if slug == "" {
			slug = fmt.Sprintf("ep-%d", i)
		}
		out = append(out, plan.Item{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  endpoint,
			Template: plan.TmplPlaywrightContract,
			OutPath:  "tests/e2e/contract/" + hostSlug + "-" + ep.Method + "-" + slug + ".contract.spec.ts",
			Form:     form,
		})
		// v0.24 fan-out: idempotency for write methods that should be
		// idempotent by HTTP spec (PUT, DELETE — and PATCH when the
		// declared 204 / 2xx signals "no body change").
		switch strings.ToLower(ep.Method) {
		case "put", "delete":
			out = append(out, plan.Item{
				Symbol:   stub,
				Symbols:  []ast.Symbol{stub},
				PageURL:  endpoint,
				Template: plan.TmplPlaywrightIdempotency,
				OutPath:  "tests/e2e/api/" + hostSlug + "-" + ep.Method + "-" + slug + ".idempotency.spec.ts",
				Form:     form,
			})
		}
		// Pagination: heuristic on the endpoint path / operation —
		// any OpenAPI list endpoint that's clearly a collection GET
		// (path ends in a plural noun) gets a pagination probe.
		if strings.ToLower(ep.Method) == "get" && looksLikeCollectionPath(ep.Path) {
			out = append(out, plan.Item{
				Symbol:   stub,
				Symbols:  []ast.Symbol{stub},
				PageURL:  endpoint,
				Template: plan.TmplPlaywrightPagination,
				OutPath:  "tests/e2e/api/" + hostSlug + "-" + ep.Method + "-" + slug + ".pagination.spec.ts",
				Form:     form,
			})
		}
	}
	// API versioning: if we see endpoints under both /v1/ and /v2/,
	// emit one versioning spec per pair (capped at 4).
	versioningItems := pairVersionedPaths(endpoints, origin, hostSlug)
	const versCap = 4
	if len(versioningItems) > versCap {
		versioningItems = versioningItems[:versCap]
	}
	out = append(out, versioningItems...)
	return out
}

// looksLikeCollectionPath returns true for OpenAPI paths that smell
// like list endpoints — `/pets`, `/users`, `/api/orders`. Conservative:
// returns false when the last segment is a path parameter (`{id}`).
func looksLikeCollectionPath(p string) bool {
	if p == "" {
		return false
	}
	last := p
	if i := strings.LastIndexByte(last, '/'); i != -1 {
		last = last[i+1:]
	}
	if last == "" || strings.HasPrefix(last, "{") {
		return false
	}
	return strings.HasSuffix(last, "s") || strings.HasSuffix(last, "es")
}

// pairVersionedPaths detects (/v1/x, /v2/x) pairs and emits one
// versioning spec per kept pair.
func pairVersionedPaths(endpoints []openapi.Endpoint, origin, hostSlug string) []plan.Item {
	type key struct{ rest, method string }
	seen := map[key]map[string]bool{}
	for _, ep := range endpoints {
		var version, rest string
		if strings.HasPrefix(ep.Path, "/v1/") {
			version, rest = "v1", ep.Path[len("/v1"):]
		} else if strings.HasPrefix(ep.Path, "/v2/") {
			version, rest = "v2", ep.Path[len("/v2"):]
		} else {
			continue
		}
		k := key{rest, ep.Method}
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		seen[k][version] = true
	}
	var out []plan.Item
	for k, versions := range seen {
		if !(versions["v1"] && versions["v2"]) {
			continue
		}
		// PageURL points at v1; the template rewrites for v2.
		endpoint := origin + "/v1" + k.rest
		stub := ast.Symbol{
			Name:     hostToName(parseHost(endpoint)),
			Kind:     ast.KindComponent,
			File:     endpoint,
			Language: "ts",
		}
		slug := pathSlug(k.rest)
		if slug == "" {
			slug = "root"
		}
		out = append(out, plan.Item{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  endpoint,
			Template: plan.TmplPlaywrightVersioning,
			OutPath:  "tests/e2e/api/" + hostSlug + "-" + k.method + "-" + slug + ".versioning.spec.ts",
			Form:     &ast.FormSpec{Method: k.method},
		})
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// apiSpecItems synthesises one plan.Item per (page, form) where the
// form carries an action attribute that resolves to a same-origin URL.
// Bounded at 8 to keep big sites manageable.
func apiSpecItems(sourceURL string, m *mindmap.Map) []plan.Item {
	const cap = 8
	var out []plan.Item
	for _, pURL := range m.Order {
		if len(out) >= cap {
			break
		}
		p := m.Pages[pURL]
		if p == nil {
			continue
		}
		for i := range p.Forms {
			if len(out) >= cap {
				break
			}
			form := p.Forms[i]
			endpoint := resolveFormAction(p.URL, form.Action)
			if endpoint == "" {
				continue
			}
			if !sameOriginAsSource(sourceURL, endpoint) {
				continue
			}
			// Reject forms with no Inputs — nothing to body-template.
			if len(form.Inputs) == 0 {
				continue
			}
			host := parseHost(endpoint)
			stub := ast.Symbol{
				Name:     hostToName(host),
				Kind:     ast.KindComponent,
				File:     endpoint,
				Language: "ts",
			}
			out = append(out, plan.Item{
				Symbol:   stub,
				Symbols:  []ast.Symbol{stub},
				PageURL:  endpoint,
				Template: plan.TmplPlaywrightAPI,
				OutPath:  "tests/e2e/api/" + apiSpecStem(endpoint, len(out)) + ".api.spec.ts",
				Form:     &form,
			})
		}
	}
	return out
}

// resolveFormAction resolves the form's action attribute against the
// page URL. Empty action means "submit to self" — returns the page URL.
// Absolute http(s) actions are returned as-is. Other schemes (mailto:,
// javascript:, tel:) return "" so the caller skips emission.
func resolveFormAction(pageURL, action string) string {
	pageU, perr := url.Parse(pageURL)
	if perr != nil {
		return ""
	}
	if action == "" {
		return pageURL
	}
	a, err := url.Parse(action)
	if err != nil {
		return ""
	}
	if a.Scheme != "" && a.Scheme != "http" && a.Scheme != "https" {
		return ""
	}
	return pageU.ResolveReference(a).String()
}

// sameOriginAsSource reports whether the endpoint sits on the same
// registrable domain as the probe's source URL. Mirrors the redirect
// guard so SSRF posture is preserved on the API spec output.
func sameOriginAsSource(sourceURL, endpoint string) bool {
	s, serr := url.Parse(sourceURL)
	e, eerr := url.Parse(endpoint)
	if serr != nil || eerr != nil {
		return false
	}
	return sameRegistrableDomain(registrableDomain(s.Hostname()), e.Hostname())
}

// apiSpecStem builds a filesystem-safe stem for an API spec. The index
// keeps multi-form pages from colliding on disk.
func apiSpecStem(endpoint string, idx int) string {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil {
		return fmt.Sprintf("api-%d", idx)
	}
	host := strings.TrimPrefix(strings.ReplaceAll(u.Hostname(), ".", "-"), "www-")
	slug := pathSlug(endpoint)
	stem := host
	if slug != "" {
		stem += "-" + slug
	}
	if idx > 0 {
		stem += fmt.Sprintf("-%d", idx)
	}
	return stem
}

// buildCatalogue aggregates the suite-level data that the catalogue +
// summary templates render against. Pulls page tags + titles from the
// mindmap, priority + steps from the journey items.
func buildCatalogue(sourceURL string, m *mindmap.Map, journeyItems, fuzzItems []plan.Item) *plan.Catalogue {
	parsed, _ := url.Parse(sourceURL)
	origin := sourceURL
	if parsed != nil && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	cat := &plan.Catalogue{Origin: origin}
	for _, pURL := range m.Order {
		p := m.Pages[pURL]
		cat.Pages = append(cat.Pages, plan.CataloguePage{
			URL:   p.URL,
			Title: p.Title,
			Tags:  append([]string(nil), p.Tags...),
		})
	}
	for _, it := range journeyItems {
		steps := make([]plan.CatalogueStep, 0, len(it.Symbols))
		for _, s := range it.Symbols {
			steps = append(steps, plan.CatalogueStep{
				URL:        s.AbsoluteURL,
				Title:      s.PageTitle,
				EnteredVia: s.EnteredVia,
			})
		}
		cat.Journeys = append(cat.Journeys, plan.CatalogueJourney{
			Kind:     it.JourneyKind,
			Priority: mindmap.JourneyPriority(mindmap.JourneyKind(it.JourneyKind)),
			OutPath:  it.OutPath,
			Steps:    steps,
		})
	}
	for _, it := range fuzzItems {
		cat.Fuzz = append(cat.Fuzz, plan.CatalogueFuzz{
			PageURL: it.PageURL,
			OutPath: it.OutPath,
		})
	}
	return cat
}

// domSnapshotItems emits one raw-content plan.Item per crawled page that
// carries non-empty DOMHTML. Outpath is tests/e2e/_dom/<slug>.html. Lets
// reviewers see what the browser actually rendered without re-running the
// probe — and gives Playwright trace-viewer something to diff against.
func domSnapshotItems(sourceURL string, m *mindmap.Map) []plan.Item {
	var out []plan.Item
	for _, pURL := range m.Order {
		p := m.Pages[pURL]
		if p == nil || p.DOMHTML == "" {
			continue
		}
		stem := domSnapshotStem(p.URL)
		stub := ast.Symbol{
			Name:     hostToName(parseHost(p.URL)),
			Kind:     ast.KindComponent,
			File:     p.URL,
			Language: "ts",
		}
		out = append(out, plan.Item{
			Symbol:     stub,
			Symbols:    []ast.Symbol{stub},
			PageURL:    p.URL,
			Template:   plan.TmplRaw,
			OutPath:    "tests/e2e/_dom/" + stem + ".html",
			RawContent: []byte(p.DOMHTML),
		})
	}
	return out
}

// domSnapshotStem builds a filesystem-safe slug for a DOM snapshot
// filename. Mirrors outPathStem/pathSlug so a snapshot's filename matches
// the spec filename for the same page.
func domSnapshotStem(pageURL string) string {
	u, err := url.Parse(pageURL)
	if err != nil || u == nil {
		return "page"
	}
	host := strings.TrimPrefix(strings.ReplaceAll(u.Hostname(), ".", "-"), "www-")
	slug := pathSlug(pageURL)
	if slug == "" {
		return host
	}
	return host + "-" + slug
}

func parseHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return ""
	}
	return u.Hostname()
}

// companionItems returns the suite-wide files reviewqa drops alongside the
// per-journey specs: tests/e2e/_fixtures.ts, playwright.config.ts,
// tests/e2e/README.md, the Steps API helper, and (when a catalogue is
// supplied) the stakeholder-facing test catalogue + work-summary deck.
// The PageURL on each item is the probed origin so the templates can
// render baseURL defaults and a relevant README intro.
func companionItems(sourceURL string, m *mindmap.Map, cat *plan.Catalogue) []plan.Item {
	parsed, _ := url.Parse(sourceURL)
	host := ""
	if parsed != nil {
		host = parsed.Hostname()
	}
	stub := ast.Symbol{
		Name:     hostToName(host),
		Kind:     ast.KindComponent,
		File:     sourceURL,
		Language: "ts",
	}
	originOnly := sourceURL
	if parsed != nil {
		originOnly = parsed.Scheme + "://" + parsed.Host
	}
	items := []plan.Item{
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightFixtures,
			OutPath:  "tests/e2e/_fixtures.ts",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightConfig,
			OutPath:  "playwright.config.ts",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightReadme,
			OutPath:  "tests/e2e/README.md",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightPackage,
			OutPath:  "package.json",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightTsconfig,
			OutPath:  "tsconfig.json",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightCIFile,
			OutPath:  ".github/workflows/e2e.yml",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightSteps,
			OutPath:  "tests/e2e/lib/steps.ts",
		},
		{
			Symbol:   stub,
			Symbols:  []ast.Symbol{stub},
			PageURL:  originOnly,
			Template: plan.TmplPlaywrightStepsBDD,
			OutPath:  "tests/e2e/steps/reviewqa.steps.ts",
		},
		{
			Symbol:        stub,
			Symbols:       []ast.Symbol{stub},
			PageURL:       originOnly,
			Template:      plan.TmplPlaywrightFindings,
			OutPath:       "tests/e2e/docs/findings.md",
			IfMissingOnly: true,
		},
	}
	if cat != nil {
		items = append(items,
			plan.Item{
				Symbol:    stub,
				Symbols:   []ast.Symbol{stub},
				PageURL:   originOnly,
				Template:  plan.TmplPlaywrightCatalogue,
				OutPath:   "tests/e2e/docs/test-catalogue.md",
				Catalogue: cat,
			},
			plan.Item{
				Symbol:    stub,
				Symbols:   []ast.Symbol{stub},
				PageURL:   originOnly,
				Template:  plan.TmplPlaywrightSummary,
				OutPath:   "tests/e2e/docs/summary.html",
				Catalogue: cat,
			},
		)
	}
	return items
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
	for idx, step := range j.Steps {
		s := symbolFromPage(step.Page)
		s.EnteredVia = step.EnteredVia
		s.AbsoluteURL = step.Page.URL
		// If the EnteredVia path isn't a clickable link on the previous
		// step's page (commonly true for sitemap-discovered URLs), mark the
		// step as DirectGoto so the template uses page.goto(absURL) instead
		// of locator(href).click(). Caught by the v0.11 smoke run: case-study
		// URLs surfaced via sitemap but not linked from the landing.
		if idx > 0 && step.EnteredVia != "" && !linkExistsOnPage(j.Steps[idx-1].Page, step.EnteredVia) {
			s.DirectGoto = true
		}
		// The landing step in non-form-goal journeys should NOT inherit
		// the homepage's form. Otherwise every browse/research/explore
		// spec submits the email signup before navigating — both noisy
		// (real ESP submissions) and incorrect (the post-submit redirect
		// breaks the next step). Form-goal kinds (convert, contact, auth)
		// keep the form intact because exercising it IS their purpose.
		if idx == 0 && !mindmap.JourneyExercisesForm(j.Kind) {
			s = withoutForm(s)
		}
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

// linkExistsOnPage reports whether href is present as an outbound link on
// the given page. Used by itemFromJourney to decide between click and
// page.goto in chained-step emission.
func linkExistsOnPage(p *mindmap.Page, href string) bool {
	if p == nil || href == "" {
		return false
	}
	for _, l := range p.Links {
		if l.Aria == href {
			return true
		}
	}
	return false
}

// withoutForm zeroes out form-related fields on a symbol. Used to keep
// the landing-page visit assertions (title, h1) but suppress the
// fill-and-submit emission for journeys whose goal isn't conversion.
func withoutForm(s ast.Symbol) ast.Symbol {
	s.HasForm = false
	s.Inputs = nil
	kept := s.Anchors[:0]
	for _, a := range s.Anchors {
		if a.Tag == "submit" {
			continue
		}
		kept = append(kept, a)
	}
	s.Anchors = kept
	return s
}

func symbolFromPage(p *mindmap.Page) ast.Symbol {
	u, _ := url.Parse(p.URL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	return ast.Symbol{
		Name:         hostToName(host),
		Kind:         ast.KindComponent,
		File:         p.URL,
		Language:     "ts",
		Anchors:      p.Anchors,
		Inputs:       p.Inputs,
		Links:        p.Links,
		Contents:     p.Contents,
		Interactions: p.Interactions,
		Images:       p.Images,
		Meta:         p.Meta,
		PageTitle:    p.Title,
		HasForm:      p.HasForm,
	}
}

// featurePathFor rewrites a `tests/e2e/<x>.spec.ts` path to its sibling
// Gherkin documentation path `tests/e2e/features/<x>.feature`.
func featurePathFor(specPath string) string {
	base := strings.TrimSuffix(filepath.Base(specPath), ".spec.ts")
	return "tests/e2e/features/" + base + ".feature"
}

// pageNeedsFuzz reports whether a page has enough surface to make a fuzz
// spec worthwhile — at least one text-like input or one interactive
// component the keyboard branch can exercise.
func pageNeedsFuzz(p *mindmap.Page) bool {
	if p == nil {
		return false
	}
	if pageHasTextInput(p) {
		return true
	}
	for _, ix := range p.Interactions {
		switch ix.Kind {
		case "details", "collapse", "tab", "popup":
			return true
		}
	}
	return false
}

// pageHasTextInput reports whether the page exposes at least one
// text-shaped input the v0.42 clipboard / paste edge spec can target.
// Centralised so other edge templates can share the same gate.
func pageHasTextInput(p *mindmap.Page) bool {
	if p == nil {
		return false
	}
	for _, i := range p.Inputs {
		switch i.Type {
		case "text", "email", "search", "url", "tel", "textarea", "":
			return true
		}
	}
	return false
}

// pageHasInputType reports whether the page exposes at least one
// <input> with one of the given type attributes. Drives v0.44 gated
// edge templates (file-upload, date-edges).
func pageHasInputType(p *mindmap.Page, types ...string) bool {
	if p == nil {
		return false
	}
	want := make(map[string]bool, len(types))
	for _, t := range types {
		want[t] = true
	}
	for _, i := range p.Inputs {
		if want[i.Type] {
			return true
		}
	}
	return false
}

// fuzzItemForPage builds the plan.Item that drives pw_fuzz.tmpl. The
// item's Symbol carries the page's inputs + interactions (the template
// loops over them with bounded caps inside).
func fuzzItemForPage(p *mindmap.Page, sourceURL string) plan.Item {
	u, _ := url.Parse(p.URL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	sym := ast.Symbol{
		Name:         hostToName(host),
		Kind:         ast.KindComponent,
		File:         p.URL,
		Language:     "ts",
		Inputs:       p.Inputs,
		Interactions: p.Interactions,
		PageTitle:    p.Title,
	}
	hostStem := strings.TrimPrefix(strings.ReplaceAll(host, ".", "-"), "www-")
	stem := hostStem
	if slug := pathSlug(p.URL); slug != "" {
		stem += "-" + slug
	}
	return plan.Item{
		Symbol:      sym,
		Symbols:     []ast.Symbol{sym},
		PageURL:     p.URL,
		Template:    plan.TmplPlaywrightFuzz,
		OutPath:     "tests/e2e/" + stem + "-fuzz.spec.ts",
		JourneyKind: "fuzz",
	}
}

func outPathStemForJourney(j mindmap.Journey, first *mindmap.Page) string {
	u, _ := url.Parse(first.URL)
	host := ""
	if u != nil {
		host = u.Hostname()
	}
	hostStem := strings.TrimPrefix(strings.ReplaceAll(host, ".", "-"), "www-")
	stem := hostStem + "-" + string(j.Kind)
	// Disambiguate multiple journeys of the same kind by the terminal page
	// slug. Multi-step journeys naturally have a non-landing terminal;
	// exercise journeys are single-step but still need the slug because
	// multiple interactive pages can each produce one. The pathSlug guard
	// against "" handles the landing-page case (slug is empty there, so
	// the stem stays clean — no dangling dash).
	useSlug := len(j.Steps) > 1 || j.Kind == mindmap.JourneyExercise
	if useSlug {
		if slug := pathSlug(j.Steps[len(j.Steps)-1].Page.URL); slug != "" {
			stem += "-" + slug
		}
	}
	return stem
}

func pathSlug(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "/", "-")
	// Strip everything that isn't a filesystem-and-Playwright-safe token.
	// Wiki article slugs frequently carry "Especial:X", percent-encoded
	// accents like "P%C3%A1ginas", and other shell-hostile chars.
	var b strings.Builder
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	// Collapse repeat dashes; trim trailing.
	collapsed := b.String()
	for strings.Contains(collapsed, "--") {
		collapsed = strings.ReplaceAll(collapsed, "--", "-")
	}
	return strings.Trim(collapsed, "-_.")
}

// buildJourney fetches the source URL, then chains up to maxChain pages by
// (legacy buildJourney + pathOf removed — RunAll now goes through the
// mindmap package which handles crawling, journey identification, and
// path resolution end-to-end.)


// (Nav-target ranking now lives in internal/mindmap. The legacy single-URL
// chained probe is fully superseded by mindmap-driven journey emission.)
