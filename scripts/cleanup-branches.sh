#!/usr/bin/env bash
# cleanup-branches.sh — repo housekeeping for end-of-day wrap-up.
#
# Run from the repo root, on `main`, AFTER a release PR has merged.
# Three passes:
#   1. Delete remote branches whose tip is reachable from origin/main
#      (skips main itself, and any HEAD pointer).
#   2. Delete local merged branches (skips main and current HEAD).
#   3. Close stale open PRs / issues (default cutoffs: 30 days for PRs,
#      60 days for issues). Override via STALE_PR_DAYS / STALE_ISSUE_DAYS.
#
# Usage:
#   ./scripts/cleanup-branches.sh         # default cutoffs, all three passes
#   DRY_RUN=1 ./scripts/cleanup-branches.sh   # log only, change nothing
#
# Requires: git, gh (logged in).

set -euo pipefail

STALE_PR_DAYS=${STALE_PR_DAYS:-30}
STALE_ISSUE_DAYS=${STALE_ISSUE_DAYS:-60}
DRY_RUN=${DRY_RUN:-0}

run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "  [dry-run] $*"
  else
    "$@"
  fi
}

echo "==> Fetching + pruning remote refs"
git fetch --prune origin

echo "==> Deleting merged remote branches"
deleted_remote=0
while read -r ref; do
  # Format from `git branch -r --merged` is "  origin/<name>"; trim it.
  ref=$(echo "$ref" | sed 's|^\s*||')
  # Skip protected refs.
  case "$ref" in
    origin/main|origin/HEAD*|"") continue ;;
  esac
  name=${ref#origin/}
  echo "  - deleting $name"
  run git push origin --delete "$name" 2>/dev/null || echo "    (failed; may already be gone)"
  deleted_remote=$((deleted_remote + 1))
done < <(git branch -r --merged origin/main)
echo "==> Deleted $deleted_remote remote branches"

echo "==> Deleting merged local branches"
deleted_local=0
while read -r b; do
  b=$(echo "$b" | sed 's|^\s*||')
  case "$b" in
    main|"* "*|"") continue ;;
  esac
  # Defensive: --merged into main only.
  if git branch --merged main | grep -qE "^\s+$b\$"; then
    echo "  - deleting local $b"
    run git branch -d "$b" 2>/dev/null || echo "    (failed; not fully merged?)"
    deleted_local=$((deleted_local + 1))
  fi
done < <(git branch --merged main)
echo "==> Deleted $deleted_local local branches"

echo "==> Closing stale open PRs (older than $STALE_PR_DAYS days)"
pr_cutoff=$(date -u -d "$STALE_PR_DAYS days ago" --iso-8601=seconds)
stale_prs=$(gh pr list --state open --limit 100 --json number,updatedAt \
  --jq ".[] | select(.updatedAt < \"$pr_cutoff\") | .number")
if [[ -z "$stale_prs" ]]; then
  echo "  (no stale PRs)"
else
  while read -r n; do
    [[ -z "$n" ]] && continue
    echo "  - closing PR #$n"
    run gh pr close "$n" --comment "Closing as stale per repo cleanup. Reopen if still relevant."
  done <<< "$stale_prs"
fi

echo "==> Closing stale open issues (older than $STALE_ISSUE_DAYS days)"
issue_cutoff=$(date -u -d "$STALE_ISSUE_DAYS days ago" --iso-8601=seconds)
stale_issues=$(gh issue list --state open --limit 100 --json number,updatedAt \
  --jq ".[] | select(.updatedAt < \"$issue_cutoff\") | .number")
if [[ -z "$stale_issues" ]]; then
  echo "  (no stale issues)"
else
  while read -r n; do
    [[ -z "$n" ]] && continue
    echo "  - closing issue #$n"
    run gh issue close "$n" --comment "Closing as stale per repo cleanup. Reopen if still relevant."
  done <<< "$stale_issues"
fi

echo "==> Done."
