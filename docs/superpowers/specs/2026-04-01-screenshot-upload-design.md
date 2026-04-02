# Screenshot Upload for Managed Sessions

## Overview

Add the ability for users to upload a screenshot/image alongside a message in managed mode sessions. Images are stored temporarily for the session lifetime and cleaned up when the session ends. One image per message, managed mode only.

## Motivation

Developers often want to show Claude what they're seeing — a UI bug, an error dialog, a design mockup. Currently the only way to share visual context is to describe it in text. Screenshot upload lets users attach an image directly to a message, leveraging Claude's native vision capabilities.

## Architecture

Single image upload per message. Image flows through the system as:

```
Web UI → multipart POST /upload → server saves to temp dir
→ user sends message with image_id → server base64-encodes file
→ stream-json content block [{type: "image", ...}, {type: "text", ...}]
→ claude -p stdin → Claude processes image + text
→ NDJSON response streams back via SSE (unchanged)
```

## API Changes

### New Endpoint: `POST /api/sessions/{id}/upload`

Accepts `multipart/form-data` with a single image file field named `image`.

**Validation:**
- Session must exist and be in `managed` mode
- File must be an image: `image/png`, `image/jpeg`, `image/gif`, `image/webp`
- File size must be under 20MB (Claude's limit)

**Behavior:**
- Saves file to `{session_CWD}/.claude-controller-uploads/{sessionID}/{uuid}.{ext}`
- Creates the upload directory if it doesn't exist
- Uploading a new image replaces any existing image for that session (deletes the old file)
- Returns JSON: `{ "image_id": "<uuid>", "filename": "<original_name>" }`

**Error responses:**
- `400` — not managed mode, invalid file type, missing file
- `404` — session not found
- `413` — file too large

### Modified Endpoint: `POST /api/sessions/{id}/message`

**Request body changes:**
```json
{
  "message": "describe what you see in this screenshot",
  "image_id": "uuid-of-uploaded-image"   // optional, new field
}
```

**Behavior when `image_id` is present:**
1. Locate the uploaded file by `image_id` in the session's upload directory
2. Read and base64-encode the file
3. Determine MIME type from file extension
4. Build the `content` array with both an image block and a text block:

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "image",
        "source": {
          "type": "base64",
          "media_type": "image/png",
          "data": "<base64-encoded-data>"
        }
      },
      {
        "type": "text",
        "text": "user's message text"
      }
    ]
  }
}
```

**If `image_id` is absent**, behavior is unchanged — single text content block as today.

**If `message` is empty but `image_id` is present**, send the image block only with no text block. The user may just want Claude to look at the image without additional instructions.

**Error handling:**
- If the image file is missing or unreadable, send the message without the image and log a warning. Do not fail the entire message.

### Session Cleanup

When a managed session is deleted (existing `DELETE /api/sessions/{id}` handler):
- Remove the directory `{session_CWD}/.claude-controller-uploads/{sessionID}/` and all contents
- This is best-effort; log warnings on failure but don't block session deletion

## Database Changes

None. The `image_id` is transient — it maps to a file on disk, not a database record. The message stored in the `messages` table for a user turn with an image will contain the text portion only (same as today). The image data lives only in the temp file and in the stream-json payload sent to Claude.

## Web UI Changes

### Input Area

Add an image attachment button next to the existing send button in the chat input bar:
- Icon: camera/image icon (SVG inline)
- On click: opens a hidden `<input type="file" accept="image/*" capture="environment">`
  - `accept="image/*"` enables both file picker and camera on mobile
  - `capture="environment"` hints to use the rear camera on mobile devices
- Only visible for managed mode sessions

### Image Preview

When a file is selected:
1. Upload immediately to `POST /api/sessions/{id}/upload`
2. Show a thumbnail preview above the input area (small, ~80px height)
3. Display the filename next to the thumbnail
4. Add an "X" button to discard the attachment (clears the preview and `image_id` state)
5. If upload fails, show an error toast and clear the selection

### Sending

When the user clicks send with an image attached:
1. Include `image_id` in the message JSON payload
2. Clear the image preview after sending
3. Show the image inline in the chat as part of the user's message bubble (as a thumbnail that can be clicked to view full size)

### State Management

Alpine.js reactive data additions:
- `pendingImageId: null` — the uploaded image's ID
- `pendingImagePreview: null` — data URL for the thumbnail preview
- `pendingImageFilename: null` — original filename for display

These are cleared after sending or discarding.

## iOS App Changes

Out of scope for this iteration. The iOS app can be updated separately to support image upload using the same API endpoints.

## File Storage

### Directory Structure
```
{session_CWD}/
  .claude-controller-uploads/
    {sessionID}/
      {uuid}.png
```

### Why `.claude-controller-uploads/`?
- Dot-prefix keeps it hidden from normal directory listings
- Scoped under session CWD so it's co-located with the working directory
- Session ID subdirectory prevents collisions between sessions sharing a CWD
- Single file per session (new upload replaces old) keeps storage minimal

### Cleanup
- On session delete: remove `{CWD}/.claude-controller-uploads/{sessionID}/`
- If the parent `.claude-controller-uploads/` directory is empty after cleanup, remove it too

## Security Considerations

- **File type validation**: Check both the `Content-Type` header and file extension. Only allow `image/png`, `image/jpeg`, `image/gif`, `image/webp`.
- **File size limit**: 20MB max, enforced at the HTTP level via `http.MaxBytesReader`.
- **Path traversal**: Image IDs are server-generated UUIDs — no user-controlled path components reach the filesystem.
- **Auth**: Upload endpoint requires the same API key auth as all other session endpoints.

## Testing Strategy

- Unit tests for the upload handler: valid image, invalid type, too large, session not found, not managed mode
- Unit test for message handler with `image_id`: verify content array includes image block
- Unit test for session deletion cleanup
- Integration test: upload image → send message with image_id → verify stream-json output includes image content block
