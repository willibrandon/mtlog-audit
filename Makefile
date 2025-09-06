# Makefile for mtlog-audit
.PHONY: build test bench torture clean install docker docker-up docker-down docker-test integration-test

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/willibrandon/mtlog-audit.Version=$(VERSION)
DOCKER_COMPOSE := docker-compose -f docker/docker-compose.yml

build:
	go build -ldflags "$(LDFLAGS)" -o bin/mtlog-audit ./cmd/mtlog-audit

test:
	go test -race -coverprofile=coverage.out ./...

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
	@export $$(cat docker/.env | xargs) && go test -tags=integration ./integration/...
	@$(MAKE) docker-down

integration-test:
	@echo "Running integration tests..."
	@echo "Make sure Docker services are running (run 'make docker-up' if not)"
	go test -tags=integration -v ./integration/...

# Quick test with local services
test-with-services: docker-up test docker-down

.DEFAULT_GOAL := build