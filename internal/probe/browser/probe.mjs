// reviewqa browser-driven probe.
//
// Invoked by the Go binary when REVIEWQA_BROWSER_PROBE=1 (or --use-browser
// is set). Takes a single URL on argv, performs a bounded BFS crawl using
// Chromium, and writes a JSON document to stdout describing the rendered
// state of each page.
//
// Schema:
//   { origin: string, pages: [{
//       url, finalURL, title, h1, h2s, links, images, meta, hasForm,
//       interactions, errors
//     }, ...], errors: string[] }
//
// Bounds (env-overridable): max 20 pages, max depth 3, single Chromium
// instance, sequential page fetches (rate-friendly).

// v0.89: engine selection via REVIEWQA_ENGINE (chromium|firefox|webkit,
// default chromium) and stealth wrapping via REVIEWQA_STEALTH
// (on|off, default on). The Go side cascades chromium→firefox→webkit
// in auto mode; per-engine binaries are lazy-installed by the Go
// runner cache. playwright-extra + StealthPlugin patch JS-layer bot
// detection (navigator.webdriver, plugins, chrome runtime); WAF
// detection at the TLS/HTTP2 layer falls to the engine's native
// fingerprint, which is why Firefox/WebKit tend to slip past Akamai-
// class WAFs where Chromium gets dropped.
import { chromium as chrStock, firefox as ffStock, webkit as wkStock } from '@playwright/test'
import { addExtra } from 'playwright-extra'
import StealthPlugin from 'puppeteer-extra-plugin-stealth'

const ENGINES = { chromium: chrStock, firefox: ffStock, webkit: wkStock }
const ENGINE_NAME = (process.env.REVIEWQA_ENGINE || 'chromium').toLowerCase()
const STOCK_ENGINE = ENGINES[ENGINE_NAME] || chrStock
const STEALTH_ON = (process.env.REVIEWQA_STEALTH || 'on').toLowerCase() !== 'off'
const engine = STEALTH_ON ? addExtra(STOCK_ENGINE).use(StealthPlugin()) : STOCK_ENGINE

const TARGET = process.argv[2]
if (!TARGET) {
  console.error('reviewqa-browser-probe: usage: node probe.mjs <url>')
  process.exit(2)
}

const MAX_PAGES = Number(process.env.REVIEWQA_MAX_PAGES ?? 20)
const MAX_DEPTH = Number(process.env.REVIEWQA_MAX_DEPTH ?? 3)
const NAV_TIMEOUT_MS = Number(process.env.REVIEWQA_NAV_TIMEOUT ?? 15_000)
const IDLE_MS = 800
const ACCEPT_BUTTON_RE = /^(accept|agree|got it|continue|i understand|allow|ok|allow all|accept all)/i

async function dismissCookieBanner(page) {
  // Best-effort: click anything that looks like an accept/agree button.
  const triggers = page.locator(
    'button, [role="button"], a:has-text("accept"), a:has-text("agree")'
  )
  const n = Math.min(await triggers.count(), 8)
  for (let i = 0; i < n; i++) {
    const t = triggers.nth(i)
    try {
      const text = ((await t.innerText({ timeout: 500 })) || '').trim()
      if (ACCEPT_BUTTON_RE.test(text) && (await t.isVisible({ timeout: 200 }))) {
        await t.click({ timeout: 1000 })
        await page.waitForTimeout(200)
        return true
      }
    } catch { /* keep going */ }
  }
  return false
}

async function expandPopups(page) {
  // Hover/click each aria-haspopup trigger to surface dropdown links.
  const popups = page.locator('[aria-haspopup]')
  const n = Math.min(await popups.count(), 6)
  for (let i = 0; i < n; i++) {
    const t = popups.nth(i)
    try {
      if (await t.isVisible({ timeout: 200 })) {
        await t.hover({ timeout: 500 })
        await page.waitForTimeout(100)
      }
    } catch { /* ignore — some popups need real click */ }
  }
}

