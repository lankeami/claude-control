/**
 * Workflow run card — state helpers for `workflow_run` SSE events.
 *
 * A managed session that invokes Claude Code's built-in Workflow tool (e.g.
 * the autoship pipeline) does its real work in background subagents. The
 * server watches the run's sidecar journal and pushes full-run snapshots on
 * the per-session SSE stream; these helpers reduce those snapshots into the
 * state the live run card renders.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 */

/** Upsert a workflow_run snapshot into the runs list, keyed by run_id. */
export function upsertWorkflowRun(runs, snapshot) {
  if (!snapshot || !snapshot.run_id) return runs || [];
  snapshot._lastUpdated = Date.now();
  const next = (runs || []).slice();
  const i = next.findIndex((r) => r.run_id === snapshot.run_id);
  if (i >= 0) {
    if (!snapshot._sessionId && next[i]._sessionId) {
      snapshot._sessionId = next[i]._sessionId;
      snapshot._sessionName = next[i]._sessionName;
    }
    next[i] = snapshot;
  } else {
    next.push(snapshot);
  }
  return next;
}

/**
 * Mark tool workflow runs from a specific session as completed or stale when
 * the session's SSE stream closes. If all agents are complete the run is
 * "completed"; otherwise it becomes "stale".
 */
export function markSessionRunsTerminal(runs, sessionId) {
  if (!runs || !sessionId) return runs || [];
  let changed = false;
  const next = runs.map((r) => {
    if (r._sessionId !== sessionId || (r.status !== 'running')) return r;
    const allComplete = r.agents && r.agents.length > 0 &&
      r.agents.every((a) => a.status === 'complete');
    changed = true;
    return { ...r, status: allComplete ? 'completed' : 'stale' };
  });
  return changed ? next : runs;
}

/**
 * Flatten an agent's structured result into [{key, value, isUrl}] entries.
 * Only scalar values render; http(s) URLs are flagged for link rendering.
 */
export function workflowRunResultLinks(result) {
  if (!result || typeof result !== 'object') return [];
  const entries = [];
  for (const [key, raw] of Object.entries(result)) {
    if (raw === null || typeof raw === 'object') continue;
    const value = String(raw);
    const isUrl = /^https?:\/\//i.test(value);
    entries.push({ key, value, isUrl });
  }
  return entries;
}

if (typeof window !== 'undefined') {
  window._ccUpsertWorkflowRun = upsertWorkflowRun;
  window._ccWorkflowRunResultLinks = workflowRunResultLinks;
  window._ccMarkSessionRunsTerminal = markSessionRunsTerminal;
}
