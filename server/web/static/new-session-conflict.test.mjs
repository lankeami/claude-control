import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { resolveSessionConflict } from './session-conflict.js';

const dir = dirname(fileURLToPath(import.meta.url));

const sessions = [
  { id: 'abc', cwd: '/repos/foo', mode: 'managed' },
  { id: 'def', cwd: '/repos/bar', mode: 'managed' },
];

test('"no" selects the existing session matched by cwd', () => {
  const result = resolveSessionConflict('no', { cwd: '/repos/foo', name: '', sessions });
  assert.deepEqual(result, { action: 'select', sessionId: 'abc' });
});

test('"yes" retries with worktree flag and name', () => {
  const result = resolveSessionConflict('yes', { cwd: '/repos/foo', name: 'hotfix', sessions });
  assert.deepEqual(result, {
    action: 'retry',
    payload: { cwd: '/repos/foo', worktree: true, name: 'hotfix' },
  });
});

test('"yes" without a name is rejected', () => {
  const result = resolveSessionConflict('yes', { cwd: '/repos/foo', name: '  ', sessions });
  assert.deepEqual(result, { action: 'error', message: 'Enter a name for the new session' });
});

test('"no" with no matching session yields error', () => {
  const result = resolveSessionConflict('no', { cwd: '/repos/gone', name: '', sessions });
  assert.deepEqual(result, { action: 'error', message: 'Existing session not found' });
});

// Wiring: app.js must route 409s through the conflict dialog in both create paths
const appJs = readFileSync(join(dir, 'app.js'), 'utf8');

test('both create paths route through the shared create helper', () => {
  const create = appJs.slice(appJs.indexOf('async createManagedSession('), appJs.indexOf('async selectRecentDir('));
  const recent = appJs.slice(appJs.indexOf('async selectRecentDir('), appJs.indexOf('async postSessionCreate('));
  assert.match(create, /postSessionCreate/);
  assert.match(recent, /postSessionCreate/);
  assert.doesNotMatch(recent, /Switched to existing session/);
});

test('shared create helper opens the conflict dialog on 409', () => {
  const fn = appJs.slice(appJs.indexOf('async postSessionCreate('), appJs.indexOf('openSessionConflict(cwd)'));
  assert.match(fn, /409/);
  assert.match(fn, /openSessionConflict/);
});

// Dialog markup exists in index.html
const html = readFileSync(join(dir, 'index.html'), 'utf8');

test('index.html has the session conflict dialog with name input and yes/no actions', () => {
  assert.match(html, /sessionConflict\.open/);
  assert.match(html, /x-model="sessionConflict\.name"/);
  assert.match(html, /resolveConflict\('yes'\)/);
  assert.match(html, /resolveConflict\('no'\)/);
});
