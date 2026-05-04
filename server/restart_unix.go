//go:build !windows

package main

import (
	"log"
	"os"
	"syscall"
)

func execRestart(exe string, args []string, env []string) {
	if err := syscall.Exec(exe, args, env); err != nil {
		log.Printf("syscall.Exec failed: %v — exiting for wrapper to restart", err)
		os.Exit(0)
	}
}
