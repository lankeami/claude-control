# Directory Search UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace as-you-type directory search with a button-triggered search, and sort results shallower-first.

**Architecture:** Two independent changes — backend adds a `sort.Slice` by path depth after the walk in `browse.go`; frontend replaces `@input` handler with a 🔍 button and Enter-key trigger, removing the debounce timer.

**Tech Stack:** Go (backend), Alpine.js (frontend), no new dependencies.

---

## File Map

| File | Change |
|------|--------|
| `server/api/browse.go` | Add `sort.Slice` after walk loop |
| `server/api/browse_test.go` | Add `TestHandleBrowseSearchDepthSort` test |
| `server/web/static/index.html` | Wrap input in flex row, add 🔍 button, remove `@input` handler |
| `server/web/static/app.js` | Rename `onDirSearchInput` → `triggerDirSearch`, remove debounce timer and `_dirSearchTimer` |

---

## Task 1: Backend — sort search results by depth

**Files:**
- Modify: `server/api/browse.go:112-145`
- Modify: `server/api/browse_test.go`

- [ ] **Step 1: Write the failing test**

Add this test to `server/api/browse_test.go` (after the last existing test):

```go
func TestHandleBrowseSearchDepthSort(t *testing.T) {
	ts, store := setupTestServer(t)
	defer ts.Close()
	defer store.Close()

	// Structure:
	//   base/target            (depth 1 from base — shallower)
	//   base/a/b/target        (depth 3 from base — deeper)
	base := t.TempDir()
	shallow := filepath.Join(base, "target")
	deep := filepath.Join(base, "a", "b", "target")
	for _, dir := range []string{shallow, deep} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	req, _ := http.NewRequest("GET",
		ts.URL+"/api/browse/search?path="+base+"&q=target",
		nil)
	req.Header.Set("Authorization", "Bearer test-api-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	entries := result["entries"].([]interface{})

	if len(entries) != 2 {
		t.Fatalf("len(entries)=%d, want 2", len(entries))
	}
	// Shallow result must come first.
	first := entries[0].(map[string]interface{})
	if first["path"] != shallow {
		t.Errorf("first path=%v, want %v (shallower dir)", first["path"], shallow)
	}
	second := entries[1].(map[string]interface{})
	if second["path"] != deep {
		t.Errorf("second path=%v, want %v (deeper dir)", second["path"], deep)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd server && go test ./api/ -v -run TestHandleBrowseSearchDepthSort
```

Expected: FAIL — results will be in walk order (deep may come before shallow depending on filesystem ordering, or the assertion will fail).

- [ ] **Step 3: Add sort to `handleBrowseSearch` in `server/api/browse.go`**

After the `filepath.Walk` block (line ~137, just before `if results == nil`), add the sort. The final section of `handleBrowseSearch` should look like this:

```go
	// (existing walk block ends here)

	sort.Slice(results, func(i, j int) bool {
		sep := string(filepath.Separator)
		di := strings.Count(results[i].Path, sep)
		dj := strings.Count(results[j].Path, sep)
		if di != dj {
			return di < dj
		}
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	if results == nil {
		results = []dirEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": results,
	})
```

The `sort` package is already imported at the top of `browse.go`. No new imports needed.

- [ ] **Step 4: Run all browse tests to verify they pass**

```bash
cd server && go test ./api/ -v -run TestHandleBrowseSearch
```

Expected: all four `TestHandleBrowseSearch*` tests PASS.

- [ ] **Step 5: Commit**

```bash
git add server/api/browse.go server/api/browse_test.go
git commit -m "feat: sort directory search results shallower-first"
```

---

## Task 2: Frontend — button-triggered search

**Files:**
- Modify: `server/web/static/index.html:1091-1096`
- Modify: `server/web/static/app.js:56-59,1405-1430`

- [ ] **Step 1: Replace the search input row in `index.html`**

Find this block (around line 1091):

```html
          <!-- Directory search -->
          <div style="margin-bottom:8px;">
            <input x-model="dirSearch" type="text" placeholder="Search folders…"
                   class="modal-input" style="width:100%; box-sizing:border-box;"
                   @input="onDirSearchInput()">
          </div>
```

Replace it with:

```html
          <!-- Directory search -->
          <div style="margin-bottom:8px; display:flex; gap:6px;">
            <input x-model="dirSearch" type="text" placeholder="Search folders…"
                   class="modal-input" style="flex:1;"
                   @keydown.enter.prevent="triggerDirSearch()">
            <button class="btn btn-sm" @click="triggerDirSearch()">🔍</button>
          </div>
```

- [ ] **Step 2: Remove `_dirSearchTimer` from the state object in `app.js`**

Find (around line 56):

```js
    dirSearch: '',
    dirSearchResults: [],
    dirSearchLoading: false,
    _dirSearchTimer: null,
```

Replace with:

```js
    dirSearch: '',
    dirSearchResults: [],
    dirSearchLoading: false,
```

- [ ] **Step 3: Replace `onDirSearchInput` with `triggerDirSearch` in `app.js`**

Find the entire `onDirSearchInput` function (around line 1405):

```js
    onDirSearchInput() {
      clearTimeout(this._dirSearchTimer);
      const q = this.dirSearch.trim();
      if (!q) {
        this.dirSearchResults = [];
        this.dirSearchLoading = false;
        return;
      }
      this.dirSearchLoading = true;
      this._dirSearchTimer = setTimeout(async () => {
        try {
          const path = encodeURIComponent(this.browsePath || '');
          const res = await fetch(
            '/api/browse/search?path=' + path + '&q=' + encodeURIComponent(q),
            { headers: { 'Authorization': 'Bearer ' + this.apiKey } }
          );
          if (!res.ok) throw new Error(await res.text());
          const data = await res.json();
          this.dirSearchResults = data.entries || [];
        } catch (e) {
          this.dirSearchResults = [];
        } finally {
          this.dirSearchLoading = false;
        }
      }, 300);
    },
```

Replace it with:

```js
    async triggerDirSearch() {
      const q = this.dirSearch.trim();
      if (!q) {
        this.dirSearchResults = [];
        this.dirSearchLoading = false;
        return;
      }
      this.dirSearchLoading = true;
      try {
        const path = encodeURIComponent(this.browsePath || '');
        const res = await fetch(
          '/api/browse/search?path=' + path + '&q=' + encodeURIComponent(q),
          { headers: { 'Authorization': 'Bearer ' + this.apiKey } }
        );
        if (!res.ok) throw new Error(await res.text());
        const data = await res.json();
        this.dirSearchResults = data.entries || [];
      } catch (e) {
        this.dirSearchResults = [];
      } finally {
        this.dirSearchLoading = false;
      }
    },
```

- [ ] **Step 4: Verify the server builds cleanly**

```bash
cd server && go build -o /dev/null .
```

Expected: no output, exit code 0.

- [ ] **Step 5: Start the server and manually verify**

```bash
cd server && go run . &
```

Open `http://localhost:8080` in a browser. Open the new-session modal. Confirm:
- Typing in the search box does NOT trigger a search.
- Pressing Enter triggers the search.
- Clicking 🔍 triggers the search.
- Clearing the input and clicking 🔍 clears the results panel.
- Results for a multi-depth query appear shallower-first.

Stop the server after verifying (`kill %1` or Ctrl+C).

- [ ] **Step 6: Commit**

```bash
git add server/web/static/index.html server/web/static/app.js
git commit -m "feat: trigger directory search on button click, not on input"
```
