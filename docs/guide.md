# mtlog-audit Implementation Guide for Claude Code

## Overview

This guide provides step-by-step instructions for Claude Code to implement mtlog-audit, a bulletproof audit logging sink for Go. The implementation should prioritize correctness, durability, and testability.

---

## Phase 1: Project Setup and Core Structure

### Step 1.1: Initialize Repository

```bash
# Create the repository structure
mkdir mtlog-audit
cd mtlog-audit
git init

# Initialize Go module
go mod init github.com/willibrandon/mtlog-audit

# Create initial directory structure
mkdir -p {wal,compliance,backends,resilience,performance,monitoring,torture,cmd/mtlog-audit,examples,docs,scripts,docker,assets}

# Add mtlog as a dependency
go get github.com/willibrandon/mtlog

# Create essential files
touch README.md LICENSE Makefile .gitignore
```

### Step 1.2: Create Makefile

```makefile
# Makefile
.PHONY: build test bench torture clean install

VERSION := $(shell git describe --tags --always --dirty)
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
```

### Step 1.3: Create Core Interfaces

```go
// errors.go - Define error types first
package audit

import "errors"

var (
    ErrSinkClosed       = errors.New("audit sink is closed")
    ErrWALCorrupted     = errors.New("WAL corruption detected")
    ErrWriteFailed      = errors.New("write failed")
    ErrIntegrityFailed  = errors.New("integrity check failed")
    ErrComplianceViolation = errors.New("compliance violation")
)

// version.go
package audit

var (
    Version   = "dev"
    BuildTime = "unknown"
)
```

---

## Phase 2: Write-Ahead Log Implementation

### Step 2.1: WAL Record Format

```go
// wal/record.go
package wal

import (
    "encoding/binary"
    "hash/crc32"
    "time"
)

const (
    MagicHeader = 0x4D544C47 // "MTLG"
    MagicFooter = 0x454E4452 // "ENDR"
    Version     = 1
)

type Record struct {
    // Header
    Magic       uint32
    Version     uint16
    Flags       uint16
    Length      uint32
    Timestamp   int64
    CRC32Header uint32
    
    // Payload
    Sequence    uint64
    PrevHash    [32]byte
    EventData   []byte
    
    // Footer
    CRC32Data   uint32
    MagicEnd    uint32
}

func (r *Record) Marshal() ([]byte, error) {
    // TODO: Implement serialization with CRC calculation
    // Use binary.LittleEndian for all fields
    // Calculate CRC32C for header and full record
    return nil, nil
}

func UnmarshalRecord(data []byte) (*Record, error) {
    // TODO: Implement deserialization with CRC verification
    // Verify magic numbers
    // Check CRCs
    // Return error if corrupted
    return nil, nil
}
```

### Step 2.2: WAL Core Implementation

```go
// wal/wal.go
package wal

import (
    "fmt"
    "os"
    "sync"
    "sync/atomic"
    "time"
    
    "github.com/willibrandon/mtlog/core"
)

type WAL struct {
    mu           sync.Mutex
    path         string
    file         *os.File
    sequence     uint64
    lastHash     [32]byte
    segmentSize  int64
    currentSize  int64
    syncMode     SyncMode
    closed       atomic.Bool
}

type SyncMode int

const (
    SyncImmediate SyncMode = iota // fsync after every write
    SyncInterval                   // fsync periodically
    SyncBatch                      // fsync after batch
)

type Option func(*config) error

type config struct {
    segmentSize int64
    syncMode    SyncMode
    syncInterval time.Duration
}

func New(path string, opts ...Option) (*WAL, error) {
    cfg := &config{
        segmentSize: 4 * 1024 * 1024, // 4MB default
        syncMode:    SyncImmediate,
    }
    
    for _, opt := range opts {
        if err := opt(cfg); err != nil {
            return nil, err
        }
    }
    
    // TODO: Implement WAL initialization
    // 1. Create directory if needed
    // 2. Open or create WAL file with O_SYNC for durability
    // 3. Recover from existing WAL if present
    // 4. Initialize sequence number and last hash
    
    return nil, nil
}

func (w *WAL) Write(event *core.LogEvent) error {
    if w.closed.Load() {
        return fmt.Errorf("WAL is closed")
    }
    
    w.mu.Lock()
    defer w.mu.Unlock()
    
    // TODO: Implement write with torn-write protection
    // 1. Increment sequence
    // 2. Calculate hash chain
    // 3. Create record
    // 4. Write record with CRC
    // 5. Fsync based on sync mode
    // 6. Update segment size
    // 7. Rotate if needed
    
    return nil
}

func (w *WAL) VerifyIntegrity() error {
    // TODO: Implement integrity verification
    // 1. Read all records
    // 2. Verify CRCs
    // 3. Verify hash chain
    // 4. Check sequence gaps
    return nil
}

func (w *WAL) Close() error {
    if !w.closed.CompareAndSwap(false, true) {
        return nil
    }
    
    w.mu.Lock()
    defer w.mu.Unlock()
    
    if w.file != nil {
        w.file.Sync()
        return w.file.Close()
    }
    
    return nil
}
```

