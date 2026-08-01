import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const html = readFileSync(join(dir, 'index.html'), 'utf8');
const css = readFileSync(join(dir, 'style.css'), 'utf8');

// Mobile skills tab lives between the "Skills Tab" and "Tasks Tab" comments.
const mobileSkills = html.slice(html.indexOf('<!-- Skills Tab -->'), html.indexOf('<!-- Tasks Tab -->'));

test('mobile skills tab has All/Global/Project filter pills', () => {
  assert.match(mobileSkills, /skillsFilter = 'all'/);
  assert.match(mobileSkills, /skillsFilter = 'global'/);
  assert.match(mobileSkills, /skillsFilter = 'project'/);
});

test('mobile skills tab renders the filtered list, not the raw list', () => {
  assert.match(mobileSkills, /x-for="skill in filteredSkills"/);
});

test('mobile skills tab tints project skills like desktop', () => {
  assert.match(mobileSkills, /skill\.dir === 'project'/);
});

test('mobile skills and issues lists are not height-clipped', () => {
  assert.match(css, /\.issue-list\.full\s*\{[^}]*max-height:\s*none/);
  assert.match(mobileSkills, /class="issue-list full"/);
  const mobileIssues = html.slice(html.indexOf('<!-- GitHub Issues (mobile) -->'), html.indexOf('<!-- Jira Placeholder (mobile) -->'));
  assert.match(mobileIssues, /class="issue-list full"/);
});
