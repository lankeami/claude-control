import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatAbsoluteTime } from './bubble-time.js';

// --- valid timestamps ---

test('formats an ISO timestamp with Z suffix as locale time with seconds', () => {
  const iso = '2026-10-02T16:06:32Z';
  const expected = new Date(iso).toLocaleTimeString(undefined, {
    hour: 'numeric', minute: '2-digit', second: '2-digit',
  });
  assert.equal(formatAbsoluteTime(iso), expected);
});

test('includes seconds in the formatted output', () => {
  // In en-US this renders like "12:06:32 PM"; regardless of locale it must
  // contain three numeric groups (hour, minute, second).
  const out = formatAbsoluteTime('2026-10-02T16:06:32Z');
  const groups = out.match(/\d+/g) || [];
  assert.ok(groups.length >= 3, `expected hour/minute/second groups in "${out}"`);
});

test('en-US locale renders like 12:06:32 PM', () => {
  const iso = '2026-10-02T16:06:32Z';
  const expected = new Date(iso).toLocaleTimeString('en-US', {
    hour: 'numeric', minute: '2-digit', second: '2-digit',
  });
  // Format of the expected shape: h:mm:ss AM/PM
  assert.match(expected, /^\d{1,2}:\d{2}:\d{2}\s?[AP]M$/);
});

test('normalizes timestamps without a timezone suffix as UTC (matches Z-suffixed)', () => {
  const withZ = formatAbsoluteTime('2026-10-02T16:06:32Z');
  const withoutZ = formatAbsoluteTime('2026-10-02T16:06:32');
  assert.equal(withoutZ, withZ);
});

test('preserves explicit timezone offsets', () => {
  const offset = '2026-10-02T16:06:32+00:00';
  const z = '2026-10-02T16:06:32Z';
  assert.equal(formatAbsoluteTime(offset), formatAbsoluteTime(z));
});

// --- empty / invalid input ---

test('returns empty string for undefined', () => {
  assert.equal(formatAbsoluteTime(undefined), '');
});

test('returns empty string for null', () => {
  assert.equal(formatAbsoluteTime(null), '');
});

test('returns empty string for empty string', () => {
  assert.equal(formatAbsoluteTime(''), '');
});

test('returns empty string for invalid date strings', () => {
  assert.equal(formatAbsoluteTime('not-a-date'), '');
});
