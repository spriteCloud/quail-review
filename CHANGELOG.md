# Changelog

reviewqa's release-by-release history. v0.19 → v0.30 was the
taxonomy-closure arc that took the framework from a single-Playwright
happy-flow generator to a 10-layer deterministic test author. v0.55+
shipped the depth-parity arc (Contract, Integration, Mobile, A11y trio).
v0.61–v0.62 are the live-execution + composer-validation arc — first
real-site run + composer destination-DOM enforcement.

## v0.87.1 — Humanize wall-clock budget + discover-fallback fix

Two fixes that surfaced from a v0.87 ing.nl probe: it hung
forever doing LLM humanization across 167 generated files, and
the discover-fallback (added in v0.87) silently produced nothing.

### Humanize wall-clock budget
`finishProbe` / `runGenerate` loop `llmClient.Humanize` per
rendered file. On a slow local Ollama with 100+ generated
artifacts, the wall clock spirals — the per-call timeout is
60s, so worst case is ~hours.

- New `humanizeWithBudget(ctx, client, rendered)` helper. Honours
  a wall-clock budget that defaults to **5 minutes**. Once
  exhausted, remaining files keep their deterministic content
  and a warning logs the count skipped + the env knob to
  extend.
- `REVIEWQA_HUMANIZE_BUDGET=10m` (or any Go duration) overrides
  the default. Set the legacy `REVIEWQA_HUMANIZE=0` to skip the
  pass entirely.

### Discover-fallback uses crawl order
v0.87's `synthesiseFallbackJourneys` resolved targets via
`landing.Links → m.Pages[href]` — but on SPAs the link hrefs
don't always round-trip to the page-map keys (redirect
normalisation, fragment stripping). Result: 0 matches → 0
fallback journeys.

- v0.87.1 walks `m.Order` instead: the second crawled page is
  the target. Semantically "landing → first sub-page the crawler
  followed", which is exactly what JourneyDiscover models.
- Drops the prior "needs ≥ 3 outbound landing links" gate — if
  the crawler has ≥ 2 pages total, we have what we need.

1 new test covering the "links don't match page keys" SPA case;
600 → 601 passing.

## v0.87 — Settings → probe subprocess + empty-dir reuse + discover-fallback

Two user bugs from a v0.86 ing.nl probe, plus a safety-net for
SPA-heavy sites.

### Settings → probe subprocess env
v0.77 wired Settings into the in-process serve handlers but the
probe SUBPROCESS only inherited `os.Environ()`. Result: user
toggles LLM ON, hits Probe, sees "llm humanization disabled (no
OPENAI_API_KEY)".

- New `probeSubprocessEnv()` translates Settings.LLM into the env
  vars the probe CLI expects:
  - `LLM.Endpoint` → `REVIEWQA_LLM`
  - `LLM.Model` → `REVIEWQA_MODEL`
  - `LLM.APIKey` → `OPENAI_API_KEY` (or `ollama` for keyless
    local endpoints)
  - `LLM.TimeoutSec` → `REVIEWQA_LLM_TIMEOUT`
- `LLM.Enabled=false` explicitly zeroes `OPENAI_API_KEY`.
- Settings appended AFTER `os.Environ()` so user-saved wins.

### Empty-dir reuse
v0.85 verification left an empty `~/reviewqa-projects/<brand>/`
behind. v0.86's `reserveSiblingDir` refused to land in it (looked
like a non-reviewqa squatter) and bumped to `<brand>-1/`. v0.87
reuses empty candidate dirs — same shape as never-existed.

### Discover-fallback journey
ing.nl crawled (167 spec artifacts) but journey heuristics found
zero — SPA, all the useful surfaces behind interactions. UI showed
no Features. New `synthesiseFallbackJourneys` emits one
`JourneyDiscover` (landing → first same-origin nav target) when
no journeys came back but the landing page has ≥3 outbound links.
Clear log line when even the fallback can't fire.

6 new tests; 594 → 600 passing.

## v0.86 — Probe any website (WAF bypass + zero-item visibility)

User probed `ing.nl` from the v0.85 UI and got no scenarios.
Akamai/Cloudflare-style WAFs drop HTTP/2 streams from
non-browser clients, the static probe got `stream ID 9;
INTERNAL_ERROR`, no pages came back, the CLI exited 0 with the
log line `"probe: no items produced"`, and the UI showed a green
"Probe succeeded" with an empty sidebar. v0.86 fixes both halves.

### Browser-shaped headers
- New `internal/probe/headers.go` with `applyDefaultHeaders` —
  Chrome-on-Linux UA + the full `Sec-Ch-Ua`, `Sec-Fetch-*`,
  `Accept-Language`, `Upgrade-Insecure-Requests` set every
  modern browser sends on a top-level navigation. Defeats
  header-shape fingerprinting (the easy 80% of bot detection).
- The real probe identity moves to a custom
  `X-Reviewqa-Probe: reviewqa-probe/1 (+...)` header so honest
  origins can still tell who we are.

### `--browser` flag with auto-fallback
- New flag on `reviewqa probe`: `--browser auto|always|never`.
  Default `auto`.
- `auto` tries the static fetch first; if pages came back, done.
  If the static crawl returned zero pages AND any error matches
  `looksLikeWAFRejection` (HTTP/2 `INTERNAL_ERROR`,
  `connection reset by peer`, 403 / 429 / 503, TLS handshake
  failures), it retries the URL through the Playwright-driven
  browser probe (`internal/probe/browser/probe.mjs`). The
  browser presents as real Chromium and gets through where the
  static client can't.
- `always` skips the static attempt; `never` keeps the legacy
  static-only path (for CI hosts without Node/Chromium).
- `REVIEWQA_BROWSER_PROBE=1` legacy env still works — maps to
  `--browser=always`.

### Zero-items now surface as failure
- `finishProbe` returns a real error when the probe produced no
  items. The CLI exits non-zero. The error message includes
  context — "site likely blocked us — try --browser=always for a
  real-browser crawl" when WAF signatures are detected.
- `/api/probe` SSE `done` event gains `itemCount` + `reason`
  fields. The endpoint scans the captured CLI stdout for
  `probe: wrote local files count=N` / `probe: no items
  produced` markers and surfaces them.
- `streamCommand` keeps a 200-line ring buffer so the endpoint
  can read post-run without re-piping.

### UI: browser select + accurate verdict
- HOME probe row gains a `browser: auto/always/never` select
  alongside coverage.
- Failed probes now show a red verdict with the reason. When the
  failure looks like a WAF block and the user isn't already on
  `always`, the verdict appends a tip to switch.
- No auto-switch into an empty new project on failure.

4 new tests (browser-shaped headers, WAF detector table,
ParseBrowserMode + legacy env override). 590 → 594 passing.

## v0.85 — Scratch mode (launch the UI with no project)

User: *"What if I want to start from scratch without any project
loaded, probe and then automatically load the probed site?"*

`reviewqa serve` used to hard-fail when the workdir didn't exist.
Now it tolerates a missing path and opens the UI in scratch mode.

- `Run()` no longer errors on a non-existent workdir. It logs
  "starting in scratch mode" and boots normally.
- `loadProject` marks the response with `scratch: true` when the
  workdir is empty or doesn't exist on disk.
- `pickProbeDestination` falls back to
  `~/reviewqa-projects/<brand>/` in scratch mode. New
  `reserveSiblingDir` helper factors the collision-safe branch
  out from the v0.81 in-place path.
- Frontend:
  - Header pill reads "no project" when scratch.
  - HOME hero copy adjusts: "Start with a URL. / No project
    loaded. Probe a site to generate one, or import an existing
    folder."
  - Sidebar empty-state reads "Probe a URL to get started."
  - Run-preflight banner suppressed in scratch mode.

Auto-switch after probe was already wired by v0.81's SSE
`start.workdir` field — no UI change there.

**Usage:**

```bash
mkdir /tmp/fresh && cd /tmp/fresh
/tmp/reviewqa serve --no-browser
# Server boots; UI opens on HOME with Probe + Import.
# Probe ing.nl → lands in ~/reviewqa-projects/ing/, UI switches in.
```

5 new tests; 585 → 590 passing.

## v0.84 — Hide Tests for reviewqa projects + HOME Import card + spec rendering parity

Three fixes after looking at `examples/spritecloud-com-dgx`:

### 1. Tests section suppressed for reviewqa projects
v0.80's `loadSpecs` walked `tests/e2e/` recursively and surfaced
all 222 reviewqa-emitted `*.spec.ts` layer artifacts (a11y,
mobile, contract, security, network, perf, visual, …) as
"Tests" — visually overwhelming AND incorrect (they're already
covered by Run from the Gherkin Scenario view).
- New `isReviewqaProject(workdir)` checks for any `.feature`
  file or `tests/e2e/steps/reviewqa.steps.ts`. When either
  exists, `loadSpecs` returns `nil`. The Tests sidebar
  disappears.
- Vanilla Playwright projects keep the Tests section.

