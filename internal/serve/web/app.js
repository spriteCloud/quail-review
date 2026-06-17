// reviewqa serve — Phase B frontend (read + Scenario CRUD).
// Plain vanilla JS so the project keeps its no-build posture.

const $featureList = document.querySelector('[data-feature-list]')
const $docList = document.querySelector('[data-doc-list]')
const $content = document.querySelector('[data-content]')
const $projectName = document.querySelector('[data-project-name]')

let activeFeature = null
let activeDoc = null
let viewMode = 'pretty'

async function fetchJSON (url, opts = {}) {
  const r = await fetch(url, { headers: { 'Accept': 'application/json' }, ...opts })
  if (!r.ok) throw new Error(`${url} → ${r.status} ${await r.text()}`)
  return r.json()
}

function el (tag, attrs = {}, ...children) {
  const node = document.createElement(tag)
  for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue
    if (k === 'class') node.className = v
    else if (k === 'html') node.innerHTML = v
    else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2), v)
    else node.setAttribute(k, v === true ? '' : v)
  }
  for (const c of children) {
    if (c == null) continue
    if (typeof c === 'string') node.appendChild(document.createTextNode(c))
    else node.appendChild(c)
  }
  return node
}

function escapeHTML (s) {
  return s.replace(/[&<>]/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[ch]))
}

function highlightPlaceholders (text) {
  return escapeHTML(text).replace(/"[^"]*"/g, m => `<span class="placeholder-arg">${m}</span>`)
}

function renderTags (tags) {
  if (!tags || !tags.length) return null
  return el('div', { class: 'tag-row' }, ...tags.map(t => el('span', { class: 'tag' }, t)))
}

function scenarioToGherkin (sc) {
  const lines = []
  if (sc.tags && sc.tags.length) lines.push('  ' + sc.tags.join(' '))
  lines.push('  Scenario: ' + sc.name)
  for (const st of sc.steps) lines.push('    ' + st.keyword + ' ' + st.text)
  return lines.join('\n')
}

function renderFeature (feature, gherkin) {
  $content.replaceChildren()

  const header = el('div', { class: 'feature-header' },
    el('h1', {}, feature.name || '(unnamed feature)'),
    el('div', { class: 'path' }, feature.path),
    renderTags(feature.tags),
    feature.narrative ? el('div', { class: 'narrative' }, feature.narrative) : null,
  )
  const tabs = el('div', { class: 'tab-row' },
    el('button', { class: 'tab ' + (viewMode === 'pretty' ? 'active' : ''), onclick: () => { viewMode = 'pretty'; renderFeature(feature, gherkin) } }, 'Pretty'),
    el('button', { class: 'tab ' + (viewMode === 'raw' ? 'active' : ''), onclick: () => { viewMode = 'raw'; renderFeature(feature, gherkin) } }, 'Raw .feature'),
  )
  $content.appendChild(header)
  $content.appendChild(tabs)

  if (viewMode === 'raw') {
    $content.appendChild(el('pre', { class: 'raw-block' }, gherkin))
    return
  }

  for (const sc of feature.scenarios) {
    const block = scenarioToGherkin(sc)
    const node = el('div', { class: 'scenario' },
      el('div', { class: 'scenario-head' },
        el('div', { class: 'scenario-name' }, sc.name || '(unnamed)'),
        el('div', { class: 'scenario-actions' },
          el('button', { class: 'btn-ghost', onclick: () => openEditor(feature.path, sc.name, block) }, 'Edit'),
          el('button', { class: 'btn-ghost danger', onclick: () => deleteScenario(feature.path, sc.name) }, 'Delete'),
        ),
      ),
      renderTags(sc.tags),
      el('ol', { class: 'steps' }, ...sc.steps.map(st =>
        el('li', {},
          el('span', { class: 'kw kw-' + st.keyword }, st.keyword),
          el('span', { class: 'text', html: highlightPlaceholders(st.text) }),
          stepNeedsLocatorPick(st.text)
            ? el('button', { class: 'step-suggest', title: 'Suggest a locator from the live DOM', onclick: () => openLocatorSuggest(feature, sc, st) }, '🔍 suggest')
            : null,
        ),
      )),
    )
    $content.appendChild(node)
  }

  const addBtn = el('button', { class: 'btn-add', onclick: () => openEditor(feature.path, null, defaultNewScenario()) }, '+ New Scenario')
  $content.appendChild(addBtn)
}

function stepNeedsLocatorPick (text) {
  // Heuristic: steps that include a quoted argument or a "<placeholder>"
  // are candidates for locator-suggestion. We surface a button next to
  // these so the user can probe the destination DOM.
  return /"[^"]*"|<[^>]+>/.test(text)
}

