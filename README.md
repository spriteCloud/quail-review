# reviewqa

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
reviewqa scan --pr 42            # dry-run
reviewqa generate --pr 42        # opens follow-up PR
reviewqa heal --pr 42 --report playwright-report.json
```

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

| Var | Default | Purpose |
|---|---|---|
| `GITHUB_TOKEN` / `REVIEWQA_GITHUB_TOKEN` | — | API auth |
| `GITHUB_REPOSITORY` | from event | `owner/name` |
| `REVIEWQA_PR` | from event | PR number override |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | inference endpoint |
| `OPENAI_API_KEY` | — | inference auth; empty = no LLM |
| `REVIEWQA_MODEL` | `gpt-4o-mini` | chat-completions model |
| `REVIEWQA_LLM_TIMEOUT` | `20s` | per-file LLM timeout |
| `REVIEWQA_LLM_TOKEN_CAP` | `600` | max_tokens per LLM call |
| `REVIEWQA_HEAL_MODE` | `on-failure` | `on-failure` \| `proactive` \| `off` |
| `REVIEWQA_PLAYWRIGHT_REPORT` | `playwright-report.json` | report path |
| `REVIEWQA_ALLOW_DIFF_TO_LLM` | `0` | send PR diff to LLM (off by default) |
| `REVIEWQA_BRANCH_PREFIX` | `reviewqa` | created branch prefix |
| `REVIEWQA_WORKDIR` | `.` | repo working dir |
| `REVIEWQA_LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

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
