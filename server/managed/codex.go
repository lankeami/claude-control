package managed

import "fmt"

// ErrCodexNotImplemented is returned when a codex session is started.
// The codex backend will be implemented in a follow-up issue.
var ErrCodexNotImplemented = fmt.Errorf("codex agent backend not yet implemented")
