# Makefile for mtlog-audit
.PHONY: list build test bench torture clean install docker docker-up docker-down docker-test integration-test lint fmt vet torture-docker torture-docker-diskfull torture-docker-full

# Detect OS for cross-platform support
ifeq ($(OS),Windows_NT)
    DETECTED_OS := Windows
    RM := cmd /C del /F /Q
    RMDIR := cmd /C rmdir /S /Q
    PATHSEP := \\
    SHELL := cmd
else
    DETECTED_OS := $(shell uname -s)
    RM := rm -f
    RMDIR := rm -rf
    PATHSEP := /
endif

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/willibrandon/mtlog-audit.Version=$(VERSION)
DOCKER_COMPOSE := docker compose -f docker/docker-compose.yml
BIN_DIR := bin
BINARY_NAME := mtlog-audit$(if $(filter Windows,$(DETECTED_OS)),.exe,)

list:
	@echo "Available targets (Detected OS: $(DETECTED_OS)):"
	@echo ""
	@echo "Core targets (cross-platform):"
	@echo "  build                   - Build the mtlog-audit binary"
	@echo "  test                    - Run unit tests with race detector"
	@echo "  fmt                     - Format all Go source files"
	@echo "  vet                     - Run go vet on all packages"
	@echo "  lint                    - Run golangci-lint"
	@echo "  bench                   - Run performance benchmarks"
	@echo "  torture                 - Run torture tests (1M iterations, 24h timeout)"
	@echo "  install                 - Install mtlog-audit to GOPATH/bin"
	@echo "  clean                   - Remove build artifacts and test files"
	@echo ""
	@echo "Docker targets (require Git Bash/WSL on Windows):"
	@echo "  docker                  - Build Docker image"
	@echo "  docker-up               - Start Docker test infrastructure (MinIO, Azurite, etc.)"
	@echo "  docker-down             - Stop Docker test infrastructure"
	@echo "  docker-test             - Run integration tests with Docker (starts/stops services)"
	@echo "  integration-test        - Run integration tests (requires docker-up first)"
	@echo "  torture-docker          - Run containerized torture tests (1K iterations)"
	@echo "  torture-docker-diskfull - Run disk full torture test in container"
	@echo "  torture-docker-full     - Run full containerized torture suite (1M iterations)"

build:
	@echo "Building for $(DETECTED_OS)..."
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)$(PATHSEP)$(BINARY_NAME) ./cmd/mtlog-audit

test:
ifeq ($(DETECTED_OS),Darwin)
	go test -coverprofile=coverage.out ./...
else
	go test -race -coverprofile=coverage.out ./...
endif

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
ifeq ($(DETECTED_OS),Windows)
	@golangci-lint --version >nul 2>&1 || (echo golangci-lint not installed. Install from https://golangci-lint.run/usage/install/ && exit /b 1)
else
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install from https://golangci-lint.run/usage/install/" && exit 1)
endif
	golangci-lint run

bench:
	go test -bench=. -benchmem ./performance

torture:
	go test -tags=torture -timeout=24h ./torture -count=1000000

torture-docker:
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: This target requires Git Bash or WSL on Windows"
endif
	@echo "Running containerized torture tests..."
	@chmod +x docker/run-torture-tests.sh 2>/dev/null || true
	@bash docker/run-torture-tests.sh all 1000

torture-docker-diskfull:
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: This target requires Git Bash or WSL on Windows"
endif
	@echo "Running containerized disk full torture test..."
	@chmod +x docker/run-torture-diskfull.sh 2>/dev/null || true
	@bash docker/run-torture-diskfull.sh

torture-docker-full:
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: This target requires Git Bash or WSL on Windows"
endif
	@echo "Running full containerized torture test suite (1M iterations)..."
	@chmod +x docker/run-torture-tests.sh 2>/dev/null || true
	@bash docker/run-torture-tests.sh all 1000000

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/mtlog-audit

clean:
ifeq ($(DETECTED_OS),Windows)
	@if exist bin rmdir /S /Q bin 2>nul
	@if exist coverage.out del /F /Q coverage.out 2>nul
	@if exist *.wal del /F /Q *.wal 2>nul
else
	@rm -rf bin/ coverage.out *.wal 2>/dev/null || true
endif
	@go clean -testcache

docker:
	docker build -t mtlog-audit:$(VERSION) .

# Docker infrastructure commands
docker-up:
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: This target requires Git Bash or WSL on Windows"
endif
	@echo "Starting test infrastructure..."
	$(DOCKER_COMPOSE) up -d
	@echo "Waiting for services to be ready..."
	@bash docker/wait-for-services.sh
	@echo "Test infrastructure is ready!"

docker-down:
	@echo "Stopping test infrastructure..."
	$(DOCKER_COMPOSE) down -v

docker-test: docker-up
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: Environment variable loading not supported on Windows cmd. Use Git Bash, WSL, or set variables manually."
	go test -tags=integration ./integration/...
else
	@echo "Running integration tests with Docker infrastructure..."
	@export $$(grep -v '^#' docker/.env | grep -v '^$$' | xargs) && go test -tags=integration ./integration/...
endif
	@$(MAKE) docker-down

integration-test:
	@echo "Running integration tests..."
ifeq ($(DETECTED_OS),Windows)
	@echo "Note: On Windows, set environment variables from docker/.env manually or use Git Bash/WSL"
	@echo "Example: set AWS_ACCESS_KEY_ID=minioadmin && set AWS_SECRET_ACCESS_KEY=minioadmin"
	go test -tags=integration -v ./integration/...
else
	@echo "Make sure Docker services are running (run 'make docker-up' if not)"
	@export $$(grep -v '^#' docker/.env | grep -v '^$$' | xargs) && go test -tags=integration -v ./integration/...
endif

.DEFAULT_GOAL := build