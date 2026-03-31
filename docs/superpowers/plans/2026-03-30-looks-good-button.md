# 👍 Looks Good Button — Implementation Plan

**Spec:** `docs/superpowers/specs/2026-03-30-looks-good-button-design.md`

## Steps

### 1. Add `sendLgtm()` method to app.js

In the Alpine.js data object, add:

```javascript
sendLgtm() {
    this.inputText = '👍 Looks Good To Me';
    this.handleInput();
}
```

### 2. Add button to index.html

Inside `.instruction-bar`, before the shell toggle button, add:

```html
<button class="lgtm-btn"
        @click="sendLgtm()"
        :disabled="inputSending"
        title="Send 👍 Looks Good To Me">👍</button>
```

### 3. Add CSS for `.lgtm-btn` in style.css

Style to match `.shell-toggle-btn` dimensions but with emoji content. Place adjacent styles near the existing `.shell-toggle-btn` rules.

### 4. Test both modes

- Verify button appears in managed mode sessions
- Verify button appears in hook mode sessions
- Verify clicking sends the message immediately
- Verify button is disabled during send
