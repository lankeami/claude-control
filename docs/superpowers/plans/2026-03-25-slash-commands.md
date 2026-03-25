# Slash Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add slash command autocomplete to the managed session web UI, supporting built-in, CLI, and custom user commands from `.claude/commands/` directories.

**Architecture:** New Go handler scans filesystem for custom commands and merges with built-in list. Web UI adds an autocomplete dropdown to the existing textarea input. Custom commands are fetched on-demand and sent as prompts via the existing message flow.

**Tech Stack:** Go (server API), Alpine.js (web UI), CSS (styling)

**Spec:** `docs/superpowers/specs/2026-03-25-slash-commands-design.md`

---

### Task 1: Server — Command Discovery Handler

**Files:**
- Create: `server/api/commands.go`
- Create: `server/api/commands_test.go`
- Modify: `server/api/router.go:51-61` (add route)

- [ ] **Step 1: Write test for listing built-in commands**

In `server/api/commands_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListCommands_BuiltinOnly(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create a managed session
	sess, _ := store.CreateManagedSession("/tmp/test-project", `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var commands []SlashCommand
	json.NewDecoder(resp.Body).Decode(&commands)

	if len(commands) == 0 {
		t.Fatal("expected at least one built-in command")
	}

	// Check that /help is present
	found := false
	for _, c := range commands {
		if c.Name == "/help" {
			found = true
			if c.Source != "builtin" {
				t.Errorf("source=%s, want builtin", c.Source)
			}
		}
	}
	if !found {
		t.Error("/help not found in commands list")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestListCommands_BuiltinOnly`
Expected: FAIL (SlashCommand type not defined, handler not found)

- [ ] **Step 3: Implement command types and built-in list**

In `server/api/commands.go`:

```go
package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`      // "builtin" or "custom"
	HasArg      bool   `json:"hasArg"`
	ArgHint     string `json:"argHint,omitempty"`
}

var builtinCommands = []SlashCommand{
	{Name: "/clear", Description: "Clear chat display", Source: "builtin"},
	{Name: "/compact", Description: "Compact conversation context", Source: "builtin"},
	{Name: "/cost", Description: "Show session cost info", Source: "builtin"},
	{Name: "/help", Description: "Show available commands", Source: "builtin"},
	{Name: "/resume", Description: "Continue a previous CLI session", Source: "builtin"},
}

func (s *Server) handleListCommands(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	commands := make([]SlashCommand, len(builtinCommands))
	copy(commands, builtinCommands)

	// Discover custom commands from ~/.claude/commands/ and {cwd}/.claude/commands/
	homeDir, _ := os.UserHomeDir()
	seen := map[string]bool{}
	for _, c := range commands {
		seen[c.Name] = true
	}

	// Project commands first (they take priority over user commands)
	if sess.CWD != "" {
		projectCmds := discoverCommands(filepath.Join(sess.CWD, ".claude", "commands"))
		for _, c := range projectCmds {
			if !seen[c.Name] {
				commands = append(commands, c)
				seen[c.Name] = true
			}
		}
	}

	// User-level commands
	if homeDir != "" {
		userCmds := discoverCommands(filepath.Join(homeDir, ".claude", "commands"))
		for _, c := range userCmds {
			if !seen[c.Name] {
				commands = append(commands, c)
				seen[c.Name] = true
			}
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commands)
}

// discoverCommands walks a directory for .md files and parses their YAML frontmatter.
func discoverCommands(dir string) []SlashCommand {
	var commands []SlashCommand
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		cmd, err := parseCommandFile(path, dir)
		if err == nil {
			commands = append(commands, cmd)
		}
		return nil
	})
	return commands
}

// parseCommandFile reads a .md command file and extracts frontmatter fields.
func parseCommandFile(path, baseDir string) (SlashCommand, error) {
	f, err := os.Open(path)
	if err != nil {
		return SlashCommand{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	name := ""
	description := ""
	argHint := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			if inFrontmatter {
				break // end of frontmatter
			}
			inFrontmatter = true
			continue
		}

		if !inFrontmatter {
			continue
		}

		if strings.HasPrefix(trimmed, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
		} else if strings.HasPrefix(trimmed, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		} else if strings.HasPrefix(trimmed, "argument-hint:") {
			argHint = strings.TrimSpace(strings.TrimPrefix(trimmed, "argument-hint:"))
		}
	}

	if name == "" {
		// Derive name from path relative to base commands dir
		rel, _ := filepath.Rel(baseDir, path)
		name = strings.TrimSuffix(rel, ".md")
		name = strings.ReplaceAll(name, string(filepath.Separator), ":")
	}

	return SlashCommand{
		Name:        "/" + name,
		Description: description,
		Source:      "custom",
		HasArg:      argHint != "",
		ArgHint:     argHint,
	}, nil
}
```

- [ ] **Step 4: Register the route**

In `server/api/router.go`, add after line 60 (`handleShellExecute`):

```go
	apiMux.HandleFunc("GET /api/sessions/{id}/commands", s.handleListCommands)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd server && go test ./api/ -v -run TestListCommands_BuiltinOnly`
Expected: PASS

- [ ] **Step 6: Write test for custom command discovery**

Add to `server/api/commands_test.go`:

```go
func TestListCommands_WithCustomCommands(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Create temp dir with custom commands
	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, ".claude", "commands")
	os.MkdirAll(cmdDir, 0755)
	os.WriteFile(filepath.Join(cmdDir, "test-cmd.md"), []byte("---\nname: test-cmd\ndescription: A test command\nargument-hint: [arg]\n---\nHello $ARGUMENTS"), 0644)

	// Create nested command
	subDir := filepath.Join(cmdDir, "sub")
	os.MkdirAll(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "nested.md"), []byte("---\nname: sub:nested\ndescription: Nested command\n---\nNested content"), 0644)

	sess, _ := store.CreateManagedSession(tmpDir, `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var commands []SlashCommand
	json.NewDecoder(resp.Body).Decode(&commands)

	// Should have builtins + 2 custom
	customCount := 0
	for _, c := range commands {
		if c.Source == "custom" {
			customCount++
			if c.Name == "/test-cmd" {
				if !c.HasArg || c.ArgHint != "[arg]" {
					t.Errorf("test-cmd: hasArg=%v, argHint=%s", c.HasArg, c.ArgHint)
				}
			}
		}
	}
	if customCount != 2 {
		t.Errorf("custom commands=%d, want 2", customCount)
	}
}
```

Add these imports at the top of the test file:
```go
import (
	"os"
	"path/filepath"
)
```

- [ ] **Step 7: Run test to verify it passes**

Run: `cd server && go test ./api/ -v -run TestListCommands`
Expected: Both tests PASS

- [ ] **Step 8: Commit**

```bash
git add server/api/commands.go server/api/commands_test.go server/api/router.go
git commit -m "feat: add slash command discovery API endpoint"
```

---

### Task 2: Server — Command Content Endpoint

**Files:**
- Modify: `server/api/commands.go`
- Modify: `server/api/commands_test.go`
- Modify: `server/api/router.go`

- [ ] **Step 1: Write test for command content retrieval**

Add to `server/api/commands_test.go`:

```go
func TestGetCommandContent(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	tmpDir := t.TempDir()
	cmdDir := filepath.Join(tmpDir, ".claude", "commands")
	os.MkdirAll(cmdDir, 0755)
	os.WriteFile(filepath.Join(cmdDir, "greet.md"), []byte("---\nname: greet\ndescription: Greet someone\nargument-hint: [name]\n---\nHello, please greet $ARGUMENTS warmly."), 0644)

	sess, _ := store.CreateManagedSession(tmpDir, `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands/greet/content", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result struct {
		Content string `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if !strings.Contains(result.Content, "Hello, please greet") {
		t.Errorf("content=%q, expected to contain prompt body", result.Content)
	}
	// Should NOT contain frontmatter
	if strings.Contains(result.Content, "---") {
		t.Errorf("content should not contain frontmatter delimiters")
	}
}

func TestGetCommandContent_NotFound(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	sess, _ := store.CreateManagedSession("/tmp/nonexistent-path", `["Bash"]`, 50, 5.0)

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions/"+sess.ID+"/commands/nonexistent/content", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("status=%d, want 404", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./api/ -v -run TestGetCommandContent`
Expected: FAIL (handler not registered)

- [ ] **Step 3: Implement command content handler**

Add to `server/api/commands.go`:

```go
func (s *Server) handleGetCommandContent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	cmdName := r.PathValue("name")

	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Search for command file in project dir first, then user home
	homeDir, _ := os.UserHomeDir()
	searchDirs := []string{}
	if sess.CWD != "" {
		searchDirs = append(searchDirs, filepath.Join(sess.CWD, ".claude", "commands"))
	}
	if homeDir != "" {
		searchDirs = append(searchDirs, filepath.Join(homeDir, ".claude", "commands"))
	}

	for _, dir := range searchDirs {
		content, err := findCommandContent(dir, cmdName)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"content": content})
			return
		}
	}

	http.Error(w, "command not found", http.StatusNotFound)
}

// findCommandContent searches a commands directory for a command by name and returns its body.
func findCommandContent(dir, name string) (string, error) {
	// Name could be "greet" or "sub:nested" — convert colons to path separators
	relPath := strings.ReplaceAll(name, ":", string(filepath.Separator)) + ".md"
	fullPath := filepath.Join(dir, relPath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}

	return stripFrontmatter(string(data)), nil
}

// stripFrontmatter removes YAML frontmatter (--- delimited) from content.
func stripFrontmatter(content string) string {
	lines := strings.SplitN(content, "\n", -1)
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}

	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			// Return everything after the closing ---
			rest := strings.Join(lines[i+1:], "\n")
			return strings.TrimSpace(rest)
		}
	}

	return content // No closing ---, return as-is
}
```

- [ ] **Step 4: Register the route**

In `server/api/router.go`, add after the commands list route:

```go
	apiMux.HandleFunc("GET /api/sessions/{id}/commands/{name}/content", s.handleGetCommandContent)
```

Note: The `{name}` wildcard may contain colons (e.g., `gsd:help`). Go 1.22+ mux handles this correctly since colons aren't path separators. However, if the command name contains `/` (subdirectory-based like `sub/nested`), we use `:` as the separator in the URL (e.g., `sub:nested`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd server && go test ./api/ -v -run TestGetCommandContent`
Expected: Both tests PASS

- [ ] **Step 6: Commit**

```bash
git add server/api/commands.go server/api/commands_test.go server/api/router.go
git commit -m "feat: add command content retrieval endpoint"
```

---

### Task 3: Web UI — Autocomplete Dropdown Component

**Files:**
- Modify: `server/web/static/app.js` (add state + methods)
- Modify: `server/web/static/index.html` (add dropdown markup)
- Modify: `server/web/static/style.css` (add dropdown styles)

- [ ] **Step 1: Add slash command state to Alpine app**

In `server/web/static/app.js`, add after the `shellMode` state (around line 118):

```javascript
    // Slash commands state
    slashCommands: [],
    slashCommandsLoaded: false,
    slashDropdownOpen: false,
    slashFiltered: [],
    slashSelectedIndex: 0,
```

- [ ] **Step 2: Add command fetching method**

In `server/web/static/app.js`, add a new method (near the other fetch methods):

```javascript
    async fetchSlashCommands(sessionId) {
      if (!sessionId) return;
      try {
        const resp = await fetch(`/api/sessions/${sessionId}/commands`, {
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (resp.ok) {
          this.slashCommands = await resp.json();
          this.slashCommandsLoaded = true;
        }
      } catch (e) {
        console.error('Failed to fetch slash commands:', e);
      }
    },
```

- [ ] **Step 3: Call fetchSlashCommands when selecting a managed session**

Find the `selectSession` method in `app.js` and add the fetch call after the session is selected and identified as managed. Look for where `loadChatHistory` is called for managed sessions and add:

```javascript
        if (!this.slashCommandsLoaded || this.selectedSessionId !== id) {
          this.fetchSlashCommands(id);
        }
```

- [ ] **Step 4: Add input filtering and dropdown control methods**

In `server/web/static/app.js`, add these methods:

```javascript
    handleSlashInput(event) {
      const text = this.inputText;
      if (text.startsWith('/') && !this.shellMode && this.currentSession?.mode === 'managed') {
        const query = text.slice(1).toLowerCase();
        this.slashFiltered = this.slashCommands.filter(c =>
          c.name.slice(1).toLowerCase().startsWith(query)
        );
        this.slashDropdownOpen = this.slashFiltered.length > 0;
        this.slashSelectedIndex = 0;
      } else {
        this.slashDropdownOpen = false;
      }
    },

    handleSlashKeydown(event) {
      if (!this.slashDropdownOpen) return;

      if (event.key === 'ArrowDown') {
        event.preventDefault();
        this.slashSelectedIndex = Math.min(this.slashSelectedIndex + 1, this.slashFiltered.length - 1);
      } else if (event.key === 'ArrowUp') {
        event.preventDefault();
        this.slashSelectedIndex = Math.max(this.slashSelectedIndex - 1, 0);
      } else if (event.key === 'Enter' && !event.metaKey && !event.ctrlKey) {
        event.preventDefault();
        this.selectSlashCommand(this.slashFiltered[this.slashSelectedIndex]);
      } else if (event.key === 'Escape') {
        this.slashDropdownOpen = false;
      } else if (event.key === 'Tab') {
        event.preventDefault();
        this.selectSlashCommand(this.slashFiltered[this.slashSelectedIndex]);
      }
    },

    selectSlashCommand(cmd) {
      if (!cmd) return;
      this.slashDropdownOpen = false;
      if (cmd.hasArg) {
        this.inputText = cmd.name + ' ';
      } else {
        this.inputText = cmd.name;
      }
      this.$nextTick(() => {
        const ta = this.$refs.chatInput;
        if (ta) ta.focus();
      });
    },
```

- [ ] **Step 5: Wire up input events on the textarea**

In `server/web/static/index.html`, modify the textarea (around line 258) to add the slash command event handlers and a ref:

Replace the existing textarea with:
```html
              <textarea x-model="inputText" rows="1" x-ref="chatInput"
                     :placeholder="shellMode ? 'Run a command...' : (currentSession?.mode === 'managed' ? 'Send a message... (⌘↵ to send, / for commands)' : 'Send instruction (delivered on next stop)...')"
                     :class="{ 'shell-input': shellMode }"
                     @keydown="handleSlashKeydown($event); if(!slashDropdownOpen && ($event.metaKey || $event.ctrlKey) && $event.key === 'Enter') { $event.preventDefault(); handleInput(); }"
                     @input="$el.style.height = 'auto'; $el.style.height = Math.min($el.scrollHeight, 150) + 'px'; handleSlashInput($event)"
                     :disabled="inputSending"></textarea>
```

- [ ] **Step 6: Add dropdown HTML markup**

In `server/web/static/index.html`, add the dropdown just before the textarea (inside the `.instruction-bar` div, around line 257):

```html
              <!-- Slash command autocomplete dropdown -->
              <div class="slash-dropdown" x-show="slashDropdownOpen" x-cloak
                   @mousedown.prevent>
                <template x-for="(cmd, idx) in slashFiltered" :key="cmd.name">
                  <div class="slash-item" :class="{ active: idx === slashSelectedIndex }"
                       @click="selectSlashCommand(cmd)"
                       @mouseenter="slashSelectedIndex = idx">
                    <span class="slash-name" x-text="cmd.name"></span>
                    <span class="slash-arg" x-show="cmd.argHint" x-text="cmd.argHint"></span>
                    <span class="slash-desc" x-text="cmd.description"></span>
                    <span class="slash-badge" x-show="cmd.source === 'custom'">custom</span>
                  </div>
                </template>
              </div>
```

- [ ] **Step 7: Add dropdown CSS styles**

In `server/web/static/style.css`, add at the end:

```css
/* Slash command autocomplete */
.instruction-bar { position: relative; }

.slash-dropdown {
  position: absolute;
  bottom: 100%;
  left: 0;
  right: 0;
  max-height: 240px;
  overflow-y: auto;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 4px;
  box-shadow: 0 -4px 16px rgba(0,0,0,0.15);
  z-index: 100;
}

.slash-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  cursor: pointer;
  font-size: 0.875rem;
}

.slash-item.active {
  background: var(--accent);
  color: #fff;
}

.slash-item.active .slash-desc,
.slash-item.active .slash-badge,
.slash-item.active .slash-arg {
  color: rgba(255,255,255,0.8);
}

.slash-name {
  font-weight: 600;
  white-space: nowrap;
}

.slash-arg {
  color: var(--text-muted);
  font-style: italic;
  white-space: nowrap;
}

.slash-desc {
  color: var(--text-muted);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.slash-badge {
  font-size: 0.7rem;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--border);
  color: var(--text-muted);
  white-space: nowrap;
}
```

- [ ] **Step 8: Verify manually (build and test)**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds

- [ ] **Step 9: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html server/web/static/style.css
git commit -m "feat: add slash command autocomplete dropdown UI"
```

---

### Task 4: Web UI — Command Execution Logic

**Files:**
- Modify: `server/web/static/app.js`

- [ ] **Step 1: Update handleInput to detect and route slash commands**

In `server/web/static/app.js`, replace the existing `/resume` interception in `handleInput()` (lines 773-780) with a general slash command router:

```javascript
    async handleInput() {
      if (!this.selectedSessionId || !this.inputText.trim()) return;
      // Close dropdown if still open
      this.slashDropdownOpen = false;
      const sess = this.currentSession;

      // Handle slash commands for managed sessions
      if (sess && sess.mode === 'managed' && this.inputText.trim().startsWith('/')) {
        await this.executeSlashCommand(this.inputText.trim());
        return;
      }

      if (sess && sess.mode === 'managed') {
        if (this.shellMode) {
          await this.executeShell();
        } else {
          await this.sendManagedMessage();
        }
      } else {
        await this.sendInstruction();
      }
    },
```

- [ ] **Step 2: Implement executeSlashCommand method**

Add this method to `server/web/static/app.js`:

```javascript
    async executeSlashCommand(input) {
      const parts = input.split(/\s+/);
      const cmdName = parts[0]; // e.g., "/help"
      const cmdArg = parts.slice(1).join(' '); // everything after the command name

      // Show user message in chat
      this.chatMessages.push({ role: 'user', content: input, msg_type: 'text', timestamp: new Date().toISOString() });
      this.$nextTick(() => this.scrollToBottom(true));
      this.inputText = '';

      switch (cmdName) {
        case '/resume':
          await this.openResumePicker();
          break;

        case '/clear':
          this.chatMessages = [];
          break;

        case '/compact':
          this.inputText = 'Please compact the conversation context and summarize what we\'ve been working on.';
          await this.sendManagedMessage();
          break;

        case '/cost': {
          const sess = this.currentSession;
          const cost = sess?.total_cost != null ? `$${sess.total_cost.toFixed(4)}` : 'unknown';
          this.chatMessages.push({
            role: 'system', content: `Session cost: ${cost}`,
            msg_type: 'text', timestamp: new Date().toISOString()
          });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }

        case '/help': {
          const cmds = this.slashCommands.map(c => {
            const arg = c.argHint ? ` ${c.argHint}` : '';
            return `**${c.name}**${arg} — ${c.description}`;
          }).join('\n');
          this.chatMessages.push({
            role: 'system', content: cmds,
            msg_type: 'text', timestamp: new Date().toISOString()
          });
          this.$nextTick(() => this.scrollToBottom(true));
          break;
        }

        default:
          // Custom command — fetch content and send as message
          await this.executeCustomCommand(cmdName, cmdArg);
          break;
      }
    },

    async executeCustomCommand(cmdName, cmdArg) {
      const name = cmdName.slice(1); // strip leading /
      try {
        const resp = await fetch(`/api/sessions/${this.selectedSessionId}/commands/${encodeURIComponent(name)}/content`, {
          headers: { 'Authorization': `Bearer ${this.apiKey}` }
        });
        if (!resp.ok) {
          this.chatMessages.push({
            role: 'system', content: `Command not found: ${cmdName}`,
            msg_type: 'text', timestamp: new Date().toISOString()
          });
          this.$nextTick(() => this.scrollToBottom(true));
          return;
        }
        const data = await resp.json();
        let prompt = data.content;
        if (cmdArg) {
          // Replace $ARGUMENTS placeholder or append
          if (prompt.includes('$ARGUMENTS')) {
            prompt = prompt.replace(/\$ARGUMENTS/g, cmdArg);
          } else {
            prompt = prompt + '\n\n' + cmdArg;
          }
        }
        this.inputText = prompt;
        await this.sendManagedMessage();
      } catch (e) {
        this.chatMessages.push({
          role: 'system', content: `Failed to execute ${cmdName}: ${e.message}`,
          msg_type: 'text', timestamp: new Date().toISOString()
        });
      }
    },
```

- [ ] **Step 3: Build and verify**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds

- [ ] **Step 4: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js
git commit -m "feat: implement slash command execution for built-in and custom commands"
```

---

### Task 5: Final Integration & Cleanup

**Files:**
- Modify: `server/web/static/app.js` (edge cases)
- Modify: `docs/superpowers/specs/2026-03-25-slash-commands-design.md` (if needed)

- [ ] **Step 1: Close dropdown on blur and handle edge cases**

Add to the textarea in `index.html`:
```html
@blur="setTimeout(() => slashDropdownOpen = false, 150)"
```

The 150ms delay allows click events on dropdown items to fire before the dropdown closes.

- [ ] **Step 2: Invalidate command cache on session change**

In the `selectSession` method, add near the top:
```javascript
      this.slashCommandsLoaded = false;
      this.slashCommands = [];
      this.slashDropdownOpen = false;
```

- [ ] **Step 3: Run all Go tests**

Run: `cd server && go test ./... -v`
Expected: All tests PASS

- [ ] **Step 4: Build final binary**

Run: `cd server && go build -o claude-controller .`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add server/web/static/app.js server/web/static/index.html
git commit -m "feat: slash command edge cases and cache invalidation"
```
