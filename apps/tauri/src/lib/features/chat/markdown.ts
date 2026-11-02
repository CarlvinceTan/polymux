import DOMPurify from 'dompurify'
import { translate } from '../../../i18n'
import { marked } from 'marked'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import css from 'highlight.js/lib/languages/css'
import diff from 'highlight.js/lib/languages/diff'
import go from 'highlight.js/lib/languages/go'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import markdown from 'highlight.js/lib/languages/markdown'
import php from 'highlight.js/lib/languages/php'
import python from 'highlight.js/lib/languages/python'
import ruby from 'highlight.js/lib/languages/ruby'
import rust from 'highlight.js/lib/languages/rust'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'

// Only the languages Polymux realistically shows are registered, so the bundle
// carries a curated set rather than every grammar highlight.js ships.
for (const [name, language] of Object.entries({
  bash, css, diff, go, java, javascript, json, markdown, php, python, ruby, rust, sql, typescript, xml, yaml,
})) hljs.registerLanguage(name, language)

const languageNames: Record<string, string> = {
  js: 'JavaScript', javascript: 'JavaScript', ts: 'TypeScript', typescript: 'TypeScript',
  jsx: 'JSX', tsx: 'TSX', py: 'Python', python: 'Python', rb: 'Ruby', go: 'Go', rs: 'Rust',
  java: 'Java', cs: 'C#', cpp: 'C++', c: 'C', php: 'PHP', swift: 'Swift', kt: 'Kotlin',
  sh: 'Shell', bash: 'Shell', zsh: 'Shell', sql: 'SQL', json: 'JSON', yaml: 'YAML', yml: 'YAML',
  html: 'HTML', css: 'CSS', scss: 'SCSS', md: 'Markdown', markdown: 'Markdown', svelte: 'Svelte',
  vue: 'Vue', diff: 'Diff', xml: 'XML', toml: 'TOML',
}

const languageAliases: Record<string, string> = {
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  ts: 'typescript', tsx: 'typescript', svelte: 'xml', vue: 'xml', html: 'xml',
  py: 'python', rb: 'ruby', rs: 'rust', sh: 'bash', zsh: 'bash', shell: 'bash',
  yml: 'yaml', md: 'markdown',
}

