package api

import (
	"log"
	"runtime/debug"
)

// SafeGo runs fn in a new goroutine with panic recovery.
// If the goroutine panics, the panic is logged with a full stack trace
// instead of crashing the server process.
func SafeGo(label string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC recovered in goroutine %q: %v\n%s", label, r, debug.Stack())
			}
		}()
		fn()
	}()
}
