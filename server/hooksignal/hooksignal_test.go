package hooksignal

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunPostsHookEvent(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	keyFile := filepath.Join(t.TempDir(), "api.key")
	os.WriteFile(keyFile, []byte("sk-test-key"), 0600)

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	stdin := strings.NewReader(`{"hook_event_name":"Stop","session_id":"cli-uuid","transcript_path":"/tmp/t.jsonl","message":"hello"}`)
	if err := Run("stop", "managed-1", port, keyFile, stdin); err != nil {
		t.Fatal(err)
	}

	if gotPath != "/api/sessions/managed-1/hook-event" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Errorf("auth = %s", gotAuth)
	}
	if gotBody["event"] != "stop" || gotBody["claude_session_id"] != "cli-uuid" ||
		gotBody["transcript_path"] != "/tmp/t.jsonl" || gotBody["message"] != "hello" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestRunToleratesMalformedStdin(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	keyFile := filepath.Join(t.TempDir(), "api.key")
	os.WriteFile(keyFile, []byte("k"), 0600)

	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	if err := Run("notification", "m1", port, keyFile, strings.NewReader("not json")); err != nil {
		t.Fatal(err)
	}
	if gotBody["event"] != "notification" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestRunMissingKeyFileReturnsError(t *testing.T) {
	err := Run("stop", "m1", 1, filepath.Join(t.TempDir(), "nope.key"), strings.NewReader("{}"))
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
}
