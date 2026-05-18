package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

	// Create a managed session with a real temp dir as CWD
	cwd := t.TempDir()
	body := `{"cwd": "` + cwd + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessID := sess["id"].(string)

	// Upload a valid PNG
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
		t.Fatalf("status=%d, want 200", uploadResp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(uploadResp.Body).Decode(&result)
	if result["image_id"] == nil || result["image_id"] == "" {
		t.Error("expected non-empty image_id in response")
	}
	if result["filename"] != "test.png" {
		t.Errorf("filename=%v, want test.png", result["filename"])
	}
}

func TestUploadImageInvalidType(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	cwd := t.TempDir()
	body := `{"cwd": "` + cwd + `"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/sessions/create", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sess map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	sessID := sess["id"].(string)

	// Upload a .txt file
	multiBody, contentType := createMultipartBody("document.txt", []byte("hello world"))
	uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+sessID+"/upload", multiBody)
	uploadReq.Header.Set("Authorization", "Bearer test-api-key")
	uploadReq.Header.Set("Content-Type", contentType)

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != 400 {
		t.Fatalf("status=%d, want 400 for invalid file type", uploadResp.StatusCode)
	}
}

func TestUploadImageSessionNotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	pngData := createTestPNG()
	multiBody, contentType := createMultipartBody("test.png", pngData)
	uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/sessions/nonexistent/upload", multiBody)
	uploadReq.Header.Set("Authorization", "Bearer test-api-key")
	uploadReq.Header.Set("Content-Type", contentType)

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404 for nonexistent session", uploadResp.StatusCode)
	}
}

func TestUploadImageHookMode(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a hook-mode session
	_, err := store.UpsertSession("test", "/tmp/test", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(false)
	if err != nil {
		t.Fatal(err)
	}
	var hookSessID string
	for _, s := range sessions {
		if s.Mode == "hook" {
			hookSessID = s.ID
			break
		}
	}
	if hookSessID == "" {
		t.Fatal("no hook session found")
	}

	pngData := createTestPNG()
	multiBody, contentType := createMultipartBody("test.png", pngData)
	uploadReq, _ := http.NewRequest("POST", ts.URL+"/api/sessions/"+hookSessID+"/upload", multiBody)
	uploadReq.Header.Set("Authorization", "Bearer test-api-key")
	uploadReq.Header.Set("Content-Type", contentType)

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode != 400 {
		t.Fatalf("status=%d, want 400 for hook-mode session", uploadResp.StatusCode)
	}
}

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
