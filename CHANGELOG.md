# Changelog

reviewqa's release-by-release history. v0.19 → v0.30 was the
taxonomy-closure arc that took the framework from a single-Playwright
happy-flow generator to a 10-layer deterministic test author.

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