### Step 2.3: Segment Management

```go
// wal/segment.go
package wal

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
    "sort"
    "time"
)

type Segment struct {
    Path      string
    StartSeq  uint64
    EndSeq    uint64
    Size      int64
    CreatedAt time.Time
    Sealed    bool
}

type SegmentManager struct {
    baseDir     string
    segments    []*Segment
    activeIndex int
}

func NewSegmentManager(baseDir string) (*SegmentManager, error) {
    // TODO: Implement segment manager
    // 1. Create base directory
    // 2. Scan for existing segments
    // 3. Sort by sequence number
    // 4. Identify active segment
    return nil, nil
}

func (sm *SegmentManager) RotateSegment() error {
    // TODO: Implement segment rotation
    // 1. Seal current segment
    // 2. Create new segment file
    // 3. Update active index
    return nil
}

func (sm *SegmentManager) CompactSegments(olderThan time.Duration) error {
    // TODO: Implement segment compaction
    // 1. Identify segments older than threshold
    // 2. Merge small segments
    // 3. Remove redundant data
    // 4. Update index
    return nil
}
```

---

## Phase 3: Corruption Recovery

### Step 3.1: Recovery Engine

```go
// wal/recovery.go
package wal

import (
    "bytes"
    "encoding/binary"
    "fmt"
    "io"
)

type RecoveryEngine struct {
    wal *WAL
}

type CorruptionType int

const (
    CorruptionNone CorruptionType = iota
    CorruptionCRC
    CorruptionTornWrite
    CorruptionMissingData
    CorruptionHashChain
)

type RecoveryReport struct {
    TotalRecords      int
    CorruptedRecords  int
    RecoveredRecords  int
    UnrecoverableRecords int
    CorruptionTypes   map[CorruptionType]int
}

func NewRecoveryEngine(wal *WAL) *RecoveryEngine {
    return &RecoveryEngine{wal: wal}
}

func (re *RecoveryEngine) Recover() (*RecoveryReport, error) {
    // TODO: Implement multi-level recovery
    // Level 1: CRC validation and repair
    // Level 2: Torn write detection and recovery
    // Level 3: Hash chain reconstruction
    // Level 4: Forensic recovery from partial data
    
    report := &RecoveryReport{
        CorruptionTypes: make(map[CorruptionType]int),
    }
    
    // Scan for corruption
    corruptions := re.scanForCorruption()
    
    // Attempt recovery for each corruption
    for _, corruption := range corruptions {
        if re.attemptRecovery(corruption) {
            report.RecoveredRecords++
        } else {
            report.UnrecoverableRecords++
        }
    }
    
    return report, nil
}

func (re *RecoveryEngine) scanForCorruption() []Corruption {
    // TODO: Implement corruption scanning
    // 1. Read segments sequentially
    // 2. Validate each record
    // 3. Identify corruption patterns
    return nil
}

func (re *RecoveryEngine) attemptRecovery(c Corruption) bool {
    // TODO: Implement recovery strategies
    switch c.Type {
    case CorruptionCRC:
        return re.recoverFromCRCError(c)
    case CorruptionTornWrite:
        return re.recoverFromTornWrite(c)
    case CorruptionHashChain:
        return re.reconstructHashChain(c)
    default:
        return false
    }
}

func (re *RecoveryEngine) recoverFromCRCError(c Corruption) bool {
    // TODO: Implement CRC recovery
    // Try single-bit flip corrections
    // Use redundant data if available
    return false
}

func (re *RecoveryEngine) recoverFromTornWrite(c Corruption) bool {
    // TODO: Implement torn write recovery
    // Find partial record boundaries
    // Reconstruct from shadow copies
    return false
}
```

---

## Phase 4: Main Sink Implementation

