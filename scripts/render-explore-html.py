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
- Scenarios are parsed and rendered as a nested collapsible tree
  (<details>) grouped by page + category. No raw <pre> dump, no JS.
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
  /* Two-view toggle — executive above, technical hidden by default */
  details.tech-detail {{ margin: 32px 0 0; }}
  details.tech-detail > summary {{ list-style: none; cursor: pointer; user-select: none; display: flex; align-items: center; gap: 14px; padding: 14px 18px; background: var(--warm-white); border: 1px dashed var(--border-warm); border-radius: 4px; }}
  details.tech-detail > summary::-webkit-details-marker {{ display: none; }}
  details.tech-detail > summary::before {{ content: "▸"; font-family: var(--font-mono); color: var(--copper); font-size: 12px; width: 12px; flex: 0 0 12px; }}
  details.tech-detail[open] > summary::before {{ content: "▾"; }}
  details.tech-detail > summary:hover {{ background: var(--white); box-shadow: var(--shadow-soft); }}
  .tech-toggle-label {{ font-family: var(--font-sans); font-weight: 700; font-size: 14px; color: var(--deep-water); }}
  .tech-toggle-hint {{ font-family: var(--font-mono); font-size: 11px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--mist); margin-left: auto; }}
  details.tech-detail[open] > summary {{ margin-bottom: 14px; }}
  /* Scenarios tree — nested <details>, no JS */
  .scenarios-tree {{ display: grid; gap: 10px; }}
  .scenarios-tree details {{ background: transparent; }}
  .scenarios-tree details > summary {{ list-style: none; cursor: pointer; user-select: none; padding: 12px 14px; background: var(--white); border: 1px solid var(--border-warm); border-radius: 4px; display: flex; align-items: center; gap: 12px; box-shadow: var(--shadow-soft); }}
  .scenarios-tree details > summary::-webkit-details-marker {{ display: none; }}
  .scenarios-tree details > summary::before {{ content: "▸"; font-family: var(--font-mono); color: var(--mist); font-size: 12px; width: 12px; flex: 0 0 12px; transition: transform var(--t-fast); }}
  .scenarios-tree details[open] > summary::before {{ content: "▾"; color: var(--copper); }}
  .scenarios-tree details > summary:hover {{ box-shadow: var(--shadow-pop); }}
  .scenarios-tree .s-label {{ font-family: var(--font-mono); font-size: 10px; font-weight: 600; letter-spacing: 0.14em; text-transform: uppercase; color: var(--copper); padding: 2px 6px; background: var(--copper-10); border-radius: 3px; }}
  .scenarios-tree .s-url {{ font-family: var(--font-mono); font-size: 13px; color: var(--ink); word-break: break-all; flex: 1; }}
  .scenarios-tree .s-count {{ font-family: var(--font-mono); font-size: 11px; color: var(--mist); letter-spacing: 0.04em; margin-left: auto; white-space: nowrap; }}
  .scenarios-tree .s-count strong {{ color: var(--fail-red); font-weight: 700; }}
  .scenarios-tree .cat-summary {{ padding: 10px 14px; margin: 8px 0 8px 24px; background: var(--warm-white); border: 1px solid var(--border-warm); }}
  .scenarios-tree .cat-pill {{ font-family: var(--font-mono); font-size: 11px; font-weight: 700; letter-spacing: 0.06em; padding: 3px 8px; border-radius: 3px; text-transform: lowercase; }}
  .scenarios-tree .cat-pill.sev-clean {{ background: var(--ok-soft); color: var(--ok-green); }}
  .scenarios-tree .cat-pill.sev-med   {{ background: #FEF3C7; color: #B45309; }}
  .scenarios-tree .cat-pill.sev-high  {{ background: var(--fail-soft); color: var(--fail-red); }}
  .scenarios-tree .cat-pill.sev-low   {{ background: #F1F5F9; color: var(--graphite); }}
  ul.scenario-list {{ list-style: none; margin: 6px 0 12px 24px; padding: 0; display: grid; gap: 8px; }}
  li.scenario {{ background: var(--white); border: 1px solid var(--border-warm); border-left: 3px solid var(--border-warm); border-radius: 3px; padding: 12px 14px; }}
  li.scenario.has-issue {{ border-left-color: var(--fail-red); }}
  li.scenario.is-not-reached {{ border-left-color: var(--mist); background: #F1F5F9; }}
  .sc-head {{ display: flex; align-items: center; gap: 10px; margin-bottom: 8px; flex-wrap: wrap; }}
  .status-pill {{ font-family: var(--font-mono); font-size: 10px; font-weight: 700; letter-spacing: 0.12em; text-transform: uppercase; padding: 3px 8px; border-radius: 3px; white-space: nowrap; }}
  .status-clean       {{ background: var(--ok-soft); color: var(--ok-green); }}
  .status-issue       {{ background: var(--fail-soft); color: var(--fail-red); }}
  .status-not-reached {{ background: #F1F5F9; color: var(--graphite); }}
  .sc-title {{ font-size: 13.5px; color: var(--ink); flex: 1; min-width: 0; line-height: 1.4; }}
  .sc-dur {{ font-family: var(--font-mono); font-size: 11px; color: var(--mist); white-space: nowrap; }}
  ol.steps {{ list-style: none; margin: 0; padding: 0; display: grid; gap: 3px; }}
  ol.steps li {{ display: grid; grid-template-columns: 50px 1fr; gap: 10px; align-items: baseline; font-family: var(--font-mono); font-size: 12.5px; color: var(--ink); line-height: 1.55; }}
  ol.steps .kw {{ font-family: var(--font-mono); font-size: 10.5px; font-weight: 700; letter-spacing: 0.06em; text-transform: uppercase; text-align: right; padding: 1px 6px; border-radius: 2px; color: var(--white); }}
  ol.steps .kw-given {{ background: var(--deep-water); }}
  ol.steps .kw-when  {{ background: var(--copper); }}
  ol.steps .kw-and   {{ background: var(--mist); }}
  ol.steps .kw-then  {{ background: var(--clear-blue); }}
  ol.steps .kw-but   {{ background: var(--graphite); }}
  .observation {{ margin-top: 10px; padding: 10px 12px; background: var(--fail-soft); border-left: 3px solid var(--fail-red); border-radius: 3px; font-family: var(--font-mono); font-size: 12.5px; color: var(--ink); display: flex; gap: 10px; align-items: baseline; }}
  .observation .obs-label {{ font-size: 10px; font-weight: 700; letter-spacing: 0.14em; text-transform: uppercase; color: var(--fail-red); flex: 0 0 auto; }}
  .sel-footer {{ margin-top: 10px; padding-top: 8px; border-top: 1px dashed var(--border-warm); font-family: var(--font-mono); font-size: 11px; color: var(--mist); display: flex; flex-wrap: wrap; gap: 12px 18px; align-items: baseline; }}
  .sel-footer .sel-item {{ display: inline-flex; gap: 6px; align-items: baseline; }}
  .sel-footer .sel-k {{ color: var(--mist); }}
  .sel-footer .sel-arrow {{ color: var(--copper); }}
  .sel-footer .sel-v {{ color: var(--ink); background: var(--warm-white); padding: 1px 5px; border-radius: 2px; }}
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

  <details class="tech-detail">
    <summary>
      <span class="tech-toggle-label">Technical detail — full scenarios narrative</span>
      <span class="tech-toggle-hint">for QA / engineering</span>
    </summary>
    <p class="h2-caption">every scenario the exploratory session ran, grouped by page and category. Click a group to expand.</p>
    {scenarios_tree}
  </details>

  <footer>quail-explore · ephemeral suite · report preserved for review</footer>
</div>
</body>
</html>
"""


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


# ---------------------------------------------------------------------------
# Gherkin narrative → nested tree parser + renderer.
# The Go emitter's shape:
#   Feature: Adversarial exploration of <URL>
#     # Page: <URL>
#     @adversarial @<category>
#     Scenario: <title> — <badge> (<duration>)
#       Given ...
#       When ...
#       And ...
#       Then ...
#     # observation: <text>            (only when anomaly)
#     # selector-map:
#     #   <human> → <selector>
# ---------------------------------------------------------------------------

_PAGE_RE = re.compile(r"^\s*#\s*Page:\s*(.+?)\s*$")
_TAG_RE = re.compile(r"^\s*(@\S+.*)$")
_SCENARIO_RE = re.compile(
    r"^\s*Scenario:\s*(?P<title>.+?)\s*—\s*"
    r"(?P<badge>no anomalies observed(?: \(expected dismissal\))?"
    r"|anomalies observed(?: \(timeout\))?"
    r"|not reached(?: \(transport blocked\))?)"
    r"(?:\s*\((?P<dur>[^)]+)\))?\s*$"
)
_STEP_RE = re.compile(r"^\s*(Given|When|Then|And|But)\s+(.+?)\s*$")
_OBS_RE = re.compile(r"^\s*#\s*observation:\s*(.+?)\s*$")
_SEL_HDR_RE = re.compile(r"^\s*#\s*selector-map:\s*$")
_SEL_ENTRY_RE = re.compile(r"^\s*#\s*(?P<human>.+?)\s*→\s*(?P<sel>.+?)\s*$")


def _status_from_badge(badge: str) -> str:
    if badge.startswith("no anomalies"):
        return "clean"
    if badge.startswith("anomalies"):
        return "issue"
    # Covers "not reached" and "not reached (transport blocked)".
    return "not-reached"


def parse_gherkin(raw: str) -> list[dict]:
    """Walk the Gherkin narrative, returning a list of page dicts:

        [{"url": ..., "categories": [{"name": ..., "scenarios": [...]}, ...]}]

    Header comments (# quail explore, # target, # session, etc.) and the
    Feature: line are consumed but discarded — they surface in the exec
    summary and meta grid, not here.
    """
    pages: list[dict] = []
    current_page: dict | None = None
    current_cats: dict[str, list[dict]] = {}
    pending_tags: list[str] = []
    current_scenario: dict | None = None
    in_selector_map = False

    def close_scenario():
        nonlocal current_scenario, in_selector_map
        if current_scenario is None:
            return
        cat = current_scenario.pop("_category", "misc")
        current_cats.setdefault(cat, []).append(current_scenario)
        current_scenario = None
        in_selector_map = False

    def close_page():
        nonlocal current_page, current_cats
        close_scenario()
        if current_page is not None:
            current_page["categories"] = [
                {"name": name, "scenarios": current_cats[name]}
                for name in sorted(current_cats.keys())
            ]
            pages.append(current_page)
        current_page = None
        current_cats = {}

    for line in raw.splitlines():
        stripped = line.strip()

        m = _PAGE_RE.match(line)
        if m:
            close_page()
            current_page = {"url": m.group(1)}
            current_cats = {}
            pending_tags = []
            continue

        if current_page is None:
            # skip the executive header + Feature line — those data are
            # already lifted into the meta grid and exec summary.
            continue

        m = _TAG_RE.match(line)
        if m and not stripped.startswith("#"):
            close_scenario()
            pending_tags = [t.lstrip("@") for t in stripped.split() if t.startswith("@")]
            continue

        m = _SCENARIO_RE.match(line)
        if m:
            close_scenario()
            category = next((t for t in pending_tags if t != "adversarial"), "misc")
            current_scenario = {
                "_category": category,
                "tags": pending_tags[:],
                "title": m.group("title"),
                "status": _status_from_badge(m.group("badge")),
                "badge_raw": m.group("badge"),
                "duration": m.group("dur") or "",
                "steps": [],
                "observation": None,
                "selector_map": [],
            }
            pending_tags = []
            in_selector_map = False
            continue

        if current_scenario is None:
            continue

        m = _OBS_RE.match(line)
        if m:
            current_scenario["observation"] = m.group(1)
            continue

        if _SEL_HDR_RE.match(line):
            in_selector_map = True
            continue

        if in_selector_map:
            m = _SEL_ENTRY_RE.match(line)
            if m:
                current_scenario["selector_map"].append((m.group("human"), m.group("sel")))
                continue
            in_selector_map = False  # fall through and try step

        m = _STEP_RE.match(line)
        if m:
            current_scenario["steps"].append({"kw": m.group(1), "text": m.group(2)})
            continue

    close_page()
    return pages


# Category → severity class the pill inherits. Aligns with the
# findings-panel severity but scoped by category so a clean category
# reads green, a state-corrupt category reads amber, etc.
_CATEGORY_SEV = {
    "boundary":       "clean",
    "data-edge":      "clean",
    "injection":      "clean",
    "race":           "clean",
    "auth":           "clean",
    "flow-interrupt": "clean",
    "upstream-dep":   "clean",
    "state-corrupt":  "clean",   # bumped when a scenario has an issue
    "misc":           "low",
}


def _cat_severity(scenarios: list[dict]) -> str:
    """Category badge severity: 'high' if any issue in a security-flavoured
    category, 'med' for any other issue, else 'clean'."""
    has_issue = any(s["status"] == "issue" for s in scenarios)
    if not has_issue:
        return "clean"
    return "med"


def _render_scenario(sc: dict) -> str:
    status = sc["status"]
    css_class = "has-issue" if status == "issue" else ("is-not-reached" if status == "not-reached" else "")
    status_label = {"clean": "clean", "issue": "issue", "not-reached": "not reached"}[status]
    dur = f'<span class="sc-dur">{html.escape(sc["duration"])}</span>' if sc.get("duration") else ""
    steps = "".join(
        f'<li><span class="kw kw-{s["kw"].lower()}">{html.escape(s["kw"])}</span>'
        f'<span class="step-text">{html.escape(s["text"])}</span></li>'
        for s in sc["steps"]
    )
    obs = ""
    if sc.get("observation"):
        obs = (
            '<div class="observation">'
            '<span class="obs-label">observed</span>'
            f'<span>{html.escape(sc["observation"])}</span>'
            '</div>'
        )
    sel_footer = ""
    if sc.get("selector_map"):
        items = "".join(
            f'<span class="sel-item"><span class="sel-k">{html.escape(k)}</span>'
            f'<span class="sel-arrow">→</span><code class="sel-v">{html.escape(v)}</code></span>'
            for k, v in sc["selector_map"]
        )
        sel_footer = f'<div class="sel-footer">{items}</div>'
    return (
        f'<li class="scenario {css_class}">'
        '<div class="sc-head">'
        f'<span class="status-pill status-{status.replace("-", "-")}">{html.escape(status_label)}</span>'
        f'<span class="sc-title">{html.escape(sc["title"])}</span>'
        f'{dur}'
        '</div>'
        f'<ol class="steps">{steps}</ol>'
        f'{obs}'
        f'{sel_footer}'
        '</li>'
    )


def _render_category(cat: dict) -> str:
    scenarios = cat["scenarios"]
    total = len(scenarios)
    issues = sum(1 for s in scenarios if s["status"] == "issue")
    not_reached = sum(1 for s in scenarios if s["status"] == "not-reached")
    sev = _cat_severity(scenarios)
    if issues:
        count_html = f'{total} scenarios · <strong>{issues} need attention</strong>'
    elif not_reached and not_reached == total:
        count_html = f'{total} scenarios · all unreached'
    else:
        suffix = f' · {not_reached} unreached' if not_reached else ''
        count_html = f'{total} scenarios · all clean{suffix}'
    scenario_html = "".join(_render_scenario(s) for s in scenarios)
    open_attr = " open" if issues else ""
    return (
        f'<details{open_attr}>'
        f'<summary class="cat-summary">'
        f'<span class="cat-pill sev-{sev}">{html.escape(cat["name"])}</span>'
        f'<span class="s-count">{count_html}</span>'
        f'</summary>'
        f'<ul class="scenario-list">{scenario_html}</ul>'
        f'</details>'
    )


def _render_page(page: dict) -> str:
    cats = page["categories"]
    total_scenarios = sum(len(c["scenarios"]) for c in cats)
    total_issues = sum(
        sum(1 for s in c["scenarios"] if s["status"] == "issue") for c in cats
    )
    if total_issues:
        count_html = f'{total_scenarios} scenarios · <strong>{total_issues} need attention</strong>'
    else:
        count_html = f'{total_scenarios} scenarios · all clean'
    cats_html = "".join(_render_category(c) for c in cats)
    return (
        '<details open>'
        '<summary class="page-summary">'
        '<span class="s-label">page</span>'
        f'<span class="s-url">{html.escape(page["url"])}</span>'
        f'<span class="s-count">{count_html}</span>'
        '</summary>'
        f'{cats_html}'
        '</details>'
    )


def render_scenarios_tree(pages: list[dict]) -> str:
    if not pages:
        return (
            '<div class="card">'
            '<p style="margin:0;color:var(--mist);font-family:var(--font-mono);'
            'font-size:12.5px;">No scenarios in this run.</p>'
            '</div>'
        )
    return (
        '<div class="scenarios-tree">'
        + "".join(_render_page(p) for p in pages)
        + '</div>'
    )


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

    summary = extract_summary(raw)
    summary_cells = render_summary_cells(summary)
    counters = extract_anomaly_counters(raw)
    exec_summary = render_exec_summary(summary, counters)
    findings_panel = render_findings_panel(counters)
    scenarios_tree = render_scenarios_tree(parse_gherkin(raw))

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
            summary_cells=summary_cells,
            exec_summary=exec_summary,
            findings_panel=findings_panel,
            scenarios_tree=scenarios_tree,
        ),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
