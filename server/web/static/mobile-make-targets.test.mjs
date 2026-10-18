import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(dir, 'index.html'), 'utf8');
const appJs = readFileSync(join(dir, 'app.js'), 'utf8');

// The mobile Make pane lives between the "Make Tab" and "Tasks Tab" comments.
const makePaneStart = html.indexOf('<!-- Make Tab -->');
const makePane = makePaneStart === -1 ? '' : html.slice(makePaneStart, html.indexOf('<!-- Tasks Tab -->'));
// Bottom tab bar lives after the "Bottom Tab Bar" comment.
const tabBar = html.slice(html.indexOf('<!-- Bottom Tab Bar -->'));
// Mobile skills pane lives between the "Skills Tab" and "Make Tab" comments.
const skillsPane = html.slice(html.indexOf('<!-- Skills Tab -->'), makePaneStart === -1 ? html.indexOf('<!-- Tasks Tab -->') : makePaneStart);

// --- Discoverability: Make must be visible within two taps of opening the
// --- mobile UI (hamburger -> Make tab). Issue #333: nesting it as a sub-tab
// --- inside the Skills tab made it unreachable for real users.

test('bottom tab bar has a dedicated Make tab', () => {
  assert.match(tabBar, /mobileTab = 'make'; ensureMakeTargets\(\)/);
  assert.match(tabBar, /<span>Make<\/span>/);
});

test('Make tab button participates in active state and a11y like other tabs', () => {
  assert.match(tabBar, /\{ active: mobileTab === 'make' \}/);
  assert.match(tabBar, /:aria-selected="mobileTab === 'make'"/);
});

test('mobile Make pane is a top-level tab pane, not a Skills sub-tab', () => {
  assert.ok(makePaneStart !== -1, 'index.html has a <!-- Make Tab --> pane');
  assert.match(makePane, /x-show="mobileTab === 'make'"/);
  // The old sub-tab indirection is gone everywhere.
  assert.doesNotMatch(html, /mobileSkillsSection/);
  assert.doesNotMatch(appJs, /mobileSkillsSection/);
});

test('mobile Skills pane no longer embeds the Make list', () => {
  assert.doesNotMatch(skillsPane, /filteredMakeTargets/);
});

// --- Fetch gating: opening the mobile path must populate makeTargets ---

test('Make pane prompts to select a session when none is selected', () => {
  assert.match(makePane, /x-if="!selectedSessionId"/);
  assert.match(makePane, /Select a session to view make targets/);
});

test('ensureMakeTargets fetches only when targets are stale/empty', () => {
  const m = appJs.match(/ensureMakeTargets\(\)\s*\{([\s\S]*?)\n    \}/);
  assert.ok(m, 'ensureMakeTargets() is defined in app.js');
  assert.match(m[1], /makeTargets\.length === 0/);
  assert.match(m[1], /makeTargetsLoading/);
  assert.match(m[1], /fetchMakeTargets\(this\.selectedSessionId\)/);
});

// --- Section content: header, rows, and actions ---

test('mobile Make pane has a visible header with count badge and refresh', () => {
  assert.match(makePane, />Make</);
  assert.match(makePane, /x-text="filteredMakeTargets\.length"/);
  assert.match(makePane, /fetchMakeTargets\(selectedSessionId\)/);
});

test('mobile Make pane renders filtered target rows with name and description', () => {
  assert.match(makePane, /x-for="target in filteredMakeTargets"/);
  assert.match(makePane, /x-text="target\.name"/);
  assert.match(makePane, /x-text="target\.description"/);
});

test('mobile Make rows expose run, insert, and inspect actions', () => {
  assert.match(makePane, /sendMakeTarget\(target\.name\)/);
  assert.match(makePane, /insertMakeTarget\(target\.name\)/);
  assert.match(makePane, /openFileViewer\(target\.filePath\)/);
});

test('mobile Make pane surfaces loading, error, and empty states', () => {
  assert.match(makePane, /makeTargetsLoading/);
  assert.match(makePane, /makeTargetsError/);
  assert.match(makePane, /No make targets/);
});

test('mobile Make pane has the filter input', () => {
  assert.match(makePane, /x-model="makeTargetsTextFilter"/);
  assert.match(makePane, /placeholder="Filter targets\.\.\."/);
});

test('mobile Make list uses the unclipped full list class', () => {
  assert.match(makePane, /class="issue-list full"/);
});

// --- Desktop regression guard: the sidebar Make section is untouched ---

test('desktop sidebar Make section still renders behind makeTargetsExpanded', () => {
  assert.match(html, /x-show="selectedSessionId && makeTargets\.length > 0"/);
  assert.match(html, /expandMakeTargets\(\)/);
  assert.match(appJs, /expandMakeTargets\(\)\s*\{/);
});
