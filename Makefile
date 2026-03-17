# Claude Controller Makefile

PORT ?= 8080
SERVER_BIN := server/claude-controller

.PHONY: help build test run run-docker stop-docker hooks xcode clean all

.DEFAULT_GOAL := help

##@ General

help: ## Show this help message
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Server

build: ## Build the Go server binary
	cd server && go build -o claude-controller .

test: ## Run all server tests
	cd server && go test ./... -v

test-db: ## Run database layer tests only
	cd server && go test ./db/ -v

test-api: ## Run API handler tests only
	cd server && go test ./api/ -v

run: build ## Build and run the server locally
	./$(SERVER_BIN) --port $(PORT)

##@ Docker

run-docker: ## Build and run in Docker (set NGROK_AUTHTOKEN for tunnel)
	PORT=$(PORT) docker compose up --build

run-docker-bg: ## Build and run in Docker (background)
	PORT=$(PORT) docker compose up --build -d

stop-docker: ## Stop Docker containers
	docker compose down

##@ iOS

xcode: ## Open the iOS app in Xcode
	@if [ -d ios/ClaudeController.xcodeproj ]; then \
		open ios/ClaudeController.xcodeproj; \
	else \
		open -a Xcode ios/ClaudeController/; \
	fi

##@ Hooks

hooks: ## Install hooks into Claude Code settings
	./hooks/install.sh

##@ Cleanup

clean: ## Remove build artifacts and Docker volumes
	rm -f $(SERVER_BIN)
	docker compose down -v 2>/dev/null || true

all: build
