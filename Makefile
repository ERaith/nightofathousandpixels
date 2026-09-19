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

# Remember what .env supplied for the three values the slot owns, so that
# setting one there is reported rather than silently ignored (see the warning
# further down). Everything else in .env is the developer's to set.
ENV_SUPPLIED_PORT             := $(PORT)
ENV_SUPPLIED_DATABASE_URL     := $(DATABASE_URL)
ENV_SUPPLIED_ORIGIN           := $(ORIGIN)
ENV_SUPPLIED_OAUTH_ISSUER_URL := $(OAUTH_ISSUER_URL)

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

# Ephemeral database for integration tests: a second, throwaway Postgres that
# must not share a port or a volume with the one holding your dev data.
TEST_POSTGRES_PORT := $(shell expr 5434 + $(AGENT_SLOT) '*' 10)
TEST_APP_PORT      := $(shell expr 8085 + $(AGENT_SLOT) '*' 10)

# The development OIDC provider (ticket D7). Slot-derived like every other
# port, so two agents running `make dev` do not fight over one provider - and
# more importantly do not share one, since the issuer is baked into every
# session cookie's provenance.
OIDC_PORT := $(shell expr 9000 + $(AGENT_SLOT) '*' 10)
# The mock OIDC provider the browser tests sign in against (ticket G3). Its
# container port and its host port are the same number on purpose: the app and
# the browser have to agree on one issuer URL, port included. See the long note
# in docker-compose.e2e.yml.
TEST_OIDC_PORT     := $(shell expr 9085 + $(AGENT_SLOT) '*' 10)

# The test stack is a separate compose project, not an overlay on the dev one:
# `docker compose -f a.yml -f b.yml` under one project name would replace the
# dev postgres container and take your development data with it.
COMPOSE_TEST_PROJECT := $(COMPOSE_PROJECT_NAME)-test

# ---------------------------------------------------------------------------
# Configuration (see internal/config/config.go for what the server requires)
# ---------------------------------------------------------------------------
POSTGRES_USER     ?= nap
POSTGRES_PASSWORD ?= nap
POSTGRES_DB       ?= nap
POSTGRES_SERVICE  ?= postgres

# PORT, DATABASE_URL and ORIGIN all belong to the slot, so all three are
# assigned - not defaulted. Letting .env win any one of them re-creates the
# failure the slot scheme exists to prevent: a `?=` DATABASE_URL next to a `:=`
# PORT puts slot 2's app on 8100 in front of slot 0's database on 5433, and
# nothing anywhere reports it. Override for a one-off on the command line,
# which beats every assignment here: `make test-integration DATABASE_URL=...`.
PORT         := $(APP_PORT)
DATABASE_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

# During `make dev` the browser talks to the templ proxy, not to the app
# directly, so the browser-facing origin is the proxy's.
ORIGIN := http://localhost:$(TEMPL_PROXY_PORT)

# OAUTH_ISSUER_URL belongs to the slot for the same reason the three above do:
# it names a port this Makefile publishes. It is assigned rather than
# defaulted, so .env cannot quietly point slot 2's app at slot 0's provider --
# which would work, right up until both slots' sessions were signed by one
# provider and nobody could tell whose was whose.
#
# The /oidc suffix is not decoration: mockoidc serves its endpoints under that
# path and cannot be told otherwise, so an issuer without it is one the
# provider refuses to start against. Point this at Google (or any real issuer)
# on the command line to develop against the real thing.
OAUTH_ISSUER_URL := http://localhost:$(OIDC_PORT)/oidc

