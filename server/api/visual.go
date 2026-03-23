package api

import (
	"fmt"
	"html"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jaychinthrajah/claude-controller/server/web"
)

var brainstormRootOverride string

func brainstormRoot() string {
	if brainstormRootOverride != "" {
		return brainstormRootOverride
	}
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".superpowers", "brainstorm")
}

var frameTemplate = web.FrameTemplate()

func isFullDocument(htmlContent string) bool {
	trimmed := strings.TrimLeft(htmlContent, " \t\n\r")
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
}

func wrapFragment(content string) string {
	return strings.Replace(frameTemplate, "<!-- CONTENT -->", content, 1)
}

func (s *Server) handleVisual(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")

	root := brainstormRoot()
	rest := strings.TrimPrefix(r.URL.Path, "/visual/")

	if rest == "" {
		s.serveVisualIndex(w, root)
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	sessionID := filepath.Base(parts[0])
	filename := ""
	if len(parts) > 1 {
		filename = parts[1]
	}

	sessionDir := filepath.Join(root, sessionID)

	absSession, err := filepath.Abs(sessionDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absRoot, _ := filepath.Abs(root)
	if !strings.HasPrefix(filepath.Clean(absSession), filepath.Clean(absRoot)+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(absSession)
	if err != nil || !info.IsDir() {
		http.NotFound(w, r)
		return
	}

	if filename == "" || filename == "/" {
		s.serveSessionDefault(w, r, absSession)
		return
	}

	safeFilename := filepath.Base(filename)
	filePath := filepath.Join(absSession, safeFilename)

	absFile, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(filepath.Clean(absFile), filepath.Clean(absSession)+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}

	s.serveVisualFile(w, r, absFile)
}

func (s *Server) serveVisualIndex(w http.ResponseWriter, root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body><h1>Visual Companion</h1><p>No brainstorm sessions found. Start a brainstorming session to see content here.</p></body></html>")
		return
	}

	type sessionEntry struct {
		Name    string
		ModTime int64
	}
	var sessions []sessionEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionEntry{Name: e.Name(), ModTime: info.ModTime().Unix()})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime > sessions[j].ModTime
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, "<html><head><meta charset=\"utf-8\"><title>Visual Companion</title>"+
		"<style>body{font-family:system-ui,sans-serif;padding:2rem;max-width:800px;margin:0 auto}"+
		"a{color:#0071e3;text-decoration:none}a:hover{text-decoration:underline}"+
		"li{margin:0.5rem 0}</style></head>"+
		"<body><h1>Visual Companion</h1>")
	if len(sessions) == 0 {
		fmt.Fprint(w, "<p>No brainstorm sessions found.</p>")
	} else {
		fmt.Fprint(w, "<ul>")
		for _, sess := range sessions {
			escaped := html.EscapeString(sess.Name)
			fmt.Fprintf(w, "<li><a href=\"/visual/%s/\">%s</a></li>", escaped, escaped)
		}
		fmt.Fprint(w, "</ul>")
	}
	fmt.Fprint(w, "</body></html>")
}

func (s *Server) serveSessionDefault(w http.ResponseWriter, r *http.Request, sessionDir string) {
	indexPath := filepath.Join(sessionDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		s.serveVisualFile(w, r, indexPath)
		return
	}

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var newest string
	var newestTime int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > newestTime {
			newestTime = info.ModTime().Unix()
			newest = filepath.Join(sessionDir, e.Name())
		}
	}

	if newest == "" {
		http.NotFound(w, r)
		return
	}

	s.serveVisualFile(w, r, newest)
}

func (s *Server) serveVisualFile(w http.ResponseWriter, r *http.Request, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if ext == ".html" || ext == ".htm" {
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline'")
		content := string(data)
		if !isFullDocument(content) {
			content = wrapFragment(content)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, content)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
