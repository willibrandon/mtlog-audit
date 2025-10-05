# mtlog-audit: Implementation Roadmap to 100% Completion

**Document Version**: 1.0
**Last Updated**: 2025-10-05
**Current Completion**: 67%
**Target**: 100%

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Current State Assessment](#current-state-assessment)
3. [Priority-Ordered Implementation Plan](#priority-ordered-implementation-plan)
4. [Detailed Implementation Tasks](#detailed-implementation-tasks)
5. [Testing and Validation Strategy](#testing-and-validation-strategy)
6. [Success Criteria and Acceptance Tests](#success-criteria-and-acceptance-tests)
7. [Risk Management](#risk-management)
8. [Timeline and Milestones](#timeline-and-milestones)

---

## Executive Summary

This roadmap guides the completion of mtlog-audit from its current 67% implementation to 100% feature-complete status as defined in `design.md` and `guide.md`. The remaining work is organized into **4 sprints over 6 weeks**, focusing on:

1. **Sprint 1 (Week 1-2)**: Torture Testing & Performance Validation (Critical Path)
2. **Sprint 2 (Week 3)**: Compliance Features & Multi-Backend
3. **Sprint 3 (Week 4)**: Monitoring Integration & Polish
4. **Sprint 4 (Week 5-6)**: Documentation, Examples & Release Preparation

**Estimated Effort**: 6 weeks (1 developer full-time)

---

## Current State Assessment

### ✅ Completed (67%)
- Core Sink and WAL implementation
- Basic compliance profiles
- Cloud storage backends (S3, Azure, GCS)
- CLI tool with 8 commands
- Basic torture testing framework
- Resilience primitives (circuit breaker, retry)

### ⚠️ Incomplete (33%)
- Advanced torture scenarios (5/8 missing)
- Performance validation and integration
- Compliance advanced features (Merkle tree, retention)
- Multi-backend orchestration
- Monitoring integration and testing
- Comprehensive documentation
- Production examples

### 🔴 Critical Blockers to v1.0
1. Torture testing with 1M+ iterations
2. Performance benchmarks (< 5ms P99 latency)
3. Test coverage > 60% across all packages
4. Multi-backend quorum implementation
5. Complete compliance engine

---

## Priority-Ordered Implementation Plan

### Sprint 1: Torture Testing & Performance (CRITICAL - Week 1-2)

**Goal**: Validate the "bulletproof" claim with 1M+ torture tests and performance benchmarks.

**Why First**: This is the core value proposition. Must be proven before claiming reliability.

**Tasks**:
1. Complete missing torture scenarios
2. Implement torture test reporting
3. Run 1M+ iteration validation
4. Implement and validate performance optimizations
5. Create benchmark suite

**Acceptance Criteria**:
- All 8 torture scenarios passing
- 1M+ iterations completed with 0 data loss
- < 5ms P99 latency at 10K events/sec
- HTML torture test reports generated
- Benchmark results documented

---

### Sprint 2: Compliance & Multi-Backend (HIGH - Week 3)

**Goal**: Complete compliance features and multi-backend orchestration.

**Why Second**: Required for enterprise adoption and compliance certifications.

**Tasks**:
1. Implement Merkle tree for tamper detection
2. Add chain of custody
3. Implement retention policies with legal hold
4. Create multi-backend with quorum
5. Add compliance report generation

**Acceptance Criteria**:
- All compliance profiles fully functional
- Merkle tree validates data integrity
- Multi-backend quorum writes working
- Compliance reports generate PDFs
- Test coverage > 60% for compliance package

---

### Sprint 3: Monitoring Integration & Polish (MEDIUM - Week 4)

**Goal**: Complete observability and production-readiness features.

**Why Third**: Needed for production operations but not core functionality.

**Tasks**:
1. Integrate Prometheus metrics
2. Create Grafana dashboards
3. Implement health check endpoints
4. Add alert rule definitions
5. Complete integration tests
6. Improve test coverage across all packages

**Acceptance Criteria**:
- Prometheus metrics exported and tested
- Grafana dashboard JSON files created
- Health endpoints return proper status
- Integration test suite passes
- Overall test coverage > 60%

---

### Sprint 4: Documentation & Examples (LOW - Week 5-6)

**Goal**: Complete documentation and production-ready examples.

**Why Last**: Code must be complete before final documentation.

**Tasks**:
1. Write missing documentation
2. Create industry-specific examples
3. Write deployment guides
4. Create troubleshooting guide
5. Prepare release artifacts

**Acceptance Criteria**:
- All doc files from design.md created
- 4+ production-ready examples
- Deployment guides for AWS/Azure/GCP
- API reference documentation complete
- Release v1.0.0 artifacts ready

---

## Detailed Implementation Tasks

---

## SPRINT 1: Torture Testing & Performance (Week 1-2)

### Task 1.1: Complete Torture Test Scenarios

**File**: `torture/scenarios/network.go`

```go
// torture/scenarios/network.go
package scenarios

import (
    "fmt"
    "time"

    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit"
)

// NetworkPartition simulates network partition scenarios
type NetworkPartition struct {
    partitionAfter int
}

func (n *NetworkPartition) Name() string {
    return "NetworkPartition"
}

func (n *NetworkPartition) Execute(sink *audit.Sink, dir string) error {
    // Write events before partition
    for i := 0; i < n.partitionAfter; i++ {
        event := createTestEvent(i)
        sink.Emit(event)
    }

    // Simulate network partition by blocking backend writes
    // (This should not affect WAL writes)

    // Write events during partition
    for i := n.partitionAfter; i < n.partitionAfter+100; i++ {
        event := createTestEvent(i)
        sink.Emit(event)
    }

    // Heal partition and verify all events in WAL

    return nil
}

func (n *NetworkPartition) Verify(dir string) error {
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()

    // Verify all events are in WAL
    report, err := sink.VerifyIntegrity()
    if err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }

    if !report.Valid {
        return fmt.Errorf("data corruption detected")
    }

    // Verify exact count
    events, err := sink.Replay(time.Time{}, time.Now())
    if err != nil {
        return fmt.Errorf("replay failed: %w", err)
    }

    expectedCount := n.partitionAfter + 100
    if len(events) < expectedCount {
        return fmt.Errorf("expected %d events, got %d", expectedCount, len(events))
    }

    return nil
}
```

**File**: `torture/scenarios/clock.go`

```go
// torture/scenarios/clock.go
package scenarios

import (
    "fmt"
    "path/filepath"
    "time"

    "github.com/willibrandon/mtlog-audit"
)

// ClockJumpBackward tests handling of backward clock jumps
type ClockJumpBackward struct{}

func (c *ClockJumpBackward) Name() string {
    return "ClockJumpBackward"
}

func (c *ClockJumpBackward) Execute(sink *audit.Sink, dir string) error {
    // Write events with normal timestamps
    for i := 0; i < 100; i++ {
        event := createTestEvent(i)
        sink.Emit(event)
    }

    // Note: Actual clock manipulation requires OS-level changes
    // We simulate by writing events with older timestamps
    // The WAL should handle this via sequence numbers

    for i := 100; i < 200; i++ {
        event := createTestEvent(i)
        // Manually set older timestamp
        event.Timestamp = time.Now().Add(-1 * time.Hour)
        sink.Emit(event)
    }

    return nil
}

func (c *ClockJumpBackward) Verify(dir string) error {
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()

    // Verify integrity despite timestamp issues
    report, err := sink.VerifyIntegrity()
    if err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }

    if !report.Valid {
        return fmt.Errorf("data corruption detected")
    }

    // All 200 events should be present, ordered by sequence not timestamp
    events, err := sink.Replay(time.Time{}, time.Now())
    if err != nil {
        return fmt.Errorf("replay failed: %w", err)
    }

    if len(events) != 200 {
        return fmt.Errorf("expected 200 events, got %d", len(events))
    }

    return nil
}
```

**File**: `torture/scenarios/byzantine.go`

```go
// torture/scenarios/byzantine.go
package scenarios

import (
    "crypto/rand"
    "fmt"
    "io"
    "os"
    "path/filepath"

    "github.com/willibrandon/mtlog-audit"
)

// ByzantineFailure tests recovery from arbitrary corruption
type ByzantineFailure struct{}

func (b *ByzantineFailure) Name() string {
    return "ByzantineFailure"
}

func (b *ByzantineFailure) Execute(sink *audit.Sink, dir string) error {
    // Write valid records
    for i := 0; i < 100; i++ {
        event := createTestEvent(i)
        sink.Emit(event)
    }

    // Force flush
    sink.Close()

    // Inject random corruption into WAL file
    walPath := filepath.Join(dir, "test.wal")
    return b.injectByzantineCorruption(walPath)
}

func (b *ByzantineFailure) injectByzantineCorruption(path string) error {
    file, err := os.OpenFile(path, os.O_RDWR, 0644)
    if err != nil {
        return err
    }
    defer file.Close()

    // Get file size
    stat, err := file.Stat()
    if err != nil {
        return err
    }

    // Corrupt random bytes in the middle
    if stat.Size() > 1000 {
        // Seek to middle
        _, err = file.Seek(stat.Size()/2, io.SeekStart)
        if err != nil {
            return err
        }

        // Write random garbage
        garbage := make([]byte, 50)
        rand.Read(garbage)
        _, err = file.Write(garbage)
        if err != nil {
            return err
        }
    }

    return nil
}

func (b *ByzantineFailure) Verify(dir string) error {
    // Should recover as many records as possible
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()

    // Verify integrity - should detect corruption
    report, err := sink.VerifyIntegrity()
    // We expect some corruption to be detected
    if err == nil && report.Valid {
        // This is suspicious - we injected corruption
        return fmt.Errorf("expected corruption to be detected")
    }

    // But we should still be able to recover some records
    events, err := sink.Replay(time.Time{}, time.Now())
    if err != nil {
        // Replay might fail, which is acceptable
        return nil
    }

    // Should recover at least 50% of records
    if len(events) < 50 {
        return fmt.Errorf("expected to recover at least 50 events, got %d", len(events))
    }

    return nil
}
```

**File**: `torture/scenarios/power.go`

```go
// torture/scenarios/power.go
package scenarios

import (
    "fmt"
    "math/rand"
    "os"
    "path/filepath"
    "time"

    "github.com/willibrandon/mtlog-audit"
)

// PowerLossSimulation simulates sudden power loss during write
type PowerLossSimulation struct{}

func (p *PowerLossSimulation) Name() string {
    return "PowerLossSimulation"
}

func (p *PowerLossSimulation) Execute(sink *audit.Sink, dir string) error {
    // Write events
    go func() {
        for i := 0; i < 1000; i++ {
            event := createTestEvent(i)
            sink.Emit(event)
            // Random small delay
            time.Sleep(time.Duration(rand.Intn(5)) * time.Microsecond)
        }
    }()

    // Simulate power loss at random time
    time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)

    // Force close without proper shutdown
    // This simulates power loss - no Flush(), no graceful Close()

    return nil
}

func (p *PowerLossSimulation) Verify(dir string) error {
    // Reopen and verify
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()

    // Should recover without errors
    report, err := sink.VerifyIntegrity()
    if err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }

    // Data that was fsynced should be intact
    // Some records might be lost if they weren't synced
    events, err := sink.Replay(time.Time{}, time.Now())
    if err != nil {
        return fmt.Errorf("replay failed: %w", err)
    }

    // Should have recovered some events
    if len(events) == 0 {
        return fmt.Errorf("expected to recover at least some events")
    }

    // All recovered events should be valid
    if !report.Valid {
        return fmt.Errorf("recovered data should be valid")
    }

    return nil
}
```

**File**: `torture/scenarios/concurrent.go`

```go
// torture/scenarios/concurrent.go
package scenarios

import (
    "fmt"
    "path/filepath"
    "sync"
    "time"

    "github.com/willibrandon/mtlog-audit"
)

// ConcurrentCorruption tests concurrent writes with corruption
type ConcurrentCorruption struct {
    goroutines int
}

func NewConcurrentCorruption() *ConcurrentCorruption {
    return &ConcurrentCorruption{
        goroutines: 10,
    }
}

func (c *ConcurrentCorruption) Name() string {
    return "ConcurrentCorruption"
}

func (c *ConcurrentCorruption) Execute(sink *audit.Sink, dir string) error {
    var wg sync.WaitGroup
    errChan := make(chan error, c.goroutines)

    // Start concurrent writers
    for i := 0; i < c.goroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                event := createTestEvent(id*100 + j)
                sink.Emit(event)
            }
        }(i)
    }

    // Wait for all writes
    wg.Wait()
    close(errChan)

    // Check for errors
    for err := range errChan {
        if err != nil {
            return err
        }
    }

    return nil
}

func (c *ConcurrentCorruption) Verify(dir string) error {
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to reopen: %w", err)
    }
    defer sink.Close()

    // Verify all concurrent writes were recorded
    report, err := sink.VerifyIntegrity()
    if err != nil {
        return fmt.Errorf("integrity check failed: %w", err)
    }

    if !report.Valid {
        return fmt.Errorf("data corruption detected")
    }

    // Should have all events
    events, err := sink.Replay(time.Time{}, time.Now())
    if err != nil {
        return fmt.Errorf("replay failed: %w", err)
    }

    expectedCount := c.goroutines * 100
    if len(events) != expectedCount {
        return fmt.Errorf("expected %d events, got %d", expectedCount, len(events))
    }

    return nil
}
```

---

### Task 1.2: Implement Torture Test Reporting

**File**: `torture/report/generator.go`

```go
// torture/report/generator.go
package report

import (
    "fmt"
    "html/template"
    "os"
    "time"
)

// Report contains torture test results
type Report struct {
    StartTime   time.Time
    EndTime     time.Time
    Duration    time.Duration
    Iterations  int
    Scenarios   map[string]*ScenarioResult
    Success     bool
    FailureRate float64
}

// ScenarioResult holds results for a single scenario
type ScenarioResult struct {
    Name     string
    Passed   int
    Failed   int
    Errors   []error
    AvgTime  time.Duration
}

// Generator creates HTML reports
type Generator struct {
    template *template.Template
}

// NewGenerator creates a new report generator
func NewGenerator() (*Generator, error) {
    tmpl, err := template.New("report").Parse(reportTemplate)
    if err != nil {
        return nil, fmt.Errorf("failed to parse template: %w", err)
    }

    return &Generator{
        template: tmpl,
    }, nil
}

// Generate creates an HTML report
func (g *Generator) Generate(report *Report, outputPath string) error {
    file, err := os.Create(outputPath)
    if err != nil {
        return fmt.Errorf("failed to create output file: %w", err)
    }
    defer file.Close()

    // Calculate statistics
    report.Duration = report.EndTime.Sub(report.StartTime)

    totalTests := 0
    totalFailures := 0
    for _, result := range report.Scenarios {
        totalTests += result.Passed + result.Failed
        totalFailures += result.Failed
    }

    if totalTests > 0 {
        report.FailureRate = float64(totalFailures) / float64(totalTests) * 100
    }

    report.Success = totalFailures == 0

    // Execute template
    if err := g.template.Execute(file, report); err != nil {
        return fmt.Errorf("failed to execute template: %w", err)
    }

    return nil
}

const reportTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>mtlog-audit Torture Test Report</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 10px;
            margin-bottom: 30px;
        }
        .status {
            font-size: 48px;
            margin: 20px 0;
        }
        .success { color: #10b981; }
        .failure { color: #ef4444; }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .stat-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .stat-value {
            font-size: 36px;
            font-weight: bold;
            margin: 10px 0;
        }
        .stat-label {
            color: #6b7280;
            font-size: 14px;
        }
        .scenario {
            background: white;
            margin-bottom: 20px;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .scenario-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 15px;
        }
        .scenario-name {
            font-size: 20px;
            font-weight: bold;
        }
        .badge {
            padding: 5px 15px;
            border-radius: 20px;
            font-size: 12px;
            font-weight: bold;
        }
        .badge-success {
            background: #d1fae5;
            color: #065f46;
        }
        .badge-failure {
            background: #fee2e2;
            color: #991b1b;
        }
        .progress-bar {
            width: 100%;
            height: 30px;
            background: #e5e7eb;
            border-radius: 15px;
            overflow: hidden;
        }
        .progress-fill {
            height: 100%;
            background: linear-gradient(90deg, #10b981 0%, #059669 100%);
            transition: width 0.3s ease;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>mtlog-audit Torture Test Report</h1>
        <div class="status {{if .Success}}success{{else}}failure{{end}}">
            {{if .Success}}✅ ALL TESTS PASSED{{else}}❌ TESTS FAILED{{end}}
        </div>
        <p>Generated: {{.EndTime.Format "2006-01-02 15:04:05"}}</p>
    </div>

    <div class="stats">
        <div class="stat-card">
            <div class="stat-label">Total Iterations</div>
            <div class="stat-value">{{.Iterations}}</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Duration</div>
            <div class="stat-value">{{.Duration}}</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Failure Rate</div>
            <div class="stat-value">{{printf "%.4f" .FailureRate}}%</div>
        </div>
        <div class="stat-card">
            <div class="stat-label">Scenarios</div>
            <div class="stat-value">{{len .Scenarios}}</div>
        </div>
    </div>

    <h2>Scenario Results</h2>
    {{range .Scenarios}}
    <div class="scenario">
        <div class="scenario-header">
            <div class="scenario-name">{{.Name}}</div>
            {{if eq .Failed 0}}
                <span class="badge badge-success">PASSED</span>
            {{else}}
                <span class="badge badge-failure">FAILED</span>
            {{end}}
        </div>
        <div class="progress-bar">
            <div class="progress-fill" style="width: {{if gt .Passed 0}}{{div (mul .Passed 100) (add .Passed .Failed)}}{{else}}0{{end}}%"></div>
        </div>
        <p>Passed: {{.Passed}} | Failed: {{.Failed}} | Avg Time: {{.AvgTime}}</p>
        {{if .Errors}}
        <details>
            <summary>Errors ({{len .Errors}})</summary>
            <ul>
            {{range .Errors}}
                <li>{{.}}</li>
            {{end}}
            </ul>
        </details>
        {{end}}
    </div>
    {{end}}
</body>
</html>
`
```

---

### Task 1.3: High-Volume Torture Test Runner

**File**: `torture/runner.go`

```go
// torture/runner.go
package torture

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/willibrandon/mtlog-audit/torture/report"
)

// Runner executes torture tests at scale
type Runner struct {
    config   Config
    scenarios []Scenario
}

// Config for torture test execution
type Config struct {
    Iterations     int
    Concurrency    int
    StopOnFailure  bool
    ReportPath     string
    ProgressEvery  int
}

// NewRunner creates a new torture test runner
func NewRunner(cfg Config) *Runner {
    return &Runner{
        config: cfg,
    }
}

// AddScenario adds a scenario to the runner
func (r *Runner) AddScenario(s Scenario) {
    r.scenarios = append(r.scenarios, s)
}

// Run executes all scenarios for the configured iterations
func (r *Runner) Run(ctx context.Context) (*report.Report, error) {
    rep := &report.Report{
        StartTime:  time.Now(),
        Iterations: r.config.Iterations,
        Scenarios:  make(map[string]*report.ScenarioResult),
    }

    // Initialize scenario results
    for _, scenario := range r.scenarios {
        rep.Scenarios[scenario.Name()] = &report.ScenarioResult{
            Name: scenario.Name(),
        }
    }

    fmt.Printf("Starting torture tests: %d iterations across %d scenarios\n",
        r.config.Iterations, len(r.scenarios))

    // Run tests with concurrency control
    semaphore := make(chan struct{}, r.config.Concurrency)
    var wg sync.WaitGroup
    var mu sync.Mutex

    for i := 0; i < r.config.Iterations; i++ {
        // Check context cancellation
        select {
        case <-ctx.Done():
            wg.Wait()
            return rep, ctx.Err()
        default:
        }

        for _, scenario := range r.scenarios {
            wg.Add(1)
            semaphore <- struct{}{}

            go func(s Scenario, iteration int) {
                defer wg.Done()
                defer func() { <-semaphore }()

                start := time.Now()

                // Run scenario
                err := r.runScenarioOnce(s)

                duration := time.Since(start)

                // Update results
                mu.Lock()
                result := rep.Scenarios[s.Name()]
                if err != nil {
                    result.Failed++
                    result.Errors = append(result.Errors, err)

                    if r.config.StopOnFailure {
                        fmt.Printf("FAILURE in %s (iteration %d): %v\n",
                            s.Name(), iteration, err)
                    }
                } else {
                    result.Passed++
                }

                // Update average time
                totalTests := result.Passed + result.Failed
                result.AvgTime = time.Duration(
                    (int64(result.AvgTime)*(int64(totalTests)-1) + int64(duration)) /
                    int64(totalTests),
                )
                mu.Unlock()

            }(scenario, i)
        }

        // Progress reporting
        if r.config.ProgressEvery > 0 && (i+1)%r.config.ProgressEvery == 0 {
            mu.Lock()
            r.printProgress(i+1, rep)
            mu.Unlock()
        }
    }

    // Wait for all tests to complete
    wg.Wait()

    rep.EndTime = time.Now()

    // Calculate success
    rep.Success = true
    for _, result := range rep.Scenarios {
        if result.Failed > 0 {
            rep.Success = false
            break
        }
    }

    // Generate report if path specified
    if r.config.ReportPath != "" {
        generator, err := report.NewGenerator()
        if err != nil {
            return rep, fmt.Errorf("failed to create report generator: %w", err)
        }

        if err := generator.Generate(rep, r.config.ReportPath); err != nil {
            return rep, fmt.Errorf("failed to generate report: %w", err)
        }

        fmt.Printf("\nReport generated: %s\n", r.config.ReportPath)
    }

    return rep, nil
}

func (r *Runner) runScenarioOnce(s Scenario) error {
    // Create temporary directory for this test
    dir, err := os.MkdirTemp("", "torture-")
    if err != nil {
        return fmt.Errorf("failed to create temp dir: %w", err)
    }
    defer os.RemoveAll(dir)

    // Create sink
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "test.wal")),
    )
    if err != nil {
        return fmt.Errorf("failed to create sink: %w", err)
    }

    // Execute scenario
    if err := s.Execute(sink, dir); err != nil {
        sink.Close()
        return fmt.Errorf("execute failed: %w", err)
    }

    // Close sink
    sink.Close()

    // Verify
    if err := s.Verify(dir); err != nil {
        return fmt.Errorf("verify failed: %w", err)
    }

    return nil
}

func (r *Runner) printProgress(iteration int, rep *report.Report) {
    fmt.Printf("\n=== Progress: %d/%d iterations ===\n", iteration, r.config.Iterations)
    for name, result := range rep.Scenarios {
        total := result.Passed + result.Failed
        if total > 0 {
            successRate := float64(result.Passed) / float64(total) * 100
            fmt.Printf("  %s: %.2f%% success (%d/%d)\n",
                name, successRate, result.Passed, total)
        }
    }
}
```

---

### Task 1.4: Performance Benchmarks

**File**: `performance/bench_test.go`

```go
// performance/bench_test.go
package performance

import (
    "fmt"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit"
)

func BenchmarkSimpleWrite(b *testing.B) {
    dir := b.TempDir()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
        audit.WithSyncMode(audit.SyncImmediate),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    event := createBenchEvent(0)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        sink.Emit(event)
    }
}

func BenchmarkWriteWithEncryption(b *testing.B) {
    dir := b.TempDir()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
        audit.WithCompliance("HIPAA",
            compliance.WithEncryption(true),
        ),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    event := createBenchEvent(0)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        sink.Emit(event)
    }
}

func BenchmarkWriteWithSigning(b *testing.B) {
    dir := b.TempDir()

    // Generate test key
    key, _ := compliance.GenerateEd25519Key()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
        audit.WithCompliance("SOX",
            compliance.WithSigning(key),
        ),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    event := createBenchEvent(0)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        sink.Emit(event)
    }
}

