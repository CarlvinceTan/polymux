import {marked} from 'marked';

/** Reply-local metadata, kept in durable text but displayed outside the prose. */
export function extractMemoryCitations(source: string): {text: string; memories: string[]} {
  let offset = 0;
  for (const token of marked.lexer(source)) {
    const raw = token.raw;
    if ((token.type === 'html' || token.type === 'paragraph') && /^<polymux-memories>/.test(raw) && !source.slice(offset + raw.length).trim()) {
      const body = raw.slice('<polymux-memories>'.length);
      const end = body.indexOf('</polymux-memories>');
      let memories: string[] = [];
      if (end >= 0) {
        try {
          const value: unknown = JSON.parse(body.slice(0, end));
          if (Array.isArray(value)) memories = [...new Set(value.filter((item): item is string => typeof item === 'string').map(item => item.trim()).filter(Boolean))];
        } catch { /* Incomplete or malformed metadata never becomes reply prose. */ }
      }
      return {text: source.slice(0, offset).trimEnd(), memories};
    }
    offset += raw.length;
  }
  return {text: source, memories: []};
}
