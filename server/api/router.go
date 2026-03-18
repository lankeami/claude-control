package api

import (
	"net/http"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

type Server struct {
	store *db.Store
}

func NewRouter(store *db.Store, apiKey string) http.Handler {
	s := &Server{store: store}
	mux := http.NewServeMux()

	// Session endpoints
	mux.HandleFunc("POST /api/sessions/register", s.handleRegisterSession)
	mux.HandleFunc("POST /api/sessions/{id}/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("PUT /api/sessions/{id}/archive", s.handleSetArchived)

	// Prompt endpoints
	mux.HandleFunc("POST /api/prompts", s.handleCreatePrompt)
	mux.HandleFunc("GET /api/prompts/{id}/response", s.handleGetPromptResponse)
	mux.HandleFunc("POST /api/prompts/{id}/respond", s.handleRespondToPrompt)
	mux.HandleFunc("GET /api/prompts", s.handleListPrompts)

	// Instruction endpoints
	mux.HandleFunc("POST /api/sessions/{id}/instruct", s.handleInstruct)
	mux.HandleFunc("GET /api/sessions/{id}/instructions", s.handleFetchInstructions)

	// Pairing/status endpoints
	mux.HandleFunc("GET /api/pairing", s.handlePairing)
	mux.HandleFunc("GET /api/status", s.handleStatus)

	rl := NewRateLimiter(60, 10)
	authed := rl.Middleware(AuthMiddleware(apiKey, rl, mux))

	// Top-level mux: SSE route authenticates via query param (EventSource can't send headers)
	top := http.NewServeMux()
	top.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		s.handleSSEEvents(w, r, apiKey)
	})
	top.Handle("/", authed)
	return top
}
