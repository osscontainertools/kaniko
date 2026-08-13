SHELL := /bin/bash
NAME := docker-credential-acr
GO := go
CGO_ENABLED = 0

REPORTS_DIR := build/reports
COVER_OUT := $(REPORTS_DIR)/cover.out

# the race detector needs cgo, the released binary is still built without it
GOTEST := CGO_ENABLED=1 $(GO) test -race -p 4
ifdef DISABLE_TEST_CACHING
GOTEST += -count=1
endif

# the integration tests talk to a live registry, so they are excluded by path
UNIT_PACKAGES = $(shell $(GO) list ./... | grep -v /integration)

.DEFAULT_GOAL := help

.PHONY: help
help:
	@grep -h -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary for the current OS
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build -o build/$(NAME) .

.PHONY: build-all
build-all: ## Cross compile every target the release builds
	goreleaser build --snapshot --clean

.PHONY: install
install: ## Install the binary
	$(GO) install .

.PHONY: test
test: ## Run the unit tests
	$(GOTEST) -failfast -short $(UNIT_PACKAGES)

.PHONY: integration-test
integration-test: ## Run the live ACR tests
	$(GOTEST) -count=1 -v ./integration/...

.PHONY: test-coverage
test-coverage: ## Run the unit tests with a coverage profile
	mkdir -p $(REPORTS_DIR)
	$(GOTEST) -coverprofile=$(COVER_OUT) --covermode=count --coverpkg=./... -failfast -short $(UNIT_PACKAGES)
	$(GO) tool cover -func=$(COVER_OUT) | tail -1

.PHONY: fmt
fmt: ## Format the code
	golangci-lint fmt --no-config -E gofmt -E goimports

.PHONY: lint
lint: ## Lint the code, same invocation the CI uses
	golangci-lint run --no-config ./...
	golangci-lint fmt --no-config -E gofmt -E goimports --diff

.PHONY: check
check: build test lint ## Build, test and lint

.PHONY: clean
clean: ## Clean the generated artifacts
	rm -rf build dist