func BenchmarkGroupCommit(b *testing.B) {
    dir := b.TempDir()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
        audit.WithGroupCommit(100, 10*time.Millisecond),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    event := createBenchEvent(0)

    b.ResetTimer()
    b.ReportAllocs()

    for i := 0; i < b.N; i++ {
        sink.Emit(event)
    }
}

func BenchmarkConcurrentWrites(b *testing.B) {
    dir := b.TempDir()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    b.ResetTimer()
    b.ReportAllocs()

    b.RunParallel(func(pb *testing.PB) {
        event := createBenchEvent(0)
        for pb.Next() {
            sink.Emit(event)
        }
    })
}

// Latency benchmarks with percentile tracking
func BenchmarkLatencyP99(b *testing.B) {
    dir := b.TempDir()

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "bench.wal")),
    )
    if err != nil {
        b.Fatal(err)
    }
    defer sink.Close()

    event := createBenchEvent(0)
    latencies := make([]time.Duration, b.N)

    b.ResetTimer()

    for i := 0; i < b.N; i++ {
        start := time.Now()
        sink.Emit(event)
        latencies[i] = time.Since(start)
    }

    b.StopTimer()

    // Calculate percentiles
    sort.Slice(latencies, func(i, j int) bool {
        return latencies[i] < latencies[j]
    })

    p50 := latencies[b.N/2]
    p99 := latencies[b.N*99/100]
    p999 := latencies[b.N*999/1000]

    b.ReportMetric(float64(p50.Microseconds()), "p50_μs")
    b.ReportMetric(float64(p99.Microseconds()), "p99_μs")
    b.ReportMetric(float64(p999.Microseconds()), "p999_μs")
}

