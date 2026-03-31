# User Message Markdown Rendering — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render user messages with the same markdown support that assistant messages already have.

**Architecture:** Extend the existing `bubbleHTML()` function to run user messages through `marked.parse()` + `sanitizeHtml()`. Add CSS overrides for markdown elements on the blue user bubble background.

**Tech Stack:** marked.js (already loaded), highlight.js (already loaded), CSS

**Spec:** `docs/superpowers/specs/2026-03-30-user-message-markdown-design.md`

---

### Task 1: Route user messages through the markdown pipeline

**Files:**
- Modify: `server/web/static/app.js:2139-2153`

- [ ] **Step 1: Expand the markdown branch to include user messages**

In `bubbleHTML(msg)`, change the condition on line 2140 from:

```javascript
        if ((msg.role === 'assistant' || msg.command) && typeof marked !== 'undefined') {
```

to:

```javascript
        if ((msg.role === 'assistant' || msg.role === 'user' || msg.command) && typeof marked !== 'undefined') {
```

This makes user text messages enter the same `marked.parse()` + custom renderer + `sanitizeHtml()` path that assistant messages already use. The `eyebrow` variable will be empty string for user messages (since `msg.command` is falsy), so the output is just `<div class="markdown-content">...</div>` + timestamp.

- [ ] **Step 2: Verify the change manually**

Run the server:
```bash
cd server && go run .
```

Open the web UI, navigate to a managed session, and send a message containing markdown: `` `inline code` and **bold** and *italic* ``. Confirm the user bubble renders formatted markdown instead of raw backticks/asterisks.

- [ ] **Step 3: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: render user messages with markdown via marked.js"
```

---

### Task 2: Switch user bubble whitespace from pre-wrap to normal

**Files:**
- Modify: `server/web/static/style.css:1049`

- [ ] **Step 1: Change white-space on user bubbles**

On line 1049, change:

```css
.chat-bubble.user { white-space: pre-wrap; }
```

to:

```css
.chat-bubble.user { white-space: normal; }
```

This matches how `.chat-bubble.assistant` works on line 1048. With markdown rendering, whitespace is handled by `<p>` tags and `<pre>` blocks — `pre-wrap` would cause double-spacing.

- [ ] **Step 2: Verify manually**

Send a multi-line message in the web UI. Confirm paragraphs render correctly (not double-spaced) and that code blocks still preserve whitespace.

- [ ] **Step 3: Commit**

```bash
git add server/web/static/style.css
git commit -m "fix: switch user bubble whitespace to normal for markdown rendering"
```

---

### Task 3: Add CSS overrides for markdown elements on blue background

**Files:**
- Modify: `server/web/static/style.css:1075` (insert after the existing markdown styles)

- [ ] **Step 1: Add user bubble markdown overrides**

Insert the following CSS block after line 1075 (after the `.markdown-content th, .markdown-content td` rule), before the `/* ===== Mobile Menu */` comment:

```css
/* Markdown in user bubbles (white text on blue background) */
.chat-bubble.user .markdown-content code {
  background: rgba(255,255,255,0.2);
}
.chat-bubble.user .markdown-content pre {
  background: rgba(0,0,0,0.3);
}
.chat-bubble.user .markdown-content blockquote {
  border-left-color: rgba(255,255,255,0.4);
  color: rgba(255,255,255,0.8);
}
.chat-bubble.user .markdown-content th,
.chat-bubble.user .markdown-content td {
  border-color: rgba(255,255,255,0.3);
}
```

These override the default dark-on-light markdown styles for the white-on-blue user bubble context:
- **Inline code**: light translucent background instead of dark
- **Code blocks**: slightly darker translucent background (good contrast for syntax highlighting)
- **Blockquotes**: white-ish border and text instead of theme border/muted
- **Table borders**: white-ish instead of theme border

Links already use `color: inherit; text-decoration: underline` which naturally inherits white from the user bubble.

- [ ] **Step 2: Verify all markdown elements visually**

Send test messages in the web UI to verify each element:

1. `` `inline code` `` — should show white text on light translucent background
2. A fenced code block — should show syntax-highlighted code on dark translucent background
3. `> blockquote` — should show white-ish border and slightly muted white text
4. A markdown table — should show white-ish borders
5. `**bold**` and `*italic*` — should render normally (no override needed)
6. A markdown link `[text](url)` — should be white and underlined

- [ ] **Step 3: Commit**

```bash
git add server/web/static/style.css
git commit -m "style: add markdown CSS overrides for user bubbles on blue background"
```
