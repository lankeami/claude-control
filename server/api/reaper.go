package api

import (
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// WireReaper connects the idle reaper to the database layer (issue #314):
//
//   - a background-activity probe that maps a controller session to its
//     Claude per-session directory and reports the newest workflow/subagent
//     transcript or task-output mtime, so sessions with in-flight background
//     work are not reaped; and
//   - a reap callback that records every reap as a durable `system` message
//     row in that session's history, naming the session and idle duration.
//
// The managed package stays free of DB imports; main.go calls this before
// Manager.StartReaper.
func WireReaper(mgr *managed.Manager, store *db.Store) {
	mgr.SetBackgroundActivityFn(func(sessionID string) time.Time {
		sess, err := store.GetSession(sessionID)
		if err != nil || sess == nil || sess.ClaudeSessionID == "" {
			return time.Time{}
		}
		projDir, err := claudeProjectsDir(sess.CWD)
		if err != nil {
			return time.Time{}
		}
		return managed.LatestBackgroundActivity(filepath.Join(projDir, sess.ClaudeSessionID))
	})

	mgr.SetReapCallback(func(sessionID string, idleFor time.Duration) {
		content := fmt.Sprintf("Session %s reaped after %s idle (no turn or background activity within the idle window)",
			sessionID, idleFor.Round(time.Second))
		if _, err := store.CreateMessage(sessionID, "system", content, 0); err != nil {
			log.Printf("failed to record reap for session %s: %v", sessionID, err)
		}
	})
}
