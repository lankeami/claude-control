import { test } from 'node:test';
import assert from 'node:assert/strict';
import { extractOptions } from './option-selector.js';

// --- "Option A/B/C:" pattern ---

test('detects Option A/B with titles', () => {
  const content = `Here are your choices:\n\n**Option A: Deploy with Docker**\nFast setup.\n\n**Option B: Deploy on bare metal**\nMore control.`;
  const result = extractOptions(content);
  assert.equal(result.length, 2);
  assert.equal(result[0].label, 'Option A');
  assert.equal(result[0].text, 'Option A: Deploy with Docker');
  assert.equal(result[1].label, 'Option B');
  assert.equal(result[1].text, 'Option B: Deploy with Docker'.replace('Docker', 'bare metal').replace('Deploy with', 'Deploy on'));
});

test('detects Option 1/2/3 with titles', () => {
  const content = `Option 1: Use SQLite\nOption 2: Use PostgreSQL\nOption 3: Use MySQL`;
  const result = extractOptions(content);
  assert.equal(result.length, 3);
  assert.equal(result[0].label, 'Option 1');
  assert.equal(result[0].text, 'Option 1: Use SQLite');
  assert.equal(result[1].label, 'Option 2');
  assert.equal(result[2].label, 'Option 3');
});

test('detects Option A alone (no title)', () => {
  const content = `Option A\nOption B\nOption C`;
  const result = extractOptions(content);
  assert.equal(result.length, 3);
  assert.equal(result[0].text, 'Option A');
  assert.equal(result[1].text, 'Option B');
});

test('detects options in markdown list items (- **Option A: ...**)', () => {
  const content = `Pick one:\n- **Option A: Rewrite**\n- **Option B: Patch**`;
  const result = extractOptions(content);
  assert.equal(result.length, 2);
  assert.equal(result[0].label, 'Option A');
  assert.equal(result[0].text, 'Option A: Rewrite');
});

// --- Capital letter + dot pattern ---

test('detects A. / B. / C. options', () => {
  const content = `A. Rewrite the module\nB. Patch the existing code\nC. Remove it entirely`;
  const result = extractOptions(content);
  assert.equal(result.length, 3);
  assert.equal(result[0].label, 'A');
  assert.equal(result[0].text, 'A. Rewrite the module');
  assert.equal(result[2].text, 'C. Remove it entirely');
});

// --- Parenthesis pattern ---

test('detects a) / b) / c) options', () => {
  const content = `a) Keep the current approach\nb) Switch to the new API\nc) Hybrid approach`;
  const result = extractOptions(content);
  assert.equal(result.length, 3);
  assert.equal(result[0].label, 'a');
  assert.equal(result[0].text, 'a) Keep the current approach');
});

test('detects 1) / 2) / 3) options', () => {
  const content = `1) Fast but risky\n2) Slow but safe\n3) Somewhere in between`;
  const result = extractOptions(content);
  assert.equal(result.length, 3);
  assert.equal(result[0].text, '1) Fast but risky');
  assert.equal(result[1].text, '2) Slow but safe');
});

// --- Minimum count guard ---

test('returns empty array when fewer than 2 options detected', () => {
  const content = `Option A: The only option`;
  const result = extractOptions(content);
  assert.deepEqual(result, []);
});

test('returns empty array for plain prose with no options', () => {
  const content = `Here is the summary of your project. Everything looks good.`;
  const result = extractOptions(content);
  assert.deepEqual(result, []);
});

// --- Markdown stripping ---

test('strips bold markers from option titles', () => {
  const content = `**Option A: **Deploy with Docker****\n**Option B: Use Kubernetes**`;
  const result = extractOptions(content);
  assert.equal(result.length, 2);
  assert.ok(!result[0].text.includes('**'), `expected no ** in: ${result[0].text}`);
});

test('strips heading markers from option lines', () => {
  const content = `## Option A: First choice\n## Option B: Second choice`;
  const result = extractOptions(content);
  assert.equal(result.length, 2);
  assert.equal(result[0].label, 'Option A');
});

// --- Edge cases ---

test('returns empty array for empty string', () => {
  assert.deepEqual(extractOptions(''), []);
});

test('returns empty array for null/undefined', () => {
  assert.deepEqual(extractOptions(null), []);
  assert.deepEqual(extractOptions(undefined), []);
});

test('does not treat regular numbered list (1. 2. 3.) as options', () => {
  const content = `Steps:\n1. Install dependencies\n2. Run the server\n3. Open the browser`;
  const result = extractOptions(content);
  // "1." pattern is NOT detected — only "1)" parenthesis and "Option N:" forms are
  assert.deepEqual(result, []);
});
