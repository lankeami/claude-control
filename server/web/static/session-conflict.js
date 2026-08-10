/**
 * Session Conflict — resolves the "repo already has a session" dialog.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 *
 * decision "yes"  → retry session create with a new worktree + name
 * decision "no"   → select the existing session for that cwd
 */
export function resolveSessionConflict(decision, { cwd, name, sessions }) {
  if (decision === 'yes') {
    const trimmed = (name || '').trim();
    if (!trimmed) return { action: 'error', message: 'Enter a name for the new session' };
    return { action: 'retry', payload: { cwd, worktree: true, name: trimmed } };
  }
  const match = (sessions || []).find((s) => s.cwd === cwd);
  if (!match) return { action: 'error', message: 'Existing session not found' };
  return { action: 'select', sessionId: match.id };
}

if (typeof window !== 'undefined') {
  window._ccResolveSessionConflict = resolveSessionConflict;
}
