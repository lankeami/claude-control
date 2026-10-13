import { test } from 'node:test';
import assert from 'node:assert/strict';

let routeModEvent, createModBarState, applyModStateEvent, getVisiblePanels;
try {
  ({ routeModEvent, createModBarState, applyModStateEvent, getVisiblePanels } = await import('./mod-bar.js'));
} catch {
  routeModEvent = null;
  createModBarState = null;
  applyModStateEvent = null;
  getVisiblePanels = null;
}

// --- routeModEvent (SSE → state routing) ---

test('routeModEvent returns true for mod_state type and applies to state', () => {
  assert.ok(routeModEvent, 'mod-bar.js must export routeModEvent');
  const state = createModBarState();
  const event = {
    type: 'mod_state',
    panel_id: 'blast-radius',
    panel_title: 'Blast Radius',
    slot: 'pane',
    data: { files: ['a.js', 'b.js'], risk: 'low' },
  };
  const result = routeModEvent(state, event);
  assert.equal(result.handled, true);
  assert.equal(result.state.panels['blast-radius'].title, 'Blast Radius');
  assert.equal(result.state.panels['blast-radius'].slot, 'pane');
  assert.deepEqual(result.state.panels['blast-radius'].data.files, ['a.js', 'b.js']);
});

test('routeModEvent returns handled=false for non-mod events', () => {
  assert.ok(routeModEvent, 'mod-bar.js must export routeModEvent');
  const state = createModBarState();
  const result = routeModEvent(state, { type: 'assistant', message: 'hello' });
  assert.equal(result.handled, false);
  assert.deepEqual(result.state, state);
});

test('routeModEvent routes multiple panels to correct slots', () => {
  assert.ok(routeModEvent, 'mod-bar.js must export routeModEvent');
  let state = createModBarState();

  const events = [
    { type: 'mod_state', panel_id: 'tokens', panel_title: 'Tokens', slot: 'bar', data: { total: 500 } },
    { type: 'mod_state', panel_id: 'tree', panel_title: 'Tree', slot: 'pane', data: { nodes: 3 } },
    { type: 'mod_state', panel_id: 'cost', panel_title: 'Cost', slot: 'bar', data: { usd: 0.02 } },
  ];

  for (const e of events) {
    const result = routeModEvent(state, e);
    state = result.state;
  }

  const barPanels = getVisiblePanels(state, 'bar');
  const panePanels = getVisiblePanels(state, 'pane');
  assert.equal(barPanels.length, 2);
  assert.equal(panePanels.length, 1);
  assert.equal(panePanels[0].id, 'tree');
});

test('routeModEvent handles mod_state with status_text (status line bridge)', () => {
  assert.ok(routeModEvent, 'mod-bar.js must export routeModEvent');
  const state = createModBarState();
  const event = {
    type: 'mod_state',
    panel_id: 'status',
    panel_title: 'Status',
    slot: 'bar',
    data: { text: 'Analyzing 14 files...' },
  };
  const result = routeModEvent(state, event);
  assert.equal(result.handled, true);
  assert.equal(result.state.panels['status'].data.text, 'Analyzing 14 files...');
});
