# Multi-Image Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to attach up to 7 images per message in a managed Claude session, uploading each to the existing `/upload` endpoint and sending all image IDs together in a single `claude -p` turn.

**Architecture:** Sequential per-file uploads to the existing `/upload` endpoint accumulate into a `pendingImages` array on the frontend. On send, all IDs are passed as `image_ids []string` to the message handler, which loads each file from disk and builds a multi-image content block array for the Claude turn. The backend removes the one-at-a-time upload constraint and the `formatUserTurnWithImage` function is replaced by `formatUserTurnWithImages` accepting a slice.

**Tech Stack:** Go 1.22, Alpine.js (CDN), vanilla HTML/CSS; no new dependencies.

---

## File Map

| File | Change |
|---|---|
| `server/api/upload.go` | Add `imageData` struct; rename `formatUserTurnWithImage` → `formatUserTurnWithImages` accepting `[]imageData`; remove one-at-a-time dir cleanup (lines 87–93) |
| `server/api/upload_test.go` | Update existing format tests to use new function; add multi-image format test; add multi-upload accumulation test |
| `server/api/managed_sessions.go` | `ImageID string` → `ImageIDs []string`; replace single-image load with loop; update `displayMsg` and turn-send code |
| `server/web/static/app.js` | Replace scalar pending-image state with `pendingImages []`; rewrite `uploadImage()`; add `removePendingImage(index)`; rename `clearPendingImage()` → `clearPendingImages()`; update `sendManagedMessage()`; update `bubbleHTML()` |
| `server/web/static/index.html` | Add `multiple` to file input; replace single-thumbnail preview div with multi-thumbnail strip; update send button disabled binding |

---

### Task 1: Add `imageData` type and `formatUserTurnWithImages` (TDD)

**Files:**
- Modify: `server/api/upload.go`
- Modify: `server/api/upload_test.go`

- [ ] **Step 1: Write failing tests for the new function**

In `server/api/upload_test.go`, replace `TestFormatUserTurnWithImage` and `TestFormatUserTurnImageOnly` and add `TestFormatUserTurnWithMultipleImages`:

```go
func TestFormatUserTurnWithImages_SingleWithText(t *testing.T) {
	images := []imageData{{base64: "abc123", mediaType: "image/png"}}
	result := formatUserTurnWithImages("describe this", images)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["type"] != "user" {
		t.Errorf("type=%v, want user", parsed["type"])
	}
	msg := parsed["message"].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}
	imgBlock := content[0].(map[string]interface{})
	if imgBlock["type"] != "image" {
		t.Errorf("first block type=%v, want image", imgBlock["type"])
	}
	source := imgBlock["source"].(map[string]interface{})
	if source["media_type"] != "image/png" {
		t.Errorf("media_type=%v, want image/png", source["media_type"])
	}
	textBlock := content[1].(map[string]interface{})
	if textBlock["type"] != "text" || textBlock["text"] != "describe this" {
		t.Errorf("unexpected text block: %v", textBlock)
	}
}

func TestFormatUserTurnWithImages_ImageOnly(t *testing.T) {
	images := []imageData{{base64: "abc123", mediaType: "image/png"}}
	result := formatUserTurnWithImages("", images)

	var parsed map[string]interface{}
	json.Unmarshal([]byte(result), &parsed)
	msg := parsed["message"].(map[string]interface{})
	content := msg["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected 1 content block (image only), got %d", len(content))
	}
	if content[0].(map[string]interface{})["type"] != "image" {
		t.Error("expected image block")
	}
}

func TestFormatUserTurnWithImages_MultipleImages(t *testing.T) {
	images := []imageData{
		{base64: "data1", mediaType: "image/png"},
		{base64: "data2", mediaType: "image/jpeg"},
	}
	result := formatUserTurnWithImages("compare these", images)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
	}
	msg := parsed["message"].(map[string]interface{})
	content := msg["content"].([]interface{})
	// 2 image blocks + 1 text block
	if len(content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(content))
	}
	if content[0].(map[string]interface{})["type"] != "image" {
		t.Errorf("block 0 should be image")
	}
	if content[1].(map[string]interface{})["type"] != "image" {
		t.Errorf("block 1 should be image")
	}
	src0 := content[0].(map[string]interface{})["source"].(map[string]interface{})
	if src0["media_type"] != "image/png" {
		t.Errorf("block 0 media_type=%v, want image/png", src0["media_type"])
	}
	src1 := content[1].(map[string]interface{})["source"].(map[string]interface{})
	if src1["media_type"] != "image/jpeg" {
		t.Errorf("block 1 media_type=%v, want image/jpeg", src1["media_type"])
	}
	textBlock := content[2].(map[string]interface{})
	if textBlock["type"] != "text" || textBlock["text"] != "compare these" {
		t.Errorf("unexpected text block: %v", textBlock)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd server && go test ./api/ -v -run "TestFormatUserTurnWithImages"
```

