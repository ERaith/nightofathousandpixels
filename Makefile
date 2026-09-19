# Night of a Thousand Pixels - developer entry point.
#
# Several agents share one laptop, so every target is safe to run concurrently
# with someone else's: ports, container names, networks and volumes are all
# derived from AGENT_SLOT. Slot 0 is the default (a solo developer); agents use
# their own slot number and never collide.
#
#   make setup      # clean clone -> running app
#   make dev        # the three watchers
#   AGENT_SLOT=1 make dev
#
# Tools (templ, air, goose, sqlc, golangci-lint) are pinned in go.mod's `tool`
# block and run via `go tool <name>`. Nothing here installs anything globally.

# Local configuration. A missing .env is fine - `make setup` creates one from
# .env.example. Production reads its environment from the deployment platform
# and never reads this file.
-include .env

# Export every variable defined here to recipes, so go, goose, sqlc and
# docker compose all see the same values without repeating them per command.
export

# ---------------------------------------------------------------------------
# Agent slot: one genuinely private stack per agent.
# ---------------------------------------------------------------------------
AGENT_SLOT ?= 0

# The compose project name is what buys the isolation: docker namespaces the
# containers, the network and the volumes under it, so slot 1 and slot 2 share
# nothing at all.
COMPOSE_PROJECT_NAME := nap-a$(AGENT_SLOT)

APP_PORT         := $(shell expr 8080 + $(AGENT_SLOT) '*' 10)
TEMPL_PROXY_PORT := $(shell expr 7331 + $(AGENT_SLOT) '*' 10)
POSTGRES_PORT    := $(shell expr 5433 + $(AGENT_SLOT) '*' 10)

# The server reads PORT. AGENT_SLOT is deliberately the only knob, so the slot
# wins over .env here; for a one-off use `make dev PORT=3000`, which as a
# command-line variable beats everything.
PORT := $(APP_PORT)

# ---------------------------------------------------------------------------
# Configuration (see internal/config/config.go for what the server requires)
# ---------------------------------------------------------------------------
POSTGRES_USER     ?= nap
POSTGRES_PASSWORD ?= nap
POSTGRES_DB       ?= nap
POSTGRES_SERVICE  ?= postgres

DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

# During `make dev` the browser talks to the templ proxy, not to the app
# directly, so the browser-facing origin is the proxy's.
ORIGIN ?= http://localhost:$(TEMPL_PROXY_PORT)

# ---------------------------------------------------------------------------
# Paths and tools
# ---------------------------------------------------------------------------
GO             ?= go
COMPOSE        ?= docker compose
MIGRATIONS_DIR ?= migrations
QUERIES_DIR    ?= queries
BIN_DIR        ?= bin
TMP_DIR        ?= tmp
SERVER_BIN     := $(BIN_DIR)/server
IMAGE          ?= nightofathousandpixels
IMAGE_TAG      ?= dev

# goose is configured entirely through the environment (exported above), so the
# migrate-* recipes stay as short as `goose up`.
GOOSE_DRIVER        := postgres
GOOSE_DBSTRING      := $(DATABASE_URL)
GOOSE_MIGRATION_DIR := $(MIGRATIONS_DIR)

# Directories no watcher should ever walk: the archived 2025 Next.js app, other
# agents' worktrees, and build output.
WATCH_EXCLUDE_DIRS := archive,worktrees,node_modules,tmp,bin,.git,.beads,testdata
TEMPL_IGNORE       := (^|/)(archive|worktrees|node_modules|tmp|bin|\.git)(/|$$)