### 2. HOME Import card
Front-and-centre import. New full-width "Import a project"
card on HOME, under the Probe card. One labelled input + an
Import button. Shells the same `POST /api/switch-project` the
switcher dropdown uses. The dropdown's bottom input is also
renamed "Import" and auto-focuses when the menu opens.

### 3. Spec rendering parity with features
When the Tests section IS visible, the per-spec page now reads
like a feature page:
- `parseSpecFile` tracks `test.describe()` source ranges via a
  brace-counting scan so each test attaches the innermost
  containing describe.
- `openSpec` groups tests by Describe heading and renders each
  as a scenario card — same last-run pill, Run button, chrome.
- `loadSpecs` joins in `LoadLastRunIndex(workdir)` so the pill
  surfaces immediately.

584 → 585 passing.

## v0.83 — Design tokens + responsive polish + final docs

End-of-arc release closing out the v0.80–v0.83 onboarding /
history / polish thread.

### Status color tokens
- New `--ok-green`, `--ok-soft`, `--fail-red`, `--fail-soft`,
  `--bad-red`, `--bad-soft`, `--bad-dark` design tokens in
  `:root`. Scattered hex literals (`#c0392b`, `#15803D`,
  `#B91C1C`, `#2e7d32`, `#6b2118`) replaced project-wide so
  status colors stay consistent.
- New `--shadow-soft` and `--shadow-pop` shadow tokens, plus
  `--t-fast` (`.12s ease`) for transition standardization.

### Responsive polish
- New `@media (max-width: 720px)` breakpoint below the existing
  900 px sidebar-collapse rule: smaller topbar padding,
  shorter pixel-rail, tighter scenario-card spacing, content
  padding shrinks to 20 px / 14 px.

### Docs
- README "What's new" section extended through v0.83.
- README example callout: v0.79 → v0.83.
- CHANGELOG entries for v0.80–v0.83.

### Out of scope (deferred)
- The Stakeholder Summary "What changed" delta needs persisted
  Catalogue JSON sidecars to compute reliably; deferred to a
  future release when the storage shape settles.

584 tests still passing.

## v0.82 — Multi-run history + pass-rate sparkline

The per-run JSON reports written under `tests/e2e/.reviewqa-runs/`
have been on disk since v0.75 but only the most-recent one fed
the last-run pill; previous runs were inert. v0.82 reads them.

- New `LoadScenarioTimeline(workdir, scenarioName)` in
  `internal/serve/run.go` walks every `run-*.json` in the runs
  dir, parses each via the existing `ParsePlaywrightJSON`, picks
  the scenario's record out, and returns the last 20 sorted
  oldest→newest. Falls back to the file's mtime when the parsed
  record's `At` is empty.
- New `GET /api/scenario-runs?scenario=…` returns
  `{ runs: [{at, status, durationMs}] }`.
- Frontend: the last-run pill is now clickable. Click → popover
  with a vanilla SVG sparkline (one bar per run, height by
  duration, color by status) plus the last 5 rows as a list.
  Click outside / on the pill again to close.

2 new tests for the timeline reader + endpoint validation.
582 → 584 passing.

## v0.81 — Probe creates a new sibling project

The HOME Probe form used to write generated files into the
*current* workdir — probing a brand-new URL while serving the
spritecloud example dumped petstore3 files into spritecloud (we
saw the contamination in this session). v0.81 fixes that.

- New `pickProbeDestination(workdir, url)` in
  `internal/serve/probe_endpoint.go`:
  - If `BrandFromHost(url)` matches the current workdir's name →
    re-probe in place (existing behaviour).
  - Otherwise create a sibling dir named after the brand
    (`petstore3.swagger`, `playwright`, …) under the current
    workdir's parent.
  - Collision-safe: if the slot is squatted by a non-reviewqa
    dir, suffix `-1`, `-2`, … until we find an empty slot or an
    existing reviewqa project for the same brand (in which case
    we re-probe it in place).
- Frontend: SSE `start` event reports the destination workdir;
  on `done` with `passed: true` the UI auto-POSTs
  `/api/switch-project` to land in the new project. Sidebar
  refreshes, toast confirms.

4 new tests cover re-probe-in-place, fresh-brand, squatter
collision, and existing-sibling re-probe. 578 → 582 passing.

## v0.80 — Onboard non-reviewqa projects + vanilla Playwright

### Onboard any Playwright project
`looksLikeReviewqaProject` (used by the switcher + sibling
discovery) now accepts:
- the original reviewqa layout (`tests/e2e/features/`,
  `reviewqa.steps.ts`, stakeholder docs)
- `playwright.config.{ts,js,mjs,cjs}` at the project root
- `tests/`, `e2e/`, `playwright/`, `spec/`, `__tests__/` with any
  `*.spec.{ts,js,mts,mjs}` or `*.test.{ts,js}` inside.

The project switcher dropdown surfaces vanilla Playwright sibling
projects alongside reviewqa-native ones.

### Consume vanilla `*.spec.ts` in the UI
- New `internal/serve/spec_parse.go` regex parser extracts
  `test('…')` / `test.describe(…)` / `.only` / `.skip` /
  `.fixme` / `it(…)` titles. No JS interpreter — pure regex over
  source.
- New `SpecRef` type alongside `FeatureRef`; `Project.Specs` is
  a new field in `/api/project`.
- Sidebar gets a `Tests` section under `Features` (hidden when
  the project has none). Clicking a spec opens it with one
  scenario-style card per test; ▶ Run shells the existing
  `/api/run-scenario` path (bddgen is conditionally skipped when
  `node_modules/.bin/bddgen` isn't installed, so vanilla projects
  go straight to `npx playwright test --grep <name>`).
- Edit + Chat are skipped for vanilla specs — those make sense on
  Gherkin where the semantic structure is shallow; raw TS test
  bodies aren't a one-shot LLM rewrite target.

### UI polish (alongside)
- Settings `Test connection` button is now diagnostic:
  validates endpoint + model client-side, reports
  "Pinging <endpoint>…" while in-flight, distinguishes between
  HTTP failure, LLM "ok=false" replies, and network errors.
  Disabled state during the call.
- Toast restyled: bottom-right (was top-right), bottom-up slide
  with cubic-bezier easing, inline SVG check / × / info icon,
  bigger drop-shadow.
- Chat button is no longer inert when the LLM is off — clicking
  it shows a toast and opens Settings.

### Tests
Spec parser handles all three quote styles + 6 test modifier
variants. New `TestParseSpecFile_ExtractsTitles`,
`TestLoadSpecs_FindsFiles`,
`TestLooksLikeReviewqaProject_VanillaPlaywright`,
`TestLooksLikeReviewqaProject_SpecRootOnly`. 574 → 578 passing.

## v0.79.1 — Drop PRETTY/RAW toggle + UI copy trim

Quick patch on top of v0.79.

- **Dropped the PRETTY / RAW .feature toggle** from the feature
  view. Pretty is the only view now. Less clutter, one fewer
  unused code path. `renderFeature` no longer takes a `gherkin`
  arg; `/api/feature` no longer returns the raw file body.
- **UI copy sweep.** HOME hero, Probe card, capability shelf,
  Settings page, run-preflight banner, sidebar nav, history group,
  toasts, project switcher dropdown — every long sentence I
  added in v0.76–v0.78 trimmed to a short, direct label. ELI5,
  professional, no marketing prose.
- The doc viewer's Rendered/Raw toggle stays — that one's
  rendering a real format conversion, not a redundant view.

574 tests still passing; `go vet` clean.

## v0.79 — Scenario-card spacing fix + cleanup + docs refresh

End-of-day wrap-up release. Five threads, single PR.

### Scenario card empty-space bug
v0.78 made `.scenario-head` `flex-direction: column`, but
`.scenario-name-wrap` kept its old `flex: 1 1 260px` basis from
v0.76 (when the head was a *row*). In a column-flex container that
260 px is now the height basis — the wrap stretched to ~260 px
tall even when content needed ~80 px, leaving a 200-px gap
between the last-run pill and the action row. Fixed by switching
`.scenario-name-wrap` to `flex: 0 0 auto` so it shrinks to
content. Action row sits a clean 12 px under the pill now.

### Dead code removal
- Dropped the two `var _ = errors.New` sentinels (and their
  `errors` imports) from `internal/serve/settings_endpoint.go`
  and `internal/serve/projects.go` — leftovers from when those
  files initially referenced `errors` and then stopped.

### Duplicate consolidation
- Added `internal/probe/origin.go` with one canonical
  `BrandFromHost` / `BrandFromOrigin` helper. `probe.hostToName`
  and the `gen.brandFromOrigin` template helper now both go
  through it, so the scheme / `www.` / public-suffix stripping
  rules live in one place instead of drifting between probe
  symbol generation and stakeholder-summary rendering.

