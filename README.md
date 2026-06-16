# reviewqa

[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-reviewqa-1B365D?logo=github&logoColor=white&labelColor=0F1117)](https://github.com/marketplace/actions/reviewqa)
[![Release](https://img.shields.io/github/v/release/spriteCloud/reviewqa?color=4B8BBE&labelColor=0F1117)](https://github.com/spriteCloud/reviewqa/releases)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-C0805A?labelColor=0F1117)](./LICENSE)

A small Go binary + GitHub Action that watches a PR, opens a follow-up PR with
generated tests for the new code, and heals broken Playwright locators.

- **One binary**, pure Go, no CGO — works locally and in CI.
- **AST-first**: regex/heuristic extractors derive test scaffolds deterministically.
- **LLM only humanizes**: titles, step comments, PR body. Falls back to the deterministic file on any error.
- **Inference is yours**: any OpenAI-compatible base URL (ollama, vLLM, DGX-hosted vLLM, OpenAI).

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
reviewqa scan --pr 42                              # dry-run
reviewqa generate --pr 42                          # opens follow-up PR from diff
reviewqa heal --pr 42 --report playwright-report.json
reviewqa probe --url=https://www.spritecloud.com   # generate from a live URL (no diff needed)
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
