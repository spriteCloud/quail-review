// snap-landing.mjs — capture landing-page screenshots from the live
// `quail serve` UI. Uses the Playwright runner the browser-probe
// already cached at ~/.cache/quail/playwright-runner. Idempotent;
// re-run to refresh the PNGs in web/img/ for the next landing build.
//
// Usage:
//   node tools/snap-landing.mjs --base http://127.0.0.1:8765 --out web/img
//
// Reads project state from /api/state; doesn't depend on any
// particular project being loaded for the basic shots, but the
// scenario-card and run-drawer shots are best with the ING project.

import { chromium } from '/home/oa/.cache/quail/playwright-runner/node_modules/@playwright/test/index.mjs'
import { mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'

const args = Object.fromEntries(
  process.argv
    .slice(2)
    .map((a, i, arr) => (a.startsWith('--') ? [a.slice(2), arr[i + 1]] : null))
    .filter(Boolean),
)
const BASE = args.base || 'http://127.0.0.1:8765'
const OUT = resolve(args.out || 'web/img')
await mkdir(OUT, { recursive: true })

const VIEWPORT = { width: 1440, height: 1000 }
const DPR = 2

const browser = await chromium.launch()
const ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: DPR })
const page = await ctx.newPage()

async function shot (name, locatorSel, { padding = 16 } = {}) {
  await page.waitForSelector(locatorSel, { timeout: 10_000 })
  const handle = await page.$(locatorSel)
  const box = await handle.boundingBox()
  if (!box) throw new Error(`no bounding box for ${locatorSel}`)
  await page.screenshot({
    path: resolve(OUT, name),
    clip: {
      x: Math.max(0, box.x - padding),
      y: Math.max(0, box.y - padding),
      width: box.width + padding * 2,
      height: box.height + padding * 2,
    },
  })
  console.log(`shot: ${name}`)
}

async function go (path = '/') {
  await page.goto(`${BASE}${path}`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(400)
}

// 1) HOME Probe-a-URL card — clean, Advanced collapsed.
await go('/')
await shot('home-probe-card.png', '.home-probe-card')

// 2) Mid-probe: simulate progress + open log. Pure DOM poke; no real
//    probe ever fires. Stage-text matches the existing STAGES table
//    in app.js so the visual is faithful.
await go('/')
await page.evaluate(() => {
  const prog = document.querySelector('.home-probe-progress')
  const stage = document.querySelector('.home-probe-stage')
  const term = document.querySelector('.home-probe-terminal')
  const log = document.querySelector('.home-probe-log')
  const urlInput = document.querySelector('.home-probe-input')
  const nameInput = document.querySelector('.home-probe-name')
  if (urlInput) urlInput.value = 'https://www.ing.nl/particulier/hypotheek/hypotheek-berekenen'
  if (nameInput) nameInput.value = 'ING'
  if (prog) { prog.style.display = ''; prog.value = 75 }
  if (stage) stage.textContent = 'Composing scenarios'
  if (term) {
    term.textContent = [
      '$ quail probe --url https://www.ing.nl/... --local --name ING',
      'time=2026-06-19T13:30:01Z level=INFO msg="probe: crawling" url=https://www.ing.nl/...',
      'time=2026-06-19T13:30:04Z level=INFO msg="browser probe: launching" engine=chromium stealth=on',
      'time=2026-06-19T13:30:32Z level=INFO msg="identified 7 journeys" url=https://www.ing.nl/...',
      'time=2026-06-19T13:31:02Z level=INFO msg="composer: requesting LLM scenarios" model=qwen3-coder-next:latest',
      'time=2026-06-19T13:31:18Z level=INFO msg="composer: added scenarios" journey=contact count=3',
    ].join('\n')
  }
  if (log) log.open = true
})
await page.waitForTimeout(200)
await shot('home-probe-progress.png', '.home-probe-card', { padding: 12 })

// 3) Scenario card. Open a FRESH browser context after switching the
//    server-side workdir to the ING project — the dirty DOM from the
//    prior shots can suppress the sidebar render in the same context.
const ingPath = '/home/oa/workspace/sc/ing'
try {
  await page.evaluate(async (p) => {
    await fetch('/api/switch-project', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: p }) })
  }, ingPath)
  await page.waitForTimeout(800)
  const ctx2 = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: DPR })
  const page2 = await ctx2.newPage()
  await page2.goto(BASE + '/', { waitUntil: 'networkidle' })
  await page2.waitForTimeout(1500)
  const firstFeature = await page2.$('[data-feature-list] li[data-path]')
  if (firstFeature) {
    await firstFeature.click()
    await page2.waitForTimeout(800)
    const card = await page2.$('.scenario')
    if (card) {
      const box = await card.boundingBox()
      await page2.screenshot({
        path: resolve(OUT, 'scenario-card.png'),
        clip: { x: Math.max(0, box.x - 20), y: Math.max(0, box.y - 20), width: box.width + 40, height: box.height + 40 },
      })
      console.log('shot: scenario-card.png')
    } else {
      console.warn('scenario-card: .scenario not found after click')
    }
  } else {
    console.warn('scenario-card: no feature in sidebar')
  }
  await ctx2.close()
} catch (e) {
  console.warn('scenario-card capture skipped:', e.message)
}

// 4) Run drawer — simulate by injecting visible drawer DOM if the UI
//    exposes an openRunDrawer hook; else screenshot any visible
//    .run-drawer / .run-terminal that's already on screen.
try {
  await page.evaluate(() => {
    const drawer = document.querySelector('.run-drawer, .run-overlay')
    if (drawer) drawer.classList.add('open')
  })
  const drawer = await page.$('.run-drawer, .run-overlay')
  if (drawer) {
    await shot('run-drawer.png', '.run-drawer, .run-overlay', { padding: 16 })
  } else {
    // Fall back to the home-probe-terminal block as a "run-like" visual.
    await go('/')
    await page.evaluate(() => {
      const term = document.querySelector('.home-probe-terminal')
      const log = document.querySelector('.home-probe-log')
      if (term) {
        term.textContent = [
          '$ npx playwright test --grep "browse journey completes"',
          '# started 2026-06-19T13:33:11Z',
          '# bddgen — generating .features-gen/ from .feature files',
          '✓  1 [chromium] › browse-particulier.feature.spec.ts:7:5 › browse journey ... (1.4s)',
          'Test passed.',
        ].join('\n')
      }
      if (log) log.open = true
    })
    await shot('run-drawer.png', '.home-probe-card', { padding: 12 })
  }
} catch (e) {
  console.warn('run-drawer capture skipped:', e.message)
}

// 5) Multi-run sparkline — best-effort; capture the project pill +
//    history bar if present.
try {
  const pill = await page.$('.brand-project, .project-pill')
  if (pill) {
    await shot('multi-run-sparkline.png', '.brand-project, .project-pill', { padding: 14 })
  }
} catch (e) {
  console.warn('multi-run-sparkline capture skipped:', e.message)
}

await browser.close()
console.log('done.')
