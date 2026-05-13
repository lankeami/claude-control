import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyToolUse, applyToolResult, finalizeAll, extractToolContext } from './agent-tracker.js';

const NOW = 1000000; // fixed timestamp for deterministic tests

// --- applyToolUse ---

test('applyToolUse adds a running entry with correct fields', () => {
  const result = applyToolUse([], { id: 'abc', name: 'Read' }, 'Read file.js', NOW);
  assert.equal(result.length, 1);
  assert.equal(result[0].id, 'abc');
  assert.equal(result[0].name, 'Read');
  assert.equal(result[0].label, 'Read file.js');
  assert.equal(result[0].status, 'running');
  assert.equal(result[0].startTime, NOW);
  assert.equal(result[0].endTime, null);
  assert.equal(result[0].duration, null);
  assert.equal(result[0].lastOutput, null);
});

test('applyToolUse generates an id when block.id is missing', () => {
  const result = applyToolUse([], { name: 'Bash' }, 'Bash ls', NOW);
  assert.ok(result[0].id, 'id should be set');
});

test('applyToolUse completes a previous running entry before adding new one', () => {
  const base = applyToolUse([], { id: 'first', name: 'Read' }, 'Read a', NOW);
  const result = applyToolUse(base, { id: 'second', name: 'Write' }, 'Write b', NOW + 5000);
  assert.equal(result.length, 2);
  assert.equal(result[0].status, 'completed');
  assert.equal(result[0].endTime, NOW + 5000);
  assert.equal(result[0].duration, '5s');
  assert.equal(result[1].status, 'running');
  assert.equal(result[1].id, 'second');
});

test('applyToolUse does not complete already-completed entries', () => {
  const base = [
    { id: 'old', name: 'Read', status: 'completed', startTime: NOW, endTime: NOW + 1000, duration: '1s', lastOutput: null },
  ];
  const result = applyToolUse(base, { id: 'new', name: 'Write' }, 'Write x', NOW + 2000);
  assert.equal(result[0].status, 'completed'); // unchanged
  assert.equal(result[1].status, 'running');
});

// --- applyToolResult ---

test('applyToolResult completes the last running entry', () => {
  const base = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  const result = applyToolResult(base, {}, NOW + 3000);
  assert.equal(result[0].status, 'completed');
  assert.equal(result[0].endTime, NOW + 3000);
  assert.equal(result[0].duration, '3s');
});

test('applyToolResult captures string content as lastOutput snippet', () => {
  const base = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  const longText = 'x'.repeat(200);
  const result = applyToolResult(base, { content: longText }, NOW + 1000);
  assert.equal(result[0].lastOutput, longText.substring(0, 120));
});

test('applyToolResult captures text from array content', () => {
  const base = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  const result = applyToolResult(base, { content: [{ type: 'text', text: 'hello world' }] }, NOW + 1000);
  assert.equal(result[0].lastOutput, 'hello world');
});

test('applyToolResult is a no-op when no running entry exists', () => {
  const base = [
    { id: 'done', name: 'Read', status: 'completed', startTime: NOW, endTime: NOW + 1000, duration: '1s', lastOutput: null },
  ];
  const result = applyToolResult(base, { content: 'ignored' }, NOW + 2000);
  assert.equal(result[0].lastOutput, null); // unchanged
});

test('applyToolResult handles missing content gracefully', () => {
  const base = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  const result = applyToolResult(base, {}, NOW + 1000);
  assert.equal(result[0].lastOutput, null);
  assert.equal(result[0].status, 'completed');
});

// --- finalizeAll ---

test('finalizeAll completes all running entries', () => {
  let inv = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  inv = applyToolUse(inv, { id: 'b', name: 'Write' }, 'Write y', NOW + 2000);
  const result = finalizeAll(inv, NOW + 5000);
  assert.ok(result.every(i => i.status === 'completed'));
  assert.equal(result[1].duration, '3s');
});

test('finalizeAll leaves already-completed entries unchanged', () => {
  let inv = applyToolUse([], { id: 'a', name: 'Read' }, 'Read x', NOW);
  inv = applyToolResult(inv, { content: 'done' }, NOW + 1000);
  inv = applyToolUse(inv, { id: 'b', name: 'Write' }, 'Write y', NOW + 2000);
  const result = finalizeAll(inv, NOW + 5000);
  assert.equal(result[0].endTime, NOW + 1000); // untouched
  assert.equal(result[1].endTime, NOW + 5000); // finalized
});

test('finalizeAll is a no-op on empty array', () => {
  const result = finalizeAll([], NOW);
  assert.deepEqual(result, []);
});

// --- extractToolContext ---

test('extractToolContext uses basename of file_path', () => {
  const label = extractToolContext({ name: 'Read', input: { file_path: '/some/path/app.js' } });
  assert.equal(label, 'Read app.js');
});

test('extractToolContext uses command prefix for Bash', () => {
  const label = extractToolContext({ name: 'Bash', input: { command: 'go test ./...' } });
  assert.equal(label, 'Bash go test ./...');
});

test('extractToolContext truncates labels longer than 40 chars', () => {
  const label = extractToolContext({ name: 'Read', input: { file_path: '/src/some-very-long-component-name-that-exceeds-limit.tsx' } });
  assert.ok(label.length <= 40, `expected <= 40 chars, got ${label.length}: "${label}"`);
  assert.ok(label.endsWith('...'), `expected "..." suffix, got: "${label}"`);
});

test('extractToolContext falls back to tool name when no input context', () => {
  const label = extractToolContext({ name: 'TodoWrite', input: {} });
  assert.equal(label, 'TodoWrite');
});

test('extractToolContext uses pattern for Grep', () => {
  const label = extractToolContext({ name: 'Grep', input: { pattern: 'agentInvocations' } });
  assert.equal(label, 'Grep agentInvocations');
});
