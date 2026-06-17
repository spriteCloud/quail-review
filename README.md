# reviewqa

[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-reviewqa-1B365D?logo=github&logoColor=white&labelColor=0F1117)](https://github.com/marketplace/actions/reviewqa)
[![Release](https://img.shields.io/github/v/release/spriteCloud/reviewqa?color=4B8BBE&labelColor=0F1117)](https://github.com/spriteCloud/reviewqa/releases)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-C0805A?labelColor=0F1117)](./LICENSE)

A small Go binary + GitHub Action that watches a PR (or a live URL), opens a
follow-up PR with generated Playwright tests + stakeholder docs, and heals
broken locators when they drift.

- **One binary**, pure Go, no CGO — works locally and in CI.
- **AST-first**: regex/heuristic extractors derive test scaffolds deterministically.
- **LLM only humanizes**: titles, step comments, PR body. Falls back to the deterministic file on any error.
- **Inference is yours**: any OpenAI-compatible base URL (ollama, vLLM, DGX-hosted vLLM, OpenAI).

## See it on real sites

Live output from the v0.23 binary against four reference sites is
committed under [`examples/`](./examples/). Each probe now produces a
full quality-axis battery — including visual regression baselines —
alongside the journey suite:

| Site | Files | .feature | a11y | responsive | perf | visual | api | fuzz |
|---|---|---|---|---|---|---|---|---|
| [`playwright.dev`](./examples/playwright-dev/) | 39 | 3 | 5 | 5 | 5 | 5 | 1 | 1 |
| [`gohugo.io`](./examples/gohugo-io/) | 42 | 3 | 5 | 5 | 5 | 5 | 0 | 5 |
| [`books.toscrape.com`](./examples/books-toscrape-com/) | 37 | 3 | 5 | 5 | 5 | 5 | 0 | 0 |
| [`es.wikipedia.org/wiki/Madrid`](./examples/es-wikipedia-org-madrid/) | 51 | 4 | 5 | 5 | 5 | 5 | 8 | 5 |

Plus, every example carries one each of `security/`, `health/`, and
`observability/` per origin. Conditional emissions — `contract/`
(OpenAPI + GraphQL via introspection), `webhooks/`, `i18n/` — fire only
when the site advertises a schema, webhook endpoint, or hreflang
siblings. `tests/grpc/` emits in diff mode when a PR touches a
`.proto` file.

## LLM-composer demo: spritecloud.com against the DGX

`examples/spritecloud-com-dgx/` is a complete suite generated with the
v0.34 binary against `https://www.spritecloud.com` with the LLM
composer enabled (qwen3-coder-next running on the local DGX). Output:

- 45 files total (9 Gherkin journeys + a11y / responsive / perf /
  security / health / observability / API contracts / fuzz / docs)
- **17 LLM-composed Scenarios across 7 of 9 journeys** appended below
  the deterministic baseline, tagged `@llm-composed @model:qwen3-coder-next-latest`
- Scenarios use the v0.32 vocabulary (`I focus`, `field has the value`,
  `the URL contains`, …) that the model couldn't reach pre-v0.32
- Zero parse failures (v0.31 dirty-JSON sanitizer + retry working)
- Composer ran ladder-mode with 1 rung; v0.34 supports
  `REVIEWQA_LLM_LADDER` to fall back to e.g. `gpt-oss:120b` on failures

See [`examples/spritecloud-com-dgx/README.md`](./examples/spritecloud-com-dgx/README.md)
for the per-journey breakdown and step-pattern examples.

## v1.0 release candidate — full release history

See [`CHANGELOG.md`](./CHANGELOG.md) for the v0.19 → v0.30 arc that closed
the deterministic 10-layer test taxonomy: Gherkin-first journeys via
playwright-bdd, API + GraphQL + webhook + gRPC contracts, visual /
a11y / responsive / perf / security / health / observability /
i18n / mobile, schema compatibility on PR diff, scheduled-job /
event-handler / email diff-mode auto-emission, Layer 5 integration
via `reviewqa.yml`, Layer 9 data quality (dbt / pandera / GE), and
an opt-in LLM scenario composer that proposes extras beyond the
deterministic baseline.

## v0.25 — LLM-driven scenario composer (opt-in, local-only)

Templates are deterministic Go text/templates by default — the same
inputs produce identical output. v0.25 adds an OPTIONAL second layer
that asks an LLM to propose additional Gherkin Scenarios beyond the
deterministic ones for each journey.

```bash
# enable the composer against a local Ollama (or any OpenAI-compatible
# endpoint). The DGX at 100.82.34.115:11434 is on Netbird and not
# reachable from public CI — set this ONLY when probing locally.
reviewqa probe --url https://x.test --llm http://100.82.34.115:11434
# or via env:
REVIEWQA_LLM=http://100.82.34.115:11434 reviewqa probe ...
# override the model (defaults to qwen3-coder-next:latest when --llm is set):
REVIEWQA_MODEL=qwen3-quail:latest reviewqa probe --url ... --llm ...
```

The composer is strictly opt-in and runs at generation time only. The
emitted `.feature` files are plain Gherkin text — they execute on any
CI without an LLM dependency. Generated scenarios are tagged
`@llm-composed` so stricter runs can exclude them:

```bash
npx playwright test --grep-invert @llm-composed
```

The model is constrained to use ONLY the Given/When/Then patterns
already registered in `reviewqa.steps.ts` — Scenarios referencing
unknown patterns are dropped before they reach the `.feature` file.

## v0.24 — diff-mode + OpenAPI-conditional fan-outs

The taxonomy is now closed on the deterministically-reachable side. Every
PR that touches code or schemas, and every probe that finds OpenAPI, fans
out into the appropriate test family. Tagged for filtering:

```bash
npx playwright test --grep @kind:api-idempotency  # repeated PUT/DELETE
npx playwright test --grep @kind:api-pagination   # collection endpoints
npx playwright test --grep @kind:api-auth         # security-declared endpoints
npx playwright test --grep @kind:api-versioning   # /v1 + /v2 pairs
npx jest --testPathPattern .compat.test.ts        # OpenAPI/.proto/AsyncAPI compat
npx jest --testPathPattern .property.test.ts      # pure-function property tests
npx jest --testPathPattern .validator.test.ts     # *Validator positive cases
npx jest --testPathPattern .cron.test.ts          # @Cron/@Scheduled smoke
npx jest --testPathPattern .event.test.ts         # queue handler smoke
npx jest --testPathPattern .email.test.ts         # mailer smoke
```

Detection runs in two modes:
- **Diff-mode** (`reviewqa generate`): scans changed TS / Python files for
  pure functions (no `await`/`fetch`/`console`/`Date.now`/`this`),
  validator-shaped names, and cron/queue/email patterns. Schema files
  (`*.proto`, OpenAPI / Swagger JSON or YAML, AsyncAPI) trigger
  backward-compat tests via `internal/compat`.
- **Probe-mode** (`reviewqa probe`): when `/openapi.json` is discovered,
  endpoints fan out into idempotency / pagination / versioning specs
  based on declared shape.

## What a `probe` run produces (v0.23)

A single probe of a live URL emits a full, runnable suite plus stakeholder
docs. Journeys ship as Gherkin `.feature` files; `playwright-bdd` compiles
them into runnable Playwright specs at config-load time via the step
definitions in `tests/e2e/steps/`.

| File | Purpose |
|---|---|
| `tests/e2e/features/*.feature` | One Gherkin file per identified user journey — `convert`, `contact`, `authenticate`, `evaluate`, `research`, `browse`, `discover`, `explore`, `read`, `exercise`. Tags above each Scenario: `@journey:<kind> @priority:<level> @smoke` (or `@negative`). |
| `tests/e2e/steps/reviewqa.steps.ts` | Step definitions binding Given/When/Then to `lib/steps.ts`. One file per suite, stable across re-probes. |
| `tests/e2e/lib/steps.ts` | Steps API helper module — `visit`, `fillForm`, `submit`, `convert`, `authenticate`, …. Step defs compose these. |
| `tests/e2e/*-fuzz.spec.ts` | Per-page negative / keyboard / oversize-input checks. Plain Playwright TS. Capped per probe. |
| `tests/e2e/api/*.api.spec.ts` | One API-contract spec per `<form action="...">` — happy + 4xx-on-missing + malformed-email + oversized + wrong-method. Plain Playwright TS. |
| `tests/e2e/a11y/*.a11y.spec.ts` | Per-page accessibility scan via `@axe-core/playwright` — asserts no serious/critical violations. Tag `@kind:a11y`. |
| `tests/e2e/responsive/*.responsive.spec.ts` | Per-page render check at mobile (375px), tablet (768px), desktop (1280px). Tag `@kind:responsive`. |
| `tests/e2e/perf/*.perf.spec.ts` | Per-page load time under SLO (default 3000ms; `PERF_SLO_MS` env). Tag `@kind:perf`. |
| `tests/e2e/security/*.security.spec.ts` | Per-origin baseline security headers (HSTS, CSP, X-Frame-Options, X-Content-Type-Options, Referrer-Policy). Tag `@kind:security`. |
| `tests/e2e/health/*.health.spec.ts` | Per-origin probe of /health, /healthz, /ready, /status, /livez — asserts at least one responds 2xx. Tag `@kind:health`. |
| `tests/e2e/observability/*.observability.spec.ts` | Per-origin check for x-request-id / server-timing / traceparent headers. Tag `@kind:observability`. |
| `tests/e2e/visual/*.visual.spec.ts` | Per-page Playwright `toHaveScreenshot()` baseline at mobile / tablet / desktop viewports. Update with `npm run test:visual-update`. Tag `@kind:visual`. |
| `tests/e2e/contract/*.contract.spec.ts` | Per-endpoint contract test when the site exposes `/openapi.json` or a `/graphql` introspection endpoint. Tag `@kind:contract` / `@kind:graphql`. |
| `tests/e2e/webhooks/*.webhook.spec.ts` | Per-webhook signed/unsigned tests when `/webhooks/*` paths are detected (OpenAPI or provider patterns). `WEBHOOK_SECRET` env enables the signed test. Tag `@kind:webhook`. |
| `tests/e2e/i18n/*.i18n.spec.ts` | Per-locale render check when `<link rel="alternate" hreflang>` siblings detected. Tag `@kind:i18n`. |
| `tests/grpc/<svc>.<rpc>.test.ts` | Per-RPC gRPC contract test when the diff touches a `*.proto` file. Streaming shape (unary/server/client/bidi) auto-detected. Tag `@kind:grpc`. |
| `tests/e2e/_fixtures.ts` | Shared `test`/`expect` with auto page-error tracking. |
| `tests/e2e/_dom/*.html` | Browser-mode DOM snapshots (when `REVIEWQA_BROWSER_PROBE=1`). |
| `tests/e2e/docs/test-catalogue.md` | Stakeholder doc: every page crawled, every journey, every priority. |
| `tests/e2e/docs/summary.html` | Branded HTML deck with priority-mix bar. |
| `tests/e2e/docs/findings.md` | Bug-discovery ledger — failed tests deduped + persisted across runs. Run `reviewqa ledger update --report playwright-report.json` to refresh. |
| `playwright.config.ts`, `package.json`, `tsconfig.json`, `.github/workflows/e2e.yml` | Complete project scaffolding — `npm install && npx playwright test` works out of the box. The config wires `playwright-bdd` and a second project for the api/fuzz specs. |

Per-site `--coverage standard` matrix (today): `https://books.toscrape.com` →
16 files; `https://www.spritecloud.com` → 30 files including 1 API spec.
`--coverage breadth` halves the journey-per-kind cap for quick CI smoke;
`--coverage depth` triples it for high-stakes audits.

Filter examples on the generated suite:

```bash
npx playwright test --grep @priority:critical   # smoke-of-smokes
npx playwright test --grep @priority:standard   # nightly
npx playwright test --grep @journey:convert     # one journey kind
npx playwright test --grep @kind:api            # API contracts only
```

## What it covers (v1)

| Language | Unit | API | E2E |
|---|---|---|---|
| TypeScript / JavaScript | Jest / Vitest | Jest + supertest | Playwright |
| Python | pytest | pytest + FastAPI TestClient / httpx | — |
| Go | `testing` | `httptest` | — |
| Java | JUnit 5 | JUnit 5 + RestAssured | — |

Locator healing is Playwright-only and runs on test failure by default
(`REVIEWQA_HEAL_MODE`: `on-failure` | `proactive` | `off`).

## Run locally

```
export GITHUB_TOKEN=...
export GITHUB_REPOSITORY=owner/repo
reviewqa scan --pr 42                                                            # dry-run
reviewqa generate --pr 42                                                        # opens follow-up PR from diff
reviewqa heal --pr 42 --report playwright-report.json
reviewqa probe --url=https://www.spritecloud.com                                 # generate from a live URL
reviewqa probe --url=https://shop.example.com --coverage depth                   # bigger / deeper crawl
reviewqa prompt "verify the checkout flow" --url=https://shop.example.com        # focused, filter by NL
reviewqa prompt "verify the contact form" --url=https://x.com --evidence         # also runs + bundles a ZIP
reviewqa ledger update --report=playwright-report.json                           # merge fresh failures into findings.md
```

The `probe` subcommand is useful when there's nothing in the diff to extract from — e.g. a fresh repo with only a README, or when the source of truth is a deployed site rather than the code. It fetches the URL, scans the HTML for `data-testid` / `<input>` / `<a href>` anchors, and emits a Playwright happy-flow.

`reviewqa generate` ALSO honours `REVIEWQA_TARGET_URLS` (comma-separated). When set, probe items are emitted alongside any diff-derived items.

### Bootstrap on any repo (probe-only mode)

Drop this file into `.github/workflows/reviewqa-probe.yml` of any repo to generate
Playwright tests against a live URL — no other code needed.

```yaml
name: reviewqa-probe
on:
  workflow_dispatch:
    inputs:
      url:
        description: URL to probe (any reachable http(s) endpoint)
        required: true
        default: https://example.com
permissions:
  contents: write
  pull-requests: write
jobs:
  probe:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: spriteCloud/reviewqa@v1
        with:
          target-urls: ${{ github.event.inputs.url }}
          run-generate: 'false'
          run-heal: 'false'
```

Trigger from the Actions tab, paste a URL, and reviewqa opens a PR with a single
spec walking the page in a linear journey: visit → click ranked nav → land → assert.

Set an OpenAI-compatible endpoint to humanize the scaffolds:

```
export OPENAI_BASE_URL=http://dgx.internal:8000/v1
export OPENAI_API_KEY=...
export REVIEWQA_MODEL=qwen2.5-coder:14b
```

If `OPENAI_API_KEY` is empty, the LLM step is skipped entirely and you get the
deterministic output.

## Use in a workflow

```yaml
name: reviewqa
on: pull_request
permissions:
  contents: write
  pull-requests: write
jobs:
  reviewqa:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: spriteCloud/reviewqa@v1
        with:
          openai-base-url: http://dgx.internal:8000/v1
          openai-api-key: ${{ secrets.OPENAI_API_KEY }}
          model: qwen2.5-coder:14b
          heal-mode: on-failure
```

## Environment

### Core (always read)

| Var | Default | Purpose |
|---|---|---|
| `GITHUB_TOKEN` / `REVIEWQA_GITHUB_TOKEN` | — | API auth |
| `GITHUB_REPOSITORY` | from event | `owner/name` |
| `REVIEWQA_PR` | from event | PR number override |
| `REVIEWQA_BRANCH_PREFIX` | `reviewqa` | created branch prefix |
| `REVIEWQA_WORKDIR` | `.` | repo working dir |
| `REVIEWQA_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

### Playwright test shape (v0.3 / v0.4 / v0.5)

| Var | Default | Purpose |
|---|---|---|
| `BASE_URL` (in the generated spec) | `http://localhost:3000` | URL the generated tests `goto()` against. Set this in your test workflow before running `npx playwright test`. |
| `REVIEWQA_E2E_STYLE` | `auto` | `auto` (page-flow when ≥2 components share a page root, else per-component), `per-component` (always one spec per component), or `page-flow` (always group, even solo components). |
| `REVIEWQA_PAGE_URLS` | — | JSON map of `{"source/path.tsx": "/route"}` for bespoke routing the conventional walker doesn't detect. Wins over framework heuristics. Invalid JSON → warning, ignored. |
| `REVIEWQA_TARGET_URLS` | — | Comma-separated list of live URLs to probe. When set, `reviewqa generate` also emits a Playwright happy-flow per URL (uses raw HTTP HTML — JavaScript-rendered SPAs need pre-rendered output). |
| `REVIEWQA_PROBE_ALLOW_LOOPBACK` | — | `1` to bypass the loopback/private-IP guard. Only set in tests or trusted dev environments. |

### Healing (Playwright locators)

| Var | Default | Purpose |
|---|---|---|
| `REVIEWQA_HEAL_MODE` | `on-failure` | `on-failure` \| `proactive` \| `off` |
| `REVIEWQA_PLAYWRIGHT_REPORT` | `playwright-report.json` | report path |

### LLM (optional)

Leaving `OPENAI_API_KEY` empty turns off the LLM entirely — you still get the full deterministic scaffold.

| Var | Default | Purpose |
|---|---|---|
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | inference endpoint |
| `OPENAI_API_KEY` | — | inference auth; empty = no LLM |
| `REVIEWQA_MODEL` | `gpt-4o-mini` | chat-completions model |
| `REVIEWQA_LLM_TIMEOUT` | `20s` | per-file LLM timeout |
| `REVIEWQA_LLM_TOKEN_CAP` | `600` | max_tokens per LLM call |
| `REVIEWQA_ALLOW_DIFF_TO_LLM` | `0` | send PR diff to LLM (off by default) |

### Page-root conventions detected out of the box

The generator detects pages for grouping in:

- **Next.js**: `pages/**/*.{tsx,jsx,ts,js}` (pages router) and `app/**/page.{tsx,jsx,ts,js}` (app router)
- **Remix**: `app/routes/**/*.{tsx,jsx,ts,js}`
- **SvelteKit**: `src/routes/**/+page.{svelte,ts,js}`
- **Nuxt / Vue**: `pages/**/*.vue`, `views/**/*.vue`
- **Astro**: `src/pages/**/*.astro`
- **Vite + React Router**: `App.{tsx,jsx}`, `routes.{tsx,jsx}`
- **Rails**: `app/views/**/*.{html.erb,erb,haml}`
- **Django / Jinja**: `templates/**/*.{html,jinja,j2}`
- **Laravel**: `resources/views/**/*.blade.php`
- **Plain HTML / Go templates**: `**/*.{html,htm,tmpl,gohtml}`

Anything else: set `REVIEWQA_PAGE_URLS`.

## AI usage rules

The LLM is allowed to do exactly one thing: rewrite **strings** inside a
deterministic test file so test titles and step comments read like a human
wrote them. We then check the rewritten file against the original for
structural equivalence (same imports, same number of `describe`/`it`/`test`
declarations); any mismatch falls back to the deterministic output. The PR
diff is **never** sent to the LLM unless `REVIEWQA_ALLOW_DIFF_TO_LLM=1`.

## License

reviewqa is dual-licensed:

- **AGPL-3.0** for the community edition — see [`LICENSE`](./LICENSE).
- **Commercial license** for organisations that cannot accept the AGPL — see
  [`COMMERCIAL.md`](./COMMERCIAL.md). Contact `hello@spritecloud.com`.

By submitting a pull request you agree to the
[Contributor License Agreement](./CLA.md). The `cla-assistant` check on PRs
records your one-time signature.
