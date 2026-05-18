package api

import (
	"encoding/json"
	"fmt"
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

// extensionToMIME maps file extensions to MIME types for fallback detection.
var extensionToMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// imageData holds base64-encoded image bytes and its MIME type.
type imageData struct {
	base64    string
	mediaType string
}

// uploadDir returns the directory where uploaded images are stored for a session.
func uploadDir(sessionCWD, sessionID string) string {
	return filepath.Join(sessionCWD, ".claude-controller-uploads", sessionID)
}

func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess, err := s.store.GetSessionByID(id)
	if err != nil || sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		http.Error(w, "upload only supported for managed sessions", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			http.Error(w, "file too large (max 20MB)", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "failed to parse form: "+err.Error(), http.StatusBadRequest)
		}
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Determine MIME type: check Content-Type header first, then fall back to extension.
	contentType := header.Header.Get("Content-Type")
	if !allowedImageTypes[contentType] {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if mime, ok := extensionToMIME[ext]; ok {
			contentType = mime
		}
	}
	if !allowedImageTypes[contentType] {
		http.Error(w, "unsupported image type; allowed: png, jpg, gif, webp", http.StatusBadRequest)
		return
	}

	dir := uploadDir(sess.CWD, sess.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, "failed to create upload directory", http.StatusInternalServerError)
		return
	}

	// Remove any existing files in the dir (one image at a time).
	existing, _ := os.ReadDir(dir)
	for _, entry := range existing {
		if !entry.IsDir() {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}

	// Save with UUID filename + original extension.
	imageID := uuid.New().String()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	savedName := imageID + ext
	destPath := filepath.Join(dir, savedName)

	var data []byte
	if header.Size > 0 {
		data = make([]byte, header.Size)
		if _, err := file.Read(data); err != nil {
			http.Error(w, "failed to read file", http.StatusInternalServerError)
			return
		}
	} else {
		// Read without known size.
		buf := make([]byte, 0, 512)
		tmp := make([]byte, 512)
		for {
			n, readErr := file.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if readErr != nil {
				break
			}
		}
		data = buf
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"image_id": imageID,
		"filename": header.Filename,
	})
}

// cleanupSessionUploads removes the upload dir for a session and the parent if empty.
func cleanupSessionUploads(sessionCWD, sessionID string) {
	dir := uploadDir(sessionCWD, sessionID)
	os.RemoveAll(dir)

	// Remove parent (.claude-controller-uploads) if empty.
	parent := filepath.Dir(dir)
	entries, err := os.ReadDir(parent)
	if err == nil && len(entries) == 0 {
		os.Remove(parent)
	}
}

// findUploadedImage finds a file by UUID prefix in the session upload dir.
// Returns (path, mediaType) or ("", "") if not found.
func findUploadedImage(sessionCWD, sessionID, imageID string) (string, string) {
	dir := uploadDir(sessionCWD, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, imageID) {
			ext := strings.ToLower(filepath.Ext(name))
			mime := extensionToMIME[ext]
			if mime == "" {
				mime = "image/png"
			}
			return filepath.Join(dir, name), mime
		}
	}
	return "", ""
}

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

// formatImageUploadMessage returns the display string for chat history.
func formatImageUploadMessage(message, filename string) string {
	if message != "" {
		return fmt.Sprintf("[📷 %s]\n%s", filename, message)
	}
	return fmt.Sprintf("[📷 %s]", filename)
}
