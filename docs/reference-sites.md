# Reference sites for `verify-references.yml`

The `verify-references` workflow probes a fixed matrix of public sites to
measure reviewqa's fitness across diverse shapes (SaaS docs, OSS marketing,
e-commerce, non-English content). The output is a Markdown fitness report
in each job's summary tab — not a gating CI check.

This document records *why* each site was chosen and the legal basis for
probing it without explicit permission. If you add or replace a site in
the matrix, update this list.

## The matrix

| Slug                       | URL                                           | Shape it exercises          |
|----------------------------|-----------------------------------------------|-----------------------------|
| `playwright-dev`           | `https://playwright.dev`                      | SaaS-shaped docs, JS-heavy  |
| `gohugo-io`                | `https://gohugo.io`                           | OSS marketing + content     |
| `books-toscrape-com`       | `https://books.toscrape.com`                  | E-commerce list-and-detail  |
| `es-wikipedia-org-madrid`  | `https://es.wikipedia.org/wiki/Madrid`        | Non-English content page    |

## Legal basis per site

### `playwright.dev`
Microsoft-owned, Apache-2.0-licensed project docs site. Public docs site
with no robots.txt disallow on `/`. A single probe (≤20 GETs) is standard
indexing behaviour; the same content is regularly crawled by search
engines and AI assistants.

### `gohugo.io`
The Hugo project's own marketing/docs site. Apache-2.0 licensed content,
explicit OSS project intended for public reference. robots.txt permits
generic crawlers.

### `books.toscrape.com`
Built by Scrapinghub *specifically* for scraper and crawler training.
The entire site exists so developers can practise crawling without
hitting real-world ToS concerns. There is no scenario in which probing
this site is misuse — it is what the site is for.

### `es.wikipedia.org/wiki/Madrid`
Wikimedia content under CC-BY-SA-4.0. robots.txt at
`https://es.wikipedia.org/robots.txt` permits general crawlers within
reasonable rate limits. One probe run issues ≤20 GETs total, which is
orders of magnitude below the rate the Wikimedia Foundation publishes as
its tolerance threshold for human-rate crawling. Chosen because Spanish
exercises our English-only nav vocabulary fallback.

## When to swap a site out

- Site's robots.txt updates to disallow `/` — the workflow logs a warning;
  rotate to a different site of the same shape.
- Site becomes JS-only-rendered (no useful HTML at fetch time) and
  consistently produces 0 specs — the report becomes uninformative; swap.
- Maintainer of an OSS reference site asks us to stop — comply
  immediately and replace with another OSS project of similar shape.

## Known gaps surfaced by this workflow

### New in v0.12.0

- **In-page component interactions are now tested.** A new `exercise`
  journey kind emits one focused spec per interactive component
  detected in the raw HTML. Seven shapes covered: search inputs,
  native `<details>`, `aria-expanded`+`aria-controls` collapsibles,
  `<dialog>`, `role=tab`, `<input type=date|time|datetime-local>`,
  Bootstrap `data-(bs-)toggle` triggers, and `aria-haspopup` buttons.
  Per-page cap at 5 emissions so busy pages don't yield 30-line specs.
  Native-HTML5 emissions (search, details, date, dialog-attached) pass
  on the deterministic path; JS-required emissions (collapse, modal,
  popup) need real client-side wiring (Bootstrap, headlessUI, etc) and
  pass on sites that have it.

### Fixed in v0.11.x

- **Relative URLs without leading slash.** `books.toscrape.com` went
  from 0 specs to 3. `ExtractHTMLLinks` now resolves hrefs against the
  page's base URL before storing them.
- **Article-shaped pages with hundreds of links.** Wikipedia articles
  were rejected by the old `< 20 outbound links` cap in `isDetailPage`.
  New article-shape branch accepts `>= 1 h1 && >= 3 h2`. Madrid now
  produces a `read` journey.
- **Special-character filenames.** Wiki paths like `Especial:PáginasEspeciales`
  produced spec filenames with `:` and accented characters that broke
  Playwright on disk. `pathSlug` now strips to a safe charset.
- **Sitemap-discovered URLs hard-failed.** The case-study links that
  came from `sitemap.xml` weren't actually present on the homepage, so
  every `research-*` spec timed out clicking a phantom link. Itinerary
  builder now detects this and falls back to `page.goto(<absolute URL>)`.
- **HTML entities leaked into regex assertions.** `Let&#x27;s Chat` was
  embedded verbatim into the spec's `getByRole('heading', { name: /.../i })`
  call, never matching. Content extractors now `html.UnescapeString` at
  capture time.
- **Speculative outbound click on chained journeys.** Every multi-step
  spec ended with a fragile "click one more nav target" step that often
  hit dead links. Removed for chained journeys; single-step nav items
  keep it.
- **Hover-only dropdown links.** Webflow / similar sites hide nav items
  inside dropdowns. The DOM has the link but it isn't immediately
  clickable. Template now wraps clicks in `try/catch` and falls back to
  `page.goto(<absolute URL>)` when the click times out.

### Still open (LLM / DGX territory)

- **Docs-shaped sites yield only `read` journeys.** `playwright.dev` and
  `gohugo.io` produce 3 read specs each. The ranked-nav vocabulary
  doesn't score doc-page text (`Get started`, `API`, `Guides`) high
  enough relative to the SaaS vocabulary. LLM page classification will
  cover this generically.
- **Multilingual nav vocabulary.** `es.wikipedia.org` produces only 1
  spec because our English vocab scores Spanish nav text at 0. Same fix
  shape as above.

### Still open (deterministic, future v0.11.x)

- **JS form validation keeps Submit disabled.** Webflow forms disable
  the submit button until JS validates input. Our `.fill()` doesn't
  fire the same blur/change events a real user would. `convert` spec
  on spritecloud.com fails for this reason. Likely fix: use
  `pressSequentially()` for text inputs OR explicitly trigger `blur()`
  after fill OR wait for the button to be enabled before clicking.

## What this workflow is NOT

- It is **not** an attack surface or load test. The probe issues
  small-N (≤20) sequential GETs with a polite User-Agent
  (`reviewqa-probe/1 (+https://github.com/spriteCloud/reviewqa)`) and
  respects same-origin / no-private-IP guards.
- It is **not** continuous — `workflow_dispatch` only. We trigger it
  before notable releases or when changing heuristics, not on every push.
- It is **not** gating — fitness scores never block CI. They inform the
  next iteration of heuristics or LLM-assisted classification.
