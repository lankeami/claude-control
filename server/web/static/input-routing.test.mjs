import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveInputAction } from './input-routing.js';

test('shell mode routes to shell even when input starts with /', () => {
  assert.equal(resolveInputAction(true, 'managed', '/Applications/VSCode.app/Contents/MacOS/Electron .'), 'shell');
});

test('shell mode routes to shell for plain commands', () => {
  assert.equal(resolveInputAction(true, 'managed', 'ls -la'), 'shell');
});

test('prompt mode routes slash commands to slash handler', () => {
  assert.equal(resolveInputAction(false, 'managed', '/clear'), 'slash');
});

test('prompt mode routes regular text to managed message', () => {
  assert.equal(resolveInputAction(false, 'managed', 'explain this code'), 'managed');
});

test('hook session routes to instruct regardless of mode', () => {
  assert.equal(resolveInputAction(false, 'hook', 'hello'), 'instruct');
  assert.equal(resolveInputAction(true, 'hook', 'ls'), 'instruct');
});
