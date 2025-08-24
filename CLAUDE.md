# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

mtlog-audit is a bulletproof audit logging sink for Go applications that guarantees zero data loss. It's designed as a standalone project that integrates with [mtlog](https://github.com/willibrandon/mtlog) through the `core.LogEventSink` interface, but can also work with other Go logging libraries (slog, logr, zerolog).

**Key Value Proposition**: "The only audit logger for Go that mathematically cannot lose data - proven through 1,000,000+ torture tests."

## Repository Status

This is a design-phase repository containing comprehensive documentation for implementing mtlog-audit. The actual implementation is planned according to the design documents.

## Common Development Commands

### Build Commands
```bash
# Build the main binary
make build

# Build the CLI tool
go build -o bin/mtlog-audit ./cmd/mtlog-audit

# Build with version information
go build -ldflags "-X github.com/willibrandon/mtlog-audit.Version=$(git describe --tags --always --dirty)" ./cmd/mtlog-audit
```

### Test Commands
```bash
# Run all unit tests with race detection
make test
go test -race -coverprofile=coverage.out ./...

# Run integration tests
go test -tags=integration ./...

# Run torture tests (quick - 1000 iterations)
go test -tags=torture ./torture -count=1000

# Run full torture test suite (1M iterations)
make torture
go test -tags=torture -timeout=24h ./torture -count=1000000

# Run benchmarks
make bench
go test -bench=. -benchmem ./performance

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Development Commands
```bash
# Install the CLI tool locally
make install
go install ./cmd/mtlog-audit

# Clean build artifacts
make clean

# Build Docker image
make docker
docker build -t mtlog-audit:$(git describe --tags --always --dirty) .

# Format code
go fmt ./...

# Lint code (requires golangci-lint)
golangci-lint run

# Update dependencies
go mod tidy
go mod download
```

## High-Level Architecture

### Core Components

1. **WAL (Write-Ahead Log)** - The foundation of durability
   - Location: `wal/` directory
   - Implements segment-based storage with CRC32 checksums
   - Provides torn-write protection and hash chain verification
   - Supports multiple sync modes (immediate, interval, batch)
   - Recovery engine can recover from 99.99% of corruption scenarios

2. **Main Sink** - Entry point implementing `core.LogEventSink`
   - Location: `sink.go`, `options.go`, `errors.go`
   - Orchestrates WAL writes, compliance, backends, and monitoring
   - Provides functional options API for configuration
   - Handles failure scenarios with configurable strategies

3. **Compliance Engine** - Regulatory compliance features
   - Location: `compliance/` directory
   - Pre-configured profiles: HIPAA, PCI-DSS, SOX, GDPR
   - Features: encryption (AES-256-GCM), signing (Ed25519), retention policies
   - Merkle tree for tamper detection, chain of custody

4. **Storage Backends** - Multi-destination replication
   - Location: `backends/` directory
   - Supported: filesystem, S3, Azure Blob, Google Cloud Storage
   - Features: immutability policies, versioning, multi-backend with quorum

5. **Resilience Layer** - Failure handling
   - Location: `resilience/` directory
   - Circuit breakers, retry policies, shadow writes
   - Quorum-based writes for critical data

6. **Torture Testing** - Reliability validation
   - Location: `torture/` directory
   - Scenarios: kill -9, disk full, corruption, network partition, clock skew
   - Target: 1,000,000+ iterations without data loss

### Integration Architecture

mtlog-audit integrates with mtlog by implementing the `core.LogEventSink` interface:

```go
// From mtlog/core
type LogEventSink interface {
    Emit(event *LogEvent) error
}

// mtlog-audit implementation
type Sink struct {
    // ... internal fields
}

func (s *Sink) Emit(event *core.LogEvent) error {
    // Guaranteed delivery implementation
}
```

Usage pattern:
```go
auditSink, _ := audit.New(
    audit.WithWAL("/var/audit/app.wal"),
    audit.WithCompliance("HIPAA"),
)
logger := mtlog.New(mtlog.WithSink(auditSink))
```

### Implementation Phases

The design document (`docs/design.md`) outlines 10 implementation phases:

1. **Foundation** (Days 1-2): Repository setup, interfaces, basic sink
2. **WAL Implementation** (Days 3-5): Record format, segments, integrity
3. **Recovery System** (Days 6-7): Corruption detection and recovery
4. **Compliance** (Days 8-9): Profiles, encryption, signing
5. **Backends** (Days 10-11): Storage backends, replication
6. **Torture Testing** (Days 12-13): Test framework and scenarios
7. **CLI Tools** (Days 14-15): Management commands
8. **Documentation** (Day 16): Comprehensive docs
9. **Performance** (Days 17-18): Optimization, benchmarking
10. **Polish** (Days 19-20): Metrics, monitoring, final testing

### Key Design Principles

1. **Correctness over Performance**: Every write must be durable (O_SYNC, fsync)
2. **Fail Loud**: Never fail silently; panic on critical failures if configured
3. **Conservative Defaults**: Maximum durability by default
4. **Version Everything**: File formats include version numbers for compatibility
5. **Test Everything**: Especially failure scenarios through torture testing

### Performance Targets

- **Zero data loss** in 10M+ torture test iterations
- **< 5ms P99 latency** at 10,000 events/second
- **99.999% recovery rate** from corruption
- **100% compliance** validation for all profiles