/* global mermaid, renderMathInElement */

async function enhanceMermaid(article) {
  const blocks = Array.from(article.querySelectorAll('pre > code.language-mermaid'))
  if (blocks.length === 0 || typeof mermaid === 'undefined') return

  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'neutral',
    fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif',
  })

  for (const [index, code] of blocks.entries()) {
    const fallback = code.parentElement
    if (!fallback) continue

    const diagram = document.createElement('div')
    diagram.className = 'mermaid-diagram'
    diagram.setAttribute('role', 'img')
    diagram.setAttribute('aria-label', 'Mermaid diagram')
    fallback.replaceWith(diagram)

    try {
      const { svg, bindFunctions } = await mermaid.render(`artifact-mermaid-${index}`, code.textContent || '')
      diagram.innerHTML = svg
      bindFunctions?.(diagram)
    } catch {
      diagram.replaceWith(fallback)
    }
  }
}

async function copyText(text) {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }

  const textarea = document.createElement('textarea')
  textarea.value = text
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.append(textarea)
  textarea.select()
  document.execCommand('copy')
  textarea.remove()
}

function enhanceCodeBlocks(article) {
  const blocks = Array.from(article.querySelectorAll('pre > code'))
  for (const code of blocks) {
    if (code.classList.contains('language-mermaid')) continue
    const pre = code.parentElement
    if (!pre || pre.parentElement?.classList.contains('code-block')) continue

    const languageClass = Array.from(code.classList).find((name) => name.startsWith('language-'))
    const language = languageClass ? languageClass.slice('language-'.length) : 'code'
    const wrapper = document.createElement('div')
    const toolbar = document.createElement('div')
    const label = document.createElement('span')
    const button = document.createElement('button')

    wrapper.className = 'code-block'
    toolbar.className = 'code-block-toolbar'
    label.textContent = language
    button.className = 'code-copy-button'
    button.type = 'button'
    button.textContent = 'Copy'
    button.setAttribute('aria-label', `Copy ${language} code`)
    button.addEventListener('click', async () => {
      try {
        await copyText(code.textContent || '')
        button.textContent = 'Copied'
        button.classList.add('copied')
        window.setTimeout(() => {
          button.textContent = 'Copy'
          button.classList.remove('copied')
        }, 1600)
      } catch {
        button.textContent = 'Copy failed'
      }
    })

    pre.replaceWith(wrapper)
    toolbar.append(label, button)
    wrapper.append(toolbar, pre)
  }
}

function enhanceMath(article) {
  if (typeof renderMathInElement !== 'function') return
  renderMathInElement(article, {
    delimiters: [
      { left: '$$', right: '$$', display: true },
      { left: '\\[', right: '\\]', display: true },
      { left: '$', right: '$', display: false },
      { left: '\\(', right: '\\)', display: false },
    ],
    ignoredTags: ['script', 'noscript', 'style', 'textarea', 'pre', 'code', 'option'],
    throwOnError: false,
    strict: 'warn',
    trust: false,
  })
}

function enhanceTableOfContents(article) {
  const toc = document.querySelector('[data-reader-toc]')
  const list = toc?.querySelector('[data-reader-toc-list]')
  if (!toc || !list) return

  const headings = Array.from(article.querySelectorAll('h1, h2, h3, h4, h5, h6')).filter(
    (heading) => (heading.textContent || '').trim().length > 0,
  )
  if (headings.length === 0) return

  const usedIDs = new Set()
  const links = new Map()
  const uniqueID = (heading, index) => {
    const source = (heading.textContent || '')
      .normalize('NFKC')
      .trim()
      .toLowerCase()
      .replace(/\s+/g, '-')
      .replace(/[^\p{Letter}\p{Number}_-]/gu, '')
    const base = heading.id || source || `section-${index + 1}`
    let candidate = base
    let suffix = 2
    while (usedIDs.has(candidate)) {
      candidate = `${base}-${suffix}`
      suffix += 1
    }
    usedIDs.add(candidate)
    return candidate
  }

  const fragment = document.createDocumentFragment()
  headings.forEach((heading, index) => {
    heading.id = uniqueID(heading, index)
    const level = Number(heading.tagName.slice(1))
    const item = document.createElement('li')
    const link = document.createElement('a')
    link.href = `#${encodeURIComponent(heading.id)}`
    link.textContent = (heading.textContent || '').trim()
    link.style.setProperty('--toc-depth', String(Math.min(Math.max(level - 1, 0), 4)))
    item.append(link)
    fragment.append(item)
    links.set(heading, link)
  })
  list.append(fragment)
  toc.hidden = false
  toc.closest('.page-shell')?.classList.add('has-reader-toc')

  const setActive = (activeHeading) => {
    for (const [heading, link] of links) {
      if (heading === activeHeading) link.setAttribute('aria-current', 'location')
      else link.removeAttribute('aria-current')
    }
  }
  const updateActive = () => {
    let active = headings[0]
    for (const heading of headings) {
      if (heading.getBoundingClientRect().top <= 140) active = heading
      else break
    }
    setActive(active)
  }
  let updateScheduled = false
  document.addEventListener(
    'scroll',
    () => {
      if (updateScheduled) return
      updateScheduled = true
      window.requestAnimationFrame(() => {
        updateActive()
        updateScheduled = false
      })
    },
    { passive: true },
  )
  updateActive()

  if (window.location.hash) {
    window.requestAnimationFrame(() => {
      const targetID = decodeURIComponent(window.location.hash.slice(1))
      document.getElementById(targetID)?.scrollIntoView()
    })
  }
}

