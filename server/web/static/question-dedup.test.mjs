import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { questionCardExists } from './question-dedup.js';

const dir = dirname(fileURLToPath(import.meta.url));

test('no dedup for empty/missing tool_use_id', () => {
  assert.equal(questionCardExists([], ''), false);
  assert.equal(questionCardExists([{ role: 'question', toolUseId: '' }], ''), false);
});

test('matches an existing question card by tool_use_id', () => {
  const msgs = [{ role: 'question', toolUseId: 'toolu_1', answered: false }];
  assert.equal(questionCardExists(msgs, 'toolu_1'), true);
});

test('an already-answered card still dedups the buffered-flush duplicate', () => {
  // The card the user answered stays in the transcript marked answered; when
  // the CLI later flushes the buffered assistant entry it carries the same id.
  const msgs = [{ role: 'question', toolUseId: 'toolu_1', answered: true }];
  assert.equal(questionCardExists(msgs, 'toolu_1'), true);
});

test('a genuinely new question (different id) is not deduped', () => {
  const msgs = [{ role: 'question', toolUseId: 'toolu_1', answered: true }];
  assert.equal(questionCardExists(msgs, 'toolu_2'), false);
});

// --- Reproduction: replay the interactive event sequence -------------------
// Model the two front-end push paths through a reducer that shares the dedup
// predicate, and prove the duplicate that the screenshot showed cannot recur.

function showQuestionCard(chatMessages, toolUseId) {
  if (questionCardExists(chatMessages, toolUseId)) return false;
  chatMessages.push({ role: 'question', toolUseId, answered: false });
  return true;
}

test('reproduction: buffered-flush no longer pushes a second card', () => {
  const chatMessages = [];
  const id = 'toolu_012EBBZudszqsciRT9PNrUjH';

  // 1. PreToolUse hook -> pending_question SSE (Path A) shows the live card.
  showQuestionCard(chatMessages, id);
  // 2. User answers it; the card is marked answered, pendingQuestion cleared.
  chatMessages.find((m) => m.role === 'question').answered = true;
  // 3. CLI flushes the buffered assistant entry -> raw assistant tool_use
  //    (Path B) with the SAME tool_use_id.
  showQuestionCard(chatMessages, id);

  const cards = chatMessages.filter((m) => m.role === 'question');
  assert.equal(cards.length, 1, 'exactly one card survives the buffered flush');
});

test('reproduction: reconnect pending_question resend does not duplicate', () => {
  const chatMessages = [];
  const id = 'toolu_reconnect';
  showQuestionCard(chatMessages, id); // initial SSE
  showQuestionCard(chatMessages, id); // catch-up resend on SSE reconnect
  assert.equal(chatMessages.filter((m) => m.role === 'question').length, 1);
});

// --- Wiring: app.js and index.html must actually use the shared path -------

const appJs = readFileSync(join(dir, 'app.js'), 'utf8');
const html = readFileSync(join(dir, 'index.html'), 'utf8');

test('both SSE question paths route through the shared showQuestionCard helper', () => {
  assert.match(appJs, /showQuestionCard\s*\(/);
  // The raw-assistant AskUserQuestion branch must not push a bare question card
  // directly anymore — it must go through the deduped helper.
  const bIdx = appJs.indexOf("block.name === 'AskUserQuestion'");
  assert.ok(bIdx > 0, 'AskUserQuestion branch present');
  const bBranch = appJs.slice(bIdx, bIdx + 800);
  assert.match(bBranch, /showQuestionCard/);
});

test('question cards carry their toolUseId so cross-path dedup works', () => {
  const defIdx = appJs.indexOf('showQuestionCard(toolUseId, questions, timestamp)');
  assert.ok(defIdx > 0, 'showQuestionCard method definition present');
  const fn = appJs.slice(defIdx, defIdx + 500);
  assert.match(fn, /role:\s*'question'/);
  assert.match(fn, /toolUseId/);
});

test('index.html loads the question-dedup module before app.js', () => {
  assert.match(html, /question-dedup\.js/);
  assert.ok(
    html.indexOf('question-dedup.js') < html.indexOf('/app.js'),
    'question-dedup.js must load before app.js',
  );
});