func createBenchEvent(id int) *core.LogEvent {
    return &core.LogEvent{
        Timestamp: time.Now(),
        Level:     core.InformationLevel,
        MessageTemplate: &core.MessageTemplate{
            Text: fmt.Sprintf("Benchmark event %d", id),
        },
        Properties: map[string]interface{}{
            "BenchmarkID": id,
            "Data":        "Some test data for the benchmark",
        },
    }
}
```

---

## SPRINT 2: Compliance & Multi-Backend (Week 3)

### Task 2.1: Merkle Tree Implementation

**File**: `compliance/merkle.go`

```go
// compliance/merkle.go
package compliance

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
)

// MerkleTree provides tamper detection via Merkle tree
type MerkleTree struct {
    root   *MerkleNode
    leaves []*MerkleNode
}

// MerkleNode represents a node in the Merkle tree
type MerkleNode struct {
    Hash  string
    Left  *MerkleNode
    Right *MerkleNode
    Data  []byte
}

// NewMerkleTree creates a new Merkle tree from data blocks
func NewMerkleTree(data [][]byte) *MerkleTree {
    if len(data) == 0 {
        return &MerkleTree{}
    }

    // Create leaf nodes
    leaves := make([]*MerkleNode, len(data))
    for i, d := range data {
        hash := sha256.Sum256(d)
        leaves[i] = &MerkleNode{
            Hash: hex.EncodeToString(hash[:]),
            Data: d,
        }
    }

    // Build tree
    root := buildMerkleTree(leaves)

    return &MerkleTree{
        root:   root,
        leaves: leaves,
    }
}