.DEFAULT_GOAL := help

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------
.PHONY: help
help: ## Show this help
	@echo "Night of a Thousand Pixels"
	@echo ""
	@echo "Agent slot $(AGENT_SLOT)  ->  compose project $(COMPOSE_PROJECT_NAME)"
	@echo "  app        http://localhost:$(APP_PORT)"
	@echo "  templ      http://localhost:$(TEMPL_PROXY_PORT)   <- open this one, it live-reloads"
	@echo "  postgres   localhost:$(POSTGRES_PORT)"
	@echo ""
	@echo "Targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(firstword $(MAKEFILE_LIST)) \
		| sort \
		| awk 'BEGIN{FS=":.*?## "}{printf "  %-17s %s\n", $$1, $$2}'
	@echo ""
	@echo "Another agent slot:  AGENT_SLOT=1 make dev"

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------
.PHONY: setup
setup: env-file require-compose ## Clean clone to running app: env, postgres, migrate, generate, seed
	@echo "==> starting postgres (project $(COMPOSE_PROJECT_NAME), host port $(POSTGRES_PORT))"
	$(COMPOSE) up -d $(POSTGRES_SERVICE)
	@$(MAKE) wait-for-postgres
	@$(MAKE) migrate-up
	@$(MAKE) sqlc-generate templ-generate
	@$(MAKE) seed
	@echo ""
	@echo "==> ready. run 'make dev' and open http://localhost:$(TEMPL_PROXY_PORT)"

.PHONY: env-file
env-file:
	@if [ -f .env ]; then \
		echo "==> .env already exists, leaving it untouched"; \
	else \
		cp .env.example .env; \
		echo "==> created .env from .env.example - the values in it are placeholders"; \
	fi

.PHONY: wait-for-postgres
wait-for-postgres:
	@echo "==> waiting for postgres on localhost:$(POSTGRES_PORT)"
	@i=0; until $(COMPOSE) exec -T $(POSTGRES_SERVICE) pg_isready -U "$(POSTGRES_USER)" -d "$(POSTGRES_DB)" >/dev/null 2>&1; do \
		i=$$((i + 1)); \
		if [ $$i -ge 60 ]; then \
			echo "postgres was not ready after 60s" >&2; \
			$(COMPOSE) logs --tail=50 $(POSTGRES_SERVICE) >&2; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@echo "==> postgres ready"

# docker-compose.yml is ticket A4's; fail with a useful message until it lands.
.PHONY: require-compose
require-compose:
	@test -f docker-compose.yml || { \
		echo "docker-compose.yml is missing - it arrives with ticket A4." >&2; \
		echo "Until then, set DATABASE_URL in .env to a postgres you already run." >&2; \
		exit 1; \
	}

# ---------------------------------------------------------------------------
# Development
# ---------------------------------------------------------------------------
.PHONY: dev
dev: ## Run all three watchers together (templ proxy, server, sqlc)
	@echo "==> slot $(AGENT_SLOT): open http://localhost:$(TEMPL_PROXY_PORT) (app $(APP_PORT), postgres $(POSTGRES_PORT))"
	@$(MAKE) -j3 dev-templ dev-server dev-sqlc

# templ's own watcher is the only thing that refreshes the browser: it
# regenerates, then pushes a reload down to the page it is proxying.
#
# Caveat worth knowing: templ starts the proxy on its first post-generation
# event, so while the repo contains no .templ files at all the proxy port never
# opens and you must hit the app port directly. It starts working by itself as
# soon as the first template lands.
.PHONY: dev-templ
dev-templ: ## Watch .templ/.go, regenerate, and proxy the app with live reload
	$(GO) tool templ generate \
		-path . \
		-watch \
		-ignore-pattern '$(TEMPL_IGNORE)' \
		-proxy "http://localhost:$(APP_PORT)" \
		-proxybind 127.0.0.1 \
		-proxyport $(TEMPL_PROXY_PORT)

.PHONY: dev-server
dev-server: ## Rebuild and restart the server on any .go change
	@mkdir -p $(TMP_DIR)/air-server-$(AGENT_SLOT)
	$(GO) tool air \
		-tmp_dir "$(TMP_DIR)/air-server-$(AGENT_SLOT)" \
		-build.cmd "$(GO) build -o $(TMP_DIR)/air-server-$(AGENT_SLOT)/server ./cmd/server" \
		-build.bin "$(TMP_DIR)/air-server-$(AGENT_SLOT)/server" \
		-build.include_ext "go" \
		-build.exclude_dir "$(WATCH_EXCLUDE_DIRS)" \
		-build.exclude_regex "_test\.go" \
		-build.stop_on_error "true" \
		-build.send_interrupt "true" \
		-build.kill_delay "2s"