### Step 4.1: Core Sink

```go
// sink.go
package audit

import (
    "fmt"
    "sync"
    "time"
    
    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit/wal"
    "github.com/willibrandon/mtlog-audit/compliance"
    "github.com/willibrandon/mtlog-audit/backends"
    "github.com/willibrandon/mtlog-audit/resilience"
    "github.com/willibrandon/mtlog-audit/monitoring"
)

// Sink implements core.LogEventSink for bulletproof audit logging
type Sink struct {
    mu         sync.RWMutex
    wal        *wal.WAL
    compliance *compliance.Engine
    backends   []backends.Backend
    resilience *resilience.Manager
    monitor    *monitoring.Monitor
    config     *Config
    closed     bool
}

// Ensure we implement the interface
var _ core.LogEventSink = (*Sink)(nil)

// New creates a new audit sink
func New(opts ...Option) (*Sink, error) {
    config := defaultConfig()
    
    for _, opt := range opts {
        if err := opt(config); err != nil {
            return nil, fmt.Errorf("configuration error: %w", err)
        }
    }
    
    if err := config.validate(); err != nil {
        return nil, fmt.Errorf("invalid configuration: %w", err)
    }
    
    // Initialize WAL first - this is critical
    walInstance, err := wal.New(config.WALPath, config.WALOptions...)
    if err != nil {
        return nil, fmt.Errorf("WAL initialization failed: %w", err)
    }
    
    // Verify WAL integrity immediately
    if err := walInstance.VerifyIntegrity(); err != nil {
        walInstance.Close()
        return nil, fmt.Errorf("WAL integrity check failed: %w", err)
    }
    
    sink := &Sink{
        wal:     walInstance,
        config:  config,
        monitor: monitoring.New(),
    }
    
    // Initialize optional components
    if config.ComplianceProfile != "" {
        sink.compliance, err = compliance.New(config.ComplianceProfile)
        if err != nil {
            sink.cleanup()
            return nil, fmt.Errorf("compliance initialization failed: %w", err)
        }
    }
    
    // Initialize backends
    for _, cfg := range config.Backends {
        backend, err := backends.Create(cfg)
        if err != nil {
            sink.cleanup()
            return nil, fmt.Errorf("backend creation failed: %w", err)
        }
        sink.backends = append(sink.backends, backend)
    }
    
    // Initialize resilience manager
    sink.resilience = resilience.New(
        resilience.WithRetryPolicy(config.RetryPolicy),
        resilience.WithCircuitBreaker(config.CircuitBreakerOptions...),
    )
    
    // Start monitoring
    sink.monitor.Start()
    
    return sink, nil
}

// Emit implements core.LogEventSink
func (s *Sink) Emit(event *core.LogEvent) error {
    s.mu.RLock()
    if s.closed {
        s.mu.RUnlock()
        return ErrSinkClosed
    }
    s.mu.RUnlock()
    
    start := time.Now()
    defer func() {
        s.monitor.RecordLatency(time.Since(start))
    }()
    
    // Apply compliance transformations
    if s.compliance != nil {
        event = s.compliance.Transform(event)
    }
    
    // Write to WAL with resilience
    err := s.resilience.Execute(func() error {
        return s.wal.Write(event)
    })
    
    if err != nil {
        s.monitor.RecordError(err)
        if s.config.FailureHandler != nil {
            s.config.FailureHandler(event, err)
        }
        if s.config.PanicOnFailure {
            panic(fmt.Sprintf("audit write failed: %v", err))
        }
        return err
    }
    
    // Async replication to backends
    if len(s.backends) > 0 {
        go s.replicateToBackends(event)
    }
    
    s.monitor.IncrementEventCount()
    return nil
}

// Close gracefully shuts down the sink
func (s *Sink) Close() error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if s.closed {
        return nil
    }
    
    s.closed = true
    
    // Flush WAL
    if err := s.wal.Flush(); err != nil {
        return fmt.Errorf("WAL flush failed: %w", err)
    }
    
    // Close all components
    var errs []error
    
    if err := s.wal.Close(); err != nil {
        errs = append(errs, err)
    }
    
    for _, backend := range s.backends {
        if err := backend.Close(); err != nil {
            errs = append(errs, err)
        }
    }
    
    s.monitor.Stop()
    
    if len(errs) > 0 {
        return fmt.Errorf("close errors: %v", errs)
    }
    
    return nil
}

func (s *Sink) cleanup() {
    if s.wal != nil {
        s.wal.Close()
    }
    for _, backend := range s.backends {
        backend.Close()
    }
    if s.monitor != nil {
        s.monitor.Stop()
    }
}

func (s *Sink) replicateToBackends(event *core.LogEvent) {
    for _, backend := range s.backends {
        if err := backend.Write(event); err != nil {
            s.monitor.RecordBackendError(backend.Name(), err)
        }
    }
}
```

