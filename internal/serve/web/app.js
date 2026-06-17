// reviewqa serve — Phase B frontend (read + Scenario CRUD).
// Plain vanilla JS so the project keeps its no-build posture.

const $featureList = document.querySelector('[data-feature-list]')
const $docList = document.querySelector('[data-doc-list]')
const $content = document.querySelector('[data-content]')
const $projectName = document.querySelector('[data-project-name]')

let activeFeature = null
let activeDoc = null
let viewMode = 'pretty'
let llmStatus = { enabled: false, model: '', endpoint: '' }

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
          el('button', {
            class: 'btn-ghost' + (llmStatus.enabled ? '' : ' disabled'),
            title: llmStatus.enabled ? 'Chat with the LLM about this scenario' : 'Set REVIEWQA_LLM to enable',
            onclick: () => llmStatus.enabled && openChat(feature, sc, block),
          }, '💬 Chat'),
          el('button', { class: 'btn-ghost', onclick: () => openEditor(feature.path, sc.name, block, feature) }, 'Edit'),
          el('button', { class: 'btn-ghost danger', onclick: () => deleteScenario(feature.path, sc.name) }, 'Delete'),
        ),
      ),
      renderTags(sc.tags),
      el('ol', { class: 'steps' }, ...sc.steps.map(st =>
        el('li', { class: st.valid === false ? 'step-invalid' : '', title: st.valid === false ? 'This step text does not match any registered playwright-bdd pattern — it will not run.' : '' },
          el('span', { class: 'kw kw-' + st.keyword }, st.keyword),
          el('span', { class: 'text', html: highlightPlaceholders(st.text) }),
          el('button', { class: 'step-suggest', title: 'Suggest a Playwright locator from the live DOM', onclick: () => openLocatorSuggest(feature, sc, st) }, '🔍 suggest'),
        ),
      )),
    )
    $content.appendChild(node)
  }

  const addBtn = el('button', { class: 'btn-add', onclick: () => openEditor(feature.path, null, defaultNewScenario(), feature) }, '+ New Scenario')
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

