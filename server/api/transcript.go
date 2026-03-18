package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
)

type transcriptMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
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
	Type string `json:"type"`
	Text string `json:"text"`
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

		text := extractText(msg.Content)
		if text == "" {
			continue
		}

		messages = append(messages, transcriptMessage{
			Role:      entry.Type,
			Content:   text,
			Timestamp: entry.Timestamp,
		})
	}

	if len(messages) > 500 {
		messages = messages[len(messages)-500:]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func extractText(raw json.RawMessage) string {
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var texts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				texts = append(texts, b.Text)
			}
		}
		if len(texts) > 0 {
			result := texts[0]
			for i := 1; i < len(texts); i++ {
				result += "\n" + texts[i]
			}
			return result
		}
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return ""
}