async function collectPage(page, requestedURL) {
  const finalURL = page.url()
  const title = await page.title()

  const h1Texts = await page.locator('h1').allInnerTexts()
  const h2Texts = await page.locator('h2').allInnerTexts()

  // Links: include visibility hint so the Go side can prefer visible ones.
  const linkHandles = await page.locator('a[href]').elementHandles()
  const links = []
  for (const h of linkHandles.slice(0, 200)) {
    const href = (await h.getAttribute('href')) || ''
    const text = ((await h.innerText().catch(() => '')) || '').trim()
    const visible = await h.isVisible().catch(() => false)
    links.push({ href, text, visible })
    await h.dispose()
  }

  // Images with non-empty alt.
  const imgHandles = await page.locator('img[alt]').elementHandles()
  const images = []
  for (const h of imgHandles.slice(0, 50)) {
    const alt = ((await h.getAttribute('alt')) || '').trim()
    if (!alt) { await h.dispose(); continue }
    const src = (await h.getAttribute('src')) || ''
    images.push({ src, alt })
    await h.dispose()
  }

  // Meta tags + canonical.
  const meta = await page.evaluate(() => {
    const get = (name, attr = 'name') => {
      const el = document.querySelector(`meta[${attr}="${name}"]`)
      return el ? (el.getAttribute('content') || '').trim() : ''
    }
    return {
      Description: get('description'),
      ViewportContent: get('viewport'),
      OGTitle: get('og:title', 'property'),
      OGType: get('og:type', 'property'),
      OGDescription: get('og:description', 'property'),
      Canonical: (document.querySelector('link[rel="canonical"]')?.getAttribute('href') || '').trim(),
    }
  })

  // Form presence + named inputs.
  const hasForm = (await page.locator('form').count()) > 0

  // Per-form submission contract: action, method, enctype, scoped inputs.
  // Drives the API-contract test template; same-origin guard runs Go-side.
  const forms = await page.evaluate(() => {
    const out = []
    for (const f of document.querySelectorAll('form')) {
      const inputs = []
      for (const el of f.querySelectorAll('input, select, textarea')) {
        const tag = el.tagName.toLowerCase()
        const t = el.getAttribute('type') || ''
        if (tag === 'input' && (t === 'hidden' || t === 'submit' || t === 'button')) continue
        inputs.push({
          tag,
          type: t,
          name: el.getAttribute('name') || '',
          testid: el.getAttribute('data-testid') || '',
          aria: el.getAttribute('aria-label') || '',
          placeholder: el.getAttribute('placeholder') || '',
          required: el.hasAttribute('required'),
        })
      }
      out.push({
        action: f.getAttribute('action') || '',
        method: (f.getAttribute('method') || '').toLowerCase(),
        enctype: (f.getAttribute('enctype') || '').toLowerCase(),
        inputs,
      })
    }
    return out
  })
  const inputHandles = await page.locator('input, select, textarea').elementHandles()
  const inputs = []
  for (const h of inputHandles.slice(0, 40)) {
    const tag = (await h.evaluate(el => el.tagName.toLowerCase())) || 'input'
    const t = (await h.getAttribute('type')) || ''
    const name = (await h.getAttribute('name')) || ''
    const testid = (await h.getAttribute('data-testid')) || ''
    const aria = (await h.getAttribute('aria-label')) || ''
    const placeholder = (await h.getAttribute('placeholder')) || ''
    const required = (await h.evaluate(el => el.hasAttribute('required'))) ?? false
    const visible = await h.isVisible().catch(() => false)
    inputs.push({ tag, type: t, name, testid, aria, placeholder, required, visible })
    await h.dispose()
  }

  // Post-JS-render DOM snapshot — capped at 1 MiB so a single long page
  // doesn't blow the JSON pipe. Writes to tests/e2e/_dom/<slug>.html on
  // the Go side so reviewers can see what the browser actually rendered.
  let domHTML = ''
  try {
    domHTML = await page.evaluate(() => document.documentElement.outerHTML)
    if (domHTML.length > 1_048_576) domHTML = domHTML.slice(0, 1_048_576)
  } catch { /* leave empty — caller skips emission */ }

  // Interactive components (the v0.12 exercise journey kinds).
  const interactions = await page.evaluate(() => {
    const out = []
    document.querySelectorAll('input[type="search"]').forEach(el => out.push({ kind: 'search', inputType: 'search' }))
    document.querySelectorAll('[role="searchbox"]').forEach(() => out.push({ kind: 'search', role: 'searchbox' }))
    document.querySelectorAll('details').forEach(el => {
      const summary = el.querySelector('summary')
      out.push({ kind: 'details', text: (summary?.innerText || '').trim() })
    })
    document.querySelectorAll('dialog').forEach(() => out.push({ kind: 'dialog' }))
    document.querySelectorAll('[role="tab"]').forEach(el => out.push({ kind: 'tab', text: (el.innerText || '').trim(), role: 'tab' }))
    document.querySelectorAll('input[type="date"],input[type="time"],input[type="datetime-local"]').forEach(el => {
      out.push({ kind: 'date', inputType: el.getAttribute('type'), name: el.getAttribute('name') || '' })
    })
    document.querySelectorAll('[data-toggle],[data-bs-toggle]').forEach(el => {
      const t = el.getAttribute('data-toggle') || el.getAttribute('data-bs-toggle') || ''
      out.push({ kind: 'data-toggle', toggle: t, text: (el.innerText || '').trim() })
    })
    document.querySelectorAll('[aria-haspopup]').forEach(el => {
      // Skip popup if already covered by collapse (has aria-expanded + controls) or tab role.
      if (el.getAttribute('aria-expanded') !== null && el.getAttribute('aria-controls') !== null) return
      if (el.getAttribute('role') === 'tab') return
      out.push({ kind: 'popup', text: (el.innerText || '').trim() })
    })
    return out
  })

  // v0.91: explicit role-tagged actionable capture. The Go-side
  // regex over DOMHTML catches these too, but Playwright queries
  // resolve ARIA roles (both explicit role="..." and implicit
  // roles like <button>) more reliably across engines/frameworks.
  // The Go side dedups by tag+testid+aria+role+name so duplicate
  // captures are harmless. Capped at 50/page.
  const roleAnchors = await page.evaluate(() => {
    const out = []
    const els = document.querySelectorAll('[role="button"],[role="submit"],[role="link"],[role="menuitem"]')
    for (let i = 0; i < els.length && out.length < 50; i++) {
      const el = els[i]
      out.push({
        role: el.getAttribute('role') || '',
        text: (el.innerText || '').trim().slice(0, 200),
        ariaLabel: el.getAttribute('aria-label') || '',
        testid: el.getAttribute('data-testid') || '',
        visible: !!(el.offsetParent || el.getClientRects().length),
      })
    }
    return out
  })

  return {
    url: requestedURL,
    finalURL,
    title,
    h1: h1Texts.map(s => s.trim()).filter(Boolean),
    h2s: h2Texts.map(s => s.trim()).filter(Boolean),
    links,
    images,
    meta,
    hasForm,
    inputs,
    interactions,
    roleAnchors,
    domHTML,
    forms,
  }
}

