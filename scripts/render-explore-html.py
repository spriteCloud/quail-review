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
  .exec-summary {{ background: var(--white); border: 1px solid var(--border-warm); border-radius: 4px; padding: 20px 22px; box-shadow: var(--shadow-soft); margin: 20px 0; }}
  .exec-summary h3 {{ margin: 0 0 10px; font-size: 14px; font-weight: 700; color: var(--copper); letter-spacing: 0.08em; text-transform: uppercase; font-family: var(--font-mono); }}
  .exec-summary p {{ margin: 0 0 10px; font-size: 15px; line-height: 1.55; color: var(--ink); }}
  .exec-summary p:last-child {{ margin-bottom: 0; }}
  .exec-summary .verdict-bold {{ color: var(--deep-water); font-weight: 700; }}
  ul.findings {{ list-style: none; margin: 0; padding: 0; display: grid; gap: 10px; }}
  ul.findings li {{ display: grid; grid-template-columns: 90px 1fr auto; gap: 14px; align-items: center; padding: 12px 14px; background: var(--white); border: 1px solid var(--border-warm); border-left-width: 4px; border-radius: 3px; }}
  ul.findings li.sev-high {{ border-left-color: var(--fail-red); background: var(--fail-soft); }}
  ul.findings li.sev-med  {{ border-left-color: #B45309; background: #FEF3C7; }}
  ul.findings li.sev-low  {{ border-left-color: var(--mist); background: #F1F5F9; }}
  ul.findings .sev-badge {{ font-family: var(--font-mono); font-size: 11px; font-weight: 700; letter-spacing: 0.12em; text-align: center; padding: 4px 8px; border-radius: 3px; }}
  ul.findings .sev-high .sev-badge {{ background: var(--fail-red); color: #fff; }}
  ul.findings .sev-med  .sev-badge {{ background: #B45309; color: #fff; }}
  ul.findings .sev-low  .sev-badge {{ background: var(--mist); color: #fff; }}
  ul.findings .find-title {{ font-size: 14px; color: var(--ink); }}
  ul.findings .find-tech  {{ font-family: var(--font-mono); font-size: 11px; color: var(--mist); margin-top: 3px; letter-spacing: 0.02em; }}
  ul.findings .find-count {{ font-family: var(--font-mono); font-size: 13px; font-weight: 700; color: var(--ink); }}
  .no-findings {{ display: flex; align-items: center; gap: 10px; padding: 14px 16px; background: var(--ok-soft); border-left: 4px solid var(--ok-green); border-radius: 3px; color: var(--ok-green); font-weight: 600; }}
  .h2-caption {{ font-family: var(--font-mono); font-size: 11px; font-weight: 500; letter-spacing: 0.08em; color: var(--mist); margin: -8px 0 12px; }}
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

  {exec_summary}

  <h2>Findings</h2>
  <p class="h2-caption">issues surfaced during exploration — what to look at first.</p>
  {findings_panel}

  <h2>Scenarios explored</h2>
  <p class="h2-caption">the full Gherkin narrative — every step the exploratory session ran, in order.</p>
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
    counters: list[tuple[str, str]] = []
    for line in raw[hits.end():].splitlines():
        m = ANOMALY_COUNTER.match(line)
        if m:
            counters.append((m.group(1).strip(), m.group(2)))
        elif counters:
            break  # first non-counter line after the block ends parsing
        elif line.strip() == "":
            continue
        else:
            break
    return counters


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


# Business-language mapping. Keys are the technical prefixes quail-core
# emits in `# anomalies observed:` blocks; values are (severity,
# business title, one-sentence explanation the reader can act on).
FINDING_TRANSLATION = {
    "state regressions": (
        "MED",
        "Session state may survive Back navigation",
        "After clicking through and pressing Back+Reload, the original form controls "
        "were no longer where they should be. Worth a look — may leak partial state "
        "or break CSRF assumptions on retry.",
    ),
    "injection reflections": (
        "HIGH",
        "Malicious script may reflect on the page (XSS risk)",
        "A crafted script payload appeared verbatim in the rendered page. Real user "
        "input needs to be escaped before it's shown; this is a classic Cross-Site "
        "Scripting exposure.",
    ),
    "same-origin console errors": (
        "LOW",
        "Unexpected app-side errors during scenarios",
        "The application logged JavaScript errors while we exercised the flow. Not "
        "a security exploit, but a sign of unhandled edge cases — worth passing to "
        "engineering.",
    ),
    "same-origin 5xx responses": (
        "HIGH",
        "Server errors during scenarios",
        "The backend returned 5xx responses while we exercised the flow. These are "
        "server crashes visible to the user — treat as production-severity bugs.",
    ),
    "same-origin request failures": (
        "MED",
        "In-app requests failed during scenarios",
        "One or more calls made by the app during the interaction did not complete. "
        "Points to broken features or flaky dependencies within the site itself.",
    ),
    "other": (
        "MED",
        "Other unexpected behaviour",
        "The executor logged an anomaly the summary can't classify — inspect the "
        "narrative below for the raw observation text.",
    ),
}

# Severity ordering: HIGH first when picking the recommendation focus.
SEV_ORDER = {"HIGH": 0, "MED": 1, "LOW": 2}


def _match_translation(key: str):
    """Best-effort prefix lookup so slight wording tweaks in quail-core don't break translation."""
    for tech_prefix, translation in FINDING_TRANSLATION.items():
        if key.lower().startswith(tech_prefix.lower()):
            return tech_prefix, translation
    return "other", FINDING_TRANSLATION["other"]


def render_findings_panel(counters: list[tuple[str, str]]) -> str:
    """Business-language findings list. Empty state is an affirmative 'nothing surfaced' card."""
    if not counters:
        return (
            '<div class="card"><div class="no-findings">'
            'No issues surfaced during this exploration.'
            '</div></div>'
        )
    translated = []
    for raw_key, count in counters:
        _, (sev, title, _blurb) = _match_translation(raw_key)
        translated.append((sev, title, raw_key, count))
    translated.sort(key=lambda t: (SEV_ORDER.get(t[0], 9), t[1]))
    rows = "".join(
        f'      <li class="sev-{sev.lower()}">'
        f'<span class="sev-badge">{html.escape(sev)}</span>'
        f'<div><div class="find-title">{html.escape(title)}</div>'
        f'<div class="find-tech">technical: {html.escape(raw_key)}</div></div>'
        f'<span class="find-count">{html.escape(count)}×</span>'
        f'</li>'
        for sev, title, raw_key, count in translated
    )
    return (
        '<div class="card">'
        f'<ul class="findings">{rows}</ul>'
        '</div>'
    )


def render_exec_summary(summary: dict[str, str], counters: list[tuple[str, str]]) -> str:
    """One-card business-English summary at the top of the report."""
    pages = summary.get("pages visited", "?")
    scenarios = _extract_int(summary.get("session", ""), r"(\d+) scenario")
    scenarios_str = str(scenarios) if scenarios else "?"
    executed = summary.get("executed", "")
    clean = _extract_int(executed, r"(\d+) clean")
    with_anom = _extract_int(executed, r"(\d+) with anomalies")
    not_reached = _extract_int(executed, r"(\d+) not reached")
    stopped = summary.get("stopped", "")

    total_findings = sum(int(c) for _, c in counters) if counters else 0
    highs = sum(int(c) for k, c in counters if _match_translation(k)[1][0] == "HIGH")
    meds  = sum(int(c) for k, c in counters if _match_translation(k)[1][0] == "MED")
    lows  = sum(int(c) for k, c in counters if _match_translation(k)[1][0] == "LOW")

    scope = (
        f"We explored <span class='verdict-bold'>{html.escape(str(pages))} page(s)</span>, "
        f"running <span class='verdict-bold'>{html.escape(scenarios_str)} exploratory scenarios</span> "
        f"({clean or 0} completed cleanly, {with_anom or 0} raised issues, {not_reached or 0} "
        f"could not be reached)."
    )
    if total_findings == 0:
        verdict = (
            "<span class='verdict-bold'>No issues surfaced.</span> "
            "The application behaved as expected across the scenarios we ran."
        )
        recommendation = (
            "Nothing needs attention from this run. Consider expanding the exploration "
            "surface (more pages / more categories) to keep coverage broad."
        )
    else:
        badge_bits = []
        if highs: badge_bits.append(f"<span class='verdict-bold'>{highs} high</span>")
        if meds:  badge_bits.append(f"<span class='verdict-bold'>{meds} medium</span>")
        if lows:  badge_bits.append(f"<span class='verdict-bold'>{lows} low</span>")
        verdict = (
            f"<span class='verdict-bold'>{total_findings} issue(s) worth investigating</span> "
            f"({' · '.join(badge_bits)})."
        )
        focus = "highest" if highs else "medium" if meds else "low"
        recommendation = (
            f"Start with the {focus}-severity findings below. Each carries an "
            "explanation of what we observed and why it might matter; the technical "
            "Gherkin narrative further down shows the exact steps we ran."
        )
    stopped_note = f" Session stopped: {html.escape(stopped)}." if stopped else ""

    return (
        '<div class="exec-summary">'
        '<h3>Executive summary</h3>'
        f'<p><strong>Scope.</strong> {scope}{stopped_note}</p>'
        f'<p><strong>Verdict.</strong> {verdict}</p>'
        f'<p><strong>Recommendation.</strong> {recommendation}</p>'
        '</div>'
    )


def _extract_int(text: str, pattern: str) -> int | None:
    m = re.search(pattern, text)
    return int(m.group(1)) if m else None


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
    counters = extract_anomaly_counters(raw)
    exec_summary = render_exec_summary(summary, counters)
    findings_panel = render_findings_panel(counters)

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
            exec_summary=exec_summary,
            findings_panel=findings_panel,
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
