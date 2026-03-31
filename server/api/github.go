package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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

// GitHub API response structs

type ghAPIUser struct {
	Login string `json:"login"`
}

type ghAPILabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ghAPIIssue struct {
	Number       int              `json:"number"`
	Title        string           `json:"title"`
	State        string           `json:"state"`
	CreatedAt    string           `json:"created_at"`
	User         ghAPIUser        `json:"user"`
	Labels       []ghAPILabel     `json:"labels"`
	Body         string           `json:"body"`
	PullRequest  *json.RawMessage `json:"pull_request,omitempty"`
}

func reshapeAPIIssue(g ghAPIIssue) issueResponse {
	labels := make([]issueLabel, 0, len(g.Labels))
	for _, l := range g.Labels {
		labels = append(labels, issueLabel{Name: l.Name, Color: l.Color})
	}
	return issueResponse{
		Number:    g.Number,
		Title:     g.Title,
		State:     g.State,
		CreatedAt: g.CreatedAt,
		Author:    g.User.Login,
		Labels:    labels,
		Body:      g.Body,
	}
}

// repoFromRemote parses "owner/repo" from a git remote URL.
// Supports HTTPS (https://github.com/owner/repo.git) and SSH (git@github.com:owner/repo.git).
var repoPattern = regexp.MustCompile(`github\.com[:/]([^/]+/[^/.\s]+?)(?:\.git)?$`)

func repoFromRemote(cwd string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("no git remote 'origin' found")
	}
	url := strings.TrimSpace(string(out))
	m := repoPattern.FindStringSubmatch(url)
	if m == nil {
		return "", fmt.Errorf("could not parse GitHub repo from remote URL: %s", url)
	}
	return m[1], nil
}

// githubToken reads the GITHUB_TOKEN from the env file.
func (s *Server) githubToken() string {
	vals := readEnvFile(s.envPath)
	return vals["GITHUB_TOKEN"]
}

// githubAPIGet makes an authenticated GET request to the GitHub REST API.
func githubAPIGet(token, url string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
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

	token := s.githubToken()
	if token == "" {
		jsonError(w, "GitHub token not configured — add it in Settings", http.StatusBadRequest)
		return
	}

	repo, err := repoFromRemote(cwd)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", repo, number)
	body, status, err := githubAPIGet(token, apiURL)
	if err != nil {
		jsonError(w, "Failed to reach GitHub API", http.StatusInternalServerError)
		return
	}
	if status == 404 {
		jsonError(w, fmt.Sprintf("Issue #%d not found", number), http.StatusNotFound)
		return
	}
	if status == 401 || status == 403 {
		jsonError(w, "GitHub token is invalid or lacks permissions — update it in Settings", http.StatusUnauthorized)
		return
	}
	if status != 200 {
		jsonError(w, fmt.Sprintf("GitHub API error (HTTP %d)", status), http.StatusInternalServerError)
		return
	}

	var raw ghAPIIssue
	if err := json.Unmarshal(body, &raw); err != nil {
		jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reshapeAPIIssue(raw))
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

	token := s.githubToken()
	if token == "" {
		jsonError(w, "GitHub token not configured — add it in Settings", http.StatusBadRequest)
		return
	}

	repo, err := repoFromRemote(cwd)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var apiURL string
	if search != "" {
		// Use search API for text queries
		query := fmt.Sprintf("repo:%s is:issue state:%s %s", repo, state, search)
		apiURL = fmt.Sprintf("https://api.github.com/search/issues?q=%s&per_page=%d",
			strings.ReplaceAll(query, " ", "+"), limit+1)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/issues?state=%s&per_page=%d",
			repo, state, limit+1)
	}

	body, status, err := githubAPIGet(token, apiURL)
	if err != nil {
		jsonError(w, "Failed to reach GitHub API", http.StatusInternalServerError)
		return
	}
	if status == 401 || status == 403 {
		jsonError(w, "GitHub token is invalid or lacks permissions — update it in Settings", http.StatusUnauthorized)
		return
	}
	if status != 200 {
		jsonError(w, fmt.Sprintf("GitHub API error (HTTP %d)", status), http.StatusInternalServerError)
		return
	}

	var raw []ghAPIIssue
	if search != "" {
		// Search API wraps results in { items: [...] }
		var searchResult struct {
			Items []ghAPIIssue `json:"items"`
		}
		if err := json.Unmarshal(body, &searchResult); err != nil {
			jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
			return
		}
		raw = searchResult.Items
	} else {
		if err := json.Unmarshal(body, &raw); err != nil {
			jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
			return
		}
	}

	// Filter out pull requests from the issues list (GitHub /issues API returns both)
	filtered := make([]ghAPIIssue, 0, len(raw))
	for _, g := range raw {
		if g.PullRequest == nil {
			filtered = append(filtered, g)
		}
	}
	raw = filtered

	hasMore := len(raw) > limit
	if hasMore {
		raw = raw[:limit]
	}

	issues := make([]issueResponse, 0, len(raw))
	for _, g := range raw {
		issues = append(issues, reshapeAPIIssue(g))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(issueListResponse{
		Repo:    repo,
		Issues:  issues,
		HasMore: hasMore,
	})
}

