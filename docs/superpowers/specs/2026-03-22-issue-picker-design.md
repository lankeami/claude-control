# Issue Picker Design Spec

## Overview

Add a GitHub issue picker to the web UI that lets users browse repo issues, view details, and generate a structured prompt for Claude to work on an issue — including creating a feature branch and opening a draft PR.

## User Flow

1. User selects a managed session → right sidebar shows git info (branch, working tree status)
2. Below the git branch info, an **Issues** section loads automatically, fetching issues from the session's CWD git remote
3. Issues are shown in a paginated list (~10 at a time) with:
   - **Open/Closed toggle** (open selected by default)
   - **Search box** to filter issues
   - **"Show more"** link for pagination
4. Each issue row shows: status dot (green=open, purple=closed), title, `#number · opened X ago`
5. User clicks an issue → **detail view expands inline** showing:
   - Title, author, timestamp
   - Labels as colored pills
   - Scrollable body content (rendered markdown)
   - Close button (✕) to collapse back to list
   - **"Generate Prompt"** button
6. Clicking "Generate Prompt" populates the existing chat input textarea with a structured prompt:
   ```
   Work on GitHub issue #19: "Pick from a list of issues to develop against"

   Requirements:
   As a developer, I'm having fun playing around in claude-control...

   Create a feature branch, implement the solution, and open a draft PR linking to issue #19.
   ```
7. User can edit the prompt, then hits Send or ⌘↵
8. Claude creates a feature branch, develops the solution, and opens a draft PR

## Architecture

### Repo Detection

The server detects the GitHub repo from the managed session's CWD by running `git remote get-url origin` and parsing the owner/repo from the URL using a regex that supports both HTTPS (`https://github.com/owner/repo.git`) and SSH (`git@github.com:owner/repo.git`) formats.

### Backend — New API Endpoints

All endpoints use the **GitHub REST API** (`api.github.com`) directly via `net/http`, authenticated with a `GITHUB_TOKEN` stored in the server's `.env` file (configurable through the Settings modal in the web UI). This eliminates the dependency on the `gh` CLI being installed.

**Session mode guard:** These endpoints only work for managed sessions (which have a CWD). Hook-mode sessions return 400. The frontend hides the issues section for hook-mode sessions.

**Token management:** The `GITHUB_TOKEN` is stored in the `.env` file alongside other secrets and displayed masked (`****` + last 4 chars) in the settings UI. It requires `repo` scope for private repos or no scope for public repos.

#### `GET /api/sessions/{id}/github/issues`

Query params:
- `state` — `open` (default) or `closed`
- `search` — search string (optional)
- `limit` — number of issues to return (default 10)

Implementation: Calls the GitHub REST API. For plain listing, uses `GET /repos/{owner}/{repo}/issues?state={state}&per_page={limit+1}`. For search queries, uses the GitHub Search API: `GET /search/issues?q=repo:{owner/repo}+is:issue+state:{state}+{search}&per_page={limit+1}`.

The GitHub API returns `user` as an object (`{"login": "..."}`) and `labels` as an array of objects (`[{"name": "...", "color": "..."}]`). The Go handler defines structs to deserialize the raw JSON and reshapes it into the simplified response below, extracting `user.login` as `author` and mapping labels to `[{"name": "...", "color": "..."}]`. The Search API wraps results in `{ "items": [...] }` which is unwrapped before reshaping.

**Pagination:** "Show more" works by incrementing the `per_page` param (e.g., first load fetches 10, "Show more" fetches 20). We request `limit+1` results to determine `has_more`. The frontend replaces the full list on each fetch. If the number of results returned is less than the requested limit, there are no more results (used to hide the "Show more" link).

Response:
```json
{
  "repo": "lankeami/claude-control",
  "issues": [
    {
      "number": 19,
      "title": "Pick from a list of issues to develop against",
      "state": "OPEN",
      "created_at": "2026-03-22T02:42:03Z",
      "author": "lankeami",
      "labels": [{"name": "enhancement", "color": "a2eeef"}]
    }
  ],
  "has_more": false
}
```

