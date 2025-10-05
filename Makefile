# Makefile for mtlog-audit
.PHONY: list build test bench torture clean install docker docker-up docker-down docker-test integration-test lint fmt vet

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/willibrandon/mtlog-audit.Version=$(VERSION)
DOCKER_COMPOSE := docker-compose -f docker/docker-compose.yml

list:
	@echo "Available targets:"
	@echo "  build                 - Build the mtlog-audit binary"
	@echo "  test                  - Run unit tests with race detector"
	@echo "  fmt                   - Format all Go source files"
	@echo "  vet                   - Run go vet on all packages"
	@echo "  lint                  - Run golangci-lint"
	@echo "  bench                 - Run performance benchmarks"
	@echo "  torture               - Run torture tests (1M iterations, 24h timeout)"
	@echo "  torture-docker        - Run containerized torture tests (1K iterations)"
	@echo "  torture-docker-diskfull - Run disk full torture test in container"
	@echo "  torture-docker-full   - Run full containerized torture suite (1M iterations)"
	@echo "  install               - Install mtlog-audit to GOPATH/bin"
	@echo "  clean                 - Remove build artifacts and test files"
	@echo "  docker                - Build Docker image"
	@echo "  docker-up             - Start Docker test infrastructure (MinIO, Azurite, etc.)"
	@echo "  docker-down           - Stop Docker test infrastructure"
	@echo "  docker-test           - Run integration tests with Docker (starts/stops services)"
	@echo "  integration-test      - Run integration tests (requires docker-up first)"
	@echo "  test-with-services    - Run tests with Docker services (convenience wrapper)"

build:
	go build -ldflags "$(LDFLAGS)" -o bin/mtlog-audit ./cmd/mtlog-audit

test:
	CGO_ENABLED=0 go test -race -coverprofile=coverage.out ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install from https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run

bench:
	go test -bench=. -benchmem ./performance

torture:
	go test -tags=torture -timeout=24h ./torture -count=1000000

torture-docker:
	@echo "Running containerized torture tests..."
	@chmod +x docker/run-torture-tests.sh
	@bash docker/run-torture-tests.sh all 1000

torture-docker-diskfull:
	@echo "Running containerized disk full torture test..."
	@chmod +x docker/run-torture-diskfull.sh
	@bash docker/run-torture-diskfull.sh

torture-docker-full:
	@echo "Running full containerized torture test suite (1M iterations)..."
	@chmod +x docker/run-torture-tests.sh
	@bash docker/run-torture-tests.sh all 1000000

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/mtlog-audit

clean:
	rm -rf bin/ coverage.out *.wal
	go clean -testcache

docker:
	docker build -t mtlog-audit:$(VERSION) .

# Docker infrastructure commands
docker-up:
	@echo "Starting test infrastructure..."
	$(DOCKER_COMPOSE) up -d
	@echo "Waiting for services to be ready..."
	@bash docker/wait-for-services.sh
	@echo "Test infrastructure is ready!"

docker-down:
	@echo "Stopping test infrastructure..."
	$(DOCKER_COMPOSE) down -v

docker-test: docker-up
	@echo "Running integration tests with Docker infrastructure..."
	@export $$(grep -v '^#' docker/.env | grep -v '^$$' | xargs) && go test -tags=integration ./integration/...
	@$(MAKE) docker-down

integration-test:
	@echo "Running integration tests..."
	@echo "Make sure Docker services are running (run 'make docker-up' if not)"
	go test -tags=integration -v ./integration/...

# Quick test with local services
test-with-services: docker-up test docker-down

.DEFAULT_GOAL := build