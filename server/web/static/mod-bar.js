export function createModBarState() {
  return { panels: {}, visible: false };
}

export function applyModStateEvent(state, event) {
  if (!event || !event.panel_id) {
    return state;
  }

  const panels = { ...state.panels };

  if (event.data === null || event.data === undefined) {
    delete panels[event.panel_id];
  } else {
    panels[event.panel_id] = {
      id: event.panel_id,
      title: event.panel_title || event.panel_id,
      slot: event.slot || 'bar',
      data: event.data,
      updatedAt: Date.now(),
    };
  }

  const visible = Object.keys(panels).length > 0;
  return { panels, visible };
}

export function getVisiblePanels(state, slot) {
  return Object.values(state.panels).filter(p => p.slot === slot);
}

export function computePaneLayout(state, paneOpen, paneWidth) {
  const panePanels = getVisiblePanels(state, 'pane');
  const showPane = paneOpen && panePanels.length > 0;
  const width = paneWidth || 320;
  return {
    showPane,
    chatFlex: '1',
    paneFlex: showPane ? `0 0 ${width}px` : '0',
  };
}

export function routeModEvent(state, event) {
  if (!event || event.type !== 'mod_state') {
    return { handled: false, state };
  }
  const nextState = applyModStateEvent(state, event);
  return { handled: true, state: nextState };
}