function openEditor (featurePath, currentName, initialGherkin, featureCtx) {
  const overlay = el('div', { class: 'modal-overlay' })
  const validateMsg = el('div', { class: 'validate-msg' })
  const textarea = el('textarea', { class: 'editor', spellcheck: 'false' })
  textarea.value = initialGherkin
  const composeUrl = el('input', { class: 'locator-url', placeholder: 'Destination URL — required for AI compose' })
  if (featureCtx && featureCtx.narrative) {
    const m = featureCtx.narrative.match(/https?:\/\/\S+/)
    if (m) composeUrl.value = m[0].replace(/[)\.,;]$/, '')
  }
  const composeStatus = el('span', { class: 'meta' })

  async function doCompose () {
    const url = composeUrl.value.trim()
    if (!url) {
      composeStatus.textContent = 'pick a destination URL first'
      return
    }
    composeStatus.textContent = 'composing…'
    try {
      const res = await fetchJSON('/api/compose-steps', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gherkin: textarea.value, url }),
      })
      textarea.value = res.gherkin
      composeStatus.textContent = res.model ? `composed via ${res.model}` : (res.notes || 'composed (deterministic)')
    } catch (e) {
      composeStatus.textContent = 'compose failed: ' + e.message
    }
  }

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
    el('div', { class: 'compose-row' },
      el('div', { class: 'compose-row-label' }, '🤖 AI compose'),
      composeUrl,
      el('button', { class: 'btn-ghost', onclick: doCompose }, 'Compose'),
      composeStatus,
    ),
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

  const hintHelp = el('div', { class: 'meta locator-help' }, hintInput.value
    ? 'Probing for an element matching this hint.'
    : 'Type the visible text of the element you want — link label, button name, heading copy, field label.')
  hintInput.addEventListener('input', () => {
    hintHelp.textContent = hintInput.value
      ? 'Probing for an element matching this hint.'
      : 'Type the visible text of the element you want — link label, button name, heading copy, field label.'
  })
  const modal = el('div', { class: 'modal modal-wide' },
    el('h2', {}, 'Suggest a Playwright locator'),
    el('div', { class: 'locator-form' },
      el('label', {}, 'Probe URL', urlInput),
      el('label', {}, 'Kind', kindSelect),
      el('label', {}, 'Hint', hintInput),
      hintHelp,
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

function chatStorageKey (featurePath, scenarioName) {
  return `reviewqa-chat::${featurePath}::${scenarioName}`
}

function loadChatHistory (featurePath, scenarioName) {
  try {
    const raw = localStorage.getItem(chatStorageKey(featurePath, scenarioName))
    return raw ? JSON.parse(raw) : []
  } catch (_) {
    return []
  }
}

function saveChatHistory (featurePath, scenarioName, history) {
  try {
    localStorage.setItem(chatStorageKey(featurePath, scenarioName), JSON.stringify(history))
  } catch (_) { /* ignore quota errors */ }
}

function openChat (feature, scenario, currentBlock) {
  const drawer = el('aside', { class: 'chat-drawer' })
  const transcript = el('div', { class: 'chat-transcript' })
  const composer = el('textarea', { class: 'chat-composer', placeholder: 'e.g. add a step asserting the success toast is visible', spellcheck: 'true' })
  const urlInput = el('input', { class: 'locator-url chat-url', placeholder: 'destination URL (optional, gives DOM grounding)' })
  const m = (feature.narrative || '').match(/https?:\/\/\S+/)
  if (m) urlInput.value = m[0].replace(/[)\.,;]$/, '')

  let history = loadChatHistory(feature.path, scenario.name)
  let currentScenarioBlock = currentBlock
  let pendingProposed = null

  function renderTranscript () {
    transcript.replaceChildren()
    if (history.length === 0) {
      transcript.appendChild(el('div', { class: 'chat-empty' }, 'Talk to the assistant about this Scenario. Ask it to change a step, add an assertion, rename, or simply describe what you want — it will propose an updated block you can Apply.'))
    }
    for (const msg of history) {
      const bubble = el('div', { class: 'chat-bubble chat-' + msg.role }, msg.content)
      transcript.appendChild(bubble)
    }
    if (pendingProposed) {
      const wrap = el('div', { class: 'chat-proposed' },
        el('div', { class: 'chat-proposed-head' },
          el('span', { class: 'label' }, pendingProposed.valid ? 'Proposed update' : 'Proposed update — INVALID'),
          el('button', {
            class: 'btn-primary' + (pendingProposed.valid ? '' : ' disabled'),
            onclick: () => pendingProposed.valid && applyProposed(),
          }, 'Apply'),
          el('button', { class: 'btn-ghost', onclick: () => { pendingProposed = null; renderTranscript() } }, 'Dismiss'),
        ),
        el('pre', { class: 'raw-block chat-proposed-block' }, pendingProposed.gherkin),
        pendingProposed.notes ? el('div', { class: 'meta' }, pendingProposed.notes) : null,
      )
      transcript.appendChild(wrap)
    }
    transcript.scrollTop = transcript.scrollHeight
  }

  async function applyProposed () {
    if (!pendingProposed || !pendingProposed.valid) return
    try {
      const url = '/api/scenario?feature=' + encodeURIComponent(feature.path) + '&name=' + encodeURIComponent(scenario.name)
      const res = await fetch(url, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ gherkin: pendingProposed.gherkin }),
      })
      if (!res.ok) {
        alert('Apply failed: ' + (await res.text()))
        return
      }
      currentScenarioBlock = pendingProposed.gherkin
      pendingProposed = null
      drawer.remove()
      await openFeature(feature.path)
    } catch (e) {
      alert('Apply failed: ' + e.message)
    }
  }

  async function send () {
    const text = composer.value.trim()
    if (!text) return
    history.push({ role: 'user', content: text })
    composer.value = ''
    renderTranscript()
    saveChatHistory(feature.path, scenario.name, history)
    const thinking = el('div', { class: 'chat-bubble chat-assistant chat-thinking' }, 'thinking…')
    transcript.appendChild(thinking)
    transcript.scrollTop = transcript.scrollHeight
    try {
      const res = await fetchJSON('/api/scenario-chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          scenario: currentScenarioBlock,
          url: urlInput.value.trim(),
          history: history.slice(0, -1),
          user: text,
        }),
      })
      thinking.remove()
      history.push({ role: 'assistant', content: res.assistant })
      saveChatHistory(feature.path, scenario.name, history)
      if (res.proposed) {
        pendingProposed = { gherkin: res.proposed, valid: !!res.valid, notes: res.notes || '' }
      } else {
        pendingProposed = null
      }
      renderTranscript()
    } catch (e) {
      thinking.remove()
      history.push({ role: 'assistant', content: '⚠️ ' + e.message })
      saveChatHistory(feature.path, scenario.name, history)
      renderTranscript()
    }
  }

  function clearChat () {
    if (!confirm('Clear conversation history for this Scenario?')) return
    history = []
    pendingProposed = null
    saveChatHistory(feature.path, scenario.name, history)
    renderTranscript()
  }

  drawer.appendChild(el('div', { class: 'chat-head' },
    el('div', {},
      el('div', { class: 'label' }, 'AI maintenance · ' + (llmStatus.model || 'LLM')),
      el('div', { class: 'chat-scenario-name' }, scenario.name || '(unnamed)'),
    ),
    el('div', { class: 'chat-head-actions' },
      el('button', { class: 'btn-ghost', onclick: clearChat }, 'Clear'),
      el('button', { class: 'btn-ghost', onclick: () => drawer.remove() }, 'Close'),
    ),
  ))
  drawer.appendChild(el('div', { class: 'chat-url-row' }, urlInput))
  drawer.appendChild(transcript)
  drawer.appendChild(el('div', { class: 'chat-composer-row' },
    composer,
    el('button', { class: 'btn-primary', onclick: send }, 'Send'),
  ))

  composer.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      send()
    }
  })

  document.body.appendChild(drawer)
  renderTranscript()
  composer.focus()
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
    llmStatus = await fetchJSON('/api/llm-status').catch(() => llmStatus)
  } catch (_) { /* non-fatal */ }
  try {
    const project = await fetchJSON('/api/project')
    $projectName.textContent = project.name
    document.title = `reviewqa · ${project.name}`
    const $badge = document.querySelector('[data-brand-badge]')
    if ($badge && project.version) $badge.textContent = `v${project.version} · local`

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
