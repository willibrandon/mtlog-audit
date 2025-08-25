package monitoring

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Config represents monitoring configuration
type Config struct {
	UpdateInterval time.Duration
	EnableProfiler bool
	WindowSize     int
}

// DefaultConfig returns default monitoring configuration
func DefaultConfig() *Config {
	return &Config{
		UpdateInterval: 10 * time.Second,
		EnableProfiler: false,
		WindowSize:     60,
	}
}

// NewMonitor creates a new monitor from config
func NewMonitor(cfg *Config) *Monitor {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	m := &Monitor{
		updateInterval: cfg.UpdateInterval,
		enableProfiler: cfg.EnableProfiler,
		windowSize:     cfg.WindowSize,
		eventWindow:    make([]int64, cfg.WindowSize),
	}
	
	return m
}

// Monitor manages monitoring and metrics collection
type Monitor struct {
	mu            sync.RWMutex
	started       atomic.Bool
	eventCount    int64
	errorCount    int64
	lastEventTime time.Time
	startTime     time.Time
	ctx           context.Context
	cancel        context.CancelFunc
	
	// Sliding window for throughput calculation
	eventWindow   []int64
	windowSize    int
	windowIndex   int
	
	// Options
	updateInterval time.Duration
	enableProfiler bool
}

// Option configures the monitor
type Option func(*Monitor)

// WithUpdateInterval sets the metrics update interval
func WithUpdateInterval(interval time.Duration) Option {
	return func(m *Monitor) {
		m.updateInterval = interval
	}
}

// WithProfiler enables memory profiling
func WithProfiler(enabled bool) Option {
	return func(m *Monitor) {
		m.enableProfiler = enabled
	}
}

// New creates a new monitor
func New(opts ...Option) *Monitor {
	m := &Monitor{
		updateInterval: 10 * time.Second,
		windowSize:     60, // 60 samples for throughput
		eventWindow:    make([]int64, 60),
	}
	
	for _, opt := range opts {
		opt(m)
	}
	
	return m
}

// Start starts the monitor
func (m *Monitor) Start() {
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	
	m.mu.Lock()
	m.startTime = time.Now()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.mu.Unlock()
	
	// Increment active sinks
	UpdateActiveSinks(1)
	
	// Start background metrics updater
	go m.runMetricsUpdater()
}

// Stop stops the monitor
func (m *Monitor) Stop() {
	if !m.started.CompareAndSwap(true, false) {
		return
	}
	
	// Cancel background tasks
	if m.cancel != nil {
		m.cancel()
	}
	
	// Decrement active sinks
	UpdateActiveSinks(0)
}

// IncrementEventCount increments the event counter
func (m *Monitor) IncrementEventCount() {
	atomic.AddInt64(&m.eventCount, 1)
	m.mu.Lock()
	m.lastEventTime = time.Now()
	m.mu.Unlock()
}

// IncrementErrorCount increments the error counter
func (m *Monitor) IncrementErrorCount() {
	atomic.AddInt64(&m.errorCount, 1)
}

// RecordLatency records operation latency
func (m *Monitor) RecordLatency(duration time.Duration) {
	WriteLatency.WithLabelValues("primary", "success").Observe(duration.Seconds())
}

// RecordError records an error
func (m *Monitor) RecordError(err error) {
	m.IncrementErrorCount()
	EventsWritten.WithLabelValues("failure", "unknown").Inc()
}

// RecordEmit records an event emission
func (m *Monitor) RecordEmit() {
	m.IncrementEventCount()
	EventsWritten.WithLabelValues("success", "audit").Inc()
}

// RecordCriticalFailure records a critical failure
func (m *Monitor) RecordCriticalFailure(err error) {
	m.IncrementErrorCount()
	UpdateIntegrityScore(0) // Critical failure drops integrity to 0
	EventsWritten.WithLabelValues("failure", "audit").Inc()
}

// RecordBackendError records a backend error
func (m *Monitor) RecordBackendError(backend string, err error) {
	BackendOperations.WithLabelValues(backend, "write", "failure").Inc()
}

// RecordBackendFailure records a backend failure
func (m *Monitor) RecordBackendFailure(backend string, err error) {
	m.IncrementErrorCount()
	BackendOperations.WithLabelValues(backend, "write", "failure").Inc()
}

// RecordBackendSuccess records a successful backend operation
func (m *Monitor) RecordBackendSuccess(backend string) {
	BackendOperations.WithLabelValues(backend, "write", "success").Inc()
}

