#!/usr/bin/env bash
# Local end-to-end verify for a generate cycle.
#
# 1. Builds `quail` from the current working tree.
# 2. Snapshots the demo repo (main) into a workDir.
# 3. Runs `quail probe` against a target URL — hits the sibling
#    quail-core packages so touch/responsive/visual specs land in the
#    workDir the same way the real generate step would write them.
# 4. Runs `npx playwright test --list --grep @smoke` — surfaces every
#    TS compile error (duplicate `configure`, duplicate `const`,
#    parse errors, missing imports) that the real CI verify job
#    would catch. Same failure signal, ~90 seconds instead of ~15
#    minutes per iteration.
#
# Usage:
#   ./scripts/local-verify.sh                     # defaults below
#   URL=https://x.test/y KINDS=responsive ./scripts/local-verify.sh
#
# Env overrides:
#   URL      target-url the probe visits           (default: spritecloud)
#   KINDS    QUAIL_KINDS filter                    (default: journey,touch,responsive,visual)
#   OLLAMA   http://.../v1 base for humanize       (default: DGX via netbird)
#   MODEL    model id                              (default: qwen3-coder-next:latest)
#   NO_LLM=1 skip LLM humanize entirely (fastest)
#   NO_TSC=1 skip the playwright --list step (just render)

set -euo pipefail

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

echo "==> Snapshot demo tree into $WORKDIR/demo"
mkdir -p "$WORKDIR/demo"
# rsync -a excludes .git so we get a clean workDir without git history noise
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
  ${NO_LLM:+QUAIL_HUMANIZE=0} \
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