// Pull Request endpoints

type pullResponse struct {
	Number    int          `json:"number"`
	Title     string       `json:"title"`
	State     string       `json:"state"`
	CreatedAt string       `json:"created_at"`
	Author    string       `json:"author"`
	Labels    []issueLabel `json:"labels"`
	Body      string       `json:"body"`
}

type pullListResponse struct {
	Repo    string         `json:"repo"`
	Pulls   []pullResponse `json:"pulls"`
	HasMore bool           `json:"has_more"`
}

func reshapeAPIPull(g ghAPIIssue) pullResponse {
	labels := make([]issueLabel, 0, len(g.Labels))
	for _, l := range g.Labels {
		labels = append(labels, issueLabel{Name: l.Name, Color: l.Color})
	}
	return pullResponse{
		Number:    g.Number,
		Title:     g.Title,
		State:     g.State,
		CreatedAt: g.CreatedAt,
		Author:    g.User.Login,
		Labels:    labels,
		Body:      g.Body,
	}
}

func (s *Server) handleListGithubPulls(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		jsonError(w, "Pull requests are only available for managed sessions", http.StatusBadRequest)
		return
	}

	cwd := sess.CWD
	if cwd == "" {
		jsonError(w, "Session has no working directory", http.StatusBadRequest)
		return
	}

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

	token := s.githubToken()
	if token == "" {
		jsonError(w, "GitHub token not configured — add it in Settings", http.StatusBadRequest)
		return
	}

	repo, err := repoFromRemote(cwd)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var apiURL string
	if search != "" {
		query := fmt.Sprintf("repo:%s is:pr state:%s %s", repo, state, search)
		apiURL = fmt.Sprintf("https://api.github.com/search/issues?q=%s&per_page=%d",
			strings.ReplaceAll(query, " ", "+"), limit+1)
	} else {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/pulls?state=%s&per_page=%d",
			repo, state, limit+1)
	}

	body, status, err := githubAPIGet(token, apiURL)
	if err != nil {
		jsonError(w, "Failed to reach GitHub API", http.StatusInternalServerError)
		return
	}
	if status == 401 || status == 403 {
		jsonError(w, "GitHub token is invalid or lacks permissions — update it in Settings", http.StatusUnauthorized)
		return
	}
	if status != 200 {
		jsonError(w, fmt.Sprintf("GitHub API error (HTTP %d)", status), http.StatusInternalServerError)
		return
	}

	var raw []ghAPIIssue
	if search != "" {
		var searchResult struct {
			Items []ghAPIIssue `json:"items"`
		}
		if err := json.Unmarshal(body, &searchResult); err != nil {
			jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
			return
		}
		raw = searchResult.Items
	} else {
		if err := json.Unmarshal(body, &raw); err != nil {
			jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
			return
		}
	}

	hasMore := len(raw) > limit
	if hasMore {
		raw = raw[:limit]
	}

	pulls := make([]pullResponse, 0, len(raw))
	for _, g := range raw {
		pulls = append(pulls, reshapeAPIPull(g))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pullListResponse{
		Repo:    repo,
		Pulls:   pulls,
		HasMore: hasMore,
	})
}

func (s *Server) handleGetGithubPull(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")

	sess, err := s.store.GetSessionByID(sessionID)
	if err != nil {
		jsonError(w, "Session not found", http.StatusNotFound)
		return
	}

	if sess.Mode != "managed" {
		jsonError(w, "Pull requests are only available for managed sessions", http.StatusBadRequest)
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
		jsonError(w, "Invalid pull request number", http.StatusBadRequest)
		return
	}

	token := s.githubToken()
	if token == "" {
		jsonError(w, "GitHub token not configured — add it in Settings", http.StatusBadRequest)
		return
	}

	repo, err := repoFromRemote(cwd)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", repo, number)
	body, status, err := githubAPIGet(token, apiURL)
	if err != nil {
		jsonError(w, "Failed to reach GitHub API", http.StatusInternalServerError)
		return
	}
	if status == 404 {
		jsonError(w, fmt.Sprintf("Pull request #%d not found", number), http.StatusNotFound)
		return
	}
	if status == 401 || status == 403 {
		jsonError(w, "GitHub token is invalid or lacks permissions — update it in Settings", http.StatusUnauthorized)
		return
	}
	if status != 200 {
		jsonError(w, fmt.Sprintf("GitHub API error (HTTP %d)", status), http.StatusInternalServerError)
		return
	}

	var raw ghAPIIssue
	if err := json.Unmarshal(body, &raw); err != nil {
		jsonError(w, "Unexpected response from GitHub API", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reshapeAPIPull(raw))
}
