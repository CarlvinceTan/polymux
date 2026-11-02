import {derived, get, writable, type Readable} from 'svelte/store';
import {SUPPORTED_LANGUAGES} from '@polymux/protocol';
import en from './locales/en';
import es from './locales/es';
import fr from './locales/fr';
import de from './locales/de';
import pt from './locales/pt';
import it from './locales/it';
import nl from './locales/nl';
import ja from './locales/ja';
import ko from './locales/ko';
import zhHans from './locales/zh-Hans';
import zhHant from './locales/zh-Hant';
import hi from './locales/hi';
import id from './locales/id';
import ms from './locales/ms';
import th from './locales/th';
import vi from './locales/vi';
import ar from './locales/ar';
import ru from './locales/ru';

export type MessageKey = keyof typeof en;
/** Translated catalogs may lag English; missing entries deliberately fall back
 * to the English source text in `t` below. */
export type Catalog = Partial<Record<MessageKey, string>>;

const catalogs: Record<string, Catalog> = {
  en,
  es,
  fr,
  de,
  pt,
  it,
  nl,
  ja,
  ko,
  'zh-Hans': zhHans,
  'zh-Hant': zhHant,
  hi,
  id,
  ms,
  th,
  vi,
  ar,
  ru,
};

/** Written right to left, so the whole document flips direction for these. */
const rtl = new Set(['ar']);

/** Mirrors the choice for `public/theme-boot.js`, which applies `lang`/`dir` in
 * <head>. Settings only arrive over IPC after the first paint, so without a
 * pre-paint hint the splash would open in English and swap under the user. */
const STORAGE_KEY = 'polymux.language';

/** The catalog a preference resolves to. `system` walks the host's language
 * list in order and takes the first one Polymux is translated into — matching
 * on the full tag first (`zh-Hant` before `zh`), then on the base language, so
 * `en-AU` lands on English and `pt-BR` on Portuguese. */
export function resolveLocale(preference: string): string {
  if (preference !== 'system' && catalogs[preference]) return preference;
  if (preference !== 'system') return 'en';
  const requested = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const tag of requested) {
    if (!tag) continue;
    if (catalogs[tag]) return tag;
    const script = matchByScript(tag);
    if (script) return script;
    const base = tag.split('-')[0];
    if (catalogs[base]) return base;
  }
  return 'en';
}

/** Chinese is the one case where the base language is not enough to choose a
 * catalog: `zh-TW` and `zh-HK` are traditional, everything else simplified. */
function matchByScript(tag: string): string | null {
  if (!tag.toLowerCase().startsWith('zh')) return null;
  const parts = tag.split('-').map((part) => part.toLowerCase());
  if (parts.includes('hant') || parts.includes('tw') || parts.includes('hk') || parts.includes('mo'))
    return 'zh-Hant';
  return 'zh-Hans';
}

function storedPreference(): string {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    return stored && SUPPORTED_LANGUAGES.some((item) => item.value === stored) ? stored : 'system';
  } catch {
    return 'system';
  }
}

const preference = writable(storedPreference());

/** The active catalog's tag — `en`, `zh-Hant` and so on, never `system`. */
export const locale: Readable<string> = derived(preference, resolveLocale);

/**
 * Look up a message, substituting `{name}` placeholders from `values`.
 *
 * A key with no entry in the active catalog falls back to English rather than
 * rendering blank, and a key in no catalog at all renders as itself — a visible
 * marker in development that never leaves the user staring at empty chrome.
 */
export const t: Readable<(key: MessageKey, values?: Record<string, string | number>) => string> =
  derived(locale, (tag) => {
    const catalog = catalogs[tag] ?? en;
    return (key: MessageKey, values?: Record<string, string | number>) =>
      interpolate(catalog[key] ?? en[key] ?? key, values);
  });

/**
 * The tag Intl should format dates, numbers and lists with: the language the
 * interface is in, not the host's regional setting. A user reading Polymux in
 * Japanese on an English machine wants Japanese month names in the schedule.
 */
export function activeLocale(): string {
  return get(locale);
}

/**
 * A message whose wording depends on a count, chosen by the language's own
 * plural rules rather than by an English-shaped `n === 1` test — Russian needs
 * a third form at 2–4, Arabic needs six, and Japanese needs one.
 *
 * `key` names the family; the category is appended (`…hours.one`,
 * `…hours.other`). A catalog that does not distinguish a category simply has
 * the same wording under each, so the lookup never has to fall back.
 */
export function plural(
  key: PluralKey,
  count: number,
  values: Record<string, string | number> = {},
): string {
  const category = new Intl.PluralRules(activeLocale()).select(count);
  const specific = `${key}.${category}` as MessageKey;
  const message = specific in en ? specific : (`${key}.other` as MessageKey);
  return translate(message, {count, ...values});
}

/** Message families that carry one entry per plural category. Distributed over
 * the key union, so a family is offered as soon as any of its categories
 * exists. */
export type PluralKey = MessageKey extends infer Key
  ? Key extends `${infer Family}.${PluralCategory}`
    ? Family
    : never
  : never;
type PluralCategory = 'zero' | 'one' | 'two' | 'few' | 'many' | 'other';

/**
 * Ties a derivation to the active language.
 *
 * Some values are built by `Intl` or by `translate` rather than read from the
 * `t` store, and Svelte cannot see into either — so a reactive statement that
 * calls one has no dependency to re-run on. Naming the tag in the expression
 * gives it one:
 *
 *     $: months = withLocale($locale, monthNames());
 */
export function withLocale<T>(_tag: string, value: T): T {
  return value;
}

/** The same lookup outside a component, for module-level code and one-off calls
 * where subscribing to the store would be noise. Callers that need to redraw
 * when the language changes must use the `t` store instead. */
export function translate(key: MessageKey, values?: Record<string, string | number>): string {
  return get(t)(key, values);
}

function interpolate(message: string, values?: Record<string, string | number>): string {
  if (!values) return message;
  return message.replace(/\{(\w+)\}/g, (whole, name: string) =>
    name in values ? String(values[name]) : whole,
  );
}

/**
 * Apply a language preference: remember it, retranslate the UI, and tell the
 * document what it is now written in so the browser hyphenates, quotes and
 * (for Arabic) lays out right to left.
 */
export function applyLanguage(next: string): void {
  preference.set(next);
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // A denied store only costs the pre-paint hint, not the applied language.
  }
  const tag = resolveLocale(next);
  const root = document.documentElement;
  const direction = rtl.has(tag) ? 'rtl' : 'ltr';
  if (root.lang !== tag) root.lang = tag;
  if (root.dir !== direction) root.dir = direction;
}

/**
 * Keep `system` following the host. Electron changes `navigator.languages`
 * when the OS language changes, and the `languagechange` event is the only
 * notice of it; an explicit choice ignores it.
 */
export function startLanguageSync(): () => void {
  const update = () => {
    if (get(preference) === 'system') applyLanguage('system');
  };
  window.addEventListener('languagechange', update);
  applyLanguage(get(preference));
  return () => window.removeEventListener('languagechange', update);
}
