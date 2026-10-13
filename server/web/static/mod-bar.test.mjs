import { test } from 'node:test';
import assert from 'node:assert/strict';

// --- mod-bar data helpers (will be in mod-bar.js) ---

let createModBarState, applyModStateEvent, getVisiblePanels;
try {
  ({ createModBarState, applyModStateEvent, getVisiblePanels } = await import('./mod-bar.js'));
} catch {
  // Expected to fail before implementation
  createModBarState = null;
  applyModStateEvent = null;
  getVisiblePanels = null;
}

// --- createModBarState ---

test('createModBarState returns empty panels map and visible flag', () => {
  assert.ok(createModBarState, 'mod-bar.js must export createModBarState');
  const state = createModBarState();
  assert.deepEqual(state.panels, {});
  assert.equal(state.visible, false);
});

// --- applyModStateEvent ---

test('applyModStateEvent adds a panel from a mod_state SSE event', () => {
  assert.ok(applyModStateEvent, 'mod-bar.js must export applyModStateEvent');
  const state = createModBarState();
  const event = {
    type: 'mod_state',
    panel_id: 'token-counter',
    panel_title: 'Tokens',
    slot: 'bar',
    data: { input: 1200, output: 340 },
  };
  const next = applyModStateEvent(state, event);
  assert.equal(Object.keys(next.panels).length, 1);
  assert.equal(next.panels['token-counter'].title, 'Tokens');
  assert.equal(next.panels['token-counter'].slot, 'bar');
  assert.deepEqual(next.panels['token-counter'].data, { input: 1200, output: 340 });
  assert.equal(next.visible, true);
});

test('applyModStateEvent updates existing panel data', () => {
  assert.ok(applyModStateEvent, 'mod-bar.js must export applyModStateEvent');
  let state = createModBarState();
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'cost',
    panel_title: 'Cost',
    slot: 'bar',
    data: { total: 0.05 },
  });
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'cost',
    panel_title: 'Cost',
    slot: 'bar',
    data: { total: 0.12 },
  });
  assert.equal(Object.keys(state.panels).length, 1);
  assert.deepEqual(state.panels['cost'].data, { total: 0.12 });
});

test('applyModStateEvent ignores events without panel_id', () => {
  assert.ok(applyModStateEvent, 'mod-bar.js must export applyModStateEvent');
  const state = createModBarState();
  const next = applyModStateEvent(state, { type: 'mod_state' });
  assert.deepEqual(next.panels, {});
  assert.equal(next.visible, false);
});

// --- getVisiblePanels ---

test('getVisiblePanels filters by slot name', () => {
  assert.ok(getVisiblePanels, 'mod-bar.js must export getVisiblePanels');
  let state = createModBarState();
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'tokens',
    panel_title: 'Tokens',
    slot: 'bar',
    data: {},
  });
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'agent-tree',
    panel_title: 'Agent Tree',
    slot: 'pane',
    data: {},
  });
  const barPanels = getVisiblePanels(state, 'bar');
  const panePanels = getVisiblePanels(state, 'pane');
  assert.equal(barPanels.length, 1);
  assert.equal(barPanels[0].id, 'tokens');
  assert.equal(panePanels.length, 1);
  assert.equal(panePanels[0].id, 'agent-tree');
});

test('applyModStateEvent removes panel when data is null', () => {
  assert.ok(applyModStateEvent, 'mod-bar.js must export applyModStateEvent');
  let state = createModBarState();
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'tokens',
    panel_title: 'Tokens',
    slot: 'bar',
    data: { count: 10 },
  });
  assert.equal(Object.keys(state.panels).length, 1);
  state = applyModStateEvent(state, {
    type: 'mod_state',
    panel_id: 'tokens',
    data: null,
  });
  assert.equal(Object.keys(state.panels).length, 0);
  assert.equal(state.visible, false);
});
