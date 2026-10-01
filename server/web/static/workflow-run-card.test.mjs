import { test } from 'node:test';
import assert from 'node:assert/strict';
import { upsertWorkflowRun, workflowRunResultLinks } from './workflow-run-card.js';

// --- upsertWorkflowRun ------------------------------------------------------
// Reducer for `workflow_run` SSE snapshots: each snapshot is the full state of
// one Workflow tool run; runs are keyed by run_id and keep first-seen order.

test('a new run is appended', () => {
  const runs = upsertWorkflowRun([], { type: 'workflow_run', run_id: 'wf_1', status: 'running', agents: [] });
  assert.equal(runs.length, 1);
  assert.equal(runs[0].run_id, 'wf_1');
});

test('a snapshot for a known run replaces it in place', () => {
  const r1 = { type: 'workflow_run', run_id: 'wf_1', status: 'running', agents: [] };
  const r2 = { type: 'workflow_run', run_id: 'wf_2', status: 'running', agents: [] };
  let runs = upsertWorkflowRun(upsertWorkflowRun([], r1), r2);
  const updated = { type: 'workflow_run', run_id: 'wf_1', status: 'completed', agents: [{ id: 'a1', status: 'complete' }] };
  runs = upsertWorkflowRun(runs, updated);
  assert.equal(runs.length, 2);
  assert.equal(runs[0].run_id, 'wf_1');
  assert.equal(runs[0].status, 'completed');
  assert.equal(runs[0].agents.length, 1);
  assert.equal(runs[1].run_id, 'wf_2');
});

test('snapshots without a run_id are ignored', () => {
  const runs = upsertWorkflowRun([], { type: 'workflow_run', status: 'running' });
  assert.equal(runs.length, 0);
});

// --- workflowRunResultLinks -------------------------------------------------
// Flattens an agent's structured result into display entries; URL values are
// flagged so the card renders them as links.

test('url values become links, scalars become text', () => {
  const entries = workflowRunResultLinks({
    issueUrl: 'https://github.com/o/r/issues/292',
    issueNumber: 292,
    branchName: 'feat/autoship-292',
  });
  const byKey = Object.fromEntries(entries.map((e) => [e.key, e]));
  assert.equal(byKey.issueUrl.isUrl, true);
  assert.equal(byKey.issueUrl.value, 'https://github.com/o/r/issues/292');
  assert.equal(byKey.issueNumber.isUrl, false);
  assert.equal(byKey.issueNumber.value, '292');
  assert.equal(byKey.branchName.isUrl, false);
});

test('nested objects and null results are tolerated', () => {
  assert.deepEqual(workflowRunResultLinks(null), []);
  assert.deepEqual(workflowRunResultLinks(undefined), []);
  const entries = workflowRunResultLinks({ summary: { nested: true } });
  // Nested objects are skipped — only scalars render on the card.
  assert.equal(entries.length, 0);
});

test('javascript: urls are not treated as links', () => {
  const entries = workflowRunResultLinks({ bad: 'javascript:alert(1)' });
  assert.equal(entries[0].isUrl, false);
});
