package audit

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/willibrandon/mtlog-audit/backends"
	"github.com/willibrandon/mtlog-audit/compliance"
	"github.com/willibrandon/mtlog-audit/monitoring"
	"github.com/willibrandon/mtlog-audit/resilience"
	"github.com/willibrandon/mtlog-audit/wal"
	"github.com/willibrandon/mtlog/core"
)

// SyncMode defines when the WAL syncs to disk
type SyncMode = wal.SyncMode

const (
	// SyncImmediate syncs after every write (safest, slowest)
	SyncImmediate = wal.SyncImmediate
	// SyncInterval syncs periodically
	SyncInterval = wal.SyncInterval
	// SyncBatch syncs after a batch of writes
	SyncBatch = wal.SyncBatch
)

// IntegrityReport contains the results of an integrity check.
type IntegrityReport struct {
	Timestamp           time.Time
	Valid               bool
	TotalRecords        int
	CorruptedSegments   int
	WALIntegrity        *wal.IntegrityReport
	ComplianceIntegrity interface{}
	BackendReports      []interface{}
	BackendErrors       []error
}

// Sink implements a bulletproof audit sink that guarantees delivery.
// It implements the core.LogEventSink interface from mtlog.
type Sink struct {
	mu         sync.RWMutex
	wal        *wal.WAL
	config     *Config
	closed     bool
	compliance *compliance.Engine
	backends   []backends.Backend
	resilience *resilience.Manager
	monitoring *monitoring.Monitor
}

// Ensure we implement the interface
var _ core.LogEventSink = (*Sink)(nil)

// New creates a new audit sink with the specified options.
// Returns an error if the sink cannot guarantee audit requirements.
func New(opts ...Option) (*Sink, error) {
	config := defaultConfig()

	for _, opt := range opts {
		if err := opt(config); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Initialize WAL - this MUST succeed
	walInstance, err := wal.New(config.WALPath, config.WALOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize WAL: %w", err)
	}

	// Verify WAL integrity on startup
	if err := walInstance.VerifyIntegrity(); err != nil {
		_ = walInstance.Close()
		return nil, fmt.Errorf("WAL integrity check failed: %w", err)
	}

	sink := &Sink{
		wal:    walInstance,
		config: config,
	}

	// Initialize compliance engine if configured
	if config.ComplianceProfile != "" {
		sink.compliance, err = compliance.New(config.ComplianceProfile, config.ComplianceOptions...)
		if err != nil {
			_ = walInstance.Close()
			return nil, fmt.Errorf("compliance init failed: %w", err)
		}
	}

	// Initialize backends
	for _, backendConfig := range config.BackendConfigs {
		backend, err := backends.Create(backendConfig)
		if err != nil {
			_ = walInstance.Close()
			return nil, fmt.Errorf("failed to create backend: %w", err)
		}
		sink.backends = append(sink.backends, backend)
	}

	// Initialize resilience manager
	resilienceOpts := []resilience.Option{}
	if config.CircuitBreakerOptions != nil {
		// Apply circuit breaker options if provided
		for _, opt := range config.CircuitBreakerOptions {
			if fn, ok := opt.(resilience.Option); ok {
				resilienceOpts = append(resilienceOpts, fn)
			}
		}
	}
	sink.resilience = resilience.New(resilienceOpts...)

	// Initialize monitoring
	monitorConfig := monitoring.DefaultConfig()
	if config.MetricsOptions != nil {
		// Apply metrics options if provided
		for _, opt := range config.MetricsOptions {
			if cfg, ok := opt.(*monitoring.Config); ok {
				monitorConfig = cfg
				break
			}
		}
	}
	sink.monitoring = monitoring.NewMonitor(monitorConfig)
	sink.monitoring.Start()

	return sink, nil
}

// Emit processes a log event with guaranteed delivery.
// Implements core.LogEventSink from mtlog.
func (s *Sink) Emit(event *core.LogEvent) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		// Handle writes to closed sink based on configuration
		if s.config.PanicOnFailure {
			panic("attempted to write to closed audit sink")
		}
		if s.config.FailureHandler != nil {
			s.config.FailureHandler(event, ErrSinkClosed)
		}
		return
	}
	s.mu.RUnlock()

	// Apply compliance transformations if needed
	if s.compliance != nil {
		event = s.compliance.Transform(event)
	}

	// Add monitoring
	if s.monitoring != nil {
		s.monitoring.RecordEmit()
	}

	// Write to WAL with guaranteed durability
	if err := s.writeToWAL(event); err != nil {
		// This should NEVER happen, but if it does...
		s.handleCriticalFailure(event, err)
		return
	}

	// Asynchronously replicate to backends
	for _, backend := range s.backends {
		go s.replicateToBackend(backend, event)
	}
}

