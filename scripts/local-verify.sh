#!/usr/bin/env bash
# Local end-to-end verify for the four quail modes.
#
# MODE=probe (default) — generate path:
#   1. Build quail from working tree.
#   2. Snapshot demo repo into a workDir.
#   3. Run `quail probe --url X --local` (writes specs to workDir).
#   4. `npx playwright test --list --grep @smoke` — TS compile check.
#   Same failure signal as CI verify, ~90s instead of ~15min.
#
# MODE=heal — heal-on-failure path:
#   1. Build quail.
#   2. Copy the demo's broken-locator.heal-demo.spec.ts + a corpus spec
#      into a workDir.
#   3. Emit a synthetic playwright-report.json listing the broken locator
#      failure.
#   4. `quail heal --report=... --dry-run` — prints proposed edits.
#   5. Assert at least one edit was proposed and the replacement is a
#      role/label/text locator (not the same testid).
#
# Env overrides:
#   URL      target-url the probe visits           (default: spritecloud)
#   KINDS    QUAIL_KINDS filter                    (default: journey,touch,responsive,visual)
#   OLLAMA   http://.../v1 base for the LLM        (default: DGX via netbird)
#   MODEL    model id                              (default: qwen3-coder-next:latest)
#   NO_TSC=1 skip the playwright --list step (probe mode only)

set -euo pipefail

MODE="${MODE:-probe}"
URL="${URL:-https://www.spritecloud.com}"
KINDS="${KINDS:-journey,touch,responsive,visual}"
OLLAMA="${OLLAMA:-http://100.82.34.115:11434/v1}"
MODEL="${MODEL:-qwen3-coder-next:latest}"

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
DEMO_SRC="${REPO_ROOT}/../quail-e2e-demo"
WORKDIR=$(mktemp -d -t quail-verify-XXXX)
BIN="${WORKDIR}/quail"

echo "==> Build quail from working tree"
(cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/quail)

case "$MODE" in
  probe)
    echo "==> Snapshot demo tree into $WORKDIR/demo"
    mkdir -p "$WORKDIR/demo"
    rsync -a --exclude .git --exclude node_modules --exclude .quail-history \
      "$DEMO_SRC/" "$WORKDIR/demo/"

    echo "==> Probe $URL (kinds: $KINDS)"
    cd "$WORKDIR/demo"
    env \
      QUAIL_TARGET_URLS="$URL" \
      QUAIL_KINDS="$KINDS" \
      OPENAI_BASE_URL="${OLLAMA}" \
      OPENAI_API_KEY=ollama \
      QUAIL_MODEL="$MODEL" \
      "$BIN" probe --url "$URL" --local 2>&1 | tee "$WORKDIR/probe.log" | tail -40

    if [[ "${NO_TSC:-0}" == "1" ]]; then
      echo "==> Skipping playwright --list (NO_TSC=1)"
      echo "==> Emitted specs under $WORKDIR/demo/tests/e2e/"
      exit 0
    fi

    echo "==> npm install (silent)"
    npm install --no-audit --no-fund --silent >/dev/null

    echo "==> playwright --list @smoke (compile-check)"
    if ! npx playwright test --list --grep @smoke >"$WORKDIR/list.log" 2>&1; then
      echo "FAIL: playwright rejected the emitted suite"
      echo
      cat "$WORKDIR/list.log"
      echo
      echo "workDir kept for inspection: $WORKDIR"
      exit 1
    fi

    TESTS=$(grep -c ' › ' "$WORKDIR/list.log" || true)
    echo "OK: $TESTS test cases listed"
    echo "workDir: $WORKDIR"
    ;;

  heal)
    echo "==> Prep heal fixture at $WORKDIR/demo"
    mkdir -p "$WORKDIR/demo/tests/e2e/heal-demo"
    mkdir -p "$WORKDIR/demo/tests/e2e/corpus"

    # Broken locator — the demo's own broken-locator spec.
    cat > "$WORKDIR/demo/tests/e2e/heal-demo/broken-locator.spec.ts" <<'SPEC'
import { test, expect } from '@playwright/test'

test('@heal-demo broken locator', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByTestId('quail-heal-demo-anchor')).toBeVisible({ timeout: 4000 })
})
SPEC

    # Corpus spec — provides high-stability anchors heal can rank.
    cat > "$WORKDIR/demo/tests/e2e/corpus/anchors.spec.ts" <<'CORPUS'
import { test, expect } from '@playwright/test'

test('corpus anchors', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Welcome home' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
  await expect(page.getByLabel('Email address')).toBeVisible()
})
CORPUS

    # Synthetic playwright-report.json listing the broken locator failure
    # at the spec's line 5.
    cat > "$WORKDIR/demo/playwright-report.json" <<'REPORT'
{
  "suites": [
    {
      "title": "tests/e2e/heal-demo/broken-locator.spec.ts",
      "file": "tests/e2e/heal-demo/broken-locator.spec.ts",
      "specs": [
        {
          "title": "broken locator",
          "file": "tests/e2e/heal-demo/broken-locator.spec.ts",
          "line": 5,
          "tests": [{"results": [{
            "status": "failed",
            "errors": [{
              "message": "Error: expect(locator).toBeVisible() failed\nLocator: getByTestId('quail-heal-demo-anchor')\nExpected: visible\nTimeout: 4000ms",
              "location": {"file": "tests/e2e/heal-demo/broken-locator.spec.ts", "line": 5, "column": 3}
            }]
          }]}]
        }
      ]
    }
  ]
}
REPORT

    cd "$WORKDIR/demo"
    echo "==> Run quail heal --dry-run"
    env \
      QUAIL_HEAL_MODE=on-failure \
      OPENAI_BASE_URL="${OLLAMA}" \
      OPENAI_API_KEY=ollama \
      QUAIL_MODEL="$MODEL" \
      "$BIN" heal --report=playwright-report.json --dry-run 2>&1 | tee "$WORKDIR/heal.log" | tail -40

    echo "==> Assert heal proposed an edit"
    if ! grep -q "edits prepared" "$WORKDIR/heal.log"; then
      echo "FAIL: heal did not report any edits prepared"
      echo "workDir kept: $WORKDIR"
      exit 1
    fi
    if ! grep -q -- "-.*getByTestId('quail-heal-demo-anchor')" "$WORKDIR/heal.log"; then
      echo "FAIL: heal did not target the broken locator"
      echo "workDir kept: $WORKDIR"
      exit 1
    fi
    if ! grep -qE "\+.*(getByRole|getByLabel|getByText)" "$WORKDIR/heal.log"; then
      echo "FAIL: heal replacement is not a role/label/text locator"
      echo "workDir kept: $WORKDIR"
      exit 1
    fi

    EDIT_COUNT=$(grep -oE 'count=[0-9]+' "$WORKDIR/heal.log" | head -1 | cut -d= -f2)
    echo "OK: heal proposed $EDIT_COUNT edit(s) targeting the broken locator"
    echo "workDir: $WORKDIR"
    ;;

  *)
    echo "unknown MODE=$MODE (want: probe | heal)" >&2
    exit 2
    ;;
esac