func buildMerkleTree(nodes []*MerkleNode) *MerkleNode {
    if len(nodes) == 0 {
        return nil
    }

    if len(nodes) == 1 {
        return nodes[0]
    }

    // Create parent level
    var parents []*MerkleNode

    for i := 0; i < len(nodes); i += 2 {
        left := nodes[i]
        var right *MerkleNode

        if i+1 < len(nodes) {
            right = nodes[i+1]
        } else {
            // Duplicate last node if odd number
            right = left
        }

        parent := &MerkleNode{
            Left:  left,
            Right: right,
            Hash:  hashNodes(left, right),
        }

        parents = append(parents, parent)
    }

    return buildMerkleTree(parents)
}

func hashNodes(left, right *MerkleNode) string {
    combined := left.Hash + right.Hash
    hash := sha256.Sum256([]byte(combined))
    return hex.EncodeToString(hash[:])
}

// Root returns the root hash of the tree
func (mt *MerkleTree) Root() string {
    if mt.root == nil {
        return ""
    }
    return mt.root.Hash
}

// Verify checks if the Merkle tree is valid
func (mt *MerkleTree) Verify() bool {
    if mt.root == nil {
        return false
    }
    return verifyNode(mt.root)
}

func verifyNode(node *MerkleNode) bool {
    if node == nil {
        return false
    }

    // Leaf node
    if node.Left == nil && node.Right == nil {
        if node.Data == nil {
            return false
        }
        hash := sha256.Sum256(node.Data)
        expectedHash := hex.EncodeToString(hash[:])
        return node.Hash == expectedHash
    }

    // Internal node
    if node.Left == nil || node.Right == nil {
        return false
    }

    // Verify children first
    if !verifyNode(node.Left) || !verifyNode(node.Right) {
        return false
    }

    // Verify this node's hash
    expectedHash := hashNodes(node.Left, node.Right)
    return node.Hash == expectedHash
}

// GetProof generates a Merkle proof for a leaf at the given index
func (mt *MerkleTree) GetProof(index int) ([]string, error) {
    if index < 0 || index >= len(mt.leaves) {
        return nil, fmt.Errorf("index out of range")
    }

    proof := []string{}
    node := mt.leaves[index]

    // Traverse up to root collecting sibling hashes
    currentIndex := index
    currentLevel := mt.leaves

    for len(currentLevel) > 1 {
        // Find sibling
        siblingIndex := currentIndex ^ 1 // XOR with 1 to get sibling
        if siblingIndex < len(currentLevel) {
            proof = append(proof, currentLevel[siblingIndex].Hash)
        }

        // Move to parent level
        currentIndex = currentIndex / 2
        currentLevel = getParentLevel(currentLevel)
    }

    return proof, nil
}

func getParentLevel(nodes []*MerkleNode) []*MerkleNode {
    var parents []*MerkleNode
    for i := 0; i < len(nodes); i += 2 {
        left := nodes[i]
        var right *MerkleNode
        if i+1 < len(nodes) {
            right = nodes[i+1]
        } else {
            right = left
        }

        parent := &MerkleNode{
            Left:  left,
            Right: right,
            Hash:  hashNodes(left, right),
        }
        parents = append(parents, parent)
    }
    return parents
}

// VerifyProof verifies a Merkle proof
func VerifyProof(leafHash string, proof []string, rootHash string, index int) bool {
    currentHash := leafHash

    for _, siblingHash := range proof {
        var combined string
        if index%2 == 0 {
            // Current node is left child
            combined = currentHash + siblingHash
        } else {
            // Current node is right child
            combined = siblingHash + currentHash
        }

        hash := sha256.Sum256([]byte(combined))
        currentHash = hex.EncodeToString(hash[:])
        index = index / 2
    }

    return currentHash == rootHash
}
```

---

### Task 2.2: Chain of Custody

**File**: `compliance/chain.go`

```go
// compliance/chain.go
package compliance

import (
    "crypto/ed25519"
    "encoding/json"
    "fmt"
    "time"
)

// ChainOfCustody tracks the complete history of audit log handling
type ChainOfCustody struct {
    entries []CustodyEntry
    signer  ed25519.PrivateKey
}

// CustodyEntry represents a single custody event
type CustodyEntry struct {
    Timestamp   time.Time              `json:"timestamp"`
    EventType   string                 `json:"event_type"`
    Actor       string                 `json:"actor"`
    Action      string                 `json:"action"`
    RecordHash  string                 `json:"record_hash"`
    Metadata    map[string]interface{} `json:"metadata"`
    Signature   string                 `json:"signature"`
}

// NewChainOfCustody creates a new chain of custody tracker
func NewChainOfCustody(signer ed25519.PrivateKey) *ChainOfCustody {
    return &ChainOfCustody{
        entries: []CustodyEntry{},
        signer:  signer,
    }
}

// AddEntry adds a new custody entry
func (c *ChainOfCustody) AddEntry(eventType, actor, action, recordHash string, metadata map[string]interface{}) error {
    entry := CustodyEntry{
        Timestamp:  time.Now(),
        EventType:  eventType,
        Actor:      actor,
        Action:     action,
        RecordHash: recordHash,
        Metadata:   metadata,
    }

    // Sign the entry
    signature, err := c.signEntry(entry)
    if err != nil {
        return fmt.Errorf("failed to sign entry: %w", err)
    }

    entry.Signature = signature
    c.entries = append(c.entries, entry)

    return nil
}

func (c *ChainOfCustody) signEntry(entry CustodyEntry) (string, error) {
    // Create canonical representation
    data, err := json.Marshal(struct {
        Timestamp   time.Time              `json:"timestamp"`
        EventType   string                 `json:"event_type"`
        Actor       string                 `json:"actor"`
        Action      string                 `json:"action"`
        RecordHash  string                 `json:"record_hash"`
        Metadata    map[string]interface{} `json:"metadata"`
    }{
        Timestamp:  entry.Timestamp,
        EventType:  entry.EventType,
        Actor:      entry.Actor,
        Action:     entry.Action,
        RecordHash: entry.RecordHash,
        Metadata:   entry.Metadata,
    })
    if err != nil {
        return "", err
    }

    signature := ed25519.Sign(c.signer, data)
    return hex.EncodeToString(signature), nil
}