### Step 4.2: Configuration Options

```go
// options.go
package audit

import (
    "fmt"
    "time"
    
    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit/wal"
)

type Option func(*Config) error

type Config struct {
    WALPath           string
    WALOptions        []wal.Option
    ComplianceProfile string
    Backends          []BackendConfig
    RetryPolicy       RetryPolicy
    CircuitBreakerOptions []interface{}
    FailureHandler    FailureHandler
    PanicOnFailure    bool
}

type FailureHandler func(event *core.LogEvent, err error)

type RetryPolicy struct {
    MaxAttempts int
    InitialDelay time.Duration
    MaxDelay     time.Duration
    Multiplier   float64
}

func defaultConfig() *Config {
    return &Config{
        WALPath: "/var/audit/mtlog.wal",
        RetryPolicy: RetryPolicy{
            MaxAttempts:  3,
            InitialDelay: 100 * time.Millisecond,
            MaxDelay:     5 * time.Second,
            Multiplier:   2.0,
        },
    }
}

func (c *Config) validate() error {
    if c.WALPath == "" {
        return fmt.Errorf("WAL path is required")
    }
    return nil
}

// Option functions
func WithWAL(path string, opts ...wal.Option) Option {
    return func(c *Config) error {
        c.WALPath = path
        c.WALOptions = opts
        return nil
    }
}

func WithCompliance(profile string) Option {
    return func(c *Config) error {
        c.ComplianceProfile = profile
        return nil
    }
}

func WithFailureHandler(handler FailureHandler) Option {
    return func(c *Config) error {
        c.FailureHandler = handler
        return nil
    }
}

func WithPanicOnFailure() Option {
    return func(c *Config) error {
        c.PanicOnFailure = true
        return nil
    }
}
```

---

## Phase 5: Torture Testing

### Step 5.1: Test Framework

```go
// torture/suite.go
package torture

import (
    "context"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"
    "sync"
    "time"
    
    "github.com/willibrandon/mtlog-audit"
)

type Suite struct {
    scenarios []Scenario
    config    Config
}

type Scenario interface {
    Name() string
    Execute(sink *audit.Sink, dir string) error
    Verify(dir string) error
}

type Config struct {
    Iterations     int
    StopOnFailure  bool
    TempDir        string
    Concurrency    int
}

type Report struct {
    StartTime  time.Time
    EndTime    time.Time
    Iterations int
    Scenarios  map[string]*ScenarioResult
    Success    bool
}

type ScenarioResult struct {
    Passed   int
    Failed   int
    Errors   []error
}

func NewSuite(cfg Config) *Suite {
    return &Suite{
        config: cfg,
        scenarios: []Scenario{
            &Kill9DuringWrite{},
            &DiskFull99Percent{},
            &RandomCorruption{},
            &ClockJumpBackward{},
            &NetworkPartition{},
            &PowerLossSimulation{},
        },
    }
}

func (s *Suite) Run() (*Report, error) {
    report := &Report{
        StartTime:  time.Now(),
        Iterations: s.config.Iterations,
        Scenarios:  make(map[string]*ScenarioResult),
    }
    
    for _, scenario := range s.scenarios {
        report.Scenarios[scenario.Name()] = &ScenarioResult{}
    }
    
    for i := 0; i < s.config.Iterations; i++ {
        for _, scenario := range s.scenarios {
            if err := s.runScenario(scenario, report); err != nil {
                if s.config.StopOnFailure {
                    report.EndTime = time.Now()
                    return report, err
                }
            }
        }
        
        if i%100 == 0 {
            fmt.Printf("Progress: %d/%d iterations\n", i, s.config.Iterations)
        }
    }
    
    report.EndTime = time.Now()
    report.Success = s.calculateSuccess(report)
    
    return report, nil
}

func (s *Suite) runScenario(scenario Scenario, report *Report) error {
    // Create isolated test directory
    dir, err := ioutil.TempDir(s.config.TempDir, "torture-")
    if err != nil {
        return err
    }
    defer os.RemoveAll(dir)
    
    // Create sink
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return err
    }
    
    // Execute scenario
    if err := scenario.Execute(sink, dir); err != nil {
        report.Scenarios[scenario.Name()].Failed++
        report.Scenarios[scenario.Name()].Errors = append(
            report.Scenarios[scenario.Name()].Errors, err)
        return err
    }
    
    // Close sink
    sink.Close()
    
    // Verify results
    if err := scenario.Verify(dir); err != nil {
        report.Scenarios[scenario.Name()].Failed++
        report.Scenarios[scenario.Name()].Errors = append(
            report.Scenarios[scenario.Name()].Errors, err)
        return err
    }
    
    report.Scenarios[scenario.Name()].Passed++
    return nil
}

func (s *Suite) calculateSuccess(report *Report) bool {
    for _, result := range report.Scenarios {
        if result.Failed > 0 {
            return false
        }
    }
    return true
}
```

