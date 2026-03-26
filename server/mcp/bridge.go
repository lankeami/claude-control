package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Result  interface{} `json:"result"`
}

func parseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func Run(sessionID string, serverPort int) error {
	baseURL := fmt.Sprintf("http://localhost:%d", serverPort)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		req, err := parseRequest(line)
		if err != nil {
			continue
		}

		switch req.Method {
		case "initialize":
			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":     map[string]interface{}{"name": "claude-controller-bridge", "version": "1.0.0"},
			})

		case "notifications/initialized":
			continue

		case "tools/list":
			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "permission_prompt",
						"description": "Handle permission prompts from Claude Code",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			})

		case "tools/call":
			var params ToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeToolError(os.Stdout, req.ID, "invalid params")
				continue
			}
			if params.Name != "permission_prompt" {
				writeToolError(os.Stdout, req.ID, "unknown tool: "+params.Name)
				continue
			}

			decision, err := forwardPermissionRequest(baseURL, sessionID, params.Arguments)
			if err != nil {
				writeToolError(os.Stdout, req.ID, "server error: "+err.Error())
				continue
			}

			writeResponse(os.Stdout, req.ID, map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": decision},
				},
			})
		}
	}

	return scanner.Err()
}

func forwardPermissionRequest(baseURL, sessionID string, arguments json.RawMessage) (string, error) {
	url := fmt.Sprintf("%s/api/sessions/%s/permission-request", baseURL, sessionID)

	client := &http.Client{Timeout: 6 * time.Minute}
	resp, err := client.Post(url, "application/json", bytes.NewReader(arguments))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func writeResponse(w io.Writer, id int, result interface{}) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "%s\n", data)
}

func writeToolError(w io.Writer, id int, message string) {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": message},
			},
			"isError": true,
		},
	}
	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "%s\n", data)
}
