package api

import (
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
	"github.com/jaychinthrajah/claude-controller/server/web"
)

type Server struct {
	store   *db.Store
	manager *managed.Manager
}

func NewRouter(store *db.Store, apiKey string, mgr *managed.Manager) http.Handler {
	s := &Server{store: store, manager: mgr}

	// API mux — all existing endpoints, behind auth middleware
	apiMux := http.NewServeMux()

	// Session endpoints
	apiMux.HandleFunc("POST /api/sessions/register", s.handleRegisterSession)
	apiMux.HandleFunc("POST /api/sessions/{id}/heartbeat", s.handleHeartbeat)
	apiMux.HandleFunc("GET /api/sessions", s.handleListSessions)
	apiMux.HandleFunc("PUT /api/sessions/{id}/archive", s.handleSetArchived)
	apiMux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)

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
	apiMux.HandleFunc("POST /api/sessions/{id}/message", s.handleSendMessage)
	apiMux.HandleFunc("POST /api/sessions/{id}/interrupt", s.handleInterrupt)
	apiMux.HandleFunc("GET /api/sessions/{id}/messages", s.handleListMessages)

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

	// Static files — no auth required
	root.Handle("/", web.Handler())

	return root
}
