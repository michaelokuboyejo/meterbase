# MeterBase API (Go) — Makefile
# Run from apps/api. Start the database first with the ROOT `make up-db`.
# Some targets need Phase 0 code (cmd/meterbase) to exist first.

# --- Config (override via environment) -------------------------------------
DATABASE_URL   ?= postgres://meterbase:meterbase@localhost:5432/meterbase?sslmode=disable
MIGRATIONS_DIR ?= ./migrations
BIN            ?= bin/meterbase
PKG            ?= ./cmd/meterbase

# Tools (installed on demand where possible)
GOOSE          ?= goose
GOLANGCI_LINT  ?= golangci-lint

.DEFAULT_GOAL := help

# --- Migrations ------------------------------------------------------------
.PHONY: migrate
migrate: ## Apply all up migrations
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

.PHONY: server
server: ## Run the API server (hot reload via air if installed, else go run)
	@if command -v air >/dev/null 2>&1; then \
		air; \
	else \
		echo "air not found; running without hot reload (go install github.com/air-verse/air@latest to enable)"; \
		go run $(PKG); \
	fi

# --- Build / quality -------------------------------------------------------
.PHONY: build
build: ## Compile the binary to $(BIN)
	go build -o $(BIN) $(PKG)

.PHONY: test
test: ## Run all tests with the race detector
	go test ./... -race -count=1

.PHONY: lint
lint: ## Run linters (installs golangci-lint hint if missing)
	@if command -v $(GOLANGCI_LINT) >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run; \
	else \
		echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: tools
tools: ## Install dev tools (goose, air)
	go install github.com/pressly/goose/v3/cmd/goose@latest
	go install github.com/air-verse/air@latest

# --- Meta ------------------------------------------------------------------
.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
