import { test } from 'node:test';
import assert from 'node:assert/strict';
import { filterSessions } from './session-filter.js';

const sessions = [
  { id: '1', name: 'my-api-server', project_path: '/home/dev/api', computer_name: 'macbook' },
  { id: '2', name: '', project_path: '/home/dev/claude-control', computer_name: 'workstation' },
  { id: '3', name: 'dashboard', project_path: '/home/dev/dashboard', computer_name: 'macbook' },
];

function nameFn(s) {
  if (s.name) return s.name;
  const parts = s.project_path.split('/');
  return `${s.computer_name} / ${parts[parts.length - 1]}`;
}

test('returns all sessions when filter is empty', () => {
  assert.equal(filterSessions(sessions, '', nameFn).length, 3);
});

test('returns all sessions when filter is null/undefined', () => {
  assert.equal(filterSessions(sessions, null, nameFn).length, 3);
  assert.equal(filterSessions(sessions, undefined, nameFn).length, 3);
});

test('filters by custom name', () => {
  const result = filterSessions(sessions, 'api', nameFn);
  assert.equal(result.length, 1);
  assert.equal(result[0].id, '1');
});

test('filters by project directory name', () => {
  const result = filterSessions(sessions, 'claude', nameFn);
  assert.equal(result.length, 1);
  assert.equal(result[0].id, '2');
});

test('filters by computer_name', () => {
  const result = filterSessions(sessions, 'workstation', nameFn);
  assert.equal(result.length, 1);
  assert.equal(result[0].id, '2');
});

test('case-insensitive matching', () => {
  const result = filterSessions(sessions, 'DASHBOARD', nameFn);
  assert.equal(result.length, 1);
  assert.equal(result[0].id, '3');
});

test('returns empty array when nothing matches', () => {
  const result = filterSessions(sessions, 'nonexistent', nameFn);
  assert.equal(result.length, 0);
});

test('returns original session objects, not copies', () => {
  const result = filterSessions(sessions, 'api', nameFn);
  assert.equal(result[0], sessions[0]);
});
