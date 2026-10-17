import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shouldShowJumpToBottom, pickScroller, JUMP_THRESHOLD_PX } from './jump-to-bottom.js';

test('hidden when element is missing', () => {
  assert.equal(shouldShowJumpToBottom(null), false);
  assert.equal(shouldShowJumpToBottom(undefined), false);
});

test('hidden when content fits in the viewport (nothing to scroll)', () => {
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 400, scrollTop: 0, clientHeight: 600 }), false);
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 600, scrollTop: 0, clientHeight: 600 }), false);
});

test('hidden when at the bottom', () => {
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 2000, scrollTop: 1400, clientHeight: 600 }), false);
});

test('hidden when near the bottom (inside the auto-scroll threshold)', () => {
  // 149px from bottom — auto-scroll would still engage, so no button.
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 2000, scrollTop: 1251, clientHeight: 600 }), false);
});

test('shown once scrolled past the threshold', () => {
  // Exactly 150px from bottom.
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 2000, scrollTop: 1250, clientHeight: 600 }), true);
  // Scrolled to the very top of a long transcript.
  assert.equal(shouldShowJumpToBottom({ scrollHeight: 2000, scrollTop: 0, clientHeight: 600 }), true);
});

test('threshold matches the auto-scroll disengage distance in app.js', () => {
  assert.equal(JUMP_THRESHOLD_PX, 150);
});

test('pickScroller prefers the chat area when it overflows (desktop)', () => {
  const chat = { scrollHeight: 2000, clientHeight: 600 };
  const doc = { scrollHeight: 3000, clientHeight: 800 };
  assert.equal(pickScroller(chat, doc), chat);
});

test('pickScroller falls back to the document scroller when the chat area grows to content height (mobile)', () => {
  const chat = { scrollHeight: 3171, clientHeight: 3171 };
  const doc = { scrollHeight: 3349, clientHeight: 844 };
  assert.equal(pickScroller(chat, doc), doc);
  assert.equal(pickScroller(null, doc), doc);
});

test('pickScroller returns null when nothing is available', () => {
  assert.equal(pickScroller(null, null), null);
  assert.equal(pickScroller(undefined, undefined), null);
});

test('custom threshold is honored', () => {
  const el = { scrollHeight: 2000, scrollTop: 1350, clientHeight: 600 }; // 50px from bottom
  assert.equal(shouldShowJumpToBottom(el, 40), true);
  assert.equal(shouldShowJumpToBottom(el, 60), false);
});
