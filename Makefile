# MeterBase monorepo — root orchestration.
# Cross-language tasks live here; the Go app has its own Makefile in apps/api,
# and the JS workspace is driven by pnpm + turbo.

.DEFAULT_GOAL := help

# --- Full stack (Docker) ---------------------------------------------------
.PHONY: up
up: ## Build & run the whole stack (db + api + web) in Docker
	docker compose up -d --build

.PHONY: up-db
up-db: ## Start only the database (for host-based dev), wait until healthy
	docker compose up -d --wait db

.PHONY: down
down: ## Stop the stack
	docker compose down

.PHONY: db-reset
db-reset: ## Wipe the database volume and restart it
	docker compose down -v
	$(MAKE) up-db

# --- Host development (hot reload) -----------------------------------------
.PHONY: install
install: ## Install JS deps (pnpm) and Go tools
	pnpm install
	$(MAKE) -C apps/api tools

.PHONY: migrate
migrate: ## Apply database migrations
	$(MAKE) -C apps/api migrate

.PHONY: dev-api
dev-api: ## Run the Go API with hot reload (run `make up-db` first)
	$(MAKE) -C apps/api server

.PHONY: dev-web
dev-web: ## Run the Next.js dev server
	pnpm --filter web dev

# --- Build / quality (both apps) -------------------------------------------
.PHONY: build
build: ## Build api binary and web app
	$(MAKE) -C apps/api build
	pnpm --filter web build

.PHONY: test
test: ## Run api tests and web typecheck
	$(MAKE) -C apps/api test
	pnpm --filter web typecheck

.PHONY: lint
lint: ## Lint both apps
	$(MAKE) -C apps/api lint
	pnpm --filter web lint

# --- Contract / SDK --------------------------------------------------------
.PHONY: gen-sdk
gen-sdk: ## Generate the TS client types from the OpenAPI contract
	pnpm gen:sdk

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
