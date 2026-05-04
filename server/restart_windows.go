//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
)

func execRestart(exe string, args []string, _ []string) {
	cmd := exec.Command(exe, args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("restart failed: %v — exiting", err)
	}
	os.Exit(0)
}
