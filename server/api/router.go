package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
	"github.com/jaychinthrajah/claude-controller/server/web"
)

// TaskTrigger runs a scheduled task immediately, returning the created run.
// Wired to scheduler.Trigger in production; nil when no scheduler is available.
type TaskTrigger func(db.ScheduledTask) (*db.TaskRun, error)

type Server struct {
	store             *db.Store
	manager           SessionManager
	taskTrigger       TaskTrigger
	workflowEngine    *managed.WorkflowEngine
	envPath           string
	permissions       *PermissionManager
	pendingQuestions  *PendingQuestionManager
	shutdownFunc      func() // called to trigger server restart
	restartInProgress atomic.Bool
	serverID          string // unique ID per server instance, used by clients to detect restart
	usageUpstreamURL  string // override for tests; empty means use real Anthropic URL
	skipKeychain      bool   // skip macOS keychain lookup in tests
	usageCache        *UsageCache
	usageCacheMu      sync.RWMutex
	// interactiveTurns maps sessionID -> *interactiveTurnState for the turn
	// currently in flight. The transcript callback is registered once at
	// process spawn and outlives individual turns, so it must look up the
	// live turn state here instead of capturing it.
	interactiveTurns sync.Map
}

func NewRouter(store *db.Store, apiKey string, mgr SessionManager, envPath string, shutdownFunc func(), serverID string, taskTrigger TaskTrigger) http.Handler {
	s := &Server{store: store, manager: mgr, envPath: envPath, permissions: NewPermissionManager(), pendingQuestions: NewPendingQuestionManager(), shutdownFunc: shutdownFunc, serverID: serverID, taskTrigger: taskTrigger}

	// Initialize workflow engine with callbacks wired to the server
	s.workflowEngine = managed.NewWorkflowEngine(
		store,
		func(sessionID, prompt string) error {
			return s.sendWorkflowMessage(sessionID, prompt)
		},
		func(sessionID string) (string, error) {
			sess, err := store.GetSession(sessionID)
			if err != nil {
				return "", err
			}
			if sess == nil {
				return "", fmt.Errorf("session not found: %s", sessionID)
			}
			return sess.ActivityState, nil
		},
		func(sessionID string) error {
			return s.interruptManagedSession(sessionID)
		},
	)
	s.workflowEngine.RecoverStaleRuns()

	// API mux — all existing endpoints, behind auth middleware
	apiMux := http.NewServeMux()

	// Session endpoints
	apiMux.HandleFunc("POST /api/sessions/register", s.handleRegisterSession)
	apiMux.HandleFunc("POST /api/sessions/{id}/heartbeat", s.handleHeartbeat)
	apiMux.HandleFunc("GET /api/sessions", s.handleListSessions)
	apiMux.HandleFunc("GET /api/sessions/{id}", s.handleGetManagedSession)
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

	// Browse endpoints
	apiMux.HandleFunc("GET /api/browse", s.handleBrowse)
	apiMux.HandleFunc("GET /api/browse/search", s.handleBrowseSearch)

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
	apiMux.HandleFunc("POST /api/sessions/{id}/upload", s.handleUploadImage)
	apiMux.HandleFunc("POST /api/sessions/{id}/clear", s.handleClearSession)
	apiMux.HandleFunc("POST /api/sessions/{id}/hook-event", s.handleHookEvent)
	apiMux.HandleFunc("POST /api/sessions/{id}/permission-request", s.handlePermissionRequest)
	apiMux.HandleFunc("POST /api/sessions/{id}/permission-respond", s.handlePermissionRespond)
	apiMux.HandleFunc("GET /api/sessions/{id}/pending-permission", s.handlePendingPermission)
	apiMux.HandleFunc("POST /api/sessions/{id}/question-respond", s.handleQuestionRespond)
	apiMux.HandleFunc("GET /api/sessions/{id}/pending-question", s.handlePendingQuestion)
	apiMux.HandleFunc("POST /api/sessions/{id}/question-dismiss", s.handleDismissQuestion)
	apiMux.HandleFunc("GET /api/sessions/{id}/commands", s.handleListCommands)
	apiMux.HandleFunc("GET /api/sessions/{id}/commands/content", s.handleCommandContent)

	// File browser endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/files", s.handleListSessionFiles)
	apiMux.HandleFunc("GET /api/sessions/{id}/filetree", s.handleFileTree)
	apiMux.HandleFunc("GET /api/files/content", s.handleGetFileContent)
	apiMux.HandleFunc("GET /api/files/diff", s.handleFileDiff)

	// Skills endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/skills", s.handleListSkills)
	apiMux.HandleFunc("GET /api/skill-trust", s.handleListSkillTrust)
	apiMux.HandleFunc("POST /api/skill-trust", s.handleTrustSkill)
	apiMux.HandleFunc("DELETE /api/skill-trust/{name}", s.handleRevokeSkillTrust)

	// GitHub endpoints
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues", s.handleListGithubIssues)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/linked", s.handleBatchLinkedPRs)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/{number}", s.handleGetGithubIssue)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/{number}/comments", s.handleListIssueComments)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/issues/{number}/linked-prs", s.handleListIssueLinkedPRs)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/pulls", s.handleListGithubPulls)
	apiMux.HandleFunc("GET /api/sessions/{id}/github/pulls/{number}", s.handleGetGithubPull)

	// Settings endpoints
	apiMux.HandleFunc("GET /api/settings/exists", s.handleSettingsExists)
	apiMux.HandleFunc("GET /api/settings", s.handleGetSettings)
	apiMux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	// Usage endpoint
	apiMux.HandleFunc("GET /api/usage", s.handleUsage)

	// Cost summary endpoint
	apiMux.HandleFunc("GET /api/cost-summary", s.handleCostSummary)

	// Server management
	apiMux.HandleFunc("POST /api/restart", s.handleRestart)

	// Scheduled task endpoints
	apiMux.HandleFunc("POST /api/tasks", s.handleCreateTask)
	apiMux.HandleFunc("GET /api/tasks", s.handleListTasks)
	apiMux.HandleFunc("GET /api/tasks/{taskId}", s.handleGetTask)
	apiMux.HandleFunc("PUT /api/tasks/{taskId}", s.handleUpdateTask)
	apiMux.HandleFunc("DELETE /api/tasks/{taskId}", s.handleDeleteTask)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs", s.handleListTaskRuns)
	apiMux.HandleFunc("GET /api/tasks/{taskId}/runs/{runId}", s.handleGetTaskRun)
	apiMux.HandleFunc("POST /api/tasks/{taskId}/trigger", s.handleTriggerTask)

	// Workflow CRUD endpoints
	apiMux.HandleFunc("POST /api/workflows", s.handleCreateWorkflow)
	apiMux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	apiMux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	apiMux.HandleFunc("PUT /api/workflows/{id}", s.handleUpdateWorkflow)
	apiMux.HandleFunc("DELETE /api/workflows/{id}", s.handleDeleteWorkflow)

	// Workflow execution endpoints
	apiMux.HandleFunc("POST /api/workflows/{id}/run", s.handleStartWorkflowRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/pause", s.handlePauseRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/resume", s.handleResumeRun)
	apiMux.HandleFunc("POST /api/workflow-runs/{id}/cancel", s.handleCancelRun)
	apiMux.HandleFunc("GET /api/workflow-runs", s.handleListWorkflowRuns)
	apiMux.HandleFunc("GET /api/workflow-runs/{id}", s.handleGetWorkflowRun)

	rl := NewRateLimiter(180, 10)
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

	// Workflow run SSE stream — handles its own auth via query param
	root.HandleFunc("GET /api/workflow-runs/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		s.handleWorkflowRunStream(w, r, apiKey)
	})

	// Raw file endpoint — handles its own auth via query param
	// (HTML <video>/<audio>/<img> tags can't send Authorization headers)
	root.HandleFunc("GET /api/files/raw", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" || key != apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.handleGetFileRaw(w, r)
	})

	// All other /api/ routes — through auth middleware
	root.Handle("/api/", authedAPI)

	// Health check — no auth required, used by Claude to verify server is alive
	root.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "server_id": s.serverID})
	})

	// Visual file server — no auth required
	root.HandleFunc("/visual/", s.handleVisual)

	// Static files — no auth required
	root.Handle("/", web.Handler())

	return RecoveryMiddleware(root)
}

// RecoveryMiddleware catches panics in HTTP handlers, logs the stack trace,
// and returns a 500 response instead of crashing the server process.
// Go's net/http already recovers handler panics, but this middleware provides
// structured logging with full stack traces for debugging.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				stack := debug.Stack()
				log.Printf("PANIC recovered in %s %s: %v\n%s", r.Method, r.URL.Path, err, stack)
				if !headerWritten(w) {
					http.Error(w, fmt.Sprintf("Internal Server Error: %v", err), http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// headerWritten is a best-effort check; ResponseWriter doesn't expose this directly.
func headerWritten(w http.ResponseWriter) bool {
	// If Content-Type is already set, headers were likely written
	return w.Header().Get("Content-Type") != ""
}