Expected: compile error — `imageData` undefined and `formatUserTurnWithImages` undefined.

- [ ] **Step 3: Add `imageData` type and `formatUserTurnWithImages` to `upload.go`**

In `server/api/upload.go`, add after the `extensionToMIME` map (after line 30):

```go
// imageData holds base64-encoded image bytes and its MIME type.
type imageData struct {
	base64    string
	mediaType string
}
```

Then replace the existing `formatUserTurnWithImage` function (lines 174–204) with:

```go
// formatUserTurnWithImages builds a stream-json user turn with one image content
// block per entry in images, followed by an optional text block.
func formatUserTurnWithImages(message string, images []imageData) string {
	var content []interface{}
	for _, img := range images {
		content = append(content, map[string]interface{}{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": img.mediaType,
				"data":       img.base64,
			},
		})
	}
	if message != "" {
		content = append(content, map[string]string{
			"type": "text",
			"text": message,
		})
	}
	turn := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": content,
		},
	}
	b, _ := json.Marshal(turn)
	return string(b)
}
```

Keep `formatImageUploadMessage` unchanged. Delete the old `formatUserTurnWithImage` function entirely.

- [ ] **Step 4: Run tests — confirm they pass**

```bash
cd server && go test ./api/ -v -run "TestFormatUserTurnWithImages"
```

Expected: all three `TestFormatUserTurnWithImages_*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd server && git add api/upload.go api/upload_test.go
git commit -m "feat(api): add imageData type and formatUserTurnWithImages for multi-image support"
```

---

### Task 2: Remove one-at-a-time upload constraint (TDD)

**Files:**
- Modify: `server/api/upload.go`
- Modify: `server/api/upload_test.go`

- [ ] **Step 1: Write failing test for multi-upload accumulation**

In `server/api/upload_test.go`, add:

```go
func TestUploadMultipleImagesAccumulate(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	cwd := t.TempDir()
	body := `{"cwd": "` + cwd + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessID := sess["id"].(string)

	uploadOnePNG := func() string {
		pngData := createTestPNG()
		multiBody, contentType := createMultipartBody("test.png", pngData)
		uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessID+"/upload", multiBody)
		uploadReq.Header.Set("Authorization", "Bearer test-api-key")
		uploadReq.Header.Set("Content-Type", contentType)
		uploadResp, err := http.DefaultClient.Do(uploadReq)
		if err != nil {
			t.Fatal(err)
		}
		defer uploadResp.Body.Close()
		if uploadResp.StatusCode != 200 {
			t.Fatalf("upload status=%d, want 200", uploadResp.StatusCode)
		}
		var result map[string]interface{}
		json.NewDecoder(uploadResp.Body).Decode(&result)
		return result["image_id"].(string)
	}

	id1 := uploadOnePNG()
	id2 := uploadOnePNG()

	// Both files should exist on disk
	dir := filepath.Join(cwd, ".claude-controller-uploads", sessID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read upload dir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 files in upload dir, got %d", len(entries))
	}

	// Both IDs should be findable
	path1, _ := findUploadedImage(cwd, sessID, id1)
	if path1 == "" {
		t.Error("image 1 not found after second upload")
	}
	path2, _ := findUploadedImage(cwd, sessID, id2)
	if path2 == "" {
		t.Error("image 2 not found after second upload")
	}
}
```

- [ ] **Step 2: Run test — confirm it fails**

```bash
cd server && go test ./api/ -v -run "TestUploadMultipleImagesAccumulate"
```

Expected: FAIL — second upload deletes the first file, so `len(entries)` is 1, not 2.

- [ ] **Step 3: Remove one-at-a-time cleanup from `handleUploadImage`**

In `server/api/upload.go`, delete lines 87–93 (the comment and loop that removes existing files):

```go
// Delete these 7 lines:
	// Remove any existing files in the dir (one image at a time).
	existing, _ := os.ReadDir(dir)
	for _, entry := range existing {
		if !entry.IsDir() {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
```

The `MkdirAll` call on line 82 stays; the UUID assignment on line 96 stays.

- [ ] **Step 4: Run all upload tests — confirm everything passes**

```bash
cd server && go test ./api/ -v -run "TestUpload"
```

Expected: all `TestUpload*` tests PASS.

- [ ] **Step 5: Commit**

```bash
cd server && git add api/upload.go api/upload_test.go
git commit -m "feat(api): allow multiple images to accumulate in session upload dir"
```

---

### Task 3: Update message handler to accept `image_ids []string`

**Files:**
- Modify: `server/api/managed_sessions.go`

- [ ] **Step 1: Update the request struct**

In `server/api/managed_sessions.go` around line 145, change:

```go
// Before
var req struct {
    Message string `json:"message"`
    ImageID string `json:"image_id"`
    Model   string `json:"model"`
}
```

To:

```go
// After
var req struct {
    Message  string   `json:"message"`
    ImageIDs []string `json:"image_ids"`
    Model    string   `json:"model"`
}
```

- [ ] **Step 2: Update the empty-body validation**

Change line 154:

```go
// Before
if req.Message == "" && req.ImageID == "" {
    http.Error(w, "message or image_id is required", http.StatusBadRequest)
    return
}

// After
if req.Message == "" && len(req.ImageIDs) == 0 {
    http.Error(w, "message or image_ids is required", http.StatusBadRequest)
    return
}
```

- [ ] **Step 3: Replace single-image load with multi-image loop**

Replace lines 187–201 (the `// Load uploaded image if provided` block):

```go
// Before
var imageBase64, imageMediaType string
if req.ImageID != "" {
    imgPath, mediaType := findUploadedImage(sess.CWD, sessionID, req.ImageID)
    if imgPath != "" {
        imgData, readErr := os.ReadFile(imgPath)
        if readErr == nil {
            imageBase64 = base64.StdEncoding.EncodeToString(imgData)
            imageMediaType = mediaType
        } else {
            log.Printf("session %s: failed to read image %s: %v", sessionID, req.ImageID, readErr)
        }
    } else {
        log.Printf("session %s: uploaded image %s not found", sessionID, req.ImageID)
    }
}
```

With:

```go
// After
var images []imageData
for _, id := range req.ImageIDs {
    imgPath, mediaType := findUploadedImage(sess.CWD, sessionID, id)
    if imgPath == "" {
        log.Printf("session %s: uploaded image %s not found", sessionID, id)
        continue
    }
    imgBytes, readErr := os.ReadFile(imgPath)
    if readErr != nil {
        log.Printf("session %s: failed to read image %s: %v", sessionID, id, readErr)
        continue
    }
    images = append(images, imageData{
        base64:    base64.StdEncoding.EncodeToString(imgBytes),
        mediaType: mediaType,
    })
}
```

- [ ] **Step 4: Update `displayMsg` assignment**

Replace lines 204–207:

```go
// Before
displayMsg := req.Message
if imageBase64 != "" {
    displayMsg = formatImageUploadMessage(req.Message, req.ImageID)
}
```

With:

```go
// After
displayMsg := req.Message
if len(images) > 0 {
    label := fmt.Sprintf("%d image", len(images))
    if len(images) > 1 {
        label += "s"
    }
    displayMsg = formatImageUploadMessage(req.Message, label)
}
```

- [ ] **Step 5: Update the turn-send code inside the goroutine**

Replace lines 344–350 (inside the `for` loop in the goroutine):

```go
// Before
var userTurn string
if imageBase64 != "" {
    userTurn = formatUserTurnWithImage(currentMessage, imageBase64, imageMediaType)
    imageBase64 = "" // Only include image on the first turn, not auto-continues
} else {
    userTurn = formatUserTurn(currentMessage)
}
```

With:

```go
// After
var userTurn string
if len(images) > 0 {
    userTurn = formatUserTurnWithImages(currentMessage, images)
    images = nil // Only include images on the first turn, not auto-continues
} else {
    userTurn = formatUserTurn(currentMessage)
}
```

- [ ] **Step 6: Build to confirm no compile errors**

```bash
cd server && go build ./...
```

Expected: exits 0, no errors.

- [ ] **Step 7: Run all API tests**

```bash
cd server && go test ./api/ -v
```

Expected: all tests PASS.

- [ ] **Step 8: Commit**

```bash
cd server && git add api/managed_sessions.go
git commit -m "feat(api): accept image_ids []string in message handler for multi-image turns"
```

---

### Task 4: Update frontend state, upload handler, and send logic in `app.js`

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Replace scalar pending-image state with array**

Find lines 159–163 (the image upload state comment block):

```js
// Image upload state
pendingImageId: null,
pendingImagePreview: null,
pendingImageFilename: null,
imageUploading: false,
```

Replace with:

```js
// Image upload state
pendingImages: [],   // [{id, preview, filename}]
imageUploading: false,
```

- [ ] **Step 2: Rewrite `uploadImage()`**

Find the `uploadImage(event)` method (around line 1663) and replace the entire function body:

```js
async uploadImage(event) {
    const files = Array.from(event.target.files || []);
    if (!files.length || !this.selectedSessionId) return;

    if (this.pendingImages.length + files.length > 7) {
        this.toast('Maximum 7 images per message');
        event.target.value = '';
        return;
    }

    this.imageUploading = true;
    for (const file of files) {
        if (!file.type.startsWith('image/')) {
            this.toast(`${file.name}: not an image, skipped`);
            continue;
        }
        if (file.size > 20 * 1024 * 1024) {
            this.toast(`${file.name}: must be under 20MB, skipped`);
            continue;
        }
        try {
            const formData = new FormData();
            formData.append('image', file);
            const res = await fetch(`/api/sessions/${this.selectedSessionId}/upload`, {
                method: 'POST',
                headers: { 'Authorization': 'Bearer ' + this.apiKey },
                body: formData
            });
            if (!res.ok) throw new Error(await res.text());
            const data = await res.json();
            const preview = await new Promise(resolve => {
                const reader = new FileReader();
                reader.onload = (e) => resolve(e.target.result);
                reader.readAsDataURL(file);
            });
            this.pendingImages.push({ id: data.image_id, preview, filename: data.filename });
        } catch (e) {
            this.toast('Upload failed: ' + e.message);
        }
    }
    this.imageUploading = false;
    event.target.value = '';
},
```

- [ ] **Step 3: Replace `clearPendingImage()` with `clearPendingImages()` and add `removePendingImage()`**

Find the `clearPendingImage()` method (around line 1704) and replace it with two methods:

```js
clearPendingImages() {
    this.pendingImages = [];
},

removePendingImage(index) {
    this.pendingImages.splice(index, 1);
},
```

- [ ] **Step 4: Update `sendManagedMessage()`**

Around line 1711, find the guard:

```js
if ((!this.inputText.trim() && !this.pendingImageId) || !this.selectedSessionId) return;
```

Change to:

```js
if ((!this.inputText.trim() && !this.pendingImages.length) || !this.selectedSessionId) return;
```

Around lines 1718–1725, replace the snapshot and clear block:

```js
// Before
const imageId = this.pendingImageId;
const imagePreview = this.pendingImagePreview;
const imageFilename = this.pendingImageFilename;
this.clearPendingImage();

try {
    const body = { message: msg, model: this.selectedModel };
    if (imageId) body.image_id = imageId;
```

With:

```js
// After
const images = [...this.pendingImages];
this.clearPendingImages();

try {
    const body = { message: msg, model: this.selectedModel };
    if (images.length) body.image_ids = images.map(img => img.id);
```

Around lines 1735–1739, replace the optimistic message construction:

```js
// Before
const userMsg = { role: 'user', content: msg || `[Screenshot: ${imageFilename}]`, msg_type: 'text', timestamp: new Date().toISOString() };
if (imagePreview) {
    userMsg.imagePreview = imagePreview;
    userMsg.imageFilename = imageFilename;
}
```

With:

```js
// After
const label = images.length === 1 ? `[Screenshot: ${images[0].filename}]` : `[${images.length} images]`;
const userMsg = { role: 'user', content: msg || label, msg_type: 'text', timestamp: new Date().toISOString() };
if (images.length) {
    userMsg.images = images.map(img => ({ preview: img.preview, filename: img.filename }));
}
```

- [ ] **Step 5: Update `bubbleHTML()` to render multi-image thumbnails**

Around line 3012, replace the single-image preview block:

```js
// Before
let imgHtml = '';
if (msg.imagePreview) {
    imgHtml = `<img src="${msg.imagePreview}" style="max-width:200px;max-height:150px;border-radius:6px;margin-bottom:4px;cursor:pointer;display:block" onclick="window.open(this.src,'_blank')">`;
}
```

With:

```js
// After
let imgHtml = '';
if (msg.images && msg.images.length) {
    imgHtml = msg.images.map(img =>
        `<img src="${img.preview}" style="max-width:200px;max-height:150px;border-radius:6px;margin-bottom:4px;margin-right:4px;cursor:pointer;display:inline-block" onclick="window.open(this.src,'_blank')" title="${esc(img.filename)}">`
    ).join('');
} else if (msg.imagePreview) {
    // backward compat: messages sent before this change
    imgHtml = `<img src="${msg.imagePreview}" style="max-width:200px;max-height:150px;border-radius:6px;margin-bottom:4px;cursor:pointer;display:block" onclick="window.open(this.src,'_blank')">`;
}
```

- [ ] **Step 6: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat(ui): update pending-image state to array for multi-image upload"
```

---

### Task 5: Update HTML — file input and preview strip

**Files:**
- Modify: `server/web/static/index.html`

- [ ] **Step 1: Add `multiple` to the file input**

Find line 321–322:

```html
<input type="file" x-ref="imageInput" accept="image/*"
       @change="uploadImage($event)" style="display:none">
```

Change to:

```html
<input type="file" x-ref="imageInput" accept="image/*" multiple
       @change="uploadImage($event)" style="display:none">
```

- [ ] **Step 2: Replace single-thumbnail preview strip**

Find lines 340–345:

```html
<!-- Image preview -->
<div class="image-preview-strip" x-show="pendingImagePreview" x-cloak>
    <img :src="pendingImagePreview" class="image-preview-thumb">
    <span class="image-preview-name" x-text="pendingImageFilename"></span>
    <button class="image-preview-remove" @click="clearPendingImage()" title="Remove image">&times;</button>
</div>
```

Replace with:

```html
<!-- Multi-image preview strip -->
<div class="image-preview-strip" x-show="pendingImages.length > 0" x-cloak style="display:flex;flex-wrap:wrap;gap:6px;padding:6px 0">
    <template x-for="(img, idx) in pendingImages" :key="idx">
        <div class="image-preview-item" style="position:relative;display:inline-flex;flex-direction:column;align-items:center;gap:2px">
            <img :src="img.preview" class="image-preview-thumb">
            <span class="image-preview-name" x-text="img.filename"></span>
            <button class="image-preview-remove" @click="removePendingImage(idx)" title="Remove image">&times;</button>
        </div>
    </template>
</div>
```

- [ ] **Step 3: Update send button disabled binding**

Find line 355:

```html
<button class="btn btn-primary btn-sm" :disabled="(!inputText.trim() && !pendingImageId) || inputSending"
```

Change to:

```html
<button class="btn btn-primary btn-sm" :disabled="(!inputText.trim() && !pendingImages.length) || inputSending"
```

- [ ] **Step 4: Commit**

```bash
git add server/web/static/index.html
git commit -m "feat(ui): multi-thumbnail preview strip with per-image remove buttons"
```

---

### Task 6: Build and smoke-test

- [ ] **Step 1: Build the Go server**

```bash
cd server && go build -o claude-controller .
```

Expected: exits 0, binary created.

- [ ] **Step 2: Run full test suite**

```bash
cd server && go test ./... -v
```

Expected: all tests PASS, zero failures.

- [ ] **Step 3: Search for any remaining references to old single-image identifiers**

```bash
grep -rn "pendingImageId\|pendingImagePreview\|pendingImageFilename\|clearPendingImage()\|image_id\b" server/web/static/
```

Expected: zero hits (the only `image_id` remaining should be inside the `uploadImage` response parse: `data.image_id`).

- [ ] **Step 4: Commit build artifact cleanup (delete binary)**

```bash
rm server/claude-controller
git status
```

Expected: working tree clean after binary removal (binary should be gitignored; if not, add it).

- [ ] **Step 5: Final commit**

```bash
git add -p  # stage only if anything remains
git commit -m "chore: verify multi-image upload build and tests pass"
```
