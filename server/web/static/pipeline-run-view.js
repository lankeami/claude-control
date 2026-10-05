/**
 * Pipeline run detail view — state helpers for the chat-pane takeover.
 *
 * Clicking a run in the Pipeline Runs sidebar list opens a detail view that
 * takes over the main chat pane (the same pattern session selection uses),
 * showing a running activity log: live-updating while the pipeline runs,
 * the final log once it finishes.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 */

/**
 * Whether the pipeline run detail view has taken over the chat pane.
 * Active when either a DB-backed pipeline run or a Claude Workflow tool run
 * is selected from the sidebar list.
 */
export function pipelineRunViewActive(selectedPipelineRun, selectedToolWorkflowRunId) {
  return Boolean(selectedPipelineRun) || Boolean(selectedToolWorkflowRunId);
}

const STATUS_ICONS = { completed: '✓', running: '▶', failed: '✕' };
const STATUS_TONES = {
  completed: '#22c55e',
  running: '#f59e0b',
  failed: '#ef4444',
};

/**
 * Map pipeline run items to ordered activity-log entries the takeover view
 * renders directly: {id, icon, tone, label, sessionId, error, running}.
 */
export function pipelineRunLogEntries(items) {
  return (items || []).map((item) => ({
    id: item.id,
    icon: STATUS_ICONS[item.status] || '●',
    tone: STATUS_TONES[item.status] || '#6b7280',
    label: item.feature_label || '',
    sessionId: item.session_id || '',
    error: item.error || '',
    running: item.status === 'running',
  }));
}

/**
 * Live-updating log for running pipelines; final (static) log for finished
 * ones — poll the detail endpoint only while the run is still running.
 */
export function shouldPollPipelineRunDetail(run) {
  return Boolean(run) && run.status === 'running';
}

if (typeof window !== 'undefined') {
  window._ccPipelineRunViewActive = pipelineRunViewActive;
  window._ccPipelineRunLogEntries = pipelineRunLogEntries;
  window._ccShouldPollPipelineRunDetail = shouldPollPipelineRunDetail;
}
