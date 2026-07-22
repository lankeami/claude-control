package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

const costCacheTTL = 15 * time.Second

type costSummaryCache struct {
	data      map[string]interface{}
	timestamp time.Time
}

func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request, apiKey string) {
	// Auth via query param (EventSource can't send headers)
	token := r.URL.Query().Get("token")
	if token == "" || token != apiKey {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Send immediately, then every tick
	s.sendSSEState(w, flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			s.sendSSEState(w, flusher)
		}
	}
}

// cachedCostSummary returns the cost summary, recomputing at most once per costCacheTTL.
func (s *Server) cachedCostSummary() map[string]interface{} {
	s.costCacheMu.RLock()
	if s.costCache != nil && time.Since(s.costCache.timestamp) < costCacheTTL {
		data := s.costCache.data
		s.costCacheMu.RUnlock()
		return data
	}
	s.costCacheMu.RUnlock()

	now := time.Now()
	fiveHrStart, fiveHrEnd := db.FiveHourWindow(now)
	sevenDayStart, sevenDayEnd := db.SevenDayWindow(now)
	fiveHrLimit, sevenDayLimit := s.usageLimits()
	fiveHr, err := s.aggregateCosts(fiveHrStart, fiveHrEnd, fiveHrLimit)
	if err != nil {
		return nil
	}
	sevenDay, err := s.aggregateCosts(sevenDayStart, sevenDayEnd, sevenDayLimit)
	if err != nil {
		return nil
	}
	result := map[string]interface{}{
		"five_hour": fiveHr,
		"seven_day": sevenDay,
	}

	s.costCacheMu.Lock()
	s.costCache = &costSummaryCache{data: result, timestamp: now}
	s.costCacheMu.Unlock()

	return result
}

func (s *Server) sendSSEState(w http.ResponseWriter, flusher http.Flusher) {
	sessions, _ := s.store.ListSessions(false)
	prompts, _ := s.store.ListPrompts("", "")

	if sessions == nil {
		sessions = []db.Session{}
	}
	if prompts == nil {
		prompts = []db.Prompt{}
	}

	payload := map[string]interface{}{
		"sessions": sessions,
		"prompts":  prompts,
	}

	if cs := s.cachedCostSummary(); cs != nil {
		payload["cost_summary"] = cs
	}

	data, _ := json.Marshal(payload)

	fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
	flusher.Flush()
}
