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
)

// ParseCoverage maps a string to a CoverageMode, returning CoverageStandard
// for empty / unknown values. Case-insensitive.
func ParseCoverage(raw string) CoverageMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(CoverageBreadth):
		return CoverageBreadth
	case string(CoverageDepth):
		return CoverageDepth
	case "", string(CoverageStandard):
		return CoverageStandard
	}
	return CoverageStandard
}

// crawlOpts returns the mindmap.Options for this coverage mode.
func (c CoverageMode) crawlOpts() mindmap.Options {
	switch c {
	case CoverageBreadth:
		return mindmap.Options{MaxPages: 8, MaxDepth: 2}
	case CoverageDepth:
		return mindmap.Options{MaxPages: 50, MaxDepth: 4}
	}
	return mindmap.Options{MaxPages: 20, MaxDepth: 3}
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
		var m *mindmap.Map
		var crawlErrs []error
		opts := coverage.crawlOpts()
		if useBrowser {
			m, crawlErrs = runBrowserCrawl(ctx, u)
			if m == nil || len(m.Pages) == 0 {
				// Fall back to static crawl on any failure (missing node,
				// missing playwright, browser probe error). Static still
				// produces SOMETHING for every reachable site.
				for _, e := range crawlErrs {
					log.Warn("browser probe failed; falling back to static", "err", e)
				}
				crawlErrs = nil
				m, crawlErrs = mindmap.Crawl(ctx, u, fetcher, opts)
			}
		} else {
			m, crawlErrs = mindmap.Crawl(ctx, u, fetcher, opts)
		}
		errs = append(errs, crawlErrs...)
		if m == nil || len(m.Pages) == 0 {
			continue
		}
		journeys := mindmap.IdentifyJourneys(m, coverage.JourneysPerKind())
		if filter != nil && !filter.IsEmpty() {
			narrowed := filter.Apply(journeys)
			if len(narrowed) == 0 {
				log.Warn("prompt filter dropped every journey; falling back to unfiltered probe", "url", u)
			} else {
				log.Info("prompt filter applied", "journeys_before", len(journeys), "journeys_after", len(narrowed))
				journeys = narrowed
			}
		}
		if len(journeys) == 0 {
			continue
		}
		// Companion items: a shared fixtures module, a playwright.config.ts,
		// and a tests/e2e/README.md. Emitted once per probed origin so all
		// per-journey specs in the same suite share a common test setup
		// instead of duplicating the page-error tracking in every file.
		journeyItems := make([]plan.Item, 0, len(journeys))
		for _, j := range journeys {
			item := itemFromJourney(j, u)
			journeyItems = append(journeyItems, item)
		}
		fuzzCap := coverage.FuzzCap()
		fuzzItems := make([]plan.Item, 0, fuzzCap)
		fuzzEmitted := 0
		for _, url := range m.Order {
			if fuzzEmitted >= fuzzCap {
				break
			}
			page := m.Pages[url]
			if !pageNeedsFuzz(page) {
				continue
			}
			fuzzItems = append(fuzzItems, fuzzItemForPage(page, u))
			fuzzEmitted++
		}
		// Catalogue + summary are companion items but need aggregated data
		// from the journey list, so build them AFTER the journey items.
		catalogue := buildCatalogue(u, m, journeyItems, fuzzItems)
		catalogue.CoverageMode = string(coverage)
		items = append(items, companionItems(u, m, catalogue)...)
		for _, item := range journeyItems {
			items = append(items, item)
			// Gherkin documentation sibling: same plan.Item shape, different
			// template + .feature extension. The TS spec stays the source of
			// truth; the feature file is generated from the same Symbol so
			// they can't drift.
			featureItem := item
			featureItem.Template = plan.TmplPlaywrightFeature
			featureItem.OutPath = featurePathFor(item.OutPath)
			items = append(items, featureItem)
		}
		items = append(items, fuzzItems...)
		// API-contract specs: one per (page, form) pair where the form's
		// action resolves to a same-origin URL. Bounded at 8 per probe so
		// triage stays tractable on form-heavy sites.
		items = append(items, apiSpecItems(u, m)...)
		// Browser-mode DOM snapshots: emit one tests/e2e/_dom/<slug>.html
		// per crawled page that carries captured HTML. Static crawl pages
		// don't populate DOMHTML, so this is a no-op outside browser mode.
		items = append(items, domSnapshotItems(u, m)...)
	}
	return items, errs
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
	for _, i := range p.Inputs {
		switch i.Type {
		case "text", "email", "search", "url", "tel", "textarea":
			return true
		}
	}
	for _, ix := range p.Interactions {
		switch ix.Kind {
		case "details", "collapse", "tab", "popup":
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
