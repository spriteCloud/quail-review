# spritecloud.com — DGX-composed example

The complete output of running the v0.34 binary against
`https://www.spritecloud.com` with the LLM scenario composer **enabled**
against a local Ollama on the spriteCloud DGX:

```bash
REVIEWQA_LLM=http://100.82.34.115:11434 \
  reviewqa probe --url https://www.spritecloud.com --dry-run
```

Run completed 2026-06-17 at ~13:30 UTC. The DGX is reachable only
through Netbird; consumers without Netbird can run the same probe
against any OpenAI-compatible local endpoint.

## What's different from the LLM-off matrix examples

The four sites under [`../`](../) (playwright-dev, gohugo-io, books-toscrape-com,
es-wikipedia-org-madrid) were generated with the LLM composer
**disabled** — they're the deterministic baseline. This directory
adds the LLM-composed layer on top of the same deterministic surface.

| | LLM-off (matrix examples) | LLM-on (this directory) |
|---|---|---|
| Files | 30–46 | 45 |
| Gherkin journeys | 3–4 per site | 9 (spritecloud has more nav surface) |
| Deterministic Scenarios | 2-3 per journey | 2-3 per journey (unchanged) |
| LLM-composed Scenarios | 0 | **17 across 7 of 9 journeys** |
| Tags on LLM Scenarios | n/a | `@llm-composed @kind:edge|variant|happy|regression @model:qwen3-coder-next-latest` |

## Per-journey breakdown

| Feature file | Deterministic | LLM-composed |
|---|---|---|
| `convert.feature` | 2 | 3 |
| `research-case-study-grandvision.feature` | 1 | 3 |
| `research-case-study-ecomm-platform.feature` | 1 | 1 |
| `browse-about-us.feature` | 1 | 2 |
| `explore-contact.feature` | 1 | 0 |
| `explore-cybersecurity.feature` | 1 | 2 |
| `explore-performance-testing.feature` | 1 | 3 |
| `read-style-guide.feature` | 1 | 0 |
| `read-book-performance-test.feature` | 1 | 3 |
| **Total** | **10** | **17** |

Two journeys got 0 LLM extras — the composer was conservative on
those page shapes. v0.33's bug-discovery-ledger feedback will reduce
this further once the suite has accumulated run-history.

## v0.32 vocabulary in action

The LLM composed Scenarios use multiple v0.32 step patterns the
deterministic templates never emit. See e.g.
[`features/spritecloud-com-convert.feature`](./tests/e2e/features/spritecloud-com-convert.feature):

```gherkin
@journey:convert @priority:critical @llm-composed @kind:variant @model:qwen3-coder-next-latest
Scenario: form field correctly reflects entered value
  Given I am on the landing page
  When I focus the "email" field
  When I enter "test@example.com" into the "email" field
  Then the "email" field has the value "test@example.com"

@journey:convert @priority:critical @llm-composed @kind:variant @model:qwen3-coder-next-latest
Scenario: form submission redirects to contact page
  Given I am on the landing page
  When I enter "contact@example.com" into the "email" field
  When I submit the form
  Then the URL contains "/contact"
```

`I focus the "<field>" field`, `the "<field>" field has the value
"<v>"`, and `the URL contains "<fragment>"` are all v0.32 additions.

## Reports inside the suite

Each probe emits stakeholder docs under `tests/e2e/docs/`:

- [`test-catalogue.md`](./tests/e2e/docs/test-catalogue.md) — every
  page crawled, every journey identified, priority, step chain
- [`summary.html`](./tests/e2e/docs/summary.html) — branded HTML deck
  with priority-mix bar; open in a browser
- [`findings.md`](./tests/e2e/docs/findings.md) — seeded empty;
  consumers populate by running `reviewqa ledger update --report …`

## Filter at runtime

```bash
# strict CI: skip LLM-composed scenarios (deterministic only)
npx playwright test --grep-invert @llm-composed

# explore the LLM additions:
npx playwright test --grep @llm-composed

# only critical journeys:
npx playwright test --grep @priority:critical

# only what one specific model produced:
npx playwright test --grep @model:qwen3-coder-next-latest
```

## Composer hardening logs from this run

The v0.31 dirty-JSON sanitizer + retry kept this run clean — zero
parse failures across all 9 journey calls (vs the 22% failure rate
observed before v0.31 hardening). The humanize layer's
structure check fell back to deterministic on 3 specs, as designed.
