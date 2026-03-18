package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type transcriptMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
	MsgType   string `json:"msg_type"` // "text", "edit", "write", "bash"
	FilePath  string `json:"file_path,omitempty"`
	OldString string `json:"old_string,omitempty"`
	NewString string `json:"new_string,omitempty"`
	Command   string `json:"command,omitempty"`
}

type transcriptEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message"`
}

type messageContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type editInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type writeInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type bashInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

func (s *Server) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	path, err := s.store.GetTranscriptPath(id)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	messages := []transcriptMessage{}

	if path == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messages)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messages)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry transcriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		var msg messageContent
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}

		// Parse all content blocks
		var blocks []contentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}

		for _, block := range blocks {
			switch {
			case block.Type == "text" && block.Text != "":
				text := strings.TrimSpace(block.Text)
				// Skip noise messages
				if text == "[Request interrupted by user]" || text == "" {
					continue
				}
				messages = append(messages, transcriptMessage{
					Role:      entry.Type,
					Content:   text,
					Timestamp: entry.Timestamp,
					MsgType:   "text",
				})

			case block.Type == "tool_use" && block.Name == "Edit":
				var inp editInput
				if json.Unmarshal(block.Input, &inp) == nil && inp.FilePath != "" {
					messages = append(messages, transcriptMessage{
						Role:      "assistant",
						Content:   fmt.Sprintf("Edited %s", filepath.Base(inp.FilePath)),
						Timestamp: entry.Timestamp,
						MsgType:   "edit",
						FilePath:  inp.FilePath,
						OldString: truncate(inp.OldString, 2000),
						NewString: truncate(inp.NewString, 2000),
					})
				}

			case block.Type == "tool_use" && block.Name == "Write":
				var inp writeInput
				if json.Unmarshal(block.Input, &inp) == nil && inp.FilePath != "" {
					messages = append(messages, transcriptMessage{
						Role:      "assistant",
						Content:   fmt.Sprintf("Wrote %s", filepath.Base(inp.FilePath)),
						Timestamp: entry.Timestamp,
						MsgType:   "write",
						FilePath:  inp.FilePath,
					})
				}

			case block.Type == "tool_use" && block.Name == "Bash":
				var inp bashInput
				if json.Unmarshal(block.Input, &inp) == nil && inp.Command != "" {
					desc := inp.Description
					if desc == "" {
						desc = truncate(inp.Command, 100)
					}
					messages = append(messages, transcriptMessage{
						Role:      "assistant",
						Content:   desc,
						Timestamp: entry.Timestamp,
						MsgType:   "bash",
						Command:   truncate(inp.Command, 500),
					})
				}
			}
		}
	}

	if len(messages) > 500 {
		messages = messages[len(messages)-500:]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
