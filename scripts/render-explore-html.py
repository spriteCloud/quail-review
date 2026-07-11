#!/usr/bin/env python3
"""Render a quail-explore Gherkin report as a spriteCloud-branded HTML page.

Usage:
    render-explore-html.py <gherkin-in> <html-out> [--url <target>] [--pr <n>] [--sha <ref>]

Design notes:
- Zero network dependencies. Fonts fall back to the system stack the brand
  memory documents (Inter → -apple-system … , Fira Code → ui-monospace …).
- Palette tokens come straight from quail-platform/internal/serve/web/style.css:
  copper #C0805A primary, deep-water #1B365D anchors, warm-white #FAFAF7 page,
  ink #0F1117 text on white cards.
- Pixel-bar motif: small copper bars drawn as ::before decorations on section
  headers. No SVG, no images.
- Report body is HTML-escaped and rendered inside <pre> so any Gherkin-like
  angle-brackets survive intact.
"""
from __future__ import annotations

import argparse
import html
import os
import pathlib
import re


HTML_TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>quail explore — {title}</title>
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
  .page {{ max-width: 960px; margin: 0 auto; padding: 32px 24px 64px; }}
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
  .card {{ background: var(--white); border: 1px solid var(--border-warm); border-radius: 4px; padding: 20px 22px; box-shadow: var(--shadow-soft); transition: box-shadow var(--t-fast); }}
  .card:hover {{ box-shadow: var(--shadow-pop); }}
  pre.report {{ margin: 0; padding: 16px 18px; background: var(--warm-white); border: 1px solid var(--border-warm); border-radius: 4px; font-family: var(--font-mono); font-size: 12.5px; line-height: 1.6; color: var(--ink); overflow-x: auto; white-space: pre-wrap; word-break: break-word; }}
  pre.report .gk-tag {{ color: var(--copper); font-weight: 600; }}
  pre.report .gk-kw {{ color: var(--deep-water); font-weight: 700; }}
  pre.report .gk-comment {{ color: var(--mist); }}
  ul.anomalies {{ list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }}
  ul.anomalies li {{ display: flex; justify-content: space-between; gap: 12px; padding: 8px 12px; background: var(--fail-soft); border-left: 3px solid var(--fail-red); border-radius: 3px; font-family: var(--font-mono); font-size: 12.5px; }}
  ul.anomalies .anom-k {{ color: var(--ink); }}
  ul.anomalies .anom-v {{ color: var(--fail-red); font-weight: 700; }}
  footer {{ margin-top: 40px; padding-top: 16px; border-top: 1px solid var(--border-warm); font-family: var(--font-mono); font-size: 11px; color: var(--mist); letter-spacing: 0.04em; }}
</style>
</head>
<body>
<div class="page">
  <header class="top">
    <span class="wordmark"><span class="s">s</span>prite<span class="c">c</span>loud</span>
    <span class="divider">/</span>
    <span class="subtitle">quail · explore report</span>
  </header>

  <h1>Adversarial exploration</h1>

  <div class="meta">
    <div class="cell"><div class="k">target</div><div class="v">{target}</div></div>
    <div class="cell"><div class="k">context</div><div class="v">{context}</div></div>
    <div class="cell"><div class="k">generated</div><div class="v">{generated}</div></div>
    {summary_cells}
  </div>
  {anomaly_panel}

  <h2>Gherkin narrative</h2>
  <div class="card">
    <pre class="report">{body}</pre>
  </div>

  <footer>quail-explore · ephemeral suite · report preserved for review</footer>
