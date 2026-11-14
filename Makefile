.DEFAULT_GOAL := help

BIN := bin/subscription-service
# The single source of the linter version; CI reads the same file (design §10.5).
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version)
# Installed once under a versioned name: later runs need no network, and a new
# version in .golangci-lint-version is a new file name, so make installs it.
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
# swag fails on a directory that does not exist yet; phase 3 adds the HTTP adapter.
SWAG_DIRS := ./cmd/subscription-service

.PHONY: help swagger build db run test test-unit lint

help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

swagger: ## Generate the Swagger specification into docs/swagger
	go tool swag init --quiet -g main.go -d $(SWAG_DIRS) -o docs/swagger --outputTypes json,yaml

build: swagger ## Build a static, stripped binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BIN) ./cmd/subscription-service

db: ## Start only Postgres, published on 127.0.0.1 (compose.dev.yaml)
	docker compose -f compose.yaml -f compose.dev.yaml up -d --wait postgres

# Variables already set in the shell win over both files, as with Compose
# interpolation and dotenv (design §7): they are saved, the files are loaded,
# and the saved ones are restored.
run: swagger ## Run the service on the host: .env.example, then an optional .env; the shell wins
	saved=$$(export -p) && set -a && . ./.env.example && if [ -f .env ]; then . ./.env; fi && set +a && eval "$$saved" && go run ./cmd/subscription-service

test: ## Run all tests with the race detector (integration tests need Docker)
	go test -race ./...

test-unit: ## Run unit tests only, without Docker
	go test -short ./...

lint: $(GOLANGCI_LINT) ## Run golangci-lint at the version in .golangci-lint-version
	$(GOLANGCI_LINT) run

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@
