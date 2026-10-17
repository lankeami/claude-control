/**
 * Jump to Bottom — decides when the floating "jump to bottom" button
 * should be visible over the chat, and which element actually scrolls.
 *
 * Desktop: #chat-area is the scroller (overflow-y: auto).
 * Mobile (<=768px): the chat area grows to its content height and the
 * document itself scrolls, so visibility must be computed against
 * document.scrollingElement instead.
 *
 * The 150px threshold matches scrollToBottom's near-bottom auto-scroll
 * check in app.js.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 */
export const JUMP_THRESHOLD_PX = 150;

// Returns the element whose scroll position governs the chat: the chat
// area when it overflows, otherwise the document scroller (mobile).
export function pickScroller(chatEl, docEl) {
  if (chatEl && chatEl.scrollHeight > chatEl.clientHeight + 1) return chatEl;
  return docEl || null;
}

export function shouldShowJumpToBottom(el, threshold = JUMP_THRESHOLD_PX) {
  if (!el) return false;
  const { scrollHeight, scrollTop, clientHeight } = el;
  // Nothing to scroll — content fits in the viewport.
  if (scrollHeight <= clientHeight) return false;
  return (scrollHeight - scrollTop - clientHeight) >= threshold;
}

// Browser globals — allow app.js (loaded as a regular script) to call these
// after this module has been loaded via <script type="module">.
if (typeof window !== 'undefined') {
  window._ccShouldShowJumpToBottom = shouldShowJumpToBottom;
  window._ccPickScroller = pickScroller;
}
