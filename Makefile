.PHONY: setup build test lint fmt fmt-fix clean run help db-up db-down db-init db-reset db-shell

# Variables
BINARY_NAME=fuju-backend
GO=go
GOFLAGS=-v
LDFLAGS=-ldflags "-X main.Version=$(shell git describe --tags --always --dirty)"

# Database variables
DB_HOST ?= localhost
DB_PORT ?= 5432
DB_NAME ?= fuju
DB_USER ?= fuju_user
DB_PASSWORD ?= fuju_password

# Help command
help:
	@echo "Available commands:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make setup         - Install dependencies and tools"
	@echo "  make build         - Build the binary"
	@echo "  make run           - Run the server locally"
	@echo ""
	@echo "Testing & Quality:"
	@echo "  make test          - Run all tests with coverage"
	@echo "  make test-verbose  - Run tests with verbose output"
	@echo "  make lint          - Run linters (golangci-lint)"
	@echo "  make fmt           - Format code (go fmt)"
	@echo ""
	@echo "Database:"
	@echo "  make db-up         - Start PostgreSQL with Docker Compose"
	@echo "  make db-down       - Stop and remove database containers"
	@echo "  make db-init       - Initialize database with migrations"
	@echo "  make db-reset      - Reset database (WARNING: Deletes all data)"
	@echo "  make db-shell      - Connect to PostgreSQL shell"
	@echo ""
	@echo "Other:"
	@echo "  make clean         - Clean build artifacts"
	@echo ""

# Setup: Install linter and dependencies
setup:
	@echo "Installing dependencies..."
	$(GO) mod download
	$(GO) mod tidy
	@echo "Installing golangci-lint..."
	@command -v golangci-lint >/dev/null 2>&1 || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$($(GO) env GOPATH)/bin)
	@echo "Setup complete!"

# Build the application
build: clean
	@echo "Building $(BINARY_NAME)..."
	$(GO) build $(GOFLAGS) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/server

# Run locally
run: build
	@echo "Running $(BINARY_NAME)..."
	./bin/$(BINARY_NAME)

# Test: Run all tests with coverage
test:
	@echo "Running tests..."
	$(GO) test -v -coverprofile=coverage.out ./...
	@echo "Coverage report:"
	$(GO) tool cover -func=coverage.out

# Test verbose with race condition detection
test-verbose:
	@echo "Running tests with race detection..."
	$(GO) test -v -race -coverprofile=coverage.out ./...

# Lint: Run golangci-lint
lint:
	@echo "Running linters..."
	@command -v golangci-lint >/dev/null 2>&1 || (echo "golangci-lint not found. Run 'make setup' first." && exit 1)
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@echo "Code formatted!"

# Format and fix issues (goimports + gofmt)
fmt-fix:
	@echo "Running auto-fixers..."
	@echo "1. Running goimports for package comments..."
	@command -v goimports >/dev/null 2>&1 || ($(GO) install golang.org/x/tools/cmd/goimports@latest)
	$$($(GO) env GOPATH)/bin/goimports -w ./
	@echo "2. Running gofmt for formatting..."
	gofmt -s -w ./
	@echo ""
	@echo "Auto-fix complete!"
	@echo "NOTE: Manual fixes still needed for:"
	@echo "  - Add package comments: // Package xxx ..."
	@echo "  - Add exported function comments"
	@echo "  - Rename unused parameters to _"
	@echo "  - Add error checks for fmt.Fprintf/Fprintln"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	$(GO) clean
	rm -f bin/$(BINARY_NAME)
	rm -f coverage.out
	@echo "Clean complete!"

# Database commands
db-up:
	@echo "Starting database services with Docker Compose..."
	docker-compose up -d
	@echo "Database services started!"
	@echo "PostgreSQL: localhost:5432"
	@echo "Adminer: http://localhost:8081"

db-down:
	@echo "Stopping database services..."
	docker-compose down
	@echo "Database services stopped!"

db-init:
	@echo "Initializing database with migrations..."
	./db/init.sh
	@echo "Database initialized!"

db-reset:
	@echo "WARNING: This will delete all database data!"
	@read -p "Are you sure? (y/n) " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		docker-compose down -v; \
		docker-compose up -d; \
		./db/init.sh; \
		echo "Database reset complete!"; \
	else \
		echo "Database reset cancelled."; \
	fi

db-shell:
	@echo "Connecting to PostgreSQL..."
	PGPASSWORD="$(DB_PASSWORD)" psql -h $(DB_HOST) -p $(DB_PORT) -U $(DB_USER) -d $(DB_NAME)

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	$(GO) clean
	rm -rf bin/
	rm -f coverage.out
	@echo "Clean complete!"

# CI/CD helper: Install only (no build)
ci-setup:
	$(GO) mod download
	$(GO) mod tidy

# Full CI test (lint + test)
ci-test: lint test

# Build for production
build-prod: clean
	@echo "Building production binary..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux ./cmd/server
	@echo "Production build complete: bin/$(BINARY_NAME)-linux"
