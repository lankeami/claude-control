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
  const next = (runs || []).slice();
  const i = next.findIndex((r) => r.run_id === snapshot.run_id);
  if (i >= 0) {
    next[i] = snapshot;
  } else {
    next.push(snapshot);
  }
  return next;
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
}