function enhanceReaderSettings(article) {
  const root = document.querySelector('[data-reader-settings]')
  const toggle = root?.querySelector('.settings-pill')
  const panel = root?.querySelector('#reader-settings-panel')
  const closeButton = root?.querySelector('[data-reader-close]')
  const fontControl = root?.querySelector('[data-reader-font]')
  const sizeControl = root?.querySelector('[data-reader-size]')
  const sizeOutput = root?.querySelector('[data-reader-size-output]')
  const leadingControl = root?.querySelector('[data-reader-leading]')
  const leadingOutput = root?.querySelector('[data-reader-leading-output]')
  const resetButton = root?.querySelector('[data-reader-reset]')
  if (!root || !toggle || !panel || !fontControl || !sizeControl || !leadingControl) return

  const storageKey = 'artifact-hub:markdown-reader-settings:v1'
  const defaults = { font: 'sans', size: 16, leading: 1.75 }
  const allowedFonts = new Set([
    'sans',
    'pingfang',
    'songti',
    'kaiti',
    'helvetica',
    'arial',
    'verdana',
    'georgia',
    'times',
    'mono',
  ])
  const fontAliases = { serif: 'georgia' }
  const clamp = (value, min, max) => Math.min(max, Math.max(min, value))
  const normalize = (value) => {
    const source = value && typeof value === 'object' ? value : {}
    const requestedFont = fontAliases[source.font] || source.font
    return {
      font: allowedFonts.has(requestedFont) ? requestedFont : defaults.font,
      size: Math.round(clamp(Number(source.size) || defaults.size, 14, 22)),
      leading: Math.round(clamp(Number(source.leading) || defaults.leading, 1.4, 2.1) * 100) / 100,
    }
  }

  let preferences = defaults
  try {
    preferences = normalize(JSON.parse(window.localStorage.getItem(storageKey) || '{}'))
  } catch {
    preferences = defaults
  }

  const apply = () => {
    article.dataset.readerFont = preferences.font
    article.style.setProperty('--reader-font-size', `${preferences.size}px`)
    article.style.setProperty('--reader-line-height', String(preferences.leading))
    fontControl.value = preferences.font
    sizeControl.value = String(preferences.size)
    leadingControl.value = String(preferences.leading)
    if (sizeOutput) sizeOutput.textContent = `${preferences.size} px`
    if (leadingOutput) leadingOutput.textContent = preferences.leading.toFixed(2)
  }

  const save = () => {
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(preferences))
    } catch {
      // Reading preferences remain active for the current page when storage is unavailable.
    }
  }

  const close = ({ restoreFocus = false } = {}) => {
    panel.hidden = true
    toggle.setAttribute('aria-expanded', 'false')
    if (restoreFocus) toggle.focus()
  }

  const open = () => {
    panel.hidden = false
    toggle.setAttribute('aria-expanded', 'true')
    fontControl.focus()
  }

  toggle.addEventListener('click', () => {
    if (panel.hidden) open()
    else close()
  })
  closeButton?.addEventListener('click', () => close({ restoreFocus: true }))
  fontControl.addEventListener('change', () => {
    preferences = normalize({ ...preferences, font: fontControl.value })
    apply()
    save()
  })
  sizeControl.addEventListener('input', () => {
    preferences = normalize({ ...preferences, size: sizeControl.value })
    apply()
    save()
  })
  leadingControl.addEventListener('input', () => {
    preferences = normalize({ ...preferences, leading: leadingControl.value })
    apply()
    save()
  })
  resetButton?.addEventListener('click', () => {
    preferences = defaults
    apply()
    save()
  })
  document.addEventListener('pointerdown', (event) => {
    if (!panel.hidden && event.target instanceof Node && !root.contains(event.target)) close()
  })
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && !panel.hidden) close({ restoreFocus: true })
  })

  apply()
}

const article = document.querySelector('.markdown-body')
if (article) {
  enhanceTableOfContents(article)
  enhanceReaderSettings(article)
  enhanceCodeBlocks(article)
  enhanceMath(article)
  void enhanceMermaid(article)
}
