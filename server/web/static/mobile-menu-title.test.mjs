import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(dir, 'index.html'), 'utf8');

// Mobile menu header lives between the "Menu Header" and "Tab Content" comments.
const header = html.slice(html.indexOf('<!-- Menu Header -->'), html.indexOf('<!-- Tab Content -->'));

test('mobile menu header binds the selected session title with app-name fallback', () => {
  assert.match(header, /x-text="selectedSession \? sessionName\(selectedSession\) : 'Claude Controller'"/);
});

test('mobile menu header has no hardcoded app-name text node', () => {
  assert.doesNotMatch(header, />\s*Claude Controller\s*</);
});