### Step 5.2: Example Torture Scenario

```go
// torture/scenarios/kill9.go
package scenarios

import (
    "fmt"
    "os"
    "path/filepath"
    "syscall"
    "time"
    
    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit"
)

type Kill9DuringWrite struct{}

func (k *Kill9DuringWrite) Name() string {
    return "Kill9DuringWrite"
}

func (k *Kill9DuringWrite) Execute(sink *audit.Sink, dir string) error {
    // Write events in background
    done := make(chan error, 1)
    go func() {
        for i := 0; i < 1000; i++ {
            event := &core.LogEvent{
                Timestamp: time.Now(),
                Level:     core.InformationLevel,
                MessageTemplate: &core.MessageTemplate{
                    Text: fmt.Sprintf("Test event %d", i),
                },
            }
            
            if err := sink.Emit(event); err != nil {
                done <- err
                return
            }
            
            // Simulate random timing
            if i == 500 {
                // Simulate abrupt termination
                sink.Close()
                done <- nil
                return
            }
        }
        done <- nil
    }()
    
    // Wait for completion or timeout
    select {
    case err := <-done:
        return err
    case <-time.After(5 * time.Second):
        return fmt.Errorf("timeout")
    }
}

func (k *Kill9DuringWrite) Verify(dir string) error {
    // Reopen and verify
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()
    
    // Verify integrity
    report, err := sink.VerifyIntegrity()
    if err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }
    
    if !report.Valid {
        return fmt.Errorf("data corruption detected")
    }
    
    return nil
}
```

---

## Phase 6: CLI Tool

### Step 6.1: Main Command

```go
// cmd/mtlog-audit/main.go
package main

import (
    "fmt"
    "os"
    
    "github.com/spf13/cobra"
)

var (
    version = "dev"
    rootCmd = &cobra.Command{
        Use:   "mtlog-audit",
        Short: "Bulletproof audit log management",
        Long:  `mtlog-audit provides tools for managing bulletproof audit logs.`,
    }
)

func main() {
    rootCmd.AddCommand(
        verifyCmd(),
        replayCmd(),
        monitorCmd(),
        recoverCmd(),
        tortureCmd(),
        versionCmd(),
    )
    
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}

func versionCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "version",
        Short: "Print version information",
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Printf("mtlog-audit version %s\n", version)
        },
    }
}
```

### Step 6.2: Verify Command

```go
// cmd/mtlog-audit/commands/verify.go
package commands

import (
    "fmt"
    
    "github.com/spf13/cobra"
    "github.com/willibrandon/mtlog-audit"
)

func verifyCmd() *cobra.Command {
    var walPath string
    
    cmd := &cobra.Command{
        Use:   "verify",
        Short: "Verify audit log integrity",
        RunE: func(cmd *cobra.Command, args []string) error {
            sink, err := audit.New(
                audit.WithWAL(walPath),
            )
            if err != nil {
                return fmt.Errorf("failed to open WAL: %w", err)
            }
            defer sink.Close()
            
            report, err := sink.VerifyIntegrity()
            if err != nil {
                return fmt.Errorf("verification failed: %w", err)
            }
            
            if report.Valid {
                fmt.Println("✅ Integrity check passed")
                fmt.Printf("Total records: %d\n", report.TotalRecords)
            } else {
                fmt.Println("❌ Integrity check failed")
                fmt.Printf("Corrupted segments: %d\n", report.CorruptedSegments)
            }
            
            return nil
        },
    }
    
    cmd.Flags().StringVar(&walPath, "wal", "/var/audit/app.wal", "WAL path")
    
    return cmd
}
```

