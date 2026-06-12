package managed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type settingsFile struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
}

func TestWriteSessionSettings(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteSessionSettings(dir, "/usr/local/bin/claude controller", "sess-1", 8080, `["Bash","Read"]`, "/home/u/.claude controller/api.key")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "settings.json") {
		t.Fatalf("unexpected path %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sf settingsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}

	for _, event := range []string{"SessionStart", "Stop", "Notification"} {
		matchers, ok := sf.Hooks[event]
		if !ok || len(matchers) != 1 || len(matchers[0].Hooks) != 1 {
			t.Fatalf("missing hook for %s: %s", event, data)
		}
		cmd := matchers[0].Hooks[0].Command
		if !strings.Contains(cmd, `"/usr/local/bin/claude controller"`) {
			t.Errorf("%s hook command does not quote binary path: %s", event, cmd)
		}
		if !strings.Contains(cmd, "hook-signal") || !strings.Contains(cmd, "--session-id sess-1") || !strings.Contains(cmd, "--port 8080") {
			t.Errorf("%s hook command malformed: %s", event, cmd)
		}
		// hook-signal must read the same api.key the server generated, even
		// when the server runs with a non-default --db directory.
		if !strings.Contains(cmd, `--key-file "/home/u/.claude controller/api.key"`) {
			t.Errorf("%s hook command missing quoted --key-file: %s", event, cmd)
		}
		if matchers[0].Hooks[0].Type != "command" {
			t.Errorf("%s hook type = %s", event, matchers[0].Hooks[0].Type)
		}
	}
	if !strings.Contains(sf.Hooks["Stop"][0].Hooks[0].Command, "--event stop") {
		t.Errorf("stop hook missing event flag: %s", sf.Hooks["Stop"][0].Hooks[0].Command)
	}
	if !strings.Contains(sf.Hooks["SessionStart"][0].Hooks[0].Command, "--event session_start") {
		t.Errorf("session_start hook missing event flag")
	}

	if len(sf.Permissions.Allow) != 2 || sf.Permissions.Allow[0] != "Bash" || sf.Permissions.Allow[1] != "Read" {
		t.Errorf("permissions.allow = %v", sf.Permissions.Allow)
	}
}

func TestWriteSessionSettingsNoTools(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteSessionSettings(dir, "/bin/cc", "sess-2", 9090, "", "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if _, ok := raw["permissions"]; ok {
		t.Errorf("permissions should be omitted when no tools: %s", data)
	}
	if strings.Contains(string(data), "--key-file") {
		t.Errorf("--key-file should be omitted when no key path is configured: %s", data)
	}
}

func TestSessionDirIsPerSession(t *testing.T) {
	a := SessionDir("aaa")
	b := SessionDir("bbb")
	if a == b || !strings.Contains(a, "aaa") {
		t.Fatalf("SessionDir not per-session: %s vs %s", a, b)
	}
}
