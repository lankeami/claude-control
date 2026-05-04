# Claude Controller Makefile

-include .env

PORT ?= 9999
CLAUDE_DIR ?= ~/.claude
SERVER_BIN := server/claude-controller

.PHONY: help build build-windows test run stop open local run-docker run-docker-bg stop-docker logs ngrok hooks clean all

.DEFAULT_GOAL := help

##@ General

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Server

build: ## Build the Go server binary
	cd server && go build -o claude-controller .

build-windows: ## Cross-compile the Go server binary for Windows (amd64)
	cd server && GOOS=windows GOARCH=amd64 go build -o claude-controller.exe .

test: ## Run all server tests
	cd server && go test ./... -v

test-db: ## Run database layer tests only
	cd server && go test ./db/ -v

test-api: ## Run API handler tests only
	cd server && go test ./api/ -v

run: build ## Build and run the server locally (web UI at http://localhost:PORT)
	./$(SERVER_BIN) --port $(PORT)

local: ## Stop everything, rebuild, and start fresh
	-@pkill -f 'claude-controller' 2>/dev/null || true
	-@docker compose down 2>/dev/null || true
	rm -f $(SERVER_BIN)
	cd server && go build -o claude-controller .
	./$(SERVER_BIN) --port $(PORT)

stop: ## Stop the running Go server process
	@pkill -f 'claude-controller' 2>/dev/null && echo "Server stopped." || echo "No server process found."

open: ## Open the web UI in default browser
	open http://localhost:$(PORT)

##@ Docker

run-docker: ## Build and run in Docker (set NGROK_AUTHTOKEN for tunnel)
	PORT=$(PORT) NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) docker compose up --build

run-docker-bg: ## Build and run in Docker (background)
	PORT=$(PORT) NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) docker compose up --build -d

stop-docker: ## Stop Docker containers
	docker compose down

logs: ## Tail Docker container logs
	docker compose logs -f

##@ Tunnel

ngrok: ## Start an ngrok tunnel to localhost:PORT
	ngrok http $(PORT)

##@ Hooks

hooks: ## Install hooks into Claude Code settings (uses CLAUDE_DIR from .env)
	CLAUDE_DIR=$(CLAUDE_DIR) ./hooks/install.sh

##@ Cleanup

clean: ## Remove build artifacts and Docker volumes
	rm -f $(SERVER_BIN)
	docker compose down -v 2>/dev/null || true

all: build