---

## Implementation Checklist for Claude Code

### Phase 1: Foundation (Day 1-2)
- [ ] Initialize repository structure
- [ ] Set up Go module and dependencies
- [ ] Create Makefile and basic CI
- [ ] Implement error types and interfaces
- [ ] Create basic sink structure that implements `core.LogEventSink`
- [ ] Add basic unit tests

### Phase 2: WAL Implementation (Day 3-5)
- [ ] Implement WAL record format with CRC32
- [ ] Create segment management
- [ ] Implement basic write path with fsync
- [ ] Add torn-write protection
- [ ] Implement integrity verification
- [ ] Add WAL unit tests

### Phase 3: Recovery System (Day 6-7)
- [ ] Implement corruption detection
- [ ] Add CRC recovery
- [ ] Implement torn-write recovery
- [ ] Add hash chain reconstruction
- [ ] Create recovery unit tests

### Phase 4: Compliance Features (Day 8-9)
- [ ] Create compliance profiles (HIPAA, PCI-DSS, SOX)
- [ ] Implement Ed25519 signing
- [ ] Add encryption support
- [ ] Implement retention policies
- [ ] Add compliance tests

### Phase 5: Backends (Day 10-11)
- [ ] Implement filesystem backend
- [ ] Add S3 backend
- [ ] Add Azure backend
- [ ] Implement multi-backend replication
- [ ] Add backend tests

### Phase 6: Torture Testing (Day 12-13)
- [ ] Create torture test framework
- [ ] Implement kill scenario
- [ ] Add corruption scenarios
- [ ] Implement disk full scenario
- [ ] Add network partition scenario
- [ ] Create test reporting

### Phase 7: CLI and Tools (Day 14-15)
- [ ] Implement CLI framework
- [ ] Add verify command
- [ ] Add replay command
- [ ] Add monitor command
- [ ] Add recovery command
- [ ] Create Docker images

### Phase 8: Documentation (Day 16)
- [ ] Write comprehensive README
- [ ] Create architecture documentation
- [ ] Add compliance guides
- [ ] Create examples
- [ ] Add API documentation

### Phase 9: Performance (Day 17-18)
- [ ] Implement group commit
- [ ] Add ring buffer
- [ ] Create benchmarks
- [ ] Optimize hot paths
- [ ] Add performance tests

### Phase 10: Polish (Day 19-20)
- [ ] Add Prometheus metrics
- [ ] Create Grafana dashboards
- [ ] Add integration tests
- [ ] Fix any remaining issues
- [ ] Prepare for release

---

## Testing Strategy

### Unit Tests
```bash
# Run all unit tests
go test ./...

# Run with race detector
go test -race ./...

# Generate coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests
```bash
# Run integration tests
go test -tags=integration ./...
```

### Torture Tests
```bash
# Run quick torture test (1000 iterations)
go test -tags=torture ./torture -count=1000

# Run full torture test (1M iterations)
go test -tags=torture -timeout=24h ./torture -count=1000000
```

### Benchmarks
```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkWALWrite -benchmem ./wal
```

---

## Key Implementation Notes

1. **Prioritize Correctness**: Every write must be durable. Use O_SYNC and fsync liberally.

2. **Handle All Errors**: Never ignore errors. Every error should be logged and handled.

3. **Test Everything**: Each component should have comprehensive tests, especially failure scenarios.

4. **Document Thoroughly**: Every public API should have clear documentation with examples.

5. **Optimize Later**: Get it working correctly first, then optimize based on benchmarks.

6. **Use Conservative Defaults**: Default to maximum durability, let users opt into performance trade-offs.

7. **Fail Loud**: If something goes wrong, make it obvious. Don't fail silently.

8. **Version Everything**: Include version numbers in file formats for future compatibility.

---

## Success Criteria

The implementation is complete when:

1. All torture tests pass 1,000,000 iterations without data loss
2. Benchmarks show < 5ms P99 latency at 10,000 events/second
3. Recovery succeeds for 99.99% of corruption scenarios
4. All compliance profiles validate correctly
5. Documentation is complete and examples work
6. CI/CD pipeline is green
7. Docker images build and run successfully

---

This implementation guide provides Claude Code with a structured approach to building mtlog-audit. The key is to focus on correctness first, with extensive testing at each phase to ensure the "bulletproof" promise is kept.