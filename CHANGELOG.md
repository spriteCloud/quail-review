# Changelog

reviewqa's release-by-release history. v0.19 → v0.30 was the
taxonomy-closure arc that took the framework from a single-Playwright
happy-flow generator to a 10-layer deterministic test author.

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
