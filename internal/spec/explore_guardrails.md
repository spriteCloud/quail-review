# Exploratory Mode — AI Guardrails Spec

> **Version:** 1.0.0 | Embedded in the quail binary via `//go:embed`.
> This file is the single source of truth for what the LLM is and is not
> allowed to do when `quail explore` runs. The engine validates every LLM
> response against this spec before accepting output; invalid output is
> silently dropped and the deterministic fallback is used instead.

---

## 1. Purpose

`quail explore` is an adversarial probing command. Unlike `probe` (which
builds coverage from happy paths + standard negatives), `explore` hunts
for real bugs through targeted, adversarial interaction.

The LLM has **more latitude** here than in `probe` / `generate` — but it
is still a constrained second layer on top of a deterministic engine. The
deterministic layer always runs first and always produces output. The LLM
layer extends it; it cannot replace or contradict it.

---

## 2. What the LLM IS allowed to do

The LLM may perform exactly **three** operations in explore mode:

### 2.1 Attack-plan (`op: attack-plan`)

Given a page snapshot (URL, visible text, discovered form fields, links,
interactive elements), the LLM may **propose additional attack targets** —
specific element × attack-category combinations it believes are worth
probing beyond the deterministic baseline.

**Constraints:**
- Each proposed target MUST reference a discovered element by its
  deterministic ID (e.g., `#submit-btn`, `[data-testid="email"]`). The
  LLM may NOT invent selectors that were not in the page snapshot.
- Each proposed target MUST cite exactly one category from the
  **Attack-Category Registry** (§4). Targets citing unlisted categories
  are dropped.
- Maximum **10 additional targets** per page.
- Output format: see §5.1.

### 2.2 Compose (`op: compose`)

Given a confirmed anomaly (an observed deviation from expected behaviour
with a reproduction trace), the LLM may **compose a Gherkin Scenario**
that formalises the bug as a reproducible test.

**Constraints:**
- The scenario MUST use only step patterns from the **Step Vocabulary**
  (§6). Steps outside the vocabulary are dropped.
- The scenario MUST include an `@exploratory` tag and a `@severity-<X>`
  tag where `<X>` is one of `critical | high | medium | low | info`.
- The scenario MUST reference the exact URL and element the anomaly was
  found on (no paraphrasing or generalisation).
- The composed scenario is validated against the deterministic test file:
  the number of `Given/When/Then` steps cannot exceed the reproduction
  trace length by more than 2. If it does, the scenario is dropped and
  the trace is emitted verbatim.
- Maximum **1 scenario per confirmed anomaly**.

### 2.3 Classify (`op: classify`)

Given a list of raw anomalies from the deterministic probing pass, the
LLM may **assign a severity** to each and write a one-line `observed:`
summary (≤ 120 characters).

**Constraints:**
- Severity MUST be one of `critical | high | medium | low | info` (§7).
- The LLM may NOT change the `expected:` value set by the deterministic
  engine.
- The LLM may NOT mark a finding as `wontfix` or `deferred`.
- If the LLM returns a severity outside the enum or a summary longer than
  120 characters, the deterministic default (`medium` / truncated trace
  description) is used instead.

---

## 3. What the LLM is FORBIDDEN from doing

The following are hard constraints. The engine enforces them
programmatically; they are stated here for prompt transparency.

| Forbidden action | Enforcement |
|---|---|
| Invent selectors / locators not in the page snapshot | Target dropped |
| Hallucinate URLs not discovered by the crawler | Target dropped |
| Propose categories not in the Attack-Category Registry (§4) | Target dropped |
| Use step patterns not in the Step Vocabulary (§6) | Step dropped; if scenario has < 3 remaining steps it is dropped entirely |
| Set `expected:` to anything other than the deterministic engine's value | Value overwritten |
| Propose more than 10 targets per page | Extras truncated (highest-confidence first) |
| Propose more than 1 scenario per confirmed anomaly | Extras dropped |
| Assign severity outside `critical \| high \| medium \| low \| info` | Reset to `medium` |
| Produce free-form prose outside the structured output format (§5) | Entire response dropped; deterministic fallback used |
| Recommend deferring or ignoring a finding | Instruction ignored |
| Reference PR / commit diff **content** (require `QUAIL_ALLOW_DIFF_TO_LLM=1`) | Diff content redacted |
| Invent selectors from "recently changed files" paths (§11) | Target dropped |

