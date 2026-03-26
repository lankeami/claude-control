package api

import (
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
	"github.com/jaychinthrajah/claude-controller/server/web"
)

type Server struct {
	store       *db.Store
	manager     *managed.Manager
	envPath     string
	permissions *PermissionManager
}

func NewRouter(store *db.Store, apiKey string, mgr *managed.Manager, envPath string) http.Handler {
	s := &Server{store: store, manager: mgr, envPath: envPath, permissions: NewPermissionManager()}

	// API mux — all existing endpoints, behind auth middleware
	apiMux := http.NewServeMux()

	// Session endpoints
	apiMux.HandleFunc("POST /api/sessions/register", s.handleRegisterSession)
	apiMux.HandleFunc("POST /api/sessions/{id}/heartbeat", s.handleHeartbeat)
	apiMux.HandleFunc("GET /api/sessions", s.handleListSessions)
	apiMux.HandleFunc("PUT /api/sessions/{id}/archive", s.handleSetArchived)
	apiMux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	apiMux.HandleFunc("PUT /api/sessions/{id}/name", s.handleUpdateSessionName)

	// Prompt endpoints
	apiMux.HandleFunc("POST /api/prompts", s.handleCreatePrompt)
	apiMux.HandleFunc("GET /api/prompts/{id}/response", s.handleGetPromptResponse)
	apiMux.HandleFunc("POST /api/prompts/{id}/respond", s.handleRespondToPrompt)
	apiMux.HandleFunc("GET /api/prompts", s.handleListPrompts)

	// Instruction endpoints
	apiMux.HandleFunc("POST /api/sessions/{id}/instruct", s.handleInstruct)
	apiMux.HandleFunc("GET /api/sessions/{id}/instructions", s.handleFetchInstructions)

	// Transcript endpoint
	apiMux.HandleFunc("GET /api/sessions/{id}/transcript", s.handleGetTranscript)

	// Pairing/status endpoints
	apiMux.HandleFunc("GET /api/pairing", s.handlePairing)
	apiMux.HandleFunc("GET /api/status", s.handleStatus)

	// Browse endpoint
	apiMux.HandleFunc("GET /api/browse", s.handleBrowse)

	// Managed session endpoints
	apiMux.HandleFunc("POST /api/sessions/create", s.handleCreateManagedSession)
	apiMux.HandleFunc("POST /api/sessions/create-project", s.handleCreateProject)
	apiMux.HandleFunc("GET /api/sessions/recent-dirs", s.handleRecentDirs)
	apiMux.HandleFunc("POST /api/sessions/{id}/message", s.handleSendMessage)
	apiMux.HandleFunc("POST /api/sessions/{id}/interrupt", s.handleInterrupt)
	apiMux.HandleFunc("GET /api/sessions/{id}/messages", s.handleListMessages)
	apiMux.HandleFunc("GET /api/sessions/{id}/resumable", s.handleResumableList)
	apiMux.HandleFunc("POST /api/sessions/{id}/resume", s.handleResumeSession)
	apiMux.HandleFunc("POST /api/sessions/{id}/shell", s.handleShellExecute)
	apiMux.HandleFunc("POST /api/sessions/{id}/permission-request", s.handlePermissionRequest)
	apiMux.HandleFunc("POST /api/sessions/{id}/permission-respond", s.handlePermissionRespond)
	apiMux.HandleFunc("GET /api/sessions/{id}/pending-permission", s.handlePendingPermission)
	apiMux.HandleFunc("GET /api/sessions/{id}/commands", s.handleListCommands)
	apiMux.HandleFunc("GET /api/sessions/{id}/commands/content", s.handleCommandContent)

	// File browser endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/files", s.handleListSessionFiles)
	apiMux.HandleFunc("GET /api/sessions/{id}/filetree", s.handleFileTree)
	apiMux.HandleFunc("GET /api/files/content", s.handleGetFileContent)
	apiMux.HandleFunc("GET /api/files/diff", s.handleFileDiff)

	// GitHub endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues", s.handleListGithubIssues)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/{number}", s.handleGetGithubIssue)

	// Settings endpoints
	apiMux.HandleFunc("GET /api/settings/exists", s.handleSettingsExists)
	apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
	apiMux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// Scheduled task endpoints
	apiMux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	apiMux.HandleFunc("GET /api/tasks", s.handleListTasks)
	apiMux.HandleFunc("GET /api/tasks/{taskId}", s.handleGetTask)
	apiMux.HandleFunc("PUT /api/tasks/{taskId}", s.handleUpdateTask)
	apiMux.HandleFunc("DELETE /api/tasks/{taskId}", s.handleDeleteTask)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs", s.handleListTaskRuns)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs/{runId}", s.handleGetTaskRun)
	apiMux.HandleFunc("POST /api/tasks/{taskId}/trigger", s.handleTriggerTask)

	rl := NewRateLimiter(60, 10)
	authedAPI := rl.Middleware(AuthMiddleware(apiKey, rl, apiMux))

	// Root mux — routes to appropriate handler
	root := http.NewServeMux()

	// SSE endpoint — handles its own auth via query param
	root.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		s.handleSSEEvents(w, r, apiKey)
	})

	// Per-session SSE stream — handles its own auth via query param
	root.HandleFunc("GET /api/sessions/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		s.handleSessionStream(w, r, apiKey)
	})

	// All other /api/ routes — through auth middleware
	root.Handle("/api/", authedAPI)

	// Visual file server — no auth required
	root.HandleFunc("/visual/", s.handleVisual)

	// Static files — no auth required
	root.Handle("/", web.Handler())

	return root
}
