# Screenshot Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to upload a screenshot alongside a message in managed mode, sending it to Claude as a base64 image content block.

**Architecture:** New `/upload` endpoint saves image to temp dir scoped to session. Modified `/message` endpoint reads the image, base64-encodes it into the stream-json `content` array. Web UI gets an image button, preview strip, and inline display. Cleanup on session delete.

**Tech Stack:** Go (net/http, encoding/base64, mime/multipart), Alpine.js, HTML/CSS

---

### Task 1: Upload endpoint — tests

**Files:**
- Create: `server/api/upload_test.go`

- [ ] **Step 1: Write failing tests for the upload endpoint**

```go
package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
)

func createTestPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func createMultipartBody(filename string, data []byte) (*bytes.Buffer, string) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("image", filename)
	part.Write(data)
	writer.Close()
	return body, writer.FormDataContentType()
}

func TestUploadImage(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a managed session
	sessBody := `{"cwd": "` + t.TempDir() + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(sessBody))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessionID := sess["id"].(string)

	// Upload a PNG
	imgData := createTestPNG()
	body, contentType := createMultipartBody("screenshot.png", imgData)
	req, _ = http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/upload", body)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d, want 200, body=%s", resp.StatusCode, string(b))
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["image_id"] == "" {
		t.Error("expected non-empty image_id")
	}
	if result["filename"] != "screenshot.png" {
		t.Errorf("filename=%s, want screenshot.png", result["filename"])
	}
}

func TestUploadImageInvalidType(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sessBody := `{"cwd": "` + t.TempDir() + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(sessBody))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessionID := sess["id"].(string)

	body, contentType := createMultipartBody("file.txt", []byte("not an image"))
	req, _ = http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/upload", body)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestUploadImageSessionNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	imgData := createTestPNG()
	body, contentType := createMultipartBody("screenshot.png", imgData)
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/upload", body)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", contentType)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}

func TestUploadImageHookMode(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a hook-mode session
	_, _ = store.UpsertSession("test", "/tmp/test", "")
	sessions, _ := store.ListSessions(false)
	sessionID := sessions[0].ID

	imgData := createTestPNG()
	body, contentType := createMultipartBody("screenshot.png", imgData)
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/upload", body)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", contentType)
	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != 400 {
		t.Fatalf("status=%d, want 400 for hook mode", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd server && go test ./api/ -v -run TestUpload`
Expected: FAIL — `handleUploadImage` does not exist yet

- [ ] **Step 3: Commit test file**

```bash
git add server/api/upload_test.go
git commit -m "test: add upload image endpoint tests"
```

---

### Task 2: Upload endpoint — implementation

**Files:**
- Create: `server/api/upload.go`
- Modify: `server/api/router.go:61` (add route)

- [ ] **Step 1: Implement the upload handler**

Create `server/api/upload.go`:

```go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const maxUploadSize = 20 << 20 // 20MB

var allowedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// uploadDir returns the upload directory for a session.
func uploadDir(sessionCWD, sessionID string) string {
	return filepath.Join(sessionCWD, ".claude-controller-uploads", sessionID)
}

func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}
	if sess.Mode != "managed" {
		http.Error(w, "not a managed session", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large (max 20MB)", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		// Fallback: check extension
		ext := strings.ToLower(filepath.Ext(header.Filename))
		switch ext {
		case ".png":
			contentType = "image/png"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".gif":
			contentType = "image/gif"
		case ".webp":
			contentType = "image/webp"
		default:
			http.Error(w, "invalid image type (allowed: png, jpeg, gif, webp)", http.StatusBadRequest)
			return
		}
		if !allowedImageTypes[contentType] {
			http.Error(w, "invalid image type", http.StatusBadRequest)
			return
		}
	}

	dir := uploadDir(sess.CWD, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("create upload dir: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Remove any existing upload for this session (one image at a time)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.Remove(filepath.Join(dir, e.Name()))
	}

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".png"
	}
	imageID := uuid.New().String()
	destPath := filepath.Join(dir, imageID+ext)

	dst, err := os.Create(destPath)
	if err != nil {
		log.Printf("create upload file: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		log.Printf("write upload file: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"image_id": imageID,
		"filename": header.Filename,
	})
}

// cleanupSessionUploads removes the upload directory for a session.
func cleanupSessionUploads(sessionCWD, sessionID string) {
	dir := uploadDir(sessionCWD, sessionID)
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("cleanup uploads for session %s: %v", sessionID, err)
	}
	// Remove parent if empty
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err == nil && len(entries) == 0 {
		os.Remove(parent)
	}
}