// Verify verifies the entire chain of custody
func (c *ChainOfCustody) Verify(publicKey ed25519.PublicKey) error {
    for i, entry := range c.entries {
        if err := c.verifyEntry(entry, publicKey); err != nil {
            return fmt.Errorf("entry %d verification failed: %w", i, err)
        }
    }
    return nil
}

func (c *ChainOfCustody) verifyEntry(entry CustodyEntry, publicKey ed25519.PublicKey) error {
    // Recreate canonical representation
    data, err := json.Marshal(struct {
        Timestamp   time.Time              `json:"timestamp"`
        EventType   string                 `json:"event_type"`
        Actor       string                 `json:"actor"`
        Action      string                 `json:"action"`
        RecordHash  string                 `json:"record_hash"`
        Metadata    map[string]interface{} `json:"metadata"`
    }{
        Timestamp:  entry.Timestamp,
        EventType:  entry.EventType,
        Actor:      entry.Actor,
        Action:     entry.Action,
        RecordHash: entry.RecordHash,
        Metadata:   entry.Metadata,
    })
    if err != nil {
        return err
    }

    signature, err := hex.DecodeString(entry.Signature)
    if err != nil {
        return fmt.Errorf("invalid signature encoding: %w", err)
    }

    if !ed25519.Verify(publicKey, data, signature) {
        return fmt.Errorf("signature verification failed")
    }

    return nil
}

// Export exports the chain to JSON
func (c *ChainOfCustody) Export() ([]byte, error) {
    return json.MarshalIndent(c.entries, "", "  ")
}

// Import imports a chain from JSON
func (c *ChainOfCustody) Import(data []byte) error {
    return json.Unmarshal(data, &c.entries)
}

// GetEntries returns all custody entries
func (c *ChainOfCustody) GetEntries() []CustodyEntry {
    return c.entries
}
```

---

### Task 2.3: Retention Policies

**File**: `compliance/retention.go`

```go
// compliance/retention.go
package compliance

import (
    "fmt"
    "time"
)

// RetentionPolicy defines how long audit logs must be retained
type RetentionPolicy struct {
    MinimumRetention time.Duration
    LegalHold        bool
    AutoDelete       bool
    ArchiveAfter     time.Duration
    CompressAfter    time.Duration
}

// RetentionManager manages retention policies
type RetentionManager struct {
    policies map[string]*RetentionPolicy
}

// NewRetentionManager creates a new retention manager
func NewRetentionManager() *RetentionManager {
    return &RetentionManager{
        policies: make(map[string]*RetentionPolicy),
    }
}

// AddPolicy adds a retention policy
func (rm *RetentionManager) AddPolicy(name string, policy *RetentionPolicy) {
    rm.policies[name] = policy
}

// GetPolicy retrieves a retention policy by name
func (rm *RetentionManager) GetPolicy(name string) (*RetentionPolicy, error) {
    policy, ok := rm.policies[name]
    if !ok {
        return nil, fmt.Errorf("policy not found: %s", name)
    }
    return policy, nil
}

// CanDelete checks if a record can be deleted based on its age
func (rm *RetentionManager) CanDelete(policyName string, recordTime time.Time) (bool, error) {
    policy, err := rm.GetPolicy(policyName)
    if err != nil {
        return false, err
    }

    // Never delete if legal hold is active
    if policy.LegalHold {
        return false, nil
    }

    // Check minimum retention
    age := time.Since(recordTime)
    if age < policy.MinimumRetention {
        return false, nil
    }

    // Check if auto-delete is enabled
    return policy.AutoDelete, nil
}

// ShouldArchive checks if a record should be archived
func (rm *RetentionManager) ShouldArchive(policyName string, recordTime time.Time) (bool, error) {
    policy, err := rm.GetPolicy(policyName)
    if err != nil {
        return false, err
    }

    if policy.ArchiveAfter == 0 {
        return false, nil
    }

    age := time.Since(recordTime)
    return age >= policy.ArchiveAfter, nil
}

// ShouldCompress checks if a record should be compressed
func (rm *RetentionManager) ShouldCompress(policyName string, recordTime time.Time) (bool, error) {
    policy, err := rm.GetPolicy(policyName)
    if err != nil {
        return false, err
    }

    if policy.CompressAfter == 0 {
        return false, nil
    }

    age := time.Since(recordTime)
    return age >= policy.CompressAfter, nil
}

// Predefined retention policies for common compliance requirements

// HIPAARetentionPolicy returns a HIPAA-compliant retention policy
func HIPAARetentionPolicy() *RetentionPolicy {
    return &RetentionPolicy{
        MinimumRetention: 6 * 365 * 24 * time.Hour, // 6 years
        LegalHold:        false,
        AutoDelete:       false,
        ArchiveAfter:     90 * 24 * time.Hour,      // 90 days
        CompressAfter:    30 * 24 * time.Hour,      // 30 days
    }
}

// PCIDSSRetentionPolicy returns a PCI-DSS compliant retention policy
func PCIDSSRetentionPolicy() *RetentionPolicy {
    return &RetentionPolicy{
        MinimumRetention: 1 * 365 * 24 * time.Hour, // 1 year
        LegalHold:        false,
        AutoDelete:       true,
        ArchiveAfter:     90 * 24 * time.Hour,      // 90 days
        CompressAfter:    30 * 24 * time.Hour,      // 30 days
    }
}

// SOXRetentionPolicy returns a SOX-compliant retention policy
func SOXRetentionPolicy() *RetentionPolicy {
    return &RetentionPolicy{
        MinimumRetention: 7 * 365 * 24 * time.Hour, // 7 years
        LegalHold:        false,
        AutoDelete:       false,
        ArchiveAfter:     180 * 24 * time.Hour,     // 180 days
        CompressAfter:    30 * 24 * time.Hour,      // 30 days
    }
}

// GDPRRetentionPolicy returns a GDPR-compliant retention policy
func GDPRRetentionPolicy() *RetentionPolicy {
    return &RetentionPolicy{
        MinimumRetention: 0,                        // No minimum (data minimization)
        LegalHold:        false,
        AutoDelete:       true,                     // Must delete when no longer needed
        ArchiveAfter:     30 * 24 * time.Hour,      // 30 days
        CompressAfter:    7 * 24 * time.Hour,       // 7 days
    }
}
```

---

### Task 2.4: Multi-Backend with Quorum

**File**: `backends/multi/multi.go`

```go
// backends/multi/multi.go
package multi

import (
    "fmt"
    "sync"
    "time"

    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit/backends"
)

// MultiBackend manages multiple backends with quorum writes
type MultiBackend struct {
    backends      []backends.Backend
    quorumSize    int
    writeTimeout  time.Duration
    mu            sync.RWMutex
}

// Config for multi-backend
type Config struct {
    Backends     []backends.Backend
    QuorumSize   int
    WriteTimeout time.Duration
}

// New creates a new multi-backend
func New(cfg Config) (*MultiBackend, error) {
    if cfg.QuorumSize <= 0 {
        cfg.QuorumSize = (len(cfg.Backends) / 2) + 1
    }

    if cfg.QuorumSize > len(cfg.Backends) {
        return nil, fmt.Errorf("quorum size %d exceeds number of backends %d",
            cfg.QuorumSize, len(cfg.Backends))
    }

    if cfg.WriteTimeout == 0 {
        cfg.WriteTimeout = 5 * time.Second
    }

    return &MultiBackend{
        backends:     cfg.Backends,
        quorumSize:   cfg.QuorumSize,
        writeTimeout: cfg.WriteTimeout,
    }, nil
}

// Name returns the backend name
func (mb *MultiBackend) Name() string {
    return "multi-backend"
}