</div>
</body>
</html>
"""


GK_TAG = re.compile(r"(@[a-zA-Z0-9_-]+)")
GK_KW = re.compile(r"^(\s*)(Feature|Scenario|Given|When|Then|And|But):", re.M)
GK_COMMENT = re.compile(r"^(#.*)$", re.M)

# Header lines quail-core emits at the top of the Gherkin body — used to
# lift session data into the .meta grid without reshaping the template.
HEADER_LINE = re.compile(r"^# (pages visited|session|executed|stopped|mode|target): (.+)$", re.M)
ANOMALY_HEADER = re.compile(r"^# anomalies observed: (.+)$", re.M)
ANOMALY_COUNTER = re.compile(r"^#   (.+?): (\d+)$", re.M)


def extract_summary(raw: str) -> dict[str, str]:
    """Pull `# key: value` header lines out for the executive summary cards."""
    out: dict[str, str] = {}
    for match in HEADER_LINE.finditer(raw):
        out[match.group(1)] = match.group(2).strip()
    m = ANOMALY_HEADER.search(raw)
    if m:
        out["anomalies observed"] = m.group(1).strip()
    return out


def extract_anomaly_counters(raw: str) -> list[tuple[str, str]]:
    """Return per-kind counters from the `# anomalies observed:` block."""
    hits = ANOMALY_HEADER.search(raw)
    if not hits or hits.group(1).strip() == "none":
        return []
    tail = raw[hits.end():]
    end = re.search(r"^\S", tail, re.M)
    block = tail[: end.start()] if end else tail
    return [(m.group(1).strip(), m.group(2)) for m in ANOMALY_COUNTER.finditer(block)]


def render_summary_cells(summary: dict[str, str]) -> str:
    """Emit extra .cell divs for the session data, in a stable order."""
    order = [
        ("pages visited", "pages visited"),
        ("session", "session"),
        ("executed", "executed"),
        ("anomalies observed", "anomalies"),
        ("stopped", "stopped"),
        ("mode", "mode"),
    ]
    cells: list[str] = []
    for key, label in order:
        val = summary.get(key)
        if not val:
            continue
        cells.append(
            f'    <div class="cell"><div class="k">{html.escape(label)}</div>'
            f'<div class="v">{html.escape(val)}</div></div>'
        )
    return "\n".join(cells)


def render_anomaly_panel(counters: list[tuple[str, str]]) -> str:
    """Card for per-kind anomaly totals — empty string when nothing surfaced."""
    if not counters:
        return ""
    rows = "".join(
        f'      <li><span class="anom-k">{html.escape(k)}</span>'
        f'<span class="anom-v">{html.escape(v)}</span></li>'
        for k, v in counters
    )
    return (
        '\n  <h2>Anomalies observed</h2>\n'
        '  <div class="card">\n'
        f'    <ul class="anomalies">\n{rows}\n    </ul>\n'
        '  </div>'
    )


def colourise(escaped: str) -> str:
    # order matters: comments first (they may contain tag-like tokens),
    # then keywords, then tag decoration.
    out = GK_COMMENT.sub(r'<span class="gk-comment">\1</span>', escaped)
    out = GK_KW.sub(r'\1<span class="gk-kw">\2</span>:', out)
    out = GK_TAG.sub(r'<span class="gk-tag">\1</span>', out)
    return out


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("gherkin_in", type=pathlib.Path)
    ap.add_argument("html_out", type=pathlib.Path)
    ap.add_argument("--url", default="")
    ap.add_argument("--pr", default="")
    ap.add_argument("--sha", default="")
    ap.add_argument("--generated", default="")
    args = ap.parse_args()

    raw = args.gherkin_in.read_text(encoding="utf-8", errors="replace")
    escaped = html.escape(raw)
    coloured = colourise(escaped)

    summary = extract_summary(raw)
    summary_cells = render_summary_cells(summary)
    anomaly_panel = render_anomaly_panel(extract_anomaly_counters(raw))

    target = html.escape(args.url or os.environ.get("QUAIL_EXPLORE_URL", "n/a"))
    ctx_bits = []
    if args.pr:
        ctx_bits.append(f"PR #{html.escape(args.pr)}")
    if args.sha:
        ctx_bits.append(html.escape(args.sha[:12]))
    if not ctx_bits:
        ctx_bits.append("local run")
    context = " · ".join(ctx_bits)
    generated = html.escape(args.generated or "just now")

    args.html_out.write_text(
        HTML_TEMPLATE.format(
            title=target,
            target=target,
            context=context,
            generated=generated,
            body=coloured,
            summary_cells=summary_cells,
            anomaly_panel=anomaly_panel,
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
