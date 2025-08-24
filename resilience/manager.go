package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Manager coordinates resilience strategies
type Manager struct {
	mu              sync.RWMutex
	retryPolicy     *RetryPolicy
	circuitBreakers map[string]*CircuitBreaker
	retryExecutor   *RetryExecutor
	defaultBreaker  *CircuitBreaker
}

// Option configures the resilience manager
type Option func(*Manager)

// New creates a new resilience manager
func New(opts ...Option) *Manager {
	m := &Manager{
		retryPolicy:     DefaultRetryPolicy(),
		circuitBreakers: make(map[string]*CircuitBreaker),
	}
	
	// Apply options
	for _, opt := range opts {
		opt(m)
	}
	
	// Create retry executor
	m.retryExecutor = NewRetryExecutor(m.retryPolicy)
	
	// Create default circuit breaker
	if m.defaultBreaker == nil {
		m.defaultBreaker = NewCircuitBreaker(CircuitBreakerConfig{
			Name:         "default",
			MaxFailures:  5,
			ResetTimeout: 60 * time.Second,
		})
	}
	
	return m
}

// WithRetryPolicy sets the retry policy
func WithRetryPolicy(policy *RetryPolicy) Option {
	return func(m *Manager) {
		m.retryPolicy = policy
	}
}

// WithCircuitBreaker adds a named circuit breaker
func WithCircuitBreaker(name string, config CircuitBreakerConfig) Option {
	return func(m *Manager) {
		config.Name = name
		m.circuitBreakers[name] = NewCircuitBreaker(config)
	}
}

// WithDefaultCircuitBreaker sets the default circuit breaker
func WithDefaultCircuitBreaker(config CircuitBreakerConfig) Option {
	return func(m *Manager) {
		config.Name = "default"
		m.defaultBreaker = NewCircuitBreaker(config)
	}
}

// Execute executes a function with retry and circuit breaker protection
func (m *Manager) Execute(fn func() error) error {
	return m.ExecuteWithBreaker("default", fn)
}

// ExecuteWithBreaker executes with a specific circuit breaker
func (m *Manager) ExecuteWithBreaker(breakerName string, fn func() error) error {
	breaker := m.getBreaker(breakerName)
	
	// Execute through circuit breaker first
	return breaker.Execute(func() error {
		// Then apply retry policy
		return m.retryExecutor.Execute(fn)
	})
}

// ExecuteWithContext executes with context support
func (m *Manager) ExecuteWithContext(ctx context.Context, fn func() error) error {
	return m.ExecuteWithBreakerAndContext(ctx, "default", fn)
}

// ExecuteWithBreakerAndContext executes with context and specific breaker
func (m *Manager) ExecuteWithBreakerAndContext(ctx context.Context, breakerName string, fn func() error) error {
	breaker := m.getBreaker(breakerName)
	
	// Check context first
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	
	// Execute through circuit breaker
	return breaker.Execute(func() error {
		// Then apply retry policy with context
		return m.retryPolicy.ExecuteWithContext(ctx, fn)
	})
}

// getBreaker gets a circuit breaker by name
func (m *Manager) getBreaker(name string) *CircuitBreaker {
	m.mu.RLock()
	breaker, exists := m.circuitBreakers[name]
	m.mu.RUnlock()
	
	if exists {
		return breaker
	}
	
	return m.defaultBreaker
}

// GetCircuitBreakerStats returns stats for all circuit breakers
func (m *Manager) GetCircuitBreakerStats() map[string]CircuitBreakerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	stats := make(map[string]CircuitBreakerStats)
	
	// Get default breaker stats
	stats["default"] = m.defaultBreaker.GetStats()
	
	// Get all named breaker stats
	for name, breaker := range m.circuitBreakers {
		stats[name] = breaker.GetStats()
	}
	
	return stats
}

// GetRetryStats returns retry statistics
func (m *Manager) GetRetryStats() RetryStats {
	return m.retryExecutor.GetStats()
}

// ResetCircuitBreaker resets a specific circuit breaker
func (m *Manager) ResetCircuitBreaker(name string) {
	breaker := m.getBreaker(name)
	breaker.Reset()
}

// ResetAllCircuitBreakers resets all circuit breakers
func (m *Manager) ResetAllCircuitBreakers() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	m.defaultBreaker.Reset()
	
	for _, breaker := range m.circuitBreakers {
		breaker.Reset()
	}
}

// HealthCheck performs a health check on all components
func (m *Manager) HealthCheck() HealthReport {
	report := HealthReport{
		Timestamp: time.Now(),
		Healthy:   true,
	}
	
	// Check circuit breakers
	stats := m.GetCircuitBreakerStats()
	for name, stat := range stats {
		if stat.State == StateOpen {
			report.Healthy = false
			report.Issues = append(report.Issues, 
				fmt.Sprintf("Circuit breaker '%s' is open", name))
		}
		
		// Warn if success rate is low
		if stat.TotalCalls > 100 && stat.SuccessRate() < 0.5 {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("Circuit breaker '%s' has low success rate: %.2f%%", 
					name, stat.SuccessRate()*100))
		}
	}
	
	// Check retry stats
	retryStats := m.GetRetryStats()
	if retryStats.TotalAttempts > 0 {
		failureRate := float64(retryStats.FailedRetries) / float64(retryStats.TotalAttempts)
		if failureRate > 0.5 {
			report.Warnings = append(report.Warnings,
				fmt.Sprintf("High retry failure rate: %.2f%%", failureRate*100))
		}
	}
	
	report.CircuitBreakers = stats
	report.RetryStats = retryStats
	
	return report
}

// HealthReport contains health check results
type HealthReport struct {
	Timestamp       time.Time
	Healthy         bool
	Issues          []string
	Warnings        []string
	CircuitBreakers map[string]CircuitBreakerStats
	RetryStats      RetryStats
}

// BulkExecutor executes bulk operations with resilience
type BulkExecutor struct {
	manager         *Manager
	partialSuccess  bool
	minSuccessRate  float64
	maxConcurrency  int
}

// NewBulkExecutor creates a new bulk executor
func (m *Manager) NewBulkExecutor(partialSuccess bool, minSuccessRate float64, maxConcurrency int) *BulkExecutor {
	if maxConcurrency <= 0 {
		maxConcurrency = 10
	}
	
	return &BulkExecutor{
		manager:        m,
		partialSuccess: partialSuccess,
		minSuccessRate: minSuccessRate,
		maxConcurrency: maxConcurrency,
	}
}

// Execute executes a bulk operation
func (be *BulkExecutor) Execute(items []interface{}, fn func(interface{}) error) ([]error, error) {
	errors := make([]error, len(items))
	successCount := 0
	
	// Use semaphore for concurrency control
	sem := make(chan struct{}, be.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	for i, item := range items {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore
		
		go func(idx int, itm interface{}) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore
			
			err := be.manager.Execute(func() error {
				return fn(itm)
			})
			
			mu.Lock()
			if err != nil {
				errors[idx] = err
			} else {
				successCount++
			}
			mu.Unlock()
		}(i, item)
	}
	
	wg.Wait()
	
	// Check success rate
	successRate := float64(successCount) / float64(len(items))
	
	if !be.partialSuccess && successCount < len(items) {
		return errors, fmt.Errorf("bulk operation failed: %d/%d succeeded", successCount, len(items))
	}
	
	if be.minSuccessRate > 0 && successRate < be.minSuccessRate {
		return errors, fmt.Errorf("success rate too low: %.2f%% < %.2f%%",
			successRate*100, be.minSuccessRate*100)
	}
	
	return errors, nil
}