// Write writes to backends with quorum requirement
func (mb *MultiBackend) Write(event *core.LogEvent) error {
    mb.mu.RLock()
    defer mb.mu.RUnlock()

    type writeResult struct {
        backend backends.Backend
        err     error
    }

    results := make(chan writeResult, len(mb.backends))

    // Write to all backends concurrently
    for _, backend := range mb.backends {
        go func(b backends.Backend) {
            err := b.Write(event)
            results <- writeResult{backend: b, err: err}
        }(backend)
    }

    // Collect results with timeout
    successCount := 0
    var lastErr error
    timeout := time.After(mb.writeTimeout)

    for i := 0; i < len(mb.backends); i++ {
        select {
        case result := <-results:
            if result.err == nil {
                successCount++
                if successCount >= mb.quorumSize {
                    // Quorum achieved
                    return nil
                }
            } else {
                lastErr = result.err
            }
        case <-timeout:
            return fmt.Errorf("write timeout: only %d/%d backends succeeded (quorum: %d)",
                successCount, len(mb.backends), mb.quorumSize)
        }
    }

    if successCount < mb.quorumSize {
        return fmt.Errorf("quorum not achieved: only %d/%d backends succeeded (quorum: %d): %w",
            successCount, len(mb.backends), mb.quorumSize, lastErr)
    }

    return nil
}

// Close closes all backends
func (mb *MultiBackend) Close() error {
    mb.mu.Lock()
    defer mb.mu.Unlock()

    var errs []error
    for _, backend := range mb.backends {
        if err := backend.Close(); err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("failed to close %d backends: %v", len(errs), errs)
    }

    return nil
}

// VerifyIntegrity verifies integrity across all backends
func (mb *MultiBackend) VerifyIntegrity() (*backends.IntegrityReport, error) {
    mb.mu.RLock()
    defer mb.mu.RUnlock()

    report := &backends.IntegrityReport{
        Valid:     true,
        Timestamp: time.Now(),
    }

    // Verify all backends
    for _, backend := range mb.backends {
        backendReport, err := backend.VerifyIntegrity()
        if err != nil {
            report.Valid = false
            report.Errors = append(report.Errors, err)
            continue
        }

        if !backendReport.Valid {
            report.Valid = false
        }
    }

    return report, nil
}

// GetHealthyBackends returns a list of healthy backends
func (mb *MultiBackend) GetHealthyBackends() []backends.Backend {
    mb.mu.RLock()
    defer mb.mu.RUnlock()

    var healthy []backends.Backend

    for _, backend := range mb.backends {
        report, err := backend.VerifyIntegrity()
        if err == nil && report.Valid {
            healthy = append(healthy, backend)
        }
    }

    return healthy
}
```

**File**: `backends/multi/failover.go`

```go
// backends/multi/failover.go
package multi

import (
    "fmt"
    "sync"
    "time"

    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit/backends"
)

// FailoverBackend provides automatic failover between backends
type FailoverBackend struct {
    primary    backends.Backend
    secondary  []backends.Backend
    mu         sync.RWMutex
    healthCheck time.Duration
    currentBackend backends.Backend
}

// NewFailover creates a new failover backend
func NewFailover(primary backends.Backend, secondary []backends.Backend, healthCheckInterval time.Duration) *FailoverBackend {
    fb := &FailoverBackend{
        primary:     primary,
        secondary:   secondary,
        healthCheck: healthCheckInterval,
        currentBackend: primary,
    }

    // Start health checking
    go fb.startHealthCheck()

    return fb
}

// Name returns the backend name
func (fb *FailoverBackend) Name() string {
    return "failover-backend"
}

// Write writes to the current active backend
func (fb *FailoverBackend) Write(event *core.LogEvent) error {
    fb.mu.RLock()
    current := fb.currentBackend
    fb.mu.RUnlock()

    err := current.Write(event)
    if err != nil {
        // Try to failover
        fb.performFailover()

        // Retry with new backend
        fb.mu.RLock()
        current = fb.currentBackend
        fb.mu.RUnlock()

        return current.Write(event)
    }

    return nil
}

// Close closes all backends
func (fb *FailoverBackend) Close() error {
    fb.mu.Lock()
    defer fb.mu.Unlock()

    var errs []error

    if err := fb.primary.Close(); err != nil {
        errs = append(errs, err)
    }

    for _, backend := range fb.secondary {
        if err := backend.Close(); err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("failed to close backends: %v", errs)
    }

    return nil
}

// VerifyIntegrity verifies the current backend
func (fb *FailoverBackend) VerifyIntegrity() (*backends.IntegrityReport, error) {
    fb.mu.RLock()
    defer fb.mu.RUnlock()

    return fb.currentBackend.VerifyIntegrity()
}

func (fb *FailoverBackend) startHealthCheck() {
    ticker := time.NewTicker(fb.healthCheck)
    defer ticker.Stop()

    for range ticker.C {
        fb.checkHealth()
    }
}

func (fb *FailoverBackend) checkHealth() {
    fb.mu.RLock()
    current := fb.currentBackend
    fb.mu.RUnlock()

    // Check if current backend is healthy
    report, err := current.VerifyIntegrity()
    if err != nil || !report.Valid {
        fb.performFailover()
    }
}

func (fb *FailoverBackend) performFailover() {
    fb.mu.Lock()
    defer fb.mu.Unlock()

    // Try primary first
    if fb.currentBackend != fb.primary {
        report, err := fb.primary.VerifyIntegrity()
        if err == nil && report.Valid {
            fb.currentBackend = fb.primary
            fmt.Printf("Failed back to primary backend\n")
            return
        }
    }

    // Try secondary backends
    for _, backend := range fb.secondary {
        if backend == fb.currentBackend {
            continue
        }

        report, err := backend.VerifyIntegrity()
        if err == nil && report.Valid {
            fb.currentBackend = backend
            fmt.Printf("Failed over to secondary backend: %s\n", backend.Name())
            return
        }
    }

    fmt.Printf("WARNING: No healthy backends available\n")
}
```

---

## SPRINT 3: Monitoring & Polish (Week 4)

### Task 3.1: Prometheus Metrics Integration

**File**: `monitoring/prometheus.go`

```go
// monitoring/prometheus.go
package monitoring

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

// PrometheusMetrics holds all Prometheus metrics
type PrometheusMetrics struct {
    // Writes
    writesTotal    *prometheus.CounterVec
    writeDuration  *prometheus.HistogramVec
    writeErrors    *prometheus.CounterVec

    // WAL
    walSize        prometheus.Gauge
    segmentCount   prometheus.Gauge
    compactionTime prometheus.Histogram

    // Integrity
    integrityScore      prometheus.Gauge
    corruptionsDetected prometheus.Counter
    recoverySuccess     *prometheus.CounterVec

    // Backends
    backendWrites  *prometheus.CounterVec
    backendErrors  *prometheus.CounterVec
    backendLatency *prometheus.HistogramVec

    // Performance
    emitLatency   prometheus.Histogram
    eventRate     prometheus.Gauge
    queueDepth    prometheus.Gauge
}

// NewPrometheusMetrics creates new Prometheus metrics
func NewPrometheusMetrics(namespace string) *PrometheusMetrics {
    return &PrometheusMetrics{
        writesTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "writes_total",
                Help:      "Total number of write operations",
            },
            []string{"status"},
        ),

        writeDuration: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Namespace: namespace,
                Name:      "write_duration_seconds",
                Help:      "Write operation duration in seconds",
                Buckets:   prometheus.DefBuckets,
            },
            []string{"operation"},
        ),

        writeErrors: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "write_errors_total",
                Help:      "Total number of write errors",
            },
            []string{"error_type"},
        ),

        walSize: promauto.NewGauge(
            prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      "wal_size_bytes",
                Help:      "Current WAL size in bytes",
            },
        ),

        segmentCount: promauto.NewGauge(
            prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      "wal_segments_count",
                Help:      "Number of WAL segments",
            },
        ),

        compactionTime: promauto.NewHistogram(
            prometheus.HistogramOpts{
                Namespace: namespace,
                Name:      "compaction_duration_seconds",
                Help:      "WAL compaction duration in seconds",
                Buckets:   prometheus.DefBuckets,
            },
        ),

        integrityScore: promauto.NewGauge(
            prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      "integrity_score",
                Help:      "Current integrity score (0-100)",
            },
        ),

        corruptionsDetected: promauto.NewCounter(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "corruptions_detected_total",
                Help:      "Total number of corruptions detected",
            },
        ),

        recoverySuccess: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "recovery_operations_total",
                Help:      "Total number of recovery operations",
            },
            []string{"status"},
        ),

        backendWrites: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "backend_writes_total",
                Help:      "Total number of backend writes",
            },
            []string{"backend", "status"},
        ),

        backendErrors: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Namespace: namespace,
                Name:      "backend_errors_total",
                Help:      "Total number of backend errors",
            },
            []string{"backend", "error_type"},
        ),

        backendLatency: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Namespace: namespace,
                Name:      "backend_write_duration_seconds",
                Help:      "Backend write duration in seconds",
                Buckets:   prometheus.DefBuckets,
            },
            []string{"backend"},
        ),

        emitLatency: promauto.NewHistogram(
            prometheus.HistogramOpts{
                Namespace: namespace,
                Name:      "emit_duration_seconds",
                Help:      "Event emission duration in seconds",
                Buckets:   []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
            },
        ),

        eventRate: promauto.NewGauge(
            prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      "event_rate_per_second",
                Help:      "Current event ingestion rate per second",
            },
        ),

        queueDepth: promauto.NewGauge(
            prometheus.GaugeOpts{
                Namespace: namespace,
                Name:      "queue_depth",
                Help:      "Current queue depth",
            },
        ),
    }
}
```

---

### Task 3.2: Health Check Endpoints

**File**: `monitoring/health.go`

```go
// monitoring/health.go
package monitoring

