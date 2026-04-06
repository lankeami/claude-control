package api

import (
	"time"

	"github.com/jaychinthrajah/claude-controller/server/managed"
)

// SessionManager abstracts the managed.Manager for testability.
// The real *managed.Manager satisfies this interface.
type SessionManager interface {
	EnsureProcess(sessionID string, opts managed.SpawnOpts) (*managed.Process, error)
	SendTurn(sessionID string, messageJSON string) error
	Interrupt(sessionID string) error
	IsRunning(sessionID string) bool
	GetBroadcaster(sessionID string) *managed.Broadcaster
	GracefulShutdown(sessionID string, timeout time.Duration) error
	RunCompact(sessionID string, resumeID string, cwd string, timeout time.Duration) error
	Teardown(sessionID string, timeout time.Duration) error
	SpawnShell(sessionID string, opts managed.ShellOpts) (*managed.Process, error)
	Config() managed.Config
	UpdateConfig(cfg managed.Config)
}