// Close gracefully shuts down the audit sink.
func (s *Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Give background goroutines a moment to see the closed flag
	time.Sleep(100 * time.Millisecond)

	// Flush any pending writes
	if err := s.wal.Flush(); err != nil {
		return fmt.Errorf("failed to flush WAL: %w", err)
	}

	// Close WAL
	if err := s.wal.Close(); err != nil {
		return fmt.Errorf("WAL close: %w", err)
	}

	// Close backends gracefully
	for _, backend := range s.backends {
		if err := backend.Close(); err != nil {
			// Don't fail on backend close errors - just log them
			if !strings.Contains(err.Error(), "already closed") {
				fmt.Fprintf(os.Stderr, "Warning: backend close error: %v\n", err)
			}
		}
	}

	// Stop monitoring
	if s.monitoring != nil {
		s.monitoring.Stop()
	}

	return nil
}

// VerifyIntegrity performs a full integrity check of the audit log.
func (s *Sink) VerifyIntegrity() (*IntegrityReport, error) {
	report := &IntegrityReport{
		Timestamp: time.Now(),
	}

	// Verify WAL integrity
	walReport, err := s.wal.VerifyIntegrityReport()
	if err != nil {
		return nil, fmt.Errorf("WAL verification failed: %w", err)
	}
	report.WALIntegrity = walReport

	// Verify compliance chain if enabled
	if s.compliance != nil {
		complReport, err := s.compliance.VerifyChain()
		if err != nil {
			return nil, fmt.Errorf("compliance verification failed: %w", err)
		}
		report.ComplianceIntegrity = complReport
		if !complReport.Valid {
			report.Valid = false
		}
	}

	// Verify backend consistency
	for _, backend := range s.backends {
		backendReport, err := backend.VerifyIntegrity()
		if err != nil {
			report.BackendErrors = append(report.BackendErrors, err)
			continue
		}
		report.BackendReports = append(report.BackendReports, backendReport)
		if !backendReport.Valid {
			report.Valid = false
		}
	}

	report.Valid = walReport.Valid
	report.TotalRecords = walReport.TotalRecords
	report.CorruptedSegments = walReport.CorruptedSegments

	return report, nil
}

// WALPath returns the path to the WAL for recovery operations.
func (s *Sink) WALPath() string {
	return s.config.WALPath
}

// Private methods

func (s *Sink) writeToWAL(event *core.LogEvent) error {
	// Add resilience wrapper if configured
	if s.resilience != nil {
		return s.resilience.Execute(func() error {
			return s.wal.Write(event)
		})
	}
	return s.wal.Write(event)
}

func (s *Sink) handleCriticalFailure(event *core.LogEvent, err error) {
	// Record in monitor
	if s.monitoring != nil {
		s.monitoring.RecordCriticalFailure(err)
	}

	if s.config.FailureHandler != nil {
		s.config.FailureHandler(event, err)
	}

	if s.config.PanicOnFailure {
		panic(fmt.Sprintf("AUDIT SINK CRITICAL FAILURE: %v", err))
	}

	// Last resort: log to stderr
	fmt.Fprintf(os.Stderr, "CRITICAL: Failed to write audit event: %v\n", err)
}

// replicateToBackend replicates events to a specific backend
func (s *Sink) replicateToBackend(backend backends.Backend, event *core.LogEvent) {
	// Check if sink is closed before attempting write
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		// Silently skip - sink is shutting down
		return
	}
	s.mu.RUnlock()

	// Use resilience manager for backend writes
	var writeErr error
	if s.resilience != nil {
		writeErr = s.resilience.ExecuteWithBreaker(backend.Name(), func() error {
			return backend.Write(event)
		})
	} else {
		writeErr = backend.Write(event)
	}

	if writeErr != nil {
		// Check if error is due to backend being closed
		if strings.Contains(writeErr.Error(), "backend closed") || strings.Contains(writeErr.Error(), "closed") {
			// During shutdown, this is expected - don't log
			return
		}

		// Record failure in monitoring
		if s.monitoring != nil {
			s.monitoring.RecordBackendFailure(backend.Name(), writeErr)
		}
		// Log error but don't fail - this is async replication
		fmt.Fprintf(os.Stderr, "Backend %s replication error: %v\n", backend.Name(), writeErr)
	} else if s.monitoring != nil {
		// Record success
		s.monitoring.RecordBackendSuccess(backend.Name())
	}
}

// Replay reads events from the WAL within a time range
func (s *Sink) Replay(start, end time.Time) ([]*core.LogEvent, error) {
	reader, err := wal.NewReader(s.config.WALPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	if start.IsZero() && end.IsZero() {
		return reader.ReadAll()
	}

	return reader.ReadRange(start, end)
}