TEST_DATABASE_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(TEST_POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable

# ORIGIN for the containerised app, which is published on APP_PORT rather than
# sitting behind the templ proxy. Deployment overrides this with the real URL.
COMPOSE_ORIGIN      := http://localhost:$(APP_PORT)
COMPOSE_TEST_ORIGIN := http://localhost:$(TEST_APP_PORT)

# The issuer the e2e stack runs against. `/oidc` is not decoration: mockoidc
# serves its endpoints under that path and cannot be told otherwise, and
# mockoidcd refuses to start if the issuer it is handed does not end in it.
E2E_ISSUER_URL := http://localhost:$(TEST_OIDC_PORT)/oidc

# Say so out loud rather than ignoring a value someone took the trouble to set.
$(foreach v,PORT DATABASE_URL ORIGIN OAUTH_ISSUER_URL,$(if $(and $(ENV_SUPPLIED_$(v)),$(filter-out $($(v)),$(ENV_SUPPLIED_$(v)))),$(warning .env sets $(v)=$(ENV_SUPPLIED_$(v)), which AGENT_SLOT=$(AGENT_SLOT) overrides with $($(v)). Remove it from .env, or pass $(v)=... on the make command line.)))

# ---------------------------------------------------------------------------
# Paths and tools
# ---------------------------------------------------------------------------
GO             ?= go
COMPOSE        ?= docker compose
COMPOSE_TEST   := $(COMPOSE) -p $(COMPOSE_TEST_PROJECT) -f docker-compose.yml -f docker-compose.test.yml

# The development stack: the base compose file plus the mock OIDC provider.
# The deploy never passes docker-compose.dev.yml to -f, which is what keeps the
# mock provider out of production - see the header of that file.
COMPOSE_DEV    := $(COMPOSE) -f docker-compose.yml -f docker-compose.dev.yml
COMPOSE_E2E    := $(COMPOSE_TEST) -f docker-compose.e2e.yml --profile e2e
MIGRATIONS_DIR ?= internal/store/migrations
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

# The same list, ANCHORED at this checkout's root, with the root regex-escaped.
#
# templ matches the ignore pattern against ABSOLUTE paths, and TEMPL_IGNORE
# above is unanchored. Inside worktrees/<agent>/ every absolute path therefore
# contains "/worktrees/", the pattern matches every file in the tree, and templ
# generates nothing while printing a tick and exiting 0 (nap-hil).
#
# Three fixes were tried before this one, and the first two are instructive
# because each reintroduced the bug by another route:
#
#   - Dropping "worktrees" removes the protection it exists for. The main
#     clone holds every agent's worktree, so templ would walk into all of them
#     and rewrite their generated files - and it is not the agent doing it, it
#     is whoever runs the target in the main clone.
#   - Anchoring at a bare $(CURDIR) splices a filesystem path into a REGEX.
#     templ rejects a malformed one loudly (a "++" in the path gives "invalid
#     nested repetition operator"), so this one is a crash rather than a silent
#     hole, but a "." in the path still widens the pattern quietly.
#   - Including "worktrees" only when $(wildcard) finds one keeps the pattern
#     unanchored, so AMBIENT directory names still match: a checkout under a
#     directory called tmp, bin, archive or node_modules - a CI runner cloning
#     into /tmp is the realistic case - matches every file and generates
#     nothing. Reproduced, with exactly nap-hil's symptom.
#
# Anchoring fixes the ambient-name hole and escaping fixes the injection, and
# only both together fix both. Verified with a repo root at "<tmp>/c++(x)",
# which has metacharacters and an ambient "tmp" at once.
#
# Scoped to the e2e guard rather than replacing TEMPL_IGNORE: that is nap-hil's
# to change and several agents run the shared targets.
#
# (A single quote in the repository path would break the shell quoting below.
# Nothing else in this Makefile survives that either.)
TEMPL_ROOT_RE    := $(shell printf '%s' '$(CURDIR)' | sed 's/[][\.^$$*+?(){}|\/]/\\&/g')
TEMPL_IGNORE_E2E := ^$(TEMPL_ROOT_RE)/(archive|worktrees|node_modules|tmp|bin|\.git)(/|$$)

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
	@echo "  oidc       http://localhost:$(OIDC_PORT)/oidc   (mock provider, dev only)"
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
setup: env-file require-compose ## Clean clone to running app: env, postgres, oidc, migrate, generate, seed
	@echo "==> starting postgres and the dev OIDC provider (project $(COMPOSE_PROJECT_NAME))"
	$(COMPOSE_DEV) up -d --build $(POSTGRES_SERVICE) mockoidc
	@$(MAKE) wait-for-postgres
	@$(MAKE) migrate-up
	@$(MAKE) sqlc-generate templ-generate
	@$(MAKE) seed-dev
	@echo ""
	@echo "==> ready. run 'make dev' and open http://localhost:$(TEMPL_PROXY_PORT)"
	@echo "    sign in at http://localhost:$(TEMPL_PROXY_PORT)/auth/login - see README.md for the test users"

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
# The dev stack the watchers run against. The server performs OIDC discovery
# at startup and will not come up without a reachable provider, so this has to
# happen before `dev-server` rather than beside it.
.PHONY: dev-up
dev-up: require-compose ## Start the dev stack: postgres and the mock OIDC provider
	$(COMPOSE_DEV) up -d --build $(POSTGRES_SERVICE) mockoidc
	@$(MAKE) wait-for-postgres
	@echo "==> oidc provider on http://localhost:$(OIDC_PORT)/oidc"

.PHONY: dev-down
dev-down: ## Stop the dev stack, keeping the database volume
	$(COMPOSE_DEV) down

.PHONY: dev
dev: dev-up ## Run all three watchers together (templ proxy, server, sqlc)
	@echo "==> slot $(AGENT_SLOT): open http://localhost:$(TEMPL_PROXY_PORT) (app $(APP_PORT), postgres $(POSTGRES_PORT), oidc $(OIDC_PORT))"
	@echo "==> sign in: http://localhost:$(TEMPL_PROXY_PORT)/auth/login"
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
test-integration: test-db-up ## Run the integration tests against a throwaway database
	DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test -tags=integration -count=1 -p 1 ./...

.PHONY: lint
lint: ## Run golangci-lint over the module
	$(GO) tool golangci-lint run ./...

# The theme-pack contract is enforced in CSS and checked by a stdlib-only
# python script; no node, no npm, nothing to install.
.PHONY: themes-check
themes-check: ## Check the theme tooling and the fallback palette's contrast
	python3 -m unittest discover -s themes -p 'test_*.py'
	./themes/check-contrast.py --defaults

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
seed: seed-dev ## Load development seed data (alias for seed-dev)

# One command from a clean checkout, which is the whole point of ticket D7.
# It migrates first so that `make seed-dev` works against a database that has
# only just been created, rather than failing with "relation does not exist"
# and making the reader work out that migrate-up was the missing step.
.PHONY: seed-dev
seed-dev: ## Seed the 2026 season and the four test users (see README.md)
	@$(MAKE) migrate-up
	$(GO) run ./cmd/seed

# The guarantee that the mock provider is not in the shipped binary, checked
# against the build graph rather than trusted. A Dockerfile stage is a
# convention; an absent import is a fact.
.PHONY: no-mock-provider-in-server
no-mock-provider-in-server: ## Fail if the mock OIDC provider is reachable from cmd/server
	@if $(GO) list -deps ./cmd/server | grep -q "oauth2-proxy/mockoidc"; then \
		echo "FAIL: oauth2-proxy/mockoidc is a dependency of ./cmd/server." >&2; \
		echo "      The mock identity provider would ship in the server binary." >&2; \
		exit 1; \
	fi
	@echo "==> ok: the mock OIDC provider is not reachable from ./cmd/server"

.PHONY: docker-build
docker-build: ## Build the shipped image (distroless, server binary only)
	docker build --target final -t $(IMAGE):$(IMAGE_TAG) .

.PHONY: compose-up
compose-up: ## Bring the whole stack up in containers (migrations gate the app)
	$(COMPOSE) up -d --build
	@echo "==> app on http://localhost:$(APP_PORT) (project $(COMPOSE_PROJECT_NAME))"

.PHONY: compose-down
compose-down: ## Stop this slot's stack, keeping its database volume
	$(COMPOSE) down

.PHONY: compose-nuke
compose-nuke: ## Stop this slot's stack and delete its database volume
	$(COMPOSE) down -v

.PHONY: compose-logs
compose-logs: ## Follow this slot's container logs
	$(COMPOSE) logs -f

.PHONY: compose-config
compose-config: ## Print the fully resolved compose config for this slot
	$(COMPOSE) config

.PHONY: compose-test-config
compose-test-config: ## Print the fully resolved test-stack config for this slot
	$(COMPOSE_TEST) config

.PHONY: compose-e2e-config
compose-e2e-config: ## Print the fully resolved e2e-stack config for this slot
	$(COMPOSE_E2E) config

# ---------------------------------------------------------------------------
# Throwaway test database
# ---------------------------------------------------------------------------
.PHONY: test-db-up
# `run` rather than `up -d` for the migrator on purpose: it runs in the
# foreground and returns goose's exit code, so a broken migration fails this
# target instead of handing the tests an empty database to fail against.
test-db-up: ## Start the disposable test database and migrate it
	$(COMPOSE_TEST) up -d --build postgres
	$(COMPOSE_TEST) run --rm --build migrate
	@echo "==> test database on localhost:$(TEST_POSTGRES_PORT) (project $(COMPOSE_TEST_PROJECT))"

.PHONY: test-db-down
test-db-down: ## Stop and erase the disposable test database
	$(COMPOSE_TEST) down -v

# ---------------------------------------------------------------------------
# End-to-end browser tests (ticket G3)
# ---------------------------------------------------------------------------
#
# The suite is a Node project in e2e/ driving @playwright/test. It shares no
# code with the application - it is black-box HTTP against the shipped image -
# so the second language costs nothing and buys the trace viewer, UI mode and
# codegen.
#
# `npx playwright test` from e2e/ does the same thing: its global setup calls
# `make e2e-up` and its teardown calls `make e2e-down`, so the stack is brought
# up the one way rather than two.

# The e2e suite resolves its ports from here rather than re-deriving them, so
# the slot arithmetic has exactly one home. e2e/lib/stack.ts parses this.
.PHONY: e2e-env
e2e-env: ## Print the environment the e2e suite runs against
	@echo "AGENT_SLOT=$(AGENT_SLOT)"
	@echo "E2E_BASE_URL=$(COMPOSE_TEST_ORIGIN)"
	@echo "E2E_ISSUER_URL=$(E2E_ISSUER_URL)"
	@echo "E2E_OIDC_PORT=$(TEST_OIDC_PORT)"
	@echo "E2E_CLIENT_ID=$(OAUTH_CLIENT_ID)"
	@echo "E2E_CLIENT_SECRET=$(OAUTH_CLIENT_SECRET)"
	@echo "E2E_COMPOSE_PROJECT=$(COMPOSE_TEST_PROJECT)"

# The browser tests assert on rendered HTML, and the image they run against is
# built from the COMMITTED *_templ.go - the Dockerfile only runs `go build`.
# So a .templ edited without regenerating produces a suite that passes against
# markup nobody is serving any more. Inside a worktree that is not hypothetical:
# `make templ-generate` silently generates nothing there (nap-hil), so the
# normal way of keeping them in step does not work and says nothing about it.
#
# This regenerates IN PLACE and fails if anything changed. Delete it when
# nap-hil lands and templ-generate is trustworthy everywhere.
.PHONY: e2e-templ-fresh
e2e-templ-fresh: ## Fail if the committed templ output is stale
	@$(GO) tool templ generate -path . -ignore-pattern '$(TEMPL_IGNORE_E2E)' >/dev/null
	@if [ -n "$$(git status --porcelain -- '*_templ.go')" ]; then \
		echo "" >&2; \
		echo "The committed templ output was STALE and has been regenerated in place:" >&2; \
		git status --short -- '*_templ.go' >&2; \
		echo "" >&2; \
		echo "The e2e suite tests the committed output, so it would have passed" >&2; \
		echo "against markup nobody is serving. Review the diff and commit it." >&2; \
		exit 1; \
	fi

.PHONY: e2e
e2e: e2e-install e2e-typecheck e2e-templ-fresh ## Run the Playwright end-to-end suite against a throwaway stack
	cd e2e && npx playwright test $(E2E_ARGS)

# Playwright transpiles TypeScript without type-checking it, so a spec with a
# type error runs anyway and fails somewhere less informative. This is cheap
# and catches it at the right place.
.PHONY: e2e-typecheck
e2e-typecheck: ## Type-check the e2e suite
	cd e2e && npx tsc --noEmit

.PHONY: e2e-ui
e2e-ui: e2e-install ## Open Playwright's UI mode against a throwaway stack
	cd e2e && E2E_KEEP_STACK=1 npx playwright test --ui

.PHONY: e2e-report
e2e-report: ## Open the HTML report from the last e2e run
	cd e2e && npx playwright show-report

# `npm ci` needs a lockfile and reinstalls from scratch; falling back to
# `npm install` keeps a fresh clone working before one is committed.
.PHONY: e2e-install
e2e-install: ## Install the e2e suite's node dependencies and browsers
	@cd e2e && if [ -f package-lock.json ]; then npm ci; else npm install; fi
	@cd e2e && npx playwright install chromium

.PHONY: e2e-up
e2e-up: ## Bring up the e2e stack: throwaway database, mock OIDC provider, app
	$(COMPOSE_E2E) up -d --build --wait mockoidc postgres
	$(COMPOSE_E2E) run --rm --build migrate
	$(COMPOSE_E2E) up -d --build --wait app
	@echo "==> e2e app  http://localhost:$(TEST_APP_PORT)"
	@echo "==> e2e oidc $(E2E_ISSUER_URL)"

.PHONY: e2e-down
e2e-down: ## Stop the e2e stack and erase its database
	$(COMPOSE_E2E) down -v --remove-orphans

.PHONY: e2e-logs
e2e-logs: ## Follow the e2e stack's container logs
	$(COMPOSE_E2E) logs -f

# Prints the slot-derived environment. Useful when two agents are debugging why
# they are or are not sharing something.
.PHONY: slot-env
slot-env: ## Print the environment this slot derives
	@echo "AGENT_SLOT=$(AGENT_SLOT)"
	@echo "COMPOSE_PROJECT_NAME=$$COMPOSE_PROJECT_NAME"
	@echo "COMPOSE_TEST_PROJECT=$$COMPOSE_TEST_PROJECT"
	@echo "APP_PORT=$$APP_PORT"
	@echo "PORT=$$PORT"
	@echo "TEMPL_PROXY_PORT=$$TEMPL_PROXY_PORT"
	@echo "POSTGRES_PORT=$$POSTGRES_PORT"
	@echo "TEST_POSTGRES_PORT=$$TEST_POSTGRES_PORT"
	@echo "TEST_APP_PORT=$$TEST_APP_PORT"
	@echo "TEST_OIDC_PORT=$$TEST_OIDC_PORT"
	@echo "DATABASE_URL=$$DATABASE_URL"
	@echo "TEST_DATABASE_URL=$$TEST_DATABASE_URL"
	@echo "ORIGIN=$$ORIGIN"
	@echo "OIDC_PORT=$$OIDC_PORT"
	@echo "OAUTH_ISSUER_URL=$$OAUTH_ISSUER_URL"
	@echo "COMPOSE_ORIGIN=$$COMPOSE_ORIGIN"
	@echo "COMPOSE_TEST_ORIGIN=$$COMPOSE_TEST_ORIGIN"
	@echo "E2E_ISSUER_URL=$$E2E_ISSUER_URL"
