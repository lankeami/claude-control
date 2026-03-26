package main

import (
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
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

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
	defer store.Close()

	if err := store.ResetStaleActivityStates(); err != nil {
		log.Printf("Warning: failed to reset stale activity states: %v", err)
	}

	sched := scheduler.New(store)
	sched.Reconcile()
	sched.Start()
	defer sched.Stop()

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

	router := api.NewRouter(store, apiKey, mgr, envPath)

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

	// Start serving HTTP immediately
	go http.Serve(localListener, router)

	// Start ngrok tunnel (may block if no auth token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun, err := tunnel.Start(ctx)
	if err != nil {
		log.Printf("Warning: ngrok tunnel failed: %v", err)
		log.Printf("Server is running locally only at http://%s", addr)
		log.Printf("To expose via ngrok, set NGROK_AUTHTOKEN environment variable")
	} else {
		defer tun.Close()
		// ngrok-go's Addr().String() may or may not include scheme
		ngrokURL := tun.URL()
		if !strings.HasPrefix(ngrokURL, "https://") {
			ngrokURL = "https://" + ngrokURL
		}
		fmt.Printf("ngrok tunnel: %s\n", ngrokURL)
		displayQRCode(ngrokURL, apiKey)

		// Serve on ngrok listener too
		go http.Serve(tun.Listener(), router)
	}

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh
	fmt.Println("\nShutting down...")
	cancel()
	localListener.Close()
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
