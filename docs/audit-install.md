# Quail Audit — one-file install for external repos

Two steps to wire up a PR-comment "please audit" flow on any repo.

## 1. Invite the collaborator

Add [`@spritecloud-quail`](https://github.com/spritecloud-quail) as a
collaborator (Settings → Collaborators → Add people). Write access is
enough — the identity is what posts audit comments on your PRs.

## 2. Drop this file

Save as `.github/workflows/quail-audit.yml` in your repo:

```yaml
name: quail-audit
on:
  pull_request:
    types: [opened, synchronize, reopened]
  issue_comment:
    types: [created]

jobs:
  audit:
    # Auto-audit every PR + on-demand `/quail audit` slash command
    if: >
      github.event_name == 'pull_request' ||
      (github.event.issue.pull_request && contains(github.event.comment.body, '/quail audit'))
    uses: spriteCloud/quail-review/.github/workflows/audit.yml@v1
    with:
      target-url: https://staging.example.com   # your SUT
      pass-rate-threshold: 80                    # % passing = green
    permissions:
      contents: read
      pull-requests: write
```

That's it. Every PR gets audited; any collaborator can also comment
`/quail audit` on a PR to re-run on demand.

## What it does

- Checks out the PR head
- Installs Playwright + browsers
- Runs `npx playwright test --grep @smoke` against `target-url`
- Computes pass rate
- Comments the ratio on the PR
- Fails the check if pass rate drops below `pass-rate-threshold`

## Customising the pool

Default grep is `@smoke`. Override with `grep-tag` to select a different
Playwright tag:

```yaml
    with:
      target-url: https://staging.example.com
      grep-tag: '@critical'
```
