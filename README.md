# quail-review

[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-quail--review-1B365D?logo=github&logoColor=white&labelColor=0F1117)](https://github.com/marketplace/actions/quail-review)
[![Release](https://img.shields.io/github/v/release/spriteCloud/quail-review?color=4B8BBE&labelColor=0F1117)](https://github.com/spriteCloud/quail-review/releases)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-C0805A?labelColor=0F1117)](./LICENSE)

OSS PR bot. A small Go binary + GitHub Action that watches a PR (or a live
URL), opens a follow-up PR with generated Playwright tests + stakeholder
docs, and heals broken locators when they drift.

> **Looking for the local UI / DGX tunnel / on-prem deploy?** Those live in
> the commercial [`spriteCloud/quail`](https://github.com/spriteCloud/quail)
> edition. Both editions share the same engine; the OSS one ships the
> PR/CI surface only.
>
> **Migration notice (v1.0.0):** the repository was renamed from
> `spriteCloud/quail` to `spriteCloud/quail-review`. The action input
> `uses: spriteCloud/quail@v1` continues to work via GitHub's redirect
> for 30 days; please update to `spriteCloud/quail-review@v1` at your
> earliest convenience.

- **One binary**, pure Go, no CGO — works locally and in CI.
- **Deterministic-first**: regex/AST/HTML extractors emit test scaffolds the
  same way every time. LLM-composed Scenarios are an OPT-IN second layer.
- **10-layer taxonomy** out of the box: Unit, Component, API, Contract,
  Integration, Backend, UI, Mobile, Data, Non-functional. See [docs](https://spritecloud.github.io/quail-page/docs.html).
- **Inference is yours**: any OpenAI-compatible base URL (Ollama, vLLM,
  DGX-hosted vLLM, OpenAI).

## See it on real sites

Live output from the **v0.83 binary** is committed under [`examples/`](./examples/).
Six reference probes covering a spread of site shapes; each is a complete,
runnable Playwright + Gherkin project. Re-emit on every release via
[`scripts/refresh-examples.sh`](./scripts/refresh-examples.sh).

## Tailor the generated suite locally (commercial)

The local browser UI (`quail serve` — browsing, editing, running,
chat-composing Scenarios with SSE log streaming) lives in the
**commercial** [`spriteCloud/quail`](https://github.com/spriteCloud/quail)
edition. Same engine, plus `serve` + the DGX-hosted Qwen tunnel +
on-prem / data-sovereign deploy paths. Contact hello@spritecloud.com.

| Example | Site | Files | Pages w/ a11y | Notes |
|---|---|---:|---:|---|
| [`playwright-dev/`](./examples/playwright-dev/) | <https://playwright.dev> | **183** | 30 | SaaS-shaped docs, JS-heavy |
| [`gohugo-io/`](./examples/gohugo-io/) | <https://gohugo.io> | **196** | 30 | OSS marketing + content |
| [`books-toscrape-com/`](./examples/books-toscrape-com/) | <https://books.toscrape.com> | **186** | 30 | E-commerce list-and-detail |
| [`es-wikipedia-org-madrid/`](./examples/es-wikipedia-org-madrid/) | <https://es.wikipedia.org/wiki/Madrid> | **201** | 30 | Non-English content page |
| [`spritecloud-com-dgx/`](./examples/spritecloud-com-dgx/) | <https://www.spritecloud.com> | **197** | 30 | Marketing + lead-gen, **LLM composer ON** |
| [`petstore-swagger-io-dgx/`](./examples/petstore-swagger-io-dgx/) | <https://petstore3.swagger.io> | **46** | 1 | API service exposing OpenAPI 3 — **lights up Contract layer with 12 specs × 9 tests each** |

Every example exercises the full template family: a11y trio
(uncapped) + responsive + perf + visual + visual-states + security + health +
observability + i18n + mobile (4 devices × 2 orientations) + network +
storage + zoom + prefs + print + history-depth + touch + auth-expiry +
integration stubs (api / db / cache / obs / auth) + graphql/webhook stubs.

Layer 4 (Contract) needs an OpenAPI / GraphQL / Webhook surface to populate
beyond the stubs — that's why **petstore-swagger-io-dgx** is the demo:
12 contract specs auto-derived from petstore's OpenAPI 3 doc, each with
9 tests (1 happy + 8 adversarial negatives — wrong-method, oversized-query,
invalid-json, unicode, sqli, xss, null-byte, rapid-burst). The other 5
sites have no schema surface so they get the always-attempt stubs only.

## Quick start

```bash
# Probe a live URL — generate the matrix-of-everything against the
# pages the spider finds. No source code needed.
quail probe --url https://www.spritecloud.com
quail probe --url https://shop.example.com --coverage depth

# Focus on a specific journey kind via natural-language filter.
quail prompt "verify the checkout flow" --url https://shop.example.com
quail prompt "verify the contact form rejects bad emails" --url https://x.com --evidence

# Generate from a PR diff — fan changed source code into per-aspect tests.
quail generate --pr 42
quail scan --pr 42                                   # dry-run first

# Run the generated suite once locally; --record updates the bug ledger
# so the next probe emits a sentinel spec per failure.
quail run-once --record

# Repair broken Playwright locators on test failure.
quail heal --pr 42 --report playwright-report.json

# Merge fresh Playwright failures into tests/e2e/docs/findings.md by hand.
quail ledger update --report playwright-report.json
```

## The 10-layer taxonomy

Every test quail emits maps to one of ten layers. Six of them
auto-emit on any live-URL probe; the other four trigger from PR-diff
source code.

| # | Layer | How it emits | Per-emit depth |
|---|---|---|---:|
| 1 | Unit | PR diff adds a function/method | 1+ per symbol |
| 2 | Component | PR diff touches a Kind=Component symbol | 3–5 |
| 3 | API | HTML form OR OpenAPI endpoint | **10 per form** (1 happy + 9 negatives) |
| 4 | Contract | OpenAPI/GraphQL/Webhook discovered OR always-attempt stubs | **9 per endpoint** |
| 5 | Integration | Always-on scaffold (5 stubs × 3 blocks) + real Testcontainers via `quail.yml` | 15 per origin |
| 6 | Backend | PR diff touches gRPC source | 1+ per method |
| 7 | UI | Every probed page (a11y trio is uncapped) | a11y/landmarks/keyboard 5 each per page; rest ~1–3 |
| 8 | Mobile | Every probed page (capped) | **8 per page** (4 devices × 2 orientations) |
| 9 | Data | PR diff touches dbt / pandera / Great-Expectations | 1+ per schema |
| 10 | Non-functional | Every probed page (mix of capped and uncapped) | ~17 templates, 1–3 tests each |

Full reference + recipes: <https://spritecloud.github.io/quail-page/docs.html>.

## Subcommands

| Command | Purpose | Key flags |
|---|---|---|
| `probe` | Crawl a live URL → full Playwright + Gherkin suite | `--url`, `--coverage breadth\|standard\|depth\|max`, `--llm <url>`, `--ignore-robots`, `--dry-run` |
| `prompt "<text>"` | Probe scoped to journey kinds the prompt describes | same as probe + `--evidence` |
| `generate` | Fan a PR diff into per-aspect test scaffolds; open follow-up PR | `--pr <N>` |
| `scan` | Dry-run for `generate` | `--pr <N>` |
| `heal` | Repair broken Playwright locators | `--pr <N>`, `--report <file>` |
| `ledger update` | Merge Playwright failures into `findings.md` | `--report <file>` |
| `run-once` | Run the generated suite locally, record failures | `--workdir`, `--record`, `--report`, `--grep <pat>` |

## Environment

The full set; every var is read from the environment AND most have a CLI flag.

### LLM composer (opt-in)

| Var | Default | Purpose |
|---|---|---|
| `QUAIL_LLM` | (empty) | OpenAI-compatible endpoint. When set, the composer adds up to 5 `@llm-composed` Scenarios per journey. **Strictly local-only** — never set in public CI. |
| `QUAIL_MODEL` | `gpt-4o-mini` | Model id. Auto-set to `qwen3-coder-next:latest` when `--llm` points at an Ollama-shaped endpoint. |
| `QUAIL_LLM_LADDER` | (empty) | Comma-separated model fallbacks. |
| `QUAIL_LLM_TIMEOUT` | `60s` *(since v0.48)* | Per-call timeout. Bump on slower local LLMs. |
| `QUAIL_LLM_TOKEN_CAP` | `600` | Max output tokens per LLM call. |
| `QUAIL_HUMANIZE` | (unset) | Set to `0` to skip per-file humanization while keeping composer active. |
| `QUAIL_ALLOW_DIFF_TO_LLM` | `0` | Send PR diff to LLM; off by default. |
| `QUAIL_GRAPHQL_ENDPOINT` | `/graphql` | Override stub introspection path. |
| `QUAIL_WEBHOOK_ENDPOINT` | (empty) | Webhook receiver path to activate signed-POST checks. |
| `QUAIL_WEBHOOK_SECRET` | (empty) | HMAC signing secret. |

### Probe / spider

| Var | Default | Purpose |
|---|---|---|
| `QUAIL_TARGET_URLS` | — | Comma-separated URLs to probe (alternative to `--url`). |
| `QUAIL_COVERAGE` | `standard` | `breadth` (8/2) · `standard` (30/3) · `depth` (75/5) · `max` (120/5). |
| `QUAIL_BROWSER_PROBE` | (unset) | Set to `1` to drive Chromium (Playwright) instead of static HTML crawl. Required for SPAs. |
| `QUAIL_IGNORE_ROBOTS` | (unset) | Set to `1` to crawl `robots.txt` Disallow paths. Default OFF; enable for QA of sites you own. |
| `QUAIL_PROBE_ALLOW_LOOPBACK` | (unset) | `1` to bypass loopback/private-IP guard (tests only). |

### CI / PR plumbing

| Var | Default | Purpose |
|---|---|---|
| `GITHUB_TOKEN` / `QUAIL_GITHUB_TOKEN` | — | API auth |
| `GITHUB_REPOSITORY` | from event | `owner/name` |
| `QUAIL_PR` | from event | PR number override |
| `QUAIL_BRANCH_PREFIX` | `quail` | Branch prefix for generated PRs |
| `QUAIL_WORKDIR` | `.` | Repo working dir |
| `QUAIL_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

### Healing + framework conventions

| Var | Default | Purpose |
|---|---|---|
| `QUAIL_HEAL_MODE` | `on-failure` | `on-failure` \| `proactive` \| `off` |
| `QUAIL_PLAYWRIGHT_REPORT` | `playwright-report.json` | Report path |
| `QUAIL_E2E_STYLE` | `auto` | `auto` · `per-component` · `page-flow` |
| `QUAIL_PAGE_URLS` | — | JSON map of `{"source/path.tsx": "/route"}` for bespoke routing |

## Use in a workflow

```yaml
name: quail
on: pull_request
permissions:
  contents: write
  pull-requests: write
jobs:
  quail:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: spriteCloud/quail-review@v1
        with:
          # Optional — leave empty to skip the LLM humanizer entirely.
          openai-base-url: ${{ vars.QUAIL_LLM_URL }}
          openai-api-key:  ${{ secrets.OPENAI_API_KEY }}
          model: qwen3-coder-next:latest
          heal-mode: on-failure
```

### Probe-only mode (no diff needed)

Drop this into `.github/workflows/quail-probe.yml` of any repo to generate
Playwright tests against a live URL — no source code required:

```yaml
name: quail-probe
on:
  workflow_dispatch:
    inputs:
      url:
        description: URL to probe
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
      - uses: spriteCloud/quail-review@v1
        with:
          target-urls: ${{ github.event.inputs.url }}
          run-generate: 'false'
          run-heal: 'false'
```

## AI usage rules

The LLM is allowed to do exactly two things:

1. **Humanize**: rewrite strings inside a deterministic test file so titles
   and step comments read like a human wrote them. The rewritten file is
   structure-checked against the original (same imports, same number of
   `describe`/`it`/`test`) and falls back to the deterministic output on
   any mismatch.
2. **Compose** (opt-in via `QUAIL_LLM`): propose up to 5 additional
   Gherkin Scenarios per journey, drawn ONLY from a registered step-pattern
   vocabulary. Invalid scenarios are dropped before the template renders.

The PR diff is **never** sent to the LLM unless `QUAIL_ALLOW_DIFF_TO_LLM=1`.

## License

quail is dual-licensed:

- **AGPL-3.0** for the community edition — see [`LICENSE`](./LICENSE).
- **Commercial license** for organisations that cannot accept the AGPL — see
  [`COMMERCIAL.md`](./COMMERCIAL.md). Contact `hello@spritecloud.com`.

By submitting a pull request you agree to the
[Contributor License Agreement](./CLA.md). The `cla-assistant` check on PRs
will prompt you to sign if you haven't.

For the release-by-release history (v0.19 → v0.75), see [`CHANGELOG.md`](./CHANGELOG.md).
