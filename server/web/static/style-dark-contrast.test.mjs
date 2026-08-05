import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(dir, 'style.css'), 'utf8');

// Session/task rows must not hardcode a light background — in dark mode that
// renders light text on a light-gray card (issue #239).
const sessionItemRule = css.match(/\.session-item\s*\{[^}]*\}/)[0];

test('session-item background is theme-aware, not hardcoded white', () => {
  assert.doesNotMatch(sessionItemRule, /rgba\(255,\s*255,\s*255/);
  assert.match(sessionItemRule, /background:\s*var\(--item-bg\)/);
});

test('--item-bg is defined for light theme and overridden for dark', () => {
  const rootBlock = css.match(/:root\s*\{[^}]*\}/)[0];
  assert.match(rootBlock, /--item-bg:/);

  const darkBlock = css.slice(css.indexOf('@media (prefers-color-scheme: dark)'));
  const darkRoot = darkBlock.match(/:root\s*\{[^}]*\}/)[0];
  assert.match(darkRoot, /--item-bg:/);
});