# sqlc has no watch mode of its own, so air drives it: rebuild == regenerate,
# and the "binary" it runs afterwards is a no-op.
.PHONY: dev-sqlc
dev-sqlc: ## Regenerate sqlc code whenever queries/*.sql changes
	@if [ ! -d "$(QUERIES_DIR)" ]; then \
		echo "==> $(QUERIES_DIR)/ does not exist yet - sqlc watcher idle"; \
		exit 0; \
	fi; \
	mkdir -p $(TMP_DIR)/air-sqlc-$(AGENT_SLOT); \
	$(GO) tool air \
		-tmp_dir "$(TMP_DIR)/air-sqlc-$(AGENT_SLOT)" \
		-build.cmd "$(GO) tool sqlc generate" \
		-build.bin "/usr/bin/true" \
		-build.include_dir "$(QUERIES_DIR)" \
		-build.include_ext "sql" \
		-build.exclude_dir "$(WATCH_EXCLUDE_DIRS)"

# ---------------------------------------------------------------------------
# Build, test, lint
# ---------------------------------------------------------------------------
.PHONY: build
build: templ-generate ## Compile the server into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(SERVER_BIN) ./cmd/server
	@echo "==> built $(SERVER_BIN)"

.PHONY: test
test: ## Run the unit tests
	$(GO) test ./...

.PHONY: test-integration
test-integration: ## Run the integration tests (needs `make setup` first)
	$(GO) test -tags=integration -count=1 ./...

.PHONY: lint
lint: ## Run golangci-lint over the module
	$(GO) tool golangci-lint run ./...

# ---------------------------------------------------------------------------
# Migrations
# ---------------------------------------------------------------------------
.PHONY: migrate-up
migrate-up: ## Apply every pending migration
	@if [ ! -d "$(MIGRATIONS_DIR)" ]; then \
		echo "==> no $(MIGRATIONS_DIR)/ yet - nothing to migrate"; \
		exit 0; \
	fi; \
	$(GO) tool goose up

.PHONY: migrate-down
migrate-down: ## Roll back the most recent migration
	$(GO) tool goose down

.PHONY: migrate-new
migrate-new: ## Create a migration: make migrate-new name=add_votes_table
	@if [ -z "$(name)" ]; then \
		echo "usage: make migrate-new name=<snake_case_name>" >&2; \
		echo "  e.g. make migrate-new name=add_votes_table" >&2; \
		exit 1; \
	fi
	@mkdir -p $(MIGRATIONS_DIR)
	$(GO) tool goose create $(name) sql

# ---------------------------------------------------------------------------
# Code generation
# ---------------------------------------------------------------------------
.PHONY: sqlc-generate
sqlc-generate: ## Generate Go from queries/*.sql
	@if [ ! -f sqlc.yaml ] && [ ! -f sqlc.json ]; then \
		echo "==> no sqlc config yet - skipping"; \
		exit 0; \
	fi; \
	$(GO) tool sqlc generate

.PHONY: templ-generate
templ-generate: ## Generate Go from *.templ
	$(GO) tool templ generate -path . -ignore-pattern '$(TEMPL_IGNORE)'

# ---------------------------------------------------------------------------
# Data and images
# ---------------------------------------------------------------------------
.PHONY: seed
seed: ## Load development seed data
	@if [ -d ./cmd/seed ]; then \
		$(GO) run ./cmd/seed; \
	else \
		echo "==> no cmd/seed yet - skipping"; \
	fi

.PHONY: docker-build
docker-build: ## Build the production container image
	@test -f Dockerfile || { echo "Dockerfile is missing - not written yet" >&2; exit 1; }
	docker build -t $(IMAGE):$(IMAGE_TAG) .

# Prints the slot-derived environment. Useful when two agents are debugging why
# they are or are not sharing something.
.PHONY: slot-env
slot-env: ## Print the environment this slot derives
	@echo "AGENT_SLOT=$(AGENT_SLOT)"
	@echo "COMPOSE_PROJECT_NAME=$$COMPOSE_PROJECT_NAME"
	@echo "PORT=$$PORT"
	@echo "TEMPL_PROXY_PORT=$$TEMPL_PROXY_PORT"
	@echo "POSTGRES_PORT=$$POSTGRES_PORT"
	@echo "DATABASE_URL=$$DATABASE_URL"
	@echo "ORIGIN=$$ORIGIN"
