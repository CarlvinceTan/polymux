/**
 * Cron expressions, for the schedules the pickers cannot say.
 *
 * The frequency pickers cover what people actually ask for, and they cover it
 * legibly. They cannot express "every 15 minutes between 9 and 5 on weekdays",
 * and rather than grow a picker for every such shape, a schedule can carry a
 * cron expression instead. Both the renderer and the main process need to read
 * one — the renderer to validate what is being typed and preview it, the
 * scheduler to fire on it — so the parser lives here, in the package they
 * share, rather than in either of them.
 *
 * Standard five-field cron: minute, hour, day-of-month, month, day-of-week.
 */

export interface CronFields {
  minutes: number[];
  hours: number[];
  daysOfMonth: number[];
  months: number[];
  daysOfWeek: number[];
  /**
   * Standard cron's oddest rule, and the one every implementation has to make
   * a decision about: when BOTH day-of-month and day-of-week are restricted,
   * a day matching EITHER runs — not both. `*` in a field is not a
   * restriction, so the usual "on the 1st" and "on Monday" keep meaning what
   * they look like.
   */
  dayUnion: boolean;
}

const MONTH_NAMES = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
const DAY_NAMES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

/** Shorthands every cron accepts, spelled out rather than special-cased later. */
const ALIASES: Record<string, string> = {
  "@yearly": "0 0 1 1 *",
  "@annually": "0 0 1 1 *",
  "@monthly": "0 0 1 * *",
  "@weekly": "0 0 * * 0",
  "@daily": "0 0 * * *",
  "@midnight": "0 0 * * *",
  "@hourly": "0 * * * *",
};

export class CronError extends Error {}

interface FieldSpec {
  min: number;
  max: number;
  names?: string[];
  label: string;
}

const FIELDS: FieldSpec[] = [
  {min: 0, max: 59, label: "minute"},
  {min: 0, max: 23, label: "hour"},
  {min: 1, max: 31, label: "day of month"},
  {min: 1, max: 12, names: MONTH_NAMES, label: "month"},
  {min: 0, max: 6, names: DAY_NAMES, label: "day of week"},
];

/**
 * Parses an expression, or throws `CronError` with something a person can act
 * on. Callers that only want to know whether it is valid should use
 * `cronIsValid`.
 */
export function parseCron(expression: string): CronFields {
  const trimmed = expression.trim().toLowerCase();
  if (!trimmed) throw new CronError("Enter a cron expression");
  const normalised = ALIASES[trimmed] ?? trimmed;
  const parts = normalised.split(/\s+/);
  if (parts.length !== 5)
    throw new CronError(
      `A cron expression has five fields (minute hour day-of-month month day-of-week); this has ${parts.length}`,
    );
  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts;
  return {
    minutes: parseField(minute, FIELDS[0]),
    hours: parseField(hour, FIELDS[1]),
    daysOfMonth: parseField(dayOfMonth, FIELDS[2]),
    months: parseField(month, FIELDS[3]),
    daysOfWeek: parseField(dayOfWeek, FIELDS[4]),
    dayUnion: !isUnrestricted(dayOfMonth) && !isUnrestricted(dayOfWeek),
  };
}

export function cronIsValid(expression: string): boolean {
  try {
    parseCron(expression);
    return true;
  } catch {
    return false;
  }
}

/** The parse error, or null when the expression is good. */
export function cronError(expression: string): string | null {
  try {
    parseCron(expression);
    return null;
  } catch (error) {
    return error instanceof CronError ? error.message : "Invalid cron expression";
  }
}

/** `*` and `?` mean "any", which is what makes the day-union rule apply. */
function isUnrestricted(field: string): boolean {
  return field === "*" || field === "?";
}

function parseField(field: string, spec: FieldSpec): number[] {
  const values = new Set<number>();
  for (const term of field.split(",")) {
    if (!term) throw new CronError(`Empty ${spec.label} in the expression`);
    const [range, stepText] = term.split("/");
    if (stepText !== undefined && !/^\d+$/.test(stepText))
      throw new CronError(`Step in the ${spec.label} must be a whole number`);
    const step = stepText === undefined ? 1 : Number(stepText);
    if (step < 1) throw new CronError(`Step in the ${spec.label} must be at least 1`);

    let from: number;
    let to: number;
    if (isUnrestricted(range)) {
      from = spec.min;
      to = spec.max;
    } else if (range.includes("-")) {
      const [left, right] = range.split("-");
      from = readValue(left, spec);
      to = readValue(right, spec);
      // A wrapping range — "fri-mon" — is how weekends get written, so it is
      // read as two runs rather than rejected.
      if (from > to) {
        for (let value = from; value <= spec.max; value += 1) values.add(value);
        for (let value = spec.min; value <= to; value += 1) values.add(value);
        continue;
      }
    } else {
      from = readValue(range, spec);
      // A bare value with a step counts on from it: "5/10" is 5, 15, 25…
      to = stepText === undefined ? from : spec.max;
    }
    for (let value = from; value <= to; value += step) values.add(value);
  }
  const list = [...values].sort((a, b) => a - b);
  if (!list.length) throw new CronError(`Nothing matches in the ${spec.label}`);
  return list;
}

