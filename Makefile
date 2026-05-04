# Claude Controller Makefile

-include .env

PORT ?= 9999
CLAUDE_DIR ?= ~/.claude

# Platform detection — sets EXE suffix, delete command, and null redirect
ifeq ($(OS),Windows_NT)
  EXE      := .exe
  RM_F     := del /f /q
  DEVNULL  := 2>NUL
  OPEN_CMD := start
else
  EXE      :=
  RM_F     := rm -f
  DEVNULL  := 2>/dev/null
  OPEN_CMD := open
endif

SERVER_BIN := server/claude-controller$(EXE)

.PHONY: help build build-windows test run stop open local run-docker run-docker-bg stop-docker logs ngrok hooks clean all

.DEFAULT_GOAL := help

##@ General

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Server

build: ## Build the Go server binary
	cd server && go build -o claude-controller$(EXE) .

build-windows: ## Build the Go server binary for Windows (amd64)
ifeq ($(OS),Windows_NT)
	cd server && go build -o claude-controller.exe .
else
	cd server && GOOS=windows GOARCH=amd64 go build -o claude-controller.exe .
endif

test: ## Run all server tests
	cd server && go test ./... -v

test-db: ## Run database layer tests only
	cd server && go test ./db/ -v

test-api: ## Run API handler tests only
	cd server && go test ./api/ -v

run: build ## Build and run the server locally (web UI at http://localhost:PORT)
	$(SERVER_BIN) --port $(PORT)

local: ## Stop everything, rebuild, and start fresh
ifeq ($(OS),Windows_NT)
	-taskkill /F /IM claude-controller.exe $(DEVNULL)
	-$(RM_F) $(SERVER_BIN) $(DEVNULL)
else
	-pkill -f 'claude-controller' $(DEVNULL) || true
	-docker compose down $(DEVNULL) || true
	$(RM_F) $(SERVER_BIN)
endif
	cd server && go build -o claude-controller$(EXE) .
	$(SERVER_BIN) --port $(PORT)

stop: ## Stop the running Go server process
ifeq ($(OS),Windows_NT)
	@taskkill /F /IM claude-controller.exe $(DEVNULL) && echo Server stopped. || echo No server process found.
else
	@pkill -f 'claude-controller' $(DEVNULL) && echo "Server stopped." || echo "No server process found."
endif

open: ## Open the web UI in default browser
	$(OPEN_CMD) http://localhost:$(PORT)

##@ Docker

run-docker: ## Build and run in Docker (set NGROK_AUTHTOKEN for tunnel)
ifeq ($(OS),Windows_NT)
	cmd /c "set PORT=$(PORT) && set NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) && docker compose up --build"
else
	PORT=$(PORT) NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) docker compose up --build
endif

run-docker-bg: ## Build and run in Docker (background)
ifeq ($(OS),Windows_NT)
	cmd /c "set PORT=$(PORT) && set NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) && docker compose up --build -d"
else
	PORT=$(PORT) NGROK_AUTHTOKEN=$(NGROK_AUTHTOKEN) docker compose up --build -d
endif

stop-docker: ## Stop Docker containers
	docker compose down

logs: ## Tail Docker container logs
	docker compose logs -f

##@ Tunnel

ngrok: ## Start an ngrok tunnel to localhost:PORT
	ngrok http $(PORT)

##@ Hooks

hooks: ## Install hooks into Claude Code settings
ifeq ($(OS),Windows_NT)
	pwsh -NonInteractive -File hooks\install.ps1
else
	CLAUDE_DIR=$(CLAUDE_DIR) ./hooks/install.sh
endif

##@ Cleanup

clean: ## Remove build artifacts and Docker volumes
ifeq ($(OS),Windows_NT)
	-$(RM_F) $(SERVER_BIN) $(DEVNULL)
else
	$(RM_F) $(SERVER_BIN)
	docker compose down -v $(DEVNULL) || true
endif

all: build