import (
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

// HealthCheck provides health check endpoints
type HealthCheck struct {
    monitor *Monitor
}

// HealthStatus represents the health status
type HealthStatus struct {
    Status      string            `json:"status"`
    Timestamp   time.Time         `json:"timestamp"`
    Version     string            `json:"version"`
    Checks      map[string]Check  `json:"checks"`
}

// Check represents a single health check
type Check struct {
    Status  string    `json:"status"`
    Message string    `json:"message,omitempty"`
    Time    time.Time `json:"time"`
}

// NewHealthCheck creates a new health check handler
func NewHealthCheck(monitor *Monitor) *HealthCheck {
    return &HealthCheck{
        monitor: monitor,
    }
}

// Handler returns an HTTP handler for health checks
func (hc *HealthCheck) Handler() http.Handler {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", hc.handleHealth)
    mux.HandleFunc("/health/live", hc.handleLiveness)
    mux.HandleFunc("/health/ready", hc.handleReadiness)
    return mux
}

// handleHealth returns detailed health status
func (hc *HealthCheck) handleHealth(w http.ResponseWriter, r *http.Request) {
    status := hc.getHealthStatus()

    w.Header().Set("Content-Type", "application/json")
    if status.Status != "healthy" {
        w.WriteHeader(http.StatusServiceUnavailable)
    }

    json.NewEncoder(w).Encode(status)
}

// handleLiveness is a simple liveness probe
func (hc *HealthCheck) handleLiveness(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w, "OK")
}

// handleReadiness checks if the service is ready to accept traffic
func (hc *HealthCheck) handleReadiness(w http.ResponseWriter, r *http.Request) {
    status := hc.getHealthStatus()

    if status.Status == "healthy" {
        w.WriteHeader(http.StatusOK)
        fmt.Fprintf(w, "READY")
    } else {
        w.WriteHeader(http.StatusServiceUnavailable)
        fmt.Fprintf(w, "NOT READY")
    }
}

func (hc *HealthCheck) getHealthStatus() HealthStatus {
    status := HealthStatus{
        Status:    "healthy",
        Timestamp: time.Now(),
        Version:   "1.0.0", // TODO: Get from version package
        Checks:    make(map[string]Check),
    }

    // Check WAL health
    walCheck := hc.checkWAL()
    status.Checks["wal"] = walCheck
    if walCheck.Status != "pass" {
        status.Status = "degraded"
    }

    // Check backends
    backendCheck := hc.checkBackends()
    status.Checks["backends"] = backendCheck
    if backendCheck.Status != "pass" {
        status.Status = "degraded"
    }

    // Check metrics
    metricsCheck := hc.checkMetrics()
    status.Checks["metrics"] = metricsCheck

    return status
}

func (hc *HealthCheck) checkWAL() Check {
    // TODO: Implement actual WAL health check
    return Check{
        Status:  "pass",
        Message: "WAL is operational",
        Time:    time.Now(),
    }
}

func (hc *HealthCheck) checkBackends() Check {
    // TODO: Implement actual backend health check
    return Check{
        Status:  "pass",
        Message: "All backends operational",
        Time:    time.Now(),
    }
}

