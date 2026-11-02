import assert from "node:assert/strict";
import { test } from "node:test";
import { cronError, nextCronRun, parseCron } from "../src/cron.js";

const ZONE = "Australia/Sydney";

/** What a zone's clock reads at an instant, for asserting on wall time. */
function reads(epoch: number, options: Intl.DateTimeFormatOptions): string {
  return new Intl.DateTimeFormat("en-GB", {timeZone: ZONE, hour12: false, ...options}).format(new Date(epoch));
}

test("parses the fields, with names, ranges, lists and steps", () => {
  const fields = parseCron("0,30 9-17/4 * jan-mar mon,wed");
  assert.deepEqual(fields.minutes, [0, 30]);
  assert.deepEqual(fields.hours, [9, 13, 17]);
  assert.deepEqual(fields.months, [1, 2, 3]);
  assert.deepEqual(fields.daysOfWeek, [1, 3]);
});

test("accepts both spellings of Sunday, and the shorthands", () => {
  assert.deepEqual(parseCron("0 0 * * 7").daysOfWeek, [0]);
  assert.deepEqual(parseCron("0 0 * * 0").daysOfWeek, [0]);
  assert.deepEqual(parseCron("@daily").hours, [0]);
});

test("reads a weekend range that wraps around the end of the week", () => {
  assert.deepEqual(parseCron("0 0 * * fri-mon").daysOfWeek, [0, 1, 5, 6]);
});

test("explains what is wrong rather than just failing", () => {
  assert.match(cronError("0 0 * *")!, /five fields/);
  assert.match(cronError("99 0 * * *")!, /minute must be between 0 and 59/);
  assert.match(cronError("0 0 * * funday")!, /not a valid day of week/);
  assert.equal(cronError("*/15 9-17 * * 1-5"), null);
});

test("finds the next run, in the expression's own zone", () => {
  const after = Date.parse("2026-03-02T10:00:00+11:00");
  const next = nextCronRun("30 8 * * *", after, ZONE)!;
  assert.equal(reads(next, {hour: "2-digit", minute: "2-digit"}), "08:30");
  assert.equal(reads(next, {day: "numeric", month: "short"}), "3 Mar");
});

test("steps within the hour", () => {
  const after = Date.parse("2026-03-02T10:07:00+11:00");
  const next = nextCronRun("*/15 * * * *", after, ZONE)!;
  assert.equal(reads(next, {hour: "2-digit", minute: "2-digit"}), "10:15");
});

test("a weekday-only expression skips the weekend", () => {
  // Friday evening: the next weekday morning is Monday.
  const after = Date.parse("2026-03-06T20:00:00+11:00");
  const next = nextCronRun("0 9 * * 1-5", after, ZONE)!;
  assert.equal(reads(next, {weekday: "short"}), "Mon");
});

test("keeps its wall-clock time across a daylight saving change", () => {
  // Sydney leaves daylight saving on 5 April 2026.
  const before = nextCronRun("0 8 * * *", Date.parse("2026-04-03T09:00:00+11:00"), ZONE)!;
  const after = nextCronRun("0 8 * * *", Date.parse("2026-04-06T09:00:00+10:00"), ZONE)!;
  assert.equal(reads(before, {hour: "2-digit", minute: "2-digit"}), "08:00");
  assert.equal(reads(after, {hour: "2-digit", minute: "2-digit"}), "08:00");
});

test("restricting both day fields matches either, as cron does", () => {
  // The 1st or any Monday — not only Mondays that fall on the 1st.
  const after = Date.parse("2026-03-02T12:00:00+11:00");
  const next = nextCronRun("0 0 1 * mon", after, ZONE)!;
  assert.equal(reads(next, {day: "numeric", month: "short"}), "9 Mar");
});

test("an impossible date never comes round", () => {
  assert.equal(nextCronRun("0 0 30 2 *", Date.now(), ZONE), null);
});
