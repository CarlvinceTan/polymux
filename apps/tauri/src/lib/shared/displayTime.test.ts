import {test} from 'node:test';
import assert from 'node:assert/strict';
import {clockTime, displayTime} from './displayTime';

/** The clock is 12-hour wherever the app runs, whatever the host's regional
 * setting is — a machine set to a 24-hour region must not change it. The words
 * around the clock belong to the interface language, so the assertions here
 * hold in every language: they compare a time with its own afternoon twin
 * rather than with an English string. */
const at = (hour: number, minute = 0): Date => new Date(2026, 8, 11, hour, minute);

/** Everything a stamp says apart from its words: `3:24 PM` and `午後3:24` both
 * come down to `324`, and a 24-hour `15:24` would keep its extra digit. */
const numbers = (stamp: string): string => stamp.replace(/[^\p{Nd}]/gu, '');

test('renders the clock in 12-hour time', () => {
  // 3 pm and 3 am say the same hour; 15:24 would not match 3:24.
  for (let hour = 1; hour <= 11; hour += 1) {
    assert.equal(
      numbers(clockTime(at(hour, 24))),
      numbers(clockTime(at(hour + 12, 24))),
      `${hour}:24 and ${hour + 12}:24 should share an hour`,
    );
  }
  // Midnight and noon are the two hours that share the label `12`.
  assert.equal(numbers(clockTime(at(0))).startsWith('12'), true);
  assert.equal(numbers(clockTime(at(12))).startsWith('12'), true);
});

test('never renders an hour above 12', () => {
  for (let hour = 0; hour < 24; hour += 1) {
    const stamp = clockTime(at(hour));
    const hourPart = numbers(stamp).slice(0, -2);
    assert.ok(
      Number(hourPart) >= 1 && Number(hourPart) <= 12,
      `${hour}:00 rendered as ${stamp}`,
    );
  }
});

test('carries the 12-hour clock into the shared stamp', () => {
  assert.equal(numbers(displayTime(at(15, 24), {now: at(15, 24)})), numbers(clockTime(at(15, 24))));
});

test('an unreadable value has no clock rather than a failure in the row', () => {
  assert.equal(clockTime('not a time'), '');
  assert.equal(displayTime('not a time'), 'not a time');
});
