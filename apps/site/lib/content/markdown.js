// @ts-check

import createDOMPurify from 'dompurify';
import {marked} from 'marked';

const MARKDOWN_TAGS = [
  'a', 'blockquote', 'br', 'code', 'del', 'em', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'hr', 'img', 'li', 'ol', 'p', 'pre', 'strong', 'table', 'tbody', 'td', 'th',
  'thead', 'tr', 'ul', 'section',
];
const MARKDOWN_ATTRIBUTES = ['align', 'alt', 'class', 'href', 'src', 'start', 'title'];

/**
 * Render repository Markdown without carrying executable HTML into Svelte.
 * @param {string} body
 * @param {import('dompurify').WindowLike} [windowLike]
 */
export function renderSafeMarkdown(body, windowLike = globalThis.window) {
  if (!windowLike) {
    throw new Error('Markdown rendering requires a browser-compatible window.');
  }
  const rendered = /** @type {string} */ (marked.parse(body, {gfm: true}));
  return createDOMPurify(windowLike).sanitize(rendered, {
    ALLOWED_ATTR: MARKDOWN_ATTRIBUTES,
    ALLOWED_TAGS: MARKDOWN_TAGS,
  });
}

/**
 * Render a single line of Markdown (release-note bullets, short labels) as
 * inline HTML, without wrapping the result in a block element.
 * @param {string} text
 * @param {import('dompurify').WindowLike} [windowLike]
 */
export function renderSafeMarkdownInline(text, windowLike = globalThis.window) {
  if (!windowLike) {
    throw new Error('Markdown rendering requires a browser-compatible window.');
  }
  const rendered = /** @type {string} */ (marked.parseInline(text, {gfm: true}));
  return createDOMPurify(windowLike).sanitize(rendered, {
    ALLOWED_ATTR: MARKDOWN_ATTRIBUTES,
    ALLOWED_TAGS: MARKDOWN_TAGS,
  });
}
