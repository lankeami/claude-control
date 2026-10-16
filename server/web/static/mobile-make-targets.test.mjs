import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(dir, 'index.html'), 'utf8');
const appJs = readFileSync(join(dir, 'app.js'), 'utf8');

// Mobile skills tab lives between the "Skills Tab" and "Tasks Tab" comments.
const mobileSkills = html.slice(html.indexOf('<!-- Skills Tab -->'), html.indexOf('<!-- Tasks Tab -->'));
// Bottom tab bar lives after the "Bottom Tab Bar" comment.
const tabBar = html.slice(html.indexOf('<!-- Bottom Tab Bar -->'));

// --- Discoverability: Make must be reachable without scrolling past the skills list ---

test('mobile skills tab has Skills/Make sub-tab pills', () => {
  assert.match(mobileSkills, /mobileSkillsSection = 'skills'/);
  assert.match(mobileSkills, /mobileSkillsSection = 'make'/);
});

test('make list is gated by the sub-tab, not appended below the skills list', () => {
  assert.match(mobileSkills, /x-show="mobileSkillsSection === 'make'"/);
  assert.match(mobileSkills, /x-show="mobileSkillsSection === 'skills'"/);
  // The old pattern rendered Make only when targets existed, hiding fetch
  // failures and empty states entirely.
  assert.doesNotMatch(mobileSkills, /x-if="makeTargets\.length > 0"/);
});

// --- Fetch gating: opening the mobile path must populate makeTargets ---

test('switching to the Make sub-tab triggers a fetch when targets are empty', () => {
  assert.match(mobileSkills, /mobileSkillsSection = 'make'; ensureMakeTargets\(\)/);
});

test('opening the mobile Skills tab from the tab bar ensures make targets', () => {
  assert.match(tabBar, /mobileTab = 'skills'; ensureMakeTargets\(\)/);
});

test('ensureMakeTargets fetches only when targets are stale/empty', () => {
  const m = appJs.match(/ensureMakeTargets\(\)\s*\{([\s\S]*?)\n    \}/);
  assert.ok(m, 'ensureMakeTargets() is defined in app.js');
  assert.match(m[1], /makeTargets\.length === 0/);
  assert.match(m[1], /makeTargetsLoading/);
  assert.match(m[1], /fetchMakeTargets\(this\.selectedSessionId\)/);
});

// --- Section content: header, rows, and actions ---

test('mobile Make section has a visible header with count badge and refresh', () => {
  const makeSection = mobileSkills.slice(mobileSkills.indexOf('x-show="mobileSkillsSection === \'make\'"'));
  assert.match(makeSection, />Make</);
  assert.match(makeSection, /x-text="filteredMakeTargets\.length"/);
  assert.match(makeSection, /fetchMakeTargets\(selectedSessionId\)/);
});

test('mobile Make section renders filtered target rows with name and description', () => {
  assert.match(mobileSkills, /x-for="target in filteredMakeTargets"/);
  assert.match(mobileSkills, /x-text="target\.name"/);
  assert.match(mobileSkills, /x-text="target\.description"/);
});

test('mobile Make rows expose run, insert, and inspect actions', () => {
  assert.match(mobileSkills, /sendMakeTarget\(target\.name\)/);
  assert.match(mobileSkills, /insertMakeTarget\(target\.name\)/);
  assert.match(mobileSkills, /openFileViewer\(target\.filePath\)/);
});

test('mobile Make section surfaces loading, error, and empty states', () => {
  const makeSection = mobileSkills.slice(mobileSkills.indexOf('x-show="mobileSkillsSection === \'make\'"'));
  assert.match(makeSection, /makeTargetsLoading/);
  assert.match(makeSection, /makeTargetsError/);
  assert.match(makeSection, /No make targets/);
});

test('mobile Make list uses the unclipped full list class', () => {
  const makeSection = mobileSkills.slice(mobileSkills.indexOf('x-show="mobileSkillsSection === \'make\'"'));
  assert.match(makeSection, /class="issue-list full"/);
});

test('mobileSkillsSection state defaults to skills', () => {
  assert.match(appJs, /mobileSkillsSection: 'skills'/);
});
