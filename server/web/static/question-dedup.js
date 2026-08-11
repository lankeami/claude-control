/**
 * Question dedup — decides whether an incoming AskUserQuestion should create a
 * new card, given the question cards already in the transcript.
 *
 * Two independent front-end paths surface the same AskUserQuestion:
 *   - the typed `pending_question` SSE event (real-time, from the PreToolUse
 *     hook) — the canonical live signal in the interactive backend
 *   - the raw `assistant` tool_use block, broadcast when the CLI flushes the
 *     buffered transcript entry AT resolution time, and the print backend's
 *     only question signal
 * Both carry the same tool_use_id. Without dedup the second path pushes a
 * duplicate, unanswerable card AFTER the question is already resolved — the
 * "2 of the same questions" bug.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 */
export function questionCardExists(chatMessages, toolUseId) {
  if (!toolUseId) return false;
  return (chatMessages || []).some((m) => m.role === 'question' && m.toolUseId === toolUseId);
}

if (typeof window !== 'undefined') {
  window._ccQuestionCardExists = questionCardExists;
}
