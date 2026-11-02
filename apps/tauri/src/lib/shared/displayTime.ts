import {activeLocale, translate} from '../../i18n';

/**
 * One way of saying when something happened, shared by every row that carries
 * a stamp — a chat in the hub, a mail envelope, a message bubble — so the same
 * moment reads the same wherever it appears. Everything is in the machine's
 * current timezone, because that is the clock the user is reading it on.
 *
 * The stamp says as much as it has to and no more, widening a step at a time
 * as the moment recedes:
 *
 * - today            `12:34 pm`
 * - yesterday        `Yesterday · 12:34 pm`, or `Yesterday` where space is tight
 * - this week        `Mon 12:34 pm`
 * - this year        `Aug 12, 12:34 pm`
 * - earlier          `2004 Aug 12, 12:34 pm`
 */
export interface DisplayTimeOptions {
  /** The moment to measure against. Defaults to now; tests pass their own. */
  now?: Date;
  /**
   * Set where the stamp sits in a narrow slot. Only yesterday differs: it
   * drops to the bare word rather than truncating mid-clock.
   */
  compact?: boolean;
}

const DAY_MS = 86_400_000;
/** The separator between a word and a clock, per the design: a centre dot. */
const DOT = '·';

/**
 * Parses the shapes the backends hand us. A Date or epoch ms is already
 * unambiguous; a string arrives either as "2026-08-18 01:45+00:00", which
 * needs its space turned into a `T`, or as the RFC 5322 line a sender wrote —
 * "Fri, 7 Aug 2026 02:24:42 +0000" — which that repair would break
 * ("Fri,T7 Aug…"). So the plain reading is tried first and the repair is the
 * fallback rather than the rule.
 */
function parse(value: Date | string | number): Date | null {
  if (value instanceof Date) return Number.isNaN(value.getTime()) ? null : value;
  if (typeof value === 'number') {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? null : date;
  }
  for (const text of [value, value.replace(' ', 'T')]) {
    const date = new Date(text);
    if (!Number.isNaN(date.getTime())) return date;
  }
  return null;
}

function startOfDay(value: Date): Date {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate());
}

/** The clock part of every stamp: 12-hour, as asked for — the interface
 * language only chooses the words around it (`pm` or `PM`), never the cycle. */
function clock(date: Date): string {
  return date.toLocaleTimeString(activeLocale(), {
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  });
}

/**
 * The clock on its own, `3:24 pm`, for the rows that pair a time with their own
 * date wording instead of the widening stamp above. Sharing it is what keeps
 * one moment reading the same in every list. A value that cannot be read comes
 * back empty, so the row drops its clock rather than showing a failure.
 */
export function clockTime(value: Date | string | number): string {
  const date = parse(value);
  return date ? clock(date) : '';
}

/**
 * The stamp for `value`. An unparseable value comes back as its own string
 * rather than as `Invalid Date`, so a row never shows the failure.
 */
export function displayTime(
  value: Date | string | number,
  options: DisplayTimeOptions = {},
): string {
  const date = parse(value);
  if (!date) return typeof value === 'string' ? value : '';

  const now = options.now ?? new Date();
  const days = Math.round((startOfDay(now).getTime() - startOfDay(date).getTime()) / DAY_MS);
  const time = clock(date);
  const locale = activeLocale();

  if (days <= 0) return time;
  if (days === 1) {
    const yesterday = translate('chats.yesterday');
    return options.compact ? yesterday : `${yesterday} ${DOT} ${time}`;
  }
  if (days < 7) return `${date.toLocaleDateString(locale, {weekday: 'short'})} ${time}`;
  const day = date.toLocaleDateString(locale, {month: 'short', day: 'numeric'});
  return date.getFullYear() === now.getFullYear()
    ? `${day}, ${time}`
    : `${date.getFullYear()} ${day}, ${time}`;
}