function inferLocatorKind (text) {
  if (/link|navigate|click/i.test(text)) return 'link'
  if (/heading|title|reads|see the heading/i.test(text)) return 'heading'
  if (/field|enter|fill|focus/i.test(text)) return 'input'
  if (/button|submit|press/i.test(text)) return 'button'
  return 'button'
}

function extractQuotedArg (text) {
  const m = text.match(/"([^"]*)"/)
  return m ? m[1] : ''
}

function defaultNewScenario () {
  return [
    '  @new',
    '  Scenario: my new scenario',
    '    Given I open the landing page',
    '    Then I see the heading "Welcome"',
  ].join('\n')
}

function openEditor (featurePath, currentName, initialGherkin) {
  const overlay = el('div', { class: 'modal-overlay' })
  const validateMsg = el('div', { class: 'validate-msg' })
  const textarea = el('textarea', { class: 'editor', spellcheck: 'false' })
  textarea.value = initialGherkin

  async function doValidate () {
    try {
      const res = await fetchJSON('/api/validate-scenario', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gherkin: textarea.value }),
      })
      validateMsg.textContent = res.valid ? 'Valid ✓' : 'Invalid: ' + res.error
      validateMsg.className = 'validate-msg ' + (res.valid ? 'ok' : 'err')
    } catch (e) {
      validateMsg.textContent = 'Validate failed: ' + e.message
      validateMsg.className = 'validate-msg err'
    }
  }

  async function doSave () {
    try {
      const url = currentName
        ? '/api/scenario?feature=' + encodeURIComponent(featurePath) + '&name=' + encodeURIComponent(currentName)
        : '/api/scenario?feature=' + encodeURIComponent(featurePath)
      const res = await fetch(url, {
        method: currentName ? 'PATCH' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gherkin: textarea.value }),
      })
      if (!res.ok) {
        validateMsg.textContent = 'Save failed: ' + (await res.text())
        validateMsg.className = 'validate-msg err'
        return
      }
      overlay.remove()
      await openFeature(featurePath)
    } catch (e) {
      validateMsg.textContent = 'Save failed: ' + e.message
      validateMsg.className = 'validate-msg err'
    }
  }

  const modal = el('div', { class: 'modal' },
    el('h2', {}, currentName ? `Edit "${currentName}"` : 'New Scenario'),
    textarea,
    validateMsg,
    el('div', { class: 'modal-actions' },
      el('button', { class: 'btn-ghost', onclick: () => overlay.remove() }, 'Cancel'),
      el('button', { class: 'btn-ghost', onclick: doValidate }, 'Validate'),
      el('button', { class: 'btn-primary', onclick: doSave }, currentName ? 'Save' : 'Create'),
    ),
  )
  overlay.appendChild(modal)
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove() })
  document.body.appendChild(overlay)
  textarea.focus()
}

function openLocatorSuggest (feature, scenario, step) {
  const overlay = el('div', { class: 'modal-overlay' })
  const kindSelect = el('select', { class: 'locator-kind' },
    ...['button', 'input', 'link', 'heading'].map(k => el('option', { value: k }, k)))
  kindSelect.value = inferLocatorKind(step.text)
  const hintInput = el('input', { class: 'locator-hint', value: extractQuotedArg(step.text) || '' })
  const urlInput = el('input', { class: 'locator-url', placeholder: 'https://example.com/page-to-probe' })

  // Pre-populate URL guess: prefer the journey landing URL from the
  // feature narrative if it contains one; otherwise leave for the user.
  const m = (feature.narrative || '').match(/https?:\/\/\S+/)
  if (m) urlInput.value = m[0].replace(/[)\.,;]$/, '')

  const results = el('div', { class: 'locator-results' })

  async function doSuggest () {
    results.replaceChildren(el('div', { class: 'meta' }, 'Probing…'))
    try {
      const res = await fetchJSON('/api/locator-candidates', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          url: urlInput.value.trim(),
          kind: kindSelect.value,
          hint: hintInput.value.trim(),
        }),
      })
      if (!res.candidates || !res.candidates.length) {
        results.replaceChildren(el('div', { class: 'meta' }, 'No matches in the probed DOM.'))
        return
      }
      results.replaceChildren(...res.candidates.map(c =>
        el('div', { class: 'locator-row' },
          el('div', { class: 'locator-score' }, c.score.toFixed(2)),
          el('div', { class: 'locator-body' },
            el('div', { class: 'locator-name' }, c.name || '(no name)'),
            el('code', { class: 'locator-selector' }, c.selector),
          ),
          el('button', { class: 'btn-ghost', onclick: () => { navigator.clipboard.writeText(c.selector); overlay.querySelector('.copied').textContent = '✓ copied' } }, 'Copy'),
        ),
      ))
    } catch (e) {
      results.replaceChildren(el('div', { class: 'error' }, 'Probe failed: ' + e.message))
    }
  }

  const modal = el('div', { class: 'modal modal-wide' },
    el('h2', {}, 'Suggest locator'),
    el('div', { class: 'locator-form' },
      el('label', {}, 'Probe URL', urlInput),
      el('label', {}, 'Kind', kindSelect),
      el('label', {}, 'Hint', hintInput),
    ),
    el('div', { class: 'modal-actions' },
      el('span', { class: 'copied meta' }),
      el('button', { class: 'btn-ghost', onclick: () => overlay.remove() }, 'Close'),
      el('button', { class: 'btn-primary', onclick: doSuggest }, 'Probe'),
    ),
    results,
  )
  overlay.appendChild(modal)
  overlay.addEventListener('click', e => { if (e.target === overlay) overlay.remove() })
  document.body.appendChild(overlay)
  if (urlInput.value) doSuggest()
}

