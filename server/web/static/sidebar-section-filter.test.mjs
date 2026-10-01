import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  filterByText,
  filterFileTree,
  createSectionFilters,
  applySectionFilter,
} from './sidebar-section-filter.js';

// ---------- fixtures ----------

const skills = [
  { name: 'graphify', dir: 'global' },
  { name: 'autoship', dir: 'global' },
  { name: 'Jay-Day', dir: 'project' },
];

const issues = [
  { number: 1, title: 'Fix login bug' },
  { number: 2, title: 'Add dark mode' },
  { number: 3, title: 'Dark sidebar styling' },
];

const pulls = [
  { number: 10, title: 'feat: option selector' },
  { number: 11, title: 'fix: session filter' },
];

const fileTree = [
  {
    name: 'src', path: 'src', isDir: true, open: true,
    children: [
      { name: 'app.js', path: 'src/app.js', isDir: false },
      {
        name: 'util', path: 'src/util', isDir: true, open: false,
        children: [
          { name: 'helpers.js', path: 'src/util/helpers.js', isDir: false },
        ],
      },
    ],
  },
  { name: 'README.md', path: 'README.md', isDir: false },
];

const skillName = (s) => s.name;
const issueTitle = (i) => i.title;

// ---------- filterByText ----------

test('filterByText: empty query returns all items (same array)', () => {
  assert.equal(filterByText(skills, '', skillName), skills);
  assert.equal(filterByText(skills, null, skillName), skills);
  assert.equal(filterByText(skills, undefined, skillName), skills);
});

test('filterByText: case-insensitive substring over skill names', () => {
  const result = filterByText(skills, 'JAY', skillName);
  assert.equal(result.length, 1);
  assert.equal(result[0].name, 'Jay-Day');
});

test('filterByText: case-insensitive substring over issue titles', () => {
  const result = filterByText(issues, 'dark', issueTitle);
  assert.equal(result.length, 2);
  assert.deepEqual(result.map(i => i.number), [2, 3]);
});

test('filterByText: matches PR titles', () => {
  const result = filterByText(pulls, 'session', issueTitle);
  assert.equal(result.length, 1);
  assert.equal(result[0].number, 11);
});

test('filterByText: no match returns empty array', () => {
  assert.equal(filterByText(skills, 'zzz-nonexistent', skillName).length, 0);
});

test('filterByText: returns original objects, not copies', () => {
  const result = filterByText(skills, 'graph', skillName);
  assert.equal(result[0], skills[0]);
});

test('filterByText: tolerates items with missing text', () => {
  const items = [{ name: 'alpha' }, { name: null }, {}];
  const result = filterByText(items, 'alp', (x) => x.name);
  assert.equal(result.length, 1);
  assert.equal(result[0].name, 'alpha');
});

// ---------- filterFileTree ----------

test('filterFileTree: empty query respects open/closed dirs', () => {
  const result = filterFileTree(fileTree, '');
  assert.deepEqual(result.map(n => n.path), ['src', 'src/app.js', 'src/util', 'README.md']);
});

test('filterFileTree: matches file names case-insensitively', () => {
  const result = filterFileTree(fileTree, 'ReadMe');
  assert.deepEqual(result.map(n => n.path), ['README.md']);
});

test('filterFileTree: includes ancestor dirs of a match, even when closed', () => {
  const result = filterFileTree(fileTree, 'helpers');
  assert.deepEqual(result.map(n => n.path), ['src', 'src/util', 'src/util/helpers.js']);
});

test('filterFileTree: dir name match includes the dir and its ancestors', () => {
  const result = filterFileTree(fileTree, 'util');
  assert.deepEqual(result.map(n => n.path), ['src', 'src/util']);
});

test('filterFileTree: no match returns empty list', () => {
  assert.equal(filterFileTree(fileTree, 'zzz-nonexistent').length, 0);
});

test('filterFileTree: empty tree is safe', () => {
  assert.deepEqual(filterFileTree([], 'x'), []);
  assert.deepEqual(filterFileTree(null, ''), []);
});

// ---------- independent per-section state ----------

test('createSectionFilters: four independent keys, all empty', () => {
  const filters = createSectionFilters();
  assert.deepEqual(filters, { files: '', skills: '', issues: '', pulls: '' });
});

test('setting one section filter narrows only that section', () => {
  const filters = createSectionFilters();
  filters.skills = 'graph';

  const visibleSkills = applySectionFilter(filters, 'skills', skills, skillName);
  assert.equal(visibleSkills.length, 1);
  assert.equal(visibleSkills[0].name, 'graphify');

  // The other three sections are untouched — full lists remain.
  assert.equal(applySectionFilter(filters, 'files', fileTree, (n) => n.name), fileTree);
  assert.equal(applySectionFilter(filters, 'issues', issues, issueTitle), issues);
  assert.equal(applySectionFilter(filters, 'pulls', pulls, issueTitle), pulls);
});

test('each section filter is independent of the others', () => {
  const filters = createSectionFilters();
  filters.issues = 'dark';
  filters.pulls = 'option';

  assert.equal(applySectionFilter(filters, 'issues', issues, issueTitle).length, 2);
  assert.equal(applySectionFilter(filters, 'pulls', pulls, issueTitle).length, 1);
  // skills and files remain unfiltered
  assert.equal(applySectionFilter(filters, 'skills', skills, skillName), skills);
  assert.equal(applySectionFilter(filters, 'files', fileTree, (n) => n.name), fileTree);

  // clearing one section restores it without touching the others
  filters.issues = '';
  assert.equal(applySectionFilter(filters, 'issues', issues, issueTitle), issues);
  assert.equal(applySectionFilter(filters, 'pulls', pulls, issueTitle).length, 1);
});

test('text filter chains with the existing skills dir toggle', () => {
  // mimics app.js filteredSkills: dir toggle first, then the text filter
  const toggled = skills.filter(s => s.dir === 'project');
  const result = filterByText(toggled, 'jay', skillName);
  assert.equal(result.length, 1);
  assert.equal(result[0].name, 'Jay-Day');
  // toggle result unaffected when text filter targets a global-only name
  assert.equal(filterByText(toggled, 'graph', skillName).length, 0);
  assert.equal(toggled.length, 1);
});
