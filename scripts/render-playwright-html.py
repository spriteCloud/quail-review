#!/usr/bin/env python3
"""Render a Playwright JSON reporter file as a spriteCloud-branded HTML page.

Usage:
    render-playwright-html.py <json-in> <html-out> [--pr <n>] [--sha <ref>] [--generated <ts>]

Design notes:
- Same brand template as render-explore-html.py — copper primary,
  warm-white page, Inter / Fira Code system stacks, pixel-bar section
  markers. Kept in sync by copy, not by import, so this file works
  standalone if invoked directly.
- Emits a per-spec pass/fail table by walking the reporter's
  .suites[].specs[].tests[].results[] shape. Passing specs collapse
  to one row; failing specs also show the last error's message.
- Zero network dependencies. Fonts fall back to system stacks.
"""
from __future__ import annotations

import argparse
import html
import json
import os
import pathlib
from typing import Iterable


HTML_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Playwright report — PR #{pr_display}</title>
<style>
  :root {{
    --copper: #C0805A;
    --copper-hard: #a96d4a;
    --copper-10: rgba(192,128,90,.10);
    --copper-18: rgba(192,128,90,.18);
    --deep-water: #1B365D;
    --clear-blue: #4B8BBE;
    --ink: #0F1117;
    --charcoal: #1A1D26;
    --graphite: #2D3748;
    --mist: #8899AA;
    --warm-white: #FAFAF7;
    --white: #FFFFFF;
    --border-warm: #E8E6E1;
    --ok-green: #15803D;
    --ok-soft: #DCFCE7;
    --fail-red: #B91C1C;
    --fail-soft: #FEE2E2;
    --shadow-soft: 0 4px 14px rgba(15,17,23,.04);
    --shadow-pop: 0 12px 32px rgba(15,17,23,.16);
    --t-fast: .12s ease;
    --font-sans: "Inter", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    --font-mono: "Fira Code", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  }}
  * {{ box-sizing: border-box; }}
  html, body {{ margin: 0; padding: 0; background: var(--warm-white); color: var(--graphite); font: 14px/1.65 var(--font-sans); }}
  a {{ color: var(--clear-blue); text-decoration: none; }}
  a:hover {{ text-decoration: underline; }}
  .page {{ max-width: 1080px; margin: 0 auto; padding: 32px 24px 64px; }}
  header.top {{ display: flex; align-items: baseline; gap: 12px; padding-bottom: 12px; border-bottom: 1px solid var(--border-warm); margin-bottom: 24px; }}
  .wordmark {{ font-weight: 900; letter-spacing: -0.05em; font-size: 22px; color: var(--ink); }}
  .wordmark .s {{ color: #0090D0; }} .wordmark .c {{ color: #9DA5AE; }}
  .divider {{ color: var(--border-warm); }}
  .subtitle {{ font-family: var(--font-mono); font-size: 11px; font-weight: 500; letter-spacing: 0.16em; text-transform: uppercase; color: var(--copper); }}
  h1 {{ font-family: var(--font-sans); font-weight: 800; font-size: 28px; color: var(--deep-water); margin: 0 0 8px; letter-spacing: -0.01em; position: relative; padding-left: 18px; }}
  h1::before {{ content: ""; position: absolute; left: 0; top: 12px; width: 10px; height: 10px; background: var(--copper); }}
  h2 {{ font-family: var(--font-sans); font-weight: 700; font-size: 16px; color: var(--deep-water); margin: 32px 0 12px; position: relative; padding-left: 14px; }}
  h2::before {{ content: ""; position: absolute; left: 0; top: 8px; width: 6px; height: 6px; background: var(--copper); }}
  .meta {{ display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; margin: 20px 0 24px; }}
  .meta .cell {{ background: var(--white); border: 1px solid var(--border-warm); padding: 12px 14px; border-radius: 4px; box-shadow: var(--shadow-soft); }}
  .meta .k {{ font-family: var(--font-mono); font-size: 11px; font-weight: 500; letter-spacing: 0.16em; text-transform: uppercase; color: var(--copper); margin-bottom: 4px; }}
  .meta .v {{ font-family: var(--font-mono); font-size: 12px; color: var(--ink); word-break: break-all; }}
  .stat-row {{ display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin: 20px 0 12px; }}
  .stat {{ background: var(--white); border: 1px solid var(--border-warm); border-radius: 4px; padding: 14px 16px; box-shadow: var(--shadow-soft); }}
  .stat .n {{ font-family: var(--font-sans); font-weight: 800; font-size: 28px; letter-spacing: -0.01em; color: var(--deep-water); }}
  .stat .l {{ font-family: var(--font-mono); font-size: 11px; font-weight: 500; letter-spacing: 0.16em; text-transform: uppercase; color: var(--copper); margin-top: 4px; }}
  .stat.pass .n {{ color: var(--ok-green); }}
  .stat.fail .n {{ color: var(--fail-red); }}
  .card {{ background: var(--white); border: 1px solid var(--border-warm); border-radius: 4px; padding: 4px 0; box-shadow: var(--shadow-soft); overflow: hidden; }}
  table.specs {{ width: 100%; border-collapse: collapse; font-family: var(--font-mono); font-size: 12.5px; }}
  table.specs th, table.specs td {{ padding: 10px 16px; text-align: left; vertical-align: top; border-bottom: 1px solid var(--border-warm); }}
  table.specs th {{ background: var(--warm-white); font-weight: 500; letter-spacing: 0.16em; text-transform: uppercase; font-size: 11px; color: var(--copper); }}
  table.specs tr:last-child td {{ border-bottom: none; }}
  table.specs td.status {{ font-weight: 700; width: 90px; }}
  table.specs td.status.pass {{ color: var(--ok-green); }}
  table.specs td.status.fail {{ color: var(--fail-red); }}
  table.specs td.status.skip {{ color: var(--mist); }}
  table.specs td.dur {{ width: 90px; text-align: right; color: var(--mist); }}
  table.specs td.title {{ color: var(--ink); word-break: break-word; }}
  table.specs td.title .err {{ display: block; margin-top: 6px; padding: 8px 10px; background: var(--fail-soft); border-left: 3px solid var(--fail-red); color: var(--fail-red); font-size: 11.5px; white-space: pre-wrap; word-break: break-word; border-radius: 2px; }}
  footer {{ margin-top: 40px; padding-top: 16px; border-top: 1px solid var(--border-warm); font-family: var(--font-mono); font-size: 11px; color: var(--mist); letter-spacing: 0.04em; }}
  .empty {{ padding: 40px 20px; text-align: center; color: var(--mist); font-family: var(--font-mono); font-size: 12px; letter-spacing: 0.04em; }}
</style>
</head>
<body>
<div class="page">
  <header class="top">
    <span class="wordmark"><span class="s">s</span>prite<span class="c">c</span>loud</span>
    <span class="divider">/</span>
    <span class="subtitle">quail · playwright report</span>
  </header>

  <h1>Playwright run</h1>

  <div class="meta">
    <div class="cell"><div class="k">context</div><div class="v">{context}</div></div>
    <div class="cell"><div class="k">generated</div><div class="v">{generated}</div></div>
    <div class="cell"><div class="k">pass rate</div><div class="v">{rate_display}</div></div>
  </div>

  <div class="stat-row">
    <div class="stat pass"><div class="n">{passed}</div><div class="l">passed</div></div>
    <div class="stat fail"><div class="n">{failed}</div><div class="l">failed</div></div>
    <div class="stat"><div class="n">{skipped}</div><div class="l">skipped</div></div>
    <div class="stat"><div class="n">{total}</div><div class="l">total</div></div>
  </div>

  <h2>Specs</h2>
  <div class="card">
    {table}
  </div>

  <footer>quail-review · playwright report · brand tokens sourced from quail-platform style.css</footer>
</div>
</body>
</html>
"""


def _iter_specs(node) -> Iterable[dict]:
    """Yield every spec under the reporter's arbitrarily-nested suites."""
    if isinstance(node, dict):
        for spec in node.get("specs") or []:
            yield spec
        for child in node.get("suites") or []:
            yield from _iter_specs(child)


def _status_of(spec: dict) -> tuple[str, int, str]:
    """Return (status, total_duration_ms, first_error_msg) for one spec."""
    status = "skipped"
    duration = 0
    error = ""
    for t in spec.get("tests") or []:
        for r in t.get("results") or []:
            duration += int(r.get("duration") or 0)
            rs = r.get("status") or "skipped"
            if rs in ("failed", "timedOut", "interrupted"):
                status = "failed"
                if not error:
                    err = r.get("error") or {}
                    error = err.get("message") or err.get("value") or ""
            elif rs == "passed" and status != "failed":
                status = "passed"
            elif rs == "skipped" and status not in ("passed", "failed"):
                status = "skipped"
    return status, duration, error


def _render_table(report: dict) -> tuple[str, int, int, int]:
    rows = []
    passed = failed = skipped = 0
    for spec in _iter_specs(report):
        status, duration, err = _status_of(spec)
        title = spec.get("title") or "(untitled)"
        css = {"passed": "pass", "failed": "fail"}.get(status, "skip")
        symbol = {"passed": "✓ pass", "failed": "✗ fail"}.get(status, "— skip")
        err_html = f'<span class="err">{html.escape(err)}</span>' if (status == "failed" and err) else ""
        rows.append(
            f'<tr>'
            f'<td class="status {css}">{symbol}</td>'
            f'<td class="dur">{duration} ms</td>'
            f'<td class="title">{html.escape(title)}{err_html}</td>'
            f'</tr>'
        )
        if status == "passed":
            passed += 1
        elif status == "failed":
            failed += 1
        else:
            skipped += 1
    if not rows:
        return '<div class="empty">No specs executed.</div>', 0, 0, 0
    table = (
        '<table class="specs">'
        '<thead><tr><th>status</th><th style="text-align:right">duration</th><th>title</th></tr></thead>'
        f'<tbody>{"".join(rows)}</tbody>'
        '</table>'
    )
    return table, passed, failed, skipped


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("json_in", type=pathlib.Path)
    ap.add_argument("html_out", type=pathlib.Path)
    ap.add_argument("--pr", default="")
    ap.add_argument("--sha", default="")
    ap.add_argument("--generated", default="")
    args = ap.parse_args()

    try:
        report = json.loads(args.json_in.read_text(encoding="utf-8", errors="replace"))
    except (OSError, json.JSONDecodeError) as e:
        report = {"__err": str(e)}

    table, passed, failed, skipped = _render_table(report)
    total = passed + failed + skipped
    rate_display = f"{(passed * 100 // (passed + failed)) if (passed + failed) else 0}%" if total else "—"

    ctx = []
    if args.pr:
        ctx.append(f"PR #{html.escape(args.pr)}")
    if args.sha:
        ctx.append(html.escape(args.sha[:12]))
    if not ctx:
        ctx.append("local run")
    context = " · ".join(ctx)
    generated = html.escape(args.generated or "just now")
    pr_display = html.escape(args.pr) if args.pr else "n/a"

    args.html_out.write_text(
        HTML_TEMPLATE.format(
            pr_display=pr_display,
            context=context,
            generated=generated,
            rate_display=rate_display,
            passed=passed,
            failed=failed,
            skipped=skipped,
            total=total,
            table=table,
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
