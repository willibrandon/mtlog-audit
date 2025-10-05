package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Event metrics
	EventsWritten = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_events_total",
		Help: "Total number of audit events written",
	}, []string{"status", "profile"})

	WriteLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mtlog_audit_write_duration_seconds",
		Help:    "Write latency in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to 32s
	}, []string{"backend", "status"})

	EventSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mtlog_audit_event_size_bytes",
		Help:    "Size of audit events in bytes",
		Buckets: prometheus.ExponentialBuckets(100, 2, 15), // 100B to 3.2MB
	})

	// WAL metrics
	WALSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_wal_size_bytes",
		Help: "Current WAL size in bytes",
	})

	WALSegments = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_wal_segments_total",
		Help: "Number of WAL segments",
	})

	WALCorruptions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mtlog_audit_wal_corruptions_total",
		Help: "Total number of WAL corruptions detected",
	})

	WALRecoveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_wal_recoveries_total",
		Help: "Total number of WAL recovery attempts",
	}, []string{"status"})

	// Compliance metrics
	ComplianceOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_compliance_operations_total",
		Help: "Total number of compliance operations",
	}, []string{"profile", "operation", "status"})

	EncryptionOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_encryption_operations_total",
		Help: "Total number of encryption operations",
	}, []string{"algorithm", "operation", "status"})

	SigningOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_signing_operations_total",
		Help: "Total number of signing operations",
	}, []string{"algorithm", "operation", "status"})

	// Backend metrics
	BackendOperations = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_backend_operations_total",
		Help: "Total number of backend operations",
	}, []string{"backend", "operation", "status"})

	BackendLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mtlog_audit_backend_latency_seconds",
		Help:    "Backend operation latency",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"backend", "operation"})

	BackendSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mtlog_audit_backend_size_bytes",
		Help: "Backend storage size in bytes",
	}, []string{"backend"})

	// Resilience metrics
	RetryAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_retry_attempts_total",
		Help: "Total number of retry attempts",
	}, []string{"operation", "status"})

	CircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mtlog_audit_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"breaker"})

	CircuitBreakerTrips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mtlog_audit_circuit_breaker_trips_total",
		Help: "Total number of circuit breaker trips",
	}, []string{"breaker"})

	// System metrics
	IntegrityScore = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_integrity_score",
		Help: "Current integrity score (0-100)",
	})

	ActiveSinks = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_active_sinks_total",
		Help: "Number of active audit sinks",
	})

	ErrorRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mtlog_audit_error_rate",
		Help: "Current error rate",
	}, []string{"component"})

	// Performance metrics
	ThroughputRate = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_throughput_events_per_second",
		Help: "Current throughput in events per second",
	})

	QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mtlog_audit_queue_depth",
		Help: "Current queue depth",
	}, []string{"queue"})

	MemoryUsage = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mtlog_audit_memory_usage_bytes",
		Help: "Current memory usage in bytes",
	})
)

// RecordEvent records an event metric
func RecordEvent(status, profile string) {
	EventsWritten.WithLabelValues(status, profile).Inc()
}

// RecordWriteLatency records write latency
func RecordWriteLatency(backend string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	WriteLatency.WithLabelValues(backend, status).Observe(duration.Seconds())
}

// RecordEventSize records event size
func RecordEventSize(size int) {
	EventSize.Observe(float64(size))
}

// UpdateWALMetrics updates WAL metrics
func UpdateWALMetrics(size int64, segments int) {
	WALSize.Set(float64(size))
	WALSegments.Set(float64(segments))
}

// RecordWALCorruption records a WAL corruption
func RecordWALCorruption() {
	WALCorruptions.Inc()
}

// RecordWALRecovery records a WAL recovery attempt
func RecordWALRecovery(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	WALRecoveries.WithLabelValues(status).Inc()
}

// RecordComplianceOperation records a compliance operation
func RecordComplianceOperation(profile, operation string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	ComplianceOperations.WithLabelValues(profile, operation, status).Inc()
}

// RecordEncryption records an encryption operation
func RecordEncryption(algorithm, operation string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	EncryptionOperations.WithLabelValues(algorithm, operation, status).Inc()
}

// RecordSigning records a signing operation
func RecordSigning(algorithm, operation string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	SigningOperations.WithLabelValues(algorithm, operation, status).Inc()
}

// RecordBackendOperation records a backend operation
func RecordBackendOperation(backend, operation string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	BackendOperations.WithLabelValues(backend, operation, status).Inc()
}

// RecordBackendLatency records backend latency
func RecordBackendLatency(backend, operation string, duration time.Duration) {
	BackendLatency.WithLabelValues(backend, operation).Observe(duration.Seconds())
}

// UpdateBackendSize updates backend size metric
func UpdateBackendSize(backend string, size int64) {
	BackendSize.WithLabelValues(backend).Set(float64(size))
}

// RecordRetry records a retry attempt
func RecordRetry(operation string, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	RetryAttempts.WithLabelValues(operation, status).Inc()
}

// UpdateCircuitBreakerState updates circuit breaker state
func UpdateCircuitBreakerState(breaker string, state int) {
	CircuitBreakerState.WithLabelValues(breaker).Set(float64(state))
}

// RecordCircuitBreakerTrip records a circuit breaker trip
func RecordCircuitBreakerTrip(breaker string) {
	CircuitBreakerTrips.WithLabelValues(breaker).Inc()
}

// UpdateIntegrityScore updates the integrity score
func UpdateIntegrityScore(score float64) {
	IntegrityScore.Set(score)
}

// UpdateActiveSinks updates the number of active sinks
func UpdateActiveSinks(count int) {
	ActiveSinks.Set(float64(count))
}

// UpdateErrorRate updates the error rate
func UpdateErrorRate(component string, rate float64) {
	ErrorRate.WithLabelValues(component).Set(rate)
}

// UpdateThroughput updates the throughput rate
func UpdateThroughput(eventsPerSecond float64) {
	ThroughputRate.Set(eventsPerSecond)
}

// UpdateQueueDepth updates queue depth
func UpdateQueueDepth(queue string, depth int) {
	QueueDepth.WithLabelValues(queue).Set(float64(depth))
}

// UpdateMemoryUsage updates memory usage
func UpdateMemoryUsage(bytes int64) {
	MemoryUsage.Set(float64(bytes))
}
