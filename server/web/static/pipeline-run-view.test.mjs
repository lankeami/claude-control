import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  pipelineRunViewActive,
  pipelineRunLogEntries,
  shouldPollPipelineRunDetail,
} from './pipeline-run-view.js';

// --- pipelineRunViewActive ---------------------------------------------------
// The chat pane is taken over by the pipeline run detail view whenever a
// pipeline run (DB-backed) or a Claude Workflow tool run is selected.

test('inactive when nothing is selected', () => {
  assert.equal(pipelineRunViewActive(null, null), false);
  assert.equal(pipelineRunViewActive(undefined, undefined), false);
});

test('active when a pipeline run is selected', () => {
  assert.equal(pipelineRunViewActive({ id: 'pr_1', status: 'running' }, null), true);
});

test('active when a tool workflow run is selected', () => {
  assert.equal(pipelineRunViewActive(null, 'wf_123'), true);
});

// --- pipelineRunLogEntries ---------------------------------------------------
// Items become ordered activity-log entries with a status icon + tone the
// takeover view renders directly.

test('entries preserve item order and map status to icon/tone', () => {
  const entries = pipelineRunLogEntries([
    { id: 'i1', feature_label: 'issue created', status: 'completed' },
    { id: 'i2', feature_label: 'tdd', status: 'running', session_id: 'sess_9' },
    { id: 'i3', feature_label: 'ship', status: 'failed', error: 'CI red' },
    { id: 'i4', feature_label: 'release', status: 'pending' },
  ]);
  assert.equal(entries.length, 4);
  assert.deepEqual(entries.map((e) => e.id), ['i1', 'i2', 'i3', 'i4']);

  assert.equal(entries[0].icon, '✓');
  assert.equal(entries[1].icon, '▶');
  assert.equal(entries[2].icon, '✕');
  assert.equal(entries[3].icon, '●');

  assert.equal(entries[1].running, true);
  assert.equal(entries[0].running, false);

  assert.equal(entries[1].sessionId, 'sess_9');
  assert.equal(entries[0].sessionId, '');
  assert.equal(entries[2].error, 'CI red');

  assert.equal(entries[0].label, 'issue created');
});

test('empty or missing items produce an empty log', () => {
  assert.deepEqual(pipelineRunLogEntries([]), []);
  assert.deepEqual(pipelineRunLogEntries(null), []);
});

// --- shouldPollPipelineRunDetail ---------------------------------------------
// Live-updating log for running pipelines; final (static) log otherwise.

test('polls only while the run is running', () => {
  assert.equal(shouldPollPipelineRunDetail({ status: 'running' }), true);
  assert.equal(shouldPollPipelineRunDetail({ status: 'completed' }), false);
  assert.equal(shouldPollPipelineRunDetail({ status: 'failed' }), false);
  assert.equal(shouldPollPipelineRunDetail({ status: 'cancelled' }), false);
  assert.equal(shouldPollPipelineRunDetail(null), false);
});
