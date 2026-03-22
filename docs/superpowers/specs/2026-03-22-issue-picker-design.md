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

The server detects the GitHub repo from the managed session's CWD by running `gh repo view --json nameWithOwner` in that directory. This is done once when issues are first fetched for a session and cached.

### Backend — New API Endpoints

All endpoints shell out to the `gh` CLI in the session's CWD directory, reusing the user's existing `gh` auth. No API tokens to manage.

#### `GET /api/sessions/{id}/github/issues`

Query params:
- `state` — `open` (default) or `closed`
- `search` — search string (optional)
- `limit` — number of issues to return (default 10)
- `page` — page number for pagination (default 1)

Implementation: Shells out to `gh issue list --state {state} --search {search} --limit {limit} --json number,title,state,createdAt,author,labels` in the session's CWD.

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
      "labels": ["enhancement"]
    }
  ],
  "total_count": 8
}
```

#### `GET /api/sessions/{id}/github/issues/{number}`

Implementation: Shells out to `gh issue view {number} --json number,title,state,body,createdAt,author,labels,comments` in the session's CWD.

Response:
```json
{
  "number": 19,
  "title": "Pick from a list of issues to develop against",
  "state": "OPEN",
  "body": "As a developer, I'm having fun playing around...",
  "created_at": "2026-03-22T02:42:03Z",
  "author": "lankeami",
  "labels": ["enhancement"],
  "comments": []
}
```

### Frontend — Alpine.js State

New state properties:
- `githubIssues: []` — cached issue list
- `githubIssuesState: 'open'` — open/closed filter
- `githubIssuesSearch: ''` — search query
- `githubIssuesPage: 1` — current page
- `githubIssuesLoading: false`
- `githubIssuesTotal: 0` — total count for pagination
- `selectedIssue: null` — expanded issue detail
- `selectedIssueLoading: false`

New methods:
- `fetchGithubIssues(sessionId)` — calls list endpoint, updates state
- `fetchIssueDetail(sessionId, number)` — calls detail endpoint, expands view
- `generateIssuePrompt(issue)` — constructs prompt text, sets `this.inputText`, auto-resizes textarea
- `toggleIssueState(state)` — switches open/closed, re-fetches
- `searchIssues()` — debounced search, re-fetches
- `loadMoreIssues()` — increments page, appends results

### Frontend — HTML Structure

The issues section is added to `index.html` inside the right sidebar's git info area, below the branch name and working tree status. Structure:

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
- `.issue-labels` — label pill styling
- `.issue-body` — scrollable markdown content area
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

- **No `gh` CLI installed**: Show "GitHub CLI not found" message in issues section
- **Not a git repo / no remote**: Show "No GitHub remote detected" message
- **Auth failure**: Show "Run `gh auth login` to authenticate" message
- **Network errors**: Show inline error with retry button
- **Empty results**: Show "No issues found" with current filter state

## Non-Goals

- Creating/editing issues from the UI (use GitHub directly)
- Showing PR status or linking sessions to issues in the database
- Supporting non-GitHub remotes (GitLab, Bitbucket)
- Caching issues across sessions or in the database