---

## 4. Attack-Category Registry

These are the only categories the LLM may cite. The deterministic engine
applies **all** of them to every discovered element; the LLM may add
extra *targets* from these categories, not extra categories.

| ID | Category | Description |
|---|---|---|
| `boundary` | Boundary inputs | Empty submit, max-length strings, zero/negative numbers, whitespace-only, unicode edge cases |
| `injection` | Injection probes | `<script>alert(1)</script>`, `'"; DROP TABLE`, `{{7*7}}`, `../../../etc/passwd` — surface-level probes, no exploitation |
| `state-corrupt` | State corruption | Browser back after submit, refresh mid-flow, re-submit completed form, navigate away and return |
| `race` | Race conditions | Rapid repeated clicks, interact during loading spinners, double-submit, type during autocomplete debounce |
| `auth` | Auth & access control | Direct URL access without session, manipulate URL params (`id=`, `user=`), expired/missing token behaviour |
| `data-edge` | Data edge cases | Empty list, single item, pagination boundary (last page + 1), long text overflow, zero-result search, null/undefined fields |
| `cross-feature` | Cross-feature state | Edit in tab A, check tab B; apply filter then navigate back; change setting mid-flow |
| `flow-interrupt` | Interrupted flows | Start multi-step flow, abandon at each step, resume — does state survive or corrupt? |
| `sequence` | Out-of-order operations | Skip steps via direct URL, submit step N before step N-1, delete record being edited |
| `role-switch` | Role / session transitions | Log out mid-flow, re-log as different role, check stale permission survival |
| `upstream-dep` | Upstream dependency failure | Reference deleted resource, filter value removed from source, linked object returns 404 |
| `cumulative` | Cumulative state | Repeat action 20+ times — memory leak, stacked toasts, DOM growth, filter stack explosion |

---

## 5. Output Format

The LLM MUST respond with a single JSON object. Any other format causes
the entire response to be dropped.

### 5.1 Attack-plan response

```json
{
  "op": "attack-plan",
  "page_url": "https://example.com/login",
  "targets": [
    {
      "selector": "[data-testid=\"email\"]",
      "category": "injection",
      "rationale": "Email field rendered inside innerHTML — XSS surface"
    },
    {
      "selector": "#submit-btn",
      "category": "race",
      "rationale": "No visible disabled state during submission"
    }
  ]
}
```

`rationale` is ≤ 80 characters. Longer rationales are truncated. If
`selector` is not in the page snapshot, the target is dropped.

### 5.2 Compose response

```json
{
  "op": "compose",
  "anomaly_id": "explore-login-003",
  "scenario": {
    "title": "Login form accepts XSS payload in email field",
    "tags": ["@exploratory", "@severity-high", "@injection"],
    "steps": [
      "Given I navigate to '/login'",
      "When I fill in '[data-testid=\"email\"]' with '<script>alert(1)</script>'",
      "And I fill in '[data-testid=\"password\"]' with 'valid-password'",
      "And I click '[data-testid=\"submit\"]'",
      "Then I should not see '<script>' executed in the page"
    ]
  }
}
```

### 5.3 Classify response

```json
{
  "op": "classify",
  "classifications": [
    {
      "anomaly_id": "explore-login-001",
      "severity": "high",
      "observed": "Form submits silently with empty required email field; no validation error shown"
    },
    {
      "anomaly_id": "explore-login-002",
      "severity": "low",
      "observed": "Password field shows plaintext on rapid triple-click then blur"
    }
  ]
}
```

---

## 6. Step Vocabulary

The LLM may only use these step patterns in composed scenarios. Anything
else is stripped. Placeholders in `<angle brackets>` are required; those
in `[square brackets]` are optional.