### Complexity refactor: handlerForState
- The serve mux registration was a 284-line monolith with 14
  inline `mux.HandleFunc` closures. Each closure has been
  extracted into a named handler function at file scope
  (`handleProject`, `handleFeature`, `handleSteps`, `handleDoc`,
  `handleScenarioCRUD`, `handleValidateScenario`, `handleProbeDOM`,
  `handleComposeSteps`, `handleScenarioChat`, `handleRunPreflight`,
  `handleRunScenario`, `handleLLMStatus`, `handleLocatorCandidates`,
  `staticAssetsHandler`, `handleRoot`).
- `handlerForState` shrinks to a ~30-line route table. Same
  API contract, easier to grep / move / test.

### Docs refresh
- README: bumped "v0.65 binary" → "v0.79 binary" in the examples
  callout.
- README: rewrote the "Tailor the generated suite locally"
  section to cover v0.71–v0.78 (Run + last-run pill, HOME view,
  Settings page, project switcher, stakeholder summary history).
- `web/index.html` hero bumped to v0.79.

### Repo housekeeping (scripts/cleanup-branches.sh)
- New committed script. After merge, deletes all merged remote
  branches (26 `reviewqa/tests-pr-*` CI-generated leftovers),
  cleans up local merged branches, and closes any open PRs >30 d
  / issues >60 d (today both empty).

### Tests
574 still passing. No new tests added — refactor preserves
behaviour and the existing endpoint tests cover the moved
handlers.

## v0.78 — Settings polish + actions-left + Stakeholder Summary rewrite + history + project switcher

Five UX threads from one v0.77 screenshot tour.

### Settings polish
- LLM endpoint / model / API key / timeout inputs no longer stretch
  into 250-px-tall boxes. Pinned to 40px with `flex: 0 0 auto`.
- Toast helper (`toast()`) emits a brief top-right notification.
  Wired into Settings save, probe finish, project switch, and any
  future "did this work?" affordance.

### Scenario action row, always left, always below
- `.scenario-head` switched from `flex-wrap: wrap` (v0.76) to
  `flex-direction: column`. Run / Chat / Edit / Delete now sit on
  their own row at the bottom-left of every scenario card on every
  viewport — including wide screens.

### Stakeholder Summary rewrite + history
- **Hero compacted.** The 50-px URL hero that wrapped into three
  lines (`https:// www.spritec loud.com`) is replaced with
  `"Coverage map for <brand>."`. New `brandFromOrigin` template
  helper strips the scheme and `www.` so the headline reads as the
  brand. Full URL drops into the cover meta strip.
- **Risk strip** under the hero — three KPI tiles in `--charcoal`:
  *Gated on release* (critical journey count), *Standard
  regression* (standard journey count), *Awaiting wireup*
  (Integration scaffold count).
- **Sharper prose.** "At a glance" rewritten with stakeholder verbs
  — *green-lit, gated, unverified*. New "Unverified surface"
  paragraph calls out scaffold-only layers explicitly.
- **History storage.** `writeRenderedLocal` now also writes
  timestamped copies under `tests/e2e/docs/history/summary-
  <UTC>.html` and `findings-<UTC>.md` on every probe. The
  canonical `summary.html` / `findings.md` always reflect the
  latest generation; history is append-only.
- **History in the sidebar.** `loadProject` scans `docs/history/`;
  the serve UI gets a collapsible "Past summaries · findings"
  group at the bottom of the sidebar, sorted newest-first.

### Project switcher
- The header pill (`spritecloud-com-dgx`) is now a clickable
  dropdown trigger. Click → menu listing Current, Siblings
  (auto-discovered reviewqa projects in the parent dir), Recents
  (from settings), plus an "Open path" input for free-form
  workdirs.
- Backend: `workdir` promoted to a mutex-protected `workdirState`
  shared by every handler closure. The `Handler(workdir string)`
  signature is preserved (it now wraps the string in a state) so
  existing tests don't change.
- New endpoints:
  - `GET /api/projects` → `{current, siblings, recents}`
  - `POST /api/switch-project` → validate, set state, push to
    recents (capped at 8, dedup) in `~/.config/reviewqa/serve.json`.
- Frontend swaps the project state on switch, re-fetches
  `/api/project`, and toasts the new path.

### Tests
6 new tests across project switcher (`projects_test.go`) and
sibling discovery. 568 → 574 total passing.

## v0.77 — Settings page + probe `--local` (no GitHub token required)

Two threads from the same v0.76 HOME session:

### Probe from HOME no longer needs `GITHUB_TOKEN`

The v0.76 HOME probe form shelled out to `reviewqa probe --url <X>`,
which defaults to "render → open PR via gh". Without a GitHub
token the CLI errored with `gh: missing GITHUB_TOKEN /
REVIEWQA_GITHUB_TOKEN` after the probe finished — so the UI
appeared to "fail" even though the deterministic generation was
fine.

- New `--local` flag on the `probe` CLI writes rendered files
  directly into `cfg.WorkDir`, skipping the gh.New / OpenPR
  pipeline entirely. No token required.
- `POST /api/probe` (the HOME form's backend) now passes
  `--local` automatically. The user never sees the PR-open path.
- The CLI's other entry points (`generate`, `prompt`, the
  CI-friendly `probe` without `--local`) keep the existing PR
  behaviour — `--local` is opt-in.

### Settings page

The serve UI now has a `SETTINGS` row in the sidebar (below
HOME). Edits persist to `~/.config/reviewqa/serve.json` with mode
`0600` and take effect on the next API call — no restart needed.

Sections:
- **LLM composer / chat** — an ON/OFF toggle, endpoint, model,
  API key (password field), timeout. A "Test connection" button
  posts to `POST /api/llm-test` which sends a one-line ping
  through `llm.Chat` and reports the round-trip result without
  saving anything.
- **Probe defaults** — coverage preset used by HOME when none is
  picked explicitly.
- **Run defaults** — soft timeout cap, keep-playwright-report
  toggle.

The LLM settings overlay the existing `REVIEWQA_LLM` /
`OPENAI_API_KEY` env-var pipeline: anything set in the file wins;
unset fields fall back to the env defaults. An explicit OFF
zeroes the API key so downstream sees the LLM as disabled
regardless of what the env says.

A new test set (`internal/serve/settings_test.go`) covers load /
save round-trip, GET/POST endpoints, the LLM-test rejection of an
empty endpoint, and the env-overlay rules (settings win; OFF
overrides env).

### Persistence callout

Every edit you make in the UI is already persistent — chat-edited
scenarios, Edit/Delete on a scenario, HOME probe runs, run
verdicts. All write to disk (`.feature` files, project tree,
`tests/e2e/.reviewqa-runs/last-run.json`). Restarting `reviewqa
serve` doesn't lose any of it. v0.77 just makes the LLM
configuration persistent too.

## v0.76 — HOME view (probe-from-UI) + responsive scenario card

Two threads from one screenshot — same release.

### HOME view

The sidebar now opens with a `HOME` row above `FEATURES`. The
home panel is the first thing you see when you launch `reviewqa
serve`:

- **Project summary** — workdir, binary version, # features,
  # scenarios, LLM status, Playwright preflight, all read off
  the existing `/api/project`, `/api/llm-status` and
  `/api/run-preflight` endpoints.
- **Probe a URL** — a single input + coverage select + Probe
  button. Hits a new `POST /api/probe` SSE endpoint, which
  shells out to the reviewqa binary's own `probe` subcommand
  (via `os.Executable()`) in the project's root directory and
  streams stdout line-by-line into a terminal-style panel. On
  success the sidebar refreshes so the new / updated features
  appear immediately.
- **Capability shelf** — six tiles pitching what you can do from
  this UI: Run, Chat-edit, Locator suggest, Stakeholder summary,
  Test catalogue, Bug-discovery ledger. Tiles deep-link straight
  to the relevant doc or scroll the sidebar to the features
  list.

The probe endpoint validates the URL (http/https scheme + non-
empty host), rejects GET, and inherits the streaming pattern from
`/api/run-scenario` so the frontend SSE handler is almost the
same code shape as Run.

### Responsive scenario card

The Scenario card head now uses `flex-wrap: wrap`. When the title
+ action button group can't share a row, the buttons drop onto
their own line below instead of crushing the title into a 100-px
column. Fixes the "one word per line" wrap the user flagged on a
narrow viewport.

No template / probe / chat behaviour changes; new tests cover
the probe endpoint's URL validation and cwd derivation.

## v0.75.1 — UI polish: pill placement + brand-only feature names

Two narrow-scope fixes on top of v0.75:

- **Scenario card layout at narrow widths.** The "last passed/
  failed/not run" pill used to float inline next to the scenario
  name. When the name wrapped over multiple lines the pill landed
  in awkward whitespace beside the action buttons. The pill now
  stacks below the name on its own row — clean alignment whether
  the title takes 1 or 4 lines.