// findUploadedImage locates the uploaded image file by image ID within the session's upload dir.
// Returns the file path and media type, or empty strings if not found.
func findUploadedImage(sessionCWD, sessionID, imageID string) (string, string) {
	dir := uploadDir(sessionCWD, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, imageID) {
			ext := strings.ToLower(filepath.Ext(name))
			mediaType := ""
			switch ext {
			case ".png":
				mediaType = "image/png"
			case ".jpg", ".jpeg":
				mediaType = "image/jpeg"
			case ".gif":
				mediaType = "image/gif"
			case ".webp":
				mediaType = "image/webp"
			}
			return filepath.Join(dir, name), mediaType
		}
	}
	return "", ""
}

// formatUserTurnWithImage formats a user message with an optional base64 image as a stream-json input line.
func formatUserTurnWithImage(message, imageBase64, mediaType string) string {
	var content []interface{}

	if imageBase64 != "" && mediaType != "" {
		content = append(content, map[string]interface{}{
			"type": "image",
			"source": map[string]string{
				"type":       "base64",
				"media_type": mediaType,
				"data":       imageBase64,
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

// formatImageUploadMessage creates a display string for the chat history when a user sends an image.
func formatImageUploadMessage(message, filename string) string {
	if message != "" {
		return fmt.Sprintf("[📷 %s]\n%s", filename, message)
	}
	return fmt.Sprintf("[📷 %s]", filename)
}
```

- [ ] **Step 2: Add route to router.go**

Add after line 61 (`POST /api/sessions/{id}/shell`):

```go
	apiMux.HandleFunc("POST /api/sessions/{id}/upload", s.handleUploadImage)
```

- [ ] **Step 3: Run upload tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestUpload`
Expected: All 4 tests PASS

- [ ] **Step 4: Commit**

```bash
git add server/api/upload.go server/api/router.go
git commit -m "feat: add image upload endpoint for managed sessions"
```

---

### Task 3: Modify message handler to include images

**Files:**
- Modify: `server/api/managed_sessions.go:133-176` (handleSendMessage)
- Modify: `server/api/upload_test.go` (add integration test)

- [ ] **Step 1: Write failing test for message with image_id**

Add to `server/api/upload_test.go`:

```go
func TestFormatUserTurnWithImage(t *testing.T) {
	result := formatUserTurnWithImage("describe this", "abc123base64data", "image/png")

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatal(err)
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
	if textBlock["type"] != "text" {
		t.Errorf("second block type=%v, want text", textBlock["type"])
	}
	if textBlock["text"] != "describe this" {
		t.Errorf("text=%v, want 'describe this'", textBlock["text"])
	}
}

func TestFormatUserTurnImageOnly(t *testing.T) {
	result := formatUserTurnWithImage("", "abc123base64data", "image/png")

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
```

- [ ] **Step 2: Run tests to verify they pass (these test the helper, not the handler yet)**

Run: `cd server && go test ./api/ -v -run TestFormatUserTurn`
Expected: PASS

- [ ] **Step 3: Modify handleSendMessage to accept image_id**

In `server/api/managed_sessions.go`, modify `handleSendMessage`:

Change the request struct (line 136-138):
```go
	var req struct {
		Message string `json:"message"`
		ImageID string `json:"image_id"`
	}
```

Change the empty-message validation (line 143-145) to allow image-only messages:
```go
	if req.Message == "" && req.ImageID == "" {
		http.Error(w, "message or image_id is required", http.StatusBadRequest)
		return
	}
```

After the `CreateMessage` call (line 176), add image loading logic. Replace the `formatUserTurn(currentMessage)` call (line 315) with image-aware formatting. The key changes:

After line 176, add:
```go
	// Load image if provided
	var imageBase64, imageMediaType, imageFilename string
	if req.ImageID != "" {
		imgPath, mediaType := findUploadedImage(sess.CWD, sessionID, req.ImageID)
		if imgPath != "" {
			imgData, err := os.ReadFile(imgPath)
			if err == nil {
				imageBase64 = base64.StdEncoding.EncodeToString(imgData)
				imageMediaType = mediaType
				imageFilename = filepath.Base(imgPath)
			} else {
				log.Printf("session %s: failed to read image %s: %v", sessionID, req.ImageID, err)
			}
		} else {
			log.Printf("session %s: uploaded image %s not found", sessionID, req.ImageID)
		}
	}
```

Change the `CreateMessage` call to include image info in the display text:
```go
	displayMsg := req.Message
	if imageFilename != "" {
		displayMsg = formatImageUploadMessage(req.Message, imageFilename)
	}
	_, _ = s.store.CreateMessage(sessionID, "user", displayMsg)
```

Change line 315 from:
```go
		userTurn := formatUserTurn(currentMessage)
```
to:
```go
		var userTurn string
		if imageBase64 != "" {
			userTurn = formatUserTurnWithImage(currentMessage, imageBase64, imageMediaType)
			// Only include image on the first turn, not on auto-continues
			imageBase64 = ""
		} else {
			userTurn = formatUserTurn(currentMessage)
		}
```

Add `"encoding/base64"` and `"path/filepath"` to the imports.

- [ ] **Step 4: Run all tests**

Run: `cd server && go test ./api/ -v -run "TestUpload|TestFormatUserTurn"`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/managed_sessions.go server/api/upload_test.go
git commit -m "feat: include uploaded image as base64 content block in messages"
```

---

### Task 4: Session cleanup on delete

**Files:**
- Modify: `server/api/sessions.go:86-98` (handleDeleteSession)
- Modify: `server/api/upload_test.go` (add cleanup test)

- [ ] **Step 1: Write failing test for cleanup**

Add to `server/api/upload_test.go`:

```go
func TestDeleteSessionCleansUpUploads(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	cwd := t.TempDir()
	sessBody := `{"cwd": "` + cwd + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(sessBody))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessionID := sess["id"].(string)

	// Upload an image
	imgData := createTestPNG()
	body, contentType := createMultipartBody("screenshot.png", imgData)
	req, _ = http.NewRequest("POST", ts.URL+"/api/sessions/"+sessionID+"/upload", body)
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", contentType)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	// Verify upload dir exists
	dir := filepath.Join(cwd, ".claude-controller-uploads", sessionID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("upload dir should exist after upload")
	}

	// Delete session
	req, _ = http.NewRequest("DELETE", ts.URL+"/api/sessions/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("delete status=%d, want 200", resp.StatusCode)
	}

	// Verify upload dir is cleaned up
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("upload dir should be removed after session delete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestDeleteSessionCleansUpUploads`
Expected: FAIL — cleanup not implemented yet

- [ ] **Step 3: Add cleanup to handleDeleteSession**

In `server/api/sessions.go`, modify `handleDeleteSession` (line 86-98):

```go
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Clean up uploaded images before deleting session
	sess, err := s.store.GetSessionByID(id)
	if err == nil && sess.Mode == "managed" && sess.CWD != "" {
		cleanupSessionUploads(sess.CWD, id)
	}

	// Teardown managed process if running
	if s.manager != nil {
		s.manager.Teardown(id, 5*time.Second)
	}
	if err := s.store.DeleteSession(id); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd server && go test ./api/ -v -run TestDeleteSessionCleansUpUploads`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/api/sessions.go server/api/upload_test.go
git commit -m "feat: clean up uploaded images when session is deleted"
```

---

### Task 5: Web UI — image button and upload

**Files:**
- Modify: `server/web/static/index.html:271-309` (input bar area)
- Modify: `server/web/static/app.js:1-45` (Alpine data properties)
- Modify: `server/web/static/app.js:1287-1316` (sendManagedMessage)
- Modify: `server/web/static/style.css` (styles for image preview)

- [ ] **Step 1: Add Alpine.js state properties**

In `server/web/static/app.js`, add after line 30 (`inputSuccess: false,`):

```js
    // Image upload state
    pendingImageId: null,
    pendingImagePreview: null,
    pendingImageFilename: null,
    imageUploading: false,
```

- [ ] **Step 2: Add image upload method**

In `server/web/static/app.js`, add a new method after `sendManagedMessage()`:

```js
    async uploadImage(event) {
      const file = event.target.files?.[0];
      if (!file || !this.selectedSessionId) return;

      // Validate client-side
      if (!file.type.startsWith('image/')) {
        this.toast('Please select an image file');
        event.target.value = '';
        return;
      }
      if (file.size > 20 * 1024 * 1024) {
        this.toast('Image must be under 20MB');
        event.target.value = '';
        return;
      }

      this.imageUploading = true;
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
        this.pendingImageId = data.image_id;
        this.pendingImageFilename = data.filename;

        // Create local preview
        const reader = new FileReader();
        reader.onload = (e) => { this.pendingImagePreview = e.target.result; };
        reader.readAsDataURL(file);
      } catch (e) {
        this.toast('Upload failed: ' + e.message);
      }
      this.imageUploading = false;
      event.target.value = '';
    },

    clearPendingImage() {
      this.pendingImageId = null;
      this.pendingImagePreview = null;
      this.pendingImageFilename = null;
    },
```

- [ ] **Step 3: Modify sendManagedMessage to include image_id**

In `server/web/static/app.js`, modify `sendManagedMessage()` (line 1287-1316):

Change the guard (line 1288):
```js
      if ((!this.inputText.trim() && !this.pendingImageId) || !this.selectedSessionId) return;
```

Change the message body (line 1297):
```js
          body: JSON.stringify({
            message: this.inputText.trim(),
            ...(this.pendingImageId && { image_id: this.pendingImageId })
          })
```

Change the chat message push (line 1302) to include image preview:
```js
        const userMsg = {
          role: 'user',
          content: msg,
          msg_type: 'text',
          timestamp: new Date().toISOString(),
          ...(this.pendingImagePreview && { imagePreview: this.pendingImagePreview, imageFilename: this.pendingImageFilename })
        };
        this.chatMessages.push(userMsg);
        this.clearPendingImage();
```

Also update the send button disabled condition in index.html (line 303):
```html
              <button class="btn btn-primary btn-sm" :disabled="(!inputText.trim() && !pendingImageId) || inputSending"
```

- [ ] **Step 4: Add HTML for image button and preview**

In `server/web/static/index.html`, add the image button and preview inside the `.instruction-bar` div.

Add the hidden file input and image button after the shell toggle button (after line 280):

```html
              <button class="image-upload-btn"
                      x-show="currentSession?.mode === 'managed' && !shellMode"
                      @click="$refs.imageInput.click()"
                      :disabled="imageUploading"
                      title="Attach screenshot">
                <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5">
                  <rect x="1" y="2" width="14" height="12" rx="2"/>
                  <circle cx="5" cy="6.5" r="1.5"/>
                  <path d="M1 11l3.5-3.5L7 10l3-4 5 5.5V12a2 2 0 01-2 2H3a2 2 0 01-2-2z"/>
                </svg>
              </button>
              <input type="file" x-ref="imageInput" accept="image/*" capture="environment"
                     @change="uploadImage($event)" style="display:none">
```

Add the image preview strip above the textarea. Insert before the textarea (before line 296):

```html
              <!-- Image preview -->
              <div class="image-preview-strip" x-show="pendingImagePreview" x-cloak>
                <img :src="pendingImagePreview" class="image-preview-thumb">
                <span class="image-preview-name" x-text="pendingImageFilename"></span>
                <button class="image-preview-remove" @click="clearPendingImage()" title="Remove image">&times;</button>
              </div>
```

- [ ] **Step 5: Add CSS styles**

Add to `server/web/static/style.css`:

```css
/* Image upload button */
.image-upload-btn {
  background: none;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-secondary);
  cursor: pointer;
  padding: 4px 6px;
  display: flex;
  align-items: center;
  transition: color 0.15s, border-color 0.15s;
}
.image-upload-btn:hover {
  color: var(--accent);
  border-color: var(--accent);
}
.image-upload-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Image preview strip */
.image-preview-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  margin-bottom: 4px;
  width: 100%;
}
.image-preview-thumb {
  width: 48px;
  height: 48px;
  object-fit: cover;
  border-radius: 4px;
  border: 1px solid var(--border);
}
.image-preview-name {
  font-size: 0.8rem;
  color: var(--text-secondary);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.image-preview-remove {
  background: none;
  border: none;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 1.2rem;
  padding: 0 4px;
  line-height: 1;
}
.image-preview-remove:hover {
  color: var(--danger);
}
```

- [ ] **Step 6: Add inline image display in chat messages**

In `server/web/static/index.html`, find where user messages are rendered in the chat. Add image display inside the user message bubble. Look for the user message template and add:

```html
                <img x-show="msg.imagePreview" :src="msg.imagePreview"
                     style="max-width:200px;max-height:150px;border-radius:6px;margin-bottom:4px;cursor:pointer;display:block"
                     @click="window.open(msg.imagePreview, '_blank')">
```

- [ ] **Step 7: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js server/web/static/style.css
git commit -m "feat: add screenshot upload UI with preview and inline display"
```

---

### Task 6: Build verification and full test run

**Files:** None (verification only)

- [ ] **Step 1: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests pass

- [ ] **Step 2: Verify the server builds**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds with no errors

- [ ] **Step 3: Clean up build artifact**

Run: `rm server/claude-controller`

- [ ] **Step 4: Commit any remaining changes and verify clean state**

Run: `git status`
Expected: Working tree clean (all changes committed)
