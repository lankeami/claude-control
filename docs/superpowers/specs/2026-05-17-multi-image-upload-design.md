# Multi-Image Upload Design

**Date:** 2026-05-17
**Status:** Approved
**Approach:** Sequential uploads, array state

## Overview

Extend the managed-session message input to support up to 7 images per message. Each image is uploaded individually to the existing `/upload` endpoint, held in a `pendingImages` array, and sent together in a single `claude -p` turn via an updated `image_ids` payload field.

## Architecture

### Frontend (`server/web/static/`)

**State change — `app.js`**

Replace the three scalar pending-image fields with a single array:

```js
// Remove
pendingImageId: null,
pendingImagePreview: null,
pendingImageFilename: null,

// Add
pendingImages: [],  // [{id, preview, filename}]
// imageUploading: false — unchanged
```

**Upload handler — `uploadImage(event)`**

- File input gains `multiple` attribute
- Handler loops over `event.target.files`
- Before uploading, checks `pendingImages.length + newFiles.length > 7`; if exceeded, toasts "Maximum 7 images per message" and aborts
- Each file is validated (type must be `image/*`, size must be under 20 MB) then uploaded sequentially to the existing `POST /api/sessions/{id}/upload` endpoint
- On success, appends `{id, preview, filename}` to `pendingImages`
- On per-file failure, toasts the error and continues with remaining files
- `imageUploading` is set `true` for the whole batch and `false` when the loop finishes

**Guard in `sendManagedMessage`**

```js
// Before
if ((!this.inputText.trim() && !this.pendingImageId) || ...)

// After
if ((!this.inputText.trim() && !this.pendingImages.length) || ...)
```

**Send payload**

```js
const body = { message: msg, model: this.selectedModel };
if (this.pendingImages.length) {
  body.image_ids = this.pendingImages.map(img => img.id);
}
```

**Optimistic chat message**

The user message pushed to `chatMessages` gets an `images` array (one entry per attached image with `{preview, filename}`) so all thumbnails render inline, consistent with current single-image behavior.

**Clear helper**

`clearPendingImage()` renamed to `clearPendingImages()`, resets `pendingImages` to `[]`.

### UI — `index.html`

- File input: add `multiple` attribute
- Preview strip: replace single-image preview div with a horizontally scrolling strip that renders one thumbnail per `pendingImages` entry
- Each thumbnail has a filename label and an ✕ button calling `removePendingImage(index)`
- Strip shows when `pendingImages.length > 0`
- Send button disabled when `!inputText.trim() && !pendingImages.length`

### Backend (`server/api/`)

**`upload.go` — remove one-at-a-time constraint**

Lines 87–89 currently delete all existing files in the session upload dir before saving a new one. Remove this cleanup block. Files now accumulate per session until the message is sent or the session is deleted. Cleanup-on-delete is unchanged.

**`managed_sessions.go` — widen request struct**

```go
// Before
ImageID string `json:"image_id"`

// After
ImageIDs []string `json:"image_ids"`
```

Validation: `req.Message == "" && len(req.ImageIDs) == 0` → 400.

Image loading loop:

```go
var images []imageData
for _, id := range req.ImageIDs {
    imgPath, mediaType := findUploadedImage(sess.CWD, sessionID, id)
    if imgPath == "" {
        log.Printf("session %s: uploaded image %s not found", sessionID, id)
        continue
    }
    imgData, err := os.ReadFile(imgPath)
    if err != nil {
        log.Printf("session %s: failed to read image %s: %v", sessionID, id, err)
        continue
    }
    images = append(images, imageData{
        base64:    base64.StdEncoding.EncodeToString(imgData),
        mediaType: mediaType,
    })
}
```

**`upload.go` — update `formatUserTurnWithImage`**

Rename to `formatUserTurnWithImages(message string, images []imageData) string`. Emits one image content block per entry, followed by a text block (omitted if message is empty). Single-image behavior is preserved when `len(images) == 1`.

## Data Flow

```
User selects files
  → uploadImage() loops files
  → POST /upload (one per file)
  → append to pendingImages[]
  → preview strip renders thumbnails

User clicks Send
  → snapshot pendingImages, clear state
  → POST /message { message, image_ids: [...] }
  → backend loads each image from disk
  → formatUserTurnWithImages() builds content blocks
  → passed to claude -p as user turn
  → optimistic chat message with all thumbnails appended
```

## Error Handling

| Scenario | Behaviour |
|---|---|
| File not image type | Toast, skip that file, continue |
| File > 20 MB | Toast, skip that file, continue |
| > 7 images selected | Toast "Maximum 7 images per message", abort entire selection |
| Upload request fails | Toast error, skip that image, continue |
| Image missing on send | Log warning server-side, skip that image, proceed with remaining |

## Constraints

- Max 7 images per message (client-side enforcement)
- Max 20 MB per image (unchanged from current)
- Allowed types: png, jpg, gif, webp (unchanged)
- Upload-on-send order: images appear in content blocks in the order they were uploaded
- `imageUploading` flag disables the attach button for the duration of the batch upload

## Files Changed

| File | Change |
|---|---|
| `server/web/static/app.js` | State array, upload loop, send payload, clear helper |
| `server/web/static/index.html` | `multiple` on input, multi-thumbnail preview strip |
| `server/api/upload.go` | Remove one-at-a-time cleanup; rename/update `formatUserTurnWithImages` |
| `server/api/managed_sessions.go` | `ImageIDs []string`, image loading loop |

## Out of Scope

- Hook-mode sessions (upload already returns 400 for hook mode; unchanged)
- iOS app (separate client; not modified)
- Drag-and-drop image attachment
- Reordering images before send
