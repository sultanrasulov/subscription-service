.DEFAULT_GOAL := help

BIN := bin/subscription-service
# The single source of the linter version; CI reads the same file.
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version)
# Installed once under a versioned name: later runs need no network, and a new
# version in .golangci-lint-version is a new file name, so make installs it.
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
# swag reads the general API info from main.go, the endpoints from the HTTP
# adapter, and the error schema from the problem package.
SWAG_DIRS := ./cmd/subscription-service,./internal/subscription/adapters/httpapi,./internal/platform/problem

.PHONY: help swagger build db run up down logs test test-unit lint pre-commit-install pre-commit-run

help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

swagger: ## Generate the Swagger specification into docs/swagger
	go tool swag init --quiet -g main.go -d $(SWAG_DIRS) -o docs/swagger --outputTypes json,yaml

build: swagger ## Build a static, stripped binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BIN) ./cmd/subscription-service

db: ## Start only Postgres, published on 127.0.0.1 (compose.dev.yaml)
	docker compose -f compose.yaml -f compose.dev.yaml up -d --wait postgres

# Variables already set in the shell win over both files, as with Compose
# interpolation and dotenv: they are saved, the files are loaded, and the
# saved ones are restored.
run: swagger ## Run the service on the host: .env.example, then an optional .env; the shell wins
	saved=$$(export -p) && set -a && . ./.env.example && if [ -f .env ]; then . ./.env; fi && set +a && eval "$$saved" && go run ./cmd/subscription-service

up: ## Build and start the whole stack, then wait until it is healthy
	docker compose up -d --build --wait

down: ## Stop the stack
	docker compose down

logs: ## Follow the stack's logs
	docker compose logs -f

test: ## Run all tests with the race detector (integration tests need Docker)
	go test -race ./...

test-unit: ## Run unit tests only, without Docker
	go test -short ./...

lint: $(GOLANGCI_LINT) ## Run golangci-lint at the version in .golangci-lint-version
	$(GOLANGCI_LINT) run

# pre-commit itself is a Python tool, installed once per machine, for example
# with `pipx install pre-commit`; the hooks it runs are pinned in the config.
pre-commit-install: ## Install the git hook that runs .pre-commit-config.yaml on every commit
	pre-commit install

pre-commit-run: ## Run every pre-commit hook on every file
	pre-commit run --all-files

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@
