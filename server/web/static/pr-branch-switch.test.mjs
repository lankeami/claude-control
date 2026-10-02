import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(dir, 'index.html'), 'utf8');
const appJs = readFileSync(join(dir, 'app.js'), 'utf8');

// Isolate the PR row template (the x-for over githubPulls in the right sidebar).
const start = html.indexOf('x-for="pull in githubPulls"');
assert.ok(start !== -1, 'PR row template (x-for over githubPulls) exists');
const prRow = html.slice(start, html.indexOf('</template>', start));

test('PR row has a switch-to-branch button that stops click propagation', () => {
  assert.match(prRow, /@click\.stop="switchToPullBranch\(pull\)"/);
});

test('branch-switch button uses the GitHub octicon git-branch glyph', () => {
  // Distinctive start of the octicon git-branch-16 path data.
  assert.match(prRow, /M9\.5 3\.25a2\.25/);
});

test('branch-switch button has a "Switch to branch <name>" tooltip', () => {
  assert.match(prRow, /:title="'Switch to branch ' \+ pull\.head_branch"/);
});

test('branch-switch button only renders when the head branch is known', () => {
  assert.match(prRow, /x-show="pull\.head_branch"/);
});

test('app.js defines switchToPullBranch using the head branch', () => {
  assert.match(appJs, /switchToPullBranch\(pull\)\s*\{/);
  const fn = appJs.slice(appJs.indexOf('switchToPullBranch(pull)'));
  assert.match(fn.slice(0, 1200), /head_branch/);
  assert.match(fn.slice(0, 1200), /sendManagedMessage/);
});