### Navigation
- `Given I navigate to '<path>'`
- `Given I am logged in as '<role>'`
- `Given I am not authenticated`
- `When I reload the page`
- `When I press the browser back button`
- `When I open a new tab and navigate to '<path>'`

### Interaction
- `When I click '<selector>'`
- `When I fill in '<selector>' with '<value>'`
- `When I clear '<selector>'`
- `When I select '<option>' from '<selector>'`
- `When I upload '<filename>' to '<selector>'`
- `When I click '<selector>' <N> times in rapid succession`
- `When I submit the form`
- `When I wait <N> seconds`

### Assertions
- `Then I should see '<text>'`
- `Then I should not see '<text>'`
- `Then I should see an error message`
- `Then I should not see an error message`
- `Then the page title should be '<title>'`
- `Then the URL should contain '<fragment>'`
- `Then the URL should not contain '<fragment>'`
- `Then I should be redirected to '<path>'`
- `Then the response status should be <code>`
- `Then '<selector>' should be visible`
- `Then '<selector>' should not be visible`
- `Then '<selector>' should contain '<value>'`
- `Then I should not see '<script>' executed in the page`
- `Then no console errors should be present`
- `Then the network request to '<pattern>' should return status <code>`

### Multi-step / state
- `And I open a second browser context`
- `And I switch to the second browser context`
- `And I log out`
- `And I log in again as '<role>'`

---

## 7. Severity Taxonomy

The LLM uses this taxonomy when classifying findings. The engine enforces
the five-value enum; rationale guidance is for prompt calibration only.

| Severity | When to assign |
|---|---|
| `critical` | Security vulnerability, auth bypass, data leakage, IDOR, XSS that executes, CSRF, broken primary flow for all users |
| `high` | User cannot complete an intended action without a non-obvious workaround; core feature broken or silently fails |
| `medium` | User notices something wrong but can continue; data-correctness issue in non-critical path |
| `low` | Minor inconsistency, cosmetic defect, UX nit; typical user would not notice |
| `info` | Found only in DOM / network inspector; invisible to end users (hidden elements, metadata mismatches) |

**Escalation rule:** If the deterministic engine already assigned a severity,
the LLM may only escalate (raise severity), never downgrade. The engine
always wins on downgrades.

---

## 8. Validation Rules (engine-enforced)

After every LLM response the engine runs these checks in order. Any
failure triggers the stated fallback — the check loop stops and the next
anomaly is processed.

1. **JSON parse** — response must be valid JSON. Fallback: drop response.
2. **Op check** — `op` must be one of `attack-plan | compose | classify`. Fallback: drop response.
3. **Selector existence** (`attack-plan`) — every `selector` must appear verbatim in the page snapshot element list. Fallback: drop that target.
4. **Category check** (`attack-plan`) — every `category` must be in §4. Fallback: drop that target.
5. **Step vocabulary check** (`compose`) — each step must match a pattern in §6. Fallback: drop invalid steps; if < 3 steps remain, drop scenario.
6. **Tag check** (`compose`) — scenario must have `@exploratory` and one `@severity-<X>`. Fallback: drop scenario, emit trace verbatim.
7. **Step count check** (`compose`) — step count must not exceed reproduction trace length + 2. Fallback: drop scenario, emit trace verbatim.
8. **Severity enum** (`classify`) — must be one of the five values in §7. Fallback: assign `medium`.
9. **Summary length** (`classify`) — `observed:` must be ≤ 120 chars. Fallback: truncate.
10. **No-hallucination check** — any URL in the response not in the crawl manifest is flagged and redacted.

---

## 9. Findings Output Format

Every confirmed finding is written to the findings ledger
(`tests/e2e/docs/exploratory-findings.md` by default, overridable via
`--findings`). The engine writes the ledger; the LLM may only populate
`severity:` and `observed:` fields (via the `classify` op).

