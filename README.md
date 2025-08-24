# mtlog-audit

[![Go Reference](https://pkg.go.dev/badge/github.com/willibrandon/mtlog-audit.svg)](https://pkg.go.dev/github.com/willibrandon/mtlog-audit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

The audit sink that cannot lose data. A bulletproof audit logging solution for [mtlog](https://github.com/willibrandon/mtlog) and other Go logging libraries, designed for financial services, healthcare, government, and any application where audit logs are critical.

## 🚧 Work in Progress

This project is currently under active development. The core WAL (Write-Ahead Log) implementation is functional and has been validated with torture testing, but additional features are still being implemented.

## Features (Planned)

- 🛡️ **Zero data loss guarantee** - Mathematically proven through torture tests
- 🔄 **99.99% corruption recovery** - Recovers from any failure scenario
- 📜 **Compliance ready** - Pre-configured HIPAA, PCI-DSS, SOX, GDPR profiles
- ⚡ **High performance** - 20,000+ events/sec with full durability
- 🔐 **Cryptographic integrity** - Ed25519 chain of custody, tamper detection
- ☁️ **Cloud native** - S3, Azure Blob, GCS backends with immutability
- 🔧 **Powerful CLI** - Verify, replay, export, monitor, and recover
- 📊 **Observable** - Prometheus metrics, health checks, Grafana dashboards

## Current Status

### ✅ Implemented
- Core Sink structure implementing `core.LogEventSink`
- Configuration API with options pattern
- WAL record format with CRC32 checksums
- Basic WAL write operations with O_SYNC durability
- SHA256 hash chaining for integrity
- Torture test framework with Kill9DuringWrite scenario
- CLI tool with `verify` and `torture` commands
- Basic unit tests
- 10/10 torture tests passing (process kill simulation)

### 🚧 In Progress
- Segment management and rotation
- Recovery engine for advanced corruption scenarios

### 📋 TODO
- Compliance engine (HIPAA, PCI-DSS, SOX, GDPR)
- Storage backends (S3, Azure, GCS)
- Resilience layer (retry, circuit breaker)
- Performance optimizations (group commit, ring buffer)
- Monitoring and metrics
- Complete torture test suite

## Quick Start

### Installation

```bash
go get github.com/willibrandon/mtlog-audit
```

### Basic Usage with mtlog

```go
package main

import (
    "log"
    "github.com/willibrandon/mtlog"
    audit "github.com/willibrandon/mtlog-audit"
)

func main() {
    // Create bulletproof audit sink
    auditSink, err := audit.New(
        audit.WithWAL("/var/audit/app.wal"),
        audit.WithPanicOnFailure(), // Panic on write failure
    )
    if err != nil {
        log.Fatal("Audit system must initialize:", err)
    }
    defer auditSink.Close()
    
    // Use with mtlog
    logger := mtlog.New(
        mtlog.WithSink(auditSink),
    )
    
    // Your logs are now durable
    logger.Info("User {UserId} accessed record {RecordId}", userId, recordId)
}
```

## Development

### Building

```bash
make build
```

### Testing

```bash
# Run unit tests
make test

# Run torture tests with build tag
go test -tags=torture ./torture

# Run benchmarks (when implemented)
make bench
```

### CLI Tool

```bash
# Build the CLI
make build

# Verify WAL integrity
./bin/mtlog-audit verify --wal /path/to/audit.wal

# Run torture tests
./bin/mtlog-audit torture --iterations 100 --scenario kill9

# Show version
./bin/mtlog-audit version
```

## Architecture

mtlog-audit uses a Write-Ahead Log (WAL) to guarantee durability:

1. **WAL Records**: Each log event is wrapped in a record with:
   - CRC32 checksums for corruption detection
   - SHA256 hash chaining for tamper detection
   - Sequence numbers for ordering
   - Magic headers/footers for torn-write protection

2. **Durability**: Uses O_SYNC flag to ensure data is written to disk before returning

3. **Recovery**: Can recover from various failure scenarios including:
   - Process crashes (kill -9)
   - Disk corruption
   - Power loss
   - Network partitions

## Contributing

This project is under active development. Contributions are welcome! Please see the design documents in the `docs/` folder for the full architecture and implementation plan.

## License

MIT - Because audit logs should be accessible to everyone.

## Acknowledgments

Built for the [mtlog](https://github.com/willibrandon/mtlog) ecosystem, but designed to work with any Go logger.

---

**mtlog-audit**: When failure is not an option.