# Makefile for mtlog-audit
.PHONY: build test bench torture clean install docker

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X github.com/willibrandon/mtlog-audit.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/mtlog-audit ./cmd/mtlog-audit

test:
	go test -race -coverprofile=coverage.out ./...

bench:
	go test -bench=. -benchmem ./performance

torture:
	go test -tags=torture -timeout=24h ./torture -count=1000000

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/mtlog-audit

clean:
	rm -rf bin/ coverage.out *.wal

docker:
	docker build -t mtlog-audit:$(VERSION) .

.DEFAULT_GOAL := build