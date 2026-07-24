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
	"runtime"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/jaychinthrajah/claude-controller/server/api"
	"github.com/jaychinthrajah/claude-controller/server/db"
	"github.com/jaychinthrajah/claude-controller/server/hooksignal"
	"github.com/jaychinthrajah/claude-controller/server/instance"
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

	if len(os.Args) >= 2 && os.Args[1] == "hook-signal" {
		fs := flag.NewFlagSet("hook-signal", flag.ExitOnError)
		event := fs.String("event", "", "hook event name (session_start|stop|notification)")
		sessionID := fs.String("session-id", "", "managed session ID")
		hookPort := fs.Int("port", 8080, "server port")
		keyFile := fs.String("key-file", "", "path to api.key (default ~/.claude-controller/api.key)")
		fs.Parse(os.Args[2:])
		// Errors are intentionally swallowed: a broken relay must never
		// block Claude from finishing its turn.
		if err := hooksignal.Run(*event, *sessionID, *hookPort, *keyFile, os.Stdin); err != nil {
			log.Printf("hook-signal: %v", err)
		}
		return
	}

	port := flag.Int("port", 0, "port to listen on (default: 8080, auto-detect if occupied)")
	dbPath := flag.String("db", "", "path to SQLite database (default: ~/.claude-controller/{instance}/claude.db)")
	managedModeFlag := flag.String("managed-mode", "", "managed session backend: interactive or print (default: MANAGED_MODE env, then interactive)")
	instanceName := flag.String("instance", "default", "instance name (default: default)")
	flag.Parse()

	instReg, err := instance.New()
	if err != nil {
		log.Fatalf("Failed to initialize instance registry: %v", err)
	}

	inst, err := instReg.Get(*instanceName)
	if err != nil {
		log.Fatalf("Instance %q not found: %v", *instanceName, err)
	}

	if *port == 0 {
		if p := os.Getenv("PORT"); p != "" {
			v, err := strconv.Atoi(p)
			if err == nil {
				*port = v
			}
		}
		if *port == 0 {
			preferred := inst.Port
			if preferred == 0 {
				preferred = 8080
			}
			*port = findAvailablePort(preferred)
		}
	}

	if *dbPath == "" {
		var err error
		*dbPath, err = instance.DBPath(*instanceName)
		if err != nil {
			log.Fatalf("Failed to determine database path: %v", err)
		}
		dir := filepath.Dir(*dbPath)
		os.MkdirAll(dir, 0755)
		migrated, err := instance.MigrateLegacy(*instanceName)
		if err != nil {
			log.Printf("Warning: failed to migrate legacy database: %v", err)
		} else if migrated {
			log.Printf("Migrated legacy database and API key from ~/.claude-controller/ to %s", dir)
		}
	}

	store, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err := store.ResetStaleActivityStates(); err != nil {
		log.Printf("Warning: failed to reset stale activity states: %v", err)
	}

	apiKey := loadOrCreateAPIKey(*dbPath)

	sched := scheduler.New(store)
	sched.SetLoopback(fmt.Sprintf("http://localhost:%d", *port), apiKey)
	sched.Reconcile()
	sched.Start()

	envPath, err := instance.EnvPath(*instanceName)
	if err == nil {
		loadDotEnv(envPath)
	}
	binaryPath, _ := os.Executable()
	managedCfg := managed.Config{
		ClaudeBin:   envOrDefault("CLAUDE_BIN", "claude"),
		ClaudeArgs:  strings.Fields(os.Getenv("CLAUDE_ARGS")),
		ClaudeEnv:   splitEnv(os.Getenv("CLAUDE_ENV")),
		ServerPort:  *port,
		BinaryPath:  binaryPath,
		KeyFilePath: filepath.Join(filepath.Dir(*dbPath), "api.key"),
		Mode:        managedMode(*managedModeFlag),
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
	router := api.NewRouter(store, apiKey, mgr, envPath, shutdownFunc, serverID, sched.Trigger, *instanceName)

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
			log.Printf("Build failed, restarting with previous binary:\n%s", buildOut.String())
		} else {
			log.Printf("Build succeeded, restarting %s %v", exe, os.Args)
		}
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

// managedMode picks the managed-session backend. "interactive" (default)
// drives a long-lived interactive Claude Code process billed against the
// subscription; "print" is the legacy claude -p path kept for one release as
// a rollback. Precedence: --managed-mode flag > MANAGED_MODE env (incl. .env)
// > "interactive". Windows has no ConPTY support yet, so it always uses
// print mode.
func managedMode(flagValue string) string {
	mode := flagValue
	if mode == "" {
		mode = envOrDefault("MANAGED_MODE", "interactive")
	}
	if runtime.GOOS == "windows" && mode == "interactive" {
		log.Printf("interactive managed mode is not supported on Windows yet; falling back to print mode")
		return "print"
	}
	if mode != "interactive" && mode != "print" {
		log.Printf("unknown MANAGED_MODE %q, defaulting to interactive", mode)
		return "interactive"
	}
	return mode
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
