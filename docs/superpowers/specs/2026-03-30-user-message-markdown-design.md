# User Message Markdown Rendering

## Problem

In the chat UI, assistant messages are rendered with full markdown support (via `marked.js`), but user messages are HTML-escaped and rendered as plain text. This means backtick code spans, bold, italic, lists, and other markdown in user messages display as raw characters rather than formatted text.

## Solution

Run user messages through the same `marked.parse()` + `sanitizeHtml()` pipeline already used for assistant messages. Add CSS overrides so markdown elements render correctly on the blue user bubble background.

## Changes

### `app.js` — `bubbleHTML(msg)` function (~line 2153)

**Current behavior:**
```javascript
return `${esc(msg.content)}${time}`;
```

**New behavior:**
```javascript
// Same marked.parse() + sanitizeHtml() pipeline as assistant messages
return `<div class="markdown-content">${sanitizedMarkdownHTML}</div>${time}`;
```

The same custom renderer configuration applies:
- Links open in new tabs (`target="_blank"`, `rel="noopener noreferrer"`)
- Code blocks use highlight.js when available
- Output is sanitized via `sanitizeHtml()`

### `style.css` — new `.chat-bubble.user .markdown-content` overrides

The existing `.markdown-content` styles assume dark text on a light/gray background. On the white-on-blue user bubble, these elements need color overrides:

| Element | Current (assistant) | Override (user) |
|---------|-------------------|-----------------|
| Inline `code` background | `rgba(0,0,0,0.15)` | `rgba(255,255,255,0.2)` |
| `pre` block background | `rgba(0,0,0,0.25)` | `rgba(0,0,0,0.3)` |
| `blockquote` border | `var(--border)` | `rgba(255,255,255,0.4)` |
| `blockquote` text color | `var(--text-muted)` | `rgba(255,255,255,0.8)` |
| Links | `color: inherit; text-decoration: underline` | Same (inherits white from parent) |
| Table borders | implicit `var(--border)` | `rgba(255,255,255,0.3)` |

### `style.css` — remove `white-space: pre-wrap` from `.chat-bubble.user`

Currently user bubbles use `white-space: pre-wrap` to preserve newlines in plain text. With markdown rendering, whitespace is handled by `<p>` tags and `<pre>` blocks, so `pre-wrap` should be removed to avoid double-spacing.

## Files Modified

1. `server/web/static/app.js` — `bubbleHTML()` function
2. `server/web/static/style.css` — user bubble markdown overrides

## What Doesn't Change

- Assistant message rendering (untouched)
- Tool message rendering (untouched)
- The `esc()` helper function (still used elsewhere)
- HTML sanitization logic (already shared)
- Blue bubble color, alignment, and visual identity
- No new dependencies

## Edge Cases

- **Empty messages**: `marked.parse("")` returns `""` — no issue
- **XSS**: Handled by existing `sanitizeHtml()` which strips disallowed tags and event handlers
- **Messages without markdown**: Render identically to before (plain text wrapped in `<p>` tags, visually the same)
- **highlight.js on blue**: Code blocks use `rgba(0,0,0,0.3)` background which provides sufficient contrast for syntax-highlighted code on blue
