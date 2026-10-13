import { test } from 'node:test';
import assert from 'node:assert/strict';

let computePaneLayout;
try {
  ({ computePaneLayout } = await import('./mod-bar.js'));
} catch {
  computePaneLayout = null;
}

// --- computePaneLayout ---

test('computePaneLayout returns collapsed when no pane panels exist', () => {
  assert.ok(computePaneLayout, 'mod-bar.js must export computePaneLayout');
  const layout = computePaneLayout({ panels: {}, visible: false }, false);
  assert.equal(layout.showPane, false);
  assert.equal(layout.chatFlex, '1');
});

test('computePaneLayout shows pane when pane-slot panels exist and pane is open', () => {
  assert.ok(computePaneLayout, 'mod-bar.js must export computePaneLayout');
  const state = {
    panels: {
      'agent-tree': { id: 'agent-tree', title: 'Agent Tree', slot: 'pane', data: {} },
    },
    visible: true,
  };
  const layout = computePaneLayout(state, true);
  assert.equal(layout.showPane, true);
  assert.equal(layout.paneFlex, '0 0 320px');
  assert.equal(layout.chatFlex, '1');
});

test('computePaneLayout respects custom pane width', () => {
  assert.ok(computePaneLayout, 'mod-bar.js must export computePaneLayout');
  const state = {
    panels: {
      'agent-tree': { id: 'agent-tree', title: 'Agent Tree', slot: 'pane', data: {} },
    },
    visible: true,
  };
  const layout = computePaneLayout(state, true, 400);
  assert.equal(layout.paneFlex, '0 0 400px');
});

test('computePaneLayout hides pane when paneOpen is false even with pane panels', () => {
  assert.ok(computePaneLayout, 'mod-bar.js must export computePaneLayout');
  const state = {
    panels: {
      'agent-tree': { id: 'agent-tree', title: 'Agent Tree', slot: 'pane', data: {} },
    },
    visible: true,
  };
  const layout = computePaneLayout(state, false);
  assert.equal(layout.showPane, false);
});