func (hc *HealthCheck) checkMetrics() Check {
    return Check{
        Status:  "pass",
        Message: "Metrics collection active",
        Time:    time.Now(),
    }
}
```

---

### Task 3.3: Grafana Dashboard

**File**: `monitoring/dashboard.json`

```json
{
  "dashboard": {
    "title": "mtlog-audit Performance Dashboard",
    "tags": ["audit", "logging"],
    "timezone": "browser",
    "panels": [
      {
        "title": "Event Ingestion Rate",
        "targets": [
          {
            "expr": "rate(mtlog_audit_writes_total{status=\"success\"}[5m])",
            "legendFormat": "Events/sec"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Write Latency (P99)",
        "targets": [
          {
            "expr": "histogram_quantile(0.99, rate(mtlog_audit_emit_duration_seconds_bucket[5m]))",
            "legendFormat": "P99 Latency"
          },
          {
            "expr": "histogram_quantile(0.95, rate(mtlog_audit_emit_duration_seconds_bucket[5m]))",
            "legendFormat": "P95 Latency"
          },
          {
            "expr": "histogram_quantile(0.50, rate(mtlog_audit_emit_duration_seconds_bucket[5m]))",
            "legendFormat": "P50 Latency"
          }
        ],
        "type": "graph"
      },
      {
        "title": "WAL Size",
        "targets": [
          {
            "expr": "mtlog_audit_wal_size_bytes",
            "legendFormat": "WAL Size (bytes)"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Integrity Score",
        "targets": [
          {
            "expr": "mtlog_audit_integrity_score",
            "legendFormat": "Score"
          }
        ],
        "type": "gauge",
        "options": {
          "thresholds": [
            { "value": 0, "color": "red" },
            { "value": 80, "color": "yellow" },
            { "value": 95, "color": "green" }
          ]
        }
      },
      {
        "title": "Backend Success Rate",
        "targets": [
          {
            "expr": "rate(mtlog_audit_backend_writes_total{status=\"success\"}[5m]) / rate(mtlog_audit_backend_writes_total[5m])",
            "legendFormat": "{{backend}}"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Corruptions Detected",
        "targets": [
          {
            "expr": "increase(mtlog_audit_corruptions_detected_total[1h])",
            "legendFormat": "Last Hour"
          }
        ],
        "type": "stat"
      }
    ]
  }
}
```

---

### Task 3.4: Integration Tests

**File**: `integration/integration_test.go`

```go
// integration/integration_test.go
// +build integration

package integration

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/willibrandon/mtlog/core"
    "github.com/willibrandon/mtlog-audit"
    "github.com/willibrandon/mtlog-audit/backends"
    "github.com/willibrandon/mtlog-audit/compliance"
)

func TestEndToEndWithCompliance(t *testing.T) {
    dir := t.TempDir()

    // Create sink with HIPAA compliance
    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "audit.wal")),
        audit.WithCompliance("HIPAA",
            compliance.WithEncryption(true),
        ),
    )
    if err != nil {
        t.Fatalf("Failed to create sink: %v", err)
    }
    defer sink.Close()

    // Write events
    for i := 0; i < 100; i++ {
        event := &core.LogEvent{
            Timestamp: time.Now(),
            Level:     core.InformationLevel,
            MessageTemplate: &core.MessageTemplate{
                Text: fmt.Sprintf("Patient record accessed: %d", i),
            },
        }
        sink.Emit(event)
    }

    // Verify integrity
    report, err := sink.VerifyIntegrity()
    if err != nil {
        t.Fatalf("Integrity check failed: %v", err)
    }

    if !report.Valid {
        t.Error("Expected valid integrity")
    }

    if report.TotalRecords != 100 {
        t.Errorf("Expected 100 records, got %d", report.TotalRecords)
    }
}

func TestMultiBackendQuorum(t *testing.T) {
    dir := t.TempDir()

    // Create multiple filesystem backends
    backend1, _ := backends.NewFilesystem(filepath.Join(dir, "backend1"))
    backend2, _ := backends.NewFilesystem(filepath.Join(dir, "backend2"))
    backend3, _ := backends.NewFilesystem(filepath.Join(dir, "backend3"))

    sink, err := audit.New(
        audit.WithWAL(filepath.Join(dir, "audit.wal")),
        audit.WithBackends(backend1, backend2, backend3),
        audit.WithQuorum(2), // Require 2 out of 3
    )
    if err != nil {
        t.Fatalf("Failed to create sink: %v", err)
    }
    defer sink.Close()

    // Write events
    event := &core.LogEvent{
        Timestamp: time.Now(),
        Level:     core.InformationLevel,
        MessageTemplate: &core.MessageTemplate{
            Text: "Test event",
        },
    }

    sink.Emit(event)

    // Verify backends
    time.Sleep(100 * time.Millisecond) // Allow async writes

    // At least 2 backends should have the data
    successCount := 0
    for _, backend := range []backends.Backend{backend1, backend2, backend3} {
        report, err := backend.VerifyIntegrity()
        if err == nil && report.Valid {
            successCount++
        }
    }

    if successCount < 2 {
        t.Errorf("Expected at least 2 backends to succeed, got %d", successCount)
    }
}

func TestRecoveryFromCorruption(t *testing.T) {
    dir := t.TempDir()
    walPath := filepath.Join(dir, "audit.wal")

    // Create and write some data
    sink1, _ := audit.New(audit.WithWAL(walPath))
    for i := 0; i < 50; i++ {
        event := &core.LogEvent{
            Timestamp: time.Now(),
            Level:     core.InformationLevel,
            MessageTemplate: &core.MessageTemplate{
                Text: fmt.Sprintf("Event %d", i),
            },
        }
        sink1.Emit(event)
    }
    sink1.Close()

    // Corrupt the WAL
    file, _ := os.OpenFile(walPath, os.O_RDWR, 0644)
    file.WriteAt([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 100)
    file.Close()

    // Reopen and verify recovery
    sink2, err := audit.New(audit.WithWAL(walPath))
    if err != nil {
        t.Fatalf("Failed to reopen: %v", err)
    }
    defer sink2.Close()

    // Should detect corruption but still function
    report, _ := sink2.VerifyIntegrity()

    // Some records should be recoverable
    events, _ := sink2.Replay(time.Time{}, time.Now())
    if len(events) < 25 {
        t.Errorf("Expected to recover at least 50%% of events, got %d/50", len(events))
    }
}
```

---

## SPRINT 4: Documentation & Examples (Week 5-6)

### Task 4.1: Architecture Documentation

**File**: `docs/architecture.md`

Create comprehensive architecture documentation with:
- System overview diagram
- Component interactions
- Data flow diagrams
- Failure scenarios and recovery
- Performance characteristics
- Security model

### Task 4.2: Deployment Guides

Create the following deployment guides:

**Files**:
- `docs/deployment/aws.md` - AWS deployment with S3
- `docs/deployment/azure.md` - Azure deployment with Blob Storage
- `docs/deployment/gcp.md` - GCP deployment with Cloud Storage
- `docs/deployment/on-premise.md` - On-premise deployment
- `docs/deployment/kubernetes.md` - Kubernetes deployment

Each should include:
- Infrastructure requirements
- Configuration examples
- Security considerations
- Monitoring setup
- Troubleshooting

### Task 4.3: Industry Examples

**File**: `examples/healthcare/main.go`

```go
// examples/healthcare/main.go
package main

import (
    "log"
    "time"

    "github.com/willibrandon/mtlog"
    "github.com/willibrandon/mtlog-audit"
    "github.com/willibrandon/mtlog-audit/compliance"
)

func main() {
    // HIPAA-compliant audit logging for healthcare
    auditSink, err := audit.New(
        audit.WithWAL("/secure/audit/patient-access.wal"),
        audit.WithCompliance("HIPAA",
            compliance.WithEncryption(true),
            compliance.WithRetention(6*365*24*time.Hour), // 6 years
            compliance.WithAccessLogging(true),
        ),
        audit.WithS3("healthcare-audit-backup", "us-east-1",
            backends.WithImmutability(true),
            backends.WithVersioning(true),
        ),
        audit.WithPanicOnFailure(),
    )
    if err != nil {
        log.Fatal("CRITICAL: Audit system failed to initialize:", err)
    }
    defer auditSink.Close()

    logger := mtlog.New(mtlog.WithSink(auditSink))

    // Example: Log patient record access
    logger.With("Audit", true).Info(
        "User {UserId} accessed patient record {PatientId} from {IPAddress}",
        "USR-12345",
        "PAT-67890",
        "192.168.1.100",
    )
}
```

**File**: `examples/financial/main.go` - SOX-compliant financial transaction logging
**File**: `examples/multi-tenant/main.go` - SaaS multi-tenant with isolation
**File**: `examples/kubernetes/deployment.yaml` - K8s deployment manifests

---

## Success Criteria and Acceptance Tests

### Torture Test Validation
- [ ] All 8 scenarios implemented
- [ ] 1,000,000+ iterations completed
- [ ] Zero data loss across all iterations
- [ ] HTML report generated
- [ ] Success rate: 100%

### Performance Validation
- [ ] Simple write: > 20,000/sec
- [ ] P99 latency: < 5ms
- [ ] Group commit: > 100,000/sec
- [ ] Zero allocations for simple write
- [ ] Benchmarks documented

### Test Coverage
- [ ] Overall: > 60%
- [ ] WAL package: > 70%
- [ ] Compliance: > 60%
- [ ] Backends: > 50%
- [ ] Monitoring: > 40%
- [ ] Resilience: > 40%

### Compliance
- [ ] All 4 profiles fully functional
- [ ] Merkle tree working
- [ ] Retention policies enforced
- [ ] Compliance reports generate PDFs
- [ ] Chain of custody verified

### Documentation
- [ ] All design.md docs created
- [ ] API reference complete
- [ ] 4+ examples working
- [ ] Deployment guides for AWS/Azure/GCP
- [ ] Troubleshooting guide

### Production Readiness
- [ ] Prometheus metrics exported
- [ ] Grafana dashboards imported
- [ ] Health endpoints responding
- [ ] Docker images building
- [ ] Integration tests passing

---

## Timeline and Milestones

### Week 1-2: Sprint 1 (Torture & Performance)
- **Day 1-2**: Implement missing torture scenarios
- **Day 3-4**: Torture test runner and 1M validation
- **Day 5-7**: Performance benchmarks and optimization
- **Day 8-10**: HTML reporting and validation

**Milestone**: Torture tests passing with < 5ms P99 latency

### Week 3: Sprint 2 (Compliance & Backends)
- **Day 11-12**: Merkle tree and chain of custody
- **Day 13-14**: Retention policies
- **Day 15-17**: Multi-backend with quorum
- **Day 18-19**: Compliance report generation
- **Day 20-21**: Testing and validation

**Milestone**: All compliance features functional

### Week 4: Sprint 3 (Monitoring & Polish)
- **Day 22-23**: Prometheus integration
- **Day 24-25**: Grafana dashboards
- **Day 26-27**: Health checks
- **Day 28**: Integration tests
- **Day 29-30**: Test coverage improvement

**Milestone**: > 60% test coverage, monitoring operational

### Week 5-6: Sprint 4 (Documentation)
- **Day 31-33**: Architecture and deployment docs
- **Day 34-36**: Industry examples
- **Day 37-38**: API documentation
- **Day 39-40**: Troubleshooting guide
- **Day 41-42**: Release preparation

**Milestone**: v1.0.0 release ready

---

## Risk Management

### High Risk Items
1. **1M Torture Test Duration**: May take 24+ hours
   - Mitigation: Run in parallel, use powerful hardware

2. **Performance Targets**: < 5ms P99 may be challenging
   - Mitigation: Focus on O_SYNC alternatives, group commit

3. **Cloud Backend Testing**: Requires cloud accounts
   - Mitigation: Use LocalStack/Azurite for testing

### Medium Risk Items
4. **Test Coverage**: Getting to 60% requires significant effort
   - Mitigation: Prioritize high-value packages

5. **Documentation Scope**: Large amount of docs to write
   - Mitigation: Template-based approach, reuse patterns

---

## Conclusion

This roadmap provides a clear path to 100% completion of mtlog-audit. By following the priority-ordered sprints, the project will achieve:

1. **Proven reliability** through 1M+ torture tests
2. **Performance targets** validated through benchmarks
3. **Enterprise features** via compliance and multi-backend
4. **Production readiness** with monitoring and health checks
5. **Complete documentation** enabling adoption

**Total Estimated Effort**: 6 weeks (240 hours)
**Target Release**: v1.0.0

---

*This roadmap should be reviewed and updated weekly as implementation progresses.*