/** Reads the fenced language token from the code element's class. */
export function codeToken(pre: Element): string {
  const className = pre.querySelector('code')?.getAttribute('class') ?? ''
  return (/language-([A-Za-z0-9+#-]+)/.exec(className)?.[1] ?? '').toLowerCase()
}

export function codeLanguage(pre: Element): string {
  const token = codeToken(pre)
  if (!token) return 'Code'
  return languageNames[token] ?? token.toUpperCase()
}

/**
 * Highlighting runs on the sanitized DOM's text content and emits only
 * class-bearing spans, so it cannot reintroduce anything DOMPurify removed.
 */
function highlightCode(pre: Element): void {
  const code = pre.querySelector('code')
  if (!code) return
  const source = code.textContent ?? ''
  if (!source.trim()) return
  const token = codeToken(pre)
  const language = languageAliases[token] ?? token
  try {
    const highlighted = language && hljs.getLanguage(language)
      ? hljs.highlight(source, { language, ignoreIllegals: true })
      : hljs.highlightAuto(source)
    code.innerHTML = highlighted.value
    code.classList.add('hljs')
  } catch {
    // Leave the plain escaped code in place if highlighting fails.
  }
}

/**
 * Web links read as running text rather than raw urls: the site's own icon,
 * then a short label, underlined like any other link instead of boxed into a
 * chip that interrupts the sentence. The icon is the site's own — fetched
 * from the site, never through a third-party favicon service, so rendering a
 * link tells nobody the user was sent it. It sits behind a globe that only gets replaced once the real
 * icon loads, which is also the fallback for a site that has none.
 *
 * The image is left without a `src`: the renderer's CSP allows no remote
 * images, so the bytes come from the main process instead, and the site the
 * icon belongs to is recorded here for whoever mounts this html to ask for.
 */
function decorateLink(document: Document, anchor: HTMLAnchorElement): void {
  let url: URL
  try {
    url = new URL(anchor.getAttribute('href') ?? '')
  } catch {
    return
  }
  if (url.protocol === 'file:') return decorateFileLink(document, anchor, url)
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return

  const text = anchor.textContent?.trim() ?? ''
  // An autolinked url is its own text; a hostname reads better as a mention.
  const label = !text || text === anchor.getAttribute('href') || /^https?:\/\//i.test(text)
    ? url.hostname.replace(/^www\./, '')
    : text

  const icon = document.createElement('span')
  icon.className = 'link-icon'
  icon.innerHTML = GLOBE_SVG
  const favicon = document.createElement('img')
  favicon.className = 'link-favicon'
  favicon.dataset.linkFavicon = url.origin
  favicon.alt = ''
  icon.append(favicon)

  const name = document.createElement('span')
  name.className = 'link-label'
  name.textContent = label

  anchor.classList.add('markdown-link')
  anchor.replaceChildren(icon, name)
}

/**
 * A file the agent wrote reads the same as a web link — same icon-then-label
 * shape, same underline — so a reply that cites a page and a file does not
 * switch visual language halfway through. Only the icon differs (a document
 * rather than a favicon) and the label is the file's own name, since a full
 * path is unreadable inline. Clicks are routed to the shell by whoever mounts
 * this html; the class is what tells them which of the two this is.
 */
function decorateFileLink(document: Document, anchor: HTMLAnchorElement, url: URL): void {
  let name: string
  try {
    name = decodeURIComponent(url.pathname).split('/').filter(Boolean).pop() ?? ''
  } catch {
    name = url.pathname.split('/').filter(Boolean).pop() ?? ''
  }
  if (!name) return

  const text = anchor.textContent?.trim() ?? ''
  const icon = document.createElement('span')
  icon.className = 'link-icon'
  icon.innerHTML = FILE_SVG

  const label = document.createElement('span')
  label.className = 'link-label'
  // The author's own words win; the basename is the fallback for a bare path.
  label.textContent = !text || text === anchor.getAttribute('href') ? name : text

  anchor.classList.add('markdown-link', 'markdown-file-link')
  anchor.dataset.filePath = decodeURIComponentSafe(url.pathname)
  anchor.replaceChildren(icon, label)
}

function decodeURIComponentSafe(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

// The same document mark the Icon component draws, so a file link matches the
// file icons used everywhere else in the app.
const FILE_SVG = '<svg class="link-glyph" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>'

// The same globe the Icon component draws, so a link's fallback icon matches
// every other globe in the app.
const GLOBE_SVG = '<svg class="link-globe" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18"/></svg>'

/**
 * DOMPurify's default allowlist has no `file:`, so a link to a file the agent
 * wrote lost its href and rendered as dead text. This is that default with
 * `file` added and nothing else removed — notably still no `javascript:` or
 * `data:`. Surviving sanitisation only makes the link clickable; the main
 * process still resolves the path and refuses anything that is not an existing
 * regular file before the shell sees it.
 */
const ALLOWED_URI =
  /^(?:(?:(?:f|ht)tps?|file|mailto|tel|callto|sms|cid|xmpp):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i

export function renderMarkdown(source: string): string {
  if (!source.trim()) return ''
  const rendered = marked.parse(source, {
    async: false,
    breaks: true,
    gfm: true,
  }) as string

  const sanitized = DOMPurify.sanitize(rendered, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input', 'button', 'textarea', 'select', 'svg', 'math'],
    FORBID_ATTR: ['style', 'srcset'],
    ALLOWED_URI_REGEXP: ALLOWED_URI,
  })

  const document = new DOMParser().parseFromString(sanitized, 'text/html')
  for (const anchor of document.querySelectorAll('a')) {
    anchor.setAttribute('rel', 'noopener noreferrer')
    decorateLink(document, anchor)
  }
  for (const pre of document.querySelectorAll('pre')) {
    const wrapper = document.createElement('div')
    wrapper.className = 'markdown-code-block'

    // A titled header carrying the fenced language and its own Copy action,
    // rather than a control floating over the first line of code.
    const header = document.createElement('div')
    header.className = 'markdown-code-header'
    const label = document.createElement('span')
    label.className = 'markdown-code-language'
    label.textContent = codeLanguage(pre)
    highlightCode(pre)
    const copy = document.createElement('button')
    copy.type = 'button'
    copy.className = 'markdown-code-copy'
    copy.dataset.markdownCopy = ''
    copy.setAttribute('aria-label', translate('markdown.copyCode'))
    copy.textContent = translate('common.copy')
    header.append(label, copy)

    pre.replaceWith(wrapper)
    wrapper.append(header, pre)
  }
  return document.body.innerHTML
}