#### `GET /api/sessions/{id}/github/issues/{number}`

Implementation: Calls `GET /repos/{owner}/{repo}/issues/{number}` on the GitHub REST API. Same struct reshaping as the list endpoint.

Response:
```json
{
  "number": 19,
  "title": "Pick from a list of issues to develop against",
  "state": "OPEN",
  "body": "As a developer, I'm having fun playing around...",
  "created_at": "2026-03-22T02:42:03Z",
  "author": "lankeami",
  "labels": [{"name": "enhancement", "color": "a2eeef"}]
}
```

### Frontend — Alpine.js State

New state properties:
- `githubIssues: []` — cached issue list
- `githubIssuesState: 'open'` — open/closed filter
- `githubIssuesSearch: ''` — search query
- `githubIssuesLimit: 10` — current fetch limit (increases on "Show more")
- `githubIssuesHasMore: false` — whether more results exist
- `githubIssuesLoading: false`
- `selectedIssue: null` — expanded issue detail
- `selectedIssueLoading: false`

New methods:
- `fetchGithubIssues(sessionId)` — calls list endpoint, updates state
- `fetchIssueDetail(sessionId, number)` — calls detail endpoint, expands view
- `generateIssuePrompt(issue)` — constructs prompt text, sets `this.inputText`, auto-resizes textarea
- `toggleIssueState(state)` — switches open/closed, re-fetches
- `searchIssues()` — debounced search, re-fetches
- `loadMoreIssues()` — increases limit by 10, re-fetches full list

### Frontend — HTML Structure

The issues section is added to `index.html` inside the right sidebar (`.file-tree-sidebar`), below the git info block. The entire sidebar already has `overflow-y: auto`, so the issues section scrolls naturally with the file tree — no separate scroll container needed. When the issue detail panel is expanded, it replaces the issue list inline (not a separate panel).

Structure:

```
.git-info-section (existing)
  ├── branch name + working tree status (existing)
  └── .issues-section (NEW)
      ├── header: "Issues" label + Open/Closed toggle pills
      ├── search input
      ├── issue list (scrollable)
      │   └── issue rows (clickable)
      ├── "Show more" pagination link
      └── issue detail panel (when expanded)
          ├── title, author, timestamp
          ├── labels
          ├── body (scrollable, rendered markdown)
          └── "Generate Prompt" button
```

### Frontend — CSS

New styles for:
- `.issues-section` — container below git info
- `.issues-header` — flex row with title and toggle pills
- `.issue-state-toggle` — open/closed pill buttons
- `.issues-search` — search input matching existing file tree search style
- `.issue-row` — clickable issue item with hover state
- `.issue-detail` — expanded view panel
- `.issue-labels` — label pill styling; background color derived from the `color` field returned by the GitHub API (prefixed with `#`)
- `.issue-body` — markdown content area, rendered using the existing `marked.parse()` (already loaded in index.html)
- `.generate-prompt-btn` — green action button

### Prompt Generation

The generated prompt follows this template:

```
Work on GitHub issue #{number}: "{title}"

Requirements:
{body}

Create a feature branch, implement the solution, and open a draft PR linking to issue #{number}.
```

The prompt is inserted into the existing `inputText` Alpine.js property, which binds to the chat textarea. The textarea auto-resizes to fit the content (existing behavior via the `@input` handler).

## Error Handling

- **No GitHub token configured**: Show "GitHub token not configured — add it in Settings" message in issues section
- **Not a git repo / no remote**: Show "No GitHub remote detected" message
- **Auth failure (401/403)**: Show "GitHub token is invalid or lacks permissions — update it in Settings" message
- **Network errors**: Show inline error with retry button
- **Empty results**: Show "No issues found" with current filter state

## Non-Goals

- Creating/editing issues from the UI (use GitHub directly)
- Showing PR status or linking sessions to issues in the database
- Supporting non-GitHub remotes (GitLab, Bitbucket)
- Caching issues across sessions or in the database
