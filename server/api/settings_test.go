package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// shortcutsFilePath returns the shortcuts.json path for a given envPath.
func shortcutsFilePath(envPath string) string {
	return strings.TrimSuffix(envPath, ".env") + "shortcuts.json"
}

func newTestServerWithManager(t *testing.T) (*httptest.Server, *db.Store, *managed.Manager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := db.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	mgr := managed.NewManager(managed.Config{ClaudeBin: "claude"})
	envPath := filepath.Join(tmpDir, ".env")
	router := NewRouter(store, "test-key", mgr, envPath, nil, "test-server-id")
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, store, mgr, envPath
}

func TestSettingsExists_NoFile(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	req := authReq("GET", ts.URL+"/api/settings/exists", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)
	if body["exists"] {
		t.Error("expected exists=false")
	}
}

func TestSettingsExists_WithFile(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("PORT=8080\n"), 0600)

	req := authReq("GET", ts.URL+"/api/settings/exists", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]bool
	json.NewDecoder(resp.Body).Decode(&body)
	if !body["exists"] {
		t.Error("expected exists=true")
	}
}

func TestGetSettings_NoFile(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["port"] != "" || body["claude_bin"] != "" {
		t.Errorf("expected empty fields, got %v", body)
	}
}

func TestPutSettings_CreatesFile(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)

	settings := map[string]string{
		"port":            "9090",
		"ngrok_authtoken": "tok_abc123",
		"claude_bin":      "/usr/bin/claude",
		"claude_args":     "--flag1 --flag2",
		"claude_env":      "K1=V1,K2=V2",
	}
	req := authReq("PUT", ts.URL+"/api/settings", settings)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify file was created
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "PORT=9090") {
		t.Errorf("expected PORT=9090 in file, got: %s", content)
	}
	if !strings.Contains(content, "CLAUDE_BIN=/usr/bin/claude") {
		t.Errorf("expected CLAUDE_BIN in file, got: %s", content)
	}
}

func TestPutSettings_MaskedAuthtoken(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("NGROK_AUTHTOKEN=secret_token_12345\n"), 0600)

	// PUT with masked token — should preserve original
	settings := map[string]string{
		"ngrok_authtoken": "****2345",
		"claude_bin":      "claude",
	}
	req := authReq("PUT", ts.URL+"/api/settings", settings)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "NGROK_AUTHTOKEN=secret_token_12345") {
		t.Errorf("expected original token preserved, got: %s", string(data))
	}
}

func TestPutSettings_RestartRequired(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("PORT=8080\n"), 0600)

	settings := map[string]string{"port": "9090"}
	req := authReq("PUT", ts.URL+"/api/settings", settings)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]bool
	json.NewDecoder(resp.Body).Decode(&result)
	if !result["restart_required"] {
		t.Error("expected restart_required=true when PORT changed")
	}
}

func TestPutSettings_InvalidPort(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)
	settings := map[string]string{"port": "abc"}
	req := authReq("PUT", ts.URL+"/api/settings", settings)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetSettings_MasksAuthtoken(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)
	os.WriteFile(envPath, []byte("NGROK_AUTHTOKEN=secret_token_12345\nPORT=8080\n"), 0600)

	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["ngrok_authtoken"] != "****2345" {
		t.Errorf("expected masked token ****2345, got %s", body["ngrok_authtoken"])
	}
	if body["port"] != "8080" {
		t.Errorf("expected port 8080, got %s", body["port"])
	}
}

func TestPutThenGetSettings_RoundTrip(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)

	// PUT settings
	settings := map[string]string{
		"port":        "3000",
		"claude_bin":  "/usr/local/bin/claude",
		"claude_args": "--verbose --safe",
		"claude_env":  "FOO=bar,BAZ=qux",
	}
	req := authReq("PUT", ts.URL+"/api/settings", settings)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// GET settings back
	req = authReq("GET", ts.URL+"/api/settings", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["port"] != "3000" {
		t.Errorf("expected port 3000, got %s", body["port"])
	}
	if body["claude_bin"] != "/usr/local/bin/claude" {
		t.Errorf("expected claude_bin, got %s", body["claude_bin"])
	}
	if body["claude_args"] != "--verbose --safe" {
		t.Errorf("expected claude_args, got %s", body["claude_args"])
	}
	if body["claude_env"] != "FOO=bar,BAZ=qux" {
		t.Errorf("expected claude_env, got %s", body["claude_env"])
	}
}

func TestPutSettings_WithShortcuts(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)

	body := `{"port":"8080","shortcuts":[{"key":"/deploy","value":"Deploy to production"},{"key":"/test","value":"Run all tests"}]}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	scPath := shortcutsFilePath(envPath)
	data, err := os.ReadFile(scPath)
	if err != nil {
		t.Fatalf("shortcuts.json not created: %v", err)
	}
	var shortcuts []Shortcut
	if err := json.Unmarshal(data, &shortcuts); err != nil {
		t.Fatalf("invalid shortcuts.json: %v", err)
	}
	if len(shortcuts) != 2 {
		t.Fatalf("expected 2 shortcuts, got %d", len(shortcuts))
	}
	if shortcuts[0].Key != "/deploy" || shortcuts[0].Value != "Deploy to production" {
		t.Errorf("unexpected first shortcut: %+v", shortcuts[0])
	}
	if shortcuts[1].Key != "/test" || shortcuts[1].Value != "Run all tests" {
		t.Errorf("unexpected second shortcut: %+v", shortcuts[1])
	}
}

func TestGetSettings_IncludesShortcuts(t *testing.T) {
	ts, _, _, envPath := newTestServerWithManager(t)

	scPath := shortcutsFilePath(envPath)
	scData := `[{"key":"/hello","value":"Hello world"}]`
	if err := os.WriteFile(scPath, []byte(scData), 0600); err != nil {
		t.Fatalf("failed to write shortcuts.json: %v", err)
	}

	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result settingsPayload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Shortcuts) != 1 {
		t.Fatalf("expected 1 shortcut, got %d", len(result.Shortcuts))
	}
	if result.Shortcuts[0].Key != "/hello" || result.Shortcuts[0].Value != "Hello world" {
		t.Errorf("unexpected shortcut: %+v", result.Shortcuts[0])
	}
}

func TestGetSettings_NoShortcutsFile_ReturnsEmptyArray(t *testing.T) {
	ts, _, _, _ := newTestServerWithManager(t)

	req := authReq("GET", ts.URL+"/api/settings", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result settingsPayload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if result.Shortcuts == nil {
		t.Error("expected empty array for shortcuts, got nil")
	}
	if len(result.Shortcuts) != 0 {
		t.Errorf("expected 0 shortcuts, got %d", len(result.Shortcuts))
	}
}
