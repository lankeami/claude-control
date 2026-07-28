// Tests for sendShortcut() shell-mode behavior.
// Uses source inspection to verify the real app.js handles mode correctly,
// plus a self-contained behavior test of the expected logic.
// Run with: node server/web/static/test-send-shortcut.js
'use strict';

const fs   = require('fs');
const path = require('path');

const appJs = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');

let passed = 0, failed = 0;

function assert(label, condition) {
  if (condition) {
    console.log(`  PASS: ${label}`);
    passed++;
  } else {
    console.error(`  FAIL: ${label}`);
    failed++;
  }
}

// ── Part 1: Source inspection ────────────────────────────────────────────────
// Verify the real sendShortcut function in app.js accepts a mode parameter
// and prepends "! " when mode === 'shell'.
// These checks are RED before the implementation and GREEN after.

const fnMatch = appJs.match(/sendShortcut\(([^)]*)\)/);
const params  = fnMatch ? fnMatch[1] : '';
assert('sendShortcut declares a mode parameter', /\bmode\b/.test(params));

// Extract body between sendShortcut(...) { and the closing },
const bodyMatch = appJs.match(/sendShortcut\([^)]*\)\s*\{([\s\S]*?)\n    \},/);
const body = bodyMatch ? bodyMatch[1] : '';
assert('sendShortcut body checks mode === \'shell\'', /mode\s*===\s*['"]shell['"]/.test(body));
assert('sendShortcut body prepends "! "', /['"]!\s['"]/.test(body));

// ── Part 2: Behavior test ────────────────────────────────────────────────────
// Self-contained test of the expected sendShortcut logic.
// This documents the contract that the implementation must satisfy.

function makeSendShortcut() {
  const ctx = {
    showShortcutPicker: true,
    inputText: '',
    handleInputCalled: false,
    handleInput() { this.handleInputCalled = true; },
  };
  // Mirrors exactly what app.js sendShortcut should do after the change.
  ctx.sendShortcut = function(value, mode) {
    this.showShortcutPicker = false;
    this.inputText = mode === 'shell' ? '! ' + value : value;
    this.handleInput();
  };
  return ctx;
}

const c1 = makeSendShortcut();
c1.sendShortcut('npm test', 'shell');
assert('shell mode sets inputText to "! npm test"', c1.inputText === '! npm test');
assert('shell mode closes picker',                  c1.showShortcutPicker === false);
assert('shell mode calls handleInput',              c1.handleInputCalled === true);

const c2 = makeSendShortcut();
c2.sendShortcut('Deploy to prod', 'normal');
assert('normal mode sends value as-is', c2.inputText === 'Deploy to prod');

const c3 = makeSendShortcut();
c3.sendShortcut('Hello world');
assert('no mode arg sends value as-is', c3.inputText === 'Hello world');

const c4 = makeSendShortcut();
c4.sendShortcut('echo hi', '');
assert('empty-string mode sends value as-is', c4.inputText === 'echo hi');

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
