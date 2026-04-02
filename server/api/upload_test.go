package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
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
