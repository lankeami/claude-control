package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
)

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
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		http.Error(w, `{"error":"github issues only available for managed sessions"}`, http.StatusBadRequest)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		http.Error(w, `{"error":"session has no working directory"}`, http.StatusBadRequest)
		return
	}

	numberStr := r.PathValue("number")
	number, err := strconv.Atoi(numberStr)
	if err != nil || number < 1 {
		http.Error(w, `{"error":"number must be a positive integer"}`, http.StatusBadRequest)
		return
	}

	cmd := exec.Command("gh", "issue", "view", strconv.Itoa(number),
		"--json", "number,title,state,body,createdAt,author,labels")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, `{"error":"failed to get github issue: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	var raw ghIssue
	if err := json.Unmarshal(out, &raw); err != nil {
		http.Error(w, `{"error":"failed to parse gh output: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reshapeIssue(raw))
}

func (s *Server) handleListGithubIssues(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		http.Error(w, `{"error":"session not found"}`, http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		http.Error(w, `{"error":"github issues only available for managed sessions"}`, http.StatusBadRequest)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		http.Error(w, `{"error":"session has no working directory"}`, http.StatusBadRequest)
		return
	}

	// Parse query params
	q := r.URL.Query()

	state := q.Get("state")
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" {
		http.Error(w, `{"error":"state must be open or closed"}`, http.StatusBadRequest)
		return
	}

	search := q.Get("search")

	limit := 10
	if ls := q.Get("limit"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 {
			http.Error(w, `{"error":"limit must be a positive integer"}`, http.StatusBadRequest)
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}

	// Detect repo name
	repoCmd := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	repoCmd.Dir = cwd
	repoOut, err := repoCmd.Output()
	if err != nil {
		http.Error(w, `{"error":"failed to detect github repo: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	repo := string(repoOut)
	// trim trailing newline
	for len(repo) > 0 && (repo[len(repo)-1] == '\n' || repo[len(repo)-1] == '\r') {
		repo = repo[:len(repo)-1]
	}

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

	issueCmd := exec.Command("gh", args...)
	issueCmd.Dir = cwd
	issueOut, err := issueCmd.Output()
	if err != nil {
		http.Error(w, `{"error":"failed to list github issues: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	var raw []ghIssue
	if err := json.Unmarshal(issueOut, &raw); err != nil {
		http.Error(w, `{"error":"failed to parse gh output: `+err.Error()+`"}`, http.StatusInternalServerError)
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
