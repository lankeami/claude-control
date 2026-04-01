package api

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".ico": true, ".bmp": true,
}

func isImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return imageExtensions[ext]
}

var mediaExtensions = map[string]bool{
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".ico": true, ".bmp": true, ".svg": true,
	// Video
	".mp4": true, ".webm": true, ".mov": true, ".avi": true,
	".mkv": true, ".m4v": true,
	// Audio
	".mp3": true, ".wav": true, ".ogg": true, ".m4a": true,
	".flac": true, ".aac": true, ".wma": true,
}

func isMediaFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return mediaExtensions[ext]
}

const maxRawFileSize = 10 << 20 // 10MB

func (s *Server) handleGetFileRaw(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")

	if filePath == "" || sessionID == "" {
		http.Error(w, "path and session_id are required", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Symlink resolution
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// CWD boundary check
	if sess.CWD != "" {
		resolvedCWD, err := filepath.EvalSymlinks(sess.CWD)
		if err != nil {
			resolvedCWD = sess.CWD
		}
		rel, err := filepath.Rel(resolvedCWD, resolved)
		if err != nil || len(rel) >= 2 && rel[:2] == ".." {
			http.Error(w, "file is outside session working directory", http.StatusForbidden)
			return
		}
	}

	// Check file size
	info, err := os.Stat(resolved)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	if info.Size() > maxRawFileSize {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Detect MIME type by extension
	ext := strings.ToLower(filepath.Ext(resolved))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, resolved)
}

type fileEntry struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

type filePathInput struct {
	FilePath string `json:"file_path"`
}

func (s *Server) handleListSessionFiles(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	var files []fileEntry

	if sess.Mode == "managed" {
		dbFiles, err := s.store.ListSessionFiles(sessionID)
		if err != nil {
			http.Error(w, `{"error":"failed to list files"}`, http.StatusInternalServerError)
			return
		}
		for _, f := range dbFiles {
			files = append(files, fileEntry{Path: f.FilePath, Action: f.Action})
		}
	} else {
		if sess.TranscriptPath != "" {
			files, err = extractFilesFromTranscript(sess.TranscriptPath)
			if err != nil {
				// Non-fatal: return empty list if transcript can't be read
				files = []fileEntry{}
			}
		}
	}

	if files == nil {
		files = []fileEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"files": files,
	})
}

func extractFilesFromTranscript(transcriptPath string) ([]fileEntry, error) {
	f, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	seen := make(map[string]struct{})
	var files []fileEntry

	for scanner.Scan() {
		var entry transcriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.Type != "assistant" {
			continue
		}

		var msg messageContent
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}

		var blocks []contentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}

		for _, block := range blocks {
			if block.Type != "tool_use" {
				continue
			}

			var action string
			switch block.Name {
			case "Edit":
				action = "edit"
			case "Write":
				action = "write"
			case "Read":
				action = "read"
			default:
				continue
			}

			var inp filePathInput
			if err := json.Unmarshal(block.Input, &inp); err != nil || inp.FilePath == "" {
				continue
			}

			key := inp.FilePath + ":" + action
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			files = append(files, fileEntry{Path: inp.FilePath, Action: action})
		}
	}

	return files, scanner.Err()
}

const maxFileSize = 1 << 20 // 1MB

type fileContentResponse struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Exists    bool   `json:"exists"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
}

func (s *Server) handleGetFileContent(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	sessionID := r.URL.Query().Get("session_id")

	if filePath == "" || sessionID == "" {
		http.Error(w, `{"error":"path and session_id are required"}`, http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	// Symlink resolution and path validation
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		// File doesn't exist (or symlink broken)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fileContentResponse{
			Path:      filePath,
			Content:   "",
			Exists:    false,
			Truncated: false,
			Binary:    false,
		})
		return
	}

	// Validate resolved path is within session CWD
	if sess.CWD != "" {
		resolvedCWD, err := filepath.EvalSymlinks(sess.CWD)
		if err != nil {
			resolvedCWD = sess.CWD
		}
		rel, err := filepath.Rel(resolvedCWD, resolved)
		if err != nil || len(rel) >= 2 && rel[:2] == ".." {
			http.Error(w, `{"error":"file is outside session working directory"}`, http.StatusForbidden)
			return
		}
	}

	// Read the file
	f, err := os.Open(resolved)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fileContentResponse{
			Path:      filePath,
			Content:   "",
			Exists:    false,
			Truncated: false,
			Binary:    false,
		})
		return
	}
	defer f.Close()

	buf := make([]byte, maxFileSize+1)
	n, _ := f.Read(buf)
	buf = buf[:n]

	truncated := n > maxFileSize
	if truncated {
		buf = buf[:maxFileSize]
	}

	// Binary detection: check first 512 bytes for null bytes
	checkLen := n
	if checkLen > 512 {
		checkLen = 512
	}
	binary := bytes.IndexByte(buf[:checkLen], 0) >= 0

	// For image files, always return base64 regardless of null-byte detection
	if isImageFile(filePath) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fileContentResponse{
			Path:      filePath,
			Content:   base64.StdEncoding.EncodeToString(buf),
			Exists:    true,
			Truncated: truncated,
			Binary:    true,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fileContentResponse{
		Path:      filePath,
		Content:   string(buf),
		Exists:    true,
		Truncated: truncated,
		Binary:    binary,
	})
}
