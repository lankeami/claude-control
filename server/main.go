package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/jaychinthrajah/claude-controller/server/api"
	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/managed"
	"github.com/jaychinthrajah/claude-controller/server/mcp"
	"github.com/jaychinthrajah/claude-controller/server/scheduler"
	"github.com/jaychinthrajah/claude-controller/server/tunnel"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "mcp-bridge" {
		bridgeFlags := flag.NewFlagSet("mcp-bridge", flag.ExitOnError)
		sessionID := bridgeFlags.String("session-id", "", "session ID")
		port := bridgeFlags.Int("port", 8080, "server port")
		bridgeFlags.Parse(os.Args[2:])
		if *sessionID == "" {
			log.Fatal("--session-id is required")
		}
		if err := mcp.Run(*sessionID, *port); err != nil {
			log.Fatalf("mcp-bridge error: %v", err)
		}
		return
	}

	port := flag.Int("port", 0, "port to listen on (default: 8080, auto-detect if occupied)")
	dbPath := flag.String("db", "", "path to SQLite database (default: ~/.claude-controller/data.db)")
	flag.Parse()

	if *port == 0 {
		if p := os.Getenv("PORT"); p != "" {
			v, err := strconv.Atoi(p)
			if err == nil {
				*port = v
			}
		}
		if *port == 0 {
			*port = findAvailablePort(8080)
		}
	}

	if *dbPath == "" {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".claude-controller")
		os.MkdirAll(dir, 0755)
		*dbPath = filepath.Join(dir, "data.db")
	}

	store, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err := store.ResetStaleActivityStates(); err != nil {
		log.Printf("Warning: failed to reset stale activity states: %v", err)
	}

	sched := scheduler.New(store)
	sched.Reconcile()
	sched.Start()

	apiKey := loadOrCreateAPIKey(*dbPath)

	loadDotEnv(".env")
	envPath, _ := filepath.Abs(".env")
	binaryPath, _ := os.Executable()
	managedCfg := managed.Config{
		ClaudeBin:  envOrDefault("CLAUDE_BIN", "claude"),
		ClaudeArgs: strings.Fields(os.Getenv("CLAUDE_ARGS")),
		ClaudeEnv:  splitEnv(os.Getenv("CLAUDE_ENV")),
		ServerPort: *port,
		BinaryPath: binaryPath,
	}
	mgr := managed.NewManager(managedCfg)
	mgr.StartReaper()

	restartCh := make(chan struct{}, 1)
	shutdownFunc := func() {
		select {
		case restartCh <- struct{}{}:
		default:
		}
	}

	serverID := fmt.Sprintf("%d", time.Now().UnixNano())
	router := api.NewRouter(store, apiKey, mgr, envPath, shutdownFunc, serverID)

	// Start local server
	bindHost := "localhost"
	if h := os.Getenv("BIND_HOST"); h != "" {
		bindHost = h
	}
	addr := fmt.Sprintf("%s:%d", bindHost, *port)
	localListener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	fmt.Printf("Local server listening on %s\n", addr)
	fmt.Printf("API key: %s\n", apiKey)

	// Start serving HTTP with http.Server for graceful shutdown
	httpServer := &http.Server{Handler: router}
	go httpServer.Serve(localListener)

	// Start ngrok tunnel in a goroutine so it doesn't block signal handling
	ctx, cancel := context.WithCancel(context.Background())

	var ngrokServer *http.Server
	var tun *tunnel.Tunnel
	ngrokDone := make(chan struct{})
	go func() {
		defer close(ngrokDone)
		if os.Getenv("NGROK_AUTHTOKEN") == "" {
			log.Printf("Server is running locally only at http://%s", addr)
			log.Printf("To expose via ngrok, set NGROK_AUTHTOKEN environment variable")
			return
		}
		var err error
		tun, err = tunnel.Start(ctx)
		if err != nil {
			log.Printf("Warning: ngrok tunnel failed: %v", err)
			log.Printf("Server is running locally only at http://%s", addr)
			return
		}
		ngrokURL := tun.URL()
		if !strings.HasPrefix(ngrokURL, "https://") {
			ngrokURL = "https://" + ngrokURL
		}
		fmt.Printf("ngrok tunnel: %s\n", ngrokURL)
		displayQRCode(ngrokURL, apiKey)

		ngrokServer = &http.Server{Handler: router}
		go ngrokServer.Serve(tun.Listener())
	}()

	// Handle shutdown via signal or restart request
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	restartRequested := false
	select {
	case <-sigCh:
		fmt.Println("\nShutting down...")
	case <-restartCh:
		fmt.Println("\nRestarting server...")
		restartRequested = true
	}

	// Graceful shutdown: stop HTTP first so clients see the server as down
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	httpServer.Shutdown(shutdownCtx)
	if ngrokServer != nil {
		ngrokServer.Shutdown(shutdownCtx)
	}
	shutdownCancel()
	cancel() // cancel ngrok context early so tunnel.Start unblocks if still running
	<-ngrokDone
	if tun != nil {
		tun.Close()
	}

	// Then shut down background services
	mgr.ShutdownAll(5 * time.Second)
	sched.Stop()
	store.Close()

	if restartRequested {
		exe, err := os.Executable()
		if err != nil {
			log.Printf("Failed to find executable path: %v", err)
			os.Exit(0)
		}
		log.Printf("Building %s...", exe)
		buildCmd := exec.Command("go", "build", "-o", exe, ".")
		buildCmd.Dir = filepath.Dir(exe)
		var buildOut bytes.Buffer
		buildCmd.Stdout = &buildOut
		buildCmd.Stderr = &buildOut
		if err := buildCmd.Run(); err != nil {
			log.Printf("Build failed, not restarting:\n%s", buildOut.String())
			os.Exit(1)
		}
		log.Printf("Build succeeded, restarting %s %v", exe, os.Args)
		execRestart(exe, os.Args, os.Environ())
	}
}

func findAvailablePort(preferred int) int {
	l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", preferred))
	if err == nil {
		l.Close()
		return preferred
	}
	// Find random available port
	l, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		return preferred
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func loadOrCreateAPIKey(dbPath string) string {
	keyFile := filepath.Join(filepath.Dir(dbPath), "api.key")
	data, err := os.ReadFile(keyFile)
	if err == nil && len(data) > 0 {
		return string(data)
	}

	key := generateAPIKey()
	os.WriteFile(keyFile, []byte(key), 0600)
	return key
}

func generateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "sk-" + hex.EncodeToString(b)
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			os.Setenv(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitEnv(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func displayQRCode(ngrokURL, apiKey string) {
	payload := map[string]interface{}{
		"url":     ngrokURL,
		"key":     apiKey,
		"version": 1,
	}
	jsonData, _ := json.Marshal(payload)

	qr, err := qrcode.New(string(jsonData), qrcode.Medium)
	if err != nil {
		log.Printf("Failed to generate QR code: %v", err)
		fmt.Printf("\nPairing payload: %s\n", jsonData)
		return
	}

	fmt.Println("\n--- Scan this QR code with the Claude Controller iOS app ---")
	fmt.Println(qr.ToSmallString(false))
	fmt.Printf("Pairing payload: %s\n\n", jsonData)
}