- **Drop `www.` and TLDs from generated symbol names.** Feature
  titles, test.describe groups and contract suites read as the
  brand only: `www.spritecloud.com` → `Spritecloud` (was
  `WwwSpritecloudCom`), `playwright.dev` → `Playwright`,
  `petstore3.swagger.io` → `Petstore3Swagger`. Uses the
  publicsuffix list so `.co.uk`, `.github.io`, etc. strip
  correctly. All example fixtures regenerated.

No template, run, or chat behaviour changes.

## v0.75 — per-step results + Last-execution pills + Stakeholder Summary polish

Three asks from the user after the v0.74 Run fix worked:
1. Show each step's verdict, not just the overall test result —
   "I want to evaluate if the chat-AI's last step worked".
2. Stakeholder Summary still didn't bring value — restructure with
   a stakeholder lens.
3. Show the last-execution status on every Scenario card so the
   verdict survives across reloads.

### Per-step run results

- `internal/serve/run.go` now passes `--reporter=line,json` with
  `PLAYWRIGHT_JSON_OUTPUT_NAME=<runs-dir>/run-<ts>.json` so
  Playwright writes the structured report alongside the streamed
  line output. (Playwright 1.61's CLI rejects `--reporter=json:path`
  — the env var is the supported escape hatch.)
- After the run exits, `ParsePlaywrightJSON` walks
  `suites[].specs[].tests[].results[].steps[]`. Only outer-most
  steps whose title starts with a Gherkin keyword (Given / When /
  Then / And / But) are kept — playwright-bdd wraps each Gherkin
  step in `test.step()` and Playwright internals nest inside.
- A final SSE chunk `event: steps` carries `{scenario, status,
  durationMs, steps:[{title, status, durationMs, error}]}`.
- Frontend renders a per-step panel above the streamed terminal:
  ✓ green for passed, ✗ red for failed, – mist for skipped, with
  durations and error tooltips.

### Last-execution pill per Scenario

- Every Run writes
  `tests/e2e/.reviewqa-runs/last-run.json` — a flat map keyed by
  Scenario name. `.reviewqa-runs/` is already gitignored from
  v0.74.
- `/api/feature` joins this on every request: `Scenario.LastRun
  *LastRunRecord` is populated when an entry exists.
- Each Scenario card shows a pill next to the name:
  green "passed · 4.2s · 2m ago", red "failed · …", mist "not run"
  when no record exists. Time formatted via a fresh `formatAgo`
  helper.

### Stakeholder Summary polish

The Stakeholder Summary's structure was metadata-first (KPI big
numbers, priority bar, tables). Real stakeholders read prose
first — restructured the template (`pw_work_summary.tmpl`) around
their questions:

- **At a glance** — narrative card. Two paragraphs translating the
  counts into prose ("reviewqa crawled N pages and identified M
  journeys; K are critical — gated on every release; …"). Drops
  the 4 KPI big-number boxes that v0.73 had.
- **Coverage map** — a 2×3 grid of layer cards. Each layer (UI /
  API / Contract / Integration / Mobile / Non-functional) gets a
  status pill (`exercised` green, `scaffold only` copper, `no
  surface` mist) and a one-line description of what reviewqa
  generated for it.
- **Priority mix** — kept; same visual.
- **Journey cards** (NEW) — replaces the journey table. Each card
  shows the priority pill, the kind (`convert`, `browse`, …), a
  one-line stakeholder blurb derived from a new `journeyKindBlurb`
  template func ("users submit the lead / contact form", "users
  navigate marketing content", …), the .feature path, and a big
  copper step count.
- **Pages crawled** — kept as a table, moved below journeys.
- **Recommended next steps** (NEW) — numbered checklist guiding
  the stakeholder through install → execute → triage → re-probe →
  tailor in the browser.

The v0.73 brand polish (deep-water cover, pixel-rail, copper
labels, Sora display) stays intact — only the section structure
changes.

### Examples + tests

- All five summary-emitting examples re-emitted via
  `scripts/refresh-examples.sh`.
- 3 new tests in `internal/serve/run_test.go` (Playwright JSON
  parsing extracts only Gherkin steps; last-run.json round-trip;
  missing file → empty index).
- `TestSummaryTemplate_RendersPriorityMix` token list updated
  for the new section IDs.
- Suite 556/556 green.

main.go version 0.74 → 0.75.

## v0.74 — Run scenario: bddgen pre-step + drop stray feature arg

The ▶ Run scenario flow shipped in v0.72 always returned "Error: No
tests found" against a real playwright-bdd suite. Two bugs:

1. **playwright-bdd v9 requires `bddgen` to run BEFORE `playwright
   test`.** bddgen parses the .feature files and writes
   .features-gen/*.spec.js — the actual test files Playwright
   discovers. v0.72 skipped this step, so Playwright collected zero
   tests and exited with "No tests found".

2. **The feature path was being passed as a positional arg to
   `playwright test`.** Even after bddgen ran, the positional arg
   would have constrained Playwright to look for a Playwright spec
   at that path (the .feature file itself isn't one), masking any
   real test discovery.

Fixes:

- `internal/serve/run.go::RunScenarioStream` now runs
  `node_modules/.bin/bddgen` first when the binary exists, streams
  its output as `event: line` chunks alongside the playwright run,
  and aborts with the bddgen exit code if generation fails. After a
  clean bddgen, runs `node_modules/.bin/playwright test
  --reporter=line --grep "<scenarioName>"` — no positional arg.
- Refactored stdout/stderr piping into a shared `streamCommand`
  helper so the two phases share one SSE stream.
- The featureRel argument stays on the API for symmetry (the UI
  still passes it for the start event's logging) but is intentionally
  unused at run time — a comment notes the reasoning.

Live-verified against `examples/spritecloud-com-dgx` with a real
`npm install && npx playwright install`. A run of
`browse — back button after navigation returns to landing` streams:
- `# bddgen — generating .features-gen/ from .feature files`
- `# playwright test --grep "..."`
- `[1/1] [bdd-chromium] › ... › 1 passed (5.4s)`
- `event: done` with `exitCode: 0`, `passed: true`.

Suite 553 / 553 green. main.go version 0.73 → 0.74.

## v0.73 — Stakeholder Summary restyle + Markdown render + Run preflight banner

Three problems the user hit in real usage of the v0.72 UI:

1. **`tests/e2e/docs/summary.html` shipped a dark utilitarian
   theme** that didn't match the rest of the branded UI. The user
   flagged it twice — restyled now.
2. **`tests/e2e/docs/test-catalogue.md` and `findings.md` rendered
   as raw monospaced text** in the serve UI's docs view. The
   stakeholder docs are meant to be readable to non-technical
   reviewers; that wasn't useful.
3. **▶ Run was disabled without a clear next step** when the
   workdir lacked `node_modules/`. Tooltip-only signal was buried.

This release:

- **Rewrote `internal/gen/templates/ts/pw_work_summary.tmpl`** to
  honour the deck brand system from `sc-claude-plugin/shared/`:
  - Deep-water cover band with copper Fira Code label, Sora 800
    title, `spriteCloud` wordmark (Inter 900, sprite #0090D0,
    Cloud #9DA5AE).
  - Pixel-rail separator (15 blocks, copper peak at #9, solid hex
    per the projector rule).
  - Light KPI strip with Sora 800 52px deep-water values, Fira
    Code copper labels.
  - Priority mix bar in solid hex tokens (bad red / deep-water /
    mist), 22px pill bar with legend.
  - Tables with Fira Code copper headers, Inter 13px bodies,
    border-warm separators, copper-soft hover rows.
  - Status pills for priorities using the brand spec tokens.
  - Footer ink band with regenerate hint + GitHub link.
- **Added `github.com/yuin/goldmark`** as a direct dependency.
  Configured with GFM (tables, autolinks, strikethrough) +
  `AutoHeadingID`. `/api/doc` now returns an `html` field for
  `.md` paths.
- **Markdown styling** under `.doc-content.markdown` mirrors the
  brand: Sora display headings, copper h3 in Fira Code uppercase,
  charcoal pre blocks, copper marker on lists, copper-soft hover
  rows in tables, copper underline on h1.
- **Rendered / Raw toggle** on doc views — Pretty by default,
  monospaced fallback for the raw markdown source.
- **`.run-banner`** appears above feature views when preflight
  fails. Shows the exact `cd <abs path> && npm install && npx
  playwright install` command with a Copy button. The Run button
  itself stays disabled with the same tooltip — the banner just
  makes it discoverable.
- All six examples re-emitted via `scripts/refresh-examples.sh`
  so the committed `summary.html` ships the new look.

Suite 553 / 553 green (test tokens updated for the new template).

## v0.72 — ▶ Run scenario from the UI

Each Scenario card gets a **▶ Run** button that shells out to
`npx playwright test --grep "<scenario name>"` in the workdir,
streams stdout via Server-Sent Events, and renders a live terminal
panel docked to the bottom of the page.

Backend:
- `internal/serve/run.go` — `Preflight(workdir)` stats
  `node_modules/.bin/playwright`. Surfaced through new
  `GET /api/run-preflight` so the UI can gate the button at load.
- `RunScenarioStream` spawns the command with `--reporter=line`,
  pipes stdout, and writes `event: start` / `event: line` /
  `event: done` SSE chunks. Final `done` carries `exitCode` and
  `passed`.
- Single-flight per workdir — `acquireRun`/`releaseRun` keyed by
  the absolute path so concurrent runs against the same suite are
  refused with 400.
- Browser disconnect cancels the underlying exec (request context
  propagates via `exec.CommandContext`).
- `POST /api/run-scenario` body `{feature, scenario}`. When
  preflight fails the response is a 400 with the install hint.
- `ParseRunSummary` tested separately — extracts passed / failed /
  skipped counts from the line reporter's summary lines.

Frontend:
- ▶ Run button (copper primary, smaller padding) next to Chat /
  Edit / Delete. Disabled with install-hint tooltip when preflight
  fails.
- Bottom drawer slides up with a charcoal terminal panel rendering
  the SSE stream live. Indeterminate copper progress bar at the
  top while running, freezes when done. Status pill
  (running / passed / failed) and a Stop button at the right.
- Output lines color-coded: copper for the command line, green for
  `✓`-prefixed pass lines, red for `✘` / `FAIL` lines.

Tests: 5 new in `internal/serve/run_test.go` (preflight ready /
not-ready, summary parsing for all-pass / mixed / empty,
single-flight lock). Suite 553 / 553 green.

Usage:
```bash
# In your generated suite (one-time setup)
cd ./my-generated-suite
npm install && npx playwright install

# Back in reviewqa serve, hit ▶ Run on any Scenario.
```

## v0.71 — chat-flow hardening + always-on suggest + step validity

Surfaced two real problems from a v0.70 chat round-trip in the
user's hands:

1. The LLM proposed two extra steps that LOOKED like Gherkin but
   didn't match any registered playwright-bdd pattern ("Then the
   form contains an email address field", "Then the form contains
   a submit button"). The chat validator only checked structural
   parse, so they sailed through as `valid:true` and would have
   crashed the suite at test time.
2. The 🔍 suggest button was hidden on those new steps because the
   client-side predicate required a quoted argument or
   `<placeholder>` in the step text. The user couldn't reach the
   locator probe for the steps the AI just wrote.

This release closes both gaps:

- `internal/composer/composer.go` exports `MatchesRegisteredPattern`
  (thin wrapper over the existing private matcher). The serve
  package uses it to evaluate every step.
- `internal/serve/feature.go` Step now carries `Valid bool`,
  populated by the parser. Surfaced through `/api/project`,
  `/api/feature`, and `/api/scenario-chat`.
- `internal/serve/chat.go` adds `firstUnregisteredStep` — after the
  existing structural validation, it walks the proposed block and
  surfaces the first step that doesn't match a registered pattern.
  `Valid:false` + `Notes:"step does not match a registered
  pattern: <text>"`. The UI's Apply button stays disabled until
  the user asks the chat to rewrite.
- `chatSystemPrompt` got firmer language and a self-check
  instruction ("locate each step verbatim in the pattern list
  below; if you cannot, REWRITE that step"). Live-tested against
  the DGX — the model now adapts to the closest registered pattern
  instead of inventing new step text.
- Frontend always renders the 🔍 suggest button on every step
  (predicate dropped). Modal copy reworded: "Suggest a Playwright
  locator. Hint can be the visible text of the element you're
  after."
- Invalid steps render with a red left border + " · not a
  registered pattern" inline label so existing scenarios with
  hand-edited drift are visible at a glance.

Tests: 3 new (parser Valid field; firstUnregisteredStep finds
hallucinated step; clean block returns empty). Suite 547 / 547
green.

## v0.70 — per-Scenario chat for non-technical maintenance

A non-technical reviewer can now talk to the configured LLM about a
SPECIFIC Scenario — "change the form to contact, not login"; "add a
step asserting the success toast appears"; "the page now redirects
to /thanks, update the assertion" — and Apply the assistant's
proposed update with one click. The conversation persists across
page reloads in `localStorage`, keyed by feature path + Scenario name.

Backend
- `internal/serve/chat.go` — new `Chat(ctx, ChatInput)` orchestrates
  the LLM round-trip. System prompt instructs the model to act as a
  Scenario maintenance helper, output a 1-3 sentence conversational
  reply, and emit the full updated block after a `---SCENARIO---`
  sentinel ONLY when an edit is implied.
- `parseChatResponse` splits the reply, strips backtick fences,
  reuses `extractScenarioBlock` to clean any LLM chatter, and
  validates the proposed block via `validateScenarioBlock`. Invalid
  proposals surface as `{valid: false, notes: "..."}` so the UI can
  render the block read-only and the user can edit-then-Apply.
- Stateless backend — the client owns the conversation history.
  Bounded to ~20 turns server-side to keep token use sane.
- Best-effort DOM grounding: when `url` is set, `FetchAndParseDOM`
  feeds landmarks into the prompt so the LLM can reference real
  headings, links, and form fields.
- New endpoints: `POST /api/scenario-chat` and `GET /api/llm-status`
  (the UI uses the latter to decide whether to enable the Chat
  button at page load).

Frontend
- 💬 Chat button on every Scenario card. Disabled with tooltip when
  REVIEWQA_LLM is unset.
- Right-side drawer (440px) instead of a modal so the Scenario card
  remains visible while chatting. Slide-in animation.
- Bubble UI: copper user bubbles right, white assistant bubbles left.
  Thinking spinner while the LLM responds.
- When the assistant returns a `proposed` block, a copper-bordered
  card appears below the last bubble with the proposed Gherkin
  preview + an Apply button (calls the existing v0.66 PATCH
  endpoint) + a Dismiss button.
- Cmd/Ctrl-Enter sends; Clear button purges history; Close just
  shuts the drawer (history is kept).

Tests: 5 new in `internal/serve/chat_test.go` (response-only,
proposed-block extraction, backtick-fence stripping, empty-input
rejection, history inclusion). Suite 544 / 544 green.

Live-verified against the spritecloud-com-dgx example with REVIEWQA_LLM
pointed at the DGX (`qwen3-coder-next:latest`): asking "add a step to
assert the page title also contains Contact" round-trips a valid
updated Scenario in 8-12s.

## v0.69 — reviewqa serve UI branding parity

User feedback after the v0.65–v0.68 serve arc: the local UI shipped
with the brand CSS *variables* but not the full design *system* of
the public site (https://spritecloud.github.io/reviewqa/). The GH
Pages site is meaningfully more polished — wordmark, pixel-bar motif,
Sora display hierarchy, copper Fira Code labels, card-and-hover rhythm.

This release brings the serve UI native to that system. No new
features — pure polish:

- **Wordmark** (`spriteCloud` with blue sprite + grey Cloud, Inter
  900) anchors the topbar.
- **Pixel-rail** under the topbar — 15 small varying-height blocks
  with the copper peak at #9 — mirrors the public hero's `.pixel-bar`.
- **Sora display hierarchy** with -0.025em letter-spacing on feature
  names and the placeholder hero.
- **Copper Fira Code labels** (uppercase, 0.16em tracking) for section
  markers throughout.
- **Scenario cards** now have the public-site `.card` rhythm:
  border-warm border, hover lift to copper border + soft shadow,
  Sora 600 scenario names.
- **Step rows** use a borderless layout with copper-tinted keyword
  pills. 🔍 suggest button fades in on row hover.
- **`.btn-primary`** is copper with `translate-y(-1px)` + copper
  shadow on hover (matches the public hero CTA). `.btn-ghost` white
  with copper on hover. `.btn-add` (dashed copper ring) for new
  Scenarios.
- **Modal** gains backdrop-blur, fade-in animation, big Sora h2, and
  a copper focus ring (3px box-shadow) on the textarea.
- **AI compose row** in the editor modal is copper-tinted with a
  copper-soft background.
- **Brand badge** in the topbar now shows the live binary version,
  sourced from a new `Version` field on `GET /api/project` and a
  `serve.BinaryVersion` package variable set from `cmd/reviewqa/`
  at process startup.

Bumped main.go version 0.68 → 0.69. Suite 539/539 green (no API
changes; pure visual).

## v0.68 — reviewqa serve Phase D: AI compose step bindings

Closes out the `reviewqa serve` arc. The Scenario editor gains an "AI
compose" row — type a free-form description of the Scenario you want
(natural language) and a destination URL, click Compose, and the
binary probes the page's DOM and asks the LLM to produce a valid
Scenario whose steps match the registered patterns and whose
assertions reference real DOM elements.

Backend:
- `internal/serve/compose.go` — `ComposeSteps(ctx, in)` orchestrates
  the probe + LLM call. The system prompt is composer-style but
  narrowed: it instructs the model to output exactly one Scenario
  using only the registered step patterns, with placeholders
  substituted from the supplied DOM landmarks (title / headings /
  links / forms / buttons).
- `extractScenarioBlock` strips any preamble or trailing chatter the
  model adds despite the strict prompt; the result is then validated
  through the existing `validateScenarioBlock`.
- Deterministic fallback when REVIEWQA_LLM is unset: emits a minimal
  Scenario that opens the landing page, navigates to the destination
  path, and asserts on the page's h1. Always parses cleanly.
- New endpoint `POST /api/compose-steps` with a 90s context timeout
  (LLM calls against local Ollama-shaped endpoints often need 20-30s).

The endpoint respects the same REVIEWQA_LLM convention as
`reviewqa probe`: `REVIEWQA_LLM=http://your-endpoint:11434` opts in;
REVIEWQA_MODEL overrides the model; unset → deterministic mode.

Frontend:
- The Scenario editor modal now has a copper-tinted "🤖 AI compose"
  row above the textarea. URL field pre-fills from the feature
  narrative when it carries a bare URL. Status pill shows the model
  used (or a fallback note) after each compose.

Tests: 5 new in `internal/serve/compose_test.go` (deterministic H1
extraction, fallback when no headings, block extraction strips
preamble + chatter, scenario-name normalization, empty-input
rejection). Full suite 539 / 539 green.

This closes the four-phase v0.65–v0.68 serve roadmap. Future work
(v0.69+) is iteration on UX from real usage, not new endpoints.

## v0.67 — reviewqa serve Phase C: locator suggestion from DOM

Per-step locator suggestion. The UI surfaces a 🔍 button next to each
step that contains a placeholder or quoted argument — clicking it
opens a modal where the user supplies the destination URL, the kind
of element (button / input / link / heading), and the hint text. The
binary fetches the page via the existing `probe.Fetch` (SSRF guards
+ 4 MiB cap inherited), parses the HTML into landmarks, and returns
a ranked list of Playwright locators.

Backend:
- New `internal/serve/dom.go` — `FetchAndParseDOM` extracts title,
  headings (h1-h6), forms with their inputs (label-aware via the
  document's `<label for=...>` registry), standalone inputs,
  buttons, and links.
- New `RankLocators(lm, kind, hint)` produces selector candidates
  with a similarity score (exact > substring > token-overlap).
  Caps at 10 results.
- Two new HTTP endpoints:
  - `POST /api/probe-dom` `{url, base?}` → full landmarks JSON.
  - `POST /api/locator-candidates` `{url, base?, kind, hint}` →
    ranked candidates with `selector`, `role`, `name`, `score`,
    `source`.

Selectors produced today:
- Buttons → `page.getByRole('button', { name: '...' })`
- Inputs → `getByLabel` / `getByPlaceholder` / `page.locator('[name=...]')` / `#id`
- Links → `getByRole('link', ...)` AND `a[href=...]` (both offered
  because generic link text — "Read more" — needs the href).
- Headings → `getByRole('heading', { level: N, name: '...' })`

Frontend:
- 🔍 suggest button on every step with `"..."` or `<placeholder>` in
  the text. Visible on row-hover so it doesn't fight for visual
  weight.
- Modal: URL / Kind / Hint fields (kind auto-guessed from the step's
  verb; URL pre-filled from the feature narrative when it carries a
  bare https:// URL). Probe button calls the endpoint; results show
  with score + name + selector. Copy button stages the selector to
  the system clipboard.

Tests: 10 new in `internal/serve/dom_test.go` (parsing title /
headings / forms-with-inputs / links / buttons; ranking for each
kind; relative-URL resolution). Full suite 534 / 534 green.

## v0.66 — reviewqa serve Phase B: Scenario CRUD

Mutation endpoints on top of v0.65's read-only viewer. Users can now
delete, edit, and append Scenarios from the browser UI. Each write is
preceded by a backup into `tests/e2e/.reviewqa-history/<ts>/` so an
accidental delete is recoverable.

- `DELETE /api/scenario?feature=<rel>&name=<scenario-name>` — splice
  the named Scenario out of the file (including its preceding @tag
  block). Returns `{"deleted":1}`.
- `PATCH /api/scenario?feature=<rel>&name=<scenario-name>` body
  `{"gherkin":"..."}` — replace the block. Server validates the new
  block parses as exactly one Scenario with ≥1 step; rejects with
  400 otherwise. Supports rename — the new Scenario name is returned.
- `POST /api/scenario?feature=<rel>` body `{"gherkin":"..."}` —
  append at EOF with a single blank line separator.
- `POST /api/validate-scenario` body `{"gherkin":"..."}` — pre-check
  without writing; returns `{"valid":true}` or `{"valid":false,
  "error":"..."}`. UI uses this for the modal's Validate button.

File I/O via atomic tmpfile + rename in the same directory. Pre-edit
backups land at `tests/e2e/.reviewqa-history/<UTC timestamp>/` so the
user can `cp` files back if the new Scenario was wrong.

Frontend additions to `internal/serve/web/`:
- Each Scenario gets Edit + Delete buttons.
- `+ New Scenario` button at the foot of each feature.
- Edit modal: textarea with raw Gherkin, Validate / Save / Cancel.
  Validate uses the new endpoint so the user sees the parser's
  exact rejection reason before committing.
- All mutations re-fetch + re-render automatically.

Tests: 10 new in `internal/serve/edit_test.go` (range finding,
delete, replace, append, invalid-block rejection, multi-scenario
rejection, plus HTTP-handler coverage for each verb). 524 / 524 green.

## v0.65 — reviewqa serve (Phase A: read-only project viewer)

New `reviewqa serve` subcommand. Opens a localhost HTTP server (default
`127.0.0.1:8765`) that loads an existing reviewqa-generated project and
renders a read-only browser UI for its Features, Scenarios, and
stakeholder docs. Auto-opens the user's default browser (skip with
`--no-browser`).

This is **Phase A** of the local-UI roadmap. Read-only — no mutations.
Phase B (Scenario CRUD), Phase C (locator suggestion from live DOM),
and Phase D (AI-composed step bindings from hand-written Gherkin) ship
as follow-up PRs.

- New package `internal/serve/` with hand-rolled Gherkin parser (no
  cucumber-go dependency), TypeScript step-defs regex parser, and an
  HTTP handler exposing `GET /api/project`, `GET /api/feature`,
  `GET /api/steps`, `GET /api/doc`. Localhost-only by request-origin
  enforcement; `--addr` overridable but the default is loopback.
- New `cmd/reviewqa/serve.go` wires the subcommand with `--workdir`,
  `--addr`, `--no-browser` flags.
- Frontend lives under `internal/serve/web/` and is embedded via
  `//go:embed`. Plain HTML + CSS + JS — no build step. CSS variables
  mirror the public site (copper / deep-water / ink + Sora / Inter /
  Fira Code) so the local UI looks native next to the marketing pages.
- Path-traversal defenses on `GET /api/feature` and `GET /api/doc`
  (rooted at workdir, `../` escape rejected with 400).
- 14 new tests in `internal/serve/` (Gherkin parser × 5, step-defs
  parser × 2, HTTP handler × 5, including path-traversal rejection).
- `cmd/reviewqa/main.go` version constant bumped 0.64 → 0.65.

Usage:
```bash
reviewqa serve --workdir ./examples/playwright-dev
```

## v0.64 — re-emit examples + drop version-caveat prose + AI badge

User feedback after v0.63: the "Output from the v0.59 binary — templates
remain current through v0.62" gymnastics was the wrong convention. The
website must show CURRENT output, not a multi-release explanation. The
"DGX" badge is vendor-specific; users may point `REVIEWQA_LLM` at any
OpenAI-compatible endpoint. And the depth-parity / live-execution /
composer-DOM narration in docs.html belongs in this changelog, not in
the docs page.

- All six committed examples re-emitted against the v0.64 binary.
  Counts unchanged (183 / 196 / 186 / 201 / 197 / 46) — v0.61's four
  template fixes apply automatically; v0.62's composer DOM validation
  filters out the F-1 mismatches the previous emission carried.
- New `scripts/refresh-examples.sh` codifies the build + probe + split
  flow with the awk split-pattern baked in. Run before tagging every
  release. CI-safe for the 4 deterministic examples; the 2 AI-on ones
  need a local `REVIEWQA_LLM` endpoint.
- web/index.html + web/docs.html + README.md + examples/README.md
  prose simplified everywhere to "v0.64 binary" — no more multi-release
  caveats, no more "introduced in vX.Y, current in vZ.W" labels.
- "DGX" badge → "AI" badge on the spritecloud + petstore cards. The
  directory names `examples/*-dgx/` stay (stable URLs) but every other
  surface (prose, docs section title, env var docs, env-var examples)
  is vendor-agnostic.
- Three feature callouts removed from web/docs.html: "Depth parity
  (v0.55–v0.57)", "Live execution validated (v0.61)", "Composer
  destination-DOM validation (v0.62)". Their content lives here.
- `cmd/reviewqa/main.go` version constant bumped 0.63 → 0.64.

## v0.63 — repo-wide consistency sweep + force gh-pages redeploy

Live https://spritecloud.github.io/reviewqa/ was showing **v0.23-era**
content because the pages.yml workflow did not fire for the v0.60 merge
(missed-push window during the v0.60/v0.61/v0.62 cluster on 2026-06-17)
and v0.61/v0.62 did not touch web/. Net effect: the live site was two
arcs behind, and the repo's web/ source itself was at v0.59-era prose.

This release does not change any code. It:

- Brings web/index.html and web/docs.html up to v0.62. Hero badge
  bumped from v0.59 to v0.62. "What's new in v0.61 + v0.62" callout
  added above the examples grid. docs.html gains two new callouts
  (live execution validated; composer destination-DOM validation).
- Brings README.md and examples/README.md up to v0.62. The committed
  example output remains v0.59-emitted — that's labelled explicitly
  ("committed at v0.59; current binary v0.62"). The deterministic
  templates beneath the examples ARE current through v0.62.
- Backfills CHANGELOG.md with the v0.61 and v0.62 entries that were
  shipped without changelog notes.
- Bumps cmd/reviewqa/main.go's source-default version constant from
  "0.1.0" to "0.62" so a locally-built binary's --version output
  matches the release line (release.yml still ldflags-overrides this).
- Triggers pages.yml via `gh workflow run pages.yml` so the live
  site picks up the new content even if the path-filter misfires
  again.

## v0.62 — composer destination-DOM validation (F-1 fix)

Adds `composer.ValidateAgainst(scenarios, journey)` after the existing
pattern-syntax `Validate` in the propose pipeline. The new pass walks
each composed scenario tracking the "current page" as the journey
progresses through link clicks / direct navigations, then drops any
scenario whose H1 or title assertion conflicts with the destination
metadata recorded in `Journey.Pages`.

Match is loose (case-insensitive substring either direction) so
real human-quality assertions like `the main heading reads "About"`
still pass when the page H1 is `About us`. Pages with no recorded
metadata pass through — the validator is strictly additive, it only
ever drops scenarios it can prove wrong.

Closes the F-1 finding from the v0.61 spritecloud run (3 of 66
composed scenarios asserted destination H1 text the page does not
render). Future generations against any site will silently drop
these instead of shipping broken specs.

7 new tests in `internal/composer/composer_destination_test.go`.
Full suite 502/502 green.

## v0.61 — execute spritecloud-com-dgx locally + 4 template fixes

First time the generated suite was actually executed against a real
production site (https://www.spritecloud.com). Until v0.61 reviewqa
had been validated only at the **emission layer** — Go tests asserted
templates rendered correctly, file counts were right, Scenarios
contained the expected blocks. The execution surfaced four template
bugs that compile cleanly but fail at runtime, all now fixed:

- **T-1** — `pw_package.tmpl` pinned `playwright-bdd@^7.4.0` which
  resolves to 7.5.0; that version imports
  `playwright/lib/common/configLoader.js`, removed in playwright
  1.51+. Every test died at config load. Bumped to `^9.0.0`.
- **T-2** — `gen.go::annotateQualityReport` prepended a C-style
  block comment to `.feature` files; Gherkin only allows `#` line
  comments. Added `TmplPlaywrightFeature` to the exclusion switch.
- **T-3** — `pw_mobile.tmpl` put `test.use({...devices[name]})` inside
  `test.describe()`. Playwright forbids this ("forces a new
  worker"). Refactored to `browser.newContext({...devices[name]})`
  per test — same coverage, no `test.use()` violation.
- **T-4** — `pw_steps.tmpl::submit()` grabbed the first submit-like
  button on the page. On spritecloud.com that's the disabled footer
  newsletter "Subscribe" — `click()` hung for 30s waiting for it to
  enable. Rewrote to skip disabled buttons and prefer buttons inside
  filled forms.

Real spritecloud findings (S category) — kept as sentinel candidates,
not template bugs: 5 footer social-icon links with no accessible name
(WCAG 2.4.4, 4.1.2); /guides page has zero `<h1>` tags (WCAG 1.3.1);
color-contrast violations on the homepage; unlabeled form inputs.

LLM-design issues (F category) — 3 of 66 composed scenarios asserted
against destination-page H1 text not actually rendered. Addressed in
v0.62.

Numbers after all four fixes: 32/40 mobile pass (8 fails all on the
/guides h1 S finding), 67/71 Gherkin pass (3 F-1, 1 tab-order S),
25/25 stub specs cleanly skip.

Run report: `examples/spritecloud-com-dgx/_run/v0.61-run-report.md`.

## v0.60 — doc sweep + refresh all 6 examples

User audit: "the project sustain certain consistency. README, GitHub
Pages, Docs and all are updated? They have any cognitive dissonance?
Have we run the full reviewqa locally using the DGX for all the
examples?"

Two Explore agents confirmed the dissonance. README claimed v0.23,
web/index.html hero said "One Go binary. v0.23.", web/docs.html
example matrix carried stale v0.58 numbers, and the four non-DGX
example directories were last refreshed at v0.30 — missing every
v0.41+ template family (a11y trio, integration stubs, mobile matrix,
edge family, etc.).

No code logic changed. The sweep does three things:

1. **Refreshed all 6 example projects against the v0.59 binary**:
     playwright-dev          39 → 183 files
     gohugo-io               42 → 196 files
     books-toscrape-com      37 → 186 files
     es-wikipedia-org-madrid 51 → 201 files
     spritecloud-com-dgx     197 (refresh; 66 Scenarios, 34 @llm-composed)
     petstore-swagger-io-dgx 46 (refresh; 108 contract test blocks)

2. **Rewrote the stale README sections** into a single coherent v0.59
   voice: hero, quick-start (probe / prompt / generate / scan / heal /
   ledger update / run-once), 10-layer taxonomy table with depth column,
   subcommand reference, environment-variable tables (LLM / probe /
   CI / healing), workflow examples. Dropped per-version cruft from
   v0.21/v0.23/v0.24/v0.25 that no longer matched the binary.

3. **Aligned web/index.html + web/docs.html**: hero version v0.23 → v0.59,
   examples grid 4 cards → 6 (added petstore-DGX), docs.html §05 matrix
   numbers refreshed (118 → 197, 41 → 46), examples/README.md table
   rebuilt against the v0.59 file counts.

Verification:
  - grep -r "v0\.\(23\|30\|34\)" README.md web/index.html web/docs.html examples/README.md
    → only intentional history markers remain.
  - Each of the 6 example directories has tests/e2e/integration-{db,cache,
    obs,auth}/ subdirs (v0.57 templates) — confirms the refresh hit every dir.
  - 495 tests still passing.

## v0.59 — a11y trio uncapped + deepened (1+2+2 → 5+5+5)

Two gaps the post-v0.58 audit surfaced:

- **A11y emitted on only half the crawled pages.** The per-page
  companions loop was capped at `coverage.FuzzCap()` (5 in standard
  mode). On a 30-page spritecloud.com crawl only the first 5 pages
  got the a11y trio (a11y / landmarks / keyboard). The other 25 were
  blind spots — wrong for cheap companions like axe-core.
- **A11y sub-templates themselves were shallow.** `pw_a11y` shipped
  one axe smoke; `pw_a11y_landmarks` two structural checks; 
  `pw_keyboard_nav` two interaction checks.

`internal/probe/probe.go::qualityCompanions` now splits the per-page
loop into two passes — Pass A (uncapped) emits the a11y trio on every
crawled page; Pass B (capped) keeps the heavy companions (visual /
perf / mobile multi-device / etc.) within budget.

Each a11y template now ships 5 tests:

  pw_a11y.tmpl                1 → 5
    @smoke           axe WCAG 2a/2aa/21a/21aa — serious+critical = 0
    @wcag-aa         WCAG 2.1 AA-only profile for targeted CI gates
    @color-contrast  most-violated WCAG rule standalone
    @aria-attrs      ARIA-attribute discipline (cat.aria rules)
    @form-labels     every form input has a programmatic label

  pw_a11y_landmarks.tmpl      2 → 5
    @smoke               single main / h1 / ≥1 nav
    @no-aria-hidden      no focusables inside aria-hidden subtrees
    @heading-hierarchy   heading order is sequential (no h1→h3 skips)
    @landmark-names      duplicate landmarks have accessible names
    @skip-link           skip-to-content affordance (WCAG 2.4.1)

  pw_keyboard_nav.tmpl        2 → 5
    @smoke           Tab cycles through first 10 focusables
    @focus-indicator  every focusable has a visible focus indicator
    @escape-dismiss  Escape closes a dialog AND returns focus
    @enter-space     Enter activates focused links/buttons
    @no-trap         Tab past last focusable wraps or exits

Effect on spritecloud-com-dgx: a11y subdir went 25 → 100 files
(30 pages × 3 sub-templates × 5 tests = **450 a11y test blocks**).
The example refresh + docs depth column reflect the new floor.

## v0.55 → v0.58 — taxonomy depth parity

User audit: "we touched all of the categories but not in the same depth
… ensure consistent level/depth for each category." Three layers
shipped 1–2 tests per emission while API/UI/Non-functional shipped
5–15. This arc closes the gap.

- **v0.55 — Contract depth.** `pw_contract.tmpl` 1 → 9 tests per
  endpoint (8 adversarial: wrong-method, oversized-query, invalid-json,
  unicode, sqli, xss, null-byte, rapid-burst). `pw_graphql_stub.tmpl`
  1 → 6 (empty-query, malformed, type-mismatch, deep-nested,
  bare-GET). `pw_webhook_stub.tmpl` 1 → 6 (replay, expired timestamp,
  wrong algorithm, truncated signature, tampered body). Path-only —
  schema-aware semantic negatives deferred.
- **v0.56 — Mobile depth.** `pw_mobile.tmpl` now iterates 4 devices
  (iPhone 13, Pixel 5, iPad Pro 11, Galaxy S9+) × 2 orientations =
  8 effective tests per page. `pw_touch.tmpl` adds pinch-zoom,
  scroll-momentum, tap-then-rotate (5 gesture families total).
- **v0.57 — Integration depth.** Four new per-kind scaffold templates
  emit unconditionally per origin:
  `pw_integration_{db,cache,obs,auth}_stub.tmpl`, each with 3
  `test.skip()` blocks documenting the wire-up TODOs. Total: 5
  integration stubs per origin (the v0.43 catch-all + 4 new), 15
  skipped blocks per origin documenting concrete scenarios.
- **v0.58 — Examples refresh + docs.** spritecloud-com-dgx 118 → 122
  files (51 Scenarios, 16 `@llm-composed`); petstore-swagger-io-dgx
  41 → 44 files with **108 contract-layer test blocks** (12 endpoints
  × 9 tests each). `web/docs.html` taxonomy table gained a
  "tests per emit" column; two new FAQ entries cover the depth-parity
  story and how integration stubs activate.

## v0.40 — bug sentinels + v1.0.0-rc.2

- `pw_sentinel.tmpl` — every "open" row in `tests/e2e/docs/findings.md`
  becomes a `test.fail()` spec under `tests/e2e/sentinels/`. Stays red
  while the bug is unfixed; flips to "unexpected pass" the moment the
  fix lands.
- `internal/ledger/sentinels.go::EmitSentinels` drives the emission;
  `parseLedger` re-parses the markdown table in cmd/reviewqa so the
  generate path picks them up alongside compat tests + integration
  items.
- CHANGELOG.md consolidated v0.31 → v0.40 history.
- Tag `v1.0.0-rc.2` — the depth gap from the honest assessment is
  meaningfully closed (per-journey density doubled + parameterized
  Outlines + stateful variants + interaction-state visual + keyboard
  + landmarks). Rename to "quail" still postponed.

## v0.39 — interaction-state visual + a11y depth

Three new per-page quality companions:
- `pw_visual_states.tmpl` — default / hover / focus screenshots of the
  primary CTA
- `pw_keyboard_nav.tmpl` — Tab through 10 focusables, assert
  reachability + focus indicator
- `pw_a11y_landmarks.tmpl` — single main, single h1, ≥1 nav, no
  focusables inside aria-hidden

`qualityCompanions` extended with a per-kind `suffix` so templates
sharing a subdir (a11y/visual) don't collide on filename.

## v0.38 — stateful + cross-journey Scenarios

- `@state:logged-in` / `@state:anonymous` variants when suite has
  authenticate journey
- `@kind:cross-journey` when suite has convert journey
- `Catalogue` threaded into journey items for suite-level introspection

## v0.37 — parameterized Scenario Outlines

- One Scenario Outline + 3 example rows per text-like input type
  (email / password / tel / url / number / text)
- `paramRowsFor` registry of deterministic value sweeps

## v0.36 — per-journey scenario depth

5 new conditional Scenario family templates: retry, boundary,
tab-order, overflow, empty-state, resume, back-button. Per-journey
average density doubled (2.1 → 5.7).

## v0.35 — composer response cache

- `internal/composer/cache.go` file-backed memo keyed by `(model, URL,
  kind, title, h1, links, pages)`
- Re-runs against the same site return cached scenarios in 0s
- Strictly opt-in via `REVIEWQA_LLM_CACHE=auto` or explicit path

## v0.34 — multi-model ladder

- `composer.Ladder` walks rungs in order; first to parse cleanly wins
- `@model:<id>` tag on every emitted scenario
- `REVIEWQA_LLM_LADDER=<m1>,<m2>,...` env opts in

## v0.33 — composer feedback loop

- `composer.LoadFeedback` reads `findings.md`; failed test titles
  embedded in the LLM prompt as "DO NOT re-propose" list

## v0.32 — step pattern vocabulary expansion

12 new Gherkin step patterns each backed by a step definition:
URL contains, page has at least N items, scroll, menu open/close,
focus, dropdown select, key press, wait, response status, scroll
into view.

## v0.31 — composer robustness

- Dirty-JSON sanitizer strips trailing commas, smart quotes, doubled
  commas
- Single retry with stricter prompt on parse failure
- `Journey.Pages` per-link destination metadata fixes the cross-page
  h1 mismatch
- Cross-journey dedup via `ScenarioKey`

## v0.30 — polish + cleanup + v1.0-rc

- Cyclomatic complexity refactor: `gen.templateLocation` switch
  (cyclo 75) → O(1) registry map (cyclo 2). Out of the top-5
  hotspot list.
- Stale-PR sweep: closed every remaining `reviewqa: tests for PR #N`
  dogfood follow-up.
- CHANGELOG.md (this file) consolidates the v0.19 → v0.30 history.

## v0.29 — Layer 9 data quality

- `dbt_schema.tmpl` — per-column tests (unique + not_null) routed by
  identity-column heuristic
- `pandera_conformance.tmpl` — two-case validate test
- `great_expectations.tmpl` — expect_column_to_exist + not_null suite

## v0.28 — mobile reach

- `pw_mobile.tmpl` — iPhone 13 emulation + touch
- `pw_deeplink.tmpl` — per-declared deep-link path
- `rn_happyflow.tmpl` — Detox scaffold for React Native
- `flutter_happyflow.tmpl` — flutter_test scaffold

## v0.27 — Layer 5 integration tests

- `reviewqa.yml` config file at repo root
- `internal/integration` package: `Load` + `EmitItems`
- Templates for DB / broker / cache / storage / search / auth +
  shared `_containers.ts` and `docker-compose.test.yml`

## v0.26 — finish deferred auto-emission

- TS extractor: DTO interfaces, classes-with-constructors,
  Redux/Pinia/Zustand/Vuex stores
- `plan.fanOutAspects` routes IsDTO / FrameworkHint="class" /
  StoreKind to the matching aspect templates

## v0.25 — LLM-driven scenario composer (opt-in)

- `internal/composer` package: `Propose` returns validated
  `[]ExtraScenario` constrained to the registered step patterns
- `--llm <url>` flag on probe + prompt; strictly opt-in, local-only
  (DGX netbird-private; CI runs see no LLM step)
- `qwen3-coder-next:latest` default model
- `pw_feature.tmpl` appends `@llm-composed` block below deterministic
  scenarios
- Cyclomatic refactor: `probe.runAllImpl` 150 lines → 10 lines + 5
  named helpers

## v0.24 — taxonomy gaps closed (URL + diff)

- Diff-mode auto-detection wired (property / validator / cron /
  event / email)
- API expansions (idempotency / pagination / versioning when OpenAPI)
- Schema compatibility on PR diff (OpenAPI / proto / AsyncAPI) via
  `internal/compat`
- Templates for state / constructor / parameterized registered

## v0.23 — visual / GraphQL / webhook / gRPC

- Visual regression baselines via Playwright `toHaveScreenshot`
- GraphQL contracts via introspection
- Webhook signature tests (OpenAPI + provider-pattern detection)
- gRPC contracts from `.proto` diffs (per streaming shape)

## v0.22 — broader taxonomy

- a11y (@axe-core/playwright), responsive, perf, security headers,
  health probes, observability headers, OpenAPI contract,
  i18n, cross-browser

## v0.21 — Gherkin-first

- `.feature` files become executable contracts via playwright-bdd
- `.spec.ts` happy-flow emission dropped; step defs in
  `tests/e2e/steps/reviewqa.steps.ts`

## v0.20 — Achilles Tier 2

- API contract testing per `<form action>`
- Coverage modes: breadth / standard / depth
- Bug-discovery ledger (`tests/e2e/docs/findings.md`)

## v0.19 — Steps API + priorities + catalogue

- `tests/e2e/lib/steps.ts` higher-level helper verbs
- `@priority:critical|standard|nice-to-have` tags
- `tests/e2e/docs/test-catalogue.md` + `summary.html`
- `prompt --evidence` runs + ZIPs artifacts
- Browser-mode DOM snapshots under `tests/e2e/_dom/`