function readValue(text: string, spec: FieldSpec): number {
  const named = spec.names?.indexOf(text);
  // Month names are 1-based, weekday names 0-based, which their `min` says.
  if (named !== undefined && named >= 0) return named + spec.min;
  if (!/^\d+$/.test(text)) throw new CronError(`"${text}" is not a valid ${spec.label}`);
  const value = Number(text);
  // Cron writes Sunday as either 0 or 7.
  if (spec.label === "day of week" && value === 7) return 0;
  if (value < spec.min || value > spec.max)
    throw new CronError(`${spec.label} must be between ${spec.min} and ${spec.max}`);
  return value;
}

/**
 * The first firing strictly after `after`, in the expression's own zone, or
 * null when nothing matches inside the search horizon (a date like 30 February
 * that no year can satisfy).
 *
 * Days are walked rather than minutes: a yearly expression would otherwise
 * take half a million steps to find its next run.
 */
export function nextCronRun(
  expression: string,
  after: number,
  timeZone: string,
): number | null {
  const fields = parseCron(expression);
  const start = zonedParts(timeZone, after);
  // Five years is past every real expression and still bounded, so a nonsense
  // one fails fast instead of spinning.
  for (let step = 0; step <= 366 * 5; step += 1) {
    const day = new Date(Date.UTC(start.year, start.month, start.day) + step * 86_400_000);
    const year = day.getUTCFullYear();
    const month = day.getUTCMonth();
    const date = day.getUTCDate();
    if (!cronMatchesDay(fields, month + 1, date, day.getUTCDay())) continue;
    for (const hour of fields.hours) {
      for (const minute of fields.minutes) {
        const candidate = zonedEpoch(timeZone, {year, month, day: date, hour, minute});
        if (candidate > after) return candidate;
      }
    }
  }
  return null;
}

/** Wall-clock fields of an instant, read in a zone. */
export function zonedParts(
  zone: string,
  epochMs: number,
): {year: number; month: number; day: number; hour: number; minute: number; weekday: number} {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    hour12: false,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    weekday: "short",
  }).formatToParts(new Date(epochMs));
  const field = (type: string) => parts.find((part) => part.type === type)?.value ?? "0";
  const weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  return {
    year: Number(field("year")),
    month: Number(field("month")) - 1,
    day: Number(field("day")),
    // Some ICU versions write midnight as "24" under hour12: false.
    hour: Number(field("hour")) % 24,
    minute: Number(field("minute")),
    weekday: Math.max(0, weekdays.indexOf(field("weekday"))),
  };
}

/**
 * The instant at which a zone's clock reads the given wall time. Guessed with
 * one offset and re-read with the offset that actually applies there, which
 * settles every real zone including the hour daylight saving adds or removes.
 */
export function zonedEpoch(
  zone: string,
  wall: {year: number; month: number; day: number; hour: number; minute: number},
): number {
  const naive = Date.UTC(wall.year, wall.month, wall.day, wall.hour, wall.minute);
  let guess = naive - zoneOffset(zone, naive);
  guess = naive - zoneOffset(zone, guess);
  return guess;
}

function zoneOffset(zone: string, epochMs: number): number {
  const wall = zonedParts(zone, epochMs);
  const asUtc = Date.UTC(wall.year, wall.month, wall.day, wall.hour, wall.minute);
  return asUtc - Math.floor(epochMs / 60_000) * 60_000;
}

/** Whether a date's day satisfies the expression, under the day-union rule. */
export function cronMatchesDay(
  fields: CronFields,
  month: number,
  dayOfMonth: number,
  dayOfWeek: number,
): boolean {
  if (!fields.months.includes(month)) return false;
  const byDate = fields.daysOfMonth.includes(dayOfMonth);
  const byDay = fields.daysOfWeek.includes(dayOfWeek);
  return fields.dayUnion ? byDate || byDay : byDate && byDay;
}
