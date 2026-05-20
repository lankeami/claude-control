package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
)

// CostSummary represents the cost breakdown for a time window
type CostSummary struct {
	TotalCost float64              `json:"total_cost"`
	Sessions  []SessionCostDetail  `json:"sessions"`
}

// SessionCostDetail represents cost for a single session with per-model breakdown
type SessionCostDetail struct {
	SessionID string             `json:"id"`
	TotalCost float64            `json:"total_cost"`
	ByModel   map[string]float64 `json:"by_model"`
}

// aggregateCosts sums costs from messages in the given time window, grouped by session + model
func (s *Server) aggregateCosts(windowStart, windowEnd time.Time) (*CostSummary, error) {
	query := `
	SELECT COALESCE(m.session_id, '') as session_id, COALESCE(ms.model, '') as model, SUM(COALESCE(m.cost, 0)) as total
	FROM messages m
	LEFT JOIN sessions ms ON m.session_id = ms.id
	WHERE m.created_at >= ? AND m.created_at < ? AND m.cost > 0
	GROUP BY m.session_id, ms.model
	`

	rows, err := s.store.QueryRows(query, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := &CostSummary{
		Sessions: make([]SessionCostDetail, 0),
	}
	sessionMap := make(map[string]*SessionCostDetail)

	for rows.Next() {
		var sessionID, model string
		var cost float64
		if err := rows.Scan(&sessionID, &model, &cost); err != nil {
			return nil, err
		}

		if _, exists := sessionMap[sessionID]; !exists {
			sessionMap[sessionID] = &SessionCostDetail{
				SessionID: sessionID,
				ByModel:   make(map[string]float64),
			}
		}

		detail := sessionMap[sessionID]
		detail.ByModel[model] = cost
		detail.TotalCost += cost
		summary.TotalCost += cost
	}

	for _, detail := range sessionMap {
		summary.Sessions = append(summary.Sessions, *detail)
	}

	return summary, nil
}

func (s *Server) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	// Extract and validate sessionId
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"missing_session"}`))
		return
	}

	// Verify session exists
	sess, err := s.store.GetSession(sessionID)
	if err != nil || sess == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_session"}`))
		return
	}

	// Calculate window boundaries
	now := time.Now()
	fiveHrStart, fiveHrEnd := db.FiveHourWindow(now)
	sevenDayStart, sevenDayEnd := db.SevenDayWindow(now)

	// Aggregate costs for each window
	fiveHrSummary, _ := s.aggregateCosts(fiveHrStart, fiveHrEnd)
	sevenDaySummary, _ := s.aggregateCosts(sevenDayStart, sevenDayEnd)

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, max-age=10")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"five_hour": fiveHrSummary,
		"seven_day": sevenDaySummary,
	})
}