async function deleteScenario (featurePath, name) {
  if (!confirm(`Delete scenario "${name}"?\n\nA backup is saved under tests/e2e/.reviewqa-history/ for recovery.`)) return
  try {
    const res = await fetch('/api/scenario?feature=' + encodeURIComponent(featurePath) + '&name=' + encodeURIComponent(name), { method: 'DELETE' })
    if (!res.ok) {
      alert('Delete failed: ' + (await res.text()))
      return
    }
    await openFeature(featurePath)
  } catch (e) {
    alert('Delete failed: ' + e.message)
  }
}

async function openFeature (path) {
  activeFeature = path
  activeDoc = null
  refreshSidebarSelection()
  try {
    const data = await fetchJSON('/api/feature?path=' + encodeURIComponent(path))
    renderFeature(data.feature, data.gherkin)
    // Also refresh the sidebar scenario counts after edits.
    await refreshSidebarCounts()
  } catch (err) {
    $content.replaceChildren(el('div', { class: 'error' }, 'Failed to load feature: ' + err.message))
  }
}

async function refreshSidebarCounts () {
  try {
    const project = await fetchJSON('/api/project')
    for (const f of project.features) {
      const li = $featureList.querySelector(`li[data-path="${f.path}"]`)
      if (li) {
        const meta = li.querySelector('.meta')
        if (meta) meta.textContent = `${f.scenarios} scenario${f.scenarios === 1 ? '' : 's'}${f.tags && f.tags.length ? ' · ' + f.tags.join(' ') : ''}`
      }
    }
  } catch (_) { /* non-fatal */ }
}

async function openDoc (path, kind) {
  activeDoc = path
  activeFeature = null
  refreshSidebarSelection()
  try {
    const data = await fetchJSON('/api/doc?path=' + encodeURIComponent(path))
    const isHTML = path.endsWith('.html')
    const inner = isHTML
      ? el('div', { class: 'doc-content', html: data.body })
      : el('pre', { class: 'raw-block' }, data.body)
    $content.replaceChildren(
      el('div', { class: 'feature-header' },
        el('h1', {}, kind === 'catalogue' ? 'Test catalogue' : kind === 'summary' ? 'Stakeholder summary' : kind === 'findings' ? 'Bug-discovery ledger' : 'Doc'),
        el('div', { class: 'path' }, path),
      ),
      inner,
    )
  } catch (err) {
    $content.replaceChildren(el('div', { class: 'error' }, 'Failed to load doc: ' + err.message))
  }
}

function refreshSidebarSelection () {
  for (const li of $featureList.querySelectorAll('li')) {
    li.classList.toggle('active', li.dataset.path === activeFeature)
  }
  for (const li of $docList.querySelectorAll('li')) {
    li.classList.toggle('active', li.dataset.path === activeDoc)
  }
}

async function init () {
  try {
    const project = await fetchJSON('/api/project')
    $projectName.textContent = project.name
    document.title = `reviewqa · ${project.name}`

    $featureList.replaceChildren()
    if (!project.features.length) {
      $featureList.appendChild(el('li', { class: 'empty' }, 'No .feature files found'))
    } else {
      for (const f of project.features) {
        const meta = `${f.scenarios} scenario${f.scenarios === 1 ? '' : 's'}${f.tags && f.tags.length ? ' · ' + f.tags.join(' ') : ''}`
        $featureList.appendChild(el('li', { 'data-path': f.path, onclick: () => openFeature(f.path) },
          el('div', {}, f.name || f.path.split('/').pop()),
          el('span', { class: 'meta' }, meta),
        ))
      }
    }

    $docList.replaceChildren()
    if (!project.docs || !project.docs.length) {
      $docList.appendChild(el('li', { class: 'empty' }, '—'))
    } else {
      for (const d of project.docs) {
        $docList.appendChild(el('li', { 'data-path': d.path, onclick: () => openDoc(d.path, d.kind) },
          el('div', {}, d.title),
          el('span', { class: 'meta' }, d.path),
        ))
      }
    }
  } catch (err) {
    $content.replaceChildren(el('div', { class: 'error' }, 'Failed to load project: ' + err.message))
  }
}

init()
