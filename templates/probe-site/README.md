# probe-site starter

Minimal setup to run [quail-review](https://github.com/spriteCloud/quail-review)
against a live URL on a schedule. No source code or PR diff required.

## Three steps

1. Copy [`.github/workflows/quail-probe.yml`](./.github/workflows/quail-probe.yml)
   into your repo at the same path.
2. Set a repo variable `QUAIL_TARGET_URL` to the URL you want probed
   (Settings → Secrets and variables → Actions → Variables).
3. *(Optional)* Add `OPENAI_API_KEY` as a secret to enable
   `@llm-composed` Scenarios. Leave it unset for a deterministic-only
   run.

That's it. The first run opens a PR with a full `tests/e2e/` Playwright +
Gherkin suite — see [`examples/`](../../examples/) for what it looks
like across six reference sites.

## Trigger it now

```
gh workflow run quail-probe.yml -f url=https://your-site.example
```

Or wait for the nightly cron (`17 3 * * *`).

## Tuning

All knobs live in the workflow file itself — open it, the comments name
every dial. Common ones:

- `vars.OPENAI_BASE_URL` — point at Ollama / vLLM / DGX-hosted endpoint.
- `vars.QUAIL_MODEL` — model id.
- `schedule.cron` — change the cadence.
- **`kinds` / `exclude-kinds`** on dispatch — narrow the emitted
  taxonomy. Comma-list. Vocabulary: `journey, a11y, perf, visual,
  security, contract, health, observability, i18n, network, storage,
  print, mobile, responsive, touch, race, fuzz, webhook, graphql,
  auth-expiry, history-depth, clipboard, iframe, date-edges,
  file-upload, deeplink, http-chains, api, integration, grpc, compat,
  unit, pwa, locale, jobs`. Examples: `kinds: a11y` (accessibility
  only), `exclude-kinds: mobile,integration,touch,race` (drop the
  heaviest layers).
