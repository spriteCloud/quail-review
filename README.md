# quail-review

[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-quail--review-1B365D?logo=github&logoColor=white&labelColor=0F1117)](https://github.com/marketplace/actions/quail-review)
[![Release](https://img.shields.io/github/v/release/spriteCloud/quail-review?color=4B8BBE&labelColor=0F1117)](https://github.com/spriteCloud/quail-review/releases)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-C0805A?labelColor=0F1117)](./LICENSE)

OSS PR bot. A small Go binary + GitHub Action that watches a PR (or a live
URL), opens follow-up PRs with generated Playwright tests, heals broken
locators when they drift, hunts real bugs against a running app, and posts
review verdicts as PR comments. Product site and reference recipes:
[spritecloud.github.io/quail-review](https://spritecloud.github.io/quail-review/).

> **Looking for the local UI, on-prem / data-sovereign deploy, or named
> SLA?** Those live in the commercial
> [`spriteCloud/quail`](https://github.com/spriteCloud/quail) edition.
> Both editions share the same engine; the OSS one ships the PR/CI
> surface only.
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
  Integration, Backend, UI, Mobile, Data, Non-functional.
- **Inference is yours**: any OpenAI-compatible base URL (Ollama, vLLM,
  OpenAI). Diff content stays local unless `QUAIL_ALLOW_DIFF_TO_LLM=1`.

## Features

Four commands you'll reach for by name. Everything else in the
[reference table](#reference-other-subcommands) below.

### `explore` — adversarial bug-hunting against a live URL

Applies 12 attack categories (boundary, injection, race, auth,
flow-interrupt, state-corrupt, cross-feature, sequence, role-switch,
upstream-dep, cumulative, data-edge) to every interactive element the
crawler finds. Human cadence + keyboard fallback break past shadow-DOM
widgets that reject stock `page.fill()`. Change-aware: the last PR diff
(paths only) is auto-detected as prioritisation hints.

```bash
quail explore \
  --url https://your-app.example.com \
  --timebox 5m --focus all \
  --llm http://localhost:11434/v1 --model qwen3-coder-next:latest
```

Ephemeral by default — specs generated, executed, and discarded; only
the Gherkin report streams to stdout:

```gherkin
# quail explore — execution report
# target: https://your-app.example.com
# session: 4 LLM round(s), 42 scenario(s) composed, session took 5m0s
# executed: 21 clean, 3 with anomalies, 18 unreachable
# unreachable: 18 timeout
# anomalies observed: 3 across 3 scenario(s)
#   state regressions (target vanished after back+reload): 2

  @adversarial @boundary
  Scenario: Income field should reject 5000-char strings — unreachable (timeout)
    Given I navigate to "https://your-app.example.com/apply"
    When I fill in the "Annual income" field with a 5000-character string
    ...
```

**Common flags:** `--url`, `--timebox <duration>`, `--focus all|<cat,cat,…>`,
`--depth shallow|standard|deep`, `--persist`, `--pr <N>`, `--llm <url>`,
`--model <id>`.

### `generate` — fan a PR diff into per-aspect test scaffolds

Reads the PR diff, decides which of the 10 taxonomy layers apply, and
emits Playwright specs + Gherkin feature files matching what changed.
Opens a follow-up PR against the same branch. Dry-run with `--dry-run`
to see the plan without opening a PR.

```bash
quail generate --pr 42
```

Opens branch `quail/tests-pr-42-<sha>` with:

```
tests/e2e/generated/<component>.spec.ts     # imperative Playwright
tests/e2e/features/<journey>.feature        # declarative Gherkin
tests/e2e/docs/coverage.md                  # layer breakdown
```

**Common flags:** `--pr <N>`, `--dry-run`, `--kinds <a,b,c>`,
`--exclude-kinds <a,b>`, `--emit imperative|declarative|both`.

### `heal` — repair broken Playwright locators

Reads a Playwright JSON report, identifies failing locators that drifted
(a renamed `data-testid`, a moved selector), and patches them. Defaults
to **on-failure** mode; `QUAIL_HEAL_MODE=proactive` runs on every push
regardless of test results.

```bash
quail heal --pr 42 --report playwright-report.json
```

Opens branch `quail/heal-pr-42-<sha>` with:

```
tests/e2e/generated/*.spec.ts   # updated locators, deterministic diff
tests/e2e/docs/heal-notes.md    # what was patched and why
```

**Common flags:** `--pr <N>`, `--report <file>`, `--dry-run`.

### `review` — post a markdown verdict on a PR

Triggered by a `@quail review` comment on a PR (or via CLI with `--pr`).
Reads the diff, applies the review rubric, and posts a single markdown
comment with severity-graded findings. No branch pushed, no files
changed — the comment IS the deliverable.

```bash
quail review --pr 42
```

Posts a PR comment shaped like:

```markdown
## quail review — PR #42

**Verdict:** 2 high · 1 medium · 3 low

### High severity
- **`src/components/ContactForm.tsx:41`** — user input rendered via
  `dangerouslySetInnerHTML`; XSS surface open to any authenticated user
- **`src/components/ContactForm.tsx:19`** — `type="text"` on email
  input; no format validation before POST

### Medium severity
- **`src/api/contact.ts:12`** — no HTTP status assertion on the fetch;
  a 500 will silently succeed on the client
…
```

**Common flags:** `--pr <N>`. That's the whole surface.

### Full sweep — the four together

A typical PR lifecycle uses all four:

```bash
quail explore --url https://staging.example.com --timebox 5m --focus all   # find real bugs
quail generate --pr 42                                                      # cover the new code
quail run-once --record                                                     # run the emitted suite locally
quail heal --pr 42 --report playwright-report.json                          # patch drift
# On the PR, a reviewer comments: @quail review
```

## Reference — other subcommands

| Command | Purpose | Key flag |
|---|---|---|
| `probe` | Crawl a live URL → full Playwright + Gherkin suite | `--url`, `--coverage breadth\|standard\|depth\|max` |
| `prompt "<text>"` | Probe scoped to journey kinds a natural-language filter picks | `--url`, `--evidence` |
| `run-once` | Run the generated suite locally, optionally record failures | `--record`, `--report`, `--grep <pat>` |
| `scan` | Dry-run for `generate` | `--pr <N>` |
| `ledger update` | Merge Playwright failures into `tests/e2e/docs/findings.md` | `--report <file>` |

## Invite `@spritecloud-quail` for PR-comment audits

External repos get a per-PR audit surface in one file. Invite the
[`@spritecloud-quail`](https://github.com/spritecloud-quail) user as a
collaborator (write access) and drop:

```yaml
# .github/workflows/quail-audit.yml
name: quail-audit
on:
  pull_request:
    types: [opened, synchronize, reopened]
  issue_comment:
    types: [created]

jobs:
  audit:
    if: >
      github.event_name == 'pull_request' ||
      (github.event.issue.pull_request && contains(github.event.comment.body, '/quail audit'))
    uses: spriteCloud/quail-review/.github/workflows/audit.yml@v1
    with:
      target-url: https://staging.example.com
      pass-rate-threshold: 80
    permissions:
      contents: read
      pull-requests: write
```

Every PR gets audited automatically. Any collaborator can also comment
`/quail audit` to re-run on demand. Details:
[docs/audit-install.md](./docs/audit-install.md).

## The 10-layer taxonomy

Every test quail emits maps to one of ten layers. Six of them auto-emit
on any live-URL probe; the other four trigger from PR-diff source code.

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

Full reference + recipes: <https://spritecloud.github.io/quail-review/docs.html>.

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

### Narrow the emitted taxonomy

Pass `kinds:` / `exclude-kinds:` to ship a focused PR instead of the
full matrix. Vocabulary (same names the `--kinds` CLI flag accepts):

> `journey, a11y, perf, visual, security, contract, health,
> observability, i18n, network, storage, print, mobile, responsive,
> touch, race, fuzz, webhook, graphql, auth-expiry, history-depth,
> clipboard, iframe, date-edges, file-upload, deeplink, http-chains,
> api, integration, grpc, compat, unit, pwa, locale, jobs`

| Goal | Pass |
|---|---|
| Accessibility-only PR | `kinds: a11y` |
| User flows + a11y | `kinds: journey,a11y` |
| Drop the heaviest layers | `exclude-kinds: mobile,integration,touch,race` |
| Commit `_dom/` debug dumps too | `dom-snapshots: 'true'` |
| Uncap a11y trio to every page | `a11y-uncap: 'true'` |

## Environment

Every var below has a matching CLI flag. Set what you need; leave the
rest at defaults.

### LLM composer (opt-in)

| Var | Default | Purpose |
|---|---|---|
| `QUAIL_LLM` | — | OpenAI-compatible endpoint. When set, up to 5 `@llm-composed` Scenarios per journey. **Strictly local-only** — never in public CI. |
| `QUAIL_MODEL` | `gpt-4o-mini` | Model id. Auto-set to `qwen3-coder-next:latest` on Ollama endpoints. |
| `QUAIL_LLM_LADDER` | — | Comma-separated model fallbacks. |
| `QUAIL_LLM_TIMEOUT` | `60s` | Per-call timeout. |
| `QUAIL_LLM_TOKEN_CAP` | `600` | Max output tokens per LLM call. |
| `QUAIL_HUMANIZE` | — | `0` to skip per-file humanization while keeping composer active. |
| `QUAIL_ALLOW_DIFF_TO_LLM` | `0` | Send PR diff to LLM; off by default. |
| `QUAIL_GRAPHQL_ENDPOINT` | `/graphql` | Override stub introspection path. |
| `QUAIL_WEBHOOK_ENDPOINT` | — | Webhook receiver path to activate signed-POST checks. |
| `QUAIL_WEBHOOK_SECRET` | — | HMAC signing secret. |

### Probe / spider

| Var | Default | Purpose |
|---|---|---|
| `QUAIL_TARGET_URLS` | — | Comma-separated URLs to probe (alternative to `--url`). |
| `QUAIL_COVERAGE` | `standard` | `breadth` (8/2) · `standard` (30/3) · `depth` (75/5) · `max` (120/5). |
| `QUAIL_BROWSER_PROBE` | — | `1` to drive Chromium (Playwright) instead of static HTML crawl. Required for SPAs. |
| `QUAIL_IGNORE_ROBOTS` | — | `1` to crawl `robots.txt` Disallow paths. Off by default; enable for sites you own. |
| `QUAIL_PROBE_ALLOW_LOOPBACK` | — | `1` to bypass loopback/private-IP guard (tests only). |
| `QUAIL_A11Y_UNCAP` | — | `1` to emit the a11y trio on *every* crawled page. Capped otherwise to keep the first PR small. |
| `QUAIL_DOM_SNAPSHOTS` | — | `1` to commit raw `tests/e2e/_dom/*.html` browser-render dumps. |

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

## AI usage rules

The LLM is allowed to do exactly two things:

1. **Humanize**: rewrite strings inside a deterministic test file so
   titles and step comments read like a human wrote them. The rewritten
   file is structure-checked against the original (same imports, same
   number of `describe`/`it`/`test`) and falls back to the deterministic
   output on any mismatch.
2. **Compose** (opt-in via `QUAIL_LLM`): propose up to 5 additional
   Gherkin Scenarios per journey, drawn ONLY from a registered
   step-pattern vocabulary. Invalid scenarios are dropped before the
   template renders.

The PR diff is **never** sent to the LLM unless
`QUAIL_ALLOW_DIFF_TO_LLM=1`.

## License

quail is dual-licensed:

- **AGPL-3.0** for the community edition — see [`LICENSE`](./LICENSE).
- **Commercial license** for organisations that cannot accept the AGPL —
  see [`COMMERCIAL.md`](./COMMERCIAL.md). Contact `hello@spritecloud.com`.

By submitting a pull request you agree to the
[Contributor License Agreement](./CLA.md). The `cla-assistant` check on
PRs will prompt you to sign if you haven't.

For the release-by-release history (v0.19 → v0.75), see
[`CHANGELOG.md`](./CHANGELOG.md).
