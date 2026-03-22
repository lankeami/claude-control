package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

// ghError returns a user-friendly error message from a failed gh command.
func ghError(err error, stderr *bytes.Buffer) string {
	if stderr != nil && stderr.Len() > 0 {
		msg := strings.TrimSpace(stderr.String())
		// Common gh CLI errors → friendly messages
		if strings.Contains(msg, "not a git repository") {
			return "This directory is not a git repository"
		}
		if strings.Contains(msg, "could not determine repo") || strings.Contains(msg, "no git remotes") {
			return "No GitHub remote found in this repository"
		}
		if strings.Contains(msg, "auth login") || strings.Contains(msg, "authentication") {
			return "GitHub CLI not authenticated — run 'gh auth login' in your terminal"
		}
		if strings.Contains(msg, "not found") || strings.Contains(msg, "Could not resolve") {
			return "Repository not found on GitHub — check that the remote exists"
		}
		if strings.Contains(msg, "gh: command not found") || strings.Contains(msg, "executable file not found") {
			return "GitHub CLI (gh) is not installed"
		}
	}
	// Fallback: check if gh isn't installed
	if execErr, ok := err.(*exec.Error); ok {
		if strings.Contains(execErr.Error(), "not found") {
			return "GitHub CLI (gh) is not installed"
		}
	}
	return "Could not connect to GitHub — check that 'gh' is installed and authenticated"
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// gh CLI raw output structs

type ghAuthor struct {
	Login string `json:"login"`
}

type ghLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ghIssue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	CreatedAt string   `json:"createdAt"`
	Author    ghAuthor `json:"author"`
	Labels    []ghLabel `json:"labels"`
	Body      string   `json:"body"`
}

// Response structs

type issueLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type issueResponse struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	State     string       `json:"state"`
	CreatedAt string       `json:"created_at"`
	Author    string       `json:"author"`
	Labels    []issueLabel `json:"labels"`
	Body      string       `json:"body"`
}

type issueListResponse struct {
	Repo    string          `json:"repo"`
	Issues  []issueResponse `json:"issues"`
	HasMore bool            `json:"has_more"`
}

func reshapeIssue(g ghIssue) issueResponse {
	labels := make([]issueLabel, 0, len(g.Labels))
	for _, l := range g.Labels {
		labels = append(labels, issueLabel{Name: l.Name, Color: l.Color})
	}
	return issueResponse{
		Number:    g.Number,
		Title:     g.Title,
		State:     g.State,
		CreatedAt: g.CreatedAt,
		Author:    g.Author.Login,
		Labels:    labels,
		Body:      g.Body,
	}
}

func (s *Server) handleGetGithubIssue(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		jsonError(w, "Issues are only available for managed sessions", http.StatusBadRequest)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		jsonError(w, "Session has no working directory", http.StatusBadRequest)
		return
	}

	numberStr := r.PathValue("number")
	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 {
		jsonError(w, "Invalid issue number", http.StatusBadRequest)
		return
	}

	var stderr bytes.Buffer
	cmd := exec.Command("gh", "issue", "view", strconv.Itoa(number),
		"--json", "number,title,state,body,createdAt,author,labels")
	cmd.Dir = cwd
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		jsonError(w, ghError(err, &stderr), http.StatusInternalServerError)
		return
	}

	var raw ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		jsonError(w, "Unexpected response from GitHub CLI", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reshapeIssue(raw))
}

func (s *Server) handleListGithubIssues(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		jsonError(w, "Issues are only available for managed sessions", http.StatusBadRequest)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		jsonError(w, "Session has no working directory", http.StatusBadRequest)
		return
	}

	// Parse query params
	q := r.URL.Query()

	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" {
		jsonError(w, "State must be 'open' or 'closed'", http.StatusBadRequest)
		return
	}

	search := q.Get("search")

	limit := 10
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 {
			jsonError(w, "Limit must be a positive number", http.StatusBadRequest)
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}

	// Detect repo name
	var repoStderr bytes.Buffer
	repoCmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	repoCmd.Dir = cwd
	repoCmd.Stderr = &repoStderr
	repoOut, err := repoCmd.Output()
	if err != nil {
		jsonError(w, ghError(err, &repoStderr), http.StatusInternalServerError)
		return
	}
	repo := strings.TrimSpace(string(repoOut))

	// Build gh issue list args
	args := []string{
		"issue", "list",
		"--state", state,
		"--limit", strconv.Itoa(limit + 1),
		"--json", "number,title,state,createdAt,author,labels,body",
	}
	if search != "" {
		args = append(args, "--search", search)
	}

	var issueStderr bytes.Buffer
	issueCmd := exec.Command("gh", args...)
	issueCmd.Dir = cwd
	issueCmd.Stderr = &issueStderr
	issueOut, err := issueCmd.Output()
	if err != nil {
		jsonError(w, ghError(err, &issueStderr), http.StatusInternalServerError)
		return
	}

	var raw []ghIssue
	if err := json.Unmarshal(issueOut, &raw); err != nil {
		jsonError(w, "Unexpected response from GitHub CLI", http.StatusInternalServerError)
		return
	}

	hasMore := len(raw) > limit
	if hasMore {
		raw = raw[:limit]
	}

	issues := make([]issueResponse, 0, len(raw))
	for _, g := range raw {
		issues = append(issues, reshapeIssue(g))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issueListResponse{
		Repo:    repo,
		Issues:  issues,
		HasMore: hasMore,
	})
}
