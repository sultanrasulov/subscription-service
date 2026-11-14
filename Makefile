.DEFAULT_GOAL := help

BIN := bin/subscription-service
# The single source of the linter version; CI reads the same file (design §10.5).
GOLANGCI_LINT_VERSION := $(shell cat .golangci-lint-version)
# swag fails on a directory that does not exist yet; phase 3 adds the HTTP adapter.
SWAG_DIRS := ./cmd/subscription-service

.PHONY: help swagger build test test-unit lint

help: ## List the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

swagger: ## Generate the Swagger specification into docs/swagger
	go tool swag init --quiet -g main.go -d $(SWAG_DIRS) -o docs/swagger --outputTypes json,yaml

build: swagger ## Build a static, stripped binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BIN) ./cmd/subscription-service

test: ## Run all tests with the race detector (integration tests need Docker)
	go test -race ./...

test-unit: ## Run unit tests only, without Docker
	go test -short ./...

lint: ## Run golangci-lint at the version in .golangci-lint-version
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run