function canonical(u) {
  try {
    const url = new URL(u)
    url.hash = ''
    url.search = ''
    if (url.pathname !== '/' && url.pathname.endsWith('/')) {
      url.pathname = url.pathname.replace(/\/+$/, '')
    }
    if (url.pathname === '/') url.pathname = ''
    return url.toString()
  } catch {
    return ''
  }
}

function sameOrigin(originURL, candidateURL) {
  try {
    const a = new URL(originURL)
    const b = new URL(candidateURL)
    return a.origin === b.origin
  } catch {
    return false
  }
}

const isAvoided = (path) => /privacy|terms|cookie|legal|sitemap|rss|feed/i.test(path)

async function main() {
  const errors = []
  const seen = new Set()
  const order = []
  const pagesOut = {}
  const queue = [{ url: canonical(TARGET), depth: 0 }]
  const browser = await engine.launch({ headless: true })
  const ctx = await browser.newContext({
    userAgent: 'reviewqa-browser-probe/1 (+https://github.com/spriteCloud/reviewqa)',
  })
  const page = await ctx.newPage()
  page.on('pageerror', (e) => errors.push(`pageerror: ${e}`))
  page.setDefaultNavigationTimeout(NAV_TIMEOUT_MS)

  let originResolved = null

  while (queue.length && Object.keys(pagesOut).length < MAX_PAGES) {
    const { url, depth } = queue.shift()
    if (seen.has(url)) continue
    seen.add(url)
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout: NAV_TIMEOUT_MS })
      await page.waitForTimeout(IDLE_MS)
      if (!originResolved) {
        originResolved = new URL(page.url()).origin
        await dismissCookieBanner(page)
      }
      await expandPopups(page)
      const p = await collectPage(page, url)
      const key = canonical(p.finalURL) || url
      if (pagesOut[key]) continue
      pagesOut[key] = p
      order.push(key)
      if (depth >= MAX_DEPTH) continue
      for (const l of p.links) {
        if (!l.href) continue
        let abs
        try {
          abs = new URL(l.href, p.finalURL).toString()
        } catch { continue }
        if (!sameOrigin(originResolved, abs)) continue
        const c = canonical(abs)
        if (!c || isAvoided(new URL(c).pathname.toLowerCase())) continue
        if (!seen.has(c)) queue.push({ url: c, depth: depth + 1 })
      }
    } catch (e) {
      errors.push(`fetch ${url}: ${String(e?.message ?? e)}`)
    }
  }

  await ctx.close()
  await browser.close()

  const pages = order.map(k => pagesOut[k])
  process.stdout.write(JSON.stringify({ origin: originResolved ?? canonical(TARGET), pages, errors }))
}

main().catch(e => {
  console.error('reviewqa-browser-probe: fatal:', e)
  process.exit(1)
})