// GetStats returns current statistics
func (m *Monitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	uptime := time.Since(m.startTime)
	events := atomic.LoadInt64(&m.eventCount)
	errors := atomic.LoadInt64(&m.errorCount)
	
	errorRate := float64(0)
	if events > 0 {
		errorRate = float64(errors) / float64(events)
	}
	
	throughput := m.calculateThroughput()
	
	return Stats{
		Uptime:        uptime,
		EventsWritten: events,
		ErrorCount:    errors,
		ErrorRate:     errorRate,
		Throughput:    throughput,
		LastEventTime: m.lastEventTime,
	}
}

// calculateThroughput calculates current throughput
func (m *Monitor) calculateThroughput() float64 {
	// Calculate average over the window
	total := int64(0)
	count := 0
	
	for _, v := range m.eventWindow {
		if v > 0 {
			total += v
			count++
		}
	}
	
	if count == 0 {
		return 0
	}
	
	// Average events per sample interval
	avgPerInterval := float64(total) / float64(count)
	
	// Convert to events per second
	intervalsPerSecond := 1.0 / m.updateInterval.Seconds()
	return avgPerInterval * intervalsPerSecond
}

// runMetricsUpdater updates metrics periodically
func (m *Monitor) runMetricsUpdater() {
	ticker := time.NewTicker(m.updateInterval)
	defer ticker.Stop()
	
	lastEventCount := int64(0)
	
	for {
		select {
		case <-m.ctx.Done():
			return
			
		case <-ticker.C:
			m.updateMetrics(&lastEventCount)
		}
	}
}

// updateMetrics updates all metrics
func (m *Monitor) updateMetrics(lastEventCount *int64) {
	// Calculate events in this interval
	currentCount := atomic.LoadInt64(&m.eventCount)
	intervalEvents := currentCount - *lastEventCount
	*lastEventCount = currentCount
	
	// Update sliding window
	m.mu.Lock()
	m.eventWindow[m.windowIndex] = intervalEvents
	m.windowIndex = (m.windowIndex + 1) % m.windowSize
	throughput := m.calculateThroughput()
	m.mu.Unlock()
	
	// Update throughput metric
	UpdateThroughput(throughput)
	
	// Calculate and update error rate
	errors := atomic.LoadInt64(&m.errorCount)
	errorRate := float64(0)
	if currentCount > 0 {
		errorRate = float64(errors) / float64(currentCount)
	}
	UpdateErrorRate("sink", errorRate)
	
	// Update integrity score (simplified calculation)
	integrityScore := 100.0
	if errorRate > 0 {
		integrityScore = 100.0 * (1.0 - errorRate)
		if integrityScore < 0 {
			integrityScore = 0
		}
	}
	UpdateIntegrityScore(integrityScore)
	
	// Update memory usage if profiler is enabled
	if m.enableProfiler {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		UpdateMemoryUsage(int64(memStats.Alloc))
	}
}

// Stats contains monitor statistics
type Stats struct {
	Uptime        time.Duration
	EventsWritten int64
	ErrorCount    int64
	ErrorRate     float64
	Throughput    float64 // events per second
	LastEventTime time.Time
}

// HealthCheck performs a health check
func (m *Monitor) HealthCheck() Health {
	stats := m.GetStats()
	
	status := HealthStatusHealthy
	issues := []string{}
	
	// Check error rate
	if stats.ErrorRate > 0.05 { // > 5% error rate
		status = HealthStatusDegraded
		issues = append(issues, "High error rate")
	}
	
	if stats.ErrorRate > 0.5 { // > 50% error rate
		status = HealthStatusUnhealthy
	}
	
	// Check last event time
	if time.Since(stats.LastEventTime) > 5*time.Minute {
		if status == HealthStatusHealthy {
			status = HealthStatusDegraded
		}
		issues = append(issues, "No recent events")
	}
	
	// Check throughput (if we expect events)
	if stats.EventsWritten > 0 && stats.Throughput == 0 {
		if status == HealthStatusHealthy {
			status = HealthStatusDegraded
		}
		issues = append(issues, "Zero throughput")
	}
	
	return Health{
		Status:    status,
		Timestamp: time.Now(),
		Uptime:    stats.Uptime,
		Issues:    issues,
		Stats:     stats,
	}
}

// Health represents health status
type Health struct {
	Status    HealthStatus
	Timestamp time.Time
	Uptime    time.Duration
	Issues    []string
	Stats     Stats
}

// HealthStatus represents health status
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)