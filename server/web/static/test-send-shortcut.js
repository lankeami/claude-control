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
// Verify sendShortcut in app.js:
//   - accepts a mode parameter
//   - calls executeShell() for managed+shell (no '!' prefix injection needed)
//   - still prefixes '! ' for hook+shell sessions

const fnMatch = appJs.match(/sendShortcut\(([^)]*)\)/);
const params  = fnMatch ? fnMatch[1] : '';
assert('sendShortcut declares a mode parameter', /\bmode\b/.test(params));

const bodyMatch = appJs.match(/sendShortcut\([^)]*\)\s*\{([\s\S]*?)\n    \},/);
const body = bodyMatch ? bodyMatch[1] : '';
assert('sendShortcut checks mode === \'shell\'', /mode\s*===\s*['"]shell['"]/.test(body));
assert('sendShortcut calls executeShell() for managed sessions', /executeShell\(\)/.test(body));
assert('sendShortcut still prefixes "! " for hook sessions', /['"]!\s['"]/.test(body));

// ── Part 2: Behavior test ────────────────────────────────────────────────────
// Self-contained test of the expected sendShortcut routing logic.

function makeCtx(sessionMode) {
  return {
    showShortcutPicker: true,
    inputText: '',
    executeShellCalled: false,
    handleInputCalled: false,
    currentSession: { mode: sessionMode },
    executeShell() { this.executeShellCalled = true; },
    handleInput()  { this.handleInputCalled  = true; },

    sendShortcut(value, mode) {
      this.showShortcutPicker = false;
      const sess = this.currentSession;
      if (mode === 'shell' && sess?.mode === 'managed') {
        this.inputText = value;
        this.executeShell();
      } else if (mode === 'shell') {
        this.inputText = '! ' + value;
        this.handleInput();
      } else {
        this.inputText = value;
        this.handleInput();
      }
    },
  };
}

// managed + shell → executeShell, raw command
const c1 = makeCtx('managed');
c1.sendShortcut('git status', 'shell');
assert('managed+shell: executeShell called',      c1.executeShellCalled === true);
assert('managed+shell: handleInput NOT called',   c1.handleInputCalled  === false);
assert('managed+shell: inputText is raw command', c1.inputText          === 'git status');
assert('managed+shell: picker closed',            c1.showShortcutPicker === false);

// hook + shell → handleInput with '! ' prefix
const c2 = makeCtx('hook');
c2.sendShortcut('git status', 'shell');
assert('hook+shell: handleInput called',    c2.handleInputCalled  === true);
assert('hook+shell: inputText has ! prefix', c2.inputText         === '! git status');

// normal mode → handleInput, no prefix
const c3 = makeCtx('managed');
c3.sendShortcut('Deploy to prod', 'normal');
assert('normal mode: handleInput called',  c3.handleInputCalled  === true);
assert('normal mode: no ! prefix',        c3.inputText          === 'Deploy to prod');

// no mode arg (backward compat)
const c4 = makeCtx('managed');
c4.sendShortcut('Hello world');
assert('no mode: handleInput called',     c4.handleInputCalled  === true);
assert('no mode: value unchanged',        c4.inputText          === 'Hello world');

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed > 0 ? 1 : 0);