```markdown
#### <FINDING-ID>

- **page:** <url>
- **element:** <selector or "n/a" for flow-level findings>
- **category:** <attack category from §4>
- **expected:** <deterministic engine description of expected behaviour>
- **observed:** <LLM-provided or deterministic fallback, ≤ 120 chars>
- **severity:** <critical|high|medium|low|info>
- **evidence:** <relative path to screenshot or network capture>
- **repro:** <inline Gherkin scenario or reference to generated .feature file>
- **status:** open
```

`FINDING-ID` format: `explore-<page-slug>-<NNN>` (zero-padded, sequential
within the run). The engine assigns IDs; the LLM never sets them.

---

## 10. Prompt Template

The engine uses this template verbatim. Variable substitution is the only
modification permitted — the system instructions and constraints are never
altered by the engine or by flags.

```
You are a QA security analyst performing adversarial testing of a web application.

RULES (non-negotiable):
- Respond with a single JSON object only. No prose, no markdown, no code fences.
- You may only reference selectors from the DISCOVERED ELEMENTS list below.
- You may only use attack categories from the ATTACK-CATEGORY REGISTRY.
- You may only use step patterns from the STEP VOCABULARY.
- Do not invent URLs, selectors, endpoints, or behaviours not listed below.
- Maximum 10 targets (attack-plan) or 1 scenario (compose) per response.

OPERATION: {{.Op}}

PAGE URL: {{.PageURL}}

DISCOVERED ELEMENTS:
{{range .Elements}}- {{.Selector}} (type: {{.Type}}, label: {{.Label}})
{{end}}

{{if .ChangedPaths}}
RECENTLY CHANGED FILES (paths only — use to prioritise attack-plan targets,
do NOT invent selectors from these):
{{range .ChangedPaths}}- {{.}}
{{end}}
{{end}}

{{if .AnomalyList}}
ANOMALIES TO CLASSIFY:
{{range .AnomalyList}}- ID: {{.ID}} | expected: {{.Expected}} | trace: {{.Trace}}
{{end}}
{{end}}

{{if .ReproTrace}}
REPRODUCTION TRACE (for compose):
Anomaly ID: {{.AnomalyID}}
Steps observed:
{{range .ReproTrace}}- {{.}}
{{end}}
{{end}}

Respond now with a single JSON object matching the format for operation "{{.Op}}".
```

No additional instructions, examples, or context may be injected into
this prompt. Diff **content** is NEVER included unless
`QUAIL_ALLOW_DIFF_TO_LLM=1` is set. Diff **paths** are appended via the
`RECENTLY CHANGED FILES` block whenever the runner has a change context;
see §11.

---

## 11. Change-aware context

When `quail explore` runs in change-aware mode (the default whenever a PR
number is available via `$GITHUB_EVENT_PATH`, `--pr`, or `$QUAIL_PR`; or
when a local `git diff HEAD~1..HEAD` returns non-empty), the engine
forwards the list of changed file paths — **and only the paths** — to the
LLM in the `RECENTLY CHANGED FILES` block of §10.

Rules (engine-enforced):

1. **Paths only, never content.** File contents, hunk text, added/removed
   line ranges, and old/new blobs are never included. Only the value of
   `diff.File.Path` (and `diff.File.OldPath` when a rename is present) is
   emitted. `QUAIL_ALLOW_DIFF_TO_LLM=1` remains the separate gate for
   diff *content*; it is off by default and orthogonal to change-aware
   mode.
2. **Prioritisation only, never invention.** The LLM may use paths to
   decide *which discovered selectors to target first* in the
   `attack-plan` operation. It may not derive new selectors, URLs, or
   endpoints from these paths. The selector-existence check in §8.3
   continues to apply — any selector in the LLM response that is not in
   the deterministic page snapshot is dropped, regardless of whether a
   changed path suggested it.
3. **Deterministic layer is unaffected.** Every registered attack
   category still runs against every discovered element on every page.
   The diff-derived path list is a *hint* to the LLM, not a filter on
   the deterministic engine.
4. **Missing context is silent.** No PR, no local git history, no diff
   — the block is omitted entirely and the run proceeds identically to
   an unchanged-baseline run. There is no error, no warning, no fallback
   behaviour.